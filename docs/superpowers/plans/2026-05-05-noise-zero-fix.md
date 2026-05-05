# Noise Always Zero — Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix noise detection returning 0 by removing `hasEvidenceOfVolume` from the structural noise gate and adding event-count match-rate logging to surface the behavioral fetch issue.

**Architecture:** Two targeted edits — one logic fix in `internal/similarity/engine.go` (remove one boolean from the structural condition, drop dead debug-switch case), one log line in `internal/coralogix/client.go` (after pagination loop). No new files, no API or model changes.

**Tech Stack:** Go 1.21, standard `log` package, existing test helpers (`sparseVector`, `makeAlert`) in `engine_test.go`.

---

### Task 1: Update broken test and add new failing tests

The existing test `TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped` asserts that an unscoped no-entity alert is NOT noisy when the event count map contains a different alert ID (triggerCount=0 due to ID mismatch). After the fix this alert MUST be structural noise — so that sub-test must be updated first, or it will pass for the wrong reason after the fix.

**Files:**
- Modify: `backend/internal/similarity/engine_test.go:430-454` (update existing test)
- Modify: `backend/internal/similarity/engine_test.go` (add 2 new tests after line 525)

- [ ] **Step 1: Update the first sub-test of `TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped`**

Open `backend/internal/similarity/engine_test.go`. Locate `TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped` (line ~430). Replace the first sub-test block (lines ~434–439) so it now expects structural noise:

```go
func TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped(t *testing.T) {
	v := sparseVector("Azure Audit - Access Review Deletion")
	alert := makeAlert("az-1", "logs_immediate", false, true, nil, "", "")

	// Event count map has data for a different ID — az-1 resolves to triggerCount=0.
	// After fix: structural fires regardless, because design is unscoped + no entity.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"other-id": 5}, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("logs_immediate unscoped should be structural even when triggerCount=0: got %v", noisy)
	}

	// 3 triggers, unscoped, no entity → structural noise.
	noisy = findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"az-1": 3}, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("logs_immediate with 3 triggers, unscoped should be structural: got %v", noisy)
	}

	// 25 triggers → both behavioral and structural.
	noisy = findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"az-1": 25}, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "both" {
		t.Errorf("logs_immediate with 25 triggers, unscoped should be both: got %v", noisy)
	}
}
```

- [ ] **Step 2: Update the stale comment in `TestFindNoiseAlerts_flowAlert_structuralAppliesWhenUnscoped`**

Locate `TestFindNoiseAlerts_flowAlert_structuralAppliesWhenUnscoped` (line ~517). Replace its comment:

```go
func TestFindNoiseAlerts_flowAlert_structuralAppliesWhenUnscoped(t *testing.T) {
	v := sparseVector("Flow No Triggers")
	alert := makeAlert("f-2", "flow", false, true, nil, "", "")
	// Unscoped flow with no entity → structural noise regardless of event counts.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("unscoped flow alert should be structural noise: got %v", noisy)
	}
}
```

- [ ] **Step 3: Add two new tests covering the empty-map case**

Append these two tests after `TestFindNoiseAlerts_flowAlert_structuralAppliesWhenUnscoped`:

```go
// TestFindNoiseAlerts_structuralNoise_emptyEventCountsMap covers the production bug:
// fetchEventCounts returns a non-nil empty map when the API call succeeds but returns
// no matching events. Under the old code hasEvidenceOfVolume=false blocked structural
// detection. After the fix, structural fires on design alone.
func TestFindNoiseAlerts_structuralNoise_emptyEventCountsMap(t *testing.T) {
	v := sparseVector("Unscoped Alert")
	alert := makeAlert("u-1", "logs_threshold", false, true, nil, "", "")
	// Non-nil empty map — the exact shape returned when the fetch succeeds but matches nothing.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{}, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert with empty event count map, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].NoiseType != "structural" {
		t.Errorf("noise_type: want structural, got %q", noisy[0].NoiseType)
	}
}

// TestFindNoiseAlerts_structuralNoise_scopedAlertWithEmptyMap verifies that removing
// hasEvidenceOfVolume does not cause false positives: a scoped alert with an empty
// event count map must still not be flagged as noisy.
func TestFindNoiseAlerts_structuralNoise_scopedAlertWithEmptyMap(t *testing.T) {
	v := sparseVector("Scoped Alert")
	alert := makeAlert("s-1", "logs_threshold", false, true, nil, "my-app", "auth")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{}, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("scoped alert with empty event count map should not be noisy, got %v", noisy)
	}
}
```

- [ ] **Step 4: Run the new/updated tests to confirm they fail**

```bash
cd backend && go test ./internal/similarity/... -run "TestFindNoiseAlerts_logsImmediate|TestFindNoiseAlerts_structuralNoise_emptyEventCountsMap|TestFindNoiseAlerts_structuralNoise_scopedAlertWithEmptyMap" -v
```

Expected: `TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped` FAIL on the first sub-test (got 0, want 1), `TestFindNoiseAlerts_structuralNoise_emptyEventCountsMap` FAIL (got 0, want 1). `TestFindNoiseAlerts_structuralNoise_scopedAlertWithEmptyMap` should PASS already (scoped alert was never broken).

---

### Task 2: Fix structural noise condition in `engine.go`

**Files:**
- Modify: `backend/internal/similarity/engine.go:1165-1195`

- [ ] **Step 1: Replace the Signal 2 block**

