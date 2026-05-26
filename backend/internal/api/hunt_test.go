package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildOllyPrompt(t *testing.T) {
	prompt, err := buildOllyPrompt(`event_type:"cmd_exec"`, 47, "host1 svc-deploy 2024-01-01T10:00:00 cmd -enc abc\nhost2 admin 2024-01-01T10:05:00 cmd -enc xyz")
	if err != nil {
		t.Fatalf("buildOllyPrompt: %v", err)
	}
	if !strings.Contains(prompt, `event_type:"cmd_exec"`) {
		t.Error("prompt missing lucene query")
	}
	if !strings.Contains(prompt, "47") {
		t.Error("prompt missing hit count")
	}
	if !strings.Contains(prompt, "svc-deploy") {
		t.Error("prompt missing sample events")
	}
}

func TestExtractTotalFromPass1(t *testing.T) {
	tests := []struct {
		input   string
		wantN   int
		wantOK  bool
	}{
		{"Total: 47", 47, true},
		{"some text\nTotal: 1234\nmore text", 1234, true},
		{"total: 99", 99, true},
		{"Total: 0", 0, true},     // zero is a valid found result
		{"no total here", 0, false},
		{"Total:", 0, false},
		{"| Total: 5 |", 0, false}, // must not match inside a table cell
	}
	for _, tc := range tests {
		gotN, gotOK := extractTotalFromPass1(tc.input)
		if gotN != tc.wantN || gotOK != tc.wantOK {
			t.Errorf("extractTotalFromPass1(%q) = (%d, %v), want (%d, %v)",
				tc.input, gotN, gotOK, tc.wantN, tc.wantOK)
		}
	}
}

// TestExtractTotalFromPass1_ZeroUpdatesHits verifies that when Olly reports
// "Total: 0", qd.Hits is set to 0 (not left at whatever parseLogsOutput set).
func TestExtractTotalFromPass1_ZeroUpdatesHits(t *testing.T) {
	// Simulate parseLogsOutput having found 3 sample events (e.g. cx logs returned
	// 3 rows) but Olly's authoritative count query says 0 matches.
	qd := queryDoneData{Hits: 3}
	if total, ok := extractTotalFromPass1("Total: 0"); ok {
		qd.Hits = total
	}
	if qd.Hits != 0 {
		t.Errorf("qd.Hits = %d after Total: 0, want 0", qd.Hits)
	}
}

func TestBuildOllySchemaPrompt(t *testing.T) {
	prompt, err := buildOllySchemaPrompt(`EventID:13 AND TargetObject:*IFEO*`, 3, "event1\nevent2", "7d")
	if err != nil {
		t.Fatalf("buildOllySchemaPrompt: %v", err)
	}
	if !strings.Contains(prompt, `EventID:13 AND TargetObject:*IFEO*`) {
		t.Error("prompt missing lucene query")
	}
	if !strings.Contains(prompt, "3") {
		t.Error("prompt missing sample count")
	}
	if !strings.Contains(prompt, "7d") {
		t.Error("prompt missing window")
	}
	if !strings.Contains(prompt, `"Total: N"`) {
		t.Error(`prompt missing "Total: N" format instruction`)
	}
	if strings.Contains(prompt, "webhook.site") {
		t.Error("prompt must not be webhook-specific")
	}
}

func TestBuildOllyReportPrompt(t *testing.T) {
	prompt := buildOllyReportPrompt()
	for _, section := range []string{
		"## 1.", "## 2.", "## 3.", "## 4.", "## 5.",
		"## 6.", "## 7.", "## 8.", "## 9.", "## 10.", "## 11.",
	} {
		if !strings.Contains(prompt, section) {
			t.Errorf("report prompt missing section %q", section)
		}
	}
	if strings.Contains(prompt, "## 12.") {
		t.Error("report prompt must not have section 12 (only 11 sections now)")
	}
	if !strings.Contains(prompt, "What We Found") {
		t.Error("report prompt section 1 must be 'What We Found'")
	}
	if strings.Contains(prompt, "Hunt Workflow") {
		t.Error("report prompt must not contain 'Hunt Workflow' section")
	}
	if !strings.Contains(prompt, "RUN") {
		t.Error("report prompt must instruct Olly to RUN the confirmed query")
	}
}

