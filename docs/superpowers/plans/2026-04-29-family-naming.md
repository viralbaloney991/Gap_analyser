# Detection Family Naming Improvement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate "Detection Family N" fallback names by expanding the Tier 2 action keyword map and adding two new naming tiers (nameTokens, dataSources) to `deriveFamilyName`.

**Architecture:** Two isolated changes to `backend/internal/similarity/engine.go`, each independently testable. Task 1 appends 8 entries to `actionCategories` (Part F). Task 2 adds a package-level stop-word map and inserts Tier 4 / Tier 5 logic inside `deriveFamilyName` immediately before the `"Detection Family N"` fallback return (Part G). No signature changes, no frontend changes.

**Tech Stack:** Go (backend only).

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/similarity/engine.go` | Append 8 entries to `actionCategories` (line 170); add `familyNameStopWords` var before `deriveFamilyName`; insert Tier 4 + Tier 5 inside `deriveFamilyName` (replace early `return fmt.Sprintf("Detection Family %d", fallbackNum)`) |
| `backend/internal/similarity/engine_test.go` | Append 5 new tests at end of file |

---

## Task 1: Expand Tier 2 action keyword categories (Part F)

**Files:**
- Modify: `backend/internal/similarity/engine.go` (lines 163–171 — `actionCategories` slice)
- Modify: `backend/internal/similarity/engine_test.go` (append at end)

Context: `actionCategories` is a package-level slice of `{keywords []string, category string}` at line 159. Tier 2 in `deriveFamilyName` (line 606) iterates this slice in order; first match wins. New entries go after the existing 8 so existing behaviour is unchanged.

- [ ] **Step 1: Write failing tests**

Append to `backend/internal/similarity/engine_test.go`:

```go
// ── Tier 2 expanded keyword categories ───────────────────────────────────────

