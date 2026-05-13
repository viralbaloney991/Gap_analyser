# CX CLI Hunt Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Hunt button to each Detection Builder suggestion that streams a two-step cx CLI investigation (log query + Olly AI analysis) to a dedicated hunt page, ending with a structured Hunt Report deliverable.

**Architecture:** SSE streaming from `GET /api/hunt/stream`; Go backend shells out to `cx logs` then `cx olly chat` with a structured 12-section prompt; frontend EventSource fills skeleton sections progressively and reveals Hunt Report card last.

**Tech Stack:** Go (`exec.CommandContext`, `http.Flusher`), React + TypeScript (`EventSource`, History API), Redis (`hunt_result:<id>` TTL 1h), cx CLI (`cx logs`, `cx olly chat`), Inter + JetBrains Mono fonts.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/internal/api/hunt.go` | Create | All hunt logic: structs, sanitizer, Olly prompt, section parser, verdict deriver, cx executor, SSE handler, export handler |
| `backend/internal/api/hunt_test.go` | Create | Unit tests for sanitizeQuery, buildOllyPrompt, parseOllySections, deriveVerdict; mock executor for handler |
| `backend/cmd/server/main.go` | Modify | Read `CX_BIN_PATH` env var, pass to Handler, register `/api/hunt/stream` and `/api/hunt/export` |
| `backend/internal/api/handlers.go` | Modify | Add `cxBinPath string` field to Handler struct; include in NewHandler |
| `frontend/src/types/index.ts` | Modify | Add HuntPayload, HuntVerdict, HuntQueryResult, HuntLogEvent, OllySection, HuntReport, HuntFinding, HuntAction, HuntAlertDef |
| `frontend/src/services/api.ts` | Modify | Add `openHuntStream(detection: HuntPayload): EventSource` |
| `frontend/src/components/HuntView.tsx` | Create | Full hunt page: stepper, skeleton sections, EventSource connection, progressive reveal, Hunt Report card |
| `frontend/src/components/HuntView.css` | Create | Styles for hunt page — shimmer skeletons, stepper, accordions, verdict card, deliverable card |
| `frontend/src/components/DetectionBuilder.tsx` | Modify | Add Hunt button to each FlowAlert card in generated panel; accept `onHunt` prop |
| `frontend/src/App.tsx` | Modify | Add `'hunt'` to View type, `huntDetection` state, render `<HuntView>`, pass `onHunt` to DetectionBuilder |

---

## Task 1: Backend data models and input sanitization

**Files:**
- Create: `backend/internal/api/hunt.go`
- Create: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write failing tests for sanitizeQuery**

```go
// backend/internal/api/hunt_test.go
package api

import (
	"testing"
)

