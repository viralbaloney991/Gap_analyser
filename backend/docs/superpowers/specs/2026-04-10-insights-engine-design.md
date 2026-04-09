# Insights Engine Enhancement — Design Spec

**Date:** 2026-04-10
**Scope:** Enhance the alert insights engine with expanded algorithmic signals, LLM enrichment, and a redesigned two-pane frontend.
**Status:** Design approved, ready for implementation

---

## 1. Overview

Three coordinated changes:

1. **Algorithmic improvements** — expand `similarity/engine.go` with 20 coverage categories (up from 7) and noise alert detection
2. **LLM enrichment layer** — new `insights` package that takes the raw `SimilarityResult` and produces an `InsightsReport` via a single LLM call
3. **Frontend redesign** — two-pane Command Center layout: analyst report left panel, detail tabs right panel

---

## 2. Backend — Algorithmic Improvements

### 2.1 Expanded Coverage Categories

**File:** `backend/internal/similarity/engine.go`

Replace `commonCategories` (currently 7 entries) with 20 entries across 5 threat tiers:

```go
var commonCategories = []string{
    // Identity
    "login anomalies",
    "mfa bypass",
    "credential stuffing",
    "token abuse",
    "session hijacking",
    // Endpoint
    "malware execution",
    "persistence",
    "privilege escalation",
    // Cloud
    "iam abuse",
    "storage exfiltration",
    "resource abuse",
    // Network
    "lateral movement",
    "port scanning",
    "c2 traffic",
    // Data
    "data exfiltration",
    "sensitive data access",
    "api abuse",
    // Additional
    "ransomware",
    "supply chain",
    "insider threat",
}
```

No structural change to `analyzeCoverage()` — the expanded slice is the only diff.

### 2.2 Noise Detection

**File:** `backend/internal/similarity/engine.go`

New function:

```go
// findNoiseAlerts returns names of alerts whose total unique feature token
// count is below the noise threshold (sparse = likely threshold-only alert).
func findNoiseAlerts(vectors []featureVector) []string {
    const noiseThreshold = 3
    var noisy []string
    for _, v := range vectors {
        total := len(v.dataSources) + len(v.entities) + len(v.actions) +
            len(v.conditions) + len(v.techniques)
        if total < noiseThreshold {
            noisy = append(noisy, v.alertName)
        }
    }
    sort.Strings(noisy)
    return noisy
}
```

Called in `Analyze()` as Step 8 (after `findUniqueDetections`).

### 2.3 Model Change

**File:** `backend/internal/models/models.go`

Add `NoiseAlerts []string` to `SimilarityResult`:

```go
type SimilarityResult struct {
    Families         []DetectionFamily  `json:"families"`
    Duplicates       []DuplicateGroup   `json:"duplicates"`
    MergeSuggestions []MergeSuggestion  `json:"merge_suggestions"`
    CoverageInsights []string           `json:"coverage_insights"`
    UniqueDetections []string           `json:"unique_detections"`
    NoiseAlerts      []string           `json:"noise_alerts"`   // new
}
```

---

## 3. Backend — LLM Enrichment Layer

### 3.1 New Package

**File:** `backend/internal/insights/enrich.go`

```go
package insights

// Enrich takes a completed SimilarityResult and alert list, sends one
// structured prompt to the LLM, and returns an InsightsReport.
// Returns nil, nil if the result has no meaningful content to enrich.
// Returns nil, err on LLM failure (caller treats as non-fatal).
func Enrich(
    ctx context.Context,
    result *models.SimilarityResult,
    alerts []*models.AlertDef,
    provider llm.Provider,
) (*models.InsightsReport, error)
```

**Prompt structure** — plain text, JSON response requested:

```
You are a security detection engineer reviewing a SIEM alert library.

Alert library ({N} alerts):
{for each alert: "- {name}: sources={...}, actions={...}, techniques={...}"}

Similarity analysis results:
- Duplicates ({N}): {list of "alertA ≈ alertB (score%)"}
- Detection families ({N}): {list of "FamilyName: alert1, alert2, ..."}
- Coverage gaps: {list of gap strings}
- Noise alerts (sparse feature vectors): {list of names}

Respond with JSON only:
{
  "summary": "2-3 sentence overview of the detection posture",
  "top_priority": ["ordered list of 3-5 most important actions"],
  "strengths": ["2-3 things well covered"],
  "recommendations": ["3-5 specific actionable items"],
  "enriched_dups": ["one sentence per duplicate pair explaining business impact"],
  "enriched_gaps": ["one sentence per coverage gap explaining risk"]
}
```

**JSON parsing** — unmarshal into `models.InsightsReport`. If unmarshal fails, return `nil, err`.

### 3.2 New Model

**File:** `backend/internal/models/models.go`

```go
type InsightsReport struct {
    Summary         string   `json:"summary"`
    TopPriority     []string `json:"top_priority"`
    Strengths       []string `json:"strengths"`
    Recommendations []string `json:"recommendations"`
    EnrichedDups    []string `json:"enriched_dups"`
    EnrichedGaps    []string `json:"enriched_gaps"`
}
```

