# Group-By Pivot Similarity — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `group_by_keys` as a weighted similarity dimension so alerts that pivot on different fields (e.g. per-account vs per-source-IP) are not falsely flagged as duplicates.

**Architecture:** 3 tasks — new `pivot_categories.go` file with normalization logic (TDD), then engine integration wiring the new dimension into `featureVector` and `scorePair`, then regression validation against real alert data. No changes outside `backend/internal/similarity/`.

**Tech Stack:** Go 1.22; no new dependencies.

> **Note on spec threshold change:** The design spec proposed raising `duplicateThreshold` from 0.85 → 0.88 based on an assumed max score of 1.10. The actual current weights sum to max 1.00. With the new weights below, the Okta pair scores ≈0.77 — already below 0.85 — so the threshold stays **unchanged**.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/similarity/pivot_categories.go` | New — `pivotCategoryMap`, `normalizeGroupByKeys()`, `jaccardGroupBy()` |
| `backend/internal/similarity/pivot_categories_test.go` | New — unit tests for normalization and jaccardGroupBy |
| `backend/internal/similarity/engine.go` | Add `groupByCategories` to `featureVector`; update weight constants; update `buildFeatureVectors`; add `jaccardGroupBy` call in `scorePair` |
| `backend/internal/similarity/engine_test.go` | Add score impact + regression tests |

---

## Task 1: Pivot Categories — `normalizeGroupByKeys` and `jaccardGroupBy` (TDD)

**Files:**
- Create: `backend/internal/similarity/pivot_categories_test.go`
- Create: `backend/internal/similarity/pivot_categories.go`

### Step 1: Write failing tests

Create `backend/internal/similarity/pivot_categories_test.go`:

```go
package similarity

import (
	"testing"
)

func TestNormalizeGroupByKeys_knownPaths(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"okta.actor.alternateId", "user"},
		{"userIdentity.arn", "user"},
		{"actor.email", "user"},
		{"USER_NAME", "user"},
		{"ClientIP", "ip"},
		{"cx_security.source_ip", "ip"},
		{"okta.client.ipAddress", "ip"},
		{"event.Hostname", "hostname"},
		{"event.ComputerName", "hostname"},
		{"requestParameters.bucketName", "resource"},
		{"requestParameters.instanceId", "resource"},
		{"instance-id", "resource"},
		{"userIdentity.accountId", "account"},
		{"awsRegion", "account"},
		{"event.DetectId", "detection"},
		{"sourceRule.name", "detection"},
	}
	for _, tc := range cases {
		got := normalizeGroupByKeys([]string{tc.input})
		if _, ok := got[tc.expected]; !ok {
			t.Errorf("normalizeGroupByKeys(%q): expected category %q, got %v", tc.input, tc.expected, got)
		}
		if len(got) != 1 {
			t.Errorf("normalizeGroupByKeys(%q): expected 1 category, got %d: %v", tc.input, len(got), got)
		}
	}
}

func TestNormalizeGroupByKeys_unknownPath(t *testing.T) {
	got := normalizeGroupByKeys([]string{"some.unknown.field"})
	if _, ok := got["some.unknown.field"]; !ok {
		t.Errorf("expected raw path as category for unknown key, got %v", got)
	}
}