func TestParseOllySections(t *testing.T) {
	raw := `## §1 Hunt Summary
Severity: High
Confidence: High

## §2 Original Query
event_type:"cmd_exec"

## §3 Schema Mapping
| field | cx_path | app | gaps |
|-------|---------|-----|------|
| event_type | log.type | auth | none |
`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 missing")
	}
	if !strings.Contains(sections["1"], "Severity: High") {
		t.Errorf("section 1 content wrong: %q", sections["1"])
	}
	if sections["2"] == "" {
		t.Error("section 2 missing")
	}
	if sections["3"] == "" {
		t.Error("section 3 missing")
	}
}

func TestParseOllySectionsNativeFormat(t *testing.T) {
	raw := `Chat ID: abc-123

## Summary
Analysis of geo-anomaly query. 29 events matched from RU/CN.

## Findings
- No matching fields in ECS schema.
- Salesforce LoginEvent has CountryIso field that matches.

## Next Steps
- Pivot on username and source IP.
- Check if Azure AD logs are ingested.
`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 (Summary) missing")
	}
	if !strings.Contains(sections["1"], "29 events") {
		t.Errorf("section 1 content wrong: %q", sections["1"])
	}
	if !strings.Contains(sections["7"], "Salesforce") {
		t.Errorf("section 7 (Findings) wrong: %q", sections["7"])
	}
	if !strings.Contains(sections["11"], "Pivot") {
		t.Errorf("section 11 (Next Steps) wrong: %q", sections["11"])
	}
}

func TestParseOllySectionsNativeFormatNoHeaders(t *testing.T) {
	raw := `Chat ID: abc-123

Analysis with no structured headers.`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 fallback missing")
	}
	if strings.Contains(sections["1"], "Chat ID:") {
		t.Error("Chat ID line should be stripped from section 1 fallback")
	}
}

func TestParseOllySectionsNumberedFormat(t *testing.T) {
	raw := `## 1. Hunt Summary
Objective: detect IFEO persistence. Severity: High.

## 2. Original Query
` + "```lucene\nEventID:13 AND TargetObject:*IFEO*\n```" + `

## 6. Detection Logic Explained
Sysmon Event ID 13 captures registry value set operations.

## 11. Recommended Follow-up Hunts
- T1547.001 Run Keys`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 should be populated from numbered format")
	}
	if sections["2"] == "" {
		t.Error("section 2 should be populated from numbered format")
	}
	if sections["6"] == "" {
		t.Error("section 6 should be populated from numbered format")
	}
	if sections["11"] == "" {
		t.Error("section 11 should be populated from numbered format")
	}
	if !strings.Contains(sections["1"], "IFEO") {
		t.Errorf("section 1 content wrong: %q", sections["1"])
	}
}

func TestParseOllySectionsBoldFormat(t *testing.T) {
	raw := `## **1. What We Found**
Total hits: 42. Top user: alice@corp.com

## **2. Hunt Summary**
Severity: High
Confidence: High

## **7. Detection Logic**
Detects encoded command execution.

## **11. Alert Definition**
Name: Encoded Command Exec
Type: standard`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 missing in bold format")
	}
	if !strings.Contains(sections["1"], "Total hits") {
		t.Errorf("section 1 content wrong: %q", sections["1"])
	}
	if sections["2"] == "" {
		t.Error("section 2 missing in bold format")
	}
	if !strings.Contains(sections["2"], "Severity: High") {
		t.Errorf("section 2 content wrong: %q", sections["2"])
	}
	if sections["7"] == "" {
		t.Error("section 7 missing in bold format")
	}
	if sections["11"] == "" {
		t.Error("section 11 missing in bold format")
	}
}

