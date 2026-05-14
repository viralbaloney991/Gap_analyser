package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

// ── Data models ──────────────────────────────────────────────────────────────

type huntLogEvent struct {
	Timestamp string `json:"timestamp"`
	Host      string `json:"host"`
	User      string `json:"user"`
	Command   string `json:"command"`
}

type queryDoneData struct {
	Hits         int            `json:"hits"`
	Hosts        int            `json:"hosts"`
	LastSeen     string         `json:"last_seen"`
	UniqueUsers  int            `json:"unique_users"`
	SampleEvents []huntLogEvent `json:"sample_events"`
	CxCommand    string         `json:"cx_command"`
	rawEvents    []json.RawMessage // excluded from SSE payload (unexported)
}

type ollyDoneData struct {
	Sections map[string]string `json:"sections"`
}

type huntFinding struct {
	Text     string `json:"text"`
	Severity string `json:"severity"`
}

type huntAction struct {
	Priority    int    `json:"priority"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:"level"`
}

type huntAlertDef struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Condition string `json:"condition"`
	Severity  string `json:"severity"`
	GroupBy   string `json:"group_by"`
}

type huntStats struct {
	Hits         string `json:"hits"`
	Hosts        string `json:"hosts"`
	AttackWindow string `json:"attack_window"`
	C2Flagged    bool   `json:"c2_flagged"`
}

type huntReport struct {
	Verdict       string        `json:"verdict"`
	Confidence    string        `json:"confidence"`
	Title         string        `json:"title"`
	Subtitle      string        `json:"subtitle"`
	Stats         huntStats     `json:"stats"`
	Findings      []huntFinding `json:"findings"`
	Actions       []huntAction  `json:"actions"`
	AlertDef      huntAlertDef  `json:"alert_def"`
	RunDurationMs int64         `json:"run_duration_ms"`
	Timestamp     string        `json:"timestamp"`
}

type huntErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Input sanitization ───────────────────────────────────────────────────────

var queryAllowlist = regexp.MustCompile(`^[\x20-\x7E]+$`)
var queryForbidden = regexp.MustCompile("[`" + `;|\\&\n\r]|\$[({]`)

func sanitizeQuery(q string) error {
	if len(q) > 1000 {
		return errors.New("query exceeds 1000 character limit")
	}
	if !queryAllowlist.MatchString(q) {
		return errors.New("query contains non-printable characters")
	}
	if queryForbidden.MatchString(q) {
		return errors.New("query contains forbidden characters")
	}
	return nil
}

// ── Olly prompts ──────────────────────────────────────────────────────────────
//
// Two-pass design: pass 1 (gpt-5.2, focus, ~72s) discovers real field names
// via 4 targeted questions and returns a chat_id. Pass 2 (claude-sonnet-4-5,
// skill, --chat-id, ~216s) continues the same chat and produces the full
// 11-section report with live pivot findings.
// All analysis stays inside Coralogix infrastructure.

const ollySchemaPromptTemplate = `Schema discovery for threat hunt. Answer ONLY these 4 questions:

Query: {{.LuceneQuery}}
Sample events ({{.SampleCount}} retrieved from last {{.Window}}):
{{.SampleEvents}}
1. Field mapping: for each key term in the Lucene query, find the matching DataPrime $d path
   in the sample events. Which confirmed paths have data?

2. Pattern match: translate the Lucene query to DataPrime using the confirmed fields from
   question 1. Run it. Do the specific IOCs/patterns actually appear? How many events match?

3. True total: run source logs | filter <your DataPrime translation> | count() as total
   Report the exact number on its own line as: "Total: N"

4. Top patterns: source logs | filter <condition> | groupby <most relevant field> aggregate
   count() as hits | orderby hits desc | limit 5

Facts only. No preamble.`

type ollySchemaPromptData struct {
	LuceneQuery  string
	SampleCount  int
	SampleEvents string
	Window       string
}

var ollySchemaTmpl = template.Must(template.New("ollySchema").Parse(ollySchemaPromptTemplate))

func buildOllySchemaPrompt(luceneQuery string, sampleCount int, sampleEvents, window string) (string, error) {
	var buf bytes.Buffer
	err := ollySchemaTmpl.Execute(&buf, ollySchemaPromptData{
		LuceneQuery:  luceneQuery,
		SampleCount:  sampleCount,
		SampleEvents: sampleEvents,
		Window:       window,
	})
	if err != nil {
		return "", fmt.Errorf("render olly schema prompt: %w", err)
	}
	return buf.String(), nil
}

