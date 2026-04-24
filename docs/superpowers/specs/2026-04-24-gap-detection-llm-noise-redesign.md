# Gap Detection & LLM Noise Accuracy Redesign

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace keyword-based gap detection with MITRE tactic coverage data and fix the LLM prompt so it reliably surfaces noise alerts it receives.

**Architecture:** Three targeted changes across two backend files. `analyzeCoverage()` in the similarity engine is rerouted to consume real MITRE tactic coverage percentages instead of fuzzy English keyword matching. `Analyze()` gains a `*models.MITRECoverageResult` parameter; both call sites in handlers.go pass it in (HandleAnalyze already computes MITRE first; HandleInsights adds a cheap in-memory MITRE call). `buildPrompt()` in enrich.go is tightened to include noise signal metadata and a mandatory LLM instruction.

**Tech Stack:** Go backend — `similarity`, `insights`, `api/handlers` packages; no frontend changes.

---

## Root Causes Being Fixed

### Issue 1: LLM contradicts engine noise findings
`buildPrompt()` omits `trigger_count` and `noise_type` from each noise alert line. The STRICT RULES section has no mandate that `noise_explanations` must be populated when noisy alerts are provided. The LLM silently drops the field.

### Issue 2: False gap identification (keyword matching misses alert names)
`categoryMatchesTokens()` only scans Lucene filter/query tokens — never `alertName`. An alert named "Azure AD - MFA Bypass via Remembered Device" won't match the category "mfa bypass" if the underlying filter query uses `authenticating_device:unmanaged` rather than the words "mfa" or "bypass". The entire `commonCategories` keyword array is a structural approximation; MITRE data is exact and already computed.

### Issue 3: Single-alert "coverage" is misleading
`categoryCounts[cat] == 0` is the only gap condition. One alert matching a category marks it fully covered. A configurable `minTacticCoveragePct = 25.0` threshold introduces three tiers.

---

## Component 1: `backend/internal/similarity/engine.go`

### Changes to `Analyze()`

Signature gains one parameter:

```go
func Analyze(
    alerts []*models.AlertDef,
    eventCounts map[string]int,
    integrationCount int,
    mitreResult *models.MITRECoverageResult,  // NEW — nil = keyword fallback
) *models.SimilarityResult
```

Pass `mitreResult` through to `analyzeCoverage()`.

### Changes to `analyzeCoverage()`

New signature:
```go
func analyzeCoverage(vectors []featureVector, mitreResult *models.MITRECoverageResult) []string
```

**When `mitreResult != nil` (primary path):**

Add constant:
```go
const minTacticCoveragePct = 25.0
```

Logic:
```go
var gaps, thin []string
for _, tc := range mitreResult.Summary.TacticBreakdown {
    if tc.Total == 0 {
        continue
    }
    switch {
    case tc.Covered == 0:
        gaps = append(gaps, tc.TacticName)
    case tc.Percent < minTacticCoveragePct:
        thin = append(thin, fmt.Sprintf("%s (%.0f%%)", tc.TacticName, tc.Percent))
    }
}
sort.Strings(gaps)
sort.Strings(thin)

if len(gaps) > 0 {
    insights = append(insights, fmt.Sprintf("No alert coverage for: %s", strings.Join(gaps, ", ")))
}
for _, t := range thin {
    insights = append(insights, fmt.Sprintf("Thin coverage: %s — consider adding more detections", t))
}
```

Heavy concentration detection (currently: `categoryCounts[cat] >= 5`) is removed — it relied on the same keyword matching. The MITRE data does not directly provide concentration counts per category; this signal is superseded by the per-tactic `covered/total` ratio already in the three-tier output.

**When `mitreResult == nil` (fallback path — HandleInsights before MITRE is added):**

Keep existing `commonCategories` / `categoryMatchesTokens()` logic exactly as-is. This branch is temporary and will be unreachable once HandleInsights is updated in the same task.

### Remove dead code (after both call sites pass mitreResult)

Once both handlers pass a non-nil `mitreResult`, the nil-branch and `commonCategories`, `categoryMatchesTokens()` can be deleted. Do this in the same task — HandleInsights adds MITRE, so the fallback is immediately dead.

### Tests to update

`engine_test.go`: All `similarity.Analyze(...)` calls gain a `nil` fourth argument (existing tests stay valid — nil triggers keyword fallback for backwards compatibility during the test). Add new tests:

- `TestAnalyzeCoverage_mitrePrimaryPath`: pass a `MITRECoverageResult` with two gap tactics and one thin tactic; assert gap and thin strings appear, no keyword-based strings appear.
- `TestAnalyzeCoverage_nilFallback`: pass `nil`; assert keyword-based strings still appear (regression guard while fallback exists).
- `TestAnalyzeCoverage_allCovered`: all tactics ≥ 25%; assert empty insights.