func TestSanitizeQuery(t *testing.T) {
	valid := []string{
		`event_type:"cmd_exec" AND cmd:"-EncodedCommand"`,
		`source.ip:10.0.0.1 AND user:admin`,
		`kubernetes.pod:frontend-*`,
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
		string(make([]byte, 1001)), // over length limit
	}
	for _, q := range invalid {
		if err := sanitizeQuery(q); err == nil {
			t.Errorf("sanitizeQuery(%q) = nil, want error", q)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestSanitizeQuery -v
```
Expected: FAIL — `sanitizeQuery` not defined.

- [ ] **Step 3: Write the hunt.go file with structs and sanitizeQuery**

```go
// backend/internal/api/hunt.go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

type huntReport struct {
	Verdict       string       `json:"verdict"`
	Confidence    string       `json:"confidence"`
	Title         string       `json:"title"`
	Subtitle      string       `json:"subtitle"`
	Stats         huntStats    `json:"stats"`
	Findings      []huntFinding `json:"findings"`
	Actions       []huntAction `json:"actions"`
	AlertDef      huntAlertDef `json:"alert_def"`
	RunDurationMs int64        `json:"run_duration_ms"`
	Timestamp     string       `json:"timestamp"`
}

type huntStats struct {
	Hits         string `json:"hits"`
	Hosts        string `json:"hosts"`
	AttackWindow string `json:"attack_window"`
	C2Flagged    bool   `json:"c2_flagged"`
}

type huntErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Input sanitization ───────────────────────────────────────────────────────

var queryAllowlist = regexp.MustCompile(`^[\x20-\x7E]+$`)
var queryForbidden = regexp.MustCompile(`[$` + "`" + `;\|\n\r]`)

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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestSanitizeQuery -v
```
Expected: PASS for all 6 valid cases, error for all 6 invalid cases.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): data models and query sanitization"
```

---

## Task 2: Olly prompt template and section parser

**Files:**
- Modify: `backend/internal/api/hunt.go`
- Modify: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write failing test for buildOllyPrompt and parseOllySections**

Add to `hunt_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run "TestBuildOllyPrompt|TestParseOllySections" -v
```
Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement buildOllyPrompt and parseOllySections in hunt.go**

Add after the `sanitizeQuery` function:

```go
// ── Olly prompt ───────────────────────────────────────────────────────────────

const ollyPromptTemplate = `You are a Coralogix threat-hunting specialist. Analyse the detection query and log results below and return a structured threat-hunting report.

Your response MUST follow this exact structure with section headers:

## §1 Hunt Summary
[Provide: Severity (Critical/High/Medium/Low), Confidence (High/Medium/Low), Search Window, Hunt Objective, MITRE ATT&CK Technique/Tactic]

## §2 Original Query
[Echo the exact Lucene query provided]

## §3 Schema Mapping
[Table with columns: Original Field | CX Log Path | Application/Subsystem | Gaps]

## §4 Translated Query — DataPrime
[The query translated to Coralogix DataPrime syntax]

## §5 Translated Query — Lucene
[Optimised Lucene query for Coralogix Explore]

## §6 Detection Logic Explained
[Plain-language explanation of what the query detects and why]

## §7 Hunt Workflow
[Step-by-step manual investigation workflow for a tier-2 analyst]

## §8 Suggested Aggregation / Pivot Query
[DataPrime or Lucene aggregation query to reveal attack patterns]

## §9 False Positive Considerations
[List likely false positive sources with suppression suggestions]

## §10 Visibility Gaps & Assumptions
[List missing log sources, fields, or coverage gaps that limit this hunt]

## §11 Recommended Follow-up Hunts
[3-5 related hunts with brief description and query sketch]

## §12 Alert Definition Skeleton
[Key-value block: Name, Type, Condition, Severity, Group-by fields]

---

## QUERY TO HUNT
{{.LuceneQuery}}

## LOG RESULTS (cx logs output)
Total hits: {{.HitCount}}
Sample events:
{{.SampleEvents}}`

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

func parseOllySections(output string) map[string]string {
	matches := sectionHeaderRe.FindAllStringIndex(output, -1)
	nums := sectionHeaderRe.FindAllStringSubmatch(output, -1)
	sections := make(map[string]string, 12)
	for i, loc := range matches {
		start := loc[1] // after the header
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(output)
		}
		content := strings.TrimSpace(output[start:end])
		sections[nums[i][1]] = content
	}
	return sections
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run "TestBuildOllyPrompt|TestParseOllySections" -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): Olly prompt template and 12-section parser"
```

---

## Task 3: Verdict derivation

**Files:**
- Modify: `backend/internal/api/hunt.go`
- Modify: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write failing test for deriveVerdict**

Add to `hunt_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestDeriveVerdict -v
```
Expected: FAIL.

- [ ] **Step 3: Implement deriveVerdict in hunt.go**

Add after `parseOllySections`:

```go
// ── Verdict derivation ────────────────────────────────────────────────────────

var severityRe = regexp.MustCompile(`(?i)Severity:\s*(\w+)`)
var confidenceRe = regexp.MustCompile(`(?i)Confidence:\s*(\w+)`)

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

	highSev := sev == "high" || sev == "critical"
	highConf := conf == "high"

	if highSev || highConf {
		return "threat", conf
	}
	return "suspicious", conf
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestDeriveVerdict -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): verdict derivation from hit count and Olly §1"
```

---

## Task 4: cx executor interface and implementation

**Files:**
- Modify: `backend/internal/api/hunt.go`
- Modify: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write failing test for cxRunner using mock**

Add to `hunt_test.go`:

```go
type mockCxExecutor struct {
	logsOutput  []byte
	logsErr     error
	ollyOutput  []byte
	ollyErr     error
}

func (m *mockCxExecutor) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	return m.logsOutput, m.logsErr
}

func (m *mockCxExecutor) runOllyChat(ctx context.Context, prompt string) ([]byte, error) {
	return m.ollyOutput, m.ollyErr
}

func TestMockCxExecutorInterface(t *testing.T) {
	// Verifies mockCxExecutor satisfies the cxExecutor interface at compile time.
	var _ cxExecutor = &mockCxExecutor{}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestMockCxExecutorInterface -v
```
Expected: FAIL — `cxExecutor` interface not defined.

- [ ] **Step 3: Implement cxExecutor interface and cxRunner in hunt.go**

Add after `deriveVerdict`:

```go
// ── cx executor ────────────────────────────────────────────────────────────────

type cxExecutor interface {
	runLogs(ctx context.Context, query, window string) ([]byte, error)
	runOllyChat(ctx context.Context, prompt string) ([]byte, error)
}

type cxRunner struct {
	binPath string
}

const maxOutputBytes = 64 * 1024 // 64 KB

func (r *cxRunner) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "logs", query,
		"--start", "now-"+window, "--output", "json", "--limit", "50")
	return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllyChat(ctx context.Context, prompt string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "olly", "chat", prompt)
	return readCapped(cmd, maxOutputBytes)
}

func readCapped(cmd *exec.Cmd, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	cmd.Stdout = io.LimitReader(&buf, limit)
	cmd.Stderr = io.Discard
	// Capture stderr separately for error context
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
	limited, err := io.ReadAll(io.LimitReader(pr, limit))
	pr.Close()

	if werr := cmd.Wait(); werr != nil {
		return nil, fmt.Errorf("cx exit: %w; stderr: %s", werr, stderr.String())
	}
	_ = err
	return limited, nil
}
```

Note: this uses `os.Pipe` — add `"os"` to the import block in hunt.go.

- [ ] **Step 4: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestMockCxExecutorInterface -v
go build ./...
```
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): cx executor interface with output capping"
```

---

## Task 5: SSE stream handler

**Files:**
- Modify: `backend/internal/api/hunt.go`
- Modify: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write failing handler test**

Add to `hunt_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
	"strings"
)

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
	// Should return SSE error event
	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event, got: %q", body)
	}
}

func TestHandleHuntStream_MockSuccess(t *testing.T) {
	mock := &mockCxExecutor{
		logsOutput: []byte(`{"hits":2,"events":[{"timestamp":"2024-01-01","host":"h1","user":"u1","cmd":"enc"}]}`),
		ollyOutput: []byte("## §1 Hunt Summary\nSeverity: High\nConfidence: High\n\n## §2 Original Query\ntest\n\n## §3 Schema Mapping\nnone\n\n## §4 Translated Query — DataPrime\nsource logs\n\n## §5 Translated Query — Lucene\nevent_type:cmd_exec\n\n## §6 Detection Logic Explained\ndetects\n\n## §7 Hunt Workflow\ncheck\n\n## §8 Suggested Aggregation / Pivot Query\nagg\n\n## §9 False Positive Considerations\nnone\n\n## §10 Visibility Gaps & Assumptions\nnone\n\n## §11 Recommended Follow-up Hunts\nnone\n\n## §12 Alert Definition Skeleton\nName: test"),
	}
	h := &Handler{cxBinPath: "/usr/local/bin/cx", cxExec: mock}

	req := httptest.NewRequest(http.MethodGet, `/api/hunt/stream?lucene=event_type%3Acmd_exec&window=5m&name=Test+Hunt&severity=high&techniqueId=T1059&tacticId=execution&source=syslog`, nil)
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
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run "TestHandleHuntStream" -v
```
Expected: FAIL — `HandleHuntStream` not defined, `cxBinPath` not in Handler.

- [ ] **Step 3: Add cxBinPath and cxExec fields to Handler struct**

In `backend/internal/api/handlers.go`, add two fields to Handler:

```go
type Handler struct {
	config         *config.Config
	mondayClient   *monday.Client
	cache          *cache.Store
	alertStore     *store.Store
	prewarmWorker  *prewarm.Worker
	prewarmCancels sync.Map
	sem            *pipeline.Semaphore
	cxBinPath      string     // path to cx binary; empty means cx not configured
	cxExec         cxExecutor // nil → real cxRunner; non-nil → injected (tests)
}
```

- [ ] **Step 4: Implement HandleHuntStream in hunt.go**

Add after `readCapped`:

```go
// ── SSE helpers ───────────────────────────────────────────────────────────────

func sendSSE(w http.ResponseWriter, f http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	f.Flush()
}

// ── HandleHuntStream ──────────────────────────────────────────────────────────

const huntTimeout = 45 * time.Second

func (h *Handler) HandleHuntStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Parse query params
	q := r.URL.Query()
	lucene := q.Get("lucene")
	window := q.Get("window")
	if window == "" {
		window = "30d"
	}
	name := q.Get("name")
	severity := q.Get("severity")
	techniqueId := q.Get("techniqueId")

	if lucene == "" {
		writeError(w, http.StatusBadRequest, "missing required param: lucene")
		return
	}

	// Set SSE headers
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

	ctx, cancel := context.WithTimeout(r.Context(), huntTimeout)
	defer cancel()

	start := time.Now()

	// Choose executor
	exec := h.cxExec
	if exec == nil {
		exec = &cxRunner{binPath: h.cxBinPath}
	}

	// ── Step 1: cx logs ──────────────────────────────────────────────────────
	cxCmd := fmt.Sprintf("cx logs '%s' --start now-%s --output json --limit 50", lucene, window)
	logsOut, err := exec.runLogs(ctx, lucene, window)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "cx_logs_failed", Message: err.Error()})
		return
	}

	qd := parseLogsOutput(logsOut, cxCmd)
	sendSSE(w, flusher, "query_done", qd)

	// ── Step 2: Olly analysis ─────────────────────────────────────────────────
	sampleText := formatSampleEvents(qd.SampleEvents)
	prompt, err := buildOllyPrompt(lucene, qd.Hits, sampleText)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "prompt_build_failed", Message: err.Error()})
		return
	}

	ollyOut, err := exec.runOllyChat(ctx, prompt)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
		return
	}

	sections := parseOllySections(string(ollyOut))
	sendSSE(w, flusher, "olly_done", ollyDoneData{Sections: sections})

	// ── Step 3: Build report ──────────────────────────────────────────────────
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

	// Persist for export
	if h.cache != nil {
		b, _ := json.Marshal(report)
		h.cache.SetString(ctx, "hunt_result:"+huntID, string(b), time.Hour)
	}

	sendSSE(w, flusher, "report_ready", report)
}

// ── Helper functions ──────────────────────────────────────────────────────────

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
		// cx logs may return plain text; use line count as hit estimate
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		qd.Hits = len(lines)
		return qd
	}
	qd.Hits = parsed.Hits
	seen := make(map[string]bool)
	for _, e := range parsed.Events {
		qd.SampleEvents = append(qd.SampleEvents, huntLogEvent{
			Timestamp: e.Timestamp,
			Host:      e.Host,
			User:      e.User,
			Command:   e.Cmd,
		})
		seen[e.Host] = true
		if e.Timestamp != "" {
			qd.LastSeen = e.Timestamp
		}
	}
	qd.Hosts = len(seen)
	qd.UniqueUsers = countUniqueUsers(parsed.Events)
	return qd
}

func countUniqueUsers(events []struct {
	Timestamp string `json:"timestamp"`
	Host      string `json:"host"`
	User      string `json:"user"`
	Cmd       string `json:"cmd"`
}) int {
	seen := make(map[string]bool)
	for _, e := range events {
		if e.User != "" {
			seen[e.User] = true
		}
	}
	return len(seen)
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
		lines := strings.Split(text, "\n")
		for _, line := range lines {
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
	lines := strings.Split(section11, "\n")
	priority := 1
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") || (len(line) > 2 && line[1] == '.') {
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
	ad := huntAlertDef{Name: name, Type: "standard", Condition: "count > 0", Severity: "high", GroupBy: "host.name"}
	lines := strings.Split(section12, "\n")
	for _, line := range lines {
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
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run "TestHandleHuntStream" -v
go build ./...
```
Expected: all 3 handler tests PASS, clean build.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/handlers.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): SSE stream handler with full cx execution pipeline"
```

---

## Task 6: Export handler and route registration

**Files:**
- Modify: `backend/internal/api/hunt.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing test for HandleHuntExport**

Add to `hunt_test.go`:

```go
func TestHandleHuntExport_NotFound(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/hunt/export?id=nonexistent-id", nil)
	w := httptest.NewRecorder()
	h.HandleHuntExport(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run TestHandleHuntExport_NotFound -v
```
Expected: FAIL.

- [ ] **Step 3: Implement HandleHuntExport and markdown serializer**

Add to `hunt.go`:

```go
// ── HandleHuntExport ──────────────────────────────────────────────────────────

func (h *Handler) HandleHuntExport(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing required param: id")
		return
	}

	if h.cache == nil {
		writeError(w, http.StatusServiceUnavailable, "cache unavailable")
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
	var sb strings.Builder
	verdictLabel := map[string]string{
		"threat":    "THREAT DETECTED",
		"suspicious": "SUSPICIOUS ACTIVITY",
		"clean":     "NO THREATS FOUND",
	}[r.Verdict]

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

func sanitizeFilename(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	result := re.ReplaceAllString(strings.ReplaceAll(s, " ", "-"), "")
	if len(result) > 80 {
		return result[:80]
	}
	return result
}
```

- [ ] **Step 4: Register routes and CX_BIN_PATH in main.go**

In `backend/cmd/server/main.go`, after the `handler` is created (line ~104):

```go
// Read CX_BIN_PATH for hunt feature.
handler.SetCxBinPath(os.Getenv("CX_BIN_PATH"))
```

Add to mux registrations (after `/api/mitre-catalog`):
```go
mux.HandleFunc("/api/hunt/stream", handler.HandleHuntStream)
mux.HandleFunc("/api/hunt/export", handler.HandleHuntExport)
```

Add to `handlers.go` — a setter method:
```go
// SetCxBinPath configures the cx binary path for the hunt feature.
func (h *Handler) SetCxBinPath(path string) {
	h.cxBinPath = path
}
```

- [ ] **Step 5: Run tests and build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -run "TestHandleHuntExport" -v
go build ./...
```
Expected: export test PASS, clean build.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/handlers.go backend/cmd/server/main.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): export endpoint and route registration"
```

---

## Task 7: Frontend TypeScript types

**Files:**
- Modify: `frontend/src/types/index.ts`

- [ ] **Step 1: Append hunt types to index.ts**

At the end of `frontend/src/types/index.ts`, add:

```typescript
// ── Hunt feature ─────────────────────────────────────────────────────────────

export interface HuntPayload {
  detectionId: string
  name: string
  logic: string
  techniqueId: string
  tacticId: string
  window: string
  source: string
  severity: string
}

export type HuntVerdict = 'clean' | 'suspicious' | 'threat'

export interface HuntLogEvent {
  timestamp: string
  host: string
  user: string
  command: string
}

export interface HuntQueryResult {
  hits: number
  hosts: number
  last_seen: string
  unique_users: number
  sample_events: HuntLogEvent[]
  cx_command: string
}

export interface HuntReport {
  verdict: HuntVerdict
  confidence: 'low' | 'medium' | 'high'
  title: string
  subtitle: string
  stats: {
    hits: string
    hosts: string
    attack_window: string
    c2_flagged: boolean
  }
  findings: HuntFinding[]
  actions: HuntAction[]
  alert_def: HuntAlertDef
  run_duration_ms: number
  timestamp: string
}

export interface HuntFinding {
  text: string
  severity: 'critical' | 'warning' | 'info'
}

export interface HuntAction {
  priority: number
  title: string
  description: string
  level: 'critical' | 'warning' | 'info'
}

export interface HuntAlertDef {
  name: string
  type: string
  condition: string
  severity: string
  group_by: string
}
```

- [ ] **Step 2: Run TypeScript check**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
npx tsc --noEmit
```
Expected: 0 errors.

- [ ] **Step 3: Add openHuntStream to api.ts**

Add at the end of `frontend/src/services/api.ts`:

```typescript
export function openHuntStream(detection: import('../types').HuntPayload): EventSource {
  const params = new URLSearchParams({
    lucene:      detection.logic,
    window:      detection.window,
    name:        detection.name,
    severity:    detection.severity,
    techniqueId: detection.techniqueId,
    tacticId:    detection.tacticId,
    source:      detection.source,
  });
  return new EventSource(`${API_BASE}/api/hunt/stream?${params.toString()}`);
}

export async function exportHuntReport(huntId: string): Promise<void> {
  const url = `${API_BASE}/api/hunt/export?id=${encodeURIComponent(huntId)}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Export failed: ${res.statusText}`);
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = res.headers.get('Content-Disposition')?.match(/filename="(.+)"/)?.[1] ?? 'hunt-report.md';
  a.click();
  URL.revokeObjectURL(a.href);
}
```

- [ ] **Step 4: TypeScript check**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
npx tsc --noEmit
```
Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/services/api.ts
git commit -m "feat(hunt): TypeScript types and api.ts openHuntStream"
```

---

## Task 8: HuntView component

**Files:**
- Create: `frontend/src/components/HuntView.tsx`
- Create: `frontend/src/components/HuntView.css`

- [ ] **Step 1: Create HuntView.css**

```css
/* frontend/src/components/HuntView.css */
.hunt-page {
  display: flex;
  flex-direction: column;
  gap: 0;
  max-width: 900px;
  margin: 0 auto;
  padding: 24px 24px 64px;
  font-family: 'Inter', sans-serif;
}

/* ── Back nav ── */
.hunt-back {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #6E7B8B;
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  margin-bottom: 20px;
}
.hunt-back:hover { color: #E6EDF3; }

/* ── Page title ── */
.hunt-title { font-size: 20px; font-weight: 700; color: #E6EDF3; margin-bottom: 8px; }
.hunt-meta { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 28px; }
.hunt-badge {
  display: inline-flex; align-items: center;
  font-size: 11px; font-weight: 600; padding: 2px 8px;
  border-radius: 4px; border: 1px solid;
  font-family: 'JetBrains Mono', monospace;
}
.hunt-badge-sev-critical { color: #EF4444; background: rgba(239,68,68,.1); border-color: rgba(239,68,68,.3); }
.hunt-badge-sev-high { color: #F97316; background: rgba(249,115,22,.1); border-color: rgba(249,115,22,.3); }
.hunt-badge-sev-medium { color: #F59E0B; background: rgba(245,158,11,.1); border-color: rgba(245,158,11,.3); }
.hunt-badge-sev-low { color: #3B82F6; background: rgba(59,130,246,.1); border-color: rgba(59,130,246,.3); }
.hunt-badge-default { color: #6E7B8B; background: rgba(110,123,139,.1); border-color: rgba(110,123,139,.2); }

/* ── Stepper ── */
.hunt-stepper {
  display: flex; align-items: center; gap: 0;
  margin-bottom: 32px; padding: 16px 20px;
  background: #0E1223; border: 1px solid #1E2A3A; border-radius: 10px;
}
.hunt-step {
  display: flex; align-items: center; gap: 8px; flex: 1;
  font-size: 12px; font-weight: 500;
}
.hunt-step-icon {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 700; flex-shrink: 0;
}
.hunt-step-pending { background: #1E2A3A; color: #6E7B8B; }
.hunt-step-active { background: rgba(59,130,246,.2); border: 2px solid #3B82F6; color: #3B82F6; animation: pulse-dot 1.5s ease-in-out infinite; }
.hunt-step-done { background: rgba(34,197,94,.15); color: #22C55E; }
.hunt-step-label { color: #6E7B8B; }
.hunt-step-label.active { color: #E6EDF3; }
.hunt-step-connector { width: 40px; height: 1px; background: #1E2A3A; flex-shrink: 0; }
@keyframes pulse-dot { 0%,100% { box-shadow: 0 0 0 0 rgba(59,130,246,.4); } 50% { box-shadow: 0 0 0 6px rgba(59,130,246,0); } }

/* ── Section cards ── */
.hunt-section {
  background: #0E1223; border: 1px solid #1E2A3A; border-radius: 10px;
  margin-bottom: 16px; overflow: hidden;
}
.hunt-section-header {
  display: flex; align-items: center; gap: 10px;
  padding: 14px 18px; border-bottom: 1px solid #1E2A3A;
}
.hunt-section-num {
  width: 22px; height: 22px; border-radius: 50%;
  background: #1E2A3A; color: #6E7B8B;
  font-size: 10px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
}
.hunt-section-title { font-size: 13px; font-weight: 600; color: #E6EDF3; }
.hunt-section-body { padding: 16px 18px; }

/* ── Shimmer ── */
@keyframes shimmer { 0% { background-position: -400px 0; } 100% { background-position: 400px 0; } }
.shimmer {
  background: linear-gradient(90deg, #1E2A3A 25%, #253347 50%, #1E2A3A 75%);
  background-size: 400px 100%;
  animation: shimmer 1.6s linear infinite;
  border-radius: 4px;
}
.shimmer-line { height: 12px; margin-bottom: 8px; }
.shimmer-line.w-full { width: 100%; }
.shimmer-line.w-3q { width: 75%; }
.shimmer-line.w-half { width: 50%; }
.shimmer-box { height: 56px; border-radius: 6px; }
.shimmer-row { height: 32px; margin-bottom: 6px; border-radius: 4px; }
.shimmer-stat-row { display: flex; gap: 8px; margin-bottom: 12px; }
.shimmer-stat { flex: 1; height: 64px; border-radius: 6px; }
.shimmer-accordion { height: 40px; margin-bottom: 6px; border-radius: 6px; }

/* ── Query section ── */
.hunt-cmd-block {
  font-family: 'JetBrains Mono', monospace; font-size: 11px;
  background: #0A0E1A; border: 1px solid #1E2A3A; border-radius: 6px;
  padding: 10px 14px; color: #F59E0B; margin-bottom: 14px;
  overflow-x: auto; white-space: pre;
}
.hunt-stat-row { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 14px; }
.hunt-stat { flex: 1; min-width: 100px; background: rgba(0,0,0,.25); border: 1px solid rgba(255,255,255,.06); border-radius: 7px; padding: 10px 14px; }
.hunt-stat-val { font-family: 'JetBrains Mono', monospace; font-size: 18px; font-weight: 700; color: #E6EDF3; }
.hunt-stat-lbl { font-size: 10px; color: #6E7B8B; margin-top: 2px; }
.hunt-log-table { width: 100%; border-collapse: collapse; font-size: 11px; font-family: 'JetBrains Mono', monospace; }
.hunt-log-table th { text-align: left; color: #6E7B8B; padding: 6px 8px; border-bottom: 1px solid #1E2A3A; font-size: 10px; text-transform: uppercase; }
.hunt-log-table td { padding: 6px 8px; color: #A0ADB8; border-bottom: 1px solid rgba(255,255,255,.04); }

/* ── Olly accordions ── */
.olly-accordion { border: 1px solid #1E2A3A; border-radius: 6px; margin-bottom: 6px; overflow: hidden; }
.olly-acc-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 14px; cursor: pointer; font-size: 12px; font-weight: 500;
  color: #A0ADB8; background: #0A0E1A; user-select: none;
}
.olly-acc-header:hover { color: #E6EDF3; }
.olly-acc-header.expanded { color: #E6EDF3; border-bottom: 1px solid #1E2A3A; }
.olly-acc-num { font-family: 'JetBrains Mono', monospace; font-size: 10px; color: #3B82F6; margin-right: 8px; }
.olly-acc-body { padding: 12px 14px; font-size: 12px; color: #A0ADB8; line-height: 1.6; white-space: pre-wrap; background: #0A0E1A; }
.olly-gap-alert { background: rgba(239,68,68,.08); border: 1px solid rgba(239,68,68,.3); border-radius: 6px; padding: 10px 12px; margin-top: 8px; font-size: 12px; color: #EF4444; }

/* ── Hunt Report (deliverable) ── */
.hunt-report {
  border-radius: 12px; overflow: hidden;
  border: 1px solid rgba(255,255,255,.08);
  margin-top: 8px;
  animation: fadein 400ms ease;
}
@keyframes fadein { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }
.report-banner {
  padding: 20px 24px;
  display: flex; align-items: flex-start; gap: 16px;
}
.report-banner-threat { background: rgba(239,68,68,.08); border-bottom: 1px solid rgba(239,68,68,.2); }
.report-banner-suspicious { background: rgba(245,158,11,.08); border-bottom: 1px solid rgba(245,158,11,.2); }
.report-banner-clean { background: rgba(34,197,94,.08); border-bottom: 1px solid rgba(34,197,94,.2); }
.report-icon { width: 44px; height: 44px; border-radius: 10px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.report-icon-threat { background: rgba(239,68,68,.18); }
.report-icon-suspicious { background: rgba(245,158,11,.18); }
.report-icon-clean { background: rgba(34,197,94,.18); }
.report-banner-body { flex: 1; }
.report-verdict-label { font-size: 10px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; margin-bottom: 4px; }
.label-threat { color: #EF4444; }
.label-suspicious { color: #F59E0B; }
.label-clean { color: #22C55E; }
.report-title { font-size: 18px; font-weight: 700; color: #E6EDF3; margin-bottom: 6px; letter-spacing: -.02em; }
.report-subtitle { font-size: 12.5px; color: #6E7B8B; line-height: 1.6; margin-bottom: 12px; }
.report-stats { display: flex; gap: 10px; flex-wrap: wrap; }
.rstat { background: rgba(0,0,0,.25); border: 1px solid rgba(255,255,255,.06); border-radius: 7px; padding: 6px 12px; }
.rstat-val { font-family: 'JetBrains Mono', monospace; font-size: 14px; font-weight: 700; }
.rstat-lbl { font-size: 10px; color: #6E7B8B; }
.report-body { padding: 20px 24px; background: #0E1223; }
.report-section-title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: #6E7B8B; margin-bottom: 10px; }
.report-findings { list-style: none; padding: 0; margin-bottom: 18px; }
.report-findings li { display: flex; gap: 8px; padding: 6px 0; font-size: 12.5px; color: #A0ADB8; border-bottom: 1px solid rgba(255,255,255,.04); }
.finding-dot { width: 6px; height: 6px; border-radius: 50%; margin-top: 6px; flex-shrink: 0; }
.dot-critical { background: #EF4444; }
.dot-warning { background: #F59E0B; }
.dot-info { background: #3B82F6; }
.report-actions { margin-bottom: 18px; }
.action-item { display: flex; gap: 10px; padding: 8px 12px; border-radius: 6px; margin-bottom: 6px; font-size: 12px; }
.action-critical { background: rgba(239,68,68,.08); border: 1px solid rgba(239,68,68,.2); }
.action-warning { background: rgba(245,158,11,.08); border: 1px solid rgba(245,158,11,.2); }
.action-info { background: rgba(59,130,246,.08); border: 1px solid rgba(59,130,246,.2); }
.action-num { font-family: 'JetBrains Mono', monospace; font-weight: 700; font-size: 11px; min-width: 18px; }
.action-num-critical { color: #EF4444; }
.action-num-warning { color: #F59E0B; }
.action-num-info { color: #3B82F6; }
.action-text { color: #A0ADB8; }
.report-alert-def { background: #0A0E1A; border: 1px solid #1E2A3A; border-radius: 6px; padding: 12px; margin-bottom: 18px; }
.alert-def-grid { display: grid; grid-template-columns: 130px 1fr; gap: 6px 12px; font-size: 12px; }
.alert-def-key { color: #6E7B8B; }
.alert-def-val { color: #A0ADB8; font-family: 'JetBrains Mono', monospace; font-size: 11px; }
.report-footer { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding: 14px 24px; background: #0A0E1A; border-top: 1px solid #1E2A3A; }
.report-footer-meta { font-size: 11px; color: #6E7B8B; }
.report-actions-row { display: flex; gap: 8px; }
.hunt-btn {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 12px; font-weight: 500; padding: 7px 14px;
  border-radius: 6px; border: 1px solid; cursor: pointer; transition: opacity .15s;
}
.hunt-btn:hover { opacity: .85; }
.hunt-btn-primary { background: rgba(59,130,246,.15); border-color: rgba(59,130,246,.4); color: #60A5FA; }
.hunt-btn-secondary { background: rgba(255,255,255,.04); border-color: #1E2A3A; color: #6E7B8B; }
```

- [ ] **Step 2: Create HuntView.tsx**

```tsx
// frontend/src/components/HuntView.tsx
import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, ExternalLink, Download, ChevronDown, ChevronRight } from 'lucide-react';
import type { FlowAlert } from '../types';
import type { HuntQueryResult, HuntReport } from '../types';
import { openHuntStream, exportHuntReport } from '../services/api';
import './HuntView.css';

interface Props {
  detection: FlowAlert;
  cxRegion?: string;
  onBack: () => void;
}

type Step = 'query' | 'olly' | 'report';

const OLLY_LABELS: Record<string, string> = {
  '1': 'Hunt Summary',
  '2': 'Original Query',
  '3': 'Schema Mapping',
  '4': 'Translated Query — DataPrime',
  '5': 'Translated Query — Lucene',
  '6': 'Detection Logic Explained',
  '7': 'Hunt Workflow',
  '8': 'Suggested Aggregation / Pivot Query',
  '9': 'False Positive Considerations',
  '10': 'Visibility Gaps & Assumptions',
  '11': 'Recommended Follow-up Hunts',
  '12': 'Alert Definition Skeleton',
};

export default function HuntView({ detection, cxRegion, onBack }: Props) {
  const [activeStep, setActiveStep] = useState<Step>('query');
  const [doneSteps, setDoneSteps] = useState<Set<Step>>(new Set());
  const [queryResult, setQueryResult] = useState<HuntQueryResult | null>(null);
  const [ollySections, setOllySections] = useState<Record<string, string> | null>(null);
  const [report, setReport] = useState<HuntReport | null>(null);
  const [huntId, setHuntId] = useState<string>('');
  const [error, setError] = useState<string>('');
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set(['1']));
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const payload = {
      detectionId: `${detection.techniqueId}-${Date.now()}`,
      name: detection.name,
      logic: detection.logic,
      techniqueId: detection.techniqueId,
      tacticId: '',
      window: detection.window,
      source: detection.source,
      severity: detection.severity,
    };

    const es = openHuntStream(payload);
    esRef.current = es;

    es.addEventListener('stream_opened', (e) => {
      const data = JSON.parse((e as MessageEvent).data);
      setHuntId(data.hunt_id);
    });

    es.addEventListener('query_done', (e) => {
      const data: HuntQueryResult = JSON.parse((e as MessageEvent).data);
      setQueryResult(data);
      setDoneSteps(prev => new Set([...prev, 'query']));
      setActiveStep('olly');
    });

    es.addEventListener('olly_done', (e) => {
      const data = JSON.parse((e as MessageEvent).data);
      setOllySections(data.sections);
      setDoneSteps(prev => new Set([...prev, 'olly']));
      setActiveStep('report');
    });

    es.addEventListener('report_ready', (e) => {
      const data: HuntReport = JSON.parse((e as MessageEvent).data);
      setReport(data);
      setDoneSteps(prev => new Set([...prev, 'report']));
      es.close();
    });

    es.addEventListener('error', (e) => {
      try {
        const data = JSON.parse((e as MessageEvent).data);
        setError(data.message || 'Hunt failed');
      } catch {
        setError('Connection lost');
      }
      es.close();
    });

    return () => { es.close(); };
  }, []);

  const toggleSection = (key: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    });
  };

  const stepStatus = (step: Step) => {
    if (doneSteps.has(step)) return 'done';
    if (activeStep === step) return 'active';
    return 'pending';
  };

  const openInCoralogix = () => {
    if (!ollySections || !cxRegion) return;
    const lucene = ollySections['5'] || detection.logic;
    const url = `https://${cxRegion}.coralogix.com/#/query-new/logs?query=${encodeURIComponent(lucene)}`;
    window.open(url, '_blank', 'noopener');
  };

  const verdictColor = (v: string) => {
    if (v === 'threat') return '#EF4444';
    if (v === 'suspicious') return '#F59E0B';
    return '#22C55E';
  };

  const severityBadgeClass = (sev: string) => {
    const s = sev.toLowerCase();
    if (s === 'critical') return 'hunt-badge-sev-critical';
    if (s === 'high') return 'hunt-badge-sev-high';
    if (s === 'medium') return 'hunt-badge-sev-medium';
    if (s === 'low') return 'hunt-badge-sev-low';
    return 'hunt-badge-default';
  };

  return (
    <div className="hunt-page">
      <button className="hunt-back" onClick={onBack}>
        <ArrowLeft size={14} /> Back to Detection Builder
      </button>

      <div className="hunt-title">Hunt: {detection.name}</div>
      <div className="hunt-meta">
        <span className={`hunt-badge ${severityBadgeClass(detection.severity)}`}>{detection.severity}</span>
        <span className="hunt-badge hunt-badge-default">{detection.techniqueId}</span>
        <span className="hunt-badge hunt-badge-default">{detection.source}</span>
        <span className="hunt-badge hunt-badge-default">{detection.window}</span>
      </div>

      {/* Stepper */}
      <div className="hunt-stepper">
        {(['query', 'olly', 'report'] as Step[]).map((step, i) => {
          const status = stepStatus(step);
          const labels = { query: 'Log Query', olly: 'Olly Analysis', report: 'Hunt Report' };
          return (
            <div key={step} style={{ display: 'contents' }}>
              <div className="hunt-step">
                <div className={`hunt-step-icon hunt-step-${status}`}>
                  {status === 'done' ? '✓' : i + 1}
                </div>
                <span className={`hunt-step-label${status === 'active' ? ' active' : ''}`}>{labels[step]}</span>
              </div>
              {i < 2 && <div className="hunt-step-connector" />}
            </div>
          );
        })}
      </div>

      {error && (
        <div style={{ background: 'rgba(239,68,68,.1)', border: '1px solid rgba(239,68,68,.3)', borderRadius: 8, padding: '12px 16px', color: '#EF4444', fontSize: 13, marginBottom: 16 }}>
          Hunt failed: {error}
        </div>
      )}

      {/* Section 1: Log Query */}
      <div className="hunt-section">
        <div className="hunt-section-header">
          <div className="hunt-section-num">1</div>
          <div className="hunt-section-title">Log Query</div>
        </div>
        <div className="hunt-section-body">
          {queryResult ? (
            <>
              <div className="hunt-cmd-block">$ {queryResult.cx_command}</div>
              <div className="hunt-stat-row">
                <div className="hunt-stat">
                  <div className="hunt-stat-val" style={{ color: queryResult.hits > 0 ? '#EF4444' : '#22C55E' }}>{queryResult.hits}</div>
                  <div className="hunt-stat-lbl">total hits</div>
                </div>
                <div className="hunt-stat">
                  <div className="hunt-stat-val">{queryResult.hosts}</div>
                  <div className="hunt-stat-lbl">hosts affected</div>
                </div>
                <div className="hunt-stat">
                  <div className="hunt-stat-val">{queryResult.last_seen}</div>
                  <div className="hunt-stat-lbl">last seen</div>
                </div>
                <div className="hunt-stat">
                  <div className="hunt-stat-val">{queryResult.unique_users}</div>
                  <div className="hunt-stat-lbl">unique users</div>
                </div>
              </div>
              {queryResult.sample_events.length > 0 && (
                <table className="hunt-log-table">
                  <thead><tr><th>Time</th><th>Host</th><th>User</th><th>Command</th></tr></thead>
                  <tbody>
                    {queryResult.sample_events.slice(0, 5).map((ev, i) => (
                      <tr key={i}>
                        <td>{ev.timestamp}</td>
                        <td>{ev.host}</td>
                        <td>{ev.user}</td>
                        <td style={{ maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ev.command}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          ) : (
            <div>
              <div className="shimmer-stat-row">
                <div className="shimmer shimmer-stat" />
                <div className="shimmer shimmer-stat" />
                <div className="shimmer shimmer-stat" />
                <div className="shimmer shimmer-stat" />
              </div>
              {[1,2,3].map(i => <div key={i} className="shimmer shimmer-row" />)}
            </div>
          )}
        </div>
      </div>

      {/* Section 2: Olly Analysis */}
      <div className="hunt-section">
        <div className="hunt-section-header">
          <div className="hunt-section-num">2</div>
          <div className="hunt-section-title">Olly Analysis</div>
        </div>
        <div className="hunt-section-body">
          {ollySections ? (
            Object.entries(OLLY_LABELS).map(([key, label]) => {
              const isExpanded = expandedSections.has(key);
              const content = ollySections[key] || '';
              const isGapSection = key === '10';
              return (
                <div key={key} className="olly-accordion">
                  <div
                    className={`olly-acc-header${isExpanded ? ' expanded' : ''}`}
                    onClick={() => toggleSection(key)}
                  >
                    <span><span className="olly-acc-num">§{key}</span>{label}</span>
                    {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  </div>
                  {isExpanded && (
                    <div className="olly-acc-body">
                      {isGapSection && content && (
                        <div className="olly-gap-alert">Visibility gaps detected — review before deploying this detection.</div>
                      )}
                      {content || <span style={{ color: '#6E7B8B', fontStyle: 'italic' }}>No content returned.</span>}
                    </div>
                  )}
                </div>
              );
            })
          ) : (
            <div>
              {Array(4).fill(0).map((_, i) => <div key={i} className="shimmer shimmer-accordion" />)}
            </div>
          )}
        </div>
      </div>

      {/* Section 3: Hunt Report */}
      {report && (() => {
        const v = report.verdict;
        const color = verdictColor(v);
        const bannerClass = v === 'threat' ? 'report-banner-threat' : v === 'suspicious' ? 'report-banner-suspicious' : 'report-banner-clean';
        const iconClass = v === 'threat' ? 'report-icon-threat' : v === 'suspicious' ? 'report-icon-suspicious' : 'report-icon-clean';
        const labelClass = v === 'threat' ? 'label-threat' : v === 'suspicious' ? 'label-suspicious' : 'label-clean';
        const verdictLabel = v === 'threat' ? 'THREAT DETECTED' : v === 'suspicious' ? 'SUSPICIOUS ACTIVITY' : 'NO THREATS FOUND';

        return (
          <div className="hunt-report">
            <div className={`report-banner ${bannerClass}`}>
              <div className={`report-icon ${iconClass}`}>
                {v === 'threat' ? (
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
                  </svg>
                ) : v === 'suspicious' ? (
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                    <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
                  </svg>
                ) : (
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><polyline points="9 12 11 14 15 10"/>
                  </svg>
                )}
              </div>
              <div className="report-banner-body">
                <div className={`report-verdict-label ${labelClass}`}>HUNT REPORT · {verdictLabel} · {report.confidence.toUpperCase()} CONFIDENCE</div>
                <div className="report-title">{report.title}</div>
                <div className="report-subtitle">{report.subtitle}</div>
                <div className="report-stats">
                  <div className="rstat">
                    <div className="rstat-val" style={{ color }}>{report.stats.hits}</div>
                    <div className="rstat-lbl">hits</div>
                  </div>
                  <div className="rstat">
                    <div className="rstat-val" style={{ color: report.stats.hosts !== '0' ? color : '#6E7B8B' }}>{report.stats.hosts}</div>
                    <div className="rstat-lbl">hosts</div>
                  </div>
                  <div className="rstat">
                    <div className="rstat-val" style={{ color: '#6E7B8B' }}>{report.stats.attack_window}</div>
                    <div className="rstat-lbl">window</div>
                  </div>
                  {report.stats.c2_flagged && (
                    <div className="rstat">
                      <div className="rstat-val" style={{ color: '#EF4444' }}>C2</div>
                      <div className="rstat-lbl">IP flagged</div>
                    </div>
                  )}
                </div>
              </div>
            </div>

            <div className="report-body">
              {report.findings.length > 0 && (
                <>
                  <div className="report-section-title">Key Findings</div>
                  <ul className="report-findings">
                    {report.findings.map((f, i) => (
                      <li key={i}>
                        <div className={`finding-dot dot-${f.severity}`} />
                        <span>{f.text}</span>
                      </li>
                    ))}
                  </ul>
                </>
              )}

              {report.actions.length > 0 && (
                <>
                  <div className="report-section-title">Immediate Actions</div>
                  <div className="report-actions">
                    {report.actions.map((a, i) => (
                      <div key={i} className={`action-item action-${a.level}`}>
                        <span className={`action-num action-num-${a.level}`}>{a.priority}.</span>
                        <span className="action-text">{a.description}</span>
                      </div>
                    ))}
                  </div>
                </>
              )}

              <div className="report-section-title">Alert Definition Skeleton</div>
              <div className="report-alert-def">
                <div className="alert-def-grid">
                  {Object.entries(report.alert_def).map(([k, v]) => (
                    <>
                      <span key={`k-${k}`} className="alert-def-key">{k.replace('_', ' ')}</span>
                      <span key={`v-${k}`} className="alert-def-val">{v}</span>
                    </>
                  ))}
                </div>
              </div>
            </div>

            <div className="report-footer">
              <span className="report-footer-meta">
                {new Date(report.timestamp).toLocaleString()} · {report.run_duration_ms}ms
              </span>
              <div className="report-actions-row">
                {ollySections?.['5'] && cxRegion && (
                  <button className="hunt-btn hunt-btn-primary" onClick={openInCoralogix}>
                    <ExternalLink size={13} /> Open in Coralogix
                  </button>
                )}
                {huntId && (
                  <button className="hunt-btn hunt-btn-secondary" onClick={() => exportHuntReport(huntId)}>
                    <Download size={13} /> Export Report
                  </button>
                )}
              </div>
            </div>
          </div>
        );
      })()}

      {/* Report skeleton while step is active */}
      {activeStep === 'report' && !report && (
        <div className="hunt-section">
          <div className="hunt-section-header">
            <div className="hunt-section-num" style={{ background: 'rgba(59,130,246,.2)', color: '#3B82F6' }}>3</div>
            <div className="hunt-section-title">Hunt Report</div>
          </div>
          <div className="hunt-section-body">
            <div className="shimmer shimmer-box" style={{ marginBottom: 12 }} />
            {[1,2,3].map(i => <div key={i} className={`shimmer shimmer-line ${i === 3 ? 'w-half' : i === 2 ? 'w-3q' : 'w-full'}`} />)}
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 3: TypeScript check**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
npx tsc --noEmit
```
Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/HuntView.tsx frontend/src/components/HuntView.css
git commit -m "feat(hunt): HuntView component with stepper, skeletons, and deliverable card"
```

---

## Task 9: Hunt button in DetectionBuilder

**Files:**
- Modify: `frontend/src/components/DetectionBuilder.tsx`

- [ ] **Step 1: Add onHunt prop to the Props interface**

In `DetectionBuilder.tsx`, find the `Props` interface (line 51) and add:

```typescript
interface Props {
  clientName: string;
  preselectedIds?: string[];
  onHunt?: (alert: FlowAlert) => void;
}
```

- [ ] **Step 2: Destructure onHunt in the component signature**

Change line 58:
```typescript
export default function DetectionBuilder({ clientName, preselectedIds, onHunt }: Props) {
```

- [ ] **Step 3: Add Hunt button to each FlowAlert card**

In the `alert-cards` render block (line ~683 in the `ac-body` div, after the `ac-window-reason` div), add:

```tsx
{onHunt && (
  <div style={{ marginTop: 12, display: 'flex', justifyContent: 'flex-end' }}>
    <button
      className="hunt-trigger-btn"
      onClick={() => onHunt(a)}
    >
      Hunt
    </button>
  </div>
)}
```

- [ ] **Step 4: Add the CSS for hunt-trigger-btn to DetectionBuilder.css (or App.css)**

In `frontend/src/App.css`, add:

```css
.hunt-trigger-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-weight: 600;
  padding: 5px 12px;
  border-radius: 5px;
  border: 1px solid rgba(59,130,246,.4);
  background: rgba(59,130,246,.1);
  color: #60A5FA;
  cursor: pointer;
  letter-spacing: .04em;
  text-transform: uppercase;
  transition: background .15s, border-color .15s;
}
.hunt-trigger-btn:hover {
  background: rgba(59,130,246,.2);
  border-color: rgba(59,130,246,.6);
}
```

- [ ] **Step 5: TypeScript check**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
npx tsc --noEmit
```
Expected: 0 errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/DetectionBuilder.tsx frontend/src/App.css
git commit -m "feat(hunt): Hunt button on each generated FlowAlert card"
```

---

## Task 10: App.tsx routing and final integration

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Add 'hunt' to the View type and huntDetection state**

Change line 17:
```typescript
type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder' | 'hunt';
```

Add state after `builderPreselectedIds` (line ~40):
```typescript
const [huntDetection, setHuntDetection] = useState<import('./types').FlowAlert | null>(null);
```

- [ ] **Step 2: Add HuntView import**

After the DetectionBuilder import (line 10):
```typescript
import HuntView from './components/HuntView';
```

- [ ] **Step 3: Pass onHunt to DetectionBuilder**

Change line 250:
```tsx
<DetectionBuilder
  clientName={clientName}
  preselectedIds={builderPreselectedIds.length > 0 ? builderPreselectedIds : undefined}
  onHunt={(alert) => { setHuntDetection(alert); navigate('hunt'); }}
/>
```

- [ ] **Step 4: Add HuntView render block**

After the builder block (after line 252, before `</AnimatePresence>`):
```tsx
{view === 'hunt' && huntDetection && (
  <motion.div key="hunt" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, overflowY: 'auto' }}>
    <HuntView
      detection={huntDetection}
      cxRegion={import.meta.env.VITE_CX_REGION}
      onBack={() => navigate('builder')}
    />
  </motion.div>
)}
```

- [ ] **Step 5: Clear huntDetection when navigating away from hunt view**

In the `popstate` handler (around line 58–60), add:
```typescript
if (target !== 'hunt') {
  setHuntDetection(null);
}
```

- [ ] **Step 6: TypeScript check and build**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
npx tsc --noEmit
npm run build
```
Expected: 0 TypeScript errors, successful Vite build.

- [ ] **Step 7: Run backend tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/api/ -v
```
Expected: all existing tests pass + all new hunt tests pass.

- [ ] **Step 8: Start dev server and verify**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run dev
```

Manually verify:
1. Navigate to Detection Builder, generate detections
2. Each FlowAlert card shows a "Hunt" button
3. Clicking Hunt navigates to `/hunt` view
4. Stepper shows Step 1 active (Log Query shimmer visible)
5. Back button returns to Detection Builder
6. If backend is running with `CX_BIN_PATH` set: full hunt flow executes

- [ ] **Step 9: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(hunt): wire HuntView into App routing — Hunt button live"
```

---

## Environment Setup Reference

Add to `.env` or server environment before running hunt in production:

```bash
CX_BIN_PATH=/usr/local/bin/cx        # Required — path to cx binary
CX_REGION=eu2                         # Required — Coralogix region for deep links
HUNT_MAX_CONCURRENT=3                 # Optional — default 3
```

Frontend `.env.local`:
```bash
VITE_CX_REGION=eu2
```

Verify cx is available and authenticated:
```bash
$CX_BIN_PATH --version
$CX_BIN_PATH profiles list
```