// ollyReportPrompt is the pass-2 message continuing the pass-1 chat (--chat-id).
// Olly already has confirmed field names from pass 1 and uses them for live pivots.
// 11 sections: findings lead, analyst workflow dropped.
const ollyReportPrompt = `Using what you found above, write the complete threat hunt report.
For section 1, RUN the confirmed DataPrime query and report actual results — do not just describe them.

## 1. What We Found
RUN the confirmed DataPrime query. Report actual results:
- Total hits (use the count from schema discovery)
- Top 5 users / actors involved
- Top 5 source IPs
- Time distribution: clustered within minutes (automated/tool) or spread over hours/days (human)?
- Any standout anomalies in the data

## 2. Hunt Summary
Severity (Critical/High/Medium/Low), Confidence (High/Medium/Low), MITRE ATT&CK tactic/technique.
2-3 sentences on what this query detects and why it matters.

## 3. Original Query
Echo the Lucene query verbatim.

## 4. Schema Mapping
Table: Lucene field | Confirmed $d path | Has data (Y/N)

## 5. Translated Query — DataPrime
Using ONLY confirmed fields from section 4.

## 6. Translated Query — Lucene
Optimised Lucene with confirmed field paths.

## 7. Detection Logic
Plain English: what does this detect, what attacker behaviour does it expose, why it matters.

## 8. False Positive Sources
Likely benign causes with suppression suggestions.

## 9. Visibility Gaps
Missing log sources or fields that limit confidence in this hunt.

## 10. Follow-up Hunts
3 related hunts with DataPrime query sketches.

## 11. Alert Definition
Name: <descriptive>
Type: standard
Condition: count > 0
Severity: <Critical/High/Medium/Low>
Group-By: <most relevant confirmed field>`

// buildOllyReportPrompt returns the pass-2 report prompt. The wrapper exists as
// a seam for future per-client or per-technique parameterisation without
// changing call sites.
func buildOllyReportPrompt() string {
	return ollyReportPrompt
}

// Deprecated: buildOllyPrompt delegates to buildOllySchemaPrompt for backward
// compatibility with existing tests. New call sites use buildOllySchemaPrompt directly.
func buildOllyPrompt(luceneQuery string, hitCount int, sampleEvents string) (string, error) {
	return buildOllySchemaPrompt(luceneQuery, hitCount, sampleEvents, "30d")
}

var totalCountRe = regexp.MustCompile(`(?i)Total:\s*(\d+)`)

// extractTotalFromPass1 parses the "Total: N" line that the pass-1 prompt
// instructs Olly to emit. Returns (n, true) when the pattern is found (including
// "Total: 0"), and (0, false) when the pattern is absent entirely.
func extractTotalFromPass1(text string) (int, bool) {
	if m := totalCountRe.FindStringSubmatch(text); len(m) > 1 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

// ── Section parser ────────────────────────────────────────────────────────────

var sectionHeaderRe = regexp.MustCompile(`(?m)^##\s+§(\d+)\s+`)
// numberedHeaderRe matches "## 1. Title", "## **1. Title**", or "## 12. Title" (Olly structured output).
var numberedHeaderRe = regexp.MustCompile(`(?m)^##\s+\**(\d+)\.\**\s+`)
var genericHeaderRe = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)

// genericSectionMap maps Olly's native free-form header names to §N slot numbers.
var genericSectionMap = map[string]string{
	"summary":      "1",
	"findings":     "6",
	"next steps":   "11",
	"next step":    "11",
	"action items": "11",
	"actions":      "11",
}

