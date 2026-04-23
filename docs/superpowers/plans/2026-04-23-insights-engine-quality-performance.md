# Insights Engine — Quality & Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 5 known issues in the similarity/insights engine: false-positive groupings, wrong family labels, vague noise explanations, slow/broken insights LLM, and add end-user model selection.

**Architecture:** Nine tasks with TDD. Each task is self-contained. Tasks 1–8 are backend; Task 9 is frontend. Tasks must run sequentially — later tasks depend on earlier data model changes. The three most significant test-breaking changes are: (a) `TestWeightsSumToOne` needs two new weight constants, (b) `TestScorePair_identical*` tests need luceneQuery+timeWindow fields, and (c) all `TestFindNoiseAlerts_*` calls need a second `nil` argument.

**Tech Stack:** Go 1.22, React 19, TypeScript, Vite

**Spec:** `docs/superpowers/specs/2026-04-23-insights-engine-quality-performance.md`

---

## File map

| File | What changes |
|---|---|
| `backend/internal/models/models.go` | Add `NoiseAlert.Reason`, `InsightsReport.Model` |
| `backend/internal/similarity/engine.go` | New `featureVector` fields; `tokenizeLucene`; weight constants; `buildFeatureVectors`; `scorePair`; `deriveFamilyName` 3-tier; `findNoiseAlerts` signature+reason |
| `backend/internal/similarity/pivot_categories.go` | `jaccardGroupBy` empty+empty → 0.0 |
| `backend/internal/similarity/engine_test.go` | Update 5 existing tests; add 4 new tests |
| `backend/internal/similarity/pivot_categories_test.go` | Update `TestJaccardGroupBy_bothEmpty` |
| `backend/internal/insights/enrich.go` | Structured prompt; richer noise; reduced caps |
| `backend/internal/api/handlers.go` | `HandleInsights` model param + ir.Model |
| `backend/clients.yaml` | `insights_model` → `mistralai/mistral-small-4-119b-2603` |
| `frontend/src/types/index.ts` | `InsightsReport.model`, `NoiseAlert.reason` |
| `frontend/src/services/api.ts` | `fetchInsights` optional model param |
| `frontend/src/components/AlertInsights.tsx` | Model badge + dropdown + Regenerate; noise reason |
| `frontend/src/App.tsx` | Pass `clientName` as `client` prop to `AlertInsights` |

---

### Task 1: Data model additions

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Add `Reason` to `NoiseAlert` and `Model` to `InsightsReport`**

In `backend/internal/models/models.go`:

Find `NoiseAlert` (currently line 75):
```go
type NoiseAlert struct {
	Name            string   `json:"name"`
	MissingFeatures []string `json:"missing_features"`
}
```
Replace with:
```go
type NoiseAlert struct {
	Name            string   `json:"name"`
	MissingFeatures []string `json:"missing_features"`
	Reason          string   `json:"reason,omitempty"`
}
```

Find `InsightsReport` (currently line 110):
```go
type InsightsReport struct {
	Summary            string   `json:"summary"`
```
Replace with:
```go
type InsightsReport struct {
	Model              string   `json:"model,omitempty"`
	Summary            string   `json:"summary"`
```