In `backend/internal/similarity/engine.go`, find the `// ── Signal 2: Structural` block (lines ~1165–1195) and replace it entirely:

```go
		// ── Signal 2: Structural ─────────────────────────────────────────
		// An alert is structurally noisy when it has no entity filter AND EITHER:
		//   (a) is unscoped (no app/subsystem), OR
		//   (b) has a broad query (wildcard OR low average IDF weight).
		// Trigger frequency (hasEvidenceOfVolume) is intentionally NOT required —
		// structural noise is a design-quality signal, not a frequency signal.
		// An unscoped, entity-less, broad-query alert is noisy by construction
		// regardless of whether it has fired recently.
		// isUnscoped and isBroadQuery default to false; they are only set (and
		// isStructural can only become true) when alert != nil.
		isStructural := false
		isUnscoped := false
		isBroadQuery := false
		if alert != nil {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
			isUnscoped = app == "" && sub == ""
			noEntity := len(v.entities) == 0
			isBroadQuery = hasWildcardQuery(v.luceneQuery) ||
				avgIDF(v.luceneQuery, idf.luceneQuery) < queryIDFThreshold
			isStructural = noEntity && (isUnscoped || isBroadQuery)

			if !isStructural && !isBehavioral {
				switch {
				case !noEntity:
					noSignalReasons["has_entity"]++
				case !isUnscoped && !isBroadQuery:
					noSignalReasons["scoped_specific_query"]++
				default:
					noSignalReasons["behavioral_below_threshold"]++
				}
			}
		} else if !isBehavioral {
			noSignalReasons["behavioral_below_threshold"]++
		}
```

Key changes from old code:
- Removed `hasEvidenceOfVolume` variable declaration and its use in `isStructural`
- Removed `case !hasEvidenceOfVolume: noSignalReasons["zero_triggers"]++` from the debug switch (dead code)
- Reordered debug switch: `scoped_specific_query` now comes before the default

- [ ] **Step 2: Run the full noise test suite**

```bash
cd backend && go test ./internal/similarity/... -run "TestFindNoiseAlerts" -v
```

Expected: ALL `TestFindNoiseAlerts_*` tests PASS. If any fail, the test name will identify which assertion broke — check that the alert in question is either scoped (`makeAlert(..., "my-app", ...)`) or has an entity (`featureVector.entities` non-empty).

- [ ] **Step 3: Run the full backend test suite to check for regressions**

```bash
cd backend && go test ./... 2>&1 | tail -20
```

Expected: `ok` for all packages. No compile errors, no test failures.

- [ ] **Step 4: Commit**

```bash
cd backend && git add internal/similarity/engine.go internal/similarity/engine_test.go
git commit -m "fix(noise): remove hasEvidenceOfVolume from structural noise gate

Structural noise is a design-quality signal (unscoped query, no entity
filter) — trigger frequency is irrelevant. The old hasEvidenceOfVolume
guard silently blocked structural detection whenever fetchEventCounts
returned a non-nil empty map, which happens when the ListAlertEvents
API call succeeds but returns no matching events (ID mismatch or
empty response). This caused noise to consistently show 0 for all clients.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Add match-rate diagnostic logging to `FetchAlertEventCounts`

This adds one log line to surface the behavioral fetch issue (event counts returning 0 despite alerts firing on the platform). No test needed — this is observability only.

**Files:**
- Modify: `backend/internal/coralogix/client.go:124-156`

- [ ] **Step 1: Add matched-count log after the pagination loop**

In `backend/internal/coralogix/client.go`, find the `return counts, nil` at the end of `FetchAlertEventCounts` (line ~155). Add the log line just before it:

```go
	matched := 0
	for _, c := range counts {
		if c > 0 {
			matched++
		}
	}
	log.Printf("INFO [noise] event counts: requested=%d matched=%d", len(alertIDs), matched)
	return counts, nil
```

The full end of the function after the pagination loop now looks like:

```go
	matched := 0
	for _, c := range counts {
		if c > 0 {
			matched++
		}
	}
	log.Printf("INFO [noise] event counts: requested=%d matched=%d", len(alertIDs), matched)
	return counts, nil
}
```

If `matched` is consistently 0 while `requested` is N (number of alerts), the log confirms the behavioral ID mismatch. The next investigation step is comparing a sample `alert.ID` value from `ListAlertDefs` against the `alertId` values appearing in `ListAlertEvents` raw responses.

- [ ] **Step 2: Build to confirm no compile errors**

```bash
cd backend && go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/coralogix/client.go
git commit -m "fix(noise): add event count match-rate logging to surface behavioral fetch issue

Logs requested vs matched alert ID count after ListAlertEvents pagination.
If matched=0 consistently, confirms ID format mismatch between ListAlertDefs
and ListAlertEvents — needed to diagnose behavioral noise returning 0.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Verification

After both tasks are complete, run the full suite one final time:

```bash
cd backend && go test ./... -count=1 2>&1 | tail -10
```

Expected output (all packages passing):
```
ok  	coralogix-alert-analyzer/internal/api	Xs
ok  	coralogix-alert-analyzer/internal/similarity	Xs
ok  	coralogix-alert-analyzer/internal/coralogix	Xs
...
```

Then start the server and run an analysis for a client with known active alerts. The Noise tab should now show structural noise results. The server logs will show:

```
INFO [noise] event counts: requested=N matched=M
```

If `matched=0`, the behavioral ID mismatch is confirmed and should be investigated separately.