func parseOllySections(output string) map[string]string {
	sections := make(map[string]string, 14)

	// Extract Olly chat URL from any artifact link in the response.
	if m := ollyURLRe.FindString(output); m != "" {
		// Keep only the base chat URL (strip /artifact/... suffix if present).
		if idx := strings.Index(m, "/artifact/"); idx != -1 {
			m = m[:idx]
		}
		sections["chat_url"] = m
	}

	// Try structured §N format first.
	matches := sectionHeaderRe.FindAllStringIndex(output, -1)
	if len(matches) > 0 {
		nums := sectionHeaderRe.FindAllStringSubmatch(output, -1)
		for i, loc := range matches {
			start := loc[1]
			var end int
			if i+1 < len(matches) {
				end = matches[i+1][0]
			} else {
				end = len(output)
			}
			sections[nums[i][1]] = strings.TrimSpace(output[start:end])
		}
		return sections
	}

	// Try numbered format: "## 1. Title", "## **1. Title**" (Olly structured output).
	nmatches := numberedHeaderRe.FindAllStringIndex(output, -1)
	if len(nmatches) > 0 {
		nnums := numberedHeaderRe.FindAllStringSubmatch(output, -1)
		for i, loc := range nmatches {
			start := loc[1]
			var end int
			if i+1 < len(nmatches) {
				end = nmatches[i+1][0]
			} else {
				end = len(output)
			}
			sections[nnums[i][1]] = strings.TrimSpace(output[start:end])
		}
		return sections
	}

	// Fall back: Olly's native free-form ## Header format.
	gmatches := genericHeaderRe.FindAllStringIndex(output, -1)
	gheaders := genericHeaderRe.FindAllStringSubmatch(output, -1)
	for i, loc := range gmatches {
		start := loc[1]
		var end int
		if i+1 < len(gmatches) {
			end = gmatches[i+1][0]
		} else {
			end = len(output)
		}
		content := strings.TrimSpace(output[start:end])
		name := strings.ToLower(strings.TrimSpace(gheaders[i][1]))
		if slot, ok := genericSectionMap[name]; ok {
			sections[slot] = content
		}
	}

	// Ensure section "1" has at least a trimmed version of the full response as
	// the report subtitle, stripping the leading "Chat ID: ..." line if present.
	if sections["1"] == "" {
		body := strings.TrimSpace(chatIDLineRe.ReplaceAllString(output, ""))
		sections["1"] = body
	}
	return sections
}

var chatIDLineRe = regexp.MustCompile(`(?m)^Chat ID:.*\n?`)
var ollyURLRe = regexp.MustCompile(`https://[^)\s]+\.coralogix\.com/#/olly/chat/[a-f0-9-]+`)

var agentsUUIDRe = regexp.MustCompile(`"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"`)
var agentsResponseRe = regexp.MustCompile(`(?s)completed,"(.*?)",(?:focus|skill|fast|deep-research),"`)

// parseAgentsOutput extracts chatID and response text from cx olly --output agents format.
// Returns empty strings if the format is not recognised (e.g. plain text or error output).
func parseAgentsOutput(data []byte) (chatID, response string) {
	if m := agentsUUIDRe.FindSubmatch(data); len(m) > 1 {
		chatID = string(m[1])
	}
	if m := agentsResponseRe.FindSubmatch(data); len(m) > 1 {
		// Unescape \n sequences that cx encodes in the agents JSON-like format.
		response = strings.ReplaceAll(string(m[1]), `\n`, "\n")
	}
	return
}

// ── Verdict derivation ────────────────────────────────────────────────────────

var severityRe = regexp.MustCompile(`(?i)Severity:\s*(\w+)`)
var confidenceRe = regexp.MustCompile(`(?i)Confidence:\s*(\w+)`)
var numberedListRe = regexp.MustCompile(`^\d+\.\s`)

func deriveVerdict(hits int, section1 string) (verdict, confidence string) {
	sev := "medium"
	conf := "medium"

	if m := severityRe.FindStringSubmatch(section1); len(m) > 1 {
		sev = strings.ToLower(m[1])
	}
	if m := confidenceRe.FindStringSubmatch(section1); len(m) > 1 {
		conf = strings.ToLower(m[1])
	}

	if hits == 0 {
		return "clean", conf
	}

	isCritical := sev == "critical"
	isHighSev := sev == "high"
	isHighConf := conf == "high"

	if isCritical || (isHighSev && isHighConf) {
		return "threat", conf
	}
	return "suspicious", conf
}

// ── cx executor ────────────────────────────────────────────────────────────────

// ollyModel is the cx model used for both passes.
// claude-sonnet-4-5 is the only model that supports --mode skill and has
// DataPrime expertise. GPT focus mode reliably 524s on the Coralogix backend.
const ollyModel = "claude-sonnet-4-5"

type cxExecutor interface {
	runLogs(ctx context.Context, query, window string) ([]byte, error)
	runOllySchema(ctx context.Context, prompt string) ([]byte, error)
	runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error)
}

type cxRunner struct {
	binPath string
	apiKey  string
	region  string
}

const maxOutputBytes = 64 * 1024 // 64 KB