### 3.3 Caching

Cache key: `insights_v1:{clientName}:{sha256(SimilarityResult JSON)[:12]}`

Uses the existing Redis/NeonDB cache infrastructure (same pattern as `HandleSuggestions`). TTL: 24h.

### 3.4 Handler Integration

**File:** `backend/internal/api/handlers.go`

After `similarity.Analyze()`:

```go
insightsReport, _ := insights.Enrich(ctx, similarityResult, alerts, h.suggestionProvider)
// error is intentionally discarded — non-fatal, frontend handles nil
```

`AnalyzeResponse` gets new field:

```go
InsightsReport *models.InsightsReport `json:"insights_report,omitempty"`
```

---

## 4. Frontend

### 4.1 Types

**File:** `frontend/src/types/index.ts`

```ts
export interface InsightsReport {
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  enriched_gaps: string[];
}
```

Add to `AnalyzeResponse`:
```ts
insights_report: InsightsReport | null;
```

### 4.2 Component Layout

**File:** `frontend/src/components/AlertInsights.tsx`

Props change:
```ts
interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;  // new
}
```

**Two-pane layout** (mirrors `IntegrationSummary` Command Center):

```
┌──────────────────────┬──────────────────────────────────────┐
│ Summary paragraph    │  Duplicates │ Families │ Merge │ ...  │
│                      │ ──────────────────────────────────── │
│ TOP PRIORITY         │  [96%]  Login Brute Force             │
│ → 1. Merge 3 logins  │         Login Anomaly                 │
│ → 2. Add lat. mvmt   │         LLM enriched explanation...   │
│ → 3. Fix vault       │                                       │
│                      │  [88%]  MFA Bypass                    │
│ STRENGTHS            │         MFA Fatigue                   │
│  • Identity coverage │         LLM enriched explanation...   │
│  • MFA detection     │                                       │
│                      │                                       │
│ SIGNALS              │                                       │
│  [3] duplicates      │                                       │
│  [2] families        │                                       │
│  [2!] noise          │                                       │
│  [4!] gaps           │                                       │
└──────────────────────┴──────────────────────────────────────┘
```

**Left panel** (`~220px`):
- Summary paragraph (from `report.summary`; skeleton placeholder when `report` is null)
- TOP PRIORITY — numbered list from `report.top_priority`
- STRENGTHS — bulleted list from `report.strengths`
- SIGNALS — stat counts for each insight type; noise + gap counts styled in danger red when > 0

**Right panel** — 5 tabs (+ new Noise tab = 6 total):
- **Duplicates** — uses `report.enriched_dups[i]` as explanation when available, falls back to algorithmic `dup.explanation`
- **Detection Families** — unchanged
- **Merge Suggestions** — unchanged
- **Coverage** — uses `report.enriched_gaps[i]` when available, falls back to raw `coverage_insights[i]`
- **Noise** — new tab; lists `data.noise_alerts`; each item shown as `[!!] alertName` with explanation: "Sparse feature vector — likely a threshold-only rule. Review for contextual conditions."
- **Unique** — unchanged

### 4.3 App.tsx

Pass `report` prop:
```tsx
<AlertInsights data={data.alert_insights} report={data.insights_report} />
```

### 4.4 CSS

New classes in `App.css` (insights section):
- `.insights-grid` — two-col grid, 220px + 1fr, same pattern as `.summary-grid`
- `.insights-panel` — left panel, flex column
- `.insights-panel-summary` — summary text block
- `.insights-panel-section` — section within panel (TOP PRIORITY, STRENGTHS, SIGNALS)
- `.insights-panel-section-title` — small uppercase label
- `.insights-panel-item` — single item in a list
- `.insights-panel-item--priority` — numbered, accent colour
- `.insights-panel-item--danger` — danger colour for noise/gap counts
- `.insights-tabs-panel` — right panel, flex column, overflow-y auto
- `.insights-skeleton` — pulsing placeholder lines shown when `report` is null

---

## 5. Files Changed

| File | Change |
|------|--------|
| `backend/internal/models/models.go` | Add `NoiseAlerts` to `SimilarityResult`; add `InsightsReport` struct; add `InsightsReport` field to `AnalyzeResponse` |
| `backend/internal/similarity/engine.go` | Expand `commonCategories`; add `findNoiseAlerts()`; call it in `Analyze()` |
| `backend/internal/insights/enrich.go` | New file — `Enrich()` function |
| `backend/internal/api/handlers.go` | Call `insights.Enrich()` after similarity analysis; include result in response |
| `frontend/src/types/index.ts` | Add `InsightsReport` interface; add field to `AnalyzeResponse` |
| `frontend/src/components/AlertInsights.tsx` | Two-pane layout, Noise tab, LLM-enriched explanations with fallback |
| `frontend/src/App.css` | Add insights grid/panel classes |
| `frontend/src/App.tsx` | Pass `report` prop to `AlertInsights` |

---

## 6. Out of Scope

- Per-alert severity data (not in current `AlertDef` model)
- Staleness detection (no timestamp on alerts)
- Streaming LLM response to frontend
- User feedback on insights quality
