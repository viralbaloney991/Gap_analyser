# Hunt Pipeline Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the hunt pipeline's broken hit count, make the pass-1 prompt generic for any query type, reorder the pass-2 report to lead with actual findings, and update the frontend section labels.

**Architecture:** Three focused backend changes to `hunt.go` (parser, pass-1 prompt, pass-2 prompt + section wiring) plus a one-map update to `HuntView.tsx`. All changes are to existing functions — no new files. The "Continue in CX" button is already implemented in `HuntView.tsx` and reads `ollySections['chat_url']` from the `olly_done` SSE; no change needed there.

**Tech Stack:** Go (backend), React/TypeScript (frontend), `encoding/json`, `regexp`, `text/template`

---

## Files

| File | Changes |
|------|---------|
| `backend/internal/api/hunt.go` | `parseLogsOutput`, `formatSampleEvents`, `runLogs`, pass-1 prompt + struct, `buildOllySchemaPrompt`, `extractTotalFromPass1`, pass-2 prompt, section number call sites, `HandleHuntStream` |
| `backend/internal/api/hunt_test.go` | New tests for bare-array parser and total extraction; update tests for changed prompts and section numbers |
| `frontend/src/components/HuntView.tsx` | `OLLY_LABELS` map only |

---

### Task 1: Fix cx logs parsing — bare JSON array + agents format

**Context:** `cx logs --output json` returns a bare JSON array `[{...}]`, not `{"hits":N,"events":[...]}`. The current parser fails to unmarshal it and falls back to counting newlines. We switch to `--output agents` (DataPrime paths: `$m`, `$l`, `$d`) and rewrite `parseLogsOutput` to unmarshal a bare array. We also rewrite `formatSampleEvents` to forward compact JSON to Olly instead of a stripped text format.

**Files:**
- Modify: `backend/internal/api/hunt.go` — `queryDoneData` struct, `parseLogsOutput`, `formatSampleEvents`, `cxRunner.runLogs`, `HandleHuntStream` (cxCmd string + `formatSampleEvents` call)
- Modify: `backend/internal/api/hunt_test.go` — new tests, update `TestHandleHuntStream_MockSuccess` mock

- [ ] **Step 1: Write two failing tests**

In `backend/internal/api/hunt_test.go`, add after `TestDeriveVerdict`:

```go
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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
cd backend && go test ./internal/api/ -run "TestParseLogsOutputBareArray|TestParseLogsOutputEmptyArray" -v
```

Expected: FAIL — `Hits = 1, want 2` (fallback counts `[]` as one line)

- [ ] **Step 3: Add `rawEvents` field to `queryDoneData`**

In `hunt.go`, the `queryDoneData` struct is at line ~30. Add one unexported field (unexported fields are automatically excluded from JSON):

```go
type queryDoneData struct {
	Hits         int            `json:"hits"`
	Hosts        int            `json:"hosts"`
	LastSeen     string         `json:"last_seen"`
	UniqueUsers  int            `json:"unique_users"`
	SampleEvents []huntLogEvent `json:"sample_events"`
	CxCommand    string         `json:"cx_command"`
	rawEvents    []json.RawMessage // excluded from SSE payload (unexported)
}
```

- [ ] **Step 4: Rewrite `parseLogsOutput`**

Replace the entire `parseLogsOutput` function (currently at line ~593):

```go
func parseLogsOutput(raw []byte, cxCmd string) queryDoneData {
	qd := queryDoneData{CxCommand: cxCmd, LastSeen: "unknown"}
	var events []json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil || len(events) == 0 {
		return qd
	}
	qd.Hits = len(events)
	qd.rawEvents = events

	seen := make(map[string]bool)
	users := make(map[string]bool)
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
	qd.UniqueUsers = len(users)
	return qd
}
```

- [ ] **Step 5: Rewrite `formatSampleEvents`**

Replace the entire `formatSampleEvents` function (currently at line ~632):

```go
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
```

- [ ] **Step 6: Switch `runLogs` to `--output agents`**

In `hunt.go`, the `cxRunner.runLogs` function is at line ~383. Change `--output json` to `--output agents`:

```go
func (r *cxRunner) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "logs", query,
		"--start", "now-"+window, "--output", "agents", "--limit", "50")
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}
```

- [ ] **Step 7: Update `HandleHuntStream` call sites**

In `HandleHuntStream` (line ~524), update the display string and `formatSampleEvents` call:

```go
// Step 1: cx logs
cxCmd := fmt.Sprintf("cx logs '%s' --start now-%s --output agents --limit 50", lucene, window)
logsOut, err := cx.runLogs(ctx, lucene, window)
if err != nil {
    sendSSE(w, flusher, "error", huntErrorData{Code: "cx_logs_failed", Message: err.Error()})
    return
}

qd := parseLogsOutput(logsOut, cxCmd)
sendSSE(w, flusher, "query_done", qd)

// Step 2a: Pass 1 — schema discovery
sampleText := formatSampleEvents(qd.rawEvents)
```

