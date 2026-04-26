# LLM-Driven Gap Analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace rule-based tactic-level gap strings with a structured-signals → Claude Opus pipeline that categorises findings into 6 typed gap slices and produces an enriched summary.

**Architecture:** `buildStructuredSignals()` assembles ~1–2k token JSON from technique coverage, noise alerts, integration gaps, and tactic percentages. `insights.Enrich()` sends this to Claude Opus and parses the 6-category JSON response into `GapCategories`. The frontend `coverage` tab renders one section per category.

**Tech Stack:** Go 1.22, React + TypeScript, Anthropic Claude Opus (via `llm.Provider`), existing `mitre.AnalyzeCoverage` and `similarity.Analyze` pipelines.

---

### File Map

| File | Action | What changes |
|------|--------|-------------|
| `backend/internal/models/models.go` | Modify | Add `TechniqueCoverageEntry`, `GapCategories`; update `MITRECoverageResult`, `InsightsReport`; remove `CoverageInsights` from `SimilarityResult` |
| `backend/internal/similarity/engine.go` | Modify | Remove `analyzeCoverage()` call + field assignment; delete the local `analyzeCoverage` func |
| `backend/internal/api/handlers.go` | Modify | Remove `CoverageInsights` from cache key; update `runInsightsBackground` and both `Enrich` call sites with new params |
| `backend/internal/mitre/mitre.go` | Modify | Populate `TechniqueCoverage` map inside `AnalyzeCoverage` |
| `backend/internal/mitre/mitre_test.go` | Modify | Add `TestAnalyzeCoverage_TechniqueLevel` |
| `backend/internal/insights/signals.go` | **New** | `buildStructuredSignals()` assembles Claude input JSON |
| `backend/internal/insights/signals_test.go` | **New** | `TestBuildStructuredSignals` |
| `backend/internal/insights/enrich.go` | Rewrite | New `Enrich` signature; replace `buildPrompt` with signals JSON; add `parseGapCategoriesResponse` |
| `backend/internal/insights/enrich_test.go` | Rewrite | Updated for new Enrich signature; add `TestParseGapCategoriesResponse` |
| `frontend/src/types/index.ts` | Modify | Add `GapCategories` interface; update `InsightsReport`, `SimilarityResult` |
| `frontend/src/components/AlertInsights.tsx` | Modify | Coverage tab: 6-category sections; update `gapCount` source |

---

### Task 1: Update models.go — Foundational Types

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Write the failing test** (compile-gate — checks new types exist)

Create `backend/internal/models/models_gap_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
)

func TestGapCategories_JSONRoundTrip(t *testing.T) {
	gc := GapCategories{
		EnvironmentCleanup:   []string{"Alert A duplicates Alert B"},
		NoDetection:          []string{"T1078: no coverage"},
		PoorTacticCoverage:   []string{},
		WeakDetectionQuality: []string{},
		AdvancedUseCases:     []string{},
		MissingSourceAlerts:  []string{"Azure AD: 0 alerts"},
	}
	b, err := json.Marshal(gc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out GapCategories
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.NoDetection) != 1 || out.NoDetection[0] != "T1078: no coverage" {
		t.Errorf("no_detection roundtrip failed: %v", out.NoDetection)
	}
}

func TestTechniqueCoverageEntry_JSONRoundTrip(t *testing.T) {
	entry := TechniqueCoverageEntry{Name: "Valid Accounts", AlertCount: 2, Weak: true}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out TechniqueCoverageEntry
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Weak || out.AlertCount != 2 {
		t.Errorf("unexpected: %+v", out)
	}
}

func TestInsightsReport_GapCategoriesField(t *testing.T) {
	ir := InsightsReport{
		Summary: "ok",
		GapCategories: GapCategories{
			NoDetection: []string{"T1059"},
		},
	}
	b, _ := json.Marshal(ir)
	var out InsightsReport
	json.Unmarshal(b, &out)
	if len(out.GapCategories.NoDetection) != 1 {
		t.Error("GapCategories not preserved through JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/models/... -run TestGapCategories -v
```

Expected: `FAIL — GapCategories undefined`

- [ ] **Step 3: Apply model changes**

In `backend/internal/models/models.go`, make the following changes:

**Add after `TacticCoverage` struct (after line 73):**

```go
// TechniqueCoverageEntry holds per-technique alert coverage for the signals pipeline.
// Weak is true when the technique is covered but all covering alerts are unscoped
// (no DataSources and no Entities), making detection quality poor.
type TechniqueCoverageEntry struct {
	Name       string `json:"name"`
	AlertCount int    `json:"alert_count"`
	Weak       bool   `json:"weak,omitempty"`
}

// GapCategories holds the 6-category LLM gap analysis output.
type GapCategories struct {
	EnvironmentCleanup   []string `json:"environment_cleanup"`
	NoDetection          []string `json:"no_detection"`
	PoorTacticCoverage   []string `json:"poor_tactic_coverage"`
	WeakDetectionQuality []string `json:"weak_detection_quality"`
	AdvancedUseCases     []string `json:"advanced_use_cases"`
	MissingSourceAlerts  []string `json:"missing_source_alerts"`
}
```

**Update `MITRECoverageResult` (replace existing struct):**

