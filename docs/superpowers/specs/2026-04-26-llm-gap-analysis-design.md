# LLM-Driven Gap Analysis Design

**Date:** 2026-04-26
**Status:** Approved

## Summary

Replace the current rule-based tactic-level gap strings with a structured-signals → Claude Opus pipeline that categorises findings across 6 actionable gap types and produces an enriched pros/cons summary.

## Before / After

```
Before:
AnalyzeCoverage (tactic-level) → plain strings → Claude prompt context → EnrichedGaps []string

After:
AnalyzeCoverage (technique-level) + buildStructuredSignals() → structured JSON → Claude Opus → GapCategories (6 typed slices) + enriched summary
```

## Six Gap Categories

| Category | Description | Signal source |
|----------|-------------|---------------|
| `environment_cleanup` | Noisy, duplicate, or redundant alerts needing consolidation | Noise alerts + duplicate groups |
| `no_detection` | MITRE techniques with zero alert coverage | Technique coverage map |
| `poor_tactic_coverage` | Tactics with < 25% coverage | Tactic coverage percentages |
| `weak_detection_quality` | Techniques covered but by unscoped/low-quality alerts | Alert quality signals (unscoped, no entity filter) |
| `advanced_use_cases` | Techniques with basic detection but missing behaviour/anomaly layer | Claude reasoning over technique + alert type |
| `missing_source_alerts` | Integrated log sources with zero alerts consuming them | Integration list × alert app/subsystem match |

## Architecture

```
Alerts + Integrations
        │
        ▼
  [Rule layer] buildStructuredSignals()
  ├── Technique-level coverage map (covered bool + alert count per T-code)
  ├── Alert quality signals (unscoped alerts per technique, no entity filter)
  ├── Integration source gaps (integrations with 0 matching alerts)
  ├── Tactic coverage percentages (from AnalyzeCoverage)
  └── Noise/duplicate summary (from similarity.Analyze)
        │
        ▼  structured JSON (~1–2k tokens)
  [Claude Opus] insights.Enrich()
  ├── Categorises findings into 6 gap types
  ├── Writes enriched pros/cons summary
  └── Returns structured JSON
        │
        ▼
  InsightsReport → frontend (6-category view + enriched summary)
```

## Structured Signals Schema (Claude Input)

```json
{
  "alert_count": 120,
  "integration_count": 15,
  "tactic_coverage": {
    "initial-access":   { "pct": 12, "alerts": 2 },
    "reconnaissance":   { "pct": 0,  "alerts": 0 }
  },
  "technique_coverage": {
    "T1078": { "name": "Valid Accounts",       "alerts": 0 },
    "T1059": { "name": "Command Interpreter",  "alerts": 3, "weak": true }
  },
  "integration_gaps": [
    { "name": "Azure AD", "alerts": 0 }
  ],
  "noise_alerts":     ["Alert A fires 200×/month", "Alert B duplicates Alert C"],
  "duplicate_groups": 2
}
```

`weak: true` = alert(s) cover the technique but are unscoped (no app/subsystem filter) or lack entity filters — derived from existing `AlertFeatures`.

## Claude Output Schema

```json
{
  "summary": "Strong credential-access coverage but critical gaps in...",
  "environment_cleanup":    ["Alert B duplicates Alert C — consolidate"],
  "no_detection":           ["T1078 Valid Accounts: no coverage"],
  "poor_tactic_coverage":   ["Reconnaissance: 0% — no alerts for any sub-technique"],
  "weak_detection_quality": ["T1059 coverage is unscoped — alerts fire on all apps"],
  "advanced_use_cases":     ["T1110 Brute Force has threshold alerts but no anomaly baseline"],
  "missing_source_alerts":  ["Azure AD is integrated but has 0 alerts"]
}
```

## Code Changes

| File | Action | Detail |
|------|--------|--------|
| `internal/mitre/mitre.go` | Extend | `AnalyzeCoverage` returns technique-level coverage map (covered bool + alert count per T-code) alongside existing tactic breakdown |
| `internal/insights/signals.go` | **New** | `buildStructuredSignals(result, alerts, integrations)` assembles Claude input JSON |
| `internal/insights/enrich.go` | Rewrite | Replace `buildPrompt()` with structured JSON serialisation; update response parser for 6-category output |
| `internal/models/models.go` | Update | Add `GapCategories` struct (6 named `[]string`); add to `InsightsReport`; remove `CoverageInsights []string` from `SimilarityResult` |
| `internal/api/handlers.go` | Update | Pass `integrations []models.IntegrationInfo` into `insights.Enrich()` |
| `frontend/src/types.ts` | Update | `InsightsReport` type updated to `gap_categories` shape |
| `frontend/src/components/AlertInsights.tsx` | Update | 6-category section view; enriched pros/cons summary layout |

**Deleted:** `SimilarityResult.CoverageInsights` (superseded); `InsightsReport.EnrichedGaps` (merged into `GapCategories`).

**Unchanged:** `similarity.Analyze`, noise detection, MITRE heatmap, suggestions pipeline, Redis cache key scheme.

## System Prompt Rules for Claude

- Return ONLY valid JSON matching the output schema above
- Each category is a `[]string` — empty array if nothing applies, never null
- `summary` is prose (2–4 sentences): start with strengths, then key gaps
- `weak_detection_quality` and `advanced_use_cases` require reasoning — do not fabricate technique names; only use techniques present in the input
- No markdown, no explanation outside the JSON

## Error Handling

| Condition | Behaviour |
|-----------|-----------|
| No alerts have MITRE labels | All techniques appear uncovered; `no_detection` and `poor_tactic_coverage` populated correctly |
| Claude returns malformed JSON | Parser fills missing categories with empty slices; analysis continues |
| Claude returns unknown gap category key | Silently ignored |
| Integration list empty | `missing_source_alerts` always empty; no error |
| `buildStructuredSignals` produces empty signals | Claude still called; returns empty categories + generic summary |

## Testing

- `TestBuildStructuredSignals` — technique coverage map shape, `weak` flag set for unscoped alerts, integration gaps populated, JSON serialises correctly
- `TestParseGapCategoriesResponse` — valid JSON → 6 categories; missing category → empty slice; malformed JSON → nil; markdown-wrapped → stripped and parsed
- `TestAnalyzeCoverage_TechniqueLevel` — technique-level map populated from alert features
- Frontend: `AlertInsights` component tests updated for `gap_categories` prop shape

## What Does Not Change

- `similarity.Analyze` and all noise/duplicate detection
- MITRE heatmap and navigator layer generation
- Suggestions pipeline
- Redis cache (key scheme unchanged — content hash invalidates naturally)
- All other LLM providers and pipeline stages
