# Noise Detection Improvements Design

**Date:** 2026-04-28
**Status:** Approved

## Summary

Noise tab consistently shows 0 alerts. Root cause (from PayU debug log): behavioral threshold is too high (20), structural noise has a type whitelist that excludes most alert types, and query breadth is not considered. Fix with a three-layer model: wider engine net + LLM validation filter + UI sub-filtering. Implemented in two phases.

---

## Diagnosis

PayU analysis (1029 alerts) debug log breakdown:
```
vendor=135  bb=0  non-sec=3  no-signal=891
no-signal reasons:
  scoped:542           correct — has app/subsystem filter
  type=logs_immediate:122   excluded by type whitelist — too strict
  flow_below_threshold:75   fires ≤10×, missed behavioral threshold
  has_entity:58        correct — entity filter narrows scope
  type=logs_unique_count:53 excluded by type whitelist — too strict
  type=logs_anomaly:19      excluded by type whitelist — debatable
  zero_triggers:12     correct — dormant alerts
  type=logs_new_value:10    excluded by type whitelist — too strict
```

Two root causes:
1. **Behavioral threshold 20** — too high; flow and other types firing 11-19× are missed
2. **Structural type whitelist** — only `logs_threshold` + `metric_threshold` qualify; 204 alerts excluded purely due to type

---

## Scope

**In scope (Phase 1 — engine + UI):**
- Lower behavioral threshold 20 → 10, flat across all types including flow
- Remove structural noise type whitelist — all types qualify
- Add broad-query signal as third structural condition
- Remove flow-alert structural exclusion — flow goes through same path
- UI filter pills: [All] [Behavioral] [Structural] within Noise tab

**In scope (Phase 2 — LLM validation):**
- Batch LLM validation of engine candidates via insights pipeline
- NeonDB cache keyed by alert hash
- Frontend validation state (pending → validated)

**Out of scope:**
- Changing the behavioral threshold per alert type
- Modifying insights `noise_explanations` (existing field, kept as-is)
- Changing lookback window logic (already implemented)

---

## Phase 1: Engine + UI

### Backend — `engine.go`

#### Behavioral threshold

```go
const behavioralNoiseThreshold = 10 // lowered from 20
```

Applies flat to all alert types. Flow alerts use the same threshold (remove separate `flow_below_threshold` path).

#### Structural noise — remove type whitelist

Current condition:
```go
isHighVolumeType := alert.AlertType == "logs_threshold" ||
    alert.AlertType == "metric_threshold"
isStructural = isUnscoped && noEntity && isHighVolumeType && hasEvidenceOfVolume
```

New condition (no type whitelist):
```go
isBroadQuery := hasWildcardQuery(v.luceneQuery) ||
    avgIDF(v.luceneQuery, idf.luceneQuery) < computeQueryIDFThreshold(vectors, idf)

isStructural = noEntity && hasEvidenceOfVolume && (isUnscoped || isBroadQuery)
```

**Three structural signals (any one qualifies):**
- `isUnscoped` — no app name AND no subsystem (existing)
- `isBroadQuery` — new (see below)

**Entity and volume guards stay:**
- `noEntity`: no entity filters (existing)
- `hasEvidenceOfVolume`: `eventCounts == nil || triggerCount > 0` (existing)

#### Broad query signal

```go
// hasWildcardQuery returns true if the tokenised query contains a wildcard token.
func hasWildcardQuery(tokens map[string]struct{}) bool {
    for t := range tokens {
        if strings.Contains(t, "*") || t == "_exists_" {
            return true
        }
    }
    return false
}

// avgIDF returns the mean IDF weight of the query tokens.
// Returns 1.0 (maximum specificity) for an empty token set.
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

// computeQueryIDFThreshold returns the 25th-percentile average query IDF
// across all vectors — alerts whose query IDF falls below this are broad.
func computeQueryIDFThreshold(vectors []featureVector, idf idfTable) float64 {
    scores := make([]float64, 0, len(vectors))
    for _, v := range vectors {
        scores = append(scores, avgIDF(v.luceneQuery, idf.luceneQuery))
    }
    sort.Float64s(scores)
    p25 := int(math.Floor(float64(len(scores)) * 0.25))
    if p25 >= len(scores) {
        return 0
    }
    return scores[p25]
}
```

`computeQueryIDFThreshold` is called once per `Analyze` run (same as `buildIDF`) and passed into `findNoiseAlerts`.

#### Updated `findNoiseAlerts` signature

```go
func findNoiseAlerts(
    vectors []featureVector,
    alerts []*models.AlertDef,
    eventCounts map[string]int,
    integrationCount int,
    idf idfTable,            // added — needed for avgIDF
    queryIDFThreshold float64, // added — pre-computed p25
) []models.NoiseAlert
```

Callers in `Analyze` pass `idf` (already computed) and the threshold.

#### Updated debug log reasons

Removed: `type=*`, `flow_below_threshold`
Remaining:
- `scoped_and_specific_query` — unscoped check failed AND query not broad
- `has_entity` — entity filter present
- `zero_triggers` — no activity with event counts available
- `behavioral_below_threshold` — trigger count ≤ 10

#### Noise reason text update

```go
// behavioral
"Fired %d times in the last %d days — alert is over-triggering."

// structural — broad query
"Broad query with no entity filter — fires on matching events across all log sources."

// structural — unscoped
"No app/subsystem scoping across an org with %d integrations — fires on all matching log sources."

// both
"Unscoped and over-triggering: fired %d times across all log sources."
```