func TestNormalizeGroupByKeys_empty(t *testing.T) {
	if got := normalizeGroupByKeys(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := normalizeGroupByKeys([]string{}); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestNormalizeGroupByKeys_mixed(t *testing.T) {
	got := normalizeGroupByKeys([]string{"okta.actor.alternateId", "some.custom.field"})
	if _, ok := got["user"]; !ok {
		t.Errorf("expected 'user' category, got %v", got)
	}
	if _, ok := got["some.custom.field"]; !ok {
		t.Errorf("expected raw path 'some.custom.field', got %v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 categories, got %d: %v", len(got), got)
	}
}

func TestNormalizeGroupByKeys_deduplicatesCategories(t *testing.T) {
	// two different "user" paths → one "user" category
	got := normalizeGroupByKeys([]string{"okta.actor.alternateId", "actor.email"})
	if len(got) != 1 {
		t.Errorf("expected 1 deduplicated category, got %d: %v", len(got), got)
	}
	if _, ok := got["user"]; !ok {
		t.Errorf("expected 'user' category, got %v", got)
	}
}

func TestJaccardGroupBy_bothEmpty(t *testing.T) {
	if score := jaccardGroupBy(nil, nil); score != 1.0 {
		t.Errorf("expected 1.0 for both empty (compatible), got %f", score)
	}
}

func TestJaccardGroupBy_oneEmpty(t *testing.T) {
	a := map[string]struct{}{"user": {}}
	if score := jaccardGroupBy(a, nil); score != 0.0 {
		t.Errorf("expected 0.0 when one side empty, got %f", score)
	}
	if score := jaccardGroupBy(nil, a); score != 0.0 {
		t.Errorf("expected 0.0 when one side empty, got %f", score)
	}
}

func TestJaccardGroupBy_sameCategory(t *testing.T) {
	a := map[string]struct{}{"user": {}}
	b := map[string]struct{}{"user": {}}
	if score := jaccardGroupBy(a, b); score != 1.0 {
		t.Errorf("expected 1.0 for same category, got %f", score)
	}
}

func TestJaccardGroupBy_differentCategory(t *testing.T) {
	a := map[string]struct{}{"user": {}}
	b := map[string]struct{}{"ip": {}}
	if score := jaccardGroupBy(a, b); score != 0.0 {
		t.Errorf("expected 0.0 for different categories, got %f", score)
	}
}
```

### Step 2: Run tests to verify they fail

```bash
cd backend && go test ./internal/similarity/... -run TestNormalize -v
cd backend && go test ./internal/similarity/... -run TestJaccardGroupBy -v
```

Expected: build error — `normalizeGroupByKeys` and `jaccardGroupBy` undefined.

### Step 3: Implement `pivot_categories.go`

Create `backend/internal/similarity/pivot_categories.go`:

```go
package similarity

import "strings"

// pivotCategoryMap maps lowercase group_by field paths to semantic pivot categories.
// Unknown paths are used as their own category (exact-path matching still works).
var pivotCategoryMap = map[string]string{
	// user identity
	"useridentity.arn":                                    "user",
	"useridentity.principalid":                            "user",
	"useridentity.sessioncontext.sessionissuer.username":  "user",
	"actor.email":                                         "user",
	"actor.user.email":                                    "user",
	"cloudflare.actor.email":                              "user",
	"okta.actor.alternateid":                              "user",
	"cx_security.email":                                   "user",
	"cx_security.username":                                "user",
	"user.username":                                       "user",
	"actor_id":                                            "user",
	"actor":                                               "user",
	"user_name":                                           "user",
	"requestparameters.username":                          "user",
	"event.userid":                                        "user",
	"event.parameters.user_email":                        "user",
	"username":                                            "user",
	"arn_extracted":                                       "user",
	// source IP
	"clientip":                   "ip",
	"cx_security.source_ip":      "ip",
	"okta.client.ipaddress":      "ip",
	"remote_ip":                  "ip",
	"msg.extension.publicipv4":   "ip",
	// hostname / endpoint
	"event.hostname":     "hostname",
	"event.computername": "hostname",
	// cloud / infrastructure resource
	"requestparameters.bucketname":   "resource",
	"requestparameters.instanceid":   "resource",
	"requestparameters.domainname":   "resource",
	"requestparameters.rolearn":      "resource",
	"instance-id":                    "resource",
	// account / tenant
	"useridentity.accountid":                  "account",
	"coralogix.metadata.applicationname":      "account",
	"awsregion":                               "account",
	// detection / rule identifier
	"event.detectid":    "detection",
	"event.compositeid": "detection",
	"event.aggregateid": "detection",
	"sourcerule.name":   "detection",
	"detail.id":         "detection",
}

// normalizeGroupByKeys maps a slice of group_by field paths to a set of semantic
// pivot categories. Keys are lowercased before lookup. Unknown paths are kept
// verbatim as their own category so exact-path matches still count.
// Returns nil for empty/nil input.
func normalizeGroupByKeys(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		lower := strings.ToLower(strings.TrimSpace(k))
		if lower == "" {
			continue
		}
		if cat, ok := pivotCategoryMap[lower]; ok {
			out[cat] = struct{}{}
		} else {
			out[lower] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// jaccardGroupBy computes Jaccard similarity for pivot category sets.
// Unlike jaccard(), two empty sets return 1.0 — both unspecified means compatible.
func jaccardGroupBy(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	return jaccard(a, b)
}
```

### Step 4: Run tests to verify they pass

```bash
cd backend && go test ./internal/similarity/... -run TestNormalize -v
cd backend && go test ./internal/similarity/... -run TestJaccardGroupBy -v
```

Expected: all 9 tests PASS.

### Step 5: Build

```bash
cd backend && go build ./...
```

Expected: no errors.

### Step 6: Commit

```bash
git add backend/internal/similarity/pivot_categories.go backend/internal/similarity/pivot_categories_test.go
git commit -m "feat: add group_by pivot normalization and jaccardGroupBy"
```

---

## Task 2: Engine Integration — `featureVector`, weights, `scorePair`

**Files:**
- Modify: `backend/internal/similarity/engine.go`

### Step 1: Add `groupByCategories` to `featureVector`

In `backend/internal/similarity/engine.go`, find:

```go
type featureVector struct {
	alertID     string
	alertName   string
	alertType   string
	dataSources map[string]struct{}
	entities    map[string]struct{}
	actions     map[string]struct{}
	conditions  map[string]struct{}
	techniques  map[string]struct{}
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
}
```

### Step 2: Update weight constants

In `backend/internal/similarity/engine.go`, find:

```go
const (
	weightDataSources = 0.20
	weightEntities    = 0.15
	weightActions     = 0.20
	weightConditions  = 0.20
	weightTechniques  = 0.15
	weightAlertType   = 0.10

	duplicateThreshold = 0.85
	familyThreshold    = 0.60
	mergeAvgThreshold  = 0.70
	uniqueThreshold    = 0.30

	// When the number of alerts exceeds this value, pairwise comparison is
	// parallelised across a worker pool.
	parallelThreshold = 50
)
```

Replace with:

```go
const (
	// Existing dimensions scaled ×0.75 to make room for weightGroupBy.
	weightDataSources = 0.15
	weightEntities    = 0.11
	weightActions     = 0.15
	weightConditions  = 0.15
	weightTechniques  = 0.11
	weightGroupBy     = 0.25
	weightAlertType   = 0.10

	duplicateThreshold = 0.85
	familyThreshold    = 0.60
	mergeAvgThreshold  = 0.70
	uniqueThreshold    = 0.30

	// When the number of alerts exceeds this value, pairwise comparison is
	// parallelised across a worker pool.
	parallelThreshold = 50
)
```

### Step 3: Populate `groupByCategories` in `buildFeatureVectors`

In `backend/internal/similarity/engine.go`, find:

```go
		vectors[i] = featureVector{
			alertID:     a.ID,
			alertName:   a.Name,
			alertType:   strings.ToLower(a.AlertType),
			dataSources: toSet(a.Features.DataSources),
			entities:    toSet(a.Features.Entities),
			actions:     toSet(a.Features.Actions),
			conditions:  toSet(a.Features.Conditions),
			techniques:  toSet(a.Features.Techniques),
		}
```

Replace with:

```go
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
```

### Step 4: Add `jaccardGroupBy` call in `scorePair`

In `backend/internal/similarity/engine.go`, find:

```go
func scorePair(a, b featureVector) float64 {
	score := 0.0
	score += weightDataSources * jaccard(a.dataSources, b.dataSources)
	score += weightEntities * jaccard(a.entities, b.entities)
	score += weightActions * jaccard(a.actions, b.actions)
	score += weightConditions * jaccard(a.conditions, b.conditions)
	score += weightTechniques * jaccard(a.techniques, b.techniques)

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

	if a.alertType == b.alertType && a.alertType != "" {
		score += weightAlertType
	}

	return score
}
```

### Step 5: Build

```bash
cd backend && go build ./...
```

Expected: no errors.

### Step 6: Run all existing tests

```bash
cd backend && go test ./internal/similarity/... -v
```

Expected: all existing `TestFindNoiseAlerts_*` tests still PASS. The new struct field is nil (zero value) in those test vectors, which is valid.

### Step 7: Commit

```bash
git add backend/internal/similarity/engine.go
git commit -m "feat: add groupByCategories to featureVector and weight redistribution"
```

---

## Task 3: Score Impact Tests + Regression

**Files:**
- Modify: `backend/internal/similarity/engine_test.go`

### Step 1: Add score impact tests

Append to `backend/internal/similarity/engine_test.go`:

```go
import (
	"encoding/json"
	"os"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)
```

Wait — the existing file only imports `"testing"`. Add the new imports and tests by appending to the file. The full addition (append after the last test):

```go
func TestScorePair_oktaPairIsNotDuplicate(t *testing.T) {
	forAccount := featureVector{
		alertName: "Okta - Multiple Login Failure For an Account",
		alertType: "logs_threshold",
		dataSources: map[string]struct{}{"okta": {}},
		entities:    map[string]struct{}{"account": {}, "session": {}, "user": {}, "ip_address": {}},
		actions:     map[string]struct{}{"login": {}, "enable": {}, "access": {}},
		conditions:  map[string]struct{}{"brute_force": {}, "failure": {}, "multiple": {}, "threshold": {}},
		techniques:  map[string]struct{}{"t1110": {}},
		groupByCategories: normalizeGroupByKeys([]string{"okta.actor.alternateId"}),
	}
	fromSource := featureVector{
		alertName: "Okta - Multiple Login Failure From a Source",
		alertType: "logs_threshold",
		dataSources: map[string]struct{}{"okta": {}},
		entities:    map[string]struct{}{"user": {}, "ip_address": {}, "account": {}, "session": {}},
		actions:     map[string]struct{}{"login": {}, "enable": {}, "access": {}},
		conditions:  map[string]struct{}{"brute_force": {}, "failure": {}, "multiple": {}, "threshold": {}},
		techniques:  map[string]struct{}{"t1110": {}},
		groupByCategories: normalizeGroupByKeys([]string{"okta.client.ipAddress"}),
	}
	score := scorePair(forAccount, fromSource)
	if score >= duplicateThreshold {
		t.Errorf("Okta pair should NOT be duplicates: score=%.4f >= threshold=%.2f", score, duplicateThreshold)
	}
}

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

### Step 2: Run score impact tests to verify they pass

```bash
cd backend && go test ./internal/similarity/... -run TestScorePair -v
```

Expected: all 3 new tests PASS.

### Step 3: Add regression test against debug_alerts.json

Append to `backend/internal/similarity/engine_test.go` (after the score impact tests above):

```go
func TestAnalyze_oktaPairIsNotDuplicate(t *testing.T) {
	data, err := os.ReadFile("../../debug_alerts.json")
	if err != nil {
		t.Skip("debug_alerts.json not available")
	}
	var alerts []*models.AlertDef
	if err := json.Unmarshal(data, &alerts); err != nil {
		t.Fatalf("failed to parse debug_alerts.json: %v", err)
	}
	result := Analyze(alerts)
	for _, dup := range result.Duplicates {
		hasAccount, hasSource := false, false
		for _, n := range dup.AlertNames {
			if n == "Okta - Multiple Login Failure For an Account" {
				hasAccount = true
			}
			if n == "Okta - Multiple Login Failure From a Source" {
				hasSource = true
			}
		}
		if hasAccount && hasSource {
			t.Errorf("Okta pair should NOT be a duplicate after group_by fix (similarity=%.4f)", dup.Similarity)
		}
	}
	if len(result.Duplicates) == 0 {
		t.Error("expected at least some duplicates in the dataset (sanity check)")
	}
}
```

### Step 4: Update imports in `engine_test.go`

The regression test needs `encoding/json`, `os`, and `coralogix-alert-analyzer/internal/models`. Update the import block at the top of `engine_test.go`:

Find:
```go
import (
	"testing"
)
```

Replace with:
```go
import (
	"encoding/json"
	"os"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)
```

### Step 5: Run all tests

```bash
cd backend && go test ./internal/similarity/... -v
```

Expected:
- All 4 `TestFindNoiseAlerts_*` tests PASS
- All 9 `TestNormalize*` and `TestJaccardGroupBy*` tests PASS
- All 3 `TestScorePair_*` tests PASS
- `TestAnalyze_oktaPairIsNotDuplicate` PASS (or SKIP if file absent)

### Step 6: Commit

```bash
git add backend/internal/similarity/engine_test.go
git commit -m "test: add score impact and regression tests for group_by pivot similarity"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| `pivot_categories.go` with `normalizeGroupByKeys()` | Task 1 Step 3 |
| `jaccardGroupBy()` with empty+empty=1.0 | Task 1 Step 3 |
| 6 pivot categories (user, ip, hostname, resource, account, detection) | Task 1 Step 3 |
| Unknown path → raw path as category | Task 1 Step 3 |
| `groupByCategories` field on `featureVector` | Task 2 Step 1 |
| Weight redistribution (×0.75 scale + `weightGroupBy=0.25`) | Task 2 Step 2 |
| `buildFeatureVectors` calls `normalizeGroupByKeys` | Task 2 Step 3 |
| `scorePair` adds `weightGroupBy * jaccardGroupBy(...)` | Task 2 Step 4 |
| Normalization unit tests | Task 1 Step 1 |
| Score impact: Okta pair below threshold | Task 3 Step 1 |
| Score impact: identical+same pivot still duplicate | Task 3 Step 1 |
| Score impact: identical+no pivot still duplicate | Task 3 Step 1 |
| Regression: Okta pair not in duplicates on real data | Task 3 Step 3 |

All spec requirements covered.
