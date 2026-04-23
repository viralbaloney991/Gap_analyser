# Similarity Engine — IDF-Weighted Jaccard & Field:Value Tokenizer

**Date:** 2026-04-23
**Status:** Approved
**Topic:** Fix false-positive similarity scores for alerts that differ only in a specific Lucene field value (e.g. DNS A vs AAAA records) by (1) preserving `field:value` pairs as atomic tokens and (2) replacing flat Jaccard with IDF-weighted Jaccard across all set-based dimensions.

---

## Problem Summary

Two classes of false positive remain after the 9-dimension scoring fix:

1. **Single-char token loss** — `tokenizeLucene` splits `record_type:"A"` on `:` and discards `"a"` (1 char, below the `len > 1` guard). The discriminating token is silently lost. `record_type:"AAAA"` survives as `"aaaa"`. Two alerts that differ only on this field therefore share all their Lucene tokens — Lucene Jaccard ≈ 1.0 — and are incorrectly scored as duplicates.

2. **Common-token dilution** — tokens like `"aws"`, `"login"`, `"threshold"` appear in the majority of alerts. Flat Jaccard treats them identically to rare tokens like `"eventtype:guestuserwanomalyevent"`. Two alerts sharing only common tokens score artificially high.

Both problems share the same root cause: the scoring function has no awareness of how discriminating a token is across the corpus.

---

## Goals

- DNS A-record and AAAA-record alerts are **not** scored as duplicates
- Any two alerts differing only in a specific Lucene field value score below the 0.85 duplicate threshold
- Common tokens (present in >50% of alerts) contribute less weight than rare tokens
- Rare, alert-specific tokens (appearing in ≤5% of alerts) dominate the similarity score
- Existing weight table (sum = 1.00) and thresholds (0.85/0.70/0.60/0.30) remain valid
- No new external dependencies; same O(n²) pairwise complexity

## Non-Goals

- Changes to the 9 dimension weights
- Changes to family/duplicate/merge/unique thresholds
- Changes to any file outside `engine.go` and `engine_test.go`
- IDF applied to binary dimensions (alertType, timeWindow) — these remain exact-match

---

## Architecture

### Two changes, one file

All changes are confined to `backend/internal/similarity/engine.go`.

**Processing pipeline after this change:**

```
buildFeatureVectors(alerts) → []featureVector    // unchanged
buildIDF(vectors)           → idfTable           // NEW — O(n) preprocessing
computePairwiseScores(vectors, idf) → scores     // idf table threaded in
scorePair(a, b, idf)        → float64            // weightedJaccard replaces jaccard
```

---

### Change 1: Field:Value Tokenizer

**File:** `engine.go` — `tokenizeLucene`

Replace the single-pass regex split with a two-step approach:

