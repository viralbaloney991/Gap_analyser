# Noise Detection Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two independent bugs that together cause high-frequency operational alerts (EBS, Lambda, RDS) to show zero noise results for clients like Deel.

**Architecture:** Two surgical fixes. Task 1 removes the `IsSecurityAlert` gate from the behavioral noise path in `engine.go` — behavioral noise applies to every alert type, structural noise stays security-only. Task 2 fixes the `ListAlertEvents` gRPC-JSON request to use proto3 camelCase field names (`alertIds`, `timestampRange`, `pageSize`) instead of snake_case, which silently sends unrecognised fields and gets back empty event counts.

**Tech Stack:** Go 1.21 (`backend/`), existing `go test ./...`.

---

## File Map

| File | Tasks |
|---|---|
| `backend/internal/similarity/engine.go` | 1 |
| `backend/internal/similarity/engine_test.go` | 1 |
| `backend/internal/coralogix/client.go` | 2 |
| `backend/internal/coralogix/client_events_test.go` | 2 |

---

### Task 1: Remove IsSecurityAlert gate from behavioral noise path

**Files:**
- Modify: `backend/internal/similarity/engine.go:1145-1236`
- Modify: `backend/internal/similarity/engine_test.go`

**Background:** `findNoiseAlerts` (engine.go line 1170) skips every non-security alert before checking trigger frequency. So an AWS EBS alert firing 8,976 times is discarded before the `triggerCount > 10` check ever runs. Behavioral noise is a frequency signal — it should apply to all alert types. Structural noise (design-quality signal) stays security-only because operational alerts are *expected* to be broad.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/similarity/engine_test.go` (after `TestFindNoiseAlerts_nonSecurityExcluded`):

```go
func TestFindNoiseAlerts_nonSecurityBehavioralNoise(t *testing.T) {
	// Regression test: non-security (operational) alerts firing above threshold
	// must be flagged as behavioral noise. The IsSecurityAlert gate was incorrectly
	// blocking them before the trigger-count check.
	v := sparseVector("Amazon EBS - Volume Was Created")
	// isSecurityAlert=false, scoped (app+subsystem set) so structural won't fire.
	alert := makeAlert("ebs-1", "logs_threshold", false, false, nil, "aws", "ebs")
	counts := map[string]int{"ebs-1": 292}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("non-security alert with 292 triggers should be behavioral noise, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].NoiseType != "behavioral" {
		t.Errorf("noise_type: want behavioral, got %q", noisy[0].NoiseType)
	}
	if noisy[0].TriggerCount != 292 {
		t.Errorf("trigger_count: want 292, got %d", noisy[0].TriggerCount)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run TestFindNoiseAlerts_nonSecurityBehavioralNoise -v
```

Expected: `FAIL — non-security alert with 292 triggers should be behavioral noise, got 0`

- [ ] **Step 3: Apply the engine.go fix — remove IsSecurityAlert from exclusion block**

In `backend/internal/similarity/engine.go`, find the debug counters block (around line 1145):

```go
	var (
		skippedVendor    int
		skippedBB        int
		skippedNonSec    int
		skippedNoSignal  int
		noSignalReasons  = make(map[string]int) // breakdown of why no signal fired
	)
```

Replace with (remove `skippedNonSec`):

```go
	var (
		skippedVendor   int
		skippedBB       int
		skippedNoSignal int
		noSignalReasons = make(map[string]int) // breakdown of why no signal fired
	)
```

- [ ] **Step 4: Apply the engine.go fix — drop the `!IsSecurityAlert` early-exit**

Find the exclusion block (around line 1160):

```go
		// ── Exclusions ────────────────────────────────────────────────────
		if alert != nil {
			if alert.Features.VendorCovered {
				skippedVendor++
				continue
			}
			if alert.Features.IsBuildingBlock {
				skippedBB++
				continue
			}
			if !alert.Features.IsSecurityAlert {
				skippedNonSec++
				continue
			}
		}
```

Replace with:

```go
		// ── Exclusions ────────────────────────────────────────────────────
		if alert != nil {
			if alert.Features.VendorCovered {
				skippedVendor++
				continue
			}
			if alert.Features.IsBuildingBlock {
				skippedBB++
				continue
			}
		}
```

- [ ] **Step 5: Apply the engine.go fix — scope structural signal to security alerts only**

Find the structural signal block (around line 1193). The current opening condition is:

```go
		if alert != nil {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
```

Change the condition to gate on `IsSecurityAlert`:

```go
		if alert != nil && alert.Features.IsSecurityAlert {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
```

Leave the rest of the structural block (`isUnscoped`, `noEntity`, `isBroadQuery`, `isStructural`, the `noSignalReasons` switch) unchanged. The `else if !isBehavioral` branch at the end already handles the non-security + no-event-counts path correctly.

- [ ] **Step 6: Update the debug log to remove the deleted counter**

Find (around line 1233):

```go
	if len(noisy) == 0 && len(vectors) > 0 {
		log.Printf("DEBUG [noise] 0 noisy alerts from %d vectors: vendor=%d bb=%d non-sec=%d no-signal=%d reasons=%v eventCountsAvailable=%v",
			len(vectors), skippedVendor, skippedBB, skippedNonSec, skippedNoSignal, noSignalReasons, eventCounts != nil)
	}
```

Replace with:

```go
	if len(noisy) == 0 && len(vectors) > 0 {
		log.Printf("DEBUG [noise] 0 noisy alerts from %d vectors: vendor=%d bb=%d no-signal=%d reasons=%v eventCountsAvailable=%v",
			len(vectors), skippedVendor, skippedBB, skippedNoSignal, noSignalReasons, eventCounts != nil)
	}
```

- [ ] **Step 7: Update the renamed existing test**

Find `TestFindNoiseAlerts_nonSecurityExcluded` in `engine_test.go`:

```go
func TestFindNoiseAlerts_nonSecurityExcluded(t *testing.T) {
	v := sparseVector("Ops Alert")
	alert := makeAlert("ops-1", "logs_threshold", false, false, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("non-security alert should be excluded, got %v", noisy)
	}
}
```

Replace with (new name + updated error message — outcome is the same but the mechanism changed):

```go
func TestFindNoiseAlerts_nonSecurityNoCountsNoNoise(t *testing.T) {
	// Non-security alert with nil eventCounts: behavioral cannot fire (no counts),
	// structural cannot fire (IsSecurityAlert=false). Result must be empty.
	v := sparseVector("Ops Alert")
	alert := makeAlert("ops-1", "logs_threshold", false, false, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("non-security alert with no event counts should produce no noise, got %v", noisy)
	}
}
```

- [ ] **Step 8: Run the new failing test to confirm it now passes**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run TestFindNoiseAlerts_nonSecurityBehavioralNoise -v
```

Expected: `PASS`

- [ ] **Step 9: Run the full similarity test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -v
```

Expected: all PASS. If `TestFindNoiseAlerts_nonSecurityExcluded` appears in the output — you renamed it in Step 7, so it should no longer exist. Confirm the new name `TestFindNoiseAlerts_nonSecurityNoCountsNoNoise` passes.

- [ ] **Step 10: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "fix(noise): allow behavioral noise for non-security high-frequency alerts — structural stays security-only"
```

---

### Task 2: Fix ListAlertEvents gRPC-JSON request to use camelCase field names

**Files:**
- Modify: `backend/internal/coralogix/client.go` (inline struct types in `FetchAlertEventCounts`)
- Modify: `backend/internal/coralogix/client_events_test.go`

**Background:** Proto3 JSON transcoding encodes field names as camelCase. The `FetchAlertEventCounts` function sends `alert_ids`, `timestamp_range`, and `page_size` (snake_case), which the Coralogix API silently ignores, returning all events without alert-ID filtering. The response then has no matching `alertId` entries and `eventCounts` stays empty — disabling the behavioral signal. Fix: extract the inline request structs to package-level types and correct the JSON tags to `alertIds`, `timestampRange`, `pageSize`.

- [ ] **Step 1: Write the failing test**

In `backend/internal/coralogix/client_events_test.go`, update the import block from:

```go
import (
	"testing"
)
```

to:

```go
import (
	"encoding/json"
	"strings"
	"testing"
)
```

Then add:

```go
func TestEventCountReqBody_camelCaseFieldNames(t *testing.T) {
	// Coralogix proto3 JSON transcoding requires camelCase field names.
	// This test ensures the request struct tags match the API expectation.
	body := eventCountReqBody{
		AlertIDs: []string{"id-1", "id-2"},
		Pagination: eventCountReqPagination{PageSize: 1000},
	}
	body.TimestampRange.From = "2024-01-01T00:00:00Z"
	body.TimestampRange.To = "2024-01-31T00:00:00Z"

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)

	for _, want := range []string{`"alertIds"`, `"timestampRange"`, `"pageSize"`} {
		if !strings.Contains(s, want) {
			t.Errorf("request JSON missing camelCase key %s\nfull JSON: %s", want, s)
		}
	}
	for _, bad := range []string{`"alert_ids"`, `"timestamp_range"`, `"page_size"`} {
		if strings.Contains(s, bad) {
			t.Errorf("request JSON must not contain snake_case key %s\nfull JSON: %s", bad, s)
		}
	}
}
```

Also add a test for `parseAlertEventsResponse` with multi-page pagination (currently untested):

```go
func TestParseAlertEventsResponse_multiPage(t *testing.T) {
	raw := []byte(`{
		"events": [
			{"alertId": "abc123"},
			{"alertId": "abc123"},
			{"alertId": "def456"}
		],
		"pagination": {"nextPage": "page-token-2"}
	}`)
	counts := make(map[string]int)
	next, err := parseAlertEventsResponse(raw, counts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "page-token-2" {
		t.Errorf("nextPage: want page-token-2, got %q", next)
	}
	if counts["abc123"] != 2 {
		t.Errorf("abc123: want 2, got %d", counts["abc123"])
	}
	if counts["def456"] != 1 {
		t.Errorf("def456: want 1, got %d", counts["def456"])
	}
}

func TestParseAlertEventsResponse_lastPage(t *testing.T) {
	raw := []byte(`{"events": [{"alertId": "abc123"}], "pagination": {}}`)
	counts := make(map[string]int)
	next, err := parseAlertEventsResponse(raw, counts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "" {
		t.Errorf("nextPage: want empty string for last page, got %q", next)
	}
	if counts["abc123"] != 1 {
		t.Errorf("abc123: want 1, got %d", counts["abc123"])
	}
}
```

The `TestEventCountReqBody_camelCaseFieldNames` test references `eventCountReqBody` and `eventCountReqPagination` — these types don't exist yet, so the test will fail to compile, which is the failing state.

- [ ] **Step 2: Run test to confirm it fails to compile**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/coralogix/... -run TestEventCountReqBody_camelCaseFieldNames -v
```

Expected: compile error — `undefined: eventCountReqBody`

- [ ] **Step 3: Extract request structs to package level with camelCase tags**

In `backend/internal/coralogix/client.go`, find the `FetchAlertEventCounts` function (around line 97) and locate the inline type declarations:

```go
	type pagination struct {
		PageSize int    `json:"page_size"`
		Page     string `json:"page,omitempty"`
	}
	type reqBody struct {
		AlertIDs       []string `json:"alert_ids"`
		TimestampRange struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"timestamp_range"`
		Pagination pagination `json:"pagination"`
	}
```

Remove those inline type declarations from inside the function entirely. Instead, add three package-level types just before the `FetchAlertEventCounts` function declaration:

```go
// eventCountReqPagination is the pagination block sent in ListAlertEvents requests.
// Field names use proto3 camelCase JSON transcoding.
type eventCountReqPagination struct {
	PageSize int    `json:"pageSize"`
	Page     string `json:"page,omitempty"`
}

// eventCountTimestampRange is the time window for ListAlertEvents requests.
type eventCountTimestampRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// eventCountReqBody is the full request body for ListAlertEvents.
// Field names use proto3 camelCase JSON transcoding (alertIds, timestampRange, pageSize).
type eventCountReqBody struct {
	AlertIDs       []string                 `json:"alertIds"`
	TimestampRange eventCountTimestampRange `json:"timestampRange"`
	Pagination     eventCountReqPagination  `json:"pagination"`
}
```

Then inside `FetchAlertEventCounts`, update the body construction to use the new types. Find:

```go
	var nextPage string
	const pageSize = 1000

	for {
		var body reqBody
		body.AlertIDs = alertIDs
		body.TimestampRange.From = from.Format(time.RFC3339)
		body.TimestampRange.To = now.Format(time.RFC3339)
		body.Pagination.PageSize = pageSize
		body.Pagination.Page = nextPage
```

Replace with:

```go
	var nextPage string
	const pageSize = 1000

	for {
		var body eventCountReqBody
		body.AlertIDs = alertIDs
		body.TimestampRange.From = from.Format(time.RFC3339)
		body.TimestampRange.To = now.Format(time.RFC3339)
		body.Pagination.PageSize = pageSize
		body.Pagination.Page = nextPage
```

- [ ] **Step 4: Run the camelCase test to confirm it passes**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/coralogix/... -run TestEventCountReqBody_camelCaseFieldNames -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full coralogix test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/coralogix/... -v
```

Expected: all PASS including `TestParseAlertEventsResponse_multiPage` and `TestParseAlertEventsResponse_lastPage`.

- [ ] **Step 6: Run the full backend test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./...
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/coralogix/client.go backend/internal/coralogix/client_events_test.go
git commit -m "fix(noise): fix ListAlertEvents request to use proto3 camelCase field names (alertIds, timestampRange, pageSize)"
```

---

## Final verification

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./...
```

All packages must pass. Expected output shows `ok` for all packages with no skipped tests.
