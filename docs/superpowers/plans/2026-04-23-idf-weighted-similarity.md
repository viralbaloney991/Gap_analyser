# IDF-Weighted Similarity — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix false-positive similarity scores (e.g. DNS A vs AAAA records) by preserving `field:value` Lucene tokens as atomic units and weighting all Jaccard comparisons by IDF so rare, discriminating tokens dominate.

**Architecture:** Two changes to `engine.go` only. (1) `tokenizeLucene` extracts `field:value` pairs as atomic tokens before splitting on Lucene operators — `record_type:"A"` becomes the atom `record_type:a` instead of being split into `record_type` + dropped `a`. (2) A new `idfTable` (computed once per `Analyze` call) weights each token by `log(1 + N/df(t))`; `weightedJaccard` replaces `jaccard` in `scorePair` for all set-based dimensions.

**Tech Stack:** Go — `regexp`, `math`, `strings` (all already imported in `engine.go`)

---

## File map

| File | Change |
|---|---|
| `backend/internal/similarity/engine.go` | Rewrite `tokenizeLucene`; add `idfTable` struct, `buildIDF`, `weightedJaccard`, `idfWeight`; update `scorePair` signature; update `computePairwiseScores`; update `Analyze` |
| `backend/internal/similarity/engine_test.go` | Update `TestTokenizeLucene_basic`; update all `scorePair(a, b)` → `scorePair(a, b, idfTable{})`; add `TestTokenizeLucene_fieldValuePreserved`, `TestTokenizeLucene_singleCharPreserved`, `TestBuildIDF_rareTokenHigherWeight`, `TestWeightedJaccard_rareTokenDominates`, `TestWeightedJaccard_identicalSetsScoreOne`, `TestScorePair_dnsAvsAAAA_notDuplicate` |

No other files change.

---

### Task 1: Fix tokenizeLucene — field:value atomic tokens

**Files:**
- Modify: `backend/internal/similarity/engine.go:182-199`
- Modify: `backend/internal/similarity/engine_test.go`

The current tokenizer splits on `:`, destroying `field:value` structure. `record_type:"A"` becomes `{record_type}` (the `a` is dropped as single-char). This task rewrites `tokenizeLucene` to extract `field:value` pairs as single atoms first, then tokenise the remainder.

- [ ] **Step 1: Add two new failing tests for the new tokenizer behaviour**

Add these two tests to `backend/internal/similarity/engine_test.go` immediately after `TestTokenizeLucene_basic`:

```go
func TestTokenizeLucene_fieldValuePreserved(t *testing.T) {
	// field:value pairs must be kept as one atomic token.
	tokens := tokenizeLucene(`record_type:"AAAA" AND source:"dns"`)
	if _, ok := tokens["record_type:aaaa"]; !ok {
		t.Errorf("expected atomic token \"record_type:aaaa\", got tokens: %v", tokens)
	}
	if _, ok := tokens["record_type"]; ok {
		t.Errorf("\"record_type\" should not appear as a bare token: %v", tokens)
	}
	if _, ok := tokens["aaaa"]; ok {
		t.Errorf("\"aaaa\" should not appear as a bare token: %v", tokens)
	}
}

func TestTokenizeLucene_singleCharPreserved(t *testing.T) {
	// Single-char values inside field:value atoms must NOT be dropped.
	tokens := tokenizeLucene(`record_type:"A"`)
	if _, ok := tokens["record_type:a"]; !ok {
		t.Errorf("expected atom \"record_type:a\" for single-char value, got tokens: %v", tokens)
	}
}
```

- [ ] **Step 2: Run the two new tests — verify they FAIL**

```bash
cd backend && go test ./internal/similarity/... -run "TestTokenizeLucene_fieldValuePreserved|TestTokenizeLucene_singleCharPreserved" -v 2>&1 | tail -20
```

Expected: both tests FAIL (token not found).

- [ ] **Step 3: Rewrite tokenizeLucene**