func (r *cxRunner) env() []string {
	e := os.Environ()
	if r.apiKey != "" {
		e = append(e, "CX_API_KEY="+r.apiKey)
	}
	if r.region != "" {
		e = append(e, "CX_REGION="+r.region)
	}
	return e
}

func (r *cxRunner) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "logs", query,
		"--start", "now-"+window, "--output", "agents", "--limit", "50")
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
	// Pass 1: claude-sonnet-4-5 in skill mode — same model as pass 2 because
	// GPT focus mode reliably 524s on the Coralogix backend. The focused 4-question
	// prompt completes quickly; --chat-id in pass 2 continues this session.
	// --output agents returns structured CSV: "chat_id","interaction_id",status,"response",mode,"model"
	// The first UUID in the output is the chat_id used to continue in pass 2.
	cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask",
		"--output", "agents", "--mode", "skill", "--model", ollyModel, "--timeout", "300", prompt)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	// Pass 2: continues the pass-1 chat (via --chat-id) so Olly uses confirmed
	// field names from schema discovery to run live pivot queries in section 8.
	args := []string{"olly", "ask", "--mode", "skill", "--model", ollyModel, "--timeout", "600"}
	if chatID != "" {
		args = append(args, "--chat-id", chatID)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, r.binPath, args...)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}

func readCapped(cmd *exec.Cmd, limit int64) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, fmt.Errorf("cx start: %w", err)
	}

	pw.Close()
	limited, _ := io.ReadAll(io.LimitReader(pr, limit))
	pr.Close()

	werr := cmd.Wait()
	// If we read a full limit-sized chunk, the child likely got SIGPIPE when we
	// closed the pipe. Treat that as a successful cap rather than an error.
	if werr != nil && int64(len(limited)) < limit {
		return nil, fmt.Errorf("cx exit: %w; stderr: %s", werr, stderr.String())
	}
	return limited, nil
}

// ── SSE helpers ───────────────────────────────────────────────────────────────

// noopFlusher is a fallback Flusher for environments that don't support
// http.Flusher (notably httptest.ResponseRecorder in some Go versions).
type noopFlusher struct{}

func (noopFlusher) Flush() {}

func sendSSE(w http.ResponseWriter, f http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	f.Flush()
}

// ── HandleHuntStream ──────────────────────────────────────────────────────────

const huntTimeout = 10 * time.Minute

func (h *Handler) HandleHuntStream(w http.ResponseWriter, r *http.Request) {
	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
	} else {
		flusher = noopFlusher{}
	}

	q := r.URL.Query()
	lucene := q.Get("lucene")
	window := q.Get("window")
	if window == "" {
		window = "30d"
	}
	name := q.Get("name")
	severity := q.Get("severity")
	techniqueId := q.Get("techniqueId")
	clientName := q.Get("client")

	if lucene == "" {
		writeError(w, http.StatusBadRequest, "missing required param: lucene")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	huntID := uuid.New().String()
	sendSSE(w, flusher, "stream_opened", map[string]string{"hunt_id": huntID})

	if err := sanitizeQuery(lucene); err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "invalid_query", Message: err.Error()})
		return
	}

	if h.cxExec == nil && h.cxBinPath == "" {
		sendSSE(w, flusher, "error", huntErrorData{
			Code:    "cx_not_configured",
			Message: "cx CLI not configured: set CX_BIN_PATH environment variable or install cx in PATH",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), huntTimeout)
	defer cancel()

	start := time.Now()

	cx := h.cxExec
	if cx == nil {
		runner := &cxRunner{binPath: h.cxBinPath}
		if h.config != nil {
			if clientCfg, ok := h.config.Clients[clientName]; ok {
				runner.apiKey = clientCfg.APIKey
				runner.region = clientCfg.Region
			}
		}
		cx = runner
	}

	// Step 1: cx logs
	cxCmd := fmt.Sprintf("cx logs '%s' --start now-%s --output agents --limit 50", lucene, window)
	logsOut, err := cx.runLogs(ctx, lucene, window)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "cx_logs_failed", Message: err.Error()})
		return
	}

	qd := parseLogsOutput(logsOut, cxCmd)
	sendSSE(w, flusher, "query_done", qd)

	// Step 2a: Pass 1 — schema discovery (gpt-5.2, focus, ~72s)
	// Uses --output agents to get structured output with chat_id.
	sampleText := formatSampleEvents(qd.rawEvents)
	schemaPrompt, err := buildOllySchemaPrompt(lucene, qd.Hits, sampleText, window)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "prompt_build_failed", Message: err.Error()})
		return
	}

	schemaOut, err := cx.runOllySchema(ctx, schemaPrompt)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
		return
	}

	chatID, schemaText := parseAgentsOutput(schemaOut)
	if total, ok := extractTotalFromPass1(schemaText); ok {
		qd.Hits = total
	}

	// Step 2b: Pass 2 — full 11-section report (claude-sonnet-4-5, skill, ~216s)
	// Continues the same chat so Olly has confirmed field names for live pivots.
	// Falls back to a fresh chat if chat_id could not be extracted.
	reportOut, err := cx.runOllyReport(ctx, chatID, buildOllyReportPrompt())
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
		return
	}

	sections := parseOllySections(string(reportOut))
	sendSSE(w, flusher, "olly_done", ollyDoneData{Sections: sections})

	// Step 3: Build report
	verdict, confidence := deriveVerdict(qd.Hits, sections["2"])
	report := huntReport{
		Verdict:    verdict,
		Confidence: confidence,
		Title:      deriveThreatTitle(verdict, qd.Hits, name),
		Subtitle:   sections["2"],
		Stats: huntStats{
			Hits:         fmt.Sprintf("%d", qd.Hits),
			Hosts:        fmt.Sprintf("%d", qd.Hosts),
			AttackWindow: window,
		},
		Findings:      extractFindings(sections, severity),
		Actions:       extractActions(sections["10"]),
		AlertDef:      parseAlertDef(sections["11"], name, techniqueId),
		RunDurationMs: time.Since(start).Milliseconds(),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	if h.cache != nil {
		b, _ := json.Marshal(report)
		h.cache.SetString(ctx, "hunt_result:"+huntID, string(b), time.Hour)
	}

	sendSSE(w, flusher, "report_ready", report)
}

