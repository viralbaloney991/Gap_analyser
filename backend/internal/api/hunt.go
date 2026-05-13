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

// ── Olly prompt ───────────────────────────────────────────────────────────────

// ollyPromptTemplate is intentionally concise. Olly runs live Coralogix queries
// as part of its response, so long prompts cause Cloudflare 524 timeouts (>5 min).
// We ask for Olly's native 3-section format and parse it in parseOllySections.
const ollyPromptTemplate = `Threat hunt analysis request.

Query: {{.LuceneQuery}}
Hits in window: {{.HitCount}}
{{if .SampleEvents}}Sample events:
{{.SampleEvents}}
{{end}}
Please:
1. Check if this query matches actual log fields in this environment and suggest corrections if needed.
2. Summarise what threat behaviour it detects and the risk level (Critical/High/Medium/Low).
3. List 2-3 recommended follow-up pivot queries or investigation steps.`

type ollyPromptData struct {
	LuceneQuery  string
	HitCount     int
	SampleEvents string
}

var ollyTmpl = template.Must(template.New("olly").Parse(ollyPromptTemplate))

func buildOllyPrompt(luceneQuery string, hitCount int, sampleEvents string) (string, error) {
	var buf bytes.Buffer
	err := ollyTmpl.Execute(&buf, ollyPromptData{
		LuceneQuery:  luceneQuery,
		HitCount:     hitCount,
		SampleEvents: sampleEvents,
	})
	if err != nil {
		return "", fmt.Errorf("render olly prompt: %w", err)
	}
	return buf.String(), nil
}

// ── Section parser ────────────────────────────────────────────────────────────

var sectionHeaderRe = regexp.MustCompile(`(?m)^##\s+§(\d+)\s+`)
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
	sections := make(map[string]string, 12)

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

type cxExecutor interface {
	runLogs(ctx context.Context, query, window string) ([]byte, error)
	runOllyChat(ctx context.Context, prompt string) ([]byte, error)
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
		"--start", "now-"+window, "--output", "json", "--limit", "50")
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllyChat(ctx context.Context, prompt string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask", prompt)
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

const huntTimeout = 6 * time.Minute

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
	cxCmd := fmt.Sprintf("cx logs '%s' --start now-%s --output json --limit 50", lucene, window)
	logsOut, err := cx.runLogs(ctx, lucene, window)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "cx_logs_failed", Message: err.Error()})
		return
	}

	qd := parseLogsOutput(logsOut, cxCmd)
	sendSSE(w, flusher, "query_done", qd)

	// Step 2: Olly analysis
	sampleText := formatSampleEvents(qd.SampleEvents)
	prompt, err := buildOllyPrompt(lucene, qd.Hits, sampleText)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "prompt_build_failed", Message: err.Error()})
		return
	}

	ollyOut, err := cx.runOllyChat(ctx, prompt)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
		return
	}

	sections := parseOllySections(string(ollyOut))
	sendSSE(w, flusher, "olly_done", ollyDoneData{Sections: sections})

	// Step 3: Build report
	verdict, confidence := deriveVerdict(qd.Hits, sections["1"])
	report := huntReport{
		Verdict:    verdict,
		Confidence: confidence,
		Title:      deriveThreatTitle(verdict, qd.Hits, name),
		Subtitle:   sections["1"],
		Stats: huntStats{
			Hits:         fmt.Sprintf("%d", qd.Hits),
			Hosts:        fmt.Sprintf("%d", qd.Hosts),
			AttackWindow: window,
		},
		Findings:      extractFindings(sections, severity),
		Actions:       extractActions(sections["11"]),
		AlertDef:      parseAlertDef(sections["12"], name, techniqueId),
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
	var parsed struct {
		Hits   int `json:"hits"`
		Events []struct {
			Timestamp string `json:"timestamp"`
			Host      string `json:"host"`
			User      string `json:"user"`
			Cmd       string `json:"cmd"`
		} `json:"events"`
	}
	qd := queryDoneData{CxCommand: cxCmd, LastSeen: "unknown"}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		qd.Hits = len(lines)
		return qd
	}
	qd.Hits = parsed.Hits
	seen := make(map[string]bool)
	users := make(map[string]bool)
	for _, e := range parsed.Events {
		qd.SampleEvents = append(qd.SampleEvents, huntLogEvent{
			Timestamp: e.Timestamp,
			Host:      e.Host,
			User:      e.User,
			Command:   e.Cmd,
		})
		seen[e.Host] = true
		if e.User != "" {
			users[e.User] = true
		}
		if e.Timestamp != "" {
			qd.LastSeen = e.Timestamp
		}
	}
	qd.Hosts = len(seen)
	qd.UniqueUsers = len(users)
	return qd
}

func formatSampleEvents(events []huntLogEvent) string {
	var sb strings.Builder
	for _, e := range events {
		fmt.Fprintf(&sb, "%s  %s  %s  %s\n", e.Timestamp, e.Host, e.User, e.Command)
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
	for _, src := range []string{"1", "6", "10"} {
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
