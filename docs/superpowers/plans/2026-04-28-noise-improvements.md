# Noise Detection Improvements — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix noise tab showing 0 alerts by lowering the behavioral threshold, removing the structural type whitelist, and adding a broad-query signal — plus UI filter pills to distinguish behavioral vs structural noise.

**Architecture:** Three changes to `engine.go`: (1) threshold 20→10, (2) remove `isHighVolumeType` gate so all alert types qualify for structural noise, (3) add `isBroadQuery` signal using wildcard detection + IDF-weighted query score. `Analyze` pre-computes the IDF threshold and passes it to `findNoiseAlerts`. Frontend adds `[All][Behavioral][Structural]` filter pills inside the Noise tab.

**Tech Stack:** Go (backend), React + TypeScript (frontend).

> **Phase 2 (LLM validation) is a separate plan.** This plan delivers a working, testable Phase 1.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/similarity/engine.go` | Add `hasWildcardQuery`, `avgIDF`, `computeQueryIDFThreshold`; update `findNoiseAlerts` signature + logic; update `buildNoiseReason`; update `Analyze` call site |
| `backend/internal/similarity/engine_test.go` | Add new tests; update 5 existing tests whose expectations change |
| `frontend/src/components/AlertInsights.tsx` | Add `noiseFilter` state + filter buttons + filtered list rendering |
| `frontend/src/App.css` | Styles for noise filter buttons |

---

## Task 1: Query analysis helpers

**Files:**
- Modify: `backend/internal/similarity/engine_test.go`
- Modify: `backend/internal/similarity/engine.go`

Context: `featureVector.luceneQuery` is `map[string]struct{}` (tokenized query terms). `idfTable.luceneQuery` is `map[string]float64` (term → IDF weight). `idfWeight(t, idf)` returns 1.0 for unknown tokens.

- [ ] **Step 1: Write failing tests**

Add to `backend/internal/similarity/engine_test.go` (after the last test in the file):

```go
// ── Query analysis helpers ─────────────────────────────────────────────────

func TestHasWildcardQuery_withWildcard(t *testing.T) {
	tokens := map[string]struct{}{"severity": {}, "error*": {}}
	if !hasWildcardQuery(tokens) {
		t.Error("expected true for token containing *")
	}
}

func TestHasWildcardQuery_withExists(t *testing.T) {
	tokens := map[string]struct{}{"_exists_": {}}
	if !hasWildcardQuery(tokens) {
		t.Error("expected true for _exists_ token")
	}
}

func TestHasWildcardQuery_withoutWildcard(t *testing.T) {
	tokens := map[string]struct{}{"severity": {}, "error": {}, "okta": {}}
	if hasWildcardQuery(tokens) {
		t.Error("expected false for specific tokens")
	}
}

func TestHasWildcardQuery_empty(t *testing.T) {
	if hasWildcardQuery(map[string]struct{}{}) {
		t.Error("expected false for empty token set")
	}
}

func TestAvgIDF_emptyReturns1(t *testing.T) {
	got := avgIDF(map[string]struct{}{}, nil)
	if got != 1.0 {
		t.Errorf("avgIDF empty: want 1.0, got %f", got)
	}
}

func TestAvgIDF_unknownTokensReturn1(t *testing.T) {
	tokens := map[string]struct{}{"raretoken": {}}
	// idfWeight returns 1.0 for tokens not in table
	got := avgIDF(tokens, map[string]float64{})
	if got != 1.0 {
		t.Errorf("avgIDF unknown token: want 1.0, got %f", got)
	}
}

func TestAvgIDF_knownTokens(t *testing.T) {
	tokens := map[string]struct{}{"error": {}, "failed": {}}
	idf := map[string]float64{"error": 0.2, "failed": 0.4}
	got := avgIDF(tokens, idf)
	want := 0.3 // (0.2 + 0.4) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("avgIDF: want %f, got %f", want, got)
	}
}

