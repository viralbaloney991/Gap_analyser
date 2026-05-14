# Two-Pass Olly Hunt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-shot Olly call with a two-pass approach: pass 1 (gpt-5.2, focus, ~72s) discovers real field names and returns a `chat_id`; pass 2 (claude-sonnet-4-5, skill, --chat-id, ~216s) continues the same chat and produces a full 12-section report with live pivot findings.

**Architecture:** `cx logs` → `query_done` SSE → pass 1 `cx olly ask --output agents` (schema discovery, extracts `chat_id`) → pass 2 `cx olly ask --chat-id` (full 12-section report, runs live pivots) → `olly_done` SSE → `report_ready` SSE. All data stays inside Coralogix infrastructure.

**Tech Stack:** Go, `cx` CLI v2 (`--chat-id`, `--mode skill`, `--output agents`), SSE streaming, regexp for agents-format parsing.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/api/hunt.go` | Replace `ollyPromptTemplate` + `buildOllyPrompt` with schema prompt + report prompt; add `parseAgentsOutput`; update `cxExecutor` interface (`runOllySchema` + `runOllyReport` replace `runOllyChat`); update `cxRunner` implementations; update `HandleHuntStream` two-pass flow; update `huntTimeout` |
| `backend/internal/api/hunt_test.go` | Update `mockCxExecutor` to new interface; update `TestHandleHuntStream_MockSuccess`; add `TestParseAgentsOutput_*` and `TestBuildOllySchemaPrompt`, `TestBuildOllyReportPrompt` |
| `backend/cmd/server/main.go` | Increase `WriteTimeout` from 8 min to 12 min |

---

### Task 1: `parseAgentsOutput` — parse chat_id from `--output agents` format

**Files:**
- Modify: `backend/internal/api/hunt.go` (after the `ollyURLRe` var, ~line 235)
- Test: `backend/internal/api/hunt_test.go`

The `--output agents` format looks like:
```
Creating new chat...
Sending message...
[1]{chat_id,interaction_id,status,response,interaction_mode,model_choice}:
  "3fe8d46b-ea69-4701-808d-43ac5c21ba55","eda6fe6e-...",completed,"<response text>",focus,"gpt-5.2"
```

We need to extract: (1) the first UUID as `chatID`, (2) the response text as `response`.

- [ ] **Step 1: Write failing tests**

Add to `hunt_test.go` after `TestParseOllySectionsNumberedFormat`:

```go
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
	_ = response
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/api/ -run TestParseAgentsOutput -v
```
Expected: FAIL with `undefined: parseAgentsOutput`

- [ ] **Step 3: Implement `parseAgentsOutput` in hunt.go**

Add after `var ollyURLRe` (around line 235):

```go
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
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd backend && go test ./internal/api/ -run TestParseAgentsOutput -v
```
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): parseAgentsOutput — extract chat_id from cx --output agents format"
```

---

### Task 2: New prompt templates

**Files:**
- Modify: `backend/internal/api/hunt.go` (replace `ollyPromptTemplate` block, lines 106–145)
- Test: `backend/internal/api/hunt_test.go`

Replace the current single prompt with two: a focused schema-discovery prompt (pass 1) and a static full-report prompt (pass 2).

- [ ] **Step 1: Write failing tests**

Add to `hunt_test.go`:

```go
func TestBuildOllySchemaPrompt(t *testing.T) {
	prompt, err := buildOllySchemaPrompt(`url:(*webhook.site*)`, 3, "host1 user1 2024-01-01 GET webhook.site/abc")
	if err != nil {
		t.Fatalf("buildOllySchemaPrompt: %v", err)
	}
	if !strings.Contains(prompt, `url:(*webhook.site*)`) {
		t.Error("prompt missing lucene query")
	}
	if !strings.Contains(prompt, "3") {
		t.Error("prompt missing hit count")
	}
	if !strings.Contains(prompt, "webhook.site") {
		t.Error("prompt missing sample events")
	}
	if !strings.Contains(prompt, "$d.url") {
		t.Error("prompt missing field check list")
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/api/ -run "TestBuildOllySchemaPrompt|TestBuildOllyReportPrompt" -v
```
Expected: FAIL with `undefined: buildOllySchemaPrompt` and `undefined: buildOllyReportPrompt`