func TestParseAgentsOutput_ExtractsChatID(t *testing.T) {
	input := []byte(`Creating new chat...
Sending message...
[1]{chat_id,interaction_id,status,response,interaction_mode,model_choice}:
  "3fe8d46b-ea69-4701-808d-43ac5c21ba55","eda6fe6e-b596-4ed8-a7ed-55436436f5ad",completed,"## Summary\nField found: $d.url",focus,"gpt-5.2"
`)
	chatID, response := parseAgentsOutput(input)
	if chatID != "3fe8d46b-ea69-4701-808d-43ac5c21ba55" {
		t.Errorf("chatID = %q, want 3fe8d46b-ea69-4701-808d-43ac5c21ba55", chatID)
	}
	if !strings.Contains(response, "$d.url") {
		t.Errorf("response missing content: %q", response)
	}
}

func TestParseAgentsOutput_EmptyOnBadInput(t *testing.T) {
	chatID, response := parseAgentsOutput([]byte("Error: API request failed (500)"))
	if chatID != "" {
		t.Errorf("chatID should be empty, got %q", chatID)
	}
	if response != "" {
		t.Errorf("response should be empty, got %q", response)
	}
}

func TestParseAgentsOutput_TextFallback(t *testing.T) {
	// When output is plain text (non-agents format), both should be empty.
	chatID, response := parseAgentsOutput([]byte("Chat ID: abc-123\n\n## Summary\nsome analysis"))
	if chatID != "" {
		t.Errorf("chatID should be empty for text format, got %q", chatID)
	}
	if response != "" {
		t.Errorf("response should be empty for text format, got %q", response)
	}
}

func TestSanitizeQuery(t *testing.T) {
	valid := []string{
		`event_type:"cmd_exec" AND cmd:"-EncodedCommand"`,
		`source.ip:10.0.0.1 AND user:admin`,
		`kubernetes.pod:frontend-*`,
		`user.risk_score:>70 OR source.geo.country_iso_code:<10`, // comparison operators are safe with argv exec
		`$m.severity:"high" AND $l.subsystemName:"auth"`,         // DataPrime field references
	}
	for _, q := range valid {
		if err := sanitizeQuery(q); err != nil {
			t.Errorf("sanitizeQuery(%q) = %v, want nil", q, err)
		}
	}

	invalid := []string{
		`event_type:cmd $(whoami)`,
		"event_type:cmd `id`",
		`event_type:cmd; rm -rf /`,
		`event_type:cmd | cat /etc/passwd`,
		`event_type:cmd` + "\ninjected",
		string(make([]byte, 1001)),          // over length limit (null bytes — caught by allowlist)
		strings.Repeat(" ", 1001),           // 1001 printable ASCII chars — tests length guard exclusively
		`event_type:cmd\injected`,           // backslash
		`event_type:cmd & id`,               // background exec
		`event_type:cmd $(cat /etc/passwd)`, // subshell via $()
		`event_type:cmd ${PATH}`,            // variable expansion via ${}
	}
	for _, q := range invalid {
		if err := sanitizeQuery(q); err == nil {
			t.Errorf("sanitizeQuery(%q) = nil, want error", q)
		}
	}
}

func TestDeriveVerdict(t *testing.T) {
	tests := []struct {
		hits        int
		section1    string
		wantVerdict string
		wantConf    string
	}{
		{0, "Severity: High\nConfidence: High", "clean", "high"},
		{5, "Severity: Low\nConfidence: Low", "suspicious", "low"},
		{5, "Severity: Medium\nConfidence: Medium", "suspicious", "medium"},
		{10, "Severity: High\nConfidence: High", "threat", "high"},
		{10, "Severity: Critical\nConfidence: Medium", "threat", "medium"},
		{3, "Severity: High\nConfidence: Low", "suspicious", "low"},
	}
	for _, tc := range tests {
		v, c := deriveVerdict(tc.hits, tc.section1)
		if v != tc.wantVerdict {
			t.Errorf("hits=%d sect=%q: verdict=%q want %q", tc.hits, tc.section1, v, tc.wantVerdict)
		}
		if c != tc.wantConf {
			t.Errorf("hits=%d sect=%q: conf=%q want %q", tc.hits, tc.section1, c, tc.wantConf)
		}
	}
}

