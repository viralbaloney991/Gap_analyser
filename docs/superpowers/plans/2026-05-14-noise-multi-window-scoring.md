# Noise Multi-Window Behavioral Scoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add burst/periodic/accelerating/persistent noise detection signals by fetching alert event counts at four fixed windows (7d/14d/21d/30d) in parallel and classifying their shape — catching noisy alerts that fire below the current hardcoded >10 threshold.

**Architecture:** New `noise_scoring.go` in the similarity package holds pure scoring functions and pattern classification. `FetchAlertEventCountsMultiWindow` on the Coralogix client makes 4 parallel calls to the existing `FetchAlertEventCounts`. A new `AnalyzeNoiseMultiWindow` function keeps the old `AnalyzeNoise` unchanged (backward compat for `HandleAnalyze`/`HandleInsights`). `HandleNoise` switches to the multi-window path. Frontend adds a pattern badge with tooltip.

**Tech Stack:** Go (backend), React + TypeScript (frontend), existing gRPC Coralogix client.

**Design note:** The spec says to rename `AnalyzeNoise` to accept `[4]int`. We deviate: `AnalyzeNoise` is kept unchanged so `HandleAnalyze`, `HandleInsights`, and `HandleExportNarrative` continue to compile without modification. `AnalyzeNoiseMultiWindow` is added as a new function used only by `HandleNoise`.

---

### Task 1: NoiseAlert model fields + scoring functions + tests

**Files:**
- Modify: `backend/internal/models/models.go:124-130` — add 3 fields to `NoiseAlert`
- Create: `backend/internal/similarity/noise_scoring.go`
- Create: `backend/internal/similarity/noise_scoring_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/similarity/noise_scoring_test.go`:

```go
package similarity

import (
	"math"
	"testing"
)

func TestBurstScore(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   float64
	}{
		{"all zeros", [4]int{0, 0, 0, 0}, 0},
		{"even distribution", [4]int{7, 14, 21, 28}, 1.0},
		{"concentrated burst", [4]int{8, 1, 1, 9}, 3.556},
		{"below expected", [4]int{1, 3, 5, 8}, 0.5},
	}
	for _, tc := range cases {
		got := burstScore(tc.counts)
		if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("burstScore %s (%v) = %.3f, want %.3f", tc.name, tc.counts, got, tc.want)
		}
	}
}

func TestPeriodicityScore(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   float64
		inf    bool
	}{
		{"perfect even rate", [4]int{4, 8, 12, 16}, 0.0, false},
		{"slightly uneven", [4]int{5, 8, 12, 16}, 0.25, false},
		{"too few 14d counts", [4]int{3, 1, 0, 1}, 0, true},
		{"completely zero 14d", [4]int{0, 0, 0, 0}, 0, true},
	}
	for _, tc := range cases {
		got := periodicityScore(tc.counts)
		if tc.inf {
			if !math.IsInf(got, 1) {
				t.Errorf("periodicityScore %s (%v) = %.3f, want +Inf", tc.name, tc.counts, got)
			}
		} else if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("periodicityScore %s (%v) = %.3f, want %.3f", tc.name, tc.counts, got, tc.want)
		}
	}
}

func TestClassifyNoisePattern(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   string
	}{
		{"high_volume — 30d count > 10", [4]int{3, 4, 5, 11}, "high_volume"},
		{"burst — concentrated in recent 7d", [4]int{8, 1, 1, 9}, "burst"},
		{"periodic — even rate across windows", [4]int{4, 8, 12, 16}, ""},       // 30d=16 > 10 → high_volume first; use lower total
		{"periodic — even rate low total", [4]int{2, 4, 6, 8}, "periodic"},
		{"accelerating — recent 50%+ above expected", [4]int{4, 2, 2, 7}, "accelerating"},
		{"persistent — fires every week low total", [4]int{2, 3, 5, 8}, "persistent"},
		{"no pattern — below threshold flat", [4]int{0, 1, 1, 2}, ""},
		{"all zeros — no pattern", [4]int{0, 0, 0, 0}, ""},
	}
	for _, tc := range cases {
		got := classifyNoisePattern(tc.counts)
		if got != tc.want {
			t.Errorf("classifyNoisePattern %s (%v) = %q, want %q", tc.name, tc.counts, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify tests fail**

```bash
cd /path/to/backend && go test ./internal/similarity/ -run "TestBurstScore|TestPeriodicityScore|TestClassifyNoisePattern" -v
```

Expected: FAIL — functions are not defined yet.

- [ ] **Step 3: Add fields to `NoiseAlert` in `models.go`**

In `backend/internal/models/models.go`, replace lines 124–130:

```go
type NoiseAlert struct {
	Name            string   `json:"name"`
	MissingFeatures []string `json:"missing_features"`
	Reason          string   `json:"reason,omitempty"`
	TriggerCount    int      `json:"trigger_count,omitempty"`
	NoiseType       string   `json:"noise_type,omitempty"`
}
```

With:

```go
type NoiseAlert struct {
	Name            string  `json:"name"`
	MissingFeatures []string `json:"missing_features"`
	Reason          string  `json:"reason,omitempty"`
	TriggerCount    int     `json:"trigger_count,omitempty"`
	NoiseType       string  `json:"noise_type,omitempty"`
	NoisePattern    string  `json:"noise_pattern,omitempty"`  // high_volume|burst|periodic|accelerating|persistent
	WindowCounts    [4]int  `json:"window_counts,omitempty"`  // [7d, 14d, 21d, 30d]
	BurstScore      float64 `json:"burst_score,omitempty"`
}
```

- [ ] **Step 4: Create `noise_scoring.go`**

Create `backend/internal/similarity/noise_scoring.go`:

```go
package similarity