- [ ] **Step 3: Replace the prompt block in hunt.go**

Replace from `// ── Olly prompt` comment through the closing brace of `buildOllyPrompt` (lines 106–145) with:

```go
// ── Olly prompts ──────────────────────────────────────────────────────────────
//
// Two-pass design: pass 1 (gpt-5.2, focus, ~72s) discovers real field names
// via 3 targeted questions and returns a chat_id. Pass 2 (claude-sonnet-4-5,
// skill, --chat-id, ~216s) continues the same chat and produces the full
// 12-section report with live pivot findings.
// All analysis stays inside Coralogix infrastructure.

const ollySchemaPromptTemplate = `Schema discovery for threat hunt. Answer ONLY these 3 questions:

Query: {{.LuceneQuery}}
Hits: {{.HitCount}}
{{if .SampleEvents}}Sample events:
{{.SampleEvents}}
{{end}}
1. What URL/URI/path/link fields exist in these logs? Check: $d.url, $d.uri, $d.cx_security.uri, $d.Island.details.file_processing_details.urls, $d.Url, $d.MessageURLs, $d.page — which ones have data?
2. Do webhook.site, discord.com/api/webhooks, hooks.slack.com, or zapier.com/hooks appear in any URL field? How many hits each?
3. Top 5 non-null event types: source logs | filter $d.event_type != null | groupby $d.event_type aggregate count() as hits | orderby hits desc | limit 5

Facts only. No preamble.`

type ollySchemaPromptData struct {
	LuceneQuery  string
	HitCount     int
	SampleEvents string
}

var ollySchemaTmpl = template.Must(template.New("ollySchema").Parse(ollySchemaPromptTemplate))

func buildOllySchemaPrompt(luceneQuery string, hitCount int, sampleEvents string) (string, error) {
	var buf bytes.Buffer
	err := ollySchemaTmpl.Execute(&buf, ollySchemaPromptData{
		LuceneQuery:  luceneQuery,
		HitCount:     hitCount,
		SampleEvents: sampleEvents,
	})
	if err != nil {
		return "", fmt.Errorf("render olly schema prompt: %w", err)
	}
	return buf.String(), nil
}

// ollyReportPrompt is a static string sent as the pass-2 message in the same
// chat session (--chat-id). Olly already knows the schema from pass 1 and
// uses confirmed field names to run live pivot queries for section 8.
const ollyReportPrompt = `Using what you found above, write the complete threat hunt report.
For section 8 Pivot Investigation, RUN actual queries and report real findings — do not just suggest them.

## 1. Hunt Summary
Severity (Critical/High/Medium/Low), Confidence (High/Medium/Low), MITRE ATT&CK Tactic/Technique

## 2. Original Query
Echo the Lucene query

## 3. Schema Mapping
Table: Original Field | Confirmed CX Field | Exists (Y/N)

## 4. Translated Query — DataPrime
Using ONLY confirmed fields from section 3

## 5. Translated Query — Lucene
Optimised Lucene with confirmed fields

## 6. Detection Logic Explained
Plain English explanation of what this detects and why

## 7. Hunt Workflow
Step-by-step for a tier-2 analyst investigating a hit

## 8. Pivot Investigation
RUN these now and report actual findings:
- Top domains/URLs in the confirmed URL fields
- Top users or source IPs generating these events
- Are hits time-clustered (automated) or spread (manual)?

## 9. False Positive Considerations
Likely FP sources with suppression suggestions

## 10. Visibility Gaps
Missing log sources or fields that limit this hunt

## 11. Follow-up Hunts
3 related hunts with DataPrime query sketches

## 12. Alert Definition
Name: <descriptive name>
Type: standard
Condition: count > 0
Severity: <Critical/High/Medium/Low>
Group-By: <most relevant field>`

func buildOllyReportPrompt() string {
	return ollyReportPrompt
}

// buildOllyPrompt is kept for backward compatibility with existing tests.
func buildOllyPrompt(luceneQuery string, hitCount int, sampleEvents string) (string, error) {
	return buildOllySchemaPrompt(luceneQuery, hitCount, sampleEvents)
}
```

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/api/ -run "TestBuildOllySchemaPrompt|TestBuildOllyReportPrompt|TestBuildOllyPrompt" -v
```
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): two-pass Olly prompts — schema discovery + full 12-section report template"
```