Replace the entire `tokenizeLucene` function in `backend/internal/similarity/engine.go` (lines 179–199):

```go
// tokenizeLucene splits a Lucene query string into a lowercase token set.
//
// Two-pass approach:
//  1. Extract field:value pairs as atomic tokens (e.g. record_type:"AAAA" → record_type:aaaa).
//     This preserves discriminating field values that would otherwise be split or dropped.
//  2. Tokenise the remainder (query with atoms stripped) on Lucene operators and whitespace.
//     Single-character standalone tokens are dropped as noise.
func tokenizeLucene(q string) map[string]struct{} {
	if q == "" {
		return nil
	}
	lower := strings.ToLower(q)
	s := make(map[string]struct{})

	// Pass 1: extract field:value atoms.
	// Matches word chars (including dots) followed by : and a non-whitespace/non-operator value.
	atomRe := regexp.MustCompile(`[\w.]+:[^\s()\[\]{}+\-!"]+`)
	normRe := regexp.MustCompile(`["\s]+`)
	for _, atom := range atomRe.FindAllString(lower, -1) {
		norm := normRe.ReplaceAllString(atom, "")
		if len(norm) > 2 { // skip trivially short atoms like "a:b"
			s[norm] = struct{}{}
		}
	}

	// Pass 2: strip atoms from query, tokenise remainder.
	remainder := atomRe.ReplaceAllString(lower, " ")
	splitRe := regexp.MustCompile(`[:()\[\]{}\s+\-!"]+`)
	for _, t := range splitRe.Split(remainder, -1) {
		t = strings.TrimSpace(t)
		if len(t) > 1 {
			s[t] = struct{}{}
		}
	}

	if len(s) == 0 {
		return nil
	}
	return s
}
```

- [ ] **Step 4: Update TestTokenizeLucene_basic — its expected tokens change**

The old tokenizer split `eventType:GuestUserAnomalyEvent` into `{eventtype, guestuserwanomalyevent}`. The new tokenizer produces the atom `{eventtype:guestuserwanomalyevent}`. Update the test:

```go
func TestTokenizeLucene_basic(t *testing.T) {
	tokens := tokenizeLucene("eventType:GuestUserAnomalyEvent AND coralogix.metadata.applicationName:salesforce")
	want := map[string]struct{}{
		"eventtype:guestuserwanomalyevent":              {},
		"and":                                           {},
		"coralogix.metadata.applicationname:salesforce": {},
	}
	if len(tokens) != len(want) {
		t.Errorf("tokenizeLucene returned %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	for k := range want {
		if _, ok := tokens[k]; !ok {
			t.Errorf("expected token %q not found in %v", k, tokens)
		}
	}
}
```

- [ ] **Step 5: Run all similarity tests — verify they pass**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | tail -30
```

Expected: all tests pass. `TestScorePair_salesforcePairIsNotDuplicate` still passes because the Salesforce pair still scores below 0.85 — the new atoms `eventtype:guestuserwanomalyevent` and `eventtype:apianomalevent` produce zero intersection on the Lucene dimension, lowering the score further.

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/similarity/engine.go internal/similarity/engine_test.go
git commit -m "fix(similarity): tokenizeLucene preserves field:value pairs as atomic tokens"
```

---

### Task 2: Add idfTable struct and buildIDF function

**Files:**
- Modify: `backend/internal/similarity/engine.go` — add after the `pairScore` struct (line ~36)
- Modify: `backend/internal/similarity/engine_test.go`

This task adds the IDF data structure and the one-pass corpus-wide IDF computation. No existing function signatures change yet.

- [ ] **Step 1: Write the failing test for buildIDF**

Add to `backend/internal/similarity/engine_test.go`:

```go
func TestBuildIDF_rareTokenHigherWeight(t *testing.T) {
	// Corpus: 10 vectors. "common" appears in all 10; "rare" appears in only 1.
	// IDF("rare") must be strictly greater than IDF("common").
	vectors := make([]featureVector, 10)
	for i := range vectors {
		vectors[i] = featureVector{
			actions: map[string]struct{}{"common": {}},
		}
	}
	vectors[0].actions["rare"] = struct{}{}

	tbl := buildIDF(vectors)

	commonW, ok1 := tbl.actions["common"]
	rareW, ok2 := tbl.actions["rare"]
	if !ok1 {
		t.Fatal("expected IDF weight for \"common\" token")
	}
	if !ok2 {
		t.Fatal("expected IDF weight for \"rare\" token")
	}
	if rareW <= commonW {
		t.Errorf("rare token IDF (%.4f) should be > common token IDF (%.4f)", rareW, commonW)
	}
	// Sanity: IDF("common") = log(1 + 10/10) = log(2) ≈ 0.693
	want := math.Log(2.0)
	if math.Abs(commonW-want) > 1e-9 {
		t.Errorf("IDF(common) = %.6f, want %.6f", commonW, want)
	}
}
```

- [ ] **Step 2: Run the test — verify it FAILS**

```bash
cd backend && go test ./internal/similarity/... -run "TestBuildIDF_rareTokenHigherWeight" -v 2>&1 | tail -10
```

Expected: FAIL — `buildIDF undefined`.

- [ ] **Step 3: Add idfTable struct and buildIDF to engine.go**

Add the following block immediately after the `pairScore` struct (after line 36, before the `const` block):

```go
// idfTable holds per-dimension inverse-document-frequency weights for the corpus.
// idf(t) = log(1 + N/df(t)) where N = number of alerts and df(t) = number of
// alerts that contain token t in the given dimension.
type idfTable struct {
	dataSources map[string]float64
	entities    map[string]float64
	actions     map[string]float64
	conditions  map[string]float64
	techniques  map[string]float64
	groupBy     map[string]float64
	luceneQuery map[string]float64
}

// buildIDF computes an idfTable from the full set of feature vectors.
// Each dimension is scored independently. Runs in O(n × avg_tokens_per_alert).
func buildIDF(vectors []featureVector) idfTable {
	n := float64(len(vectors))
	if n == 0 {
		return idfTable{}
	}

	tbl := idfTable{
		dataSources: make(map[string]float64),
		entities:    make(map[string]float64),
		actions:     make(map[string]float64),
		conditions:  make(map[string]float64),
		techniques:  make(map[string]float64),
		groupBy:     make(map[string]float64),
		luceneQuery: make(map[string]float64),
	}

	type dimDef struct {
		get func(featureVector) map[string]struct{}
		out map[string]float64
	}
	dims := []dimDef{
		{func(v featureVector) map[string]struct{} { return v.dataSources }, tbl.dataSources},
		{func(v featureVector) map[string]struct{} { return v.entities }, tbl.entities},
		{func(v featureVector) map[string]struct{} { return v.actions }, tbl.actions},
		{func(v featureVector) map[string]struct{} { return v.conditions }, tbl.conditions},
		{func(v featureVector) map[string]struct{} { return v.techniques }, tbl.techniques},
		{func(v featureVector) map[string]struct{} { return v.groupByCategories }, tbl.groupBy},
		{func(v featureVector) map[string]struct{} { return v.luceneQuery }, tbl.luceneQuery},
	}

	for _, dim := range dims {
		df := make(map[string]int)
		for _, v := range vectors {
			for t := range dim.get(v) {
				df[t]++
			}
		}
		for t, count := range df {
			dim.out[t] = math.Log(1 + n/float64(count))
		}
	}

	return tbl
}
```

- [ ] **Step 4: Run the test — verify it passes**

```bash
cd backend && go test ./internal/similarity/... -run "TestBuildIDF_rareTokenHigherWeight" -v 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 5: Run all similarity tests — verify nothing broken**

```bash
cd backend && go test ./internal/similarity/... 2>&1 | tail -5
```

Expected: `ok  coralogix-alert-analyzer/internal/similarity`

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/similarity/engine.go internal/similarity/engine_test.go
git commit -m "feat(similarity): add idfTable struct and buildIDF corpus-wide IDF computation"
```

---

### Task 3: Add weightedJaccard and idfWeight

**Files:**
- Modify: `backend/internal/similarity/engine.go` — add after the `jaccard` function (~line 347)
- Modify: `backend/internal/similarity/engine_test.go`

This task adds the two helper functions that implement IDF-weighted Jaccard. `scorePair` is not changed yet — that happens in Task 4.

- [ ] **Step 1: Write failing tests for weightedJaccard**

Add to `backend/internal/similarity/engine_test.go`:

```go
func TestWeightedJaccard_identicalSetsScoreOne(t *testing.T) {
	// Identical sets always score 1.0 regardless of IDF weights.
	idf := map[string]float64{"a": 0.1, "b": 5.0, "c": 2.3}
	s := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	score := weightedJaccard(s, s, idf)
	if math.Abs(score-1.0) > 1e-9 {
		t.Errorf("identical sets: got %.6f, want 1.0", score)
	}
}

func TestWeightedJaccard_rareTokenDominates(t *testing.T) {
	// Set A = {common, rare_a}
	// Set B = {common, rare_b}
	// With high IDF on rare tokens, the intersection (just "common") has low
	// relative weight — weighted Jaccard should be much lower than flat Jaccard.
	idf := map[string]float64{
		"common": 0.1,   // appears in almost every document
		"rare_a": 10.0,  // appears in 1 document
		"rare_b": 10.0,  // appears in 1 document
	}
	a := map[string]struct{}{"common": {}, "rare_a": {}}
	b := map[string]struct{}{"common": {}, "rare_b": {}}

	weighted := weightedJaccard(a, b, idf)
	flat := jaccard(a, b) // flat = 1/3 ≈ 0.333

	if weighted >= flat {
		t.Errorf("weighted Jaccard (%.4f) should be < flat Jaccard (%.4f) when rare tokens differ", weighted, flat)
	}
	// Manual: intersection weight = 0.1; union weight = 0.1 + 10.0 + 10.0 = 20.1
	// weighted = 0.1 / 20.1 ≈ 0.00498
	want := 0.1 / 20.1
	if math.Abs(weighted-want) > 1e-6 {
		t.Errorf("weighted Jaccard = %.6f, want %.6f", weighted, want)
	}
}

func TestWeightedJaccard_bothEmptySetsReturnZero(t *testing.T) {
	score := weightedJaccard(nil, nil, nil)
	if score != 0.0 {
		t.Errorf("both empty: got %.6f, want 0.0", score)
	}
}
```

- [ ] **Step 2: Run the tests — verify they FAIL**

```bash
cd backend && go test ./internal/similarity/... -run "TestWeightedJaccard" -v 2>&1 | tail -15
```

Expected: all three FAIL — `weightedJaccard undefined`.

- [ ] **Step 3: Add weightedJaccard and idfWeight to engine.go**

Add the following block immediately after the `jaccard` function (after its closing `}`):

```go
// weightedJaccard computes an IDF-weighted Jaccard (Tanimoto) coefficient.
// Each token's contribution to intersection and union is scaled by its IDF weight,
// so rare tokens matter more than common ones.
// Returns 0 when both sets are empty (consistent with jaccard).
func weightedJaccard(a, b map[string]struct{}, idf map[string]float64) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}
	var intersection, union float64
	for t := range a {
		w := idfWeight(t, idf)
		if _, inB := b[t]; inB {
			intersection += w
		}
		union += w
	}
	for t := range b {
		if _, inA := a[t]; !inA {
			union += idfWeight(t, idf)
		}
	}
	if union == 0 {
		return 0.0
	}
	return intersection / union
}