func TestParseLogsOutputBareArray(t *testing.T) {
	raw := []byte(`[{"$m":{"timestamp":"2024-01-01T10:00:00"},"$l":{"applicationname":"host1"},"$d":{"user":"u1"}},{"$m":{"timestamp":"2024-01-01T10:05:00"},"$l":{"applicationname":"host2"},"$d":{"user":"u2"}}]`)
	qd := parseLogsOutput(raw, "cx logs test")
	if qd.Hits != 2 {
		t.Errorf("Hits = %d, want 2", qd.Hits)
	}
	if qd.Hosts != 2 {
		t.Errorf("Hosts = %d, want 2", qd.Hosts)
	}
	if qd.LastSeen != "2024-01-01T10:05:00" {
		t.Errorf("LastSeen = %q, want 2024-01-01T10:05:00", qd.LastSeen)
	}
}

func TestParseLogsOutputEmptyArray(t *testing.T) {
	qd := parseLogsOutput([]byte("[]"), "cx logs test")
	if qd.Hits != 0 {
		t.Errorf("Hits = %d, want 0 for empty array", qd.Hits)
	}
}

type mockCxExecutor struct {
	logsOutput      []byte
	logsErr         error
	schemaOutput    []byte
	schemaErr       error
	reportOutput    []byte
	reportErr       error
	capturedChatID  string // records chatID passed to runOllyReport
}

func (m *mockCxExecutor) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	return m.logsOutput, m.logsErr
}

func (m *mockCxExecutor) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
	return m.schemaOutput, m.schemaErr
}

func (m *mockCxExecutor) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	m.capturedChatID = chatID
	return m.reportOutput, m.reportErr
}

func TestMockCxExecutorInterface(t *testing.T) {
	// Verifies mockCxExecutor satisfies the cxExecutor interface at compile time.
	var _ cxExecutor = &mockCxExecutor{}
}