---

### Task 3: Update `cxExecutor` interface and `cxRunner`

**Files:**
- Modify: `backend/internal/api/hunt.go` (`cxExecutor` interface ~line 270, `cxRunner` methods ~line 294)
- Modify: `backend/internal/api/hunt_test.go` (`mockCxExecutor`)

Replace `runOllyChat` with `runOllySchema` (pass 1, `--output agents`) and `runOllyReport` (pass 2, `--chat-id --mode skill`).

- [ ] **Step 1: Update `mockCxExecutor` in hunt_test.go first**

Replace the existing `mockCxExecutor` struct and its methods (lines 186–203):

```go
type mockCxExecutor struct {
	logsOutput   []byte
	logsErr      error
	schemaOutput []byte
	schemaErr    error
	reportOutput []byte
	reportErr    error
}

func (m *mockCxExecutor) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	return m.logsOutput, m.logsErr
}

func (m *mockCxExecutor) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
	return m.schemaOutput, m.schemaErr
}

func (m *mockCxExecutor) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	return m.reportOutput, m.reportErr
}
```

- [ ] **Step 2: Update the interface compile-time check in hunt_test.go**

The line `var _ cxExecutor = &mockCxExecutor{}` stays unchanged — it will validate the new interface once we update it.

- [ ] **Step 3: Update `cxExecutor` interface in hunt.go**

Replace (lines ~410–414):
```go
type cxExecutor interface {
	runLogs(ctx context.Context, query, window string) ([]byte, error)
	runOllyChat(ctx context.Context, prompt string) ([]byte, error)
}
```
With:
```go
type cxExecutor interface {
	runLogs(ctx context.Context, query, window string) ([]byte, error)
	runOllySchema(ctx context.Context, prompt string) ([]byte, error)
	runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error)
}
```

- [ ] **Step 4: Update `cxRunner` implementations**

Replace `runOllyChat` method (~lines 440–446) with two new methods:

```go
func (r *cxRunner) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
	// Pass 1: gpt-5.2 default, focus mode, agents output for chat_id extraction.
	// No --model flag → uses cx default (gpt-5.2), faster for schema discovery.
	cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask",
		"--output", "agents", "--mode", "focus", "--timeout", "300", prompt)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	// Pass 2: claude-sonnet-4-5, skill mode for DataPrime expertise, continues
	// the pass-1 chat so Olly uses confirmed field names for live pivot queries.
	args := []string{"olly", "ask", "--mode", "skill", "--model", "claude-sonnet-4-5", "--timeout", "600"}
	if chatID != "" {
		args = append(args, "--chat-id", chatID)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, r.binPath, args...)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}
```

- [ ] **Step 5: Verify build compiles**

```bash
cd backend && go build ./...
```
Expected: no errors

- [ ] **Step 6: Run all hunt tests**

```bash
cd backend && go test ./internal/api/ -run "TestMockCxExecutor|TestHandleHunt" -v
```
Expected: FAIL on `TestHandleHuntStream_MockSuccess` (still uses old `ollyOutput` field — fixed in Task 4)

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "refactor(hunt): cxExecutor — replace runOllyChat with runOllySchema + runOllyReport"
```

---

### Task 4: Update `HandleHuntStream` to two-pass flow

**Files:**
- Modify: `backend/internal/api/hunt.go` (Step 2 section in `HandleHuntStream`, ~lines 430–472)
- Modify: `backend/internal/api/hunt_test.go` (`TestHandleHuntStream_MockSuccess`)

- [ ] **Step 1: Update `TestHandleHuntStream_MockSuccess` in hunt_test.go**

Replace the `mock` definition and the test body — `ollyOutput` becomes `schemaOutput` + `reportOutput`. The report output uses `## 1.` format (handled by `numberedHeaderRe`):

```go
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
		logsOutput:   []byte(`{"hits":2,"events":[{"timestamp":"2024-01-01","host":"h1","user":"u1","cmd":"enc"}]}`),
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
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
cd backend && go test ./internal/api/ -run TestHandleHuntStream_MockSuccess -v
```
Expected: FAIL (handler still uses old single-pass flow)