// idfWeight returns the IDF weight for token t from the given table.
// Falls back to 1.0 (neutral weight) for tokens not present in the table,
// so scorePair degrades gracefully to flat Jaccard when called with an empty idfTable.
func idfWeight(t string, idf map[string]float64) float64 {
	if idf != nil {
		if w, ok := idf[t]; ok {
			return w
		}
	}
	return 1.0
}
```

- [ ] **Step 4: Run the new tests — verify they pass**

```bash
cd backend && go test ./internal/similarity/... -run "TestWeightedJaccard" -v 2>&1 | tail -15
```

Expected: all three PASS.

- [ ] **Step 5: Run all similarity tests**

```bash
cd backend && go test ./internal/similarity/... 2>&1 | tail -5
```

Expected: `ok  coralogix-alert-analyzer/internal/similarity`

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/similarity/engine.go internal/similarity/engine_test.go
git commit -m "feat(similarity): add weightedJaccard and idfWeight helpers"
```

---

### Task 4: Wire IDF into scorePair, computePairwiseScores, and Analyze

**Files:**
- Modify: `backend/internal/similarity/engine.go` — `scorePair`, `computePairwiseScores`, `Analyze`
- Modify: `backend/internal/similarity/engine_test.go` — update all `scorePair(a, b)` callsites; add DNS A/AAAA regression test