---

### Frontend — Noise tab filter pills

Above the noise alert list, add type filter pills:

```
[ All ]  [ Behavioral ]  [ Structural ]
```

- "All" (default) — shows all `noise_alerts`
- "Behavioral" — filters to `noise_type === 'behavioral' || noise_type === 'both'`
- "Structural" — filters to `noise_type === 'structural' || noise_type === 'both'`

State: `const [noiseFilter, setNoiseFilter] = useState<'all'|'behavioral'|'structural'>('all')`

These pills are distinct from the lookback `NoisePills` (days selector). Both appear in the Noise tab: days selector row first, type filter pills row second, alert list below.

Implementation: render as three `<button>` elements styled the same as existing UI buttons, not using the `NoisePills` component (which is for the lookback window).

---

## Phase 2: LLM Validation

### Design

Engine produces candidates (over-inclusive by design). LLM validates in batch via the existing insights enrichment pipeline.

### Data model — `InsightsReport`

Add field to `models.InsightsReport`:

```go
ValidatedNoiseIDs []string `json:"validated_noise_ids,omitempty"`
// Alert names confirmed by LLM as genuinely noisy.
// nil = validation not yet run. Empty slice = LLM found none valid.
```

### LLM validation in `insights/enrich.go`

During `Enrich()`, after existing enrichment:

```go
// Validate noise candidates with LLM
if len(alertInsights.NoiseAlerts) > 0 {
    validated, err := validateNoiseCandidates(ctx, provider, alertInsights.NoiseAlerts)
    if err == nil {
        report.ValidatedNoiseIDs = validated
    }
    // on error: leave ValidatedNoiseIDs nil → frontend shows engine candidates
}
```

`validateNoiseCandidates` sends a single batched prompt:

```
You are a security alert analyst. Review these alerts flagged as potentially noisy by our detection engine. For each, decide if it is genuinely noisy.

Return JSON: {"validated": ["Alert Name 1", "Alert Name 3", ...]}
Only include alerts you confirm are genuinely noisy. Omit false positives.

Alerts:
1. Name: "Okta Multiple Failed Logins"
   Type: logs_threshold | Scope: unscoped | Query: severity:error | Triggers: 0 | Signal: structural
2. ...
```

### NeonDB cache

```sql
CREATE TABLE IF NOT EXISTS noise_validation_cache (
    alert_hash  TEXT        PRIMARY KEY,
    is_noisy    BOOLEAN     NOT NULL,
    reason      TEXT        NOT NULL,
    cached_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Cache key: `SHA256(alert.Name + alert.AlertType + alert.Query + scope + noiseType)`

On each validation:
1. Check cache for each candidate
2. Only send uncached candidates to LLM
3. Write LLM results to cache
4. Merge cached + LLM results → `ValidatedNoiseIDs`

### Frontend validation state

```typescript
// In AlertInsights noise tab:
const validatedIDs = report?.validated_noise_ids;
const isValidating = !!report && validatedIDs === undefined; // report loaded but no validated_noise_ids yet
// validatedIDs === null → not in report yet (insights still loading)
// validatedIDs === [] → LLM ran, found nothing
// validatedIDs === ['Alert X'] → show only these

const displayedNoise = validatedIDs != null
  ? data.noise_alerts.filter(n => validatedIDs.includes(n.name))
  : data.noise_alerts; // show engine candidates while validating
```

Show subtle "Verifying with AI…" label next to the filter pills while `isValidating`.

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| LLM validation fails | `ValidatedNoiseIDs` stays nil; frontend shows all engine candidates |
| IDF threshold computation on empty corpus | `computeQueryIDFThreshold` returns 0 (no alerts flagged as broad-query) |
| `avgIDF` on empty query | Returns 1.0 (maximum specificity — not broad) |
| NeonDB cache unavailable | Skip cache, send all candidates to LLM |

---

## Tests

### Phase 1 — `engine_test.go`

| Test | What it verifies |
|------|-----------------|
| `TestFindNoiseAlerts_behavioralThreshold_10` | Alert with 10 triggers → not flagged; 11 → flagged |
| `TestFindNoiseAlerts_flowAlert_behavioral` | Flow alert with 11 triggers → behavioral noise |
| `TestFindNoiseAlerts_flowAlert_structural_unscoped` | Flow alert, unscoped, no entity → structural noise |
| `TestFindNoiseAlerts_logsImmediate_structural_unscoped` | logs_immediate unscoped + no entity → structural (no longer excluded) |
| `TestFindNoiseAlerts_broadQuery_wildcard` | Unscoped query with `*` → structural even if type was previously excluded |
| `TestFindNoiseAlerts_broadQuery_lowIDF` | Query with all low-IDF tokens → broad |
| `TestFindNoiseAlerts_specificQuery_notStructural` | Scoped alert with high-IDF query → not flagged |
| `TestComputeQueryIDFThreshold` | Returns 25th percentile correctly |
| `TestHasWildcardQuery` | `*`, `_exists_` detected; normal tokens not |
| `TestAvgIDF_emptyReturns1` | Empty token set → 1.0 |

### Phase 2 — `insights/enrich_test.go`

| Test | What it verifies |
|------|-----------------|
| `TestValidateNoiseCandidates_batchPrompt` | Single LLM call for N candidates |
| `TestValidateNoiseCandidates_cacheHit` | Cached result used, no LLM call |
| `TestValidateNoiseCandidates_llmError_returnsNil` | On error, nil returned (frontend falls back) |
