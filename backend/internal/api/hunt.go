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
// Two-pass design: pass 1 (gpt-5.4, skill, ~72s) discovers real field names
// via 4 targeted questions and returns a chat_id. Pass 2 (claude-sonnet-4-6,
// skill, --chat-id, ~216s) continues the same chat and produces the full
// 11-section report with live pivot findings.
// All analysis stays inside Coralogix infrastructure.

// ── Log source detection ──────────────────────────────────────────────────────

// detectLogSource returns a short identifier for the cloud/vendor log source
// inferred from keyword patterns in the Lucene query.
func detectLogSource(luceneQuery string) string {
	blob := strings.ToLower(luceneQuery)
	// Google Workspace — must precede GCP ("google" matches both)
	if containsAny(blob, "google workspace", "gsuite", "g-suite", "gmail-admin",
		"google-workspace", "workspace") {
		return "gworkspace"
	}
	if containsAny(blob, "cloudtrail", "guardduty", "vpc flow", "vpcflow",
		"aws", "s3 bucket", "aws iam", "aws eks", "lambda", "ec2",
		"rds snapshot", "aws waf", "recipientaccountid", "eventsource") {
		return "aws"
	}
	if containsAny(blob, "gcp", "cloudaudit", "google cloud", "stackdriver",
		"gke", "bigquery", "pubsub", "cloud audit", "protopayload", "gcs_bucket") {
		return "gcp"
	}
	if containsAny(blob, "azure", "signinlogs", "auditlogs", "entra",
		"microsoft azure", "aad", "azuread", "microsoft.authorization") {
		return "azure"
	}
	if containsAny(blob, "okta", "auth0", "outcome.result") {
		return "okta"
	}
	if containsAny(blob, "crowdstrike", "falcon", "cs-falcon") {
		return "crowdstrike"
	}
	if containsAny(blob, "paloalto", "palo alto", "panorama", "pan-os",
		"panw", "ngfw", "cortex xdr") {
		return "paloalto"
	}
	if containsAny(blob, "fortinet", "fortigate", "fortiweb", "fortisiem",
		"fortianalyzer", "forti") {
		return "fortinet"
	}
	if containsAny(blob, "cloudflare", "cf-ray", "cloudflare waf",
		"cloudflare access") {
		return "cloudflare"
	}
	if containsAny(blob, "m365", "office365", "office 365", "microsoft 365",
		"exchange online", "sharepoint", "msgraph", "microsoft-365", "defender for",
		"workload:") {
		return "m365"
	}
	if containsAny(blob, "github", "gitlab") {
		return "github"
	}
	if containsAny(blob, "sentinelone", "sentinel one", "s1-") {
		return "sentinelone"
	}
	return "generic"
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// _rawSupplementaryFields maps log source identifier → source-specific fields
// that complement cx_security.* normalization.
var _rawSupplementaryFields = map[string][]string{
	"aws": {
		"userIdentity.type              — role vs. user vs. service vs. assumed-role",
		"userIdentity.arn               — full ARN of the caller",
		"errorCode                      — e.g. AccessDenied, NoSuchBucket",
		"errorMessage                   — human-readable failure reason",
		"requestParameters.bucketName   — S3 target (or equivalent resource param)",
		"awsRegion                      — region where the API call was made",
		"recipientAccountId             — target account (cross-account activity)",
	},
	"gcp": {
		"protoPayload.methodName        — full RPC method name",
		"protoPayload.serviceName       — GCP service (storage.googleapis.com, etc.)",
		"protoPayload.status.code       — gRPC status code (0 = OK)",
		"resource.type                  — GCP resource type (gcs_bucket, etc.)",
		"resource.labels.project_id     — GCP project",
	},
	"azure": {
		"operationName                  — Azure operation",
		"resultType                     — Success / Failure",
		"resultDescription              — detailed failure reason",
		"resourceId                     — full ARM resource path",
	},
	"okta": {
		"target.displayName             — target app, group, or user acted upon",
		"outcome.result                 — SUCCESS, FAILURE, SKIPPED, ALLOW, DENY",
		"outcome.reason                 — detailed outcome reason",
		"debugContext.debugData.url     — request URL",
	},
	"crowdstrike": {
		"event.UserName                 — actor username",
		"event.ComputerName             — affected host",
		"event.RemoteAddressIP4         — source IP",
		"event.DetectId                 — detection identifier",
		"event.Technique                — MITRE ATT&CK technique name",
		"event.SeverityName             — severity (Critical, High, Medium, Low)",
		"event.PatternDispositionDescription — disposition (prevented, detected, etc.)",
	},
	"paloalto": {
		"PaloAlto.rule                  — firewall policy rule name",
		"PaloAlto.app                   — application identified",
		"PaloAlto.destination_port      — destination port",
		"PaloAlto.threat_name           — threat signature name",
		"PaloAlto.severity              — threat severity",
	},
	"fortinet": {
		"srcip                          — source IP address",
		"dstip                          — destination IP address",
		"action                         — allow / deny / close",
		"policyname                     — firewall policy name",
		"user                           — authenticated username",
		"msg                            — event description",
	},
	"cloudflare": {
		"ClientIP                       — client IP address",
		"ClientRequestUserAgent         — user agent string",
		"ClientCountry                  — client country code",
		"ClientASNDescription           — client ASN org",
		"Action                         — firewall action (block, allow, challenge)",
		"RuleID                         — firewall rule that matched",
	},
	"m365": {
		"UserId                         — actor UPN (user@domain.com)",
		"ClientIP                       — client IP address",
		"Operation                      — action type (FileDownloaded, MailboxLogin)",
		"ObjectId                       — target object (file path, mailbox)",
		"ResultStatus                   — Succeeded / Failed",
		"Workload                       — M365 workload (Exchange, SharePoint, Teams)",
	},
	"gworkspace": {
		"actor.callerType               — HUMAN vs. KEY (service account)",
		"events.name                    — specific event action",
		"events.type                    — event category (LOGIN, DRIVE, etc.)",
		"ipAddress                      — raw IP from Google's log",
	},
	"github": {
		"repo                           — repository name",
		"org                            — organization",
		"action                         — specific action (push, pull_request)",
		"data.ref                       — branch / tag reference",
		"programmatic_access_type       — token type (OAuth App, GitHub App)",
	},
	"sentinelone": {
		"event.DetectId                 — detection identifier",
		"event.SeverityName             — severity label",
		"event.category                 — detection category",
		"event.Technique                — MITRE ATT&CK technique",
		"event.PatternDispositionDescription — disposition (killed, quarantined)",
	},
	"generic": {
		"Any high-cardinality identity field (user, account, email, UPN)",
		"Any IP address field not mapped to cx_security.source_ip",
		"Any action / operation / method field",
		"Any outcome / result / status field",
	},
}

// formatSourceFields returns the supplementary fields for the given log source
// as a newline-joined string for prompt injection.
func formatSourceFields(source string) string {
	fields, ok := _rawSupplementaryFields[source]
	if !ok {
		fields = _rawSupplementaryFields["generic"]
	}
	var sb strings.Builder
	for _, f := range fields {
		sb.WriteString("  - ")
		sb.WriteString(f)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

const ollySchemaPromptConcisePrefix = "Be concise and structured. Output only what's asked. Use compact tables. No preamble."

const ollySchemaPromptTemplate = `{{.ConcisePrefix}}

Schema discovery for threat hunt. Answer ONLY these 4 questions:

Query: {{.LuceneQuery}}
Sample events ({{.SampleCount}} retrieved from last {{.Window}}):
{{.SampleEvents}}

Known normalized fields (cx_security.* — prefer these in DataPrime translation if confirmed present):
  - cx_security.username, cx_security.email, cx_security.target_username, cx_security.target_email
  - cx_security.source_ip, cx_security.destination_ip, cx_security.source_hostname
  - cx_security.userAgent, cx_security.event_name, cx_security.event_result
  - cx_security.event_type, cx_security.resource
  - GeoIP: cx_security.source_ip_geoip.asn.number, cx_security.source_ip_geoip.asn.organization, cx_security.source_ip_geoip.country_name

Log source detected: {{.LogSource}}
Source-specific supplementary fields (use if cx_security.* fields are absent):
{{.SourceFields}}

1. Field mapping: for each key term in the Lucene query, find the matching DataPrime $d path
   in the sample events. Which confirmed paths have data?

2. Pattern match: translate the Lucene query to DataPrime using the confirmed fields from
   question 1. Run it. Do the specific IOCs/patterns actually appear? How many events match?

3. True total: run source logs | filter <your DataPrime translation> | count() as total
   Report the exact number on its own line as: "Total: N"

4. Top patterns: source logs | filter <condition> | groupby <most relevant field> aggregate
   count() as hits | orderby hits desc | limit 5

Facts only.`

type ollySchemaPromptData struct {
	ConcisePrefix string
	LuceneQuery   string
	SampleCount   int
	SampleEvents  string
	Window        string
	LogSource     string
	SourceFields  string
}

var ollySchemaTmpl = template.Must(template.New("ollySchema").Parse(ollySchemaPromptTemplate))

func buildOllySchemaPrompt(luceneQuery string, sampleCount int, sampleEvents, window string) (string, error) {
	source := detectLogSource(luceneQuery)
	var buf bytes.Buffer
	err := ollySchemaTmpl.Execute(&buf, ollySchemaPromptData{
		ConcisePrefix: ollySchemaPromptConcisePrefix,
		LuceneQuery:   luceneQuery,
		SampleCount:   sampleCount,
		SampleEvents:  sampleEvents,
		Window:        window,
		LogSource:     source,
		SourceFields:  formatSourceFields(source),
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

var totalCountRe = regexp.MustCompile(`(?im)^Total:\s*(\d+)`)

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
	"findings":     "7",
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

// ollyPass1Model is used for Pass 1 (schema discovery — 4 focused questions).
// gpt-5.4 is the recommended Pro-mode model; faster for structured short output.
const ollyPass1Model = "gpt-5.4"

// ollyPass2Model is used for Pass 2 (full 11-section report).
// claude-sonnet-4-6 has stronger long-form analytical reasoning than 4-5.
const ollyPass2Model = "claude-sonnet-4-6"

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
	cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask",
		"--output", "agents", "--mode", "skill", "--model", ollyPass1Model,
		"--timeout", "300", "--read-only", prompt)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	args := []string{"olly", "ask", "--mode", "skill", "--model", ollyPass2Model,
		"--timeout", "900", "--read-only"}
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

// recoveryWaits defines the sleep durations between recovery ping attempts.
// 45s → 60s → 75s → 90s → 105s, mirroring the olly.py baselining system.
var recoveryWaits = []time.Duration{
	45 * time.Second,
	60 * time.Second,
	75 * time.Second,
	90 * time.Second,
	105 * time.Second,
}

const recoveryPing = "Are you done? Return your best answer so far in the requested format. Be concise."

// runWithRecovery calls cx.runOllyReport for Pass 2. On error (Cloudflare 524 or
// subprocess timeout), if chatID is non-empty it polls the same chat with a short
// recovery ping on a progressive back-off schedule (up to maxRetries attempts).
// If chatID is empty, it returns the error immediately — there is no chat to recover.
func runWithRecovery(ctx context.Context, cx cxExecutor, chatID, prompt string, maxRetries int) ([]byte, error) {
	out, err := cx.runOllyReport(ctx, chatID, prompt)
	if err == nil && len(out) > 0 {
		return out, nil
	}

	if chatID == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("olly report: empty response")
	}

	lastErr := err
	if lastErr == nil {
		lastErr = fmt.Errorf("olly report: empty response")
	}

	waits := recoveryWaits
	if maxRetries < len(waits) {
		waits = waits[:maxRetries]
	}

	for attempt, wait := range waits {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		rec, rerr := cx.runOllyReport(ctx, chatID, recoveryPing)
		if rerr == nil && len(rec) > 0 {
			return rec, nil
		}
		if rerr != nil {
			lastErr = rerr
		}
		_ = attempt
	}

	return nil, fmt.Errorf("olly report timed out after %d recovery attempts: %w", len(waits), lastErr)
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

	// Step 2a: Pass 1 — schema discovery (gpt-5.4, skill, ~72s)
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

	// Step 2b: Pass 2 — full 11-section report (claude-sonnet-4-6, skill, ~216s)
	// Continues the same chat (chat_id from pass 1) so Olly uses confirmed field names.
	// On timeout, polls the same chat with a recovery ping (up to 5 attempts).
	reportOut, err := runWithRecovery(ctx, cx, chatID, buildOllyReportPrompt(), 5)
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

func extractActions(section10 string) []huntAction {
	var actions []huntAction
	priority := 1
	for _, line := range strings.Split(section10, "\n") {
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

func parseAlertDef(section11, name, techniqueId string) huntAlertDef {
	_ = techniqueId // reserved for future tagging; keeps signature stable
	ad := huntAlertDef{Name: name, Type: "standard", Condition: "count > 0", Severity: "high", GroupBy: "host.name"}
	for _, line := range strings.Split(section11, "\n") {
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