This task connects everything: `Analyze` builds the IDF table, threads it into `computePairwiseScores`, and `scorePair` uses `weightedJaccard` for all set-based dimensions.

**Key design note:** `idfWeight` returns `1.0` for tokens not in the table, so passing `idfTable{}` (zero value) to `scorePair` is equivalent to flat Jaccard — this keeps all existing tests valid after the signature change.

- [ ] **Step 1: Add the DNS A/AAAA regression test (should fail until Step 3)**

Add to `backend/internal/similarity/engine_test.go`:

```go
func TestScorePair_dnsAvsAAAA_notDuplicate(t *testing.T) {
	// DNS A-record and AAAA-record alerts differ only in the record_type Lucene token.
	// The groupBy dimension also differs (typical in practice: A alerts pivot on
	// querying host; AAAA alerts pivot on source IP). With IDF weighting on the
	// Lucene dimension, the rare record_type atom dominates and the pair scores
	// below the duplicate threshold.

	commonLucene := map[string]struct{}{
		"dns:query": {}, "threshold": {}, "source.ip:10.0.0.1": {},
	}
	makeQuery := func(recordType string) map[string]struct{} {
		m := make(map[string]struct{}, len(commonLucene)+1)
		for k, v := range commonLucene {
			m[k] = v
		}
		m["record_type:"+recordType] = struct{}{}
		return m
	}

	aRecord := featureVector{
		alertName:         "DNS A Record Query Spike",
		alertType:         "logs_threshold",
		dataSources:       map[string]struct{}{"dns": {}},
		entities:          map[string]struct{}{"host": {}},
		actions:           map[string]struct{}{"query": {}},
		conditions:        map[string]struct{}{"threshold": {}},
		techniques:        map[string]struct{}{"t1071": {}},
		groupByCategories: normalizeGroupByKeys([]string{"event.hostname"}),
		luceneQuery:       makeQuery("a"),
		timeWindow:        "5m",
	}
	aaaaRecord := featureVector{
		alertName:         "DNS AAAA Record Query Spike",
		alertType:         "logs_threshold",
		dataSources:       map[string]struct{}{"dns": {}},
		entities:          map[string]struct{}{"host": {}},
		actions:           map[string]struct{}{"query": {}},
		conditions:        map[string]struct{}{"threshold": {}},
		techniques:        map[string]struct{}{"t1071": {}},
		groupByCategories: normalizeGroupByKeys([]string{"clientip"}),
		luceneQuery:       makeQuery("aaaa"),
		timeWindow:        "5m",
	}

	// Build a corpus where the common Lucene tokens appear in many alerts
	// (low IDF) and the record_type atoms appear in only 1 alert each (high IDF).
	corpus := []featureVector{aRecord, aaaaRecord}
	for i := 0; i < 8; i++ {
		corpus = append(corpus, featureVector{
			luceneQuery: map[string]struct{}{"dns:query": {}, "threshold": {}, "source.ip:10.0.0.1": {}},
		})
	}
	idf := buildIDF(corpus) // N=10; record_type:a df=1 → high IDF; dns:query df=10 → low IDF

	score := scorePair(aRecord, aaaaRecord, idf)
	if score >= duplicateThreshold {
		t.Errorf("DNS A vs AAAA should NOT be duplicates with IDF weighting: score=%.4f >= threshold=%.2f", score, duplicateThreshold)
	}
}
```

