# Export Feature Design

**Date:** 2026-04-27
**Status:** Approved

## Summary

Add export capability to the AlertInsights panel. A single Export dropdown button provides five options: export the active tab as XLSX or PDF (instant, client-side), or generate a full LLM-enriched PDF report covering all tabs plus MITRE coverage (on-demand, ~20s, requires a new backend endpoint).

## Out of Scope

- Caching the export narrative (always fresh)
- XLSX format for the full report (PDF only)
- Exporting the Duplicates, Merge, or Unique tabs
- Interactive MITRE heatmap in PDF (table format only)

---

## UX

A single **Export** dropdown button sits in the top-right of the AlertInsights panel alongside the existing Regenerate button.

```
┌─────────────────────────────┐
│  Export ▾                   │
├─────────────────────────────┤
│  Current tab → XLSX         │
│  Current tab → PDF          │
│  ─────────────────────────  │
│  Full report → PDF          │
└─────────────────────────────┘
```

- **Current tab → XLSX / PDF:** Instant download. Active tab determines the data. No backend call.
- **Full report → PDF:** Shows toast immediately: _"Generating your report, this takes ~20 seconds…"_ Then POSTs to `/api/export/narrative`. On success, triggers download. On failure, shows error toast — falls back to offering a data-only PDF without narrative.

**File naming:**
- `{client}-noise.xlsx`, `{client}-families.pdf`, `{client}-gaps.xlsx`, etc. for current-tab exports
- `{client}-full-report-YYYY-MM-DD.pdf` for the full report

---

## Current Tab Export — Column Definitions

### Noise tab

| Column | Source |
|--------|--------|
| Name | `NoiseAlert.name` |
| Noise Type | `NoiseAlert.noise_type` |
| Trigger Count | `NoiseAlert.trigger_count` |
| Reason | `NoiseAlert.reason` |
| Missing Features | `NoiseAlert.missing_features` joined with `, ` |

### Families tab

| Column | Source |
|--------|--------|
| Family Name | `DetectionFamily.name` |
| Alert Count | `DetectionFamily.alert_names.length` |
| Alert Names | `DetectionFamily.alert_names` joined with `, ` |

### Gaps tab

| Column | Source |
|--------|--------|
| Category | Section name (e.g. "No Detection") |
| Item | Prose (from `ActionableRecommendation.prose`) or plain string |
| Severity | `ActionableRecommendation.severity` (empty for plain items) |
| Log Source | `ActionableRecommendation.log_source` (empty for plain items) |
| Query | `ActionableRecommendation.query_skeleton` (empty for plain items) |

---

## Full Report PDF — Section Layout

1. **Cover page** — client name, export date, total alert count, MITRE coverage %
2. **Executive Summary** — LLM-generated narrative (3–4 paragraphs)
3. **Key Findings** — bulleted list from LLM narrative
4. **MITRE Coverage** — table of covered techniques only (AlertCount > 0), grouped by tactic: T-code | Technique Name | Tactic | Alert Count | Quality (Strong/Weak)
5. **Noise Alerts** — same columns as current-tab Noise export
6. **Detection Families** — same columns as current-tab Families export
7. **Gap Analysis** — same columns as current-tab Gaps export
8. **Recommended Actions** — bulleted list from LLM narrative

---

## Backend

### New model additions (`backend/internal/models/models.go`)

```go
// ExportNarrativeReport is the LLM-generated narrative for the full PDF report.
type ExportNarrativeReport struct {
    ExecutiveSummary    string   `json:"executive_summary"`
    KeyFindings         []string `json:"key_findings"`
    RecommendedActions  []string `json:"recommended_actions"`
}
```

### New file: `backend/internal/insights/enrich_export.go`

```go
func EnrichExportNarrative(
    ctx context.Context,
    alerts []*models.AlertDef,
    integrations []models.IntegrationInfo,
    mitreCoverage *models.MITRECoverageResult,
    insightsReport *models.InsightsReport,
    provider llm.Provider,
) (*models.ExportNarrativeReport, error)
```