```go
// MITRECoverageResult is the response for MITRE coverage analysis.
type MITRECoverageResult struct {
	NavigatorLayer    map[string]any                     `json:"navigator_layer"`
	Summary           MITRECoverageSummary               `json:"summary"`
	TechniqueCoverage map[string]TechniqueCoverageEntry  `json:"technique_coverage"`
}
```

**Update `InsightsReport` — remove `EnrichedGaps`, add `GapCategories`:**

```go
// InsightsReport is the LLM-generated analyst report for a SimilarityResult.
type InsightsReport struct {
	Model             string        `json:"model,omitempty"`
	Summary           string        `json:"summary"`
	TopPriority       []string      `json:"top_priority"`
	Strengths         []string      `json:"strengths"`
	Recommendations   []string      `json:"recommendations"`
	EnrichedDups      []string      `json:"enriched_dups"`
	GapCategories     GapCategories `json:"gap_categories"`
	NoiseExplanations []string      `json:"noise_explanations"`
}
```

**Update `SimilarityResult` — remove `CoverageInsights`:**

```go
// SimilarityResult is the response for alert insight analysis.
type SimilarityResult struct {
	Families         []DetectionFamily `json:"families"`
	Duplicates       []DuplicateGroup  `json:"duplicates"`
	MergeSuggestions []MergeSuggestion `json:"merge_suggestions"`
	UniqueDetections []string          `json:"unique_detections"`
	NoiseAlerts      []NoiseAlert      `json:"noise_alerts"`
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/models/... -v
```

Expected: PASS

- [ ] **Step 5: Verify project still builds (will surface all callers of removed fields)**

```bash
cd backend && go build ./...
```