- [ ] **Step 2: Run the new test — verify it FAILS (scorePair signature mismatch)**

```bash
cd backend && go test ./internal/similarity/... -run "TestScorePair_dnsAvsAAAA_notDuplicate" -v 2>&1 | tail -10
```

Expected: compile error — `scorePair` called with 3 arguments but defined with 2.

- [ ] **Step 3: Update scorePair signature and body**

Replace `scorePair` in `backend/internal/similarity/engine.go` (currently at line ~308):

```go
// scorePair computes the weighted IDF-Jaccard similarity between two feature vectors.
// The idf table scales each token's contribution by its corpus-wide rarity — rare
// tokens discriminate more than common ones. Pass idfTable{} to get flat-Jaccard
// behaviour (all weights default to 1.0 via idfWeight).
func scorePair(a, b featureVector, idf idfTable) float64 {
	score := 0.0
	score += weightDataSources * weightedJaccard(a.dataSources, b.dataSources, idf.dataSources)
	score += weightEntities * weightedJaccard(a.entities, b.entities, idf.entities)
	score += weightActions * weightedJaccard(a.actions, b.actions, idf.actions)
	score += weightConditions * weightedJaccard(a.conditions, b.conditions, idf.conditions)
	score += weightTechniques * weightedJaccard(a.techniques, b.techniques, idf.techniques)
	score += weightGroupBy * weightedJaccard(a.groupByCategories, b.groupByCategories, idf.groupBy)
	score += weightLuceneQuery * weightedJaccard(a.luceneQuery, b.luceneQuery, idf.luceneQuery)

	if a.alertType == b.alertType && a.alertType != "" {
		score += weightAlertType
	}
	if a.timeWindow == b.timeWindow && a.timeWindow != "" {
		score += weightTimeWindow
	}

	return score
}
```