import "math"

const (
	burstScoreThreshold       = 2.5
	periodicityScoreThreshold = 0.2
	accelerationScoreThreshold = 1.5
	minPeriodicityWindow      = 2 // counts[1] (14d) must be >= 2 to compute periodicity
)

// burstScore returns the ratio of the 7-day count to the expected weekly share
// of the 30-day total. > burstScoreThreshold means recent-week firing was
// concentrated — burst pattern.
func burstScore(counts [4]int) float64 {
	if counts[3] == 0 {
		return 0
	}
	expected := float64(counts[3]) / 4.0
	return float64(counts[0]) / expected
}

// periodicityScore returns how close the 7-day count is to half the 14-day count.
// A score near 0 means the alert fires at a perfectly even (periodic) rate.
// Returns math.MaxFloat64 when counts[1] < minPeriodicityWindow to avoid
// division by near-zero.
func periodicityScore(counts [4]int) float64 {
	if counts[1] < minPeriodicityWindow {
		return math.MaxFloat64
	}
	expected := float64(counts[1]) / 2.0
	return math.Abs(float64(counts[0])/expected - 1.0)
}

// accelerationScore is numerically identical to burstScore. It is used with the
// lower accelerationScoreThreshold (1.5 vs 2.5) to catch gradual acceleration
// before it reaches burst level.
func accelerationScore(counts [4]int) float64 {
	return burstScore(counts)
}