func TestDeriveFamilyName_tier2_expanded_network(t *testing.T) {
	vecs := []featureVector{
		{alertID: "n1", alertName: "Net Alert", actions: map[string]struct{}{"connect": {}}},
		{alertID: "n2", alertName: "Net Alert 2", actions: map[string]struct{}{"connect": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Network Detections" {
		t.Errorf("want %q, got %q", "Network Detections", got)
	}
}

func TestDeriveFamilyName_tier2_expanded_credential(t *testing.T) {
	vecs := []featureVector{
		{alertID: "c1", alertName: "Cred Alert", actions: map[string]struct{}{"token": {}}},
		{alertID: "c2", alertName: "Cred Alert 2", actions: map[string]struct{}{"token": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Credential Detections" {
		t.Errorf("want %q, got %q", "Credential Detections", got)
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd backend && go test ./internal/similarity/... -run "TestDeriveFamilyName_tier2_expanded" -v 2>&1
```

Expected: both FAIL — `want "Network Detections", got "Detection Family 1"` (and similar for Credential).

- [ ] **Step 3: Add 8 new entries to `actionCategories`**

In `backend/internal/similarity/engine.go`, replace:

```go
	{[]string{"encrypt", "ransom", "destroy"}, "Impact"},
}
```

With:

```go
	{[]string{"encrypt", "ransom", "destroy"}, "Impact"},
	{[]string{"connect", "network", "traffic", "firewall"}, "Network"},
	{[]string{"access", "read", "fetch", "view", "list"}, "Access"},
	{[]string{"modif", "update", "change", "configur", "patch"}, "Configuration Change"},
	{[]string{"api", "call", "request", "invoke"}, "API Activity"},
	{[]string{"credential", "token", "key", "secret", "password", "certif"}, "Credential"},
	{[]string{"deploy", "launch", "provision", "spawn"}, "Deployment"},
	{[]string{"backup", "restore", "export", "import", "query"}, "Data Operations"},
	{[]string{"anomal", "spike", "surge", "threshold", "volume"}, "Anomaly"},
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```bash
cd backend && go test ./internal/similarity/... -run "TestDeriveFamilyName_tier2_expanded" -v 2>&1
```

Expected: both PASS.

- [ ] **Step 5: Run all similarity tests — no regressions**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(families): expand Tier 2 action keyword categories"
```

---

## Task 2: Add Tier 4 (nameTokens) and Tier 5 (dataSources) (Part G)

**Files:**
- Modify: `backend/internal/similarity/engine.go` — add `familyNameStopWords` var before `deriveFamilyName`; replace the early `return fmt.Sprintf("Detection Family %d", fallbackNum)` in `deriveFamilyName` with Tier 4 + Tier 5 logic
- Modify: `backend/internal/similarity/engine_test.go` (append at end)

Context: `deriveFamilyName` is at line 585. At line 634 there is a guard `if len(freq) == 0 { return fmt.Sprintf(...) }` — that is the only change point. After this change the `"Detection Family N"` return moves inside the new cascade. `nameTokens` and `dataSources` are already fields on `featureVector`.

- [ ] **Step 1: Write failing tests**

Append to `backend/internal/similarity/engine_test.go`:

```go
// ── Tier 4 + Tier 5 family naming cascade ────────────────────────────────────

func TestDeriveFamilyName_tier4_nameTokens(t *testing.T) {
	// No tactics, no techniques, no actions → falls to Tier 4.
	// "cloudtrail" appears in both members — most frequent non-stop token.
	vecs := []featureVector{
		{
			alertID:    "ct1",
			alertName:  "Cloudtrail Unusual API Call",
			nameTokens: map[string]struct{}{"cloudtrail": {}, "unusual": {}, "api": {}, "call": {}},
		},
		{
			alertID:    "ct2",
			alertName:  "Cloudtrail Config Change",
			nameTokens: map[string]struct{}{"cloudtrail": {}, "config": {}, "change": {}},
		},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Cloudtrail Detections" {
		t.Errorf("want %q, got %q", "Cloudtrail Detections", got)
	}
}

func TestDeriveFamilyName_tier4_stopWordFiltered(t *testing.T) {
	// nameTokens are all stop-words → Tier 4 skips them → falls to Tier 5.
	vecs := []featureVector{
		{
			alertID:     "sw1",
			alertName:   "Alert Event Log",
			nameTokens:  map[string]struct{}{"alert": {}, "event": {}, "log": {}},
			dataSources: map[string]struct{}{"okta": {}},
		},
		{
			alertID:     "sw2",
			alertName:   "Alert Monitor Log",
			nameTokens:  map[string]struct{}{"alert": {}, "monitor": {}, "log": {}},
			dataSources: map[string]struct{}{"okta": {}},
		},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Okta Detections" {
		t.Errorf("want %q, got %q", "Okta Detections", got)
	}
}

func TestDeriveFamilyName_tier5_dataSource(t *testing.T) {
	// No tactics, no techniques, no actions, no nameTokens → Tier 5: dataSources.
	vecs := []featureVector{
		{alertID: "ds1", alertName: "Alert", dataSources: map[string]struct{}{"okta": {}}},
		{alertID: "ds2", alertName: "Alert", dataSources: map[string]struct{}{"okta": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Okta Detections" {
		t.Errorf("want %q, got %q", "Okta Detections", got)
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd backend && go test ./internal/similarity/... -run "TestDeriveFamilyName_tier4|TestDeriveFamilyName_tier5" -v 2>&1
```

Expected: all three FAIL — `want "Cloudtrail Detections", got "Detection Family 1"` etc.

- [ ] **Step 3: Add `familyNameStopWords` package-level var**

In `backend/internal/similarity/engine.go`, add immediately before the line `// deriveFamilyName builds a human-readable family name`:

```go
// familyNameStopWords are excluded when building a Tier 4 name from nameTokens
// because they appear in almost every alert and carry no cluster-specific meaning.
var familyNameStopWords = map[string]struct{}{
	"alert": {}, "alerts": {}, "detection": {}, "detections": {},
	"rule": {}, "rules": {}, "monitor": {}, "monitoring": {},
	"log": {}, "logs": {}, "event": {}, "events": {},
}

```

- [ ] **Step 4: Replace the early `"Detection Family N"` return in `deriveFamilyName` with the Tier 4 + Tier 5 cascade**

In `backend/internal/similarity/engine.go`, replace:

```go
	if len(freq) == 0 {
		return fmt.Sprintf("Detection Family %d", fallbackNum)
	}
	bestToken := ""
	bestCount := 0
	for tok, count := range freq {
		if count > bestCount || (count == bestCount && tok < bestToken) {
			bestToken = tok
			bestCount = count
		}
	}
	return toTitle(bestToken) + " Detections"
}
```

With:

```go
	if len(freq) == 0 {
		// Tier 4: most frequent nameToken, excluding generic security terms.
		nameFreq := make(map[string]int)
		for _, idx := range members {
			for tok := range vectors[idx].nameTokens {
				if _, stop := familyNameStopWords[tok]; !stop {
					nameFreq[tok]++
				}
			}
		}
		if len(nameFreq) > 0 {
			bestTok, bestCnt := "", 0
			for tok, cnt := range nameFreq {
				if cnt > bestCnt || (cnt == bestCnt && tok < bestTok) {
					bestTok = tok
					bestCnt = cnt
				}
			}
			return toTitle(bestTok) + " Detections"
		}

		// Tier 5: most frequent data source.
		dsFreq := make(map[string]int)
		for _, idx := range members {
			for src := range vectors[idx].dataSources {
				dsFreq[src]++
			}
		}
		if len(dsFreq) > 0 {
			bestSrc, bestCnt := "", 0
			for src, cnt := range dsFreq {
				if cnt > bestCnt || (cnt == bestCnt && src < bestSrc) {
					bestSrc = src
					bestCnt = cnt
				}
			}
			return toTitle(bestSrc) + " Detections"
		}

		return fmt.Sprintf("Detection Family %d", fallbackNum)
	}
	bestToken := ""
	bestCount := 0
	for tok, count := range freq {
		if count > bestCount || (count == bestCount && tok < bestToken) {
			bestToken = tok
			bestCount = count
		}
	}
	return toTitle(bestToken) + " Detections"
}
```

- [ ] **Step 5: Run new tests — all must pass**

```bash
cd backend && go test ./internal/similarity/... -run "TestDeriveFamilyName_tier4|TestDeriveFamilyName_tier5" -v 2>&1
```

Expected: all three PASS.

- [ ] **Step 6: Run full similarity test suite — no regressions**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | tail -25
```

Expected: all PASS (currently 55+ tests).

- [ ] **Step 7: Full build check**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(families): add Tier 4 nameTokens and Tier 5 dataSources naming fallbacks"
```

---

## Self-Review

**Spec coverage:**
- ✅ Expand `actionCategories` with 8 new entries → Task 1 Step 3
- ✅ Network / Access / Configuration Change / API Activity / Credential / Deployment / Data Operations / Anomaly categories → Task 1 Step 3
- ✅ `familyNameStopWords` package-level var → Task 2 Step 3
- ✅ Tier 4: nameTokens frequency, stop-word filtered → Task 2 Step 4
- ✅ Tier 5: most frequent dataSource → Task 2 Step 4
- ✅ Tie-break alphabetically in both Tier 4 and Tier 5 → `tok < bestTok` / `src < bestSrc` in Task 2 Step 4
- ✅ `"Detection Family N"` remains as true last resort → Task 2 Step 4
- ✅ Tests: tier2_expanded_network, tier2_expanded_credential → Task 1 Step 1
- ✅ Tests: tier4_nameTokens, tier4_stopWordFiltered, tier5_dataSource → Task 2 Step 1
- ✅ No frontend changes → nothing in plan

**Placeholder scan:** None found.

**Type consistency:** `familyNameStopWords map[string]struct{}` defined in Task 2 Step 3 and referenced in Task 2 Step 4. `featureVector.nameTokens map[string]struct{}` and `featureVector.dataSources map[string]struct{}` used consistently. `toTitle()` called identically in Tier 4 and Tier 5 as in existing tiers.