- [ ] **Step 4: Update computePairwiseScores to accept and thread idf**

Replace `computePairwiseScores` in `engine.go` (currently at line ~247):

```go
// computePairwiseScores calculates the weighted Jaccard similarity for every
// unique pair (i < j). When the alert count exceeds parallelThreshold, the
// work is distributed across a goroutine worker pool.
func computePairwiseScores(vectors []featureVector, n int, idf idfTable) []pairScore {
	totalPairs := n * (n - 1) / 2
	if totalPairs == 0 {
		return nil
	}

	results := make([]pairScore, totalPairs)

	if n <= parallelThreshold {
		// Sequential path for small sets.
		idx := 0
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				results[idx] = pairScore{i: i, j: j, score: scorePair(vectors[i], vectors[j], idf)}
				idx++
			}
		}
		return results
	}

	// Parallel path: worker pool.
	type pairInput struct {
		i, j    int
		destIdx int
	}

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	ch := make(chan pairInput, workers*4)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				results[p.destIdx] = pairScore{
					i:     p.i,
					j:     p.j,
					score: scorePair(vectors[p.i], vectors[p.j], idf),
				}
			}
		}()
	}

	idx := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			ch <- pairInput{i: i, j: j, destIdx: idx}
			idx++
		}
	}
	close(ch)
	wg.Wait()

	return results
}
```