func TestHandleHuntStream_MissingQuery(t *testing.T) {
	h := &Handler{cxBinPath: "/usr/local/bin/cx"}
	req := httptest.NewRequest(http.MethodGet, "/api/hunt/stream", nil)
	w := httptest.NewRecorder()
	h.HandleHuntStream(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleHuntStream_InvalidQuery(t *testing.T) {
	h := &Handler{cxBinPath: "/usr/local/bin/cx"}
	req := httptest.NewRequest(http.MethodGet, `/api/hunt/stream?lucene=bad$(cmd)&window=5m`, nil)
	w := httptest.NewRecorder()
	h.HandleHuntStream(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event, got: %q", body)
	}
}

func TestHandleHuntStream_MockSuccess(t *testing.T) {
	schemaAgentsOut := []byte(`Creating new chat...
Sending message...
[1]{chat_id,interaction_id,status,response,interaction_mode,model_choice}:
  "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","ffffffff-0000-1111-2222-333333333333",completed,"## Summary\n$d.url confirmed, 1 hit",focus,"gpt-5.2"
`)
	reportOut := []byte(`## 1. What We Found
Total hits: 2. Top user: u1@corp.com. Time: clustered within 5 minutes.

## 2. Hunt Summary
Severity: High
Confidence: High
MITRE: Execution / T1059

## 3. Original Query
event_type:"cmd_exec"

## 4. Schema Mapping
| field | cx | exists |
|-------|----|--------|
| event_type | $d.event_type | Y |

## 5. Translated Query — DataPrime
source logs | filter $d.event_type == 'cmd_exec'

## 6. Translated Query — Lucene
$d.event_type:"cmd_exec"

## 7. Detection Logic
Detects encoded command execution which is a common indicator of attacker tradecraft.

## 8. False Positive Sources
none

## 9. Visibility Gaps
none

## 10. Follow-up Hunts
hunt 1: PowerShell download cradle

## 11. Alert Definition
Name: test-alert
Type: standard
Condition: count > 0
Severity: High
Group-By: $d.suser`)

	mock := &mockCxExecutor{
		logsOutput:   []byte(`[{"$m":{"timestamp":"2024-01-01T10:00:00"},"$l":{"applicationname":"h1"},"$d":{"cmd":"enc"}}]`),
		schemaOutput: schemaAgentsOut,
		reportOutput: reportOut,
	}
	h := &Handler{cxBinPath: "/usr/local/bin/cx", cxExec: mock}

	req := httptest.NewRequest(http.MethodGet, `/api/hunt/stream?lucene=event_type%3Acmd_exec&window=5m&name=Test+Hunt&severity=high&techniqueId=T1059`, nil)
	w := httptest.NewRecorder()
	h.HandleHuntStream(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: stream_opened") {
		t.Errorf("missing stream_opened: %s", body)
	}
	if !strings.Contains(body, "event: query_done") {
		t.Errorf("missing query_done: %s", body)
	}
	if !strings.Contains(body, "event: olly_done") {
		t.Errorf("missing olly_done: %s", body)
	}
	if !strings.Contains(body, "event: report_ready") {
		t.Errorf("missing report_ready: %s", body)
	}
	// Verify pass-1 chat_id was extracted and forwarded to pass-2 runOllyReport.
	const wantChatID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if mock.capturedChatID != wantChatID {
		t.Errorf("runOllyReport chatID = %q, want %q", mock.capturedChatID, wantChatID)
	}
}

func TestHandleHuntExport_NotFound(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/hunt/export?id=nonexistent-id", nil)
	w := httptest.NewRecorder()
	h.HandleHuntExport(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOllyModelConstants(t *testing.T) {
	if ollyPass1Model == "" {
		t.Error("ollyPass1Model must not be empty")
	}
	if ollyPass2Model == "" {
		t.Error("ollyPass2Model must not be empty")
	}
	if ollyPass1Model == ollyPass2Model {
		t.Error("pass1 and pass2 models must differ")
	}
}

func TestDetectLogSource(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		// AWS
		{`eventSource:"cloudtrail.amazonaws.com"`, "aws"},
		{`eventName:"CreateBucket" AND awsRegion:us-east-1`, "aws"},
		{`errorCode:"AccessDenied" AND recipientAccountId:*`, "aws"},
		// GCP
		{`protoPayload.methodName:"storage.objects.delete"`, "gcp"},
		{`resource.type:"gcs_bucket"`, "gcp"},
		// Azure
		{`operationName:"Microsoft.Authorization/roleAssignments/write"`, "azure"},
		{`signinlogs AND resultType:"Failure"`, "azure"},
		// Okta
		{`target.type:"AppInstance" AND outcome.result:"FAILURE"`, "okta"},
		{`actor.type:"User" AND auth0`, "okta"},
		// CrowdStrike
		{`event.Technique:"T1059" AND crowdstrike`, "crowdstrike"},
		// Palo Alto
		{`PaloAlto.threat_name:* AND PaloAlto.severity:"high"`, "paloalto"},
		{`panw AND ngfw`, "paloalto"},
		// Fortinet
		{`action:"deny" AND policyname:* AND fortinet`, "fortinet"},
		// Cloudflare
		{`Action:"block" AND RuleID:* AND cloudflare`, "cloudflare"},
		// M365
		{`Operation:"MailboxLogin" AND Workload:"Exchange"`, "m365"},
		{`UserId:* AND office365`, "m365"},
		// Google Workspace
		{`events.name:"login_failure" AND workspace`, "gworkspace"},
		// GitHub
		{`repo:* AND org:* AND github`, "github"},
		// SentinelOne
		{`event.DetectId:* AND sentinelone`, "sentinelone"},
		// Generic fallback
		{`event_type:"cmd_exec" AND user:admin`, "generic"},
		{`kubernetes.pod:frontend-*`, "generic"},
	}
	for _, tc := range tests {
		got := detectLogSource(tc.query)
		if got != tc.want {
			t.Errorf("detectLogSource(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestFormatSourceFields(t *testing.T) {
	// aws should return a non-empty string with known fields
	out := formatSourceFields("aws")
	if out == "" {
		t.Error("formatSourceFields(aws) should not be empty")
	}
	if !strings.Contains(out, "errorCode") {
		t.Errorf("formatSourceFields(aws) missing errorCode: %q", out)
	}
	// generic should still return something
	if formatSourceFields("generic") == "" {
		t.Error("formatSourceFields(generic) should not be empty")
	}
}