// ── Parse helpers ─────────────────────────────────────────────────────────────

func parseLogsOutput(raw []byte, cxCmd string) queryDoneData {
	qd := queryDoneData{CxCommand: cxCmd, LastSeen: "unknown"}
	var events []json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil || len(events) == 0 {
		return qd
	}
	qd.Hits = len(events)
	qd.rawEvents = events

	seen := make(map[string]bool)
	for _, rawEvent := range events {
		var e struct {
			M struct {
				Timestamp string `json:"timestamp"`
			} `json:"$m"`
			L struct {
				AppName string `json:"applicationname"`
			} `json:"$l"`
		}
		if err := json.Unmarshal(rawEvent, &e); err != nil {
			continue
		}
		ts := e.M.Timestamp
		if ts != "" {
			qd.LastSeen = ts
		}
		host := e.L.AppName
		qd.SampleEvents = append(qd.SampleEvents, huntLogEvent{
			Timestamp: ts,
			Host:      host,
		})
		if host != "" {
			seen[host] = true
		}
	}
	qd.Hosts = len(seen)
	return qd
}

func formatSampleEvents(rawEvents []json.RawMessage) string {
	if len(rawEvents) == 0 {
		return ""
	}
	limit := 5
	if len(rawEvents) < limit {
		limit = len(rawEvents)
	}
	var sb strings.Builder
	for _, e := range rawEvents[:limit] {
		var buf bytes.Buffer
		if err := json.Compact(&buf, e); err == nil {
			sb.WriteString(buf.String())
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func deriveThreatTitle(verdict string, hits int, name string) string {
	switch verdict {
	case "threat":
		return fmt.Sprintf("Active threat confirmed — %d hits for %q", hits, name)
	case "suspicious":
		return fmt.Sprintf("%d suspicious events detected for %q", hits, name)
	default:
		return fmt.Sprintf("No threats found for %q", name)
	}
}

func extractFindings(sections map[string]string, severity string) []huntFinding {
	var findings []huntFinding
	for _, src := range []string{"1", "7", "9"} {
		text := sections[src]
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
				level := "info"
				if severity == "critical" || severity == "high" {
					level = "critical"
				} else if severity == "medium" {
					level = "warning"
				}
				findings = append(findings, huntFinding{
					Text:     strings.TrimLeft(line, "-* "),
					Severity: level,
				})
			}
		}
	}
	return findings
}

func extractActions(section11 string) []huntAction {
	var actions []huntAction
	priority := 1
	for _, line := range strings.Split(section11, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") || numberedListRe.MatchString(line) {
			level := "info"
			lower := strings.ToLower(line)
			if strings.Contains(lower, "immediately") || strings.Contains(lower, "critical") || strings.Contains(lower, "isolate") {
				level = "critical"
			} else if strings.Contains(lower, "review") || strings.Contains(lower, "investigate") {
				level = "warning"
			}
			text := strings.TrimLeft(line, "-*0123456789. ")
			actions = append(actions, huntAction{
				Priority:    priority,
				Title:       truncateString(text, 80),
				Description: text,
				Level:       level,
			})
			priority++
		}
	}
	return actions
}

