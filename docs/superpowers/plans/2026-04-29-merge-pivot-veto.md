# Merge Suggestion Pivot-Conflict Veto — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent merge suggestions where any two alerts in the group track fundamentally different pivot dimensions (e.g. one groups by IP, another by user).

**Architecture:** Add `hasPivotConflict(vectors []featureVector, members []int) bool` to `engine.go` and call it inside `buildMergeSuggestions()` after the average-similarity check. If any two alerts in the candidate group have non-empty, disjoint `groupByCategories` sets, the group is dropped. No frontend changes.

**Tech Stack:** Go (backend only).

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/similarity/engine.go` | Add `hasPivotConflict` helper; add guard call in `buildMergeSuggestions` |
| `backend/internal/similarity/engine_test.go` | Add two tests for the veto |

---

## Task 1: Pivot-conflict veto in `buildMergeSuggestions`

**Files:**
- Modify: `backend/internal/similarity/engine.go` (after line 857, and inside `buildMergeSuggestions` at line 834)
- Modify: `backend/internal/similarity/engine_test.go` (append at end)

Context:
- `featureVector.groupByCategories` is `map[string]struct{}` — normalised pivot category tokens (e.g. `"user"`, `"ip"`, `"hostname"`, `"resource"`, `"account"`, `"detection"`)
- `buildMergeSuggestions` is at lines 784–857 of `engine.go`. The `mergeAvgThreshold` check is at line 832–834. The guard goes immediately after it, before building `ids`/`names`.
- There are no existing tests for `buildMergeSuggestions` — the new tests are the first.

- [ ] **Step 1: Write failing tests**

Append to `backend/internal/similarity/engine_test.go`:

```go
// ── Merge suggestion pivot-conflict veto ──────────────────────────────────

func makeMergeVector(name, id string, pivots []string) featureVector {
	cats := make(map[string]struct{}, len(pivots))
	for _, p := range pivots {
		cats[p] = struct{}{}
	}
	return featureVector{
		alertID:           id,
		alertName:         name,
		groupByCategories: cats,
		dataSources:       map[string]struct{}{"aws": {}},
		entities:          map[string]struct{}{"user": {}},
		actions:           map[string]struct{}{"login": {}},
		conditions:        map[string]struct{}{"failed": {}},
		techniques:        map[string]struct{}{"t1078": {}},
		luceneQuery:       map[string]struct{}{"eventtype": {}, "failed": {}, "login": {}},
		nameTokens:        map[string]struct{}{"failed": {}, "login": {}},
		alertType:         "logs_threshold",
		timeWindow:        "5m",
	}
}

func TestBuildMergeSuggestions_pivotConflict_vetoed(t *testing.T) {
	// Three highly-similar alerts: two group by "user", one by "ip".
	// The "user" pair and "ip" alert are disjoint → whole group vetoed.
	vecs := []featureVector{
		makeMergeVector("Login Failure A", "lf-1", []string{"user"}),
		makeMergeVector("Login Failure B", "lf-2", []string{"user"}),
		makeMergeVector("Login Failure IP", "lf-3", []string{"ip"}),
	}
	n := len(vecs)

	// All pairs score above mergeAvgThreshold (0.70) — force via matrix.
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		for j := range matrix[i] {
			if i == j {
				matrix[i][j] = 1.0
			} else {
				matrix[i][j] = 0.95 // well above both thresholds
			}
		}
	}

	suggestions := buildMergeSuggestions(vecs, matrix, n)
	if len(suggestions) != 0 {
		t.Errorf("expected 0 suggestions (pivot conflict vetoed), got %d: %v", len(suggestions), suggestions)
	}
}

func TestBuildMergeSuggestions_pivotOverlap_notVetoed(t *testing.T) {
	// Three alerts: two group by "user", one by "user" and "ip".
	// All share "user" → no conflict → suggestion allowed.
	vecs := []featureVector{
		makeMergeVector("Login Failure A", "lf-4", []string{"user"}),
		makeMergeVector("Login Failure B", "lf-5", []string{"user"}),
		makeMergeVector("Login Failure C", "lf-6", []string{"user", "ip"}),
	}
	n := len(vecs)

	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		for j := range matrix[i] {
			if i == j {
				matrix[i][j] = 1.0
			} else {
				matrix[i][j] = 0.95
			}
		}
	}

	suggestions := buildMergeSuggestions(vecs, matrix, n)
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion (shared pivot), got %d", len(suggestions))
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd backend && go test ./internal/similarity/... -run "TestBuildMergeSuggestions_pivot" -v 2>&1
```

Expected: both FAIL — `undefined: hasPivotConflict` or assertion failure (`expected 0 suggestions, got 1`).

- [ ] **Step 3: Add `hasPivotConflict` to `engine.go`**

Add this function after the closing brace of `buildMergeSuggestions` (after line 857):

```go
// hasPivotConflict returns true if any two alerts in the group have non-empty,
// disjoint groupByCategories sets — indicating they track fundamentally different
// pivot dimensions and should not be merged.
// Alerts with empty groupByCategories are skipped (ambiguous scope — no veto).
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

- [ ] **Step 4: Add the guard in `buildMergeSuggestions`**

In `buildMergeSuggestions`, after the `mergeAvgThreshold` check (lines 832–834) and before the `ids := make(...)` line, add:

**Before (lines 832–836):**
```go
		avgSim := averagePairwiseSimilarity(matrix, members)
		if avgSim < mergeAvgThreshold {
			continue
		}

		ids := make([]string, len(members))
```

**After:**
```go
		avgSim := averagePairwiseSimilarity(matrix, members)
		if avgSim < mergeAvgThreshold {
			continue
		}
		if hasPivotConflict(vectors, members) {
			continue
		}

		ids := make([]string, len(members))
```

- [ ] **Step 5: Run tests — all must pass**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | tail -20
```

Expected: all PASS, including both new pivot tests.

- [ ] **Step 6: Full build check**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "fix(merge): veto suggestions where alerts track different pivot dimensions"
```

---

## Self-Review

**Spec coverage:**
- ✅ `hasPivotConflict` helper → Task 1 Step 3
- ✅ Guard call in `buildMergeSuggestions` after `mergeAvgThreshold` check → Task 1 Step 4
- ✅ Veto when pivots are disjoint and both non-empty → logic in `hasPivotConflict`
- ✅ No veto when either alert has empty group-by → `len(a) == 0` / `len(b) == 0` skips
- ✅ No veto when pivots overlap → `shared = true` path
- ✅ Test: conflict vetoed → `TestBuildMergeSuggestions_pivotConflict_vetoed`
- ✅ Test: overlap allowed → `TestBuildMergeSuggestions_pivotOverlap_notVetoed`
- ✅ No frontend changes → nothing in plan

**Placeholder scan:** None found.

**Type consistency:** `groupByCategories map[string]struct{}` used consistently in helper signature, test helper `makeMergeVector`, and guard call.
