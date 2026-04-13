# AlertInsights Engine — 7-Issue Remediation Design

**Date:** 2026-04-13  
**Status:** Approved  
**Approach:** Two parallel branches — backend fixes first, then frontend redesigns

---

## Summary

Seven bugs identified in the AlertInsights engine across the similarity scoring engine, LLM enrichment layer, and frontend UI. Fixes are grouped into two independent tracks: a small backend branch (low-risk, < 30 lines) and a larger frontend branch (two component rewrites + one enhancement).

---

## Track 1: Backend Fixes (`fix/insights-backend`)

### Issue 1 — LLM Insights Provider 404 Errors

**File:** `backend/internal/api/handlers.go` (~line 225)

**Root cause:** The insights enrichment provider is created with `h.config.LLM.SuggestionModel`, which contaminates the NVIDIA provider with a model string intended for the suggestion endpoint — causing 404 errors.

**Fix:** Pass `""` as the model argument so each provider falls back to its own configured default.

```go
// before
insightsProvider, providerErr := llm.NewClassifierProvider(
    h.config.LLM.SuggestionProvider,
    h.config.LLM.SuggestionModel,
    ...
)

// after
insightsProvider, providerErr := llm.NewClassifierProvider(
    h.config.LLM.SuggestionProvider,
    "",
    ...
)
```

---

### Issue 2 — Similarity Weight Overflow (scores > 1.0)

**File:** `backend/internal/similarity/engine.go` (lines 36–41)

**Root cause:** When `weightGroupBy = 0.25` was added, the remaining weights were scaled ×0.75 but the result sums to 1.02, silently inflating all similarity scores.

**Fix:** Replace with clean round numbers that sum to exactly 1.00:

| Dimension | Old | New |
|---|---|---|
| dataSources | 0.15 | 0.15 |
| entities | 0.11 | 0.10 |
| actions | 0.15 | 0.15 |
| conditions | 0.15 | 0.15 |
| techniques | 0.11 | 0.10 |
| groupBy | 0.25 | 0.25 |
| alertType | 0.10 | 0.10 |
| **Total** | **1.02** | **1.00** |

---

### Issue 3 — UniqueDetections Returns IDs Instead of Names

**File:** `backend/internal/similarity/engine.go` (line 833)

**Root cause:** `findUniqueDetections` appends `vectors[i].alertID` (UUID) but `SimilarityResult.UniqueDetections []string` is rendered as display names in the frontend `UniqueView`.

**Fix:** One-line change:
```go
// before
unique = append(unique, vectors[i].alertID)
// after
unique = append(unique, vectors[i].alertName)
```

---

### Issue 4 — Noise Alerts: Generic Enrichment Text

**Files:** `backend/internal/insights/enrich.go`, `backend/internal/models/`

**Part A — Model change:** Change `SimilarityResult.NoiseAlerts` from `[]string` to `[]NoiseAlert`:
```go
type NoiseAlert struct {
    Name            string   `json:"name"`
    MissingFeatures []string `json:"missing_features"`
}
```
The similarity engine populates `MissingFeatures` by inspecting which feature sets (data sources, actions, entities, conditions, techniques) are empty for that alert.

**Part B — LLM enrichment:** Add `noise_explanations` to the enrichment prompt JSON schema — one sentence per noisy alert. Update `InsightsReport`:
```go
NoiseExplanations []string `json:"noise_explanations"`
```

---

## Track 2: Frontend Redesigns (`feat/insights-frontend`)

*Depends on Track 1 being merged first for the `NoiseAlert` model change.*

### Issue 5 — ClientSelector: Replace World Map with Region Cards Grid

**File:** `frontend/src/components/ClientSelector.tsx` (full rewrite)

**Root cause:** The SVG world map places all clients for a given region at a single coordinate — multiple clients stack invisibly. Additionally, `react-simple-maps` is a heavyweight dependency for this use case.

**Design:**
- Remove `react-simple-maps` dependency entirely
- Two-level layout: region header cards (EU1, EU2, US1, US2, AP1, AP2, AP3) as rows, client cards nested beneath each region
- Click a client card to select it; selected state highlighted
- Selected client label + Analyze button remain at bottom, matching current behaviour
- No coordinate mapping required; region is read directly from `ClientInfo.region`

---

### Issue 6 — MITREHeatmap: Replace Flat Table with D3 Force-Directed Graph

**File:** `frontend/src/components/MITREHeatmap.tsx` (full rewrite)

**Design:**
- Install `d3` package
- `useEffect` hook runs a D3 force simulation on mount and on data change
- **Nodes:**
  - Center node: overall coverage % label
  - Tactic nodes (14): medium circles, labeled with short tactic name
  - Technique nodes: small circles, color = existing `coverageColor()` function (green/amber/orange/red)
- **Edges:** tactic → technique lines, low opacity
- Click a technique node → opens existing detail panel (suggestion generation logic unchanged)
- Drag to reposition nodes; zoom/pan via D3 zoom
- SVG rendered inside a `useRef` container; simulation cleaned up on unmount

---

### Issue 7 — AlertInsights: Left Panel + NoiseView Updates

**File:** `frontend/src/components/AlertInsights.tsx`

**7a — Recommendations section:** Add a Recommendations card below Strengths in the left panel, same style as existing sections. Renders `report.recommendations` as a bulleted list.

**7b — Null guards + fallback text:** All 6 report fields (`summary`, `top_priority`, `strengths`, `recommendations`, `enriched_dups`, `enriched_gaps`) get null/empty-array guards. When absent, render a fallback string:
- Summary: `"Enrichment unavailable — check LLM provider configuration."`
- Lists: `"No [section name] available."`

**7c — NoiseView per-alert explanations (two layers):**
- **Default layer:** render `noise_alert.missing_features` as a computed sentence, e.g. `"Missing: data sources, actions"` (from the new `NoiseAlert` struct)
- **Enrichment overlay:** if `InsightsReport.noise_explanations[i]` exists, render it in place of the default

Frontend type update required: `noise_alerts: string[]` → `noise_alerts: NoiseAlert[]` in `types.ts`.

---

## Data Flow

```
similarity.Analyze()
  └─ findNoiseAlerts() → []NoiseAlert{Name, MissingFeatures}
        └─ stored in SimilarityResult.NoiseAlerts

insights.Enrich()
  └─ prompt includes noise alert names
  └─ requests noise_explanations[] in JSON response
  └─ stored in InsightsReport.NoiseExplanations

Frontend
  └─ NoiseView renders MissingFeatures as default text
  └─ overlays NoiseExplanations[i] when available
```

---

## Testing

- **Backend:** existing similarity engine tests cover weight normalization (score range assertions). Add regression test confirming weights sum to 1.00. UniqueDetections fix verified by asserting returned strings match alert names not UUIDs.
- **LLM fix:** integration test or manual verify with `refresh=true` confirming no 404.
- **Frontend:** visual verification in browser for ClientSelector grid and D3 graph. Null-guard paths tested by passing `null` report to `AlertInsights` component.

---

## Out of Scope

- Adding new LLM providers or model configuration fields
- Changing the duplicate detection threshold
- Any changes to the Monday.com integration or MITRE classifier pipeline