Expected: compile errors referencing `CoverageInsights` in `engine.go`, `handlers.go`, and `enrich.go` — these are fixed in subsequent tasks.

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/models/models.go internal/models/models_gap_test.go
git commit -m "feat(models): add TechniqueCoverageEntry, GapCategories; remove CoverageInsights"
```

---

### Task 2: Remove CoverageInsights from engine.go and handlers.go

**Files:**
- Modify: `backend/internal/similarity/engine.go`
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Write the failing test** (verify engine compiles and Analyze returns no CoverageInsights field)

The existing engine tests in `engine_test.go` will pass once the field is removed — the struct literal just won't include it. The compile error IS the failing test.

- [ ] **Step 2: Fix engine.go — remove CoverageInsights**

In `backend/internal/similarity/engine.go`, find the `Analyze` function. Remove lines:

```go
// Step 6: Coverage insights (MITRE-based; nil = no coverage insights).
coverageInsights := analyzeCoverage(mitreResult)
```

And update the return struct (around line 228) — remove `CoverageInsights: coverageInsights,`:

```go
return &models.SimilarityResult{
    Families:         families,
    Duplicates:       duplicates,
    MergeSuggestions: mergeSuggestions,
    UniqueDetections: uniqueDetections,
    NoiseAlerts:      noiseAlerts,
}
```

Then delete the entire `analyzeCoverage` function (lines 925–955):

```go
func analyzeCoverage(mitreResult *models.MITRECoverageResult) []string {
    // ... delete this entire function
}
```

- [ ] **Step 3: Fix handlers.go cache key — remove CoverageInsights**

In `backend/internal/api/handlers.go`, find `computeInsightsCacheKey` (around line 802). Update the `stableResult` struct and its population — remove `CoverageInsights`:

```go
func computeInsightsCacheKey(clientName string, result *models.SimilarityResult) (string, error) {
    type stableResult struct {
        Families         []models.DetectionFamily
        Duplicates       []models.DuplicateGroup
        MergeSuggestions []models.MergeSuggestion
        UniqueDetections []string
        NoiseAlerts      []models.NoiseAlert
    }

    sr := stableResult{
        Families:         append([]models.DetectionFamily(nil), result.Families...),
        Duplicates:       append([]models.DuplicateGroup(nil), result.Duplicates...),
        MergeSuggestions: append([]models.MergeSuggestion(nil), result.MergeSuggestions...),
        UniqueDetections: append([]string(nil), result.UniqueDetections...),
        NoiseAlerts:      append([]models.NoiseAlert(nil), result.NoiseAlerts...),
    }
    // ... rest of function unchanged
```

- [ ] **Step 4: Verify build succeeds**

```bash
cd backend && go build ./...
```

Expected: compile errors only from `enrich.go` (references `CoverageInsights` in early-return and `buildPrompt`) — fixed in Task 5.

- [ ] **Step 5: Run engine tests**

```bash
cd backend && go test ./internal/similarity/... -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/similarity/engine.go internal/api/handlers.go
git commit -m "feat(engine): remove CoverageInsights field from SimilarityResult pipeline"
```

---

### Task 3: Extend AnalyzeCoverage with Technique-Level Coverage

**Files:**
- Modify: `backend/internal/mitre/mitre.go`
- Modify: `backend/internal/mitre/mitre_test.go` (or create if missing)

- [ ] **Step 1: Write the failing test**

Find the mitre test file:

```bash
ls backend/internal/mitre/*_test.go
```

Add `TestAnalyzeCoverage_TechniqueLevel` to the test file. If only `technique_json_test.go` exists, add to it:

```go
func TestAnalyzeCoverage_TechniqueLevel(t *testing.T) {
    // Unscoped alert (no DataSources, no Entities) — should mark T1059 as weak
    unscopedAlert := &models.AlertDef{
        Name: "Broad CMD Alert",
        Features: models.AlertFeatures{
            Techniques:  []string{"T1059"},
            DataSources: nil,
            Entities:    nil,
        },
    }
    // Scoped alert (has DataSources) — T1078 should NOT be weak
    scopedAlert := &models.AlertDef{
        Name: "Valid Accounts Scoped",
        Features: models.AlertFeatures{
            Techniques:  []string{"T1078"},
            DataSources: []string{"windows-security"},
        },
    }

    result := AnalyzeCoverage([]*models.AlertDef{unscopedAlert, scopedAlert})

    if result.TechniqueCoverage == nil {
        t.Fatal("TechniqueCoverage map is nil")
    }

    t1059, ok := result.TechniqueCoverage["T1059"]
    if !ok {
        t.Fatal("T1059 missing from TechniqueCoverage")
    }
    if t1059.AlertCount != 1 {
        t.Errorf("T1059: want AlertCount=1, got %d", t1059.AlertCount)
    }
    if !t1059.Weak {
        t.Error("T1059: want Weak=true (unscoped alert), got false")
    }
    if t1059.Name == "" {
        t.Error("T1059: Name must be populated")
    }

    t1078, ok := result.TechniqueCoverage["T1078"]
    if !ok {
        t.Fatal("T1078 missing from TechniqueCoverage")
    }
    if t1078.Weak {
        t.Error("T1078: want Weak=false (scoped alert), got true")
    }
    if t1078.AlertCount != 1 {
        t.Errorf("T1078: want AlertCount=1, got %d", t1078.AlertCount)
    }

    // Uncovered technique should be present with AlertCount=0
    t1566, ok := result.TechniqueCoverage["T1566"]
    if !ok {
        t.Fatal("T1566 (Phishing) missing from TechniqueCoverage — all parent techniques must be present")
    }
    if t1566.AlertCount != 0 {
        t.Errorf("T1566: want AlertCount=0, got %d", t1566.AlertCount)
    }
    if t1566.Weak {
        t.Error("T1566: Weak must be false when AlertCount=0")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/mitre/... -run TestAnalyzeCoverage_TechniqueLevel -v
```

Expected: FAIL — `result.TechniqueCoverage` is nil (field not yet populated)

- [ ] **Step 3: Implement technique-level coverage in AnalyzeCoverage**

In `backend/internal/mitre/mitre.go`, inside `AnalyzeCoverage` (starting at line 284):

**After the existing `techToAlerts` build loop (after line ~300), add a scoped-alert tracking loop:**

```go
// Track which techniques have at least one scoped alert.
// A scoped alert has non-empty DataSources or Entities — unscoped alerts fire on all apps.
techHasScoped := make(map[string]bool)
for _, alert := range alerts {
    isScoped := len(alert.Features.DataSources) > 0 || len(alert.Features.Entities) > 0
    if !isScoped {
        continue
    }
    for _, tid := range alert.Features.Techniques {
        baseTID := tid
        if idx := strings.Index(tid, "."); idx != -1 {
            baseTID = tid[:idx]
        }
        techHasScoped[baseTID] = true
        if baseTID != tid {
            techHasScoped[tid] = true
        }
    }
}
```

**Before the final `return` statement, build `TechniqueCoverage`:**

```go
// Build technique-level coverage map (parent techniques only, deduplicated).
techniqueCoverage := make(map[string]models.TechniqueCoverageEntry)
seenTechID := make(map[string]bool)
for i := range masterTechniqueList {
    t := &masterTechniqueList[i]
    if seenTechID[t.ID] {
        continue // skip multi-tactic duplicates
    }
    seenTechID[t.ID] = true
    alertCount := len(techToAlerts[t.ID])
    weak := alertCount > 0 && !techHasScoped[t.ID]
    techniqueCoverage[t.ID] = models.TechniqueCoverageEntry{
        Name:       t.Name,
        AlertCount: alertCount,
        Weak:       weak,
    }
}
```

**Update the return statement to include `TechniqueCoverage`:**

```go
return &models.MITRECoverageResult{
    NavigatorLayer:    navigatorLayer,
    TechniqueCoverage: techniqueCoverage,
    Summary: models.MITRECoverageSummary{
        TotalTechniques:      totalTechniques,
        CoveredTechniques:    coveredTechniques,
        CoveragePercent:      round2(overallPercent),
        TotalSubTechniques:   totalSubTechniques,
        CoveredSubTechniques: coveredSubTechniques,
        TacticBreakdown:      tacticBreakdown,
    },
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/mitre/... -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/mitre/mitre.go internal/mitre/technique_json_test.go
git commit -m "feat(mitre): populate TechniqueCoverage map in AnalyzeCoverage"
```

---

### Task 4: Create insights/signals.go

**Files:**
- Create: `backend/internal/insights/signals.go`
- Create: `backend/internal/insights/signals_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/insights/signals_test.go`:

```go
package insights

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestBuildStructuredSignals_IntegrationGaps(t *testing.T) {
	result := &models.SimilarityResult{
		NoiseAlerts: []models.NoiseAlert{
			{Name: "Alert A", NoiseType: "structural", TriggerCount: 0},
		},
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"X", "Y"}},
			{AlertNames: []string{"P", "Q"}},
		},
	}
	alerts := []*models.AlertDef{
		{Name: "Alert One"},
		{Name: "Alert Two"},
	}
	integrations := []models.IntegrationInfo{
		{Name: "Azure AD", AlertCount: 0},
		{Name: "AWS CloudTrail", AlertCount: 5},
	}
	mitreCoverage := &models.MITRECoverageResult{
		TechniqueCoverage: map[string]models.TechniqueCoverageEntry{
			"T1078": {Name: "Valid Accounts", AlertCount: 0},
			"T1059": {Name: "Command Interpreter", AlertCount: 2, Weak: true},
		},
		Summary: models.MITRECoverageSummary{
			TacticBreakdown: map[string]models.TacticCoverage{
				"initial-access": {TacticName: "Initial Access", Percent: 11.11, Covered: 1, Total: 9},
				"reconnaissance": {TacticName: "Reconnaissance", Percent: 0, Covered: 0, Total: 10},
			},
		},
	}

	sig := buildStructuredSignals(result, alerts, integrations, mitreCoverage)

	if sig.AlertCount != 2 {
		t.Errorf("alert_count: want 2, got %d", sig.AlertCount)
	}
	if sig.IntegrationCount != 2 {
		t.Errorf("integration_count: want 2, got %d", sig.IntegrationCount)
	}
	if sig.DuplicateGroups != 2 {
		t.Errorf("duplicate_groups: want 2, got %d", sig.DuplicateGroups)
	}
	if len(sig.IntegrationGaps) != 1 {
		t.Fatalf("integration_gaps: want 1, got %d", len(sig.IntegrationGaps))
	}
	if sig.IntegrationGaps[0].Name != "Azure AD" {
		t.Errorf("wrong gap: %s", sig.IntegrationGaps[0].Name)
	}
	if len(sig.NoiseAlerts) != 1 || sig.NoiseAlerts[0] != "Alert A [structural]" {
		t.Errorf("noise_alerts: %v", sig.NoiseAlerts)
	}

	tc, ok := sig.TechniqueCoverage["T1059"]
	if !ok {
		t.Fatal("T1059 missing from technique_coverage")
	}
	if !tc.Weak {
		t.Error("T1059 should be weak")
	}

	// Uncovered technique must appear
	if _, ok := sig.TechniqueCoverage["T1078"]; !ok {
		t.Error("T1078 (uncovered) should appear in technique_coverage")
	}
}

func TestBuildStructuredSignals_NilMitre(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	// Should not panic with nil mitreCoverage
	sig := buildStructuredSignals(result, nil, nil, nil)
	if sig.AlertCount != 0 {
		t.Errorf("want 0 alerts, got %d", sig.AlertCount)
	}
	if len(sig.TechniqueCoverage) != 0 {
		t.Error("want empty technique_coverage for nil mitre")
	}
}

func TestBuildStructuredSignals_WeakFlagPreserved(t *testing.T) {
	mitreCoverage := &models.MITRECoverageResult{
		TechniqueCoverage: map[string]models.TechniqueCoverageEntry{
			"T1110": {Name: "Brute Force", AlertCount: 1, Weak: false},
		},
		Summary: models.MITRECoverageSummary{},
	}
	sig := buildStructuredSignals(&models.SimilarityResult{}, nil, nil, mitreCoverage)
	if sig.TechniqueCoverage["T1110"].Weak {
		t.Error("T1110 should not be weak")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/insights/... -run TestBuildStructuredSignals -v
```

Expected: FAIL — `buildStructuredSignals` undefined

- [ ] **Step 3: Create signals.go**

Create `backend/internal/insights/signals.go`:

```go
package insights

import (
	"fmt"

	"coralogix-alert-analyzer/internal/models"
)

// structuredSignals is the JSON payload sent to Claude Opus for gap analysis.
type structuredSignals struct {
	AlertCount        int                               `json:"alert_count"`
	IntegrationCount  int                               `json:"integration_count"`
	TacticCoverage    map[string]signalsTacticEntry     `json:"tactic_coverage"`
	TechniqueCoverage map[string]signalsTechniqueEntry  `json:"technique_coverage"`
	IntegrationGaps   []signalsIntegrationGap           `json:"integration_gaps"`
	NoiseAlerts       []string                          `json:"noise_alerts"`
	DuplicateGroups   int                               `json:"duplicate_groups"`
}

type signalsTacticEntry struct {
	Pct    float64 `json:"pct"`
	Alerts int     `json:"alerts"`
}

type signalsTechniqueEntry struct {
	Name   string `json:"name"`
	Alerts int    `json:"alerts"`
	Weak   bool   `json:"weak,omitempty"`
}

type signalsIntegrationGap struct {
	Name   string `json:"name"`
	Alerts int    `json:"alerts"`
}

// buildStructuredSignals assembles the ~1–2k token JSON payload for Claude Opus.
// All parameters are optional — nil inputs produce empty but valid signals.
func buildStructuredSignals(
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
) structuredSignals {
	sig := structuredSignals{
		TacticCoverage:    make(map[string]signalsTacticEntry),
		TechniqueCoverage: make(map[string]signalsTechniqueEntry),
	}

	sig.AlertCount = len(alerts)
	sig.IntegrationCount = len(integrations)

	if result != nil {
		sig.DuplicateGroups = len(result.Duplicates)

		for _, na := range result.NoiseAlerts {
			label := na.Name
			switch {
			case na.TriggerCount > 0 && na.NoiseType != "":
				label = fmt.Sprintf("%s [%s, %d×]", na.Name, na.NoiseType, na.TriggerCount)
			case na.NoiseType != "":
				label = fmt.Sprintf("%s [%s]", na.Name, na.NoiseType)
			}
			sig.NoiseAlerts = append(sig.NoiseAlerts, label)
		}
	}

	if mitreCoverage != nil {
		for tactic, tc := range mitreCoverage.Summary.TacticBreakdown {
			sig.TacticCoverage[tactic] = signalsTacticEntry{
				Pct:    tc.Percent,
				Alerts: tc.Covered,
			}
		}
		for id, entry := range mitreCoverage.TechniqueCoverage {
			sig.TechniqueCoverage[id] = signalsTechniqueEntry{
				Name:   entry.Name,
				Alerts: entry.AlertCount,
				Weak:   entry.Weak,
			}
		}
	}

	for _, integ := range integrations {
		if integ.AlertCount == 0 {
			sig.IntegrationGaps = append(sig.IntegrationGaps, signalsIntegrationGap{
				Name:   integ.Name,
				Alerts: 0,
			})
		}
	}

	return sig
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/insights/... -run TestBuildStructuredSignals -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/insights/signals.go internal/insights/signals_test.go
git commit -m "feat(insights): add buildStructuredSignals for structured gap analysis input"
```

---

### Task 5: Rewrite insights/enrich.go

**Files:**
- Rewrite: `backend/internal/insights/enrich.go`
- Rewrite: `backend/internal/insights/enrich_test.go`

- [ ] **Step 1: Write the new tests first**

Replace `backend/internal/insights/enrich_test.go` entirely:

```go
package insights

import (
	"context"
	"errors"
	"testing"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (string, error) {
	return m.response, m.err
}
func (m *mockProvider) Name() string { return "mock" }

// ── parseGapCategoriesResponse ────────────────────────────────────────────────

func TestParseGapCategoriesResponse_ValidJSON(t *testing.T) {
	raw := `{
		"summary": "Strong credential-access coverage.",
		"environment_cleanup": ["Alert A duplicates Alert B"],
		"no_detection": ["T1078: no coverage"],
		"poor_tactic_coverage": [],
		"weak_detection_quality": ["T1059 unscoped"],
		"advanced_use_cases": [],
		"missing_source_alerts": ["Azure AD: 0 alerts"]
	}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Summary != "Strong credential-access coverage." {
		t.Errorf("wrong summary: %q", report.Summary)
	}
	if len(report.GapCategories.EnvironmentCleanup) != 1 {
		t.Errorf("environment_cleanup: want 1, got %d", len(report.GapCategories.EnvironmentCleanup))
	}
	if len(report.GapCategories.NoDetection) != 1 {
		t.Errorf("no_detection: want 1, got %d", len(report.GapCategories.NoDetection))
	}
	if len(report.GapCategories.PoorTacticCoverage) != 0 {
		t.Errorf("poor_tactic_coverage: want empty slice, got %v", report.GapCategories.PoorTacticCoverage)
	}
	if report.GapCategories.WeakDetectionQuality[0] != "T1059 unscoped" {
		t.Errorf("weak_detection_quality: %v", report.GapCategories.WeakDetectionQuality)
	}
}

func TestParseGapCategoriesResponse_MissingCategory_FillsEmptySlice(t *testing.T) {
	raw := `{"summary": "partial", "no_detection": ["T1059"]}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil even with missing categories")
	}
	if report.GapCategories.EnvironmentCleanup == nil {
		t.Error("missing category must produce empty slice, not nil")
	}
	if len(report.GapCategories.EnvironmentCleanup) != 0 {
		t.Errorf("want 0 items, got %d", len(report.GapCategories.EnvironmentCleanup))
	}
}

func TestParseGapCategoriesResponse_MalformedJSON_ReturnsNil(t *testing.T) {
	report := parseGapCategoriesResponse("{not valid}")
	if report != nil {
		t.Error("want nil for malformed JSON")
	}
}

func TestParseGapCategoriesResponse_MarkdownWrapped_Stripped(t *testing.T) {
	raw := "```json\n{\"summary\": \"ok\", \"no_detection\": [\"T1059\"]}\n```"
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("should strip markdown fence and parse")
	}
	if report.Summary != "ok" {
		t.Errorf("wrong summary after strip: %q", report.Summary)
	}
}

func TestParseGapCategoriesResponse_NullCategoryCoercedToEmpty(t *testing.T) {
	raw := `{"summary": "ok", "no_detection": null, "environment_cleanup": []}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil")
	}
	// null → should coerce to empty slice
	if report.GapCategories.NoDetection == nil {
		t.Error("null no_detection should become empty slice")
	}
}

// ── Enrich ────────────────────────────────────────────────────────────────────

func TestEnrich_nilResult_returnsNilNil(t *testing.T) {
	report, err := Enrich(context.Background(), nil, nil, nil, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil; got %v, %v", report, err)
	}
}

func TestEnrich_emptyResult_returnsNilNil(t *testing.T) {
	result := &models.SimilarityResult{}
	report, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil for empty result; got %v, %v", report, err)
	}
}

func TestEnrich_validResponse_parsesGapCategories(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"A", "B"}, Similarity: 0.95},
		},
	}
	jsonResp := `{
		"summary": "Good baseline coverage.",
		"environment_cleanup": [],
		"no_detection": ["T1078: no alerts"],
		"poor_tactic_coverage": ["Reconnaissance: 0%"],
		"weak_detection_quality": [],
		"advanced_use_cases": [],
		"missing_source_alerts": []
	}`
	report, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Summary != "Good baseline coverage." {
		t.Errorf("wrong summary: %q", report.Summary)
	}
	if len(report.GapCategories.NoDetection) != 1 {
		t.Errorf("no_detection: want 1, got %d", len(report.GapCategories.NoDetection))
	}
	if len(report.GapCategories.PoorTacticCoverage) != 1 {
		t.Errorf("poor_tactic_coverage: want 1, got %d", len(report.GapCategories.PoorTacticCoverage))
	}
}

func TestEnrich_llmError_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	_, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{err: errors.New("network error")})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEnrich_invalidJSON_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	_, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: "not json"})
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
```

- [ ] **Step 2: Run new tests to verify they fail**

```bash
cd backend && go test ./internal/insights/... -v
```

Expected: compile errors (Enrich signature mismatch, parseGapCategoriesResponse undefined) and existing buildPrompt tests fail.

- [ ] **Step 3: Rewrite enrich.go**

Replace `backend/internal/insights/enrich.go` entirely:

```go
package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

const gapAnalysisSystemPrompt = `You are a senior detection engineer analysing an organisation's security alert library.
You will receive a JSON object with these fields:
- alert_count, integration_count: library size
- tactic_coverage: per-tactic {pct, alerts} — pct is coverage percent
- technique_coverage: per T-code {name, alerts, weak} — weak=true means covered but unscoped
- integration_gaps: integrations with zero alerts [{name, alerts}]
- noise_alerts: alert names flagged as noisy (with type and trigger count)
- duplicate_groups: number of duplicate alert groups

Respond with ONLY valid JSON matching this exact schema — no prose, no markdown:
{
  "summary": "<2-4 sentences: start with strengths then key gaps>",
  "environment_cleanup": ["<string>"],
  "no_detection": ["<string>"],
  "poor_tactic_coverage": ["<string>"],
  "weak_detection_quality": ["<string>"],
  "advanced_use_cases": ["<string>"],
  "missing_source_alerts": ["<string>"]
}

Rules:
- Every category field must be a JSON array — use [] if nothing applies, never null or omit the field
- Only reference techniques, tactics, and alert names present in the input — never fabricate names
- weak_detection_quality: only flag techniques where weak=true in the input
- advanced_use_cases: reason over technique type; only flag when threshold/count alerts exist but no anomaly layer
- summary: prose only (no bullet points), 2–4 sentences`

// Enrich takes a completed SimilarityResult, assembles structured signals, sends them
// to Claude Opus, and returns an InsightsReport with 6-category gap analysis.
// Returns nil, nil if the result has no meaningful content to enrich.
// Returns nil, err on LLM failure (caller treats as non-fatal).
func Enrich(
	ctx context.Context,
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	provider llm.Provider,
) (*models.InsightsReport, error) {
	if result == nil || (len(result.Duplicates) == 0 && len(result.Families) == 0 &&
		len(result.NoiseAlerts) == 0) {
		return nil, nil
	}

	signals := buildStructuredSignals(result, alerts, integrations, mitreCoverage)
	signalsJSON, err := json.Marshal(signals)
	if err != nil {
		return nil, fmt.Errorf("insights signals marshal: %w", err)
	}

	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: gapAnalysisSystemPrompt,
		UserMessage:  string(signalsJSON),
		MaxTokens:    2048,
	})
	if err != nil {
		return nil, fmt.Errorf("insights LLM call: %w", err)
	}

	report := parseGapCategoriesResponse(raw)
	if report == nil {
		return nil, fmt.Errorf("insights JSON parse: malformed response")
	}
	return report, nil
}

// parseGapCategoriesResponse parses the Claude Opus JSON output into an InsightsReport.
// Strips markdown fences if present. Missing categories become empty slices.
// Returns nil on malformed JSON.
func parseGapCategoriesResponse(raw string) *models.InsightsReport {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i != -1 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}

	// Parse into a loose map first so we can fill missing categories with empty slices.
	var loose struct {
		Summary              string   `json:"summary"`
		EnvironmentCleanup   []string `json:"environment_cleanup"`
		NoDetection          []string `json:"no_detection"`
		PoorTacticCoverage   []string `json:"poor_tactic_coverage"`
		WeakDetectionQuality []string `json:"weak_detection_quality"`
		AdvancedUseCases     []string `json:"advanced_use_cases"`
		MissingSourceAlerts  []string `json:"missing_source_alerts"`
	}
	if err := json.Unmarshal([]byte(raw), &loose); err != nil {
		return nil
	}

	// Coerce null slices to empty slices.
	coerce := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}

	return &models.InsightsReport{
		Summary: loose.Summary,
		GapCategories: models.GapCategories{
			EnvironmentCleanup:   coerce(loose.EnvironmentCleanup),
			NoDetection:          coerce(loose.NoDetection),
			PoorTacticCoverage:   coerce(loose.PoorTacticCoverage),
			WeakDetectionQuality: coerce(loose.WeakDetectionQuality),
			AdvancedUseCases:     coerce(loose.AdvancedUseCases),
			MissingSourceAlerts:  coerce(loose.MissingSourceAlerts),
		},
		// Remaining fields (enriched_dups, noise_explanations, etc.) are not
		// populated by this prompt — they remain zero-value (nil/empty).
	}
}
```

- [ ] **Step 4: Run new tests to verify they pass**

```bash
cd backend && go test ./internal/insights/... -v
```

Expected: PASS for all new tests. (Old buildPrompt tests are gone.)

- [ ] **Step 6: Verify full backend build**

```bash
cd backend && go build ./...
```

Expected: compile errors only in `handlers.go` (Enrich called with old 4-arg signature) — fixed in Task 6.

- [ ] **Step 7: Commit**

```bash
cd backend && git add internal/insights/enrich.go internal/insights/enrich_test.go
git commit -m "feat(insights): rewrite Enrich with 6-category structured signals pipeline"
```

---

### Task 6: Update handlers.go — Wire New Enrich Signature

**Files:**
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Identify the two Enrich call sites**

The two call sites are:
1. `runInsightsBackground` → line ~340: `insights.Enrich(bgCtx, alertInsights, alerts, insightsProvider)`
2. `HandleInsights` → line ~459: `insights.Enrich(ctx, alertInsights, alerts, insightsProvider)`

- [ ] **Step 2: Update runInsightsBackground signature and body**

Find `runInsightsBackground` (around line 323). Change its signature to accept integrations and mitreCoverage:

```go
func (h *Handler) runInsightsBackground(
    client string,
    alertInsights *models.SimilarityResult,
    alerts []*models.AlertDef,
    integrations []models.IntegrationInfo,
    mitreCoverage *models.MITRECoverageResult,
) {
```

Update the `Enrich` call inside it (around line 340):

```go
ir, enrichErr := insights.Enrich(bgCtx, alertInsights, alerts, integrations, mitreCoverage, insightsProvider)
```

- [ ] **Step 3: Update HandleAnalyze call site (line 285)**

In `HandleAnalyze`, find the `runInsightsBackground` call (line ~285). Pass the available variables:

```go
if h.sem != nil && h.cache != nil {
    h.runInsightsBackground(req.Client, alertInsights, alerts, integrationInfos, mitreCoverage)
}
```

(`integrationInfos` is `[]models.IntegrationInfo` available in `HandleAnalyze`; `mitreCoverage` is `*models.MITRECoverageResult` already computed there.)

- [ ] **Step 4: Update HandleInsights foreground call site (line 459)**

In `HandleInsights`, the variables `insightsMitre` (line ~428) and `integrations` are available. However, `HandleInsights` does not fetch Monday.com integrations — pass an empty slice:

```go
ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, nil, insightsMitre, insightsProvider)
```

(`nil` integrations → `missing_source_alerts` will be empty, which the spec explicitly allows.)

- [ ] **Step 5: Build and run handler tests**

```bash
cd backend && go build ./... && go test ./internal/api/... -v
```

Expected: PASS — no compile errors, existing handler tests pass.

- [ ] **Step 6: Run full test suite**

```bash
cd backend && go test ./... -count=1
```

Expected: PASS (or pre-existing failures unrelated to this task).

- [ ] **Step 7: Commit**

```bash
cd backend && git add internal/api/handlers.go
git commit -m "feat(handlers): wire integrations and mitreCoverage into insights.Enrich call sites"
```

---

### Task 7: Update Frontend Types

**Files:**
- Modify: `frontend/src/types/index.ts`

- [ ] **Step 1: Write the failing check** (TypeScript compile)

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -20
```

This will currently pass. After editing, rerun to confirm no new errors.

- [ ] **Step 2: Update index.ts**

In `frontend/src/types/index.ts`:

**Add `GapCategories` interface before `InsightsReport`:**

```typescript
export interface GapCategories {
  environment_cleanup: string[];
  no_detection: string[];
  poor_tactic_coverage: string[];
  weak_detection_quality: string[];
  advanced_use_cases: string[];
  missing_source_alerts: string[];
}
```

**Replace `InsightsReport` — remove `enriched_gaps`, add `gap_categories`:**

```typescript
export interface InsightsReport {
  model?: string;
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  gap_categories: GapCategories;
  noise_explanations?: string[];
}
```

**Update `SimilarityResult` — remove `coverage_insights`:**

```typescript
export interface SimilarityResult {
  families: DetectionFamily[];
  duplicates: DuplicateGroup[];
  merge_suggestions: MergeSuggestion[];
  unique_detections: string[];
  noise_alerts?: NoiseAlert[];
}
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit
```

Expected: errors referencing `coverage_insights` and `enriched_gaps` in `AlertInsights.tsx` — fixed in Task 8.

- [ ] **Step 4: Commit**

```bash
cd frontend && git add src/types/index.ts
git commit -m "feat(types): add GapCategories; update InsightsReport and SimilarityResult for 6-category gaps"
```

---

### Task 8: Update AlertInsights.tsx — 6-Category Coverage Tab

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`

- [ ] **Step 1: Run TypeScript check to see current errors**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: errors on `data.coverage_insights` and `effectiveReport?.enriched_gaps` references.

- [ ] **Step 2: Update gapCount calculation**

In `AlertInsights.tsx`, find line ~81:

```typescript
const gapCount   = data.coverage_insights?.length ?? 0;
```

Replace with:

```typescript
const gapCount = effectiveReport
  ? (effectiveReport.gap_categories.environment_cleanup.length +
     effectiveReport.gap_categories.no_detection.length +
     effectiveReport.gap_categories.poor_tactic_coverage.length +
     effectiveReport.gap_categories.weak_detection_quality.length +
     effectiveReport.gap_categories.advanced_use_cases.length +
     effectiveReport.gap_categories.missing_source_alerts.length)
  : 0;
```

- [ ] **Step 3: Add renderGapSection helper**

Add the following helper function inside the component (before the `return` statement, after the `handleRegenerate` function):

```typescript
  const renderGapSection = (title: string, items: string[] | undefined) => {
    if (!items?.length) return null;
    return (
      <div key={title} style={{ marginBottom: 16 }}>
        <div className="eyebrow" style={{ marginBottom: 8 }}>{title}</div>
        {items.map((item, i) => (
          <div key={i} className="insight-card insight-card--coverage">
            <div className="insight-card-header">
              <div className="insight-card-title">{item}</div>
              <span className="badge badge--sky">Gap</span>
            </div>
          </div>
        ))}
      </div>
    );
  };
```

- [ ] **Step 4: Replace coverage tab content**

Find the `{/* ── COVERAGE ── */}` section (around line 341–366). Replace entirely:

```tsx
          {/* ── COVERAGE ── */}
          {activeTab === 'coverage' && (
            gapCount > 0 ? (
              <>
                {renderGapSection('Environment Cleanup', effectiveReport?.gap_categories.environment_cleanup)}
                {renderGapSection('No Detection', effectiveReport?.gap_categories.no_detection)}
                {renderGapSection('Poor Tactic Coverage', effectiveReport?.gap_categories.poor_tactic_coverage)}
                {renderGapSection('Weak Detection Quality', effectiveReport?.gap_categories.weak_detection_quality)}
                {renderGapSection('Advanced Use Cases', effectiveReport?.gap_categories.advanced_use_cases)}
                {renderGapSection('Missing Source Alerts', effectiveReport?.gap_categories.missing_source_alerts)}
              </>
            ) : isLoading || isRegenerating ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
              </>
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No gaps detected</div>
                <div className="state-empty__body">No significant MITRE coverage gaps or alert quality issues found.</div>
              </div>
            )
          )}
