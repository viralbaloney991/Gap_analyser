# Group-By Pivot Similarity — Design Spec

**Date:** 2026-04-10
**Scope:** `backend/internal/similarity/` only — no changes to models, handlers, or frontend.

---

## Problem

Two alerts that fire on identical conditions but group by different fields (e.g. per-account vs. per-source-IP) score 1.0 similarity and are incorrectly flagged as duplicates. The `group_by_keys` field is already present on `AlertDef` but unused by the engine.

**Example:**
- `Okta - Multiple Login Failure For an Account` — groups by `okta.actor.alternateId` (user pivot)
- `Okta - Multiple Login Failure From a Source` — groups by `okta.client.ipAddress` (IP pivot)

These serve different threat models (targeted account attack vs. credential spray) and should not be merged.

---

## Design

### 1. New file: `pivot_categories.go`

Contains the normalization map and a single exported-internal function:

```go
// normalizeGroupByKeys maps a slice of group_by field paths to a set of
// semantic pivot categories. Unknown paths are used as their own category
// so that exact-path matches still count.
func normalizeGroupByKeys(keys []string) map[string]struct{}
```

**Category mapping:**

| Category | Field paths |
|---|---|
| `user` | `userIdentity.arn`, `userIdentity.principalId`, `userIdentity.sessionContext.sessionIssuer.userName`, `actor.email`, `actor.user.email`, `cloudflare.actor.email`, `okta.actor.alternateId`, `cx_security.email`, `cx_security.username`, `user.username`, `actor_id`, `actor`, `USER_NAME`, `requestParameters.userName`, `event.UserID`, `event.UserId`, `event.parameters.USER_EMAIL`, `userName`, `arn_extracted` |
| `ip` | `ClientIP`, `cx_security.source_ip`, `okta.client.ipAddress`, `remote_ip`, `msg.extension.PublicIPv4` |
| `hostname` | `event.Hostname`, `event.ComputerName` |
| `resource` | `requestParameters.bucketName`, `requestParameters.instanceId`, `requestParameters.domainName`, `requestParameters.roleArn`, `instance-id` |
| `account` | `userIdentity.accountId`, `coralogix.metadata.applicationName`, `awsRegion` |
| `detection` | `event.DetectId`, `event.CompositeId`, `event.AggregateId`, `sourceRule.name`, `detail.id` |

Unrecognised paths are used as their own category string (verbatim, lowercased).

---

### 2. Feature vector change (`engine.go`)

Add one field to `featureVector`:

```go
groupByCategories map[string]struct{}
```

`buildFeatureVectors` populates it:

```go
groupByCategories: normalizeGroupByKeys(a.GroupByKeys),
```

---

### 3. Scoring change (`engine.go`)

**Weight redistribution** — existing 5 dimensions scaled ×0.75, `groupByCategories` takes the freed 0.25:

| Dimension | Old weight | New weight |
|---|---|---|
| `dataSources` | 0.20 | 0.15 |
| `entities` | 0.15 | 0.11 |
| `actions` | 0.25 | 0.19 |
| `conditions` | 0.25 | 0.19 |
| `techniques` | 0.15 | 0.11 |
| `groupByCategories` | — | 0.25 |
| `alertType` bonus | +0.10 | +0.10 (unchanged) |
| **Max score** | **1.10** | **1.10** |

`scorePair` adds one line:

```go
score += weightGroupBy * jaccardGroupBy(a.groupByCategories, b.groupByCategories)
```

**Empty-set convention:** Two alerts with no `group_by_keys` both produce empty sets. Rather than penalising them (Jaccard returns 0 for empty∩empty), `jaccardGroupBy` returns 1.0 when both inputs are empty — unknown pivot is treated as compatible.

```go
func jaccardGroupBy(a, b map[string]struct{}) float64 {
    if len(a) == 0 && len(b) == 0 {
        return 1.0 // both unspecified — treat as compatible
    }
    return jaccard(a, b)
}
```

**Threshold update:**

| Threshold | Old | New |
|---|---|---|
| `duplicateThreshold` | 0.85 | 0.88 |
| `familyThreshold` | 0.60 | validated in implementation |
| `mergeAvgThreshold` | 0.70 | validated in implementation |

Rationale: with the new weights, the Okta pair scores exactly 0.85 (0.75 features + 0.00 groupBy + 0.10 alertType). Raising to 0.88 puts clean daylight between it and true duplicates (which score ≥ 1.00+).

`familyThreshold` and `mergeAvgThreshold` must be re-validated against `backend/debug_alerts.json` during implementation — a small downward adjustment may be needed.

---

### 4. Validation check

During implementation, run `similarity.Analyze()` against `debug_alerts.json` and assert:

- The Okta pair moves from `duplicates` → `families` (or lower)
- Total duplicate count does not change by more than ±10% (sanity bound)
- No previously-distinct alert pairs are newly flagged as duplicates

---

## Testing

### `pivot_categories_test.go`

- Known paths → correct category: `okta.actor.alternateId` → `user`, `ClientIP` → `ip`, `event.Hostname` → `hostname`, `requestParameters.bucketName` → `resource`
- Unknown path → raw path as category
- Empty input → empty set
- Multiple keys, mixed known/unknown → correct combined set

### `engine_test.go` additions

- Okta pair (same features, different pivot): score < 0.88
- Identical alerts, same pivot: score ≥ 0.88
- Identical alerts, no group_by on either: score ≥ 0.88 (empty+empty = 1.0 convention)
- Identical alerts, one has group_by and other does not: `jaccardGroupBy({category}, {}) = 0` → 0.25 weight penalty applied. This is intentional: an alert that pivots on user identity is meaningfully different from one that doesn't pivot at all.

---

## File Map

| File | Change |
|---|---|
| `backend/internal/similarity/pivot_categories.go` | New — normalization map + `normalizeGroupByKeys()` + `jaccardGroupBy()` |
| `backend/internal/similarity/engine.go` | Add `groupByCategories` to `featureVector`; update `buildFeatureVectors`; update weight constants; add `jaccardGroupBy` call in `scorePair`; update `duplicateThreshold` |
| `backend/internal/similarity/pivot_categories_test.go` | New — normalization unit tests |
| `backend/internal/similarity/engine_test.go` | Add score impact + regression tests |