func parseAlertDef(section12, name, techniqueId string) huntAlertDef {
	_ = techniqueId // reserved for future tagging; keeps signature stable
	ad := huntAlertDef{Name: name, Type: "standard", Condition: "count > 0", Severity: "high", GroupBy: "host.name"}
	for _, line := range strings.Split(section12, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "name":
			ad.Name = val
		case "type":
			ad.Type = val
		case "condition":
			ad.Condition = val
		case "severity":
			ad.Severity = val
		case "group-by", "group_by", "groupby":
			ad.GroupBy = val
		}
	}
	return ad
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── HandleHuntExport ──────────────────────────────────────────────────────────

func (h *Handler) HandleHuntExport(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing required param: id")
		return
	}

	if h.cache == nil {
		writeError(w, http.StatusNotFound, "hunt result not found or expired")
		return
	}

	raw, ok := h.cache.GetString(r.Context(), "hunt_result:"+id)
	if !ok {
		writeError(w, http.StatusNotFound, "hunt result not found or expired")
		return
	}

	var report huntReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode hunt result")
		return
	}

	md := serializeReportToMarkdown(report)
	filename := fmt.Sprintf("hunt-%s.md", sanitizeFilename(report.Title))

	w.Header().Set("Content-Type", "text/markdown")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	fmt.Fprint(w, md)
}

func serializeReportToMarkdown(r huntReport) string {
	verdictLabel := map[string]string{
		"threat":     "THREAT DETECTED",
		"suspicious": "SUSPICIOUS ACTIVITY",
		"clean":      "NO THREATS FOUND",
	}[r.Verdict]

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Hunt Report — %s\n\n", r.Title)
	fmt.Fprintf(&sb, "**Verdict:** %s  \n", verdictLabel)
	fmt.Fprintf(&sb, "**Confidence:** %s  \n", r.Confidence)
	fmt.Fprintf(&sb, "**Timestamp:** %s  \n", r.Timestamp)
	fmt.Fprintf(&sb, "**Duration:** %dms\n\n", r.RunDurationMs)
	fmt.Fprintf(&sb, "---\n\n")
	fmt.Fprintf(&sb, "## Summary\n\n%s\n\n", r.Subtitle)

	if len(r.Findings) > 0 {
		fmt.Fprintf(&sb, "## Key Findings\n\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&sb, "- [%s] %s\n", strings.ToUpper(f.Severity), f.Text)
		}
		fmt.Fprintf(&sb, "\n")
	}

	if len(r.Actions) > 0 {
		fmt.Fprintf(&sb, "## Immediate Actions\n\n")
		for _, a := range r.Actions {
			fmt.Fprintf(&sb, "%d. **[%s]** %s\n", a.Priority, strings.ToUpper(a.Level), a.Description)
		}
		fmt.Fprintf(&sb, "\n")
	}

	fmt.Fprintf(&sb, "## Alert Definition Skeleton\n\n")
	fmt.Fprintf(&sb, "| Field | Value |\n|-------|-------|\n")
	fmt.Fprintf(&sb, "| Name | %s |\n", r.AlertDef.Name)
	fmt.Fprintf(&sb, "| Type | %s |\n", r.AlertDef.Type)
	fmt.Fprintf(&sb, "| Condition | %s |\n", r.AlertDef.Condition)
	fmt.Fprintf(&sb, "| Severity | %s |\n", r.AlertDef.Severity)
	fmt.Fprintf(&sb, "| Group By | %s |\n", r.AlertDef.GroupBy)

	return sb.String()
}

var filenameRe = regexp.MustCompile(`[^a-zA-Z0-9\-_]`)

func sanitizeFilename(s string) string {
	result := filenameRe.ReplaceAllString(strings.ReplaceAll(s, " ", "-"), "")
	if len(result) > 80 {
		return result[:80]
	}
	return result
}