- [ ] **Step 3: Replace Step 2 in `HandleHuntStream`**

Replace the existing Step 2 block in `HandleHuntStream` (from `// Step 2: Olly analysis` through `sendSSE(w, flusher, "olly_done", ...)`) with:

```go
	// Step 2a: Pass 1 — schema discovery (gpt-5.2, focus, ~72s)
	// Uses --output agents to get structured output with chat_id.
	sampleText := formatSampleEvents(qd.SampleEvents)
	schemaPrompt, err := buildOllySchemaPrompt(lucene, qd.Hits, sampleText)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "prompt_build_failed", Message: err.Error()})
		return
	}

	schemaOut, err := cx.runOllySchema(ctx, schemaPrompt)
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
		return
	}

	chatID, _ := parseAgentsOutput(schemaOut)

	// Step 2b: Pass 2 — full 12-section report (claude-sonnet-4-5, skill, ~216s)
	// Continues the same chat so Olly has confirmed field names for live pivots.
	// Falls back to a fresh chat if chat_id could not be extracted.
	reportOut, err := cx.runOllyReport(ctx, chatID, buildOllyReportPrompt())
	if err != nil {
		sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
		return
	}

	sections := parseOllySections(string(reportOut))
	sendSSE(w, flusher, "olly_done", ollyDoneData{Sections: sections})
```

- [ ] **Step 4: Run all hunt tests**

```bash
cd backend && go test ./internal/api/ -v
```
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): two-pass Olly flow — schema discovery then full report via chat continuation"
```

---

### Task 5: Update timeouts

**Files:**
- Modify: `backend/internal/api/hunt.go` (line ~354 `huntTimeout`)
- Modify: `backend/cmd/server/main.go` (line ~148 `WriteTimeout`)

Pass 1 (~72s) + pass 2 (~216s) = ~288s. Add buffer: 10 min hunt context, 12 min write timeout.

- [ ] **Step 1: Update `huntTimeout` in hunt.go**

Find and replace:
```go
const huntTimeout = 6 * time.Minute
```
With:
```go
const huntTimeout = 10 * time.Minute
```

- [ ] **Step 2: Update `WriteTimeout` in main.go**

Find and replace (line ~148):
```go
WriteTimeout: 8 * time.Minute,
```
With:
```go
WriteTimeout: 12 * time.Minute,
```

- [ ] **Step 3: Build and run all tests**

```bash
cd backend && go build ./... && go test ./internal/api/ -v
```
Expected: build clean, all tests pass

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/hunt.go backend/cmd/server/main.go
git commit -m "chore(hunt): increase timeouts — 10min hunt context, 12min write timeout for two-pass flow"
```

---

### Task 6: End-to-end smoke test

Manually verify the full two-pass flow works against real Coralogix data.

- [ ] **Step 1: Start the backend**

```bash
cd backend && source ../.env && go run ./cmd/server/main.go
```
Expected: `loaded config: N clients` and server on `:8080`

- [ ] **Step 2: Trigger a hunt via curl**

```bash
curl -N "http://localhost:8080/api/hunt/stream?lucene=url%3A%28%2Awebhook.site%2A%29&window=7d&name=Webhook+Test&severity=medium&client=Deel"
```
Watch SSE events stream in order:
1. `event: stream_opened`
2. `event: query_done` (with `hits` count)
3. (wait ~72s) cx pass 1 completes
4. (wait ~216s more) cx pass 2 completes
5. `event: olly_done` (sections present — verify section "1" and "8" have content)
6. `event: report_ready` (full report JSON with `verdict`, `findings`, `actions`)

- [ ] **Step 3: Verify section 8 has real pivot findings**

In the `olly_done` event data, check `sections["8"]` contains actual user names or IP addresses (not just "run this query"). If pivot findings are present, the two-pass + live pivot behaviour is confirmed.

- [ ] **Step 4: Final commit if any fixups were needed**

```bash
git add -A && git commit -m "fix(hunt): two-pass smoke test fixups"
```
(Only needed if step 2-3 revealed issues.)