- [ ] **Step 8: Update mock in `TestHandleHuntStream_MockSuccess`**

In `hunt_test.go`, the `logsOutput` mock at the `TestHandleHuntStream_MockSuccess` test needs to be the bare agents-format array:

```go
mock := &mockCxExecutor{
    logsOutput: []byte(`[{"$m":{"timestamp":"2024-01-01T10:00:00"},"$l":{"applicationname":"h1"},"$d":{"cmd":"enc"}}]`),
    // schemaOutput and reportOutput unchanged
```

- [ ] **Step 9: Run all tests**

```bash
cd backend && go test ./internal/api/ -v 2>&1 | tail -20
```

Expected: All pass, including the two new `TestParseLogsOutput*` tests.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "fix(hunt): parse cx logs bare JSON array; switch to --output agents format"
```

---

### Task 2: Rewrite pass-1 prompt — generic schema + count extraction

**Context:** The current pass-1 prompt asks about `$d.url` and `webhook.site` — hardcoded for one hunt type. Replace with 4 generic questions that work for any Lucene query. Add `Window` to the template. Add question 3 that forces Olly to emit `"Total: N"` for reliable regex extraction. After pass-1 completes, extract that total and override `qd.Hits` so `report_ready` carries the authoritative count.

**Files:**
- Modify: `backend/internal/api/hunt.go` — prompt constant, struct, `buildOllySchemaPrompt` signature, `buildOllyPrompt` shim, `extractTotalFromPass1` (new function), `HandleHuntStream` (use `schemaText`, wire total)
- Modify: `backend/internal/api/hunt_test.go` — update `TestBuildOllySchemaPrompt`, `TestBuildOllyPrompt`; add `TestExtractTotalFromPass1`

- [ ] **Step 1: Write failing tests**

Replace `TestBuildOllySchemaPrompt` and `TestBuildOllyPrompt` in `hunt_test.go`, and add `TestExtractTotalFromPass1`:

```go
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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
cd backend && go test ./internal/api/ -run "TestBuildOllySchemaPrompt|TestBuildOllyPrompt|TestExtractTotalFromPass1" -v
```

Expected: `TestBuildOllySchemaPrompt` FAIL (old prompt has `webhook.site`); `TestExtractTotalFromPass1` FAIL (function doesn't exist).

- [ ] **Step 3: Update `ollySchemaPromptData` struct**

In `hunt.go`, replace the struct at line ~127:

```go
type ollySchemaPromptData struct {
	LuceneQuery  string
	SampleCount  int
	SampleEvents string
	Window       string
}
```

- [ ] **Step 4: Replace `ollySchemaPromptTemplate`**

In `hunt.go`, replace the `ollySchemaPromptTemplate` constant at line ~114:

```go
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
```

- [ ] **Step 5: Update `buildOllySchemaPrompt` signature**

Replace the function at line ~135:

```go
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
```

- [ ] **Step 6: Update `buildOllyPrompt` deprecated shim**

Replace the shim at line ~207:

```go
// Deprecated: buildOllyPrompt delegates to buildOllySchemaPrompt for backward
// compatibility with existing tests. New call sites use buildOllySchemaPrompt directly.
func buildOllyPrompt(luceneQuery string, hitCount int, sampleEvents string) (string, error) {
	return buildOllySchemaPrompt(luceneQuery, hitCount, sampleEvents, "30d")
}
```

- [ ] **Step 7: Add `extractTotalFromPass1` function**

Add this function after `buildOllyReportPrompt` (line ~200), before the section parser block:

```go
var totalCountRe = regexp.MustCompile(`(?i)Total:\s*(\d+)`)

// extractTotalFromPass1 parses the "Total: N" line that the pass-1 prompt
// instructs Olly to emit, returning the event count or 0 if not found.
func extractTotalFromPass1(text string) int {
	if m := totalCountRe.FindStringSubmatch(text); len(m) > 1 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return n
		}
	}
	return 0
}
```

Add `"strconv"` to the import block at the top of `hunt.go`.

- [ ] **Step 8: Update `HandleHuntStream` — new signature + extract total**

In `HandleHuntStream`, update the pass-1 call and extraction (lines ~537–550):

```go
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
if total := extractTotalFromPass1(schemaText); total > 0 {
    qd.Hits = total
}
```

- [ ] **Step 9: Run all tests**

```bash
cd backend && go test ./internal/api/ -v 2>&1 | tail -20
```

Expected: All pass.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): generic pass-1 prompt with count extraction; Total: N wired to report stats"
```

---

### Task 3: Rewrite pass-2 prompt + update section numbers