```

- [ ] **Step 5: Verify TypeScript compiles with no errors**

```bash
cd frontend && npx tsc --noEmit
```

Expected: PASS — no type errors.

- [ ] **Step 6: Start the dev server and verify the coverage tab**

```bash
cd frontend && npm run dev
```

Open the app, run an analysis, navigate to Insights → Coverage tab. Verify:
- Coverage tab count updates once insights report loads (shows sum of all 6 categories)
- Coverage tab shows 6 section headers with individual gap items under each
- Empty categories are hidden (renderGapSection returns null for empty)
- Loading skeleton shows while report is loading
- Empty state shows when all categories are empty
- Other tabs (Duplicates, Noise, Families) work exactly as before

- [ ] **Step 7: Commit**

```bash
cd frontend && git add src/components/AlertInsights.tsx
git commit -m "feat(ui): render 6-category gap analysis in coverage tab"
```

---

## Self-Review Against Spec

**Spec coverage check:**

| Spec requirement | Task |
|-----------------|------|
| AnalyzeCoverage returns technique-level coverage map (covered bool + alert count per T-code) | Task 3 |
| buildStructuredSignals assembles Claude input JSON | Task 4 |
| Structured JSON covers technique coverage, weak alerts, integration gaps, noise/duplicate summary | Task 4 |
| Enrich signature accepts integrations | Tasks 5, 6 |
| Replace buildPrompt() with structured JSON serialisation | Task 5 |
| 6-category GapCategories response parser | Task 5 |
| GapCategories struct (6 named []string fields) | Task 1 |
| GapCategories added to InsightsReport | Task 1 |
| CoverageInsights removed from SimilarityResult | Tasks 1, 2 |
| EnrichedGaps removed from InsightsReport | Task 1 |
| handlers.go passes integrations into insights.Enrich() | Task 6 |
| frontend InsightsReport type updated to gap_categories shape | Task 7 |
| frontend SimilarityResult removes coverage_insights | Task 7 |
| AlertInsights.tsx 6-category section view | Task 8 |
| Error: malformed JSON → fill missing categories with empty slices | Task 5 (parseGapCategoriesResponse) |
| Error: integration list empty → missing_source_alerts always empty | Task 4 (buildStructuredSignals) |
| Error: nil mitreCoverage → empty signals, Claude still called | Task 4 |
| Test: TestBuildStructuredSignals | Task 4 |
| Test: TestParseGapCategoriesResponse | Task 5 |
| Test: TestAnalyzeCoverage_TechniqueLevel | Task 3 |

**Gaps found:** None. All spec requirements are covered.

**Placeholder scan:** No TBDs, no "implement later", no forward references to undefined types.

**Type consistency:** `GapCategories` struct defined in Task 1 and used identically in Tasks 5, 7, 8. `TechniqueCoverageEntry` defined in Task 1, populated in Task 3, consumed in Task 4. `structuredSignals` is a package-private type in `insights` package — not exported. `Enrich` signature is consistent across Tasks 5 and 6.

**Note on SystemPrompt field:** Task 5 Step 4 includes a conditional check for whether `llm.CompletionRequest.SystemPrompt` exists. If it doesn't, the implementer must add it to the provider interface before the Enrich rewrite will compile.