func TestComputeQueryIDFThreshold_p25(t *testing.T) {
	// 4 vectors with avgIDF scores: 0.1, 0.3, 0.7, 0.9 (after sorting)
	// p25 index = floor(4 * 0.25) = 1 → score at index 1 = 0.3
	// But computeQueryIDFThreshold uses idf.luceneQuery weights on vector.luceneQuery tokens.
	// Easiest: use vectors with known single tokens and matching idf table.
	idf := idfTable{luceneQuery: map[string]float64{"a": 0.1, "b": 0.3, "c": 0.7, "d": 0.9}}
	vectors := []featureVector{
		{luceneQuery: map[string]struct{}{"c": {}}}, // avgIDF 0.7
		{luceneQuery: map[string]struct{}{"a": {}}}, // avgIDF 0.1
		{luceneQuery: map[string]struct{}{"d": {}}}, // avgIDF 0.9
		{luceneQuery: map[string]struct{}{"b": {}}}, // avgIDF 0.3
	}
	threshold := computeQueryIDFThreshold(vectors, idf)
	want := 0.3 // p25 index=1 after sort [0.1, 0.3, 0.7, 0.9]
	if math.Abs(threshold-want) > 1e-9 {
		t.Errorf("computeQueryIDFThreshold: want %f, got %f", want, threshold)
	}
}