- [ ] **Step 5: Update Analyze to call buildIDF and pass idf into computePairwiseScores**

Replace the first three lines of `Analyze` (lines 134–139) in `engine.go`:

```go
	// Step 1: Build feature vectors.
	vectors := buildFeatureVectors(alerts)

	// Step 1b: Build IDF table for the corpus — used by scorePair to weight
	// rare tokens more heavily than common ones.
	idf := buildIDF(vectors)

	// Step 2: Compute pairwise similarity scores.
	n := len(vectors)
	scores := computePairwiseScores(vectors, n, idf)
```

- [ ] **Step 6: Update all existing scorePair callsites in engine_test.go**

Four tests call `scorePair(a, b)` directly. Change each to `scorePair(a, b, idfTable{})`. With an empty idfTable, `idfWeight` always returns 1.0, making `weightedJaccard` behave identically to flat `jaccard` — all existing assertions still hold.

In `engine_test.go`, make these four changes:

```go
// TestScorePair_oktaPairIsNotDuplicate (line ~100):
score := scorePair(forAccount, fromSource, idfTable{})

// TestScorePair_identicalAlertSamePivotIsDuplicate (line ~121):
score := scorePair(a, b, idfTable{})

// TestScorePair_identicalAlertNoPivotIsDuplicate (line ~144):
score := scorePair(a, b, idfTable{})

// TestScorePair_salesforcePairIsNotDuplicate (line ~249):
score := scorePair(guestUser, apiEvent, idfTable{})
```

- [ ] **Step 7: Run all similarity tests — verify they all pass**

```bash
cd backend && go test ./internal/similarity/... -v 2>&1 | grep -E "^(=== RUN|--- PASS|--- FAIL|FAIL|ok)" | head -50
```

Expected: all tests PASS, including the new `TestScorePair_dnsAvsAAAA_notDuplicate`. Zero failures.

- [ ] **Step 8: Run the full backend test suite**

```bash
cd backend && go test ./... 2>&1 | tail -20
```

Expected: all packages pass (`ok` prefix). No compile errors in any package.

- [ ] **Step 9: Commit**

```bash
cd backend && git add internal/similarity/engine.go internal/similarity/engine_test.go
git commit -m "feat(similarity): wire IDF-weighted Jaccard into scorePair, computePairwiseScores, and Analyze"
```

---

## Self-review

**Spec coverage:**
- ✓ Field:value tokenizer (Task 1)
- ✓ `idfTable` struct (Task 2)
- ✓ `buildIDF` O(n) preprocessing (Task 2)
- ✓ `weightedJaccard` + `idfWeight` (Task 3)
- ✓ `scorePair` signature update + all dimensions use weightedJaccard (Task 4)
- ✓ `computePairwiseScores` threads idf (Task 4)
- ✓ `Analyze` calls buildIDF (Task 4)
- ✓ DNS A/AAAA regression test (Task 4)
- ✓ `TestTokenizeLucene_basic` updated (Task 1)
- ✓ Existing `TestWeightsSumToOne` unchanged — weights not modified

**No placeholders:** All code is complete and concrete.

**Type consistency:** `idfTable` struct defined in Task 2, used in Tasks 3 and 4. `weightedJaccard(a, b map[string]struct{}, idf map[string]float64) float64` defined in Task 3, called in Task 4 `scorePair`. `buildIDF(vectors []featureVector) idfTable` defined in Task 2, called in Task 4 `Analyze`. All consistent.

**One architectural note:** `scorePair` no longer calls `jaccardGroupBy` — it calls `weightedJaccard(..., idf.groupBy)` directly, which also returns 0.0 for both-empty sets (same behaviour as the fixed `jaccardGroupBy`). The `jaccardGroupBy` function in `pivot_categories.go` is now unused by `scorePair` but remains for its own tests. This is fine — unused internal functions in the same package do not cause compile errors in Go.