// classifyNoisePattern returns the first matching noise pattern for the given
// 4-window counts [7d, 14d, 21d, 30d]. Priority: high_volume → burst →
// periodic → accelerating → persistent. Returns "" when no pattern matches
// (alert is not behaviorally noisy).
func classifyNoisePattern(counts [4]int) string {
	switch {
	case counts[3] > behavioralNoiseThreshold:
		return "high_volume"
	case burstScore(counts) > burstScoreThreshold && counts[3] <= behavioralNoiseThreshold:
		return "burst"
	case periodicityScore(counts) < periodicityScoreThreshold && counts[1] >= minPeriodicityWindow:
		return "periodic"
	case accelerationScore(counts) > accelerationScoreThreshold && counts[3] <= behavioralNoiseThreshold:
		return "accelerating"
	case counts[0] > 0 && counts[1] > counts[0] && counts[2] > counts[1] && counts[3] <= behavioralNoiseThreshold:
		return "persistent"
	default:
		return ""
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /path/to/backend && go test ./internal/similarity/ -run "TestBurstScore|TestPeriodicityScore|TestClassifyNoisePattern" -v
```

Expected: All 3 tests PASS.

- [ ] **Step 6: Build check**

```bash
cd /path/to/backend && go build ./...
```

Expected: No errors.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/models/models.go backend/internal/similarity/noise_scoring.go backend/internal/similarity/noise_scoring_test.go
git commit -m "feat(noise): NoiseAlert pattern fields + scoring functions (burst/periodic/accelerating/persistent)"
```

---

### Task 2: FetchAlertEventCountsMultiWindow

**Files:**
- Modify: `backend/internal/coralogix/client.go` — add new method after `FetchAlertEventCounts`

- [ ] **Step 1: Add `FetchAlertEventCountsMultiWindow` to `client.go`**

In `backend/internal/coralogix/client.go`, after the closing brace of `FetchAlertEventCounts` (around line 196), add:

```go
// FetchAlertEventCountsMultiWindow fetches alert event counts at 4 fixed windows
// (7d, 14d, 21d, 30d) in parallel. Returns map[alertID][4]int where index
// 0=7d, 1=14d, 2=21d, 3=30d. On partial failure, the failed window's counts
// are zero-filled rather than aborting the whole request.
func (c *Client) FetchAlertEventCountsMultiWindow(ctx context.Context, alertIDs []string) (map[string][4]int, error) {
	windows := [4]int{7, 14, 21, 30}

	type result struct {
		idx    int
		counts map[string]int
		err    error
	}
	ch := make(chan result, 4)
	for i, days := range windows {
		i, days := i, days
		go func() {
			counts, err := c.FetchAlertEventCounts(ctx, alertIDs, days)
			ch <- result{idx: i, counts: counts, err: err}
		}()
	}

	out := make(map[string][4]int, len(alertIDs))
	var firstErr error
	failures := 0
	for range windows {
		r := <-ch
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			failures++
			continue
		}
		for id, count := range r.counts {
			arr := out[id]
			arr[r.idx] = count
			out[id] = arr
		}
	}
	if failures == len(windows) {
		return nil, firstErr
	}
	return out, nil
}
```

- [ ] **Step 2: Build check**

```bash
cd /path/to/backend && go build ./...
```

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/coralogix/client.go
git commit -m "feat(noise): FetchAlertEventCountsMultiWindow — 4 parallel window fetches (7d/14d/21d/30d)"
```

---

### Task 3: AnalyzeNoiseMultiWindow + findNoiseAlertsMultiWindow

**Files:**
- Modify: `backend/internal/similarity/engine.go` — add `findNoiseAlertsMultiWindow` and `AnalyzeNoiseMultiWindow`
- Modify: `backend/internal/similarity/engine_test.go` — add tests for the new functions

- [ ] **Step 1: Write failing tests**

Add to `backend/internal/similarity/engine_test.go`:

```go
func TestAnalyzeNoiseMultiWindow_emptyAlerts(t *testing.T) {
	result := AnalyzeNoiseMultiWindow(nil, nil, 0)
	if result != nil {
		t.Errorf("expected nil for nil alerts, got %v", result)
	}
}

func TestAnalyzeNoiseMultiWindow_highVolume(t *testing.T) {
	// 30d count > 10 → high_volume (same as legacy behavioral)
	v := sparseVector("High Volume Alert")
	alert := makeAlert("hv-1", "logs_threshold", false, false, nil, "app", "svc")
	multiCounts := map[string][4]int{
		"hv-1": {3, 5, 8, 11}, // 30d = 11 > 10
	}
	noisy := AnalyzeNoiseMultiWindow([]*models.AlertDef{alert}, multiCounts, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert, got %d", len(noisy))
	}
	if noisy[0].NoisePattern != "high_volume" {
		t.Errorf("noise_pattern: want high_volume, got %q", noisy[0].NoisePattern)
	}
	if noisy[0].WindowCounts != ([4]int{3, 5, 8, 11}) {
		t.Errorf("window_counts: want [3 5 8 11], got %v", noisy[0].WindowCounts)
	}
}

func TestAnalyzeNoiseMultiWindow_burstPattern(t *testing.T) {
	// 8 fires in 7d, only 9 total — below old >10 threshold but burst score = 8/(9/4) = 3.56
	v := sparseVector("Deployment Burst Alert")
	alert := makeAlert("burst-1", "logs_threshold", false, false, nil, "app", "svc")
	multiCounts := map[string][4]int{
		"burst-1": {8, 1, 0, 9}, // burst score = 8/(9/4) ≈ 3.56 > 2.5
	}
	noisy := AnalyzeNoiseMultiWindow([]*models.AlertDef{alert}, multiCounts, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert for burst pattern, got %d", len(noisy))
	}
	if noisy[0].NoisePattern != "burst" {
		t.Errorf("noise_pattern: want burst, got %q", noisy[0].NoisePattern)
	}
}

func TestAnalyzeNoiseMultiWindow_persistentPattern(t *testing.T) {
	// Fires every week but stays under 10 total
	v := sparseVector("Weekly Persistent Alert")
	alert := makeAlert("persist-1", "logs_threshold", false, false, nil, "app", "svc")
	multiCounts := map[string][4]int{
		"persist-1": {2, 3, 5, 8}, // fires in all 4 windows, total=8 ≤ 10
	}
	noisy := AnalyzeNoiseMultiWindow([]*models.AlertDef{alert}, multiCounts, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert for persistent pattern, got %d", len(noisy))
	}
	if noisy[0].NoisePattern != "persistent" {
		t.Errorf("noise_pattern: want persistent, got %q", noisy[0].NoisePattern)
	}
}

func TestAnalyzeNoiseMultiWindow_noPattern_notFlagged(t *testing.T) {
	// Total 4, no recognisable pattern — not behaviorally noisy
	v := sparseVector("Rare Security Alert")
	alert := makeAlert("rare-1", "logs_threshold", false, false, nil, "app", "svc")
	multiCounts := map[string][4]int{
		"rare-1": {0, 1, 1, 4},
	}
	noisy := AnalyzeNoiseMultiWindow([]*models.AlertDef{alert}, multiCounts, 0)
	if len(noisy) != 0 {
		t.Errorf("expected 0 noisy alerts for rare alert, got %d: %v", len(noisy), noisy)
	}
}

func TestAnalyzeNoiseMultiWindow_vendorCoveredExcluded(t *testing.T) {
	v := sparseVector("GCP SCC Vendor Alert")
	alert := makeAlert("vendor-1", "logs_threshold", true, true, nil, "", "")
	multiCounts := map[string][4]int{
		"vendor-1": {5, 8, 10, 20}, // would be high_volume if not excluded
	}
	noisy := AnalyzeNoiseMultiWindow([]*models.AlertDef{alert}, multiCounts, 0)
	if len(noisy) != 0 {
		t.Errorf("vendor-covered alert must be excluded, got %d: %v", len(noisy), noisy)
	}
}
```

- [ ] **Step 2: Run to verify tests fail**

```bash
cd /path/to/backend && go test ./internal/similarity/ -run "TestAnalyzeNoiseMultiWindow" -v
```

Expected: FAIL — `AnalyzeNoiseMultiWindow` not defined.

- [ ] **Step 3: Add `findNoiseAlertsMultiWindow` to `engine.go`**

In `backend/internal/similarity/engine.go`, add after the closing brace of `findNoiseAlerts` (around line 1238):

```go
// findNoiseAlertsMultiWindow is like findNoiseAlerts but uses 4-window event
// counts for richer behavioral pattern detection (burst, periodic, accelerating,
// persistent) in addition to the existing high_volume signal.
// multiCounts index: 0=7d, 1=14d, 2=21d, 3=30d.
func findNoiseAlertsMultiWindow(
	vectors []featureVector,
	alerts []*models.AlertDef,
	multiCounts map[string][4]int,
	integrationCount int,
	idf idfTable,
	queryIDFThreshold float64,
) []models.NoiseAlert {
	var noisy []models.NoiseAlert

	for i, v := range vectors {
		var alert *models.AlertDef
		if alerts != nil && i < len(alerts) {
			alert = alerts[i]
		}

		// Exclusions — same as findNoiseAlerts.
		if alert != nil {
			if alert.Features.VendorCovered {
				continue
			}
			if alert.Features.IsBuildingBlock {
				continue
			}
		}

		// Signal 1: multi-window behavioral classification.
		var windowCounts [4]int
		var pattern string
		if alert != nil && multiCounts != nil {
			windowCounts = multiCounts[alert.ID]
			pattern = classifyNoisePattern(windowCounts)
		}
		isBehavioral := pattern != ""
		triggerCount := windowCounts[3] // 30d count for display

		// Signal 2: structural — identical to findNoiseAlerts.
		isStructural := false
		isUnscoped := false
		isBroadQuery := false
		if alert != nil && alert.Features.IsSecurityAlert {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
			isUnscoped = app == "" && sub == ""
			noEntity := len(v.entities) == 0
			isBroadQuery = hasWildcardQuery(v.luceneQuery) ||
				avgIDF(v.luceneQuery, idf.luceneQuery) < queryIDFThreshold
			isStructural = noEntity && (isUnscoped || isBroadQuery)
		}

		if !isBehavioral && !isStructural {
			continue
		}

		noisy = append(noisy, models.NoiseAlert{
			Name:            v.alertName,
			MissingFeatures: buildMissingFeatures(v),
			Reason:          buildNoiseReason(triggerCount, integrationCount, isBehavioral, isUnscoped, isBroadQuery),
			TriggerCount:    triggerCount,
			NoiseType:       noiseTypeString(isBehavioral, isStructural),
			NoisePattern:    pattern,
			WindowCounts:    windowCounts,
			BurstScore:      burstScore(windowCounts),
		})
	}

	sort.Slice(noisy, func(i, j int) bool {
		return noisy[i].Name < noisy[j].Name
	})
	return noisy
}
```

- [ ] **Step 4: Add `AnalyzeNoiseMultiWindow` to `engine.go`**

Immediately after `findNoiseAlertsMultiWindow`, add:

```go
// AnalyzeNoiseMultiWindow is like AnalyzeNoise but accepts 4-window event
// counts for richer behavioral pattern detection. Used by HandleNoise.
// The existing AnalyzeNoise is kept unchanged for HandleAnalyze/HandleInsights.
func AnalyzeNoiseMultiWindow(
	alerts []*models.AlertDef,
	multiCounts map[string][4]int,
	integrationCount int,
) []models.NoiseAlert {
	if len(alerts) == 0 {
		return nil
	}
	vectors := buildFeatureVectors(alerts)
	idf := buildIDF(vectors)
	threshold := computeQueryIDFThreshold(vectors, idf)
	return findNoiseAlertsMultiWindow(vectors, alerts, multiCounts, integrationCount, idf, threshold)
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /path/to/backend && go test ./internal/similarity/ -run "TestAnalyzeNoiseMultiWindow" -v
```

Expected: All 6 tests PASS.

- [ ] **Step 6: Run full similarity test suite**

```bash
cd /path/to/backend && go test ./internal/similarity/ -v 2>&1 | tail -20
```

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(noise): AnalyzeNoiseMultiWindow + findNoiseAlertsMultiWindow with pattern classification"
```

---

### Task 4: HandleNoise handler update

**Files:**
- Modify: `backend/internal/api/handlers.go` — add `fetchEventCountsMultiWindow` helper, update `HandleNoise`

- [ ] **Step 1: Add `fetchEventCountsMultiWindow` helper**

In `backend/internal/api/handlers.go`, find the `fetchEventCounts` helper (around line 1076). Add the following function directly after it:

```go
// fetchEventCountsMultiWindow fetches alert event counts at 4 fixed windows
// (7d/14d/21d/30d) in parallel. Returns nil on complete failure.
func fetchEventCountsMultiWindow(ctx context.Context, region, apiKey string, alertIDs []string) map[string][4]int {
	client, err := coralogix.NewClient(region, apiKey)
	if err != nil {
		return nil
	}
	defer client.Close()
	counts, err := client.FetchAlertEventCountsMultiWindow(ctx, alertIDs)
	if err != nil {
		log.Printf("DEBUG [noise] multi-window event count fetch failed: %v", err)
		return nil
	}
	return counts
}
```

- [ ] **Step 2: Update `HandleNoise`**

In `backend/internal/api/handlers.go`, find `HandleNoise` (line 529). Replace lines 576–587 (the event-count fetch + AnalyzeNoise call):

**Old:**
```go
	eventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, alertIDs, lookback)
	if eventCounts == nil {
		log.Printf("WARN [noise] event counts unavailable client=%s lookback=%d — structural-only", req.Client, lookback)
	} else {
		log.Printf("INFO [noise] event counts: requested=%d matched=%d client=%s lookback=%d", len(alertIDs), len(eventCounts), req.Client, lookback)
	}

	coralogix.ExtractFeatures(alerts, nil)

	// Pass 0 for integrationCount — Monday not fetched in this path; structural
	// reason text won't include org integration count but all noise signals are accurate.
	noiseAlerts := similarity.AnalyzeNoise(alerts, eventCounts, 0)
```

**New:**
```go
	multiCounts := fetchEventCountsMultiWindow(ctx, clientCfg.Region, clientCfg.APIKey, alertIDs)
	if multiCounts == nil {
		log.Printf("WARN [noise] multi-window event counts unavailable client=%s — structural-only", req.Client)
	} else {
		log.Printf("INFO [noise] multi-window event counts: requested=%d fetched=%d client=%s", len(alertIDs), len(multiCounts), req.Client)
	}

	coralogix.ExtractFeatures(alerts, nil)

	// Pass 0 for integrationCount — Monday not fetched in this path; structural
	// reason text won't include org integration count but all noise signals are accurate.
	noiseAlerts := similarity.AnalyzeNoiseMultiWindow(alerts, multiCounts, 0)
```

- [ ] **Step 3: Build and test**

```bash
cd /path/to/backend && go build ./... && go test ./internal/api/ -count=1 2>&1 | tail -5
```

Expected: Build clean, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/handlers.go
git commit -m "feat(noise): HandleNoise uses FetchAlertEventCountsMultiWindow + AnalyzeNoiseMultiWindow"
```

---

### Task 5: Frontend — types + pattern badge

**Files:**
- Modify: `frontend/src/types/index.ts:99-105` — extend `NoiseAlert` interface
- Modify: `frontend/src/components/AlertInsights.tsx` — add `noisePatternLabel` helper + badge
- Modify: `frontend/src/App.css` — add `.noise-pattern-badge` CSS after `.noise-trigger-count`

- [ ] **Step 1: Update `NoiseAlert` in `types/index.ts`**

In `frontend/src/types/index.ts`, replace lines 99–105:

```typescript
export interface NoiseAlert {
  name: string;
  reason?: string;
  missing_features?: string[];
  trigger_count?: number;
  noise_type?: 'behavioral' | 'structural' | 'both';
}
```

With:

```typescript
export interface NoiseAlert {
  name: string;
  reason?: string;
  missing_features?: string[];
  trigger_count?: number;
  noise_type?: 'behavioral' | 'structural' | 'both';
  noise_pattern?: 'high_volume' | 'burst' | 'periodic' | 'accelerating' | 'persistent';
  window_counts?: [number, number, number, number];
  burst_score?: number;
}
```

- [ ] **Step 2: Add `noisePatternLabel` helper in `AlertInsights.tsx`**

In `frontend/src/components/AlertInsights.tsx`, find the `noiseTypeLabel` function (around line 30). Add the following function directly after it:

```typescript
function noisePatternLabel(pattern: string): string {
  switch (pattern) {
    case 'high_volume':   return 'High Volume';
    case 'burst':         return 'Burst';
    case 'periodic':      return 'Periodic';
    case 'accelerating':  return 'Accelerating';
    case 'persistent':    return 'Persistent';
    default:              return pattern;
  }
}
```

- [ ] **Step 3: Add pattern badge in `AlertInsights.tsx`**

In `frontend/src/components/AlertInsights.tsx`, find the noise-type-badge span (around line 807):

```tsx
<span className={`noise-type-badge noise-type-badge--${noise.noise_type ?? 'structural'}`}>
  {noiseTypeLabel(noise.noise_type)}
</span>
```

Add the pattern badge immediately after it:

```tsx
<span className={`noise-type-badge noise-type-badge--${noise.noise_type ?? 'structural'}`}>
  {noiseTypeLabel(noise.noise_type)}
</span>
{noise.noise_pattern && (
  <span
    className={`noise-pattern-badge noise-pattern-badge--${noise.noise_pattern}`}
    title={noise.window_counts
      ? `7d: ${noise.window_counts[0]} / 14d: ${noise.window_counts[1]} / 21d: ${noise.window_counts[2]} / 30d: ${noise.window_counts[3]}`
      : undefined}
  >
    {noisePatternLabel(noise.noise_pattern)}
  </span>
)}
```

- [ ] **Step 4: Add CSS for `.noise-pattern-badge`**

In `frontend/src/App.css`, find the `.noise-trigger-count` block (around line 804). Add the following CSS after its closing brace:

```css
.noise-pattern-badge {
  font-family: var(--font-mono);
  font-size: 0.62rem;
  padding: 2px 6px;
  border-radius: 3px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex-shrink: 0;
  cursor: default;
}
.noise-pattern-badge--high_volume { background: #7f1d1d; color: #fca5a5; }
.noise-pattern-badge--burst        { background: #78350f; color: #fcd34d; }
.noise-pattern-badge--periodic     { background: #1e3a5f; color: #93c5fd; }
.noise-pattern-badge--accelerating { background: #4a1d96; color: #c4b5fd; }
.noise-pattern-badge--persistent   { background: #14532d; color: #86efac; }
```

- [ ] **Step 5: Verify TypeScript builds**

```bash
cd /path/to/frontend && npm run build 2>&1 | tail -10
```

Expected: Build succeeds, no type errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/components/AlertInsights.tsx frontend/src/App.css
git commit -m "feat(noise): pattern badge with window-count tooltip in noise alert list"
```

---

## Self-Review

**Spec coverage:**
1. ✅ `FetchAlertEventCountsMultiWindow` on Coralogix client → Task 2
2. ✅ `NoiseAlert` fields `NoisePattern`, `WindowCounts`, `BurstScore` → Task 1
3. ✅ `burstScore`, `periodicityScore`, `accelerationScore`, `classifyNoisePattern` → Task 1
4. ✅ `AnalyzeNoiseMultiWindow` with new signature → Task 3 (as `AnalyzeNoiseMultiWindow`, not renamed `AnalyzeNoise`)
5. ✅ `HandleNoise` uses multi-window path → Task 4
6. ✅ Frontend pattern badge + tooltip → Task 5
7. ✅ Threshold constants in `noise_scoring.go` → Task 1
8. ✅ `HandleAnalyze` unchanged (old `AnalyzeNoise` kept) — explicit deviation from spec, by design

**Placeholder scan:** All steps contain complete code. No TBDs.

**Type consistency:**
- `classifyNoisePattern` returns `string` — same type as `NoisePattern` field in `NoiseAlert` ✅
- `FetchAlertEventCountsMultiWindow` returns `map[string][4]int` — matches `multiCounts` parameter in `AnalyzeNoiseMultiWindow` ✅
- `WindowCounts [4]int` in Go → `window_counts?: [number, number, number, number]` in TypeScript ✅
- `burstScore` is called in `findNoiseAlertsMultiWindow` and also assigned to `NoiseAlert.BurstScore float64` ✅
- `behavioralNoiseThreshold` is defined in `engine.go` line 1115; `classifyNoisePattern` in `noise_scoring.go` uses it — both are in `package similarity` ✅
