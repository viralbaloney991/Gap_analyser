# LLM-Assisted Noise Detection for Unscoped `logs_immediate` Alerts

**Date:** 2026-04-26
**Status:** Approved

## Summary

Extend the gap analysis pipeline to surface `logs_immediate` alerts that are unscoped (no app/subsystem filter, no entity filter) as noise candidates for Claude to assess. Rule-confirmed noise (behavioral/structural) stays in the Noise tab unchanged. LLM-identified candidates appear in `environment_cleanup` in the Coverage tab.

## Problem

`logs_immediate` alerts are excluded from structural noise detection because they fire on individual log matches rather than aggregating over time. However, an unscoped `logs_immediate` alert monitoring a high-frequency event (e.g. "Power Apps App Launched") fires on every matching event across all tenants and log sources — producing excessive volume even with a specific Lucene query.

When event count data is unavailable (API failure), behavioral noise detection also cannot fire, leaving these alerts invisible to the noise pipeline entirely.

## Before / After

```
Before:
logs_immediate alerts → excluded from structural noise
                      → behavioral-only (requires eventCounts + triggerCount > 20)
                      → invisible when event API fails

After:
logs_immediate alerts → pre-filter (unscoped, no entity, security, not BB/vendor)
                      → immediate_noise_candidates [{name, query, trigger_count}]
                      → Claude Opus assesses query breadth + frequency semantics
                      → environment_cleanup in Coverage tab
```

## Architecture

```
alerts + eventCounts (optional)
        │
        ▼
  [buildStructuredSignals]                     ← signals.go
  Pre-filter: logs_immediate
              + IsSecurityAlert
              + not IsBuildingBlock
              + not VendorCovered
              + ExtractAppSubsystem → ("","")
              + len(Entities) == 0
  Cap: 30 candidates (sorted by name, deterministic)
        │
        ▼  immediate_noise_candidates [{name, query, trigger_count}]
  [Claude Opus] enrich.go
  Assesses query breadth and frequency semantics per candidate
  → environment_cleanup entries for likely high-frequency alerts
        │
        ▼
  Coverage tab > Environment Cleanup section    ← no frontend change
```

Rule-confirmed noise (behavioral/structural from `findNoiseAlerts`) is unchanged and continues to populate the Noise tab.

## Data Model

### New struct in `signals.go`

```go
type signalsImmediateCandidate struct {
    Name         string `json:"name"`
    Query        string `json:"query"`
    TriggerCount int    `json:"trigger_count,omitempty"` // 0 = not available
}
```

### Updated `structuredSignals`

```go
type structuredSignals struct {
    AlertCount               int                              `json:"alert_count"`
    IntegrationCount         int                              `json:"integration_count"`
    TacticCoverage           map[string]signalsTacticEntry    `json:"tactic_coverage"`
    TechniqueCoverage        map[string]signalsTechniqueEntry `json:"technique_coverage"`
    IntegrationGaps          []signalsIntegrationGap          `json:"integration_gaps"`
    NoiseAlerts              []string                         `json:"noise_alerts"`
    DuplicateGroups          int                              `json:"duplicate_groups"`
    ImmediateNoiseCandidates []signalsImmediateCandidate      `json:"immediate_noise_candidates"`
}
```

### Updated `buildStructuredSignals` signature

```go
func buildStructuredSignals(
    result        *models.SimilarityResult,
    alerts        []*models.AlertDef,
    integrations  []models.IntegrationInfo,
    mitreCoverage *models.MITRECoverageResult,
    eventCounts   map[string]int, // nil = trigger_count omitted for all candidates
) structuredSignals
```

`Enrich()` gains the same `eventCounts map[string]int` parameter and threads it into `buildStructuredSignals`.

## Pre-Filter Logic

