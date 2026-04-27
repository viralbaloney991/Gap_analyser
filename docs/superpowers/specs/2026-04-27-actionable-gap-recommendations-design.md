# Actionable Gap Recommendations

**Date:** 2026-04-27
**Status:** Approved

## Summary

Replace vague gap-analysis strings in four categories (No Detection, Weak Detection Quality, Missing Source Alerts, Advanced Use Cases) with structured, actionable recommendations: a one-sentence prose description, a recommended severity, the client's matching log source, and a Lucene query skeleton. The two remaining categories (Environment Cleanup, Poor Tactic Coverage) stay as plain strings.

## Problem

Current gap items read like: _"Build detections for technique T1078"_. Engineers cannot act on this without knowing which log source to target, what severity to assign, or what query to write. The result is that gap recommendations are read and ignored.

## Goals

- Every actionable gap item includes: prose, severity, log source, and a Lucene query skeleton
- Log sources are grounded in the client's actual integrations — no phantom sources suggested
- Items with no matching client integration are omitted rather than fabricated
- No increase in API response latency (generation happens in the existing background worker)
- Plain-string rendering for Environment Cleanup and Poor Tactic Coverage is unchanged

## Out of Scope

- Export of recommendations (separate feature)
- Noise day selector (separate feature)
- Per-technique heatmap suggestions (separate existing feature — not merged)

---

## Data Model

Two new types added to `backend/internal/models/models.go`:

```go
type ActionableRecommendation struct {
    Prose         string `json:"prose"`          // one-sentence description
    LogSource     string `json:"log_source"`     // client integration name
    Severity      string `json:"severity"`       // "critical"|"high"|"medium"|"low"
    QuerySkeleton string `json:"query_skeleton"` // valid Lucene query string
}

type ActionableGapCategories struct {
    NoDetection          []ActionableRecommendation `json:"no_detection"`
    WeakDetectionQuality []ActionableRecommendation `json:"weak_detection_quality"`
    MissingSourceAlerts  []ActionableRecommendation `json:"missing_source_alerts"`
    AdvancedUseCases     []ActionableRecommendation `json:"advanced_use_cases"`
}
```

`InsightsReport` gains one new optional field:

```go
ActionableGaps *ActionableGapCategories `json:"actionable_gaps,omitempty"`
```

`ActionableGaps` is `nil` when the enrichment call has not completed or returned no results. Frontend treats `nil` as "not yet available" and falls back to plain-string rendering.

---

## Backend

### New file: `backend/internal/insights/enrich_actionable.go`

**Function signature:**

```go
func EnrichActionable(
    ctx context.Context,
    gaps models.GapCategories,
    integrations []models.IntegrationInfo,
    provider llm.Provider,
) (*models.ActionableGapCategories, error)
```

**Prompt contract (`actionableSystemPrompt`):**

- Input: JSON with two fields — `gaps` (the 4 actionable category string arrays) and `integrations` (client integration list with name, application, subsystem, alert_count)
- Output: JSON matching `ActionableGapCategories` schema
- Rules enforced in prompt:
  - `log_source` must be the name of an integration from the input list
  - If no integration matches a gap item, omit that item entirely (no fabrication)
  - `severity` must be one of: `critical`, `high`, `medium`, `low`
  - `query_skeleton` must be valid Lucene syntax targeting the chosen log source
  - `prose` is one sentence, action-oriented, referencing the technique or gap by name
  - Respond with only valid JSON — no prose, no markdown

**Error handling:** Returns `nil, nil` when `gaps` has no actionable items. Returns `nil, err` on LLM failure. Caller (`runInsightsBackground`) logs the error and proceeds without actionable gaps — `InsightsReport.ActionableGaps` remains nil.

**Test file:** `backend/internal/insights/enrich_actionable_test.go`
- Valid JSON response parsed correctly into `ActionableGapCategories`
- Items with no matching integration are omitted
- Malformed JSON returns error
- Empty gap categories returns `nil, nil`
- LLM error propagates

### Changes to `backend/internal/api/handlers.go`

In `runInsightsBackground`, after `Enrich()` returns successfully:

```
report, err := insights.Enrich(...)       // existing gap analysis call
if err == nil && report != nil {
    actionable, aErr := insights.EnrichActionable(ctx, report.GapCategories, integrations, insightsProvider)
    if aErr != nil {
        log.Printf("WARN [insights] actionable enrichment failed: %v", aErr)
    }
    report.ActionableGaps = actionable    // nil if failed — frontend degrades gracefully
}
// cache report
```

Cache key bumped: `insights_v2` → `insights_v3` (response shape change).

---

## Frontend

### Rendering in `AlertInsights.tsx`

The Gaps tab renders two different item styles based on whether actionable data is available for a category.

**Actionable item card** (used when `actionable_gaps` is populated for that category):

```
┌─────────────────────────────────────────────────┐
│ [CRITICAL] AWS CloudTrail                        │
│ Build T1078 detection on AWS CloudTrail for      │
│ privileged user creation events.                 │
│                                                  │
│ ▶ Show query                                     │
│   ┌─────────────────────────────────────────┐   │
│   │ eventSource=iam.amazonaws.com AND        │   │
│   │ eventName=CreateUser                     │   │
│   └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

- Severity badge: critical=red, high=orange, medium=yellow, low=blue
- Log source label next to badge
- Prose sentence below
- "Show query" toggle (collapsed by default) → monospace code block + copy button

**Plain item** (used when `actionable_gaps` is null or category has no actionable items): existing `<li>` string rendering — unchanged.

**Progressive enhancement:** On first load, `actionable_gaps` is `null` (background still running). Actionable categories show their plain `GapCategories` strings. Once the API response is refreshed and `actionable_gaps` is populated, items upgrade to cards. No spinner, no loading state needed.

---

## Cache

| Key pattern | Version | Change |
|-------------|---------|--------|
| `insights_v3:{client}:{hash}` | new | Adds `actionable_gaps` to cached payload |
| `insights_v2:{client}:{hash}` | deprecated | Returns without `actionable_gaps`; stale entries will miss and regenerate |

---

## Tests

| File | Tests |
|------|-------|
| `enrich_actionable_test.go` | Valid parse, integration grounding (omit unmatched), malformed JSON error, empty gaps nil-nil, LLM error propagation |
| `handlers_test.go` (existing) | `runInsightsBackground` now sets `ActionableGaps` on success; nil on EnrichActionable failure |
| `AlertInsights.tsx` (manual) | Actionable card renders with query toggle; plain fallback when `actionable_gaps` null |
