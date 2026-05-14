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
		input string
		want  int
	}{
		{"Total: 47", 47},
		{"some text\nTotal: 1234\nmore text", 1234},
		{"total: 99", 99},
		{"Total: 0", 0},
		{"no total here", 0},
		{"Total:", 0},
	}
	for _, tc := range tests {
		got := extractTotalFromPass1(tc.input)
		if got != tc.want {
			t.Errorf("extractTotalFromPass1(%q) = %d, want %d", tc.input, got, tc.want)
		}
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
		"## 6.", "## 7.", "## 8.", "## 9.", "## 10.", "## 11.", "## 12.",
	} {
		if !strings.Contains(prompt, section) {
			t.Errorf("report prompt missing section %q", section)
		}
	}
	if !strings.Contains(prompt, "Pivot Investigation") {
		t.Error("report prompt missing §8 pivot instruction")
	}
	if !strings.Contains(prompt, "RUN") {
		t.Error("report prompt must tell Olly to RUN pivot queries, not just suggest them")
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
	if !strings.Contains(sections["6"], "Salesforce") {
		t.Errorf("section 6 (Findings) wrong: %q", sections["6"])
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
	// Olly actually emits ## **1. Title** (bold) — verify the parser handles it.
	raw := `## **1. Hunt Summary**
Severity: High
Confidence: High

## **2. Original Query**
event_type:"cmd_exec"

## **6. Detection Logic Explained**
Detects encoded command execution.

## **12. Alert Definition**
Name: Encoded Command Exec
Type: standard`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 missing in bold format")
	}
	if !strings.Contains(sections["1"], "Severity: High") {
		t.Errorf("section 1 content wrong: %q", sections["1"])
	}
	if sections["2"] == "" {
		t.Error("section 2 missing in bold format")
	}
	if sections["6"] == "" {
		t.Error("section 6 missing in bold format")
	}
	if sections["12"] == "" {
		t.Error("section 12 missing in bold format")
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
	reportOut := []byte(`## 1. Hunt Summary
Severity: High
Confidence: High

## 2. Original Query
event_type:"cmd_exec"

## 3. Schema Mapping
| field | cx | exists |
|-------|----|--------|
| url | $d.url | Y |

## 4. Translated Query — DataPrime
source logs | filter $d.url ~ 'webhook.site'

## 5. Translated Query — Lucene
$d.url:*webhook.site*

## 6. Detection Logic Explained
detects webhook exfil

## 7. Hunt Workflow
check hits

## 8. Pivot Investigation
webhook.site: 1 hit

## 9. False Positive Considerations
none

## 10. Visibility Gaps
none

## 11. Follow-up Hunts
hunt 1: cloud storage

## 12. Alert Definition
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
