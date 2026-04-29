# Merge Suggestion Pivot-Conflict Veto — Design

**Date:** 2026-04-29
**Status:** Approved

## Summary

Merge suggestions can flag pairs of alerts that are fundamentally different because they track different entity pivots (e.g. one groups by distinct IPs, another by user). The fix adds a hard veto inside `buildMergeSuggestions()`: if any two alerts in a candidate merge group have non-empty, disjoint `groupByCategories` sets, the group is silently dropped and never appears in the Merge tab.

---

## Root Cause

`buildMergeSuggestions()` evaluates average pairwise similarity across all dimensions. The `groupBy` (pivot) dimension carries only 5% weight — intentionally reduced after the Okta pair false-positive fix. This means two alerts that pivot on completely different entity types (user vs IP) can still exceed the 70% merge threshold if their query, data sources, and techniques are similar. The result is a merge suggestion that would be semantically wrong to act on.

---

## Scope

**In scope:**
- New `hasPivotConflict(vectors []featureVector, members []int) bool` helper in `engine.go`
- One guard call in `buildMergeSuggestions()` after the `mergeAvgThreshold` check
- Two tests in `engine_test.go`

**Out of scope:**
- Frontend changes
- Changes to scoring weights
- Changing the veto logic for pairs with empty group-by (ambiguous — not vetoed)

---

## Design

### Helper: `hasPivotConflict`

```go
// hasPivotConflict returns true if any two alerts in the group have non-empty,
// disjoint groupByCategories sets — indicating they track fundamentally different
// pivot dimensions and should not be merged.
// Alerts with empty groupByCategories are skipped (ambiguous scope).
func hasPivotConflict(vectors []featureVector, members []int) bool {
    for i := 0; i < len(members); i++ {
        a := vectors[members[i]].groupByCategories
        if len(a) == 0 {
            continue
        }
        for j := i + 1; j < len(members); j++ {
            b := vectors[members[j]].groupByCategories
            if len(b) == 0 {
                continue
            }
            // Check intersection.
            shared := false
            for cat := range a {
                if _, ok := b[cat]; ok {
                    shared = true
                    break
                }
            }
            if !shared {
                return true
            }
        }
    }
    return false
}
```

### Guard in `buildMergeSuggestions`

After the `mergeAvgThreshold` check (line 834 of `engine.go`) and before building `ids`/`names`, add:

```go
if hasPivotConflict(vectors, members) {
    continue
}
```

### Veto rules

| Scenario | Vetoed? |
|----------|---------|
| Alert A: `{user}`, Alert B: `{ip}` | ✅ Yes — disjoint |
| Alert A: `{user}`, Alert B: `{user, ip}` | ❌ No — share `user` |
| Alert A: `{user}`, Alert B: `{}` (no group-by) | ❌ No — B is ambiguous |
| Alert A: `{}`, Alert B: `{}` | ❌ No — both ambiguous |
| Alert A: `{hostname}`, Alert B: `{hostname}` | ❌ No — same pivot |
| Alert A: `{user}`, Alert B: `{ip}`, Alert C: `{user}` | ✅ Yes — A and B are disjoint |

---

## Tests

| Test | What it verifies |
|------|-----------------|
| `TestBuildMergeSuggestions_pivotConflict_vetoed` | Group where one alert pivots on `user`, another on `ip` → not in suggestions |
| `TestBuildMergeSuggestions_pivotOverlap_notVetoed` | Group where alerts share a pivot category → still in suggestions |

---

## Error Handling

No new error paths. `hasPivotConflict` operates on in-memory sets and cannot fail.