---

## Component 2: `backend/internal/api/handlers.go`

### `HandleAnalyze`

`mitreCoverage` is already computed at line 206, before `similarity.Analyze()` at line 220. Change:

```go
// Before:
alertInsights := similarity.Analyze(alerts, eventCounts, len(integrations))

// After:
alertInsights := similarity.Analyze(alerts, eventCounts, len(integrations), mitreCoverage)
```

No reordering needed.

### `HandleInsights`

Add MITRE computation immediately before the `similarity.Analyze()` call:

```go
// Compute MITRE coverage for accurate gap detection in the insights prompt.
// In-memory computation (~1ms), no external calls.
insightsMitre := mitre.AnalyzeCoverage(alerts)

alertInsights := similarity.Analyze(alerts, insightsEventCounts, 0, insightsMitre)
```

---

## Component 3: `backend/internal/insights/enrich.go`

### Noise alert line format

Current:
```
- AlertName: no entities, no data sources — Fires too frequently across a broad scope
```

New — include `noise_type` tag and trigger count when > 0:
```go
line := fmt.Sprintf("- %s", na.Name)
if na.NoiseType != "" {
    if na.TriggerCount > 0 {
        line += fmt.Sprintf(" [%s, %d×]", na.NoiseType, na.TriggerCount)
    } else {
        line += fmt.Sprintf(" [%s]", na.NoiseType)
    }
}
if len(na.MissingFeatures) > 0 {
    line += fmt.Sprintf(": no %s", strings.Join(na.MissingFeatures, ", no "))
}
if na.Reason != "" {
    line += fmt.Sprintf(" — %s", na.Reason)
}
```

Example output:
```
- Broad Windows Auth Alert [behavioral, 45×]: no entities — Fires too frequently across a broad scope
- Generic IAM Policy Change [structural]: no data sources, no entities — Unscoped high-volume alert type
```

### STRICT RULES addition

Add after the existing rules:
```
- noise_explanations MUST contain exactly one entry per noisy alert listed above — never omit or truncate this field when noisy alerts are present.
```

### JSON schema hint update

Change:
```
"noise_explanations":["<1 sentence each>"]
```
To:
```
"noise_explanations":["<mandatory — one entry per noisy alert, explain the behavioral or structural signal>"]
```

### No other changes to `enrich.go`

The `Enrich()` function, the truncation constants, and the duplicate/family/coverage sections are unchanged.

---

## Data Flow After Changes

```
HandleAnalyze:
  fetchAlerts()
  fetchEventCounts()
  mitreCoverage = mitre.AnalyzeCoverage(alerts)        ← already here
  similarity.Analyze(alerts, eventCounts, n, mitreCoverage)  ← now passes mitreCoverage
    └─ analyzeCoverage(vectors, mitreCoverage)
         └─ MITRE tactic loop → gap / thin / (covered=no insight)
  insights.Enrich(similarityResult, alerts, provider)
    └─ buildPrompt() → includes [behavioral, N×] tags + mandatory noise rule

HandleInsights:
  fetchAlerts()
  fetchEventCounts()
  insightsMitre = mitre.AnalyzeCoverage(alerts)        ← NEW
  similarity.Analyze(alerts, eventCounts, 0, insightsMitre)  ← now passes mitreCoverage
  insights.Enrich(...)
```

---

## What Is Not Changing

- `featureVector` struct — no `nameTokens` field needed (MITRE-based approach makes name tokenization unnecessary)
- Frontend — no changes; `CoverageInsights []string` field remains the same type
- MITRE computation logic in `mitre/mitre.go` — unchanged
- Noise classification logic in `findNoiseAlerts()` — unchanged
- Similarity scoring, duplicate detection, family grouping — unchanged

---

## Acceptance Criteria

1. `similarity.Analyze()` compiles with the new 4-argument signature.
2. `TestAnalyzeCoverage_mitrePrimaryPath` passes: given a MITRE result with two zero-covered tactics and one at 15%, the output contains gap and thin-coverage strings; no keyword-category strings appear.
3. An alert named "Azure AD - MFA Bypass via Remembered Device on Unmanaged Device" is NOT reported as a gap when its tactic (e.g. Credential Access) has > 0 covered techniques.
4. `buildPrompt()` includes `[behavioral, N×]` or `[structural]` tags for each noise alert when `NoiseType != ""`.
5. The STRICT RULES section contains the mandatory noise_explanations rule.
6. All existing tests pass (`go test ./...`).