**Prompt contract (`exportNarrativeSystemPrompt`):**
- Input: JSON with alert_count, integration_count, coverage_percent, tactic_breakdown (covered tactics only), noise_alert_count, duplicate_group_count, gap_summary (category counts), key_gaps (top 5 no_detection items), key_actions (top 5 actionable recommendations prose)
- Output: JSON with `executive_summary` (string), `key_findings` ([]string, max 8), `recommended_actions` ([]string, max 8 prioritised by severity)
- Returns `nil, nil` if `insightsReport` is nil (nothing meaningful to narrate)
- Returns `nil, err` on LLM or parse failure

**Test file:** `backend/internal/insights/enrich_export_test.go`
- Valid JSON response parsed correctly
- `nil` insightsReport returns nil, nil
- Malformed JSON returns error
- LLM error propagates
- Markdown fence stripping

### New handler: `HandleExportNarrative`

`POST /api/export/narrative { "client": "X" }`

1. Load alerts (store-first)
2. ExtractFeatures + AnalyzeCoverage (same as HandleInsights)
3. Run similarity analysis (for noise alerts count)
4. Check insights cache (`insights_v3:{client}:{hash}`) — reuse `InsightsReport` if cached
5. Call `EnrichExportNarrative(ctx, alerts, integrations, mitreCoverage, cachedReport, provider)`
6. Return `ExportNarrativeReport` JSON
7. Result is **not cached** — always generated fresh

Registered in `server.go` as `POST /api/export/narrative`.

---

## Frontend

### New packages

```
npm install xlsx jspdf jspdf-autotable
```

TypeScript types included with jspdf. SheetJS has its own types bundled.

### New file: `frontend/src/utils/export.ts`

All export logic lives here. `AlertInsights.tsx` calls these functions and handles the async toast state.

**Exports from this file:**
```typescript
exportTabAsXLSX(tab: Tab, data: SimilarityResult, report: InsightsReport | null, client: string): void
exportTabAsPDF(tab: Tab, data: SimilarityResult, report: InsightsReport | null, client: string): void
exportFullReportPDF(
  client: string,
  data: SimilarityResult,
  report: InsightsReport,
  mitreCoverage: MITRECoverageResult,
  narrative: ExportNarrativeReport,
  date: string
): void
```

`exportTabAsXLSX` and `exportTabAsPDF` work entirely client-side.
`exportFullReportPDF` is called after the backend returns the narrative.

### Changes to `AlertInsights.tsx`

1. Add `mitreCoverage: MITRECoverageResult` to component props (passed from parent `App.tsx` which already holds `analyzeResponse.mitre_coverage`)
2. Add `isExporting: boolean` state and `exportError: string | null` state
3. Add Export dropdown button (next to Regenerate)
4. On "Full report → PDF": POST `/api/export/narrative`, set `isExporting = true`, show toast, on resolve call `exportFullReportPDF`, clear state
5. On "Current tab → XLSX/PDF": call `exportTabAsXLSX` / `exportTabAsPDF` synchronously

### New service function: `frontend/src/services/api.ts`

```typescript
export async function fetchExportNarrative(client: string): Promise<ExportNarrativeReport>
```

### New types: `frontend/src/types/index.ts`

```typescript
export interface ExportNarrativeReport {
  executive_summary: string;
  key_findings: string[];
  recommended_actions: string[];
}
```

---

## Error Handling

| Failure point | Behaviour |
|--------------|-----------|
| `/api/export/narrative` network error | Error toast: "Export failed. Try again." |
| LLM failure in `EnrichExportNarrative` | 502 from backend → same error toast |
| Clipboard / file-save blocked | Browser handles natively (download dialog) |
| Tab with no data (e.g. 0 noise alerts) | Export still works — empty table with headers only |

---

## Tests

| File | Tests |
|------|-------|
| `enrich_export_test.go` | Valid parse, nil insightsReport, malformed JSON, LLM error, markdown stripping |
| `export.ts` (manual) | Verify XLSX download triggers for each tab; PDF downloads; full report PDF sections present |