**Context:** Drop section 7 (Hunt Workflow), promote findings to section 1, renumber to 11 sections. Update every call site in `HandleHuntStream` and helpers that references a section number. Update `OLLY_LABELS` in the frontend. Update tests that check for old section numbering or old section titles.

**Files:**
- Modify: `backend/internal/api/hunt.go` — `ollyReportPrompt`, `HandleHuntStream` (section refs), `extractFindings` (section refs)
- Modify: `backend/internal/api/hunt_test.go` — `TestBuildOllyReportPrompt`, `TestParseOllySectionsBoldFormat`, `TestHandleHuntStream_MockSuccess`
- Modify: `frontend/src/components/HuntView.tsx` — `OLLY_LABELS`

- [ ] **Step 1: Write failing tests**

Replace `TestBuildOllyReportPrompt` in `hunt_test.go`:

```go
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
```

Update `TestParseOllySectionsBoldFormat` to use new section structure (was testing `## **12.`):

```go
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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
cd backend && go test ./internal/api/ -run "TestBuildOllyReportPrompt|TestParseOllySectionsBoldFormat" -v
```

Expected: `TestBuildOllyReportPrompt` FAIL (current prompt has section 12; no "What We Found"; has "Hunt Workflow").

- [ ] **Step 3: Replace `ollyReportPrompt`**

In `hunt.go`, replace the `ollyReportPrompt` constant at line ~151:

```go
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
```

- [ ] **Step 4: Update section number call sites in `HandleHuntStream`**

In `hunt.go`, the report-building block (line ~564):

```go
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
```

- [ ] **Step 5: Update section refs in `extractFindings`**

In `hunt.go`, the `extractFindings` function at line ~651. Change the section slice:

```go
func extractFindings(sections map[string]string, severity string) []huntFinding {
	var findings []huntFinding
	for _, src := range []string{"1", "7", "9"} {   // was "1","6","10"
```

(Only the slice literal changes; the rest of the function is identical.)

- [ ] **Step 6: Update mock `reportOutput` in `TestHandleHuntStream_MockSuccess`**

In `hunt_test.go`, replace the `reportOut` byte literal with the new 11-section format (section 2 must have "Severity: High\nConfidence: High" so `deriveVerdict` returns `"threat"`):

```go
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
```

- [ ] **Step 7: Run all backend tests**

```bash
cd backend && go test ./internal/api/ -v 2>&1 | tail -25
```

Expected: All pass.

- [ ] **Step 8: Update `OLLY_LABELS` in `HuntView.tsx`**

In `frontend/src/components/HuntView.tsx`, replace the `OLLY_LABELS` constant at line ~19:

```typescript
const OLLY_LABELS: Record<string, string> = {
  '1': 'What We Found',
  '2': 'Hunt Summary',
  '3': 'Original Query',
  '4': 'Schema Mapping',
  '5': 'Translated Query — DataPrime',
  '6': 'Translated Query — Lucene',
  '7': 'Detection Logic',
  '8': 'False Positive Sources',
  '9': 'Visibility Gaps',
  '10': 'Follow-up Hunts',
  '11': 'Alert Definition',
};
```

- [ ] **Step 9: Verify frontend build**

```bash
cd frontend && npm run build 2>&1 | tail -10
```

Expected: Build succeeds with no errors.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go frontend/src/components/HuntView.tsx
git commit -m "feat(hunt): findings-first 11-section report; drop analyst workflow; generic pass-2 prompt"
```

---

## Self-Review

**Spec coverage:**
1. ✅ Fix cx logs parsing → Task 1
2. ✅ Switch to `--output agents` → Task 1, Step 6
3. ✅ Rewrite pass-1 generic prompt + Window → Task 2
4. ✅ `extractTotalFromPass1` + wire to report stats → Task 2
5. ✅ Rewrite pass-2 prompt 11 sections findings-first → Task 3
6. ✅ Update `deriveVerdict`, `Subtitle`, `extractFindings`, `extractActions`, `parseAlertDef` → Task 3
7. ✅ Update `OLLY_LABELS` frontend → Task 3
8. ✅ "Continue in CX" button — already implemented in `HuntView.tsx` (`ollySections['chat_url']`); no change needed

**Type consistency:**
- `buildOllySchemaPrompt(lucene, qd.Hits, sampleText, window)` — all 4 args match the updated signature in Task 2 Step 5
- `extractActions(sections["10"])` — section 10 = Follow-up Hunts ✓
- `parseAlertDef(sections["11"], name, techniqueId)` — section 11 = Alert Definition ✓
- `deriveVerdict(qd.Hits, sections["2"])` — section 2 = Hunt Summary (has Severity/Confidence) ✓
- `extractFindings` reads `"1","7","9"` — What We Found, Detection Logic, Visibility Gaps ✓