**Step 1** — extract `field:value` pairs as atomic tokens before any splitting:
```go
atomRe := regexp.MustCompile(`\w+:[^\s()\[\]{}+\-!"]+`)
atoms  := atomRe.FindAllString(strings.ToLower(q), -1)
// e.g. "record_type:\"A\"" → ["record_type:\"a\""]
// Quotes stripped in normalisation step below
```

**Step 2** — strip atoms from the query string and tokenise the remainder on Lucene operators:
```go
remainder := atomRe.ReplaceAllString(strings.ToLower(q), " ")
splitRe   := regexp.MustCompile(`[:()\[\]{}\s+\-!"]+`)
parts     := splitRe.Split(remainder, -1)
```

**Step 3** — normalise atoms (strip surrounding quotes/whitespace), filter remainder tokens to `len > 1`, merge into result set.

```go
// Normalise atom: strip quotes and whitespace
// "record_type:\"aaaa\"" → "record_type:aaaa"
atomNormRe := regexp.MustCompile(`["\s]`)
for _, a := range atoms {
    norm := atomNormRe.ReplaceAllString(a, "")
    if len(norm) > 2 { s[norm] = struct{}{} } // skip trivially short atoms
}
```

**Before / After examples:**

| Input | Before | After |
|---|---|---|
| `record_type:"A"` | `{record_type}` (A dropped) | `{record_type:a}` |
| `record_type:"AAAA"` | `{record_type, aaaa}` | `{record_type:aaaa}` |
| `eventType:GuestUserAnomalyEvent` | `{eventtype, guestuserwanomalyevent}` | `{eventtype:guestuserwanomalyevent}` |
| `eventType:ApiAnomalyEvent` | `{eventtype, apianomalevent}` | `{eventtype:apianomalevent}` |
| `source.ip: 10.0.0.1 AND alert:true` | `{source, ip, 10, alert, true}` | `{source.ip:10.0.0.1, alert:true}` |

The field:value form is treated as one discriminating token. Two atoms with the same field but different values produce zero intersection.

---

### Change 2: IDF Table

**File:** `engine.go` — new `idfTable` struct and `buildIDF` function

```go
type idfTable struct {
    dataSources map[string]float64
    entities    map[string]float64
    actions     map[string]float64
    conditions  map[string]float64
    techniques  map[string]float64
    groupBy     map[string]float64
    luceneQuery map[string]float64
}
```

`buildIDF` makes a single O(n) pass over all feature vectors, counts document frequency per token per dimension, then computes smoothed IDF:

```
idf(t) = log(1 + N / df(t))
```

Smoothing (`1 +`) prevents IDF from exploding for hapax tokens (df=1) and avoids log(0). `N` = total number of alerts.

**Illustrative IDF values (N=200):**

| Token | Dimension | df | IDF |
|---|---|---|---|
| `aws` | dataSources | 150 | log(2.33) = 0.85 |
| `login` | actions | 80 | log(3.50) = 1.25 |
| `threshold` | conditions | 90 | log(3.22) = 1.17 |
| `record_type:aaaa` | luceneQuery | 2 | log(101) = 4.62 |
| `eventtype:guestuserwanomalyevent` | luceneQuery | 1 | log(201) = 5.30 |

`buildIDF` is called once in `Analyze`, immediately after `buildFeatureVectors`, before `computePairwiseScores`.

---

### Change 3: Weighted Jaccard

**File:** `engine.go` — new `weightedJaccard` and `idfWeight` functions; `scorePair` signature updated

```go
func weightedJaccard(a, b map[string]struct{}, idf map[string]float64) float64 {
    if len(a) == 0 && len(b) == 0 { return 0.0 }
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
    if union == 0 { return 0.0 }
    return intersection / union
}

func idfWeight(t string, idf map[string]float64) float64 {
    if w, ok := idf[t]; ok { return w }
    return 1.0 // unseen token: neutral weight
}
```

**`scorePair` signature change:**
```go
// Before:
func scorePair(a, b featureVector) float64

// After:
func scorePair(a, b featureVector, idf idfTable) float64
```

All callers (`computePairwiseScores` worker pool and sequential path) pass `idf` through.

**`jaccardGroupBy` in `pivot_categories.go`** — this function calls `jaccard()` which operates on the normalised groupBy sets. These sets contain semantic category tokens (`user`, `ip`, `resource`, etc.) — a small, stable vocabulary where IDF weighting adds little value. `jaccardGroupBy` is **not changed**; `scorePair` uses `weightedJaccard` for the `groupByCategories` dimension via the new `idf.groupBy` table.

---

### Score impact on key cases

**DNS A vs AAAA (all other dimensions identical):**

Lucene tokens:
- Alert A: `{dns_query, record_type:a, threshold}`
- Alert B: `{dns_query, record_type:aaaa, threshold}`

With IDF (N=200, df(dns_query)=40, df(record_type:a)=3, df(record_type:aaaa)=2, df(threshold)=90):
- idf(dns_query) ≈ 1.61, idf(record_type:a) ≈ 4.20, idf(record_type:aaaa) ≈ 4.62, idf(threshold) ≈ 1.17

Intersection: `{dns_query, threshold}` → weight = 1.61 + 1.17 = 2.78
Union: all 4 tokens → weight = 1.61 + 4.20 + 4.62 + 1.17 = 11.60

Weighted Lucene Jaccard = 2.78 / 11.60 ≈ **0.24**

Contribution to total score: 0.15 × 0.24 = **0.036** (was 0.15 × ~0.83 = 0.12 before)

If all other 7 dimensions score 1.0 (identical): total = 0.85 - 0.12 + 0.036 = **0.77** — below duplicate threshold of 0.85. ✓

**Identical alerts:**
Weighted Jaccard of identical sets = Σ_t idf(t) / Σ_t idf(t) = **1.0** — unchanged. ✓

---

## Key Invariant

The `TestWeightsSumToOne` test continues to pass unchanged — the 9 weight constants are not modified. IDF affects the *quality* of each dimension's Jaccard score, not the dimension weights themselves.

---

## Files Changed

| File | Change |
|---|---|
| `backend/internal/similarity/engine.go` | `tokenizeLucene` — field:value atomic extraction; new `idfTable` struct; new `buildIDF`; new `weightedJaccard` + `idfWeight`; `scorePair` adds `idf idfTable` parameter; `Analyze` calls `buildIDF`; `computePairwiseScores` threads `idf` into workers |
| `backend/internal/similarity/engine_test.go` | `TestTokenizeLucene_fieldValueAtom`; `TestTokenizeLucene_singleCharPreserved`; `TestBuildIDF_rareTokenHighWeight`; `TestWeightedJaccard_rareTokenDominates`; `TestScorePair_dnsAvsAAAA_notDuplicate`; update `TestTokenizeLucene_basic` |

No other files change.

---

## Testing

Manual smoke tests:
1. Run analysis for a client with DNS alerts — verify A-record and AAAA-record alerts are **not** in the duplicates list
2. Verify Salesforce GuestUserAnomalyEvent / ApiAnomalyEvent pair still not duplicated (regression)
3. Run `go test ./internal/similarity/... -v` — all existing tests pass; new tests pass
4. Verify `TestWeightsSumToOne` still passes (weights unchanged)
