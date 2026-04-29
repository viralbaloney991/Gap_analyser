# Duplicate Detection Families Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge same-named detection families inside `groupFamilies()` so the API never returns duplicate family names.

**Architecture:** Add a merge pass after clustering and before the final sort in `groupFamilies()`. Same-named families are collapsed into one entry by concatenating their `AlertIDs` and `AlertNames` slices. The final sort (by size descending) runs on the merged result. No frontend changes.

**Tech Stack:** Go (backend only).

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/similarity/engine.go` | Add merge pass at end of `groupFamilies()` (lines 554–559) |
| `backend/internal/similarity/engine_test.go` | Add `TestGroupFamilies_mergesSameNamedFamilies` |

---

## Task 1: Merge same-named detection families

**Files:**
- Modify: `backend/internal/similarity/engine.go` lines 554–559
- Modify: `backend/internal/similarity/engine_test.go` (append at end of file)

Context: `groupFamilies()` ends at line 559 with a sort+return block. The merge pass replaces that block — it merges, then sorts the merged result. `models.DetectionFamily` has fields `Name string`, `AlertIDs []string`, `AlertNames []string`.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/similarity/engine_test.go`:

```go
func TestGroupFamilies_mergesSameNamedFamilies(t *testing.T) {
	// Build two pairs of alerts that will cluster together independently.
	// Both pairs share the same MITRE tactic so deriveFamilyName gives them
	// the same label. After the merge pass, only one family must be returned.
	//
	// Similarity matrix:
	//   A0↔A1 = 0.95  (cluster 1)
	//   A2↔A3 = 0.95  (cluster 2)
	//   across clusters ≤ 0.10
	//
	// Both clusters will be named "Privilege Escalation Detections" because
	// all four vectors share the privilege-escalation tactic.
	sharedTactic := []string{"privilege-escalation"}
	vecs := []featureVector{
		{alertID: "a0", alertName: "Alert A0", tactics: sharedTactic,
			dataSources: map[string]struct{}{"aws": {}}, entities: map[string]struct{}{"user": {}},
			actions: map[string]struct{}{"grant": {}}, conditions: map[string]struct{}{"escalation": {}},
			techniques: map[string]struct{}{"t1548": {}}},
		{alertID: "a1", alertName: "Alert A1", tactics: sharedTactic,
			dataSources: map[string]struct{}{"aws": {}}, entities: map[string]struct{}{"user": {}},
			actions: map[string]struct{}{"grant": {}}, conditions: map[string]struct{}{"escalation": {}},
			techniques: map[string]struct{}{"t1548": {}}},
		{alertID: "a2", alertName: "Alert A2", tactics: sharedTactic,
			dataSources: map[string]struct{}{"gcp": {}}, entities: map[string]struct{}{"role": {}},
			actions: map[string]struct{}{"sudo": {}}, conditions: map[string]struct{}{"privilege": {}},
			techniques: map[string]struct{}{"t1548": {}}},
		{alertID: "a3", alertName: "Alert A3", tactics: sharedTactic,
			dataSources: map[string]struct{}{"gcp": {}}, entities: map[string]struct{}{"role": {}},
			actions: map[string]struct{}{"sudo": {}}, conditions: map[string]struct{}{"privilege": {}},
			techniques: map[string]struct{}{"t1548": {}}},
	}

	// Build a similarity matrix where each pair clusters but cross-pair is low.
	n := len(vecs)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		matrix[i][i] = 1.0
	}
	matrix[0][1] = 0.95; matrix[1][0] = 0.95
	matrix[2][3] = 0.95; matrix[3][2] = 0.95
	// cross-cluster pairs stay 0.0 (default)

	families := groupFamilies(vecs, matrix, n)

	// Both clusters share the same name → must be merged into exactly one family.
	if len(families) != 1 {
		t.Fatalf("expected 1 merged family, got %d: %v", len(families), families)
	}
	f := families[0]
	if f.Name != "Privilege Escalation Detections" {
		t.Errorf("family name: want %q, got %q", "Privilege Escalation Detections", f.Name)
	}
	if len(f.AlertIDs) != 4 {
		t.Errorf("alert_ids: want 4, got %d: %v", len(f.AlertIDs), f.AlertIDs)
	}
	if len(f.AlertNames) != 4 {
		t.Errorf("alert_names: want 4, got %d: %v", len(f.AlertNames), f.AlertNames)
	}
}
```

- [ ] **Step 2: Run the test — confirm it fails**

```bash
cd backend && go test ./internal/similarity/... -run TestGroupFamilies_mergesSameNamedFamilies -v 2>&1
```

Expected: `FAIL` — `expected 1 merged family, got 2`

- [ ] **Step 3: Add the merge pass to `groupFamilies()`**

In `backend/internal/similarity/engine.go`, replace lines 554–559 (the sort+return block):

**Before:**
```go
	// Sort families by size descending for deterministic output.
	sort.Slice(families, func(i, j int) bool {
		return len(families[i].AlertIDs) > len(families[j].AlertIDs)
	})

	return families
```

**After:**
```go
	// Merge families that received the same name from deriveFamilyName.
	// Insertion order (first occurrence) determines position before the final sort.
	mergedMap := make(map[string]*models.DetectionFamily, len(families))
	mergedOrder := make([]string, 0, len(families))
	for i := range families {
		f := &families[i]
		if existing, ok := mergedMap[f.Name]; ok {
			existing.AlertIDs   = append(existing.AlertIDs,   f.AlertIDs...)
			existing.AlertNames = append(existing.AlertNames, f.AlertNames...)
		} else {
			mergedMap[f.Name] = f
			mergedOrder = append(mergedOrder, f.Name)
		}
	}
	merged := make([]models.DetectionFamily, 0, len(mergedOrder))
	for _, name := range mergedOrder {
		merged = append(merged, *mergedMap[name])
	}

	// Sort merged families by size descending for deterministic output.
	sort.Slice(merged, func(i, j int) bool {
		return len(merged[i].AlertIDs) > len(merged[j].AlertIDs)
	})

	return merged
```

- [ ] **Step 4: Run all similarity tests — all must pass**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | tail -20
```

Expected: all PASS, including the new `TestGroupFamilies_mergesSameNamedFamilies`.

- [ ] **Step 5: Full build check**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "fix(families): merge same-named detection families in groupFamilies"
```

---

## Self-Review

**Spec coverage:**
- ✅ Merge pass in `groupFamilies()` → Task 1 Step 3
- ✅ Insertion-order preserved via `mergedOrder` slice → Task 1 Step 3
- ✅ Final sort runs on merged result → Task 1 Step 3
- ✅ One test covering merge behaviour → Task 1 Step 1
- ✅ No frontend changes → nothing in plan

**Placeholder scan:** None found.

**Type consistency:** `models.DetectionFamily` fields (`Name`, `AlertIDs`, `AlertNames`) used consistently in test and implementation.