func TestComputeQueryIDFThreshold_emptyVectors(t *testing.T) {
	threshold := computeQueryIDFThreshold([]featureVector{}, idfTable{})
	if threshold != 0 {
		t.Errorf("empty vectors: want 0, got %f", threshold)
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd backend && go test ./internal/similarity/... -run "TestHasWildcard|TestAvgIDF|TestComputeQuery" -v 2>&1
```

Expected: `FAIL — undefined: hasWildcardQuery`

- [ ] **Step 3: Add helpers to `engine.go`**

Add these three functions in `backend/internal/similarity/engine.go`, right before `findNoiseAlerts` (around line 940):

```go
// hasWildcardQuery reports whether any token in the query set contains a
// wildcard character (*) or is the Elasticsearch exists operator (_exists_).
func hasWildcardQuery(tokens map[string]struct{}) bool {
	for t := range tokens {
		if strings.Contains(t, "*") || t == "_exists_" {
			return true
		}
	}
	return false
}

// avgIDF returns the mean IDF weight of the query tokens using the provided
// IDF table. Returns 1.0 (maximum specificity) for an empty token set so that
// alerts with no query are never treated as broad.
func avgIDF(tokens map[string]struct{}, idf map[string]float64) float64 {
	if len(tokens) == 0 {
		return 1.0
	}
	var sum float64
	for t := range tokens {
		sum += idfWeight(t, idf)
	}
	return sum / float64(len(tokens))
}

// computeQueryIDFThreshold returns the 25th-percentile average query IDF score
// across all vectors. Alerts whose query IDF falls below this are considered
// broad (their query uses predominantly common tokens).
// Returns 0 for an empty vector slice.
func computeQueryIDFThreshold(vectors []featureVector, idf idfTable) float64 {
	if len(vectors) == 0 {
		return 0
	}
	scores := make([]float64, len(vectors))
	for i, v := range vectors {
		scores[i] = avgIDF(v.luceneQuery, idf.luceneQuery)
	}
	sort.Float64s(scores)
	p25 := int(math.Floor(float64(len(scores)) * 0.25))
	if p25 >= len(scores) {
		return 0
	}
	return scores[p25]
}
```

Confirm `"math"` and `"sort"` are already imported (they are — `math` is used by `buildIDF`, `sort` is used by `findNoiseAlerts`).

- [ ] **Step 4: Run tests — confirm they pass**

```bash
cd backend && go test ./internal/similarity/... -run "TestHasWildcard|TestAvgIDF|TestComputeQuery" -v 2>&1
```

Expected: all PASS.

- [ ] **Step 5: Full build check**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(noise): add hasWildcardQuery, avgIDF, computeQueryIDFThreshold helpers"
```

---

## Task 2: Update `findNoiseAlerts` — new threshold, no type whitelist, broad-query signal

**Files:**
- Modify: `backend/internal/similarity/engine.go` (lines 942, 962–1076, 1093–1110, 221)
- Modify: `backend/internal/similarity/engine_test.go` (update 5 tests + add 6 new ones)

Context: The current `findNoiseAlerts` signature is:
```go
func findNoiseAlerts(vectors []featureVector, alerts []*models.AlertDef, eventCounts map[string]int, integrationCount int) []models.NoiseAlert
```
It will gain two new params. All existing test call sites must be updated.

The current `Analyze` calls it at line 221:
```go
noiseAlerts := findNoiseAlerts(vectors, alerts, eventCounts, integrationCount)
```
And `idf` is already computed at line 192.

- [ ] **Step 1: Update existing tests that will break**

In `backend/internal/similarity/engine_test.go`, make these changes:

**1a. Update `TestFindNoiseAlerts_nilInput` (line 13) — add new params:**
```go
func TestFindNoiseAlerts_nilInput(t *testing.T) {
	noisy := findNoiseAlerts(nil, nil, nil, 0, idfTable{}, 0)
	if noisy != nil {
		t.Errorf("expected nil for nil input, got %v", noisy)
	}
}
```

**1b. Update all calls in the exclusion tests** — `vendorCoveredExcluded`, `buildingBlockExcluded`, `nonSecurityExcluded`, `structuralNoise_unscopedHighVolume`, `structuralNoise_scopedAlertNotNoisy`:

These tests use `findNoiseAlerts(..., nil, 0)`. Add `idfTable{}, 0` at the end of every call. Example:
```go
noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
```
Apply this pattern to **every** `findNoiseAlerts` call in the file.

**1c. Update `TestFindNoiseAlerts_structuralNoise_lowVolumeTypeNotNoisy`** — `logs_anomaly` is now structural noise (no type whitelist). Change expectation:
```go
func TestFindNoiseAlerts_structuralNoise_logsAnomalyNowStructural(t *testing.T) {
	v := sparseVector("Anomaly Alert")
	alert := makeAlert("t-3", "logs_anomaly", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	// logs_anomaly is no longer excluded — unscoped + no entity = structural noise
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert for unscoped logs_anomaly, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "structural" {
		t.Errorf("noise_type: want structural, got %q", noisy[0].NoiseType)
	}
}
```
(Rename the function — the old name implied it should NOT be noisy.)

**1d. Update `TestFindNoiseAlerts_structuralNoise_logsImmediate_neverStructural`** — logs_immediate IS now structural when unscoped+no entity+evidence of volume:
```go
func TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped(t *testing.T) {
	v := sparseVector("Azure Audit - Access Review Deletion")
	alert := makeAlert("az-1", "logs_immediate", false, true, nil, "", "")

	// No activity in 30 days → no evidence of volume → not structural, not behavioral.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"other-id": 5}, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("logs_immediate with zero triggers should not be noisy: got %v", noisy)
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

**1e. Update `TestFindNoiseAlerts_flowAlert_structuralDoesNotApply`** — flow IS now structural when unscoped+no entity:
```go
func TestFindNoiseAlerts_flowAlert_structuralAppliesWhenUnscoped(t *testing.T) {
	v := sparseVector("Flow No Triggers")
	alert := makeAlert("f-2", "flow", false, true, nil, "", "")
	// nil eventCounts → hasEvidenceOfVolume = true → structural
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("unscoped flow alert should be structural noise: got %v", noisy)
	}
}
```

**1f. Update `TestFindNoiseAlerts_behavioralNoise_atThresholdNotNoisy`** — threshold is now 10, so count=20 IS noisy. Change count to 10:
```go
func TestFindNoiseAlerts_behavioralNoise_atThresholdNotNoisy(t *testing.T) {
	v := sparseVector("Borderline Alert")
	alert := makeAlert("b-2", "logs_threshold", false, true, nil, "my-app", "")
	counts := map[string]int{"b-2": 10}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	// 10 is not > 10, so not behavioral. Has app scoping and empty query, so not structural.
	if len(noisy) != 0 {
		t.Errorf("alert at threshold (10) should not be noisy, got %v", noisy)
	}
}
```

Also update `TestFindNoiseAlerts_behavioralNoise_overThreshold` — count 25 is still > 10, but it's scoped (my-app/auth). Add `idfTable{}, 0`:
```go
noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
```

Update remaining calls: `bothSignals`, `flowAlert_behavioralApplies`, `orgAmplifier_reasonContainsCount`, `sortedByName`, `missingFeaturesPopulated`, and all others — add `idfTable{}, 0` at the end.

- [ ] **Step 2: Add new tests for new behavior**

Append to `engine_test.go`:

```go
func TestFindNoiseAlerts_behavioralThreshold_11IsNoisy(t *testing.T) {
	v := sparseVector("Chatty Flow")
	alert := makeAlert("bf-1", "flow", false, true, nil, "app", "sub")
	counts := map[string]int{"bf-1": 11}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "behavioral" {
		t.Errorf("11 triggers should be behavioral noise (>10), got %v", noisy)
	}
}

func TestFindNoiseAlerts_broadQuery_wildcard_structural(t *testing.T) {
	v := featureVector{
		alertName:   "Broad Wildcard Alert",
		luceneQuery: map[string]struct{}{"severity*": {}},
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	// Scoped alert (has appName) — would normally be excluded from structural.
	// But wildcard query makes it broad → structural noise.
	alert := makeAlert("bq-1", "logs_immediate", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("scoped alert with wildcard query should be structural noise: got %v", noisy)
	}
}

func TestFindNoiseAlerts_broadQuery_lowIDF_structural(t *testing.T) {
	idf := idfTable{luceneQuery: map[string]float64{"error": 0.1, "failed": 0.1}}
	v := featureVector{
		alertName:   "Generic Scoped Alert",
		luceneQuery: map[string]struct{}{"error": {}, "failed": {}}, // avgIDF = 0.1
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	// Scoped alert — but query avgIDF (0.1) is below threshold (0.2).
	alert := makeAlert("bq-2", "logs_threshold", false, true, nil, "my-app", "")
	// threshold=0.2 means avgIDF(0.1) < 0.2 → broad
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idf, 0.2)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("scoped alert with low-IDF query should be structural noise: got %v", noisy)
	}
}

func TestFindNoiseAlerts_specificQuery_notStructural(t *testing.T) {
	idf := idfTable{luceneQuery: map[string]float64{"admin": 0.9, "delete": 0.8}}
	v := featureVector{
		alertName:   "Specific Scoped Alert",
		luceneQuery: map[string]struct{}{"admin": {}, "delete": {}}, // avgIDF = 0.85
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	// Scoped alert, high-IDF query → NOT structural noise (avgIDF 0.85 > threshold 0.2).
	alert := makeAlert("bq-3", "logs_threshold", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idf, 0.2)
	if len(noisy) != 0 {
		t.Errorf("scoped alert with high-IDF query should NOT be structural noise: got %v", noisy)
	}
}

func TestFindNoiseAlerts_broadQueryReason(t *testing.T) {
	v := featureVector{
		alertName:   "Wildcard Alert",
		luceneQuery: map[string]struct{}{"*": {}},
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	alert := makeAlert("bq-4", "logs_threshold", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noise alert, got %d", len(noisy))
	}
	if !strings.Contains(noisy[0].Reason, "Broad query") {
		t.Errorf("broad-query reason should mention 'Broad query', got: %q", noisy[0].Reason)
	}
}
```

- [ ] **Step 3: Run tests — confirm they fail as expected**

```bash
cd backend && go test ./internal/similarity/... -run "TestFindNoiseAlerts" -v 2>&1 | tail -30
```

Expected: multiple FAIL with `too many arguments in call to findNoiseAlerts` and assertion failures for updated tests.

- [ ] **Step 4: Update `findNoiseAlerts` in `engine.go`**

**4a. Change threshold constant (line 942):**
```go
const behavioralNoiseThreshold = 10 // triggers in lookback window before alert is behaviorally noisy
```

**4b. Update function signature (line 962):**
```go
func findNoiseAlerts(
	vectors []featureVector,
	alerts []*models.AlertDef,
	eventCounts map[string]int,
	integrationCount int,
	idf idfTable,
	queryIDFThreshold float64,
) []models.NoiseAlert {
```

**4c. Replace the structural signal block (lines 1010–1050) with:**
```go
		// ── Signal 2: Structural ─────────────────────────────────────────────
		// An alert is structurally noisy when it has no entity filter, evidence
		// of volume, and EITHER:
		//   (a) is unscoped (no app/subsystem), OR
		//   (b) has a broad query (wildcard OR low average IDF weight).
		// When event count data is available we additionally require
		// triggerCount > 0 to avoid false positives on dormant alerts.
		isStructural := false
		isUnscoped := false
		isBroadQuery := false
		if alert != nil {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
			isUnscoped = app == "" && sub == ""
			noEntity := len(v.entities) == 0
			isBroadQuery = hasWildcardQuery(v.luceneQuery) ||
				avgIDF(v.luceneQuery, idf.luceneQuery) < queryIDFThreshold
			hasEvidenceOfVolume := eventCounts == nil || triggerCount > 0
			isStructural = noEntity && hasEvidenceOfVolume && (isUnscoped || isBroadQuery)

			if !isStructural && !isBehavioral {
				switch {
				case !noEntity:
					noSignalReasons["has_entity"]++
				case !hasEvidenceOfVolume:
					noSignalReasons["zero_triggers"]++
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

**4d. Update the `noisy` append call (was using `isStructural` for reason):**
```go
		noisy = append(noisy, models.NoiseAlert{
			Name:            v.alertName,
			MissingFeatures: buildMissingFeatures(v),
			Reason:          buildNoiseReason(triggerCount, integrationCount, isBehavioral, isUnscoped, isBroadQuery),
			TriggerCount:    triggerCount,
			NoiseType:       noiseTypeString(isBehavioral, isStructural),
		})
```

**4e. Update `buildNoiseReason` signature and body (lines 1093–1110):**
```go
// buildNoiseReason returns a human-readable explanation for why the alert was flagged.
// isUnscoped and isBroadQuery are the two sub-conditions of the structural signal.
func buildNoiseReason(triggerCount, integrationCount int, isBehavioral, isUnscoped, isBroadQuery bool) string {
	var parts []string
	if isBehavioral {
		parts = append(parts, fmt.Sprintf(
			"Fired %d times in the last 30 days — alert is over-triggering.", triggerCount))
	}
	if isUnscoped || isBroadQuery {
		if isUnscoped {
			if integrationCount >= 10 {
				parts = append(parts, fmt.Sprintf(
					"No app/subsystem scoping across an org with %d integrations — fires on all matching log sources.",
					integrationCount))
			} else {
				parts = append(parts, "No app/subsystem scoping and no entity filter — alert may fire too broadly.")
			}
		} else {
			parts = append(parts, "Broad query with no entity filter — fires on matching events across all log sources.")
		}
	}
	return strings.Join(parts, " ")
}
```

**4f. Update `Analyze` call site (line 221):**
```go
	// Step 7: Noise detection.
	queryIDFThreshold := computeQueryIDFThreshold(vectors, idf)
	noiseAlerts := findNoiseAlerts(vectors, alerts, eventCounts, integrationCount, idf, queryIDFThreshold)
```

- [ ] **Step 5: Run tests — all must pass**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | tail -40
```

Expected: all PASS. Count should be ≥ 35 tests.

- [ ] **Step 6: Full build**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(noise): lower threshold to 10, remove type whitelist, add broad-query structural signal"
```

---

## Task 3: Frontend — Noise tab filter pills

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`
- Modify: `frontend/src/App.css`

Context: The Noise tab renders at `{activeTab === 'noise' && (...)}`. Each `NoiseAlert` has `noise_type: 'behavioral' | 'structural' | 'both'`. The lookback `NoisePills` component already renders at the top of the tab. Filter pills go below it.

Read lines 540–600 of `AlertInsights.tsx` before editing to confirm exact structure.

- [ ] **Step 1: Add `noiseFilter` state**

In `frontend/src/components/AlertInsights.tsx`, add state after the existing `expandedQueries` state (around line 52):

```tsx
const [noiseFilter, setNoiseFilter] = useState<'all' | 'behavioral' | 'structural'>('all');
```

- [ ] **Step 2: Add filter buttons and filtered list to Noise tab**

Find the Noise tab block (starts with `{activeTab === 'noise' && (`). Replace the `NoisePills` wrapper and the data.noise_alerts list with:

```tsx
{activeTab === 'noise' && (
  <>
    <div style={{ marginBottom: 12 }}>
      <NoisePills days={lookbackDays} onChange={onReanalyze} disabled={isRegenerating} />
    </div>
    <div className="noise-filter-pills">
      {(['all', 'behavioral', 'structural'] as const).map(f => (
        <button
          key={f}
          className={`noise-filter-pill${noiseFilter === f ? ' noise-filter-pill--active' : ''}`}
          onClick={() => setNoiseFilter(f)}
        >
          {f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}
        </button>
      ))}
    </div>
    {(() => {
      const filtered = (data.noise_alerts ?? []).filter(n => {
        if (noiseFilter === 'all') return true;
        if (noiseFilter === 'behavioral') return n.noise_type === 'behavioral' || n.noise_type === 'both';
        return n.noise_type === 'structural' || n.noise_type === 'both';
      });
      return filtered.length ? (
        filtered.map((noise: NoiseAlert, i) => {
```

Then close the IIFE properly — replace the closing of the existing `data.noise_alerts.map` block:

```tsx
        })
      ) : (
        <div className="state-empty">
          <div className="state-empty__icon">◎</div>
          <div className="state-empty__title">
            {noiseFilter === 'all' ? 'No rule-confirmed noisy alerts' : `No ${noiseFilter} noise alerts`}
          </div>
          <div className="state-empty__body">
            {noiseFilter === 'all'
              ? 'No alerts exceeded the behavioral or structural noise thresholds.'
              : `No alerts matched the ${noiseFilter} noise filter.`}
          </div>
        </div>
      );
    })()}
  </>
)}
```

- [ ] **Step 3: Add CSS for filter pills**

Append to `frontend/src/App.css`:

```css
/* Noise tab type filter pills */
.noise-filter-pills { display: flex; gap: 4px; margin-bottom: 12px; }
.noise-filter-pill { font-family: var(--font-mono); font-size: 0.62rem; letter-spacing: 0.05em; text-transform: uppercase; padding: 3px 10px; border-radius: var(--radius-sm); border: 1px solid var(--border-bright); background: transparent; color: var(--text-sec); cursor: pointer; transition: background 0.12s, color 0.12s, border-color 0.12s; }
.noise-filter-pill:hover { background: var(--surface-2); color: var(--text); }
.noise-filter-pill--active { background: var(--surface-2); border-color: var(--accent); color: var(--accent); font-weight: 600; }
```

- [ ] **Step 4: TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/AlertInsights.tsx frontend/src/App.css
git commit -m "feat(noise): add behavioral/structural filter pills to Noise tab"
```

---

## Self-Review

**Spec coverage:**
- ✅ Behavioral threshold 20→10 → Task 2
- ✅ Remove type whitelist → Task 2
- ✅ Remove flow structural exclusion → Task 2
- ✅ `hasWildcardQuery` → Task 1
- ✅ `avgIDF` → Task 1
- ✅ `computeQueryIDFThreshold` (p25) → Task 1
- ✅ `isBroadQuery = hasWildcard OR avgIDF < threshold` → Task 2
- ✅ `findNoiseAlerts` new signature with `idf` + `queryIDFThreshold` → Task 2
- ✅ `Analyze` pre-computes threshold, passes to `findNoiseAlerts` → Task 2
- ✅ `buildNoiseReason` updated for broad-query structural path → Task 2
- ✅ UI filter pills [All][Behavioral][Structural] → Task 3
- ✅ `noiseFilter` state → Task 3
- ✅ Empty state per filter → Task 3

**Placeholder scan:** No TBDs, no vague steps, all code shown.

**Type consistency:**
- `hasWildcardQuery(tokens map[string]struct{}) bool` — used in Task 1 tests and Task 2 implementation ✅
- `avgIDF(tokens map[string]struct{}, idf map[string]float64) float64` — consistent across Task 1 tests and Task 2 ✅
- `computeQueryIDFThreshold(vectors []featureVector, idf idfTable) float64` — consistent ✅
- `buildNoiseReason(triggerCount, integrationCount int, isBehavioral, isUnscoped, isBroadQuery bool) string` — defined in Task 2 step 4e, called in Task 2 step 4d ✅
- `noiseFilter` state type `'all' | 'behavioral' | 'structural'` — consistent throughout Task 3 ✅

> **Phase 2 (LLM validation + NeonDB cache)** is a separate plan. Implement Phase 1 first and verify noise alerts appear before building the validation layer on top.