- [ ] **Step 2: Build the backend to confirm no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/models/models.go
git commit -m "feat(models): add NoiseAlert.Reason and InsightsReport.Model fields"
```

---

### Task 2: Similarity engine — new feature dimensions

**Files:**
- Modify: `backend/internal/similarity/engine.go`
- Modify: `backend/internal/similarity/engine_test.go`

This task adds three new fields to `featureVector` (`luceneQuery`, `timeWindow`, `tactics`), the `tokenizeLucene` helper, updates weight constants to the 9-dimension model (sum still = 1.00), populates the new fields in `buildFeatureVectors`, and updates `scorePair`.

- [ ] **Step 1: Update `TestWeightsSumToOne` to include new dimensions (test first)**

In `backend/internal/similarity/engine_test.go`, find `TestWeightsSumToOne` (around line 173):
```go
func TestWeightsSumToOne(t *testing.T) {
	const sum = weightDataSources + weightEntities + weightActions +
		weightConditions + weightTechniques + weightGroupBy + weightAlertType
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("similarity weights sum to %.10f, want exactly 1.0", sum)
	}
}
```
Replace with:
```go
func TestWeightsSumToOne(t *testing.T) {
	const sum = weightDataSources + weightEntities + weightActions +
		weightConditions + weightTechniques + weightGroupBy + weightAlertType +
		weightLuceneQuery + weightTimeWindow
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("similarity weights sum to %.10f, want exactly 1.0", sum)
	}
}
```

- [ ] **Step 2: Update `TestScorePair_identicalAlertSamePivotIsDuplicate` to include new fields**

Find `TestScorePair_identicalAlertSamePivotIsDuplicate` (around line 106):
```go
func TestScorePair_identicalAlertSamePivotIsDuplicate(t *testing.T) {
	a := featureVector{
		alertName: "Alert A",
		alertType: "logs_threshold",
		dataSources: map[string]struct{}{"okta": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"login": {}},
		conditions:  map[string]struct{}{"failure": {}},
		techniques:  map[string]struct{}{"t1110": {}},
		groupByCategories: normalizeGroupByKeys([]string{"okta.actor.alternateId"}),
	}
	b := a
	b.alertName = "Alert B"
	score := scorePair(a, b)
	if score < duplicateThreshold {
		t.Errorf("identical alert with same pivot should be duplicate: score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}
```
Replace with:
```go
func TestScorePair_identicalAlertSamePivotIsDuplicate(t *testing.T) {
	a := featureVector{
		alertName: "Alert A",
		alertType: "logs_threshold",
		dataSources: map[string]struct{}{"okta": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"login": {}},
		conditions:  map[string]struct{}{"failure": {}},
		techniques:  map[string]struct{}{"t1110": {}},
		groupByCategories: normalizeGroupByKeys([]string{"okta.actor.alternateId"}),
		luceneQuery: map[string]struct{}{"eventtype": {}, "okta": {}, "login": {}},
		timeWindow:  "5m",
	}
	b := a
	b.alertName = "Alert B"
	score := scorePair(a, b)
	if score < duplicateThreshold {
		t.Errorf("identical alert with same pivot should be duplicate: score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}
```

- [ ] **Step 3: Update `TestScorePair_identicalAlertNoPivotIsDuplicate` to include new fields**

Find `TestScorePair_identicalAlertNoPivotIsDuplicate` (around line 125):
```go
func TestScorePair_identicalAlertNoPivotIsDuplicate(t *testing.T) {
	a := featureVector{
		alertName:   "Alert A",
		alertType:   "logs_threshold",
		dataSources: map[string]struct{}{"aws": {}},
		entities:    map[string]struct{}{"role": {}},
		actions:     map[string]struct{}{"assumerole": {}},
		conditions:  map[string]struct{}{"cross_account": {}},
		techniques:  map[string]struct{}{"t1550": {}},
		// groupByCategories intentionally nil on both sides
	}
	b := a
	b.alertName = "Alert B"
	score := scorePair(a, b)
	if score < duplicateThreshold {
		t.Errorf("identical alert with no pivot should still be duplicate (empty+empty=1.0): score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}
```
Replace with:
```go
func TestScorePair_identicalAlertNoPivotIsDuplicate(t *testing.T) {
	// After the jaccardGroupBy fix (empty+empty→0.0), two identical alerts with no
	// groupBy need their Lucene query and timeWindow dimensions to reach the threshold.
	a := featureVector{
		alertName:   "Alert A",
		alertType:   "logs_threshold",
		dataSources: map[string]struct{}{"aws": {}},
		entities:    map[string]struct{}{"role": {}},
		actions:     map[string]struct{}{"assumerole": {}},
		conditions:  map[string]struct{}{"cross_account": {}},
		techniques:  map[string]struct{}{"t1550": {}},
		luceneQuery: map[string]struct{}{"assumerole": {}, "cross_account": {}, "aws": {}},
		timeWindow:  "5m",
		// groupByCategories intentionally nil on both sides
	}
	b := a
	b.alertName = "Alert B"
	score := scorePair(a, b)
	if score < duplicateThreshold {
		t.Errorf("identical alert with no pivot should still be duplicate: score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}
```

- [ ] **Step 4: Add new regression test for Salesforce false-positive pair**

Add at the end of `backend/internal/similarity/engine_test.go`:
```go
func TestScorePair_salesforcePairIsNotDuplicate(t *testing.T) {
	// GuestUserAnomalyEvent and ApiAnomalyEvent are distinct Salesforce event types.
	// Without Lucene query scoring, these score 100% similar. With it, they should
	// fall below the duplicate threshold because the event-type token differs.
	guestUser := featureVector{
		alertName:   "Salesforce - SFDC - Security Event - GuestUserAnomalyEvent",
		alertType:   "logs_threshold",
		dataSources: map[string]struct{}{"salesforce": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"anomaly": {}},
		conditions:  map[string]struct{}{"security_event": {}},
		techniques:  map[string]struct{}{"t1078": {}},
		luceneQuery: tokenizeLucene("eventType:GuestUserAnomalyEvent AND coralogix.metadata.applicationName:salesforce"),
		timeWindow:  "5m",
	}
	apiEvent := featureVector{
		alertName:   "Salesforce - SFDC - Security Event - ApiAnomalyEvent",
		alertType:   "logs_threshold",
		dataSources: map[string]struct{}{"salesforce": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"anomaly": {}},
		conditions:  map[string]struct{}{"security_event": {}},
		techniques:  map[string]struct{}{"t1078": {}},
		luceneQuery: tokenizeLucene("eventType:ApiAnomalyEvent AND coralogix.metadata.applicationName:salesforce"),
		timeWindow:  "5m",
	}
	score := scorePair(guestUser, apiEvent)
	if score >= duplicateThreshold {
		t.Errorf("distinct Salesforce event types should NOT be duplicates: score=%.4f >= threshold=%.2f", score, duplicateThreshold)
	}
}
```

- [ ] **Step 5: Add test for `tokenizeLucene`**

Add at the end of `backend/internal/similarity/engine_test.go`:
```go
func TestTokenizeLucene_basic(t *testing.T) {
	tokens := tokenizeLucene("eventType:GuestUserAnomalyEvent AND coralogix.metadata.applicationName:salesforce")
	want := map[string]struct{}{
		"eventtype":              {},
		"guestuseranomalyevent":  {},
		"and":                    {},
		"coralogix.metadata.applicationname": {},
		"salesforce":             {},
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

- [ ] **Step 6: Run the tests — expect failures (new constants don't exist yet)**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... 2>&1 | head -30
```

Expected: compile error referencing `weightLuceneQuery`, `weightTimeWindow`, `luceneQuery`, `timeWindow`, `tokenizeLucene`. That's correct — the implementation comes next.

- [ ] **Step 7: Update `featureVector` struct in `engine.go`**

In `backend/internal/similarity/engine.go`, find the `featureVector` struct (lines 15–25):
```go
type featureVector struct {
	alertID           string
	alertName         string
	alertType         string
	dataSources       map[string]struct{}
	entities          map[string]struct{}
	actions           map[string]struct{}
	conditions        map[string]struct{}
	techniques        map[string]struct{}
	groupByCategories map[string]struct{}
}
```
Replace with:
```go
type featureVector struct {
	alertID           string
	alertName         string
	alertType         string
	dataSources       map[string]struct{}
	entities          map[string]struct{}
	actions           map[string]struct{}
	conditions        map[string]struct{}
	techniques        map[string]struct{}
	groupByCategories map[string]struct{}
	luceneQuery       map[string]struct{} // tokenised Lucene query — actual detection logic
	timeWindow        string              // from AlertFeatures.TimeWindow
	tactics           []string            // from AlertFeatures.Tactics — used by deriveFamilyName
}
```

- [ ] **Step 8: Update weight constants in `engine.go`**

Find the `const` block starting with `// Weights sum to exactly 1.00.` (around line 34):
```go
const (
	// Weights sum to exactly 1.00.
	weightDataSources = 0.15
	weightEntities    = 0.10
	weightActions     = 0.15
	weightConditions  = 0.15
	weightTechniques  = 0.10
	weightGroupBy     = 0.25
	weightAlertType   = 0.10
```
Replace with:
```go
const (
	// Weights sum to exactly 1.00.
	// 9-dimension model: Lucene query and TimeWindow added in 2026-04.
	weightDataSources  = 0.15
	weightEntities     = 0.10
	weightActions      = 0.15
	weightConditions   = 0.10 // reduced: less signal than Lucene query
	weightTechniques   = 0.10
	weightGroupBy      = 0.15 // reduced: was over-dominant
	weightAlertType    = 0.05 // reduced: binary, low signal
	weightLuceneQuery  = 0.15 // new: actual detection logic
	weightTimeWindow   = 0.05 // new: binary equality bonus
```

- [ ] **Step 9: Add `tokenizeLucene` helper in `engine.go`**

Add the following function just before `// ---------------------------------------------------------------------------\n// Step 1: Feature Vector Creation` comment:
```go
// tokenizeLucene splits a Lucene query string into a lowercase token set.
// Splits on whitespace and Lucene operators: `:()[]{}+-!"`.
// Single-character tokens are dropped (noise).
func tokenizeLucene(q string) map[string]struct{} {
	if q == "" {
		return nil
	}
	re := regexp.MustCompile(`[:()\[\]{}\s+\-!"]+`)
	parts := re.Split(strings.ToLower(q), -1)
	s := make(map[string]struct{})
	for _, t := range parts {
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

Also add `"regexp"` to the import block at the top of `engine.go` (currently imports `fmt`, `math`, `runtime`, `sort`, `strings`, `sync`, and the models package):
```go
import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/models"
)
```

- [ ] **Step 10: Update `buildFeatureVectors` to populate new fields**

Find `buildFeatureVectors` (around line 143):
```go
func buildFeatureVectors(alerts []*models.AlertDef) []featureVector {
	vectors := make([]featureVector, len(alerts))
	for i, a := range alerts {
		vectors[i] = featureVector{
			alertID:           a.ID,
			alertName:         a.Name,
			alertType:         strings.ToLower(a.AlertType),
			dataSources:       toSet(a.Features.DataSources),
			entities:          toSet(a.Features.Entities),
			actions:           toSet(a.Features.Actions),
			conditions:        toSet(a.Features.Conditions),
			techniques:        toSet(a.Features.Techniques),
			groupByCategories: normalizeGroupByKeys(a.GroupByKeys),
		}
	}
	return vectors
}
```
Replace with:
```go
func buildFeatureVectors(alerts []*models.AlertDef) []featureVector {
	vectors := make([]featureVector, len(alerts))
	for i, a := range alerts {
		vectors[i] = featureVector{
			alertID:           a.ID,
			alertName:         a.Name,
			alertType:         strings.ToLower(a.AlertType),
			dataSources:       toSet(a.Features.DataSources),
			entities:          toSet(a.Features.Entities),
			actions:           toSet(a.Features.Actions),
			conditions:        toSet(a.Features.Conditions),
			techniques:        toSet(a.Features.Techniques),
			groupByCategories: normalizeGroupByKeys(a.GroupByKeys),
			luceneQuery:       tokenizeLucene(coralogix.ExtractLuceneQuery(a.TypeDef)),
			timeWindow:        a.Features.TimeWindow,
			tactics:           a.Features.Tactics,
		}
	}
	return vectors
}
```

- [ ] **Step 11: Update `scorePair` to include new dimensions**

Find `scorePair` (around line 242):
```go
func scorePair(a, b featureVector) float64 {
	score := 0.0
	score += weightDataSources * jaccard(a.dataSources, b.dataSources)
	score += weightEntities * jaccard(a.entities, b.entities)
	score += weightActions * jaccard(a.actions, b.actions)
	score += weightConditions * jaccard(a.conditions, b.conditions)
	score += weightTechniques * jaccard(a.techniques, b.techniques)
	score += weightGroupBy * jaccardGroupBy(a.groupByCategories, b.groupByCategories)

	if a.alertType == b.alertType && a.alertType != "" {
		score += weightAlertType
	}

	return score
}
```
Replace with:
```go
func scorePair(a, b featureVector) float64 {
	score := 0.0
	score += weightDataSources * jaccard(a.dataSources, b.dataSources)
	score += weightEntities * jaccard(a.entities, b.entities)
	score += weightActions * jaccard(a.actions, b.actions)
	score += weightConditions * jaccard(a.conditions, b.conditions)
	score += weightTechniques * jaccard(a.techniques, b.techniques)
	score += weightGroupBy * jaccardGroupBy(a.groupByCategories, b.groupByCategories)
	score += weightLuceneQuery * jaccard(a.luceneQuery, b.luceneQuery)

	if a.alertType == b.alertType && a.alertType != "" {
		score += weightAlertType
	}
	if a.timeWindow == b.timeWindow && a.timeWindow != "" {
		score += weightTimeWindow
	}

	return score
}
```

- [ ] **Step 12: Run tests — expect them to pass now**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run "TestWeightsSumToOne|TestScorePair|TestTokenizeLucene" -v 2>&1 | tail -30
```

Expected: all listed tests pass. The `TestScorePair_identicalAlert*` tests may still fail until Task 3 fixes `jaccardGroupBy` — that's fine, they'll pass after Task 3.

Actually, run the full suite to see which pass now:
```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -v 2>&1 | grep -E "^(--- PASS|--- FAIL|FAIL|ok)"
```

Expected passing now: `TestTokenizeLucene_basic`, `TestWeightsSumToOne`, `TestScorePair_salesforcePairIsNotDuplicate`. The `TestScorePair_identicalAlert*` tests may still fail (see Task 3).

- [ ] **Step 13: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(similarity): add 9-dimension scoring model (LuceneQuery + TimeWindow)"
```

---

### Task 3: Fix jaccardGroupBy empty+empty → 0.0

**Files:**
- Modify: `backend/internal/similarity/pivot_categories.go`
- Modify: `backend/internal/similarity/pivot_categories_test.go`

The existing behaviour (`return 1.0` for both-empty) incorrectly inflates scores for alerts with no groupBy keys. Two alerts that both have no groupBy have nothing in common on that dimension.

- [ ] **Step 1: Update `TestJaccardGroupBy_bothEmpty` (test first)**

In `backend/internal/similarity/pivot_categories_test.go`, find `TestJaccardGroupBy_bothEmpty` (around line 80):
```go
func TestJaccardGroupBy_bothEmpty(t *testing.T) {
	if score := jaccardGroupBy(nil, nil); score != 1.0 {
		t.Errorf("expected 1.0 for both empty (compatible), got %f", score)
	}
}
```
Replace with:
```go
func TestJaccardGroupBy_bothEmpty(t *testing.T) {
	// Two alerts with no groupBy keys have nothing in common on this dimension.
	if score := jaccardGroupBy(nil, nil); score != 0.0 {
		t.Errorf("expected 0.0 for both empty (no common keys), got %f", score)
	}
}
```

- [ ] **Step 2: Run the updated test to confirm it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run TestJaccardGroupBy_bothEmpty -v
```

Expected: `--- FAIL: TestJaccardGroupBy_bothEmpty`.

- [ ] **Step 3: Fix `jaccardGroupBy` in `pivot_categories.go`**

Find `jaccardGroupBy` in `backend/internal/similarity/pivot_categories.go` (around line 80):
```go
// jaccardGroupBy computes Jaccard similarity for pivot category sets.
// Unlike jaccard(), two empty sets return 1.0 — both unspecified means compatible.
func jaccardGroupBy(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	return jaccard(a, b)
}
```
Replace with:
```go
// jaccardGroupBy computes Jaccard similarity for pivot category sets.
// Two empty sets return 0.0 — no groupBy keys means no common groupBy signal.
func jaccardGroupBy(a, b map[string]struct{}) float64 {
	return jaccard(a, b)
}
```

- [ ] **Step 4: Run the full similarity test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -v 2>&1 | grep -E "^(--- PASS|--- FAIL|FAIL|ok)"
```

Expected: All tests pass. Notably:
- `TestJaccardGroupBy_bothEmpty` now passes (returns 0.0)
- `TestScorePair_identicalAlertNoPivotIsDuplicate` now passes (luceneQuery+timeWindow added in Task 2 bring score to 0.85)
- `TestScorePair_identicalAlertSamePivotIsDuplicate` now passes (luceneQuery+timeWindow added bring score to 1.00)
- `TestScorePair_salesforcePairIsNotDuplicate` passes (Lucene token diff pulls score below 0.85)

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/similarity/pivot_categories.go backend/internal/similarity/pivot_categories_test.go
git commit -m "fix(similarity): jaccardGroupBy empty+empty returns 0.0 (was 1.0)"
```

---

### Task 4: deriveFamilyName — 3-tier semantic naming

**Files:**
- Modify: `backend/internal/similarity/engine.go`

Replace the single-path frequency lookup with a three-tier chain: MITRE tactic names first, action→category semantic map second, raw token fallback third.

- [ ] **Step 1: Add tests for the new family naming (test first)**

Add at the end of `backend/internal/similarity/engine_test.go`:
```go
func TestDeriveFamilyName_usesMitreTactic(t *testing.T) {
	vectors := []featureVector{
		{tactics: []string{"privilege-escalation"}, actions: map[string]struct{}{"grant": {}}},
		{tactics: []string{"privilege-escalation"}, actions: map[string]struct{}{"sudo": {}}},
	}
	name := deriveFamilyName(vectors, []int{0, 1}, 1)
	if name != "Privilege Escalation Detections" {
		t.Errorf("expected \"Privilege Escalation Detections\", got %q", name)
	}
}

func TestDeriveFamilyName_usesActionCategory(t *testing.T) {
	// No tactics set — should fall back to action→category map.
	vectors := []featureVector{
		{tactics: nil, actions: map[string]struct{}{"remove": {}}},
		{tactics: nil, actions: map[string]struct{}{"delete": {}}},
	}
	name := deriveFamilyName(vectors, []int{0, 1}, 1)
	if name != "Tampering Detections" {
		t.Errorf("expected \"Tampering Detections\", got %q", name)
	}
}

func TestDeriveFamilyName_fallsBackToRawToken(t *testing.T) {
	// No tactics, no matching action category — raw token fallback.
	vectors := []featureVector{
		{tactics: nil, actions: map[string]struct{}{"frobnicate": {}}},
		{tactics: nil, actions: map[string]struct{}{"frobnicate": {}}},
	}
	name := deriveFamilyName(vectors, []int{0, 1}, 1)
	if name != "Frobnicate Detections" {
		t.Errorf("expected \"Frobnicate Detections\", got %q", name)
	}
}
```

- [ ] **Step 2: Run the new tests — confirm they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run "TestDeriveFamilyName" -v
```

Expected: all three FAIL.

- [ ] **Step 3: Add `tacticLabels` and `actionCategories` lookup tables in `engine.go`**

Add these two package-level variables just before the `Analyze` function:

```go
// tacticLabels maps MITRE ATT&CK tactic slugs to human-readable names.
var tacticLabels = map[string]string{
	"initial-access":       "Initial Access",
	"execution":            "Execution",
	"persistence":          "Persistence",
	"privilege-escalation": "Privilege Escalation",
	"defense-evasion":      "Defense Evasion",
	"credential-access":    "Credential Access",
	"discovery":            "Discovery",
	"lateral-movement":     "Lateral Movement",
	"collection":           "Collection",
	"exfiltration":         "Exfiltration",
	"command-and-control":  "Command & Control",
	"impact":               "Impact",
	"reconnaissance":       "Reconnaissance",
	"resource-development": "Resource Development",
}

// actionCategories maps action keyword prefixes to security category labels.
// Order matters: first match wins.
var actionCategories = []struct {
	keywords []string
	category string
}{
	{[]string{"remove", "delete", "revoke", "wipe"}, "Tampering"},
	{[]string{"login", "authenticate", "signin", "logon"}, "Authentication"},
	{[]string{"escalat", "grant", "privilege", "sudo"}, "Privilege Escalation"},
	{[]string{"exfiltrat", "download", "upload", "transfer"}, "Exfiltration"},
	{[]string{"scan", "enumerat", "discover", "recon"}, "Discovery"},
	{[]string{"execute", "run", "inject", "spawn"}, "Execution"},
	{[]string{"persist", "install", "schedule", "startup"}, "Persistence"},
	{[]string{"encrypt", "ransom", "destroy"}, "Impact"},
}
```

- [ ] **Step 4: Replace `deriveFamilyName` with the 3-tier implementation**

Find `deriveFamilyName` (around line 357):
```go
func deriveFamilyName(vectors []featureVector, members []int, fallbackNum int) string {
	freq := make(map[string]int)

	// Count technique tokens first (higher signal).
	for _, idx := range members {
		for t := range vectors[idx].techniques {
			freq[t]++
		}
	}

	// If no techniques, fall back to actions.
	if len(freq) == 0 {
		for _, idx := range members {
			for a := range vectors[idx].actions {
				freq[a]++
			}
		}
	}

	if len(freq) == 0 {
		return fmt.Sprintf("Detection Family %d", fallbackNum)
	}

	// Find the most common token.
	bestToken := ""
	bestCount := 0
	for tok, count := range freq {
		if count > bestCount || (count == bestCount && tok < bestToken) {
			bestToken = tok
			bestCount = count
		}
	}

	// Title-case the token for readability.
	return toTitle(bestToken) + " Detections"
}
```
Replace with:
```go
func deriveFamilyName(vectors []featureVector, members []int, fallbackNum int) string {
	// Tier 1: most frequent MITRE tactic across cluster members.
	tacticFreq := make(map[string]int)
	for _, idx := range members {
		for _, tac := range vectors[idx].tactics {
			tacticFreq[strings.ToLower(tac)]++
		}
	}
	if len(tacticFreq) > 0 {
		bestTactic, bestCount := "", 0
		for tac, count := range tacticFreq {
			if count > bestCount || (count == bestCount && tac < bestTactic) {
				bestTactic = tac
				bestCount = count
			}
		}
		if label, ok := tacticLabels[bestTactic]; ok {
			return label + " Detections"
		}
	}

	// Tier 2: action tokens matched against semantic category map.
	for _, idx := range members {
		for action := range vectors[idx].actions {
			lower := strings.ToLower(action)
			for _, entry := range actionCategories {
				for _, kw := range entry.keywords {
					if strings.Contains(lower, kw) {
						return entry.category + " Detections"
					}
				}
			}
		}
	}

	// Tier 3: most frequent technique or action token (original behaviour).
	freq := make(map[string]int)
	for _, idx := range members {
		for t := range vectors[idx].techniques {
			freq[t]++
		}
	}
	if len(freq) == 0 {
		for _, idx := range members {
			for a := range vectors[idx].actions {
				freq[a]++
			}
		}
	}
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

- [ ] **Step 5: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run "TestDeriveFamilyName" -v
```

Expected: all three pass.

- [ ] **Step 6: Run full suite to ensure no regressions**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... 2>&1 | tail -5
```

Expected: `ok  coralogix-alert-analyzer/internal/similarity`.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(similarity): deriveFamilyName 3-tier semantic naming (tactic > category > raw)"
```

---

### Task 5: findNoiseAlerts — signature, broad-scope exclusion, and Reason field

**Files:**
- Modify: `backend/internal/similarity/engine.go`
- Modify: `backend/internal/similarity/engine_test.go`

This task updates `findNoiseAlerts` to accept `alerts []*models.AlertDef`, adds broad-scope exclusion, and populates the `Reason` field.

- [ ] **Step 1: Update all existing `findNoiseAlerts` test call sites (test update first)**

In `backend/internal/similarity/engine_test.go`, update every call to `findNoiseAlerts(vectors)` to `findNoiseAlerts(vectors, nil)`. There are 5 occurrences (lines 31, 42, 59, 69, 193 approximately):

- `TestFindNoiseAlerts_returnsNoisyAlerts`: `noisy := findNoiseAlerts(vectors)` → `noisy := findNoiseAlerts(vectors, nil)`
- `TestFindNoiseAlerts_nilInput`: `noisy := findNoiseAlerts(nil)` → `noisy := findNoiseAlerts(nil, nil)`
- `TestFindNoiseAlerts_atThreshold`: `noisy := findNoiseAlerts(vectors)` → `noisy := findNoiseAlerts(vectors, nil)`
- `TestFindNoiseAlerts_isSorted`: `noisy := findNoiseAlerts(vectors)` → `noisy := findNoiseAlerts(vectors, nil)`
- `TestFindNoiseAlerts_missingFeaturesPopulated`: `noisy := findNoiseAlerts(vectors)` → `noisy := findNoiseAlerts(vectors, nil)`

- [ ] **Step 2: Add new tests for Reason and broad-scope exclusion**

Add at the end of `backend/internal/similarity/engine_test.go`:
```go
func TestFindNoiseAlerts_reasonPopulated(t *testing.T) {
	vectors := []featureVector{
		{
			alertName:   "NoEntities",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{},
			actions:     map[string]struct{}{},
			conditions:  map[string]struct{}{},
			techniques:  map[string]struct{}{},
		},
	}
	noisy := findNoiseAlerts(vectors, nil)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert, got %d", len(noisy))
	}
	if noisy[0].Reason == "" {
		t.Error("expected Reason to be populated, got empty string")
	}
}

func TestFindNoiseAlerts_broadScopeExcluded(t *testing.T) {
	// A broad-scope alert (no app/subsystem filter) with low features is an
	// intentional global monitor — it must NOT appear in the noise list.
	vectors := []featureVector{
		{
			alertName:   "BroadScope",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{},
			actions:     map[string]struct{}{},
			conditions:  map[string]struct{}{},
			techniques:  map[string]struct{}{},
		},
	}
	// Alert with nil TypeDef → ExtractAppSubsystem returns ("", "") → broad-scope.
	alerts := []*models.AlertDef{
		{ID: "", Name: "BroadScope", TypeDef: nil},
	}
	noisy := findNoiseAlerts(vectors, alerts)
	if len(noisy) != 0 {
		t.Errorf("broad-scope alert should be excluded from noise list, got %v", noisy)
	}
}
```

- [ ] **Step 3: Run the updated tests — confirm they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run "TestFindNoiseAlerts" -v 2>&1 | head -40
```

Expected: compile error — `findNoiseAlerts` still has old signature. That's correct.

- [ ] **Step 4: Update `findNoiseAlerts` in `engine.go`**

Find `findNoiseAlerts` (around line 846):
```go
func findNoiseAlerts(vectors []featureVector) []models.NoiseAlert {
	const noiseThreshold = 3
	var noisy []models.NoiseAlert
	for _, v := range vectors {
		total := len(v.dataSources) + len(v.entities) + len(v.actions) +
			len(v.conditions) + len(v.techniques)
		if total < noiseThreshold {
			var missing []string
			if len(v.dataSources) == 0 {
				missing = append(missing, "data sources")
			}
			if len(v.entities) == 0 {
				missing = append(missing, "entities")
			}
			if len(v.actions) == 0 {
				missing = append(missing, "actions")
			}
			if len(v.conditions) == 0 {
				missing = append(missing, "conditions")
			}
			if len(v.techniques) == 0 {
				missing = append(missing, "techniques")
			}
			noisy = append(noisy, models.NoiseAlert{
				Name:            v.alertName,
				MissingFeatures: missing,
			})
		}
	}
	sort.Slice(noisy, func(i, j int) bool {
		return noisy[i].Name < noisy[j].Name
	})
	return noisy
}
```
Replace with:
```go
// findNoiseAlerts returns NoiseAlert entries for alerts whose total unique
// feature token count is below the noise threshold (sparse = likely
// threshold-only alert). Each entry includes the names of empty feature sets
// and a human-readable Reason explaining the most significant gaps.
// Broad-scope alerts (no app/subsystem filter) are excluded — they are
// intentionally global monitors, not misconfigured rules.
// alerts is parallel to vectors (same order as buildFeatureVectors input);
// pass nil in tests that don't need broad-scope detection.
func findNoiseAlerts(vectors []featureVector, alerts []*models.AlertDef) []models.NoiseAlert {
	const noiseThreshold = 3
	var noisy []models.NoiseAlert
	for i, v := range vectors {
		total := len(v.dataSources) + len(v.entities) + len(v.actions) +
			len(v.conditions) + len(v.techniques)
		if total >= noiseThreshold {
			continue
		}

		// Exclude broad-scope alerts (intentional global monitors).
		if alerts != nil && i < len(alerts) && alerts[i] != nil {
			app, sub := coralogix.ExtractAppSubsystem(alerts[i].TypeDef)
			if app == "" && sub == "" {
				continue
			}
		}

		var missing []string
		if len(v.dataSources) == 0 {
			missing = append(missing, "data sources")
		}
		if len(v.entities) == 0 {
			missing = append(missing, "entities")
		}
		if len(v.actions) == 0 {
			missing = append(missing, "actions")
		}
		if len(v.conditions) == 0 {
			missing = append(missing, "conditions")
		}
		if len(v.techniques) == 0 {
			missing = append(missing, "techniques")
		}

		// Build a specific Reason from the highest-priority gaps (up to two).
		var reasons []string
		if len(v.dataSources) == 0 {
			reasons = append(reasons, "No log source identified — alert may fire across unintended data sources.")
		}
		if len(v.entities) == 0 {
			reasons = append(reasons, "No monitored entity (user, IP, host) — cannot scope blast radius or owner.")
		}
		if len(v.actions) == 0 && len(v.conditions) == 0 {
			reasons = append(reasons, "No behavioral signal — likely a generic threshold with no attack-pattern context.")
		}
		if len(v.techniques) == 0 {
			reasons = append(reasons, "No MITRE technique mapped — coverage gap, hard to classify threat type.")
		}
		reason := ""
		if len(reasons) > 0 {
			if len(reasons) > 2 {
				reasons = reasons[:2]
			}
			reason = strings.Join(reasons, " ")
		}

		noisy = append(noisy, models.NoiseAlert{
			Name:            v.alertName,
			MissingFeatures: missing,
			Reason:          reason,
		})
	}
	sort.Slice(noisy, func(i, j int) bool {
		return noisy[i].Name < noisy[j].Name
	})
	return noisy
}
```

- [ ] **Step 5: Update the `Analyze` function call site**

Find in `Analyze` (around line 125):
```go
	// Step 8: Noise detection.
	noiseAlerts := findNoiseAlerts(vectors)
```
Replace with:
```go
	// Step 8: Noise detection.
	noiseAlerts := findNoiseAlerts(vectors, alerts)
```

- [ ] **Step 6: Run the full test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -v 2>&1 | grep -E "^(--- PASS|--- FAIL|FAIL|ok)"
```

Expected: all tests pass, including the new `TestFindNoiseAlerts_reasonPopulated` and `TestFindNoiseAlerts_broadScopeExcluded`.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(similarity): findNoiseAlerts adds Reason field and excludes broad-scope alerts"
```

---

### Task 6: buildPrompt — structured format, richer noise context, reduced caps

**Files:**
- Modify: `backend/internal/insights/enrich.go`

Replace the verbose LLM prompt with a compact structured format (~1126 chars vs ~1789 chars), include `MissingFeatures` and `Reason` per noise alert, and reduce prompt caps.

- [ ] **Step 1: Add a test for the structured prompt format**

In `backend/internal/insights/enrich_test.go`, add at the end:
```go
func TestBuildPrompt_includesNoiseReason(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"A", "B"}, Similarity: 0.92},
		},
		NoiseAlerts: []models.NoiseAlert{
			{
				Name:            "SparseAlert",
				MissingFeatures: []string{"entities", "actions"},
				Reason:          "No monitored entity. No behavioral signal.",
			},
		},
	}
	prompt := buildPrompt(result, nil)
	if !strings.Contains(prompt, "SparseAlert") {
		t.Error("prompt should contain noise alert name")
	}
	if !strings.Contains(prompt, "No monitored entity") {
		t.Error("prompt should contain noise reason")
	}
	if !strings.Contains(prompt, "entities, actions") {
		t.Error("prompt should contain missing features")
	}
}
```

Also add `"strings"` to the import block if not already present.

- [ ] **Step 2: Run the new test — confirm it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/... -run TestBuildPrompt -v
```

Expected: FAIL — current prompt doesn't include Reason.

- [ ] **Step 3: Replace `buildPrompt` and reduce caps in `enrich.go`**

Find the constants block and `buildPrompt` in `backend/internal/insights/enrich.go`:
```go
const (
	maxPromptDuplicates = 15
	maxPromptNoise      = 20
	maxPromptFamilies   = 15
)

func buildPrompt(result *models.SimilarityResult, alerts []*models.AlertDef) string {
	...
}
```
Replace the entire block (from `const (` through the closing `}` of `buildPrompt`) with:
```go
const (
	maxPromptDuplicates = 10 // reduced from 15
	maxPromptFamilies   = 8  // reduced from 15
	maxPromptNoise      = 12 // reduced from 20
)

func buildPrompt(result *models.SimilarityResult, alerts []*models.AlertDef) string {
	var sb strings.Builder

	sb.WriteString("Role: Senior detection engineer. Task: Analyze alert library quality.\n\n")
	sb.WriteString(fmt.Sprintf("Library: %d alerts | %d duplicates | %d families | %d noisy alerts\n\n",
		len(alerts), len(result.Duplicates), len(result.Families), len(result.NoiseAlerts)))

	// Duplicates section.
	dups := result.Duplicates
	if len(dups) > maxPromptDuplicates {
		dups = dups[:maxPromptDuplicates]
	}
	if len(dups) > 0 {
		sb.WriteString("Duplicates:\n")
		for _, d := range dups {
			if len(d.AlertNames) >= 2 {
				sb.WriteString(fmt.Sprintf("- %s ≈ %s (%.0f%%)\n", d.AlertNames[0], d.AlertNames[1], d.Similarity*100))
			}
		}
		sb.WriteString("\n")
	}

	// Families section.
	families := result.Families
	if len(families) > maxPromptFamilies {
		families = families[:maxPromptFamilies]
	}
	if len(families) > 0 {
		sb.WriteString("Families: ")
		parts := make([]string, len(families))
		for i, f := range families {
			parts[i] = fmt.Sprintf("%s (%s)", f.Name, strings.Join(f.AlertNames, ", "))
		}
		sb.WriteString(strings.Join(parts, " | "))
		sb.WriteString("\n\n")
	}

	// Coverage gaps section.
	if len(result.CoverageInsights) > 0 {
		sb.WriteString("Coverage gaps: ")
		sb.WriteString(strings.Join(result.CoverageInsights, "; "))
		sb.WriteString("\n\n")
	}

	// Noisy alerts section — includes missing features and reason.
	noiseAlerts := result.NoiseAlerts
	if len(noiseAlerts) > maxPromptNoise {
		noiseAlerts = noiseAlerts[:maxPromptNoise]
	}
	if len(noiseAlerts) > 0 {
		sb.WriteString("Noisy alerts:\n")
		for _, na := range noiseAlerts {
			line := fmt.Sprintf("- %s", na.Name)
			if len(na.MissingFeatures) > 0 {
				line += fmt.Sprintf(": no %s", strings.Join(na.MissingFeatures, ", no "))
			}
			if na.Reason != "" {
				line += fmt.Sprintf(" — %s", na.Reason)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`Return JSON only — no prose, no markdown:
{"summary":"<2-3 sentences>","top_priority":["<3-5 items>"],"strengths":["<2-3 items>"],"recommendations":["<3-5 items>"],"enriched_dups":["<1 sentence each>"],"enriched_gaps":["<1 sentence each>"],"noise_explanations":["<1 sentence each>"]}`)

	return sb.String()
}
```

- [ ] **Step 4: Run all insights tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/... -v 2>&1 | grep -E "^(--- PASS|--- FAIL|FAIL|ok)"
```

Expected: all pass, including `TestBuildPrompt_includesNoiseReason`.

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/insights/enrich.go backend/internal/insights/enrich_test.go
git commit -m "feat(insights): structured compact prompt with noise reasons and reduced caps"
```

---

### Task 7: HandleInsights — model parameter and ir.Model population

**Files:**
- Modify: `backend/internal/api/handlers.go`

`HandleInsights` already exists at line 353. This task adds an optional `model` field to the request body, routes to the correct NIM model, bypasses cache when a model is explicitly chosen (allows re-generating with a different model), and sets `ir.Model` to a human-readable name.

- [ ] **Step 1: Update `HandleInsights` in `handlers.go`**

Find `HandleInsights` starting at line 353. Locate the `var req models.ClientAnalyzeRequest` decode block and replace it with an inline struct:

Find this section:
```go
	var req models.ClientAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
```
Replace with:
```go
	var req struct {
		Client string `json:"client"`
		Model  string `json:"model"` // "mistral" | "gemma" | "" (default = mistral)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	req.Model = strings.TrimSpace(strings.ToLower(req.Model))
	if req.Model != "" && req.Model != "mistral" && req.Model != "gemma" {
		writeError(w, http.StatusBadRequest, "unknown insights model: use \"mistral\" or \"gemma\"")
		return
	}
```

Next, find the cache check block:
```go
	// Check insights cache.
	var insightsCacheKey string
	if h.cache != nil {
		if key, err := computeInsightsCacheKey(req.Client, alertInsights); err == nil {
			insightsCacheKey = key
			if cached, ok := h.cache.GetString(ctx, key); ok {
				var ir models.InsightsReport
				if json.Unmarshal([]byte(cached), &ir) == nil {
					log.Printf("INFO [insights] cache HIT client=%s", req.Client)
					writeJSON(w, http.StatusOK, &ir)
					return
				}
			}
		}
	}
```
Replace with:
```go
	// Check insights cache — skip when model is explicitly specified so the user
	// can switch models without being served a stale cached response.
	var insightsCacheKey string
	if h.cache != nil && req.Model == "" {
		if key, err := computeInsightsCacheKey(req.Client, alertInsights); err == nil {
			insightsCacheKey = key
			if cached, ok := h.cache.GetString(ctx, key); ok {
				var ir models.InsightsReport
				if json.Unmarshal([]byte(cached), &ir) == nil {
					log.Printf("INFO [insights] cache HIT client=%s", req.Client)
					writeJSON(w, http.StatusOK, &ir)
					return
				}
			}
		}
	}
```

Next, find the provider creation block:
```go
	// Cache miss — run LLM enrichment.
	insightsProviderName := h.config.LLM.InsightsProvider
	if insightsProviderName == "" {
		insightsProviderName = h.config.LLM.SuggestionProvider
	}
	insightsModel := h.config.LLM.InsightsModel
	if insightsModel == "" {
		insightsModel = h.config.LLM.NvidiaModel
	}
	// Insights uses the primary NVIDIA key (not the suggestion-specific key).
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	insightsProvider, err := llm.NewClassifierProvider(
		insightsProviderName,
		"",
		llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     insightsModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		},
	)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("insights provider unavailable: %v", err))
		return
	}
```
Replace with:
```go
	// Resolve model: explicit request overrides config default.
	insightsProviderName := h.config.LLM.InsightsProvider
	if insightsProviderName == "" {
		insightsProviderName = h.config.LLM.SuggestionProvider
	}
	insightsModel := h.config.LLM.InsightsModel
	if insightsModel == "" {
		insightsModel = h.config.LLM.NvidiaModel
	}
	modelLabel := "Mistral Small" // human-readable name shown in UI
	if req.Model == "gemma" {
		insightsModel = "google/gemma-3-27b-it"
		modelLabel = "Gemma 3 27B"
	}
	// Insights uses the primary NVIDIA key (not the suggestion-specific key).
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	insightsProvider, err := llm.NewClassifierProvider(
		insightsProviderName,
		"",
		llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     insightsModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		},
	)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("insights provider unavailable: %v", err))
		return
	}
```

Finally, find the line after successful `insights.Enrich`:
```go
	ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, insightsProvider)
	if enrichErr != nil {
		log.Printf("WARN [insights] enrich client=%s: %v", req.Client, enrichErr)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("insights enrichment failed: %v", enrichErr))
		return
	}
	if ir == nil {
		writeError(w, http.StatusNoContent, "no insights generated")
		return
	}
```
Replace with:
```go
	ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, insightsProvider)
	if enrichErr != nil {
		log.Printf("WARN [insights] enrich client=%s: %v", req.Client, enrichErr)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("insights enrichment failed: %v", enrichErr))
		return
	}
	if ir == nil {
		writeError(w, http.StatusNoContent, "no insights generated")
		return
	}
	ir.Model = modelLabel
```

- [ ] **Step 2: Build to confirm no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/api/handlers.go
git commit -m "feat(api): HandleInsights accepts optional model param, sets ir.Model label"
```

---

### Task 8: Switch default insights model in clients.yaml

**Files:**
- Modify: `backend/clients.yaml`

- [ ] **Step 1: Update `insights_model`**

In `backend/clients.yaml`, find:
```yaml
  insights_model: "z-ai/glm5"
```
Replace with:
```yaml
  insights_model: "mistralai/mistral-small-4-119b-2603"
```

- [ ] **Step 2: Build and run backend smoke test**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./... && echo "Build OK"
```

Expected: `Build OK`.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/clients.yaml
git commit -m "fix(config): switch insights_model from broken z-ai/glm5 to mistral-small"
```

---

### Task 9: Frontend — type updates, model selector, Regenerate button

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/AlertInsights.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Update TypeScript types**

In `frontend/src/types/index.ts`:

Find `NoiseAlert`:
```typescript
export interface NoiseAlert {
  name: string;
  missing_features: string[];
}
```
Replace with:
```typescript
export interface NoiseAlert {
  name: string;
  missing_features: string[];
  reason?: string;
}
```

Find `InsightsReport`:
```typescript
export interface InsightsReport {
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  enriched_gaps: string[];
  noise_explanations?: string[];
}
```
Replace with:
```typescript
export interface InsightsReport {
  model?: string;
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  enriched_gaps: string[];
  noise_explanations?: string[];
}
```

- [ ] **Step 2: Update `fetchInsights` in `api.ts` to accept optional model**

In `frontend/src/services/api.ts`, find:
```typescript
export async function fetchInsights(client: string): Promise<InsightsReport> {
  const res = await fetch(`${API_BASE}/api/insights`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Insights failed' }));
    throw new Error(err.error || 'Failed to fetch insights');
  }
  return res.json();
}
```
Replace with:
```typescript
export async function fetchInsights(client: string, model?: string): Promise<InsightsReport> {
  const body: Record<string, string> = { client };
  if (model) body.model = model;
  const res = await fetch(`${API_BASE}/api/insights`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Insights failed' }));
    throw new Error(err.error || 'Failed to fetch insights');
  }
  return res.json();
}
```

- [ ] **Step 3: Pass `client` prop to `AlertInsights` in `App.tsx`**

In `frontend/src/App.tsx`, find:
```tsx
        {view === 'insights' && data && (
          <AlertInsights data={data.alert_insights} report={insightsReport} insightsError={insightsError} />
        )}
```
Replace with:
```tsx
        {view === 'insights' && data && (
          <AlertInsights
            data={data.alert_insights}
            report={insightsReport}
            insightsError={insightsError}
            client={clientName}
          />
        )}
```

- [ ] **Step 4: Rewrite `AlertInsights.tsx` — add model badge, dropdown, Regenerate button, and noise reason**

In `frontend/src/components/AlertInsights.tsx`, replace the entire file with the following. This preserves all existing tab views and adds the model selector in the left panel header.

Find the current import and Props block at the very top (lines 1–11):
```tsx
import { useState } from 'react';
import type { SimilarityResult, InsightsReport, NoiseAlert } from '../types';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
}
```
Replace with:
```tsx
import { useState } from 'react';
import type { SimilarityResult, InsightsReport, NoiseAlert } from '../types';
import { fetchInsights } from '../services/api';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
  client: string;
}
```

Find the component function signature and state declarations (lines 12–14):
```tsx
export default function AlertInsights({ data, report, insightsError = false }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('duplicates');
```
Replace with:
```tsx
export default function AlertInsights({ data, report, insightsError = false, client }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('duplicates');
  const [localReport, setLocalReport] = useState<InsightsReport | null>(report);
  const [selectedModel, setSelectedModel] = useState<'mistral' | 'gemma'>('mistral');
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [regenError, setRegenError] = useState(false);

  // Sync localReport when the initial report prop loads.
  // (parent resolves this async; we start with null then it becomes non-null)
  if (report !== null && localReport === null && !isRegenerating) {
    setLocalReport(report);
  }

  const effectiveReport = localReport;

  const handleRegenerate = async () => {
    setIsRegenerating(true);
    setRegenError(false);
    setModelDropdownOpen(false);
    try {
      const newReport = await fetchInsights(client, selectedModel);
      setLocalReport(newReport);
    } catch (e) {
      console.warn('[insights regen]', e);
      setRegenError(true);
    } finally {
      setIsRegenerating(false);
    }
  };
```

Now find the left panel header section. Find the `<div className="insights-panel-summary">` block and add the model badge before it. Find:
```tsx
        <div className="insights-panel">

          <div className="insights-panel-summary">
```
Replace with:
```tsx
        <div className="insights-panel">

          {/* Model badge + dropdown */}
          <div className="insights-model-header">
            <button
              className="insights-model-badge"
              onClick={() => setModelDropdownOpen((o) => !o)}
              title="Click to change model"
            >
              {isRegenerating
                ? 'Regenerating…'
                : effectiveReport?.model
                  ? `Generated by: ${effectiveReport.model} ▾`
                  : 'Generated by: Mistral Small ▾'}
            </button>
            {modelDropdownOpen && (
              <div className="insights-model-dropdown">
                <button
                  className={`insights-model-option${selectedModel === 'mistral' ? ' active' : ''}`}
                  onClick={() => { setSelectedModel('mistral'); setModelDropdownOpen(false); }}
                >
                  Mistral Small (fast, ~7s)
                </button>
                <button
                  className={`insights-model-option${selectedModel === 'gemma' ? ' active' : ''}`}
                  onClick={() => { setSelectedModel('gemma'); setModelDropdownOpen(false); }}
                >
                  Gemma 3 27B (detailed, ~14s)
                </button>
              </div>
            )}
            <button
              className="insights-regenerate-btn"
              onClick={handleRegenerate}
              disabled={isRegenerating || !client}
              title="Regenerate insights with selected model"
            >
              {isRegenerating ? '…' : '↺'}
            </button>
          </div>

          <div className="insights-panel-summary">
```

Now update every reference to `report` in the JSX to use `effectiveReport`. There are multiple occurrences inside the panel — use bulk replace. The component renders `report` in:
- `<div className="insights-panel-summary">` — check for `report ?`
- `<div className="insights-panel-section">` — `report.top_priority`, `report.strengths`, `report.recommendations`
- The tab content: `<DuplicatesView data={data} report={report} />` etc.

Find and replace all occurrences in the JSX (not the import or state declarations):
- `report ?` → `effectiveReport ?`
- `report.summary` → `effectiveReport.summary`
- `report.top_priority` → `effectiveReport.top_priority`
- `report.strengths` → `effectiveReport.strengths`
- `report.recommendations` → `effectiveReport.recommendations`
- In tab rendering: `report={report}` → `report={effectiveReport}`

The specific lines to find in the left panel body:
```tsx
            {report ? (
              report.summary || 'Enrichment unavailable — check LLM provider configuration.'
```
Replace with:
```tsx
            {isRegenerating ? (
              <>
                <div className="insights-skeleton" style={{ width: '100%' }} />
                <div className="insights-skeleton" style={{ width: '80%' }} />
              </>
            ) : effectiveReport ? (
              effectiveReport.summary || 'Enrichment unavailable — check LLM provider configuration.'
```

Find `} : insightsError ? (` that follows the summary block and keep the rest as-is but replace `insightsError` with `(insightsError || regenError)` throughout the component.

Also update the tab content renders — find:
```tsx
            {activeTab === 'duplicates'      && <DuplicatesView      data={data} report={report} />}
            {activeTab === 'families'        && <FamiliesView        data={data} />}
            {activeTab === 'merge'           && <MergeView           data={data} />}
            {activeTab === 'coverage'        && <CoverageView        data={data} report={report} />}
            {activeTab === 'noise'           && <NoiseView           data={data} report={report} />}
            {activeTab === 'unique'          && <UniqueView          data={data} />}
            {activeTab === 'recommendations' && <RecommendationsView report={report} />}
```
Replace with:
```tsx
            {activeTab === 'duplicates'      && <DuplicatesView      data={data} report={effectiveReport} />}
            {activeTab === 'families'        && <FamiliesView        data={data} />}
            {activeTab === 'merge'           && <MergeView           data={data} />}
            {activeTab === 'coverage'        && <CoverageView        data={data} report={effectiveReport} />}
            {activeTab === 'noise'           && <NoiseView           data={data} report={effectiveReport} />}
            {activeTab === 'unique'          && <UniqueView          data={data} />}
            {activeTab === 'recommendations' && <RecommendationsView report={effectiveReport} />}
```

And update the `recsCount` line near the top of the component:
```tsx
  const recsCount  = report?.recommendations?.length ?? 0;
```
Replace with:
```tsx
  const recsCount  = effectiveReport?.recommendations?.length ?? 0;
```

- [ ] **Step 5: Update `NoiseView` to display the `reason` field**

Find `NoiseView` (around line 277):
```tsx
            {noise.missing_features?.length > 0 && (
              <div className="noise-missing">
                <span className="noise-missing-label">Missing features:</span>{' '}
                {noise.missing_features.join(', ')}
              </div>
            )}

            <div className="card-explanation">
              {enrichedExplanation ??
                'Sparse feature vector — likely a threshold-only rule. Review for contextual conditions.'}
            </div>
```
Replace with:
```tsx
            {noise.missing_features?.length > 0 && (
              <div className="noise-missing">
                <span className="noise-missing-label">Missing features:</span>{' '}
                {noise.missing_features.join(', ')}
              </div>
            )}

            {noise.reason && (
              <div className="noise-reason">{noise.reason}</div>
            )}

            <div className="card-explanation">
              {enrichedExplanation ??
                'Sparse feature vector — likely a threshold-only rule. Review for contextual conditions.'}
            </div>
```

- [ ] **Step 6: Build the frontend to verify TypeScript compiles cleanly**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run build 2>&1 | tail -20
```

Expected: `✓ built in` with zero TypeScript errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/types/index.ts frontend/src/services/api.ts frontend/src/components/AlertInsights.tsx frontend/src/App.tsx
git commit -m "feat(ui): insights model badge, dropdown, Regenerate button, and noise reason display"
```

---

## Post-implementation smoke tests

Manual verification after all tasks are complete:

1. Run analysis for a Salesforce client → verify `GuestUserAnomalyEvent` and `ApiAnomalyEvent` are **not** in the duplicates list
2. Check family names contain semantic labels ("Tampering", "Privilege Escalation") not raw tokens like "Remove Detections"
3. Check noisy alerts have a non-empty `reason` field in the API response (`/api/insights`)
4. Insights panel loads in ~7s with "Generated by: Mistral Small" badge
5. Click the badge → dropdown shows "Mistral Small (fast, ~7s)" and "Gemma 3 27B (detailed, ~14s)"
6. Select Gemma 3 27B, click Regenerate → badge updates to "Generated by: Gemma 3 27B", insights replace in ~14s
7. Call `/api/insights` with `{ "client": "X" }` before running analysis → verify 204 (no insights) or cached result
8. Run backend tests: `go test ./...` from `backend/` — all pass
9. Run frontend build: `cd frontend && npm run build` — zero TypeScript errors