```go
const maxImmediateCandidates = 30

var candidates []signalsImmediateCandidate
for _, alert := range alerts {
    if alert.AlertType != "logs_immediate" {
        continue
    }
    if !alert.Features.IsSecurityAlert || alert.Features.IsBuildingBlock || alert.Features.VendorCovered {
        continue
    }
    app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
    if app != "" || sub != "" {
        continue
    }
    if len(alert.Features.Entities) > 0 {
        continue
    }
    query := coralogix.ExtractLuceneQuery(alert.TypeDef)
    candidates = append(candidates, signalsImmediateCandidate{
        Name:         alert.Name,
        Query:        query,
        TriggerCount: eventCounts[alert.ID],
    })
}
sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
if len(candidates) > maxImmediateCandidates {
    candidates = candidates[:maxImmediateCandidates]
}
```

## System Prompt Changes

### Input description addition (after `noise_alerts` line)

```
- immediate_noise_candidates: unscoped logs_immediate security alerts with no entity filter
  [{name, query, trigger_count}] — trigger_count is 0 when event data unavailable
```

### Rules addition

```
- immediate_noise_candidates: for each entry, assess whether the Lucene query targets a
  high-frequency event (common user actions, broad field matches, platform lifecycle events).
  If yes, flag in environment_cleanup with a specific recommendation to add app/subsystem
  scoping or an entity filter. If the query is narrow enough to be low-frequency by nature,
  do not flag it. Use trigger_count as a signal when > 0; reason from query semantics when 0.
```

### Examples

**Should flag:**
- "Power Apps App Launched" — fires on every Power Apps session across all tenants
- "User Login" with broad Lucene query — matches all authentication events

**Should not flag:**
- "AWS S3 Bucket Policy Deleted" — specific administrative event, low-frequency by nature
- "Sudo Command Executed with Root" — rare privilege escalation event

## Handler Changes

`eventCounts` is already fetched in both `HandleAnalyze` (via `runInsightsBackground`) and `HandleInsights` before calling `Enrich`. Both call sites pass `eventCounts` as the new parameter.

`HandleInsights` currently fetches `insightsEventCounts` at line 420. This is threaded into `Enrich` as the new parameter.

## Error Handling

| Condition | Behaviour |
|-----------|-----------|
| `eventCounts` nil | `TriggerCount` 0 for all candidates (`omitempty` omits field from JSON); Claude reasons from query only |
| No candidates after pre-filter | `immediate_noise_candidates: []`; Claude skips silently |
| >30 candidates | First 30 by name (sorted, deterministic) |
| Claude over-flags narrow queries | Treated as any LLM output; no runtime guard needed |

## Testing

| Test | What it verifies |
|------|-----------------|
| `TestBuildStructuredSignals_ImmediateCandidates_Included` | Unscoped `logs_immediate` with no entity → in candidates |
| `TestBuildStructuredSignals_ImmediateCandidates_Excluded` | Scoped / building block / vendor-covered / non-immediate → excluded |
| `TestBuildStructuredSignals_ImmediateCandidates_Cap` | 35 candidates → capped at 30, sorted by name |
| `TestBuildStructuredSignals_ImmediateCandidates_NilEventCounts` | `TriggerCount` 0 for all entries when `eventCounts` nil |

## Files Changed

| File | Change |
|------|--------|
| `internal/insights/signals.go` | New `signalsImmediateCandidate` struct, new field on `structuredSignals`, pre-filter logic, `eventCounts` param added |
| `internal/insights/enrich.go` | System prompt: new input description + assessment rule; `eventCounts` param threaded into `buildStructuredSignals` |
| `internal/api/handlers.go` | Pass `eventCounts` into `Enrich()` at both call sites (`runInsightsBackground` and `HandleInsights`) |
| `internal/insights/signals_test.go` | 4 new tests |

## What Does Not Change

- `findNoiseAlerts` — rule-confirmed behavioral/structural noise detection unchanged
- Noise tab — unchanged; only rule-confirmed alerts appear there
- All other gap categories and LLM pipeline stages
- Frontend — `environment_cleanup` already renders in Coverage tab
- Redis cache key scheme — content hash invalidates naturally
