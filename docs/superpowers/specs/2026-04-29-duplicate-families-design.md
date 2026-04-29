# Duplicate Detection Families Fix — Design

**Date:** 2026-04-29
**Status:** Approved

## Summary

PDF reports and the API response can contain multiple `DetectionFamily` entries with the same name when two or more alert clusters independently resolve to the same derived name (e.g. two separate "Execution Detections" clusters). Fix by merging same-named families inside `groupFamilies()` before returning, so the API always delivers a deduplicated list.

---

## Root Cause

`groupFamilies()` in `backend/internal/similarity/engine.go` runs `deriveFamilyName()` per cluster. `deriveFamilyName()` uses a three-tier strategy (MITRE tactic → action category → raw token), which can assign the same label to independent clusters. No merge pass follows, so `SimilarityResult.Families` is returned with duplicates intact.

The UI's `familyGroups` useMemo in `AlertInsights.tsx` groups them by name for display, but the PDF export's `familiesRows()` in `export.ts` maps `data.families` directly, so duplicates appear as separate rows in the report.

---

## Scope

**In scope:**
- Add a merge pass at the end of `groupFamilies()` that collapses same-named families by concatenating `AlertIDs` and `AlertNames`
- One new test in `engine_test.go` covering the merge behaviour

**Out of scope:**
- Frontend changes (no change to `AlertInsights.tsx`, `export.ts`, or API shape)
- Changing `deriveFamilyName()` to produce unique names
- Changing the `DetectionFamily` model

---

## Design

### Merge pass in `groupFamilies()`

After all `DetectionFamily` structs are built, add:

```go
// Merge same-named families produced by deriveFamilyName.
merged := make(map[string]*models.DetectionFamily)
order  := make([]string, 0, len(families))
for i := range families {
    f := &families[i]
    if existing, ok := merged[f.Name]; ok {
        existing.AlertIDs   = append(existing.AlertIDs,   f.AlertIDs...)
        existing.AlertNames = append(existing.AlertNames, f.AlertNames...)
    } else {
        merged[f.Name] = f
        order = append(order, f.Name)
    }
}
result := make([]models.DetectionFamily, 0, len(order))
for _, name := range order {
    result = append(result, *merged[name])
}
return result
```

`order` preserves insertion order (first occurrence wins for position), so the output is stable and deterministic.

### No frontend changes

`familiesRows()` in `export.ts` and `familyGroups` useMemo in `AlertInsights.tsx` require no modification. The API shape (`[]DetectionFamily`) is unchanged.

---

## Test

In `engine_test.go`:

| Test | What it verifies |
|------|-----------------|
| `TestGroupFamilies_mergesSameNamedFamilies` | Two clusters with the same derived name → one family with combined alert lists |

---

## Error Handling

No new error paths. The merge pass operates on an in-memory slice and cannot fail.
