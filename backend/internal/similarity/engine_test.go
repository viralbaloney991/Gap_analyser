package similarity

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestFindNoiseAlerts_nilInput(t *testing.T) {
	noisy := findNoiseAlerts(nil, nil, nil, 0, idfTable{}, 0)
	if noisy != nil {
		t.Errorf("expected nil for nil input, got %v", noisy)
	}
}

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
	score := scorePair(forAccount, fromSource, idfTable{})
	if score >= duplicateThreshold {
		t.Errorf("Okta pair should NOT be duplicates: score=%.4f >= threshold=%.2f", score, duplicateThreshold)
	}
}

func TestScorePair_identicalAlertSamePivotIsDuplicate(t *testing.T) {
	a := featureVector{
		alertName: "Duplicate Alert Alpha",
		alertType: "logs_threshold",
		dataSources: map[string]struct{}{"okta": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"login": {}},
		conditions:  map[string]struct{}{"failure": {}},
		techniques:  map[string]struct{}{"t1110": {}},
		groupByCategories: normalizeGroupByKeys([]string{"okta.actor.alternateId"}),
		luceneQuery: map[string]struct{}{"eventtype": {}, "okta": {}, "login": {}},
		nameTokens:  tokenizeAlertName("Duplicate Alert Alpha"),
		timeWindow:  "5m",
	}
	b := a
	b.alertName = "Duplicate Alert Beta"
	b.nameTokens = tokenizeAlertName("Duplicate Alert Beta")
	score := scorePair(a, b, idfTable{})
	if score < duplicateThreshold {
		t.Errorf("identical alert with same pivot should be duplicate: score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}

func TestScorePair_identicalAlertNoPivotIsDuplicate(t *testing.T) {
	// Two identical alerts with no groupBy must still reach the duplicate threshold.
	// The Lucene query + nameTokens + other dims together carry enough weight.
	a := featureVector{
		alertName:   "AWS AssumeRole Cross Account",
		alertType:   "logs_threshold",
		dataSources: map[string]struct{}{"aws": {}},
		entities:    map[string]struct{}{"role": {}},
		actions:     map[string]struct{}{"assumerole": {}},
		conditions:  map[string]struct{}{"cross_account": {}},
		techniques:  map[string]struct{}{"t1550": {}},
		luceneQuery: map[string]struct{}{"assumerole": {}, "cross_account": {}, "aws": {}},
		nameTokens:  tokenizeAlertName("AWS AssumeRole Cross Account"),
		timeWindow:  "5m",
		// groupByCategories intentionally nil on both sides
	}
	b := a
	score := scorePair(a, b, idfTable{})
	if score < duplicateThreshold {
		t.Errorf("identical alert with no pivot should still be duplicate: score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}

func TestAnalyze_oktaPairIsNotDuplicate(t *testing.T) {
	data, err := os.ReadFile("../../debug_alerts.json")
	if err != nil {
		t.Skip("debug_alerts.json not available")
	}
	var alerts []*models.AlertDef
	if err := json.Unmarshal(data, &alerts); err != nil {
		t.Fatalf("failed to parse debug_alerts.json: %v", err)
	}
	result := Analyze(alerts, nil, 0, nil)
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

func TestWeightsSumToOne(t *testing.T) {
	const sum = weightDataSources + weightEntities + weightActions +
		weightConditions + weightTechniques + weightGroupBy + weightAlertType +
		weightLuceneQuery + weightTimeWindow + weightNameTokens
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("similarity weights sum to %.10f, want exactly 1.0", sum)
	}
}

func TestScorePair_salesforcePairIsNotDuplicate(t *testing.T) {
	// GuestUserAnomalyEvent and ApiAnomalyEvent are distinct Salesforce event types.
	// Without Lucene query scoring, these score 100% similar. With it, they should
	// fall below the duplicate threshold because the event-type token differs.
	// groupByCategories differ (guest-user alerts pivot on user; API alerts pivot on API client).
	guestUser := featureVector{
		alertName:         "Salesforce - SFDC - Security Event - GuestUserAnomalyEvent",
		alertType:         "logs_threshold",
		dataSources:       map[string]struct{}{"salesforce": {}},
		entities:          map[string]struct{}{"user": {}},
		actions:           map[string]struct{}{"anomaly": {}},
		conditions:        map[string]struct{}{"security_event": {}},
		techniques:        map[string]struct{}{"t1078": {}},
		groupByCategories: normalizeGroupByKeys([]string{"salesforce.event.userId"}),
		luceneQuery:       tokenizeLucene("eventType:GuestUserAnomalyEvent AND coralogix.metadata.applicationName:salesforce"),
		timeWindow:        "5m",
	}
	apiEvent := featureVector{
		alertName:         "Salesforce - SFDC - Security Event - ApiAnomalyEvent",
		alertType:         "logs_threshold",
		dataSources:       map[string]struct{}{"salesforce": {}},
		entities:          map[string]struct{}{"user": {}},
		actions:           map[string]struct{}{"anomaly": {}},
		conditions:        map[string]struct{}{"security_event": {}},
		techniques:        map[string]struct{}{"t1078": {}},
		groupByCategories: normalizeGroupByKeys([]string{"salesforce.event.sourceIp"}),
		luceneQuery:       tokenizeLucene("eventType:ApiAnomalyEvent AND coralogix.metadata.applicationName:salesforce"),
		timeWindow:        "5m",
	}
	score := scorePair(guestUser, apiEvent, idfTable{})
	if score >= duplicateThreshold {
		t.Errorf("distinct Salesforce event types should NOT be duplicates: score=%.4f >= threshold=%.2f", score, duplicateThreshold)
	}
}

func TestTokenizeLucene_basic(t *testing.T) {
	tokens := tokenizeLucene("eventType:GuestUserAnomalyEvent AND coralogix.metadata.applicationName:salesforce")
	want := map[string]struct{}{
		"eventtype:guestuseranomalyevent":               {},
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
		"common": 0.1,  // appears in almost every document
		"rare_a": 10.0, // appears in 1 document
		"rare_b": 10.0, // appears in 1 document
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

func TestWeightedJaccard_disjointSetsReturnZero(t *testing.T) {
	// Completely disjoint sets share no tokens — intersection weight = 0 → score = 0.
	idf := map[string]float64{"a": 5.0, "b": 3.0}
	setA := map[string]struct{}{"a": {}}
	setB := map[string]struct{}{"b": {}}
	score := weightedJaccard(setA, setB, idf)
	if score != 0.0 {
		t.Errorf("disjoint sets: got %.6f, want 0.0", score)
	}
}

// ── New hybrid noise model tests ──────────────────────────────────────────

func makeAlert(id, alertType string, vendorCovered, isSecurityAlert bool, labels map[string]string, appName, subsystem string) *models.AlertDef {
	typeDef := map[string]any{}
	if appName != "" || subsystem != "" {
		typeDef["logsFilter"] = map[string]any{
			"simpleFilter": map[string]any{
				"labelFilters": map[string]any{
					"applicationName": []any{map[string]any{"value": appName, "operation": "IS"}},
					"subsystemName":   []any{map[string]any{"value": subsystem, "operation": "IS"}},
				},
			},
		}
	}
	if labels == nil {
		labels = map[string]string{}
	}
	bb := labels["flow_alert"] == "building block" || labels["flow_alert"] == "buildingblock"
	return &models.AlertDef{
		ID:        id,
		AlertType: alertType,
		Labels:    labels,
		TypeDef:   typeDef,
		Features: models.AlertFeatures{
			VendorCovered:   vendorCovered,
			IsSecurityAlert: isSecurityAlert,
			IsBuildingBlock: bb,
		},
	}
}

func sparseVector(name string) featureVector {
	return featureVector{
		alertName:   name,
		dataSources: map[string]struct{}{},
		entities:    map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
}

func TestFindNoiseAlerts_vendorCoveredExcluded(t *testing.T) {
	v := sparseVector("GCP SCC Alert")
	alert := makeAlert("gcp-1", "logs_threshold", true, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("vendor-covered alert should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_buildingBlockExcluded(t *testing.T) {
	v := sparseVector("BB Alert")
	alert := makeAlert("bb-1", "logs_threshold", false, true,
		map[string]string{"flow_alert": "building block"}, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("building block should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_nonSecurityExcluded(t *testing.T) {
	v := sparseVector("Ops Alert")
	alert := makeAlert("ops-1", "logs_threshold", false, false, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("non-security alert should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_structuralNoise_unscopedHighVolume(t *testing.T) {
	v := sparseVector("Generic Threshold")
	alert := makeAlert("t-1", "logs_threshold", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].NoiseType != "structural" {
		t.Errorf("noise_type: want structural, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_structuralNoise_scopedAlertNotNoisy(t *testing.T) {
	v := sparseVector("Scoped Alert")
	alert := makeAlert("t-2", "logs_threshold", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("scoped alert should not be structural noise, got %v", noisy)
	}
}

func TestFindNoiseAlerts_structuralNoise_logsAnomalyNowStructural(t *testing.T) {
	v := sparseVector("Anomaly Alert")
	alert := makeAlert("t-3", "logs_anomaly", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	// logs_anomaly is no longer excluded — unscoped + no entity = structural noise
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert for unscoped logs_anomaly, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "structural" {
		t.Errorf("noise_type: want structural, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_logsImmediate_structuralWhenUnscoped(t *testing.T) {
	v := sparseVector("Azure Audit - Access Review Deletion")
	alert := makeAlert("az-1", "logs_immediate", false, true, nil, "", "")

	// Event count map has data for a different ID — az-1 resolves to triggerCount=0.
	// After fix: structural fires regardless, because design is unscoped + no entity.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"other-id": 5}, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("logs_immediate unscoped should be structural even when triggerCount=0: got %v", noisy)
	}

	// 3 triggers, unscoped, no entity → structural noise.
	noisy = findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"az-1": 3}, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("logs_immediate with 3 triggers, unscoped should be structural: got %v", noisy)
	}

	// 25 triggers → both behavioral and structural.
	noisy = findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{"az-1": 25}, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "both" {
		t.Errorf("logs_immediate with 25 triggers, unscoped should be both: got %v", noisy)
	}
}

func TestFindNoiseAlerts_behavioralNoise_overThreshold(t *testing.T) {
	v := featureVector{
		alertName:   "Chatty Alert",
		dataSources: map[string]struct{}{"cloudtrail": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"login": {}},
		conditions:  map[string]struct{}{"failed": {}},
		techniques:  map[string]struct{}{"t1078": {}},
	}
	alert := makeAlert("b-1", "logs_threshold", false, true, nil, "my-app", "auth")
	counts := map[string]int{"b-1": 25}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 behavioral noise alert, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "behavioral" {
		t.Errorf("noise_type: want behavioral, got %q", noisy[0].NoiseType)
	}
	if noisy[0].TriggerCount != 25 {
		t.Errorf("trigger_count: want 25, got %d", noisy[0].TriggerCount)
	}
}

func TestFindNoiseAlerts_behavioralNoise_atThresholdNotNoisy(t *testing.T) {
	v := sparseVector("Borderline Alert")
	alert := makeAlert("b-2", "logs_threshold", false, true, nil, "my-app", "")
	counts := map[string]int{"b-2": 10}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	// 10 is not > 10, so not behavioral. Has app scoping and empty query, so not structural.
	if len(noisy) != 0 {
		t.Errorf("alert at threshold (10) should not be noisy, got %v", noisy)
	}
}

func TestFindNoiseAlerts_bothSignals(t *testing.T) {
	v := sparseVector("Double Trouble")
	alert := makeAlert("bt-1", "logs_threshold", false, true, nil, "", "")
	counts := map[string]int{"bt-1": 30}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 5, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noise alert, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "both" {
		t.Errorf("noise_type: want both, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_flowAlert_behavioralApplies(t *testing.T) {
	v := sparseVector("Flow Alert")
	// Scoped alert so structural does not apply — only behavioral signal fires.
	alert := makeAlert("f-1", "flow", false, true, nil, "my-app", "auth")
	counts := map[string]int{"f-1": 25}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("flow alert with high count should be behavioral noise, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "behavioral" {
		t.Errorf("noise_type: want behavioral, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_flowAlert_structuralAppliesWhenUnscoped(t *testing.T) {
	v := sparseVector("Flow No Triggers")
	alert := makeAlert("f-2", "flow", false, true, nil, "", "")
	// Unscoped flow with no entity → structural noise regardless of event counts.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("unscoped flow alert should be structural noise: got %v", noisy)
	}
}

// TestFindNoiseAlerts_structuralNoise_emptyEventCountsMap covers the production bug:
// fetchEventCounts returns a non-nil empty map when the API call succeeds but returns
// no matching events. Under the old code hasEvidenceOfVolume=false blocked structural
// detection. After the fix, structural fires on design alone.
func TestFindNoiseAlerts_structuralNoise_emptyEventCountsMap(t *testing.T) {
	v := sparseVector("Unscoped Alert")
	alert := makeAlert("u-1", "logs_threshold", false, true, nil, "", "")
	// Non-nil empty map — the exact shape returned when the fetch succeeds but matches nothing.
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{}, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert with empty event count map, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].NoiseType != "structural" {
		t.Errorf("noise_type: want structural, got %q", noisy[0].NoiseType)
	}
}

// TestFindNoiseAlerts_structuralNoise_scopedAlertWithEmptyMap verifies that removing
// hasEvidenceOfVolume does not cause false positives: a scoped alert with an empty
// event count map must still not be flagged as noisy.
func TestFindNoiseAlerts_structuralNoise_scopedAlertWithEmptyMap(t *testing.T) {
	v := sparseVector("Scoped Alert")
	alert := makeAlert("s-1", "logs_threshold", false, true, nil, "my-app", "auth")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert},
		map[string]int{}, 0, idfTable{}, 0)
	if len(noisy) != 0 {
		t.Errorf("scoped alert with empty event count map should not be noisy, got %v", noisy)
	}
}

func TestFindNoiseAlerts_orgAmplifier_reasonContainsCount(t *testing.T) {
	v := sparseVector("Generic")
	alert := makeAlert("amp-1", "logs_threshold", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 15, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert, got %d", len(noisy))
	}
	if !strings.Contains(noisy[0].Reason, "15") {
		t.Errorf("reason should mention integration count 15, got: %q", noisy[0].Reason)
	}
}

func TestFindNoiseAlerts_sortedByName(t *testing.T) {
	// Two structural noise alerts — result must be alphabetically sorted.
	vectors := []featureVector{sparseVector("Zebra Alert"), sparseVector("Alpha Alert")}
	alerts := []*models.AlertDef{
		makeAlert("z-1", "logs_threshold", false, true, nil, "", ""),
		makeAlert("a-1", "logs_threshold", false, true, nil, "", ""),
	}
	noisy := findNoiseAlerts(vectors, alerts, nil, 0, idfTable{}, 0)
	if len(noisy) != 2 {
		t.Fatalf("expected 2 noisy alerts, got %d", len(noisy))
	}
	if noisy[0].Name != "Alpha Alert" || noisy[1].Name != "Zebra Alert" {
		t.Errorf("expected alphabetical order, got [%q, %q]", noisy[0].Name, noisy[1].Name)
	}
}

func TestFindNoiseAlerts_missingFeaturesPopulated(t *testing.T) {
	v := sparseVector("Generic")
	alert := makeAlert("mf-1", "logs_threshold", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert, got %d", len(noisy))
	}
	// sparseVector has all feature sets empty — all 5 should be in MissingFeatures
	expected := []string{"data sources", "entities", "actions", "conditions", "techniques"}
	if len(noisy[0].MissingFeatures) != len(expected) {
		t.Errorf("MissingFeatures: want %v, got %v", expected, noisy[0].MissingFeatures)
	}
}

func TestScorePair_dnsAvsAAAA_notDuplicate(t *testing.T) {
	// DNS A-record and AAAA-record alerts differ only in the record_type Lucene token.
	// With IDF weighting, the rare record_type atom dominates and the pair scores
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

	// Build a corpus where common Lucene tokens appear in many alerts (low IDF)
	// and record_type atoms appear in only 1 alert each (high IDF).
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


func TestScorePair_cloudflareARecordVsCNAME_notDuplicate(t *testing.T) {
	// Regression test for "Cloudflare - Audit - DNS CNAME Record Deleted" vs
	// "Cloudflare - Audit - DNS A Record Deleted" being incorrectly flagged at 93%.
	//
	// These alerts differ only in the record type: all other dimensions
	// (data sources, entities, actions, conditions, techniques, groupBy, alertType,
	// timeWindow) are identical. Previously the luceneQuery weight alone was not
	// enough to push the score below the threshold.
	//
	// The fix: nameTokens dimension + increased luceneQuery weight + reduced groupBy weight.
	// With IDF weighting, rare discriminating tokens ("cname") dominate and lower both
	// the luceneQuery and nameTokens Jaccard scores significantly.

	sharedGroupBy := normalizeGroupByKeys([]string{"cloudflare.actor.email"}) // → {"user"}

	commonLucene := map[string]struct{}{
		"coralogix.metadata.applicationname:cloudflare": {},
		"and":                      {},
		"eventtype:deletednsrecord": {},
	}
	makeQuery := func(recordType string) map[string]struct{} {
		m := make(map[string]struct{}, len(commonLucene)+1)
		for k := range commonLucene {
			m[k] = struct{}{}
		}
		m["cloudflare.newvaluejson.type:"+recordType] = struct{}{}
		return m
	}

	cname := featureVector{
		alertName:         "Cloudflare - Audit - DNS CNAME Record Deleted",
		alertType:         "logs_threshold",
		dataSources:       map[string]struct{}{"cloudflare": {}},
		entities:          map[string]struct{}{"user": {}},
		actions:           map[string]struct{}{"delete": {}},
		conditions:        map[string]struct{}{"dns": {}},
		techniques:        map[string]struct{}{"t1071": {}},
		groupByCategories: sharedGroupBy,
		luceneQuery:       makeQuery("cname"),
		nameTokens:        tokenizeAlertName("Cloudflare - Audit - DNS CNAME Record Deleted"),
		timeWindow:        "5m",
	}
	aRec := featureVector{
		alertName:         "Cloudflare - Audit - DNS A Record Deleted",
		alertType:         "logs_threshold",
		dataSources:       map[string]struct{}{"cloudflare": {}},
		entities:          map[string]struct{}{"user": {}},
		actions:           map[string]struct{}{"delete": {}},
		conditions:        map[string]struct{}{"dns": {}},
		techniques:        map[string]struct{}{"t1071": {}},
		groupByCategories: sharedGroupBy,
		luceneQuery:       makeQuery("a"),
		nameTokens:        tokenizeAlertName("Cloudflare - Audit - DNS A Record Deleted"),
		timeWindow:        "5m",
	}

	// Build a corpus where common Lucene and name tokens appear in many alerts (low IDF)
	// and the discriminating atoms ("cloudflare.newvaluejson.type:cname", "cname")
	// appear only once each (high IDF). This models a real Cloudflare alert library.
	corpus := []featureVector{cname, aRec}
	for i := 0; i < 8; i++ {
		corpus = append(corpus, featureVector{
			luceneQuery: map[string]struct{}{
				"coralogix.metadata.applicationname:cloudflare": {},
				"and":                      {},
				"eventtype:deletednsrecord": {},
			},
			nameTokens: map[string]struct{}{
				"cloudflare": {}, "audit": {}, "dns": {}, "record": {}, "deleted": {},
			},
		})
	}
	idf := buildIDF(corpus) // N=10; rare atoms df=1 → high IDF; common tokens df=10 → low IDF

	score := scorePair(cname, aRec, idf)
	if score >= duplicateThreshold {
		t.Errorf("Cloudflare DNS A vs CNAME should NOT be duplicates (identical groupBy): score=%.4f >= threshold=%.2f", score, duplicateThreshold)
	}
}

// ── Query analysis helpers ─────────────────────────────────────────────────

func TestHasWildcardQuery_withWildcard(t *testing.T) {
	tokens := map[string]struct{}{"severity": {}, "error*": {}}
	if !hasWildcardQuery(tokens) {
		t.Error("expected true for token containing *")
	}
}

func TestHasWildcardQuery_withExists(t *testing.T) {
	tokens := map[string]struct{}{"_exists_": {}}
	if !hasWildcardQuery(tokens) {
		t.Error("expected true for _exists_ token")
	}
}

func TestHasWildcardQuery_withoutWildcard(t *testing.T) {
	tokens := map[string]struct{}{"severity": {}, "error": {}, "okta": {}}
	if hasWildcardQuery(tokens) {
		t.Error("expected false for specific tokens")
	}
}

func TestHasWildcardQuery_empty(t *testing.T) {
	if hasWildcardQuery(map[string]struct{}{}) {
		t.Error("expected false for empty token set")
	}
}

func TestAvgIDF_emptyReturns1(t *testing.T) {
	got := avgIDF(map[string]struct{}{}, nil)
	if got != 1.0 {
		t.Errorf("avgIDF empty: want 1.0, got %f", got)
	}
}

func TestAvgIDF_unknownTokensReturn1(t *testing.T) {
	tokens := map[string]struct{}{"raretoken": {}}
	got := avgIDF(tokens, map[string]float64{})
	if got != 1.0 {
		t.Errorf("avgIDF unknown token: want 1.0, got %f", got)
	}
}

func TestAvgIDF_knownTokens(t *testing.T) {
	tokens := map[string]struct{}{"error": {}, "failed": {}}
	idf := map[string]float64{"error": 0.2, "failed": 0.4}
	got := avgIDF(tokens, idf)
	want := 0.3 // (0.2 + 0.4) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("avgIDF: want %f, got %f", want, got)
	}
}

func TestComputeQueryIDFThreshold_p25(t *testing.T) {
	idf := idfTable{luceneQuery: map[string]float64{"a": 0.1, "b": 0.3, "c": 0.7, "d": 0.9}}
	vectors := []featureVector{
		{luceneQuery: map[string]struct{}{"c": {}}}, // avgIDF 0.7
		{luceneQuery: map[string]struct{}{"a": {}}}, // avgIDF 0.1
		{luceneQuery: map[string]struct{}{"d": {}}}, // avgIDF 0.9
		{luceneQuery: map[string]struct{}{"b": {}}}, // avgIDF 0.3
	}
	threshold := computeQueryIDFThreshold(vectors, idf)
	want := 0.3 // p25 index=1 after sort [0.1, 0.3, 0.7, 0.9]
	if math.Abs(threshold-want) > 1e-9 {
		t.Errorf("computeQueryIDFThreshold: want %f, got %f", want, threshold)
	}
}

func TestComputeQueryIDFThreshold_emptyVectors(t *testing.T) {
	threshold := computeQueryIDFThreshold([]featureVector{}, idfTable{})
	if threshold != 0 {
		t.Errorf("empty vectors: want 0, got %f", threshold)
	}
}

func TestFindNoiseAlerts_behavioralThreshold_11IsNoisy(t *testing.T) {
	v := sparseVector("Chatty Flow")
	alert := makeAlert("bf-1", "flow", false, true, nil, "app", "sub")
	counts := map[string]int{"bf-1": 11}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "behavioral" {
		t.Errorf("11 triggers should be behavioral noise (>10), got %v", noisy)
	}
}

func TestFindNoiseAlerts_broadQuery_wildcard_structural(t *testing.T) {
	v := featureVector{
		alertName:   "Broad Wildcard Alert",
		luceneQuery: map[string]struct{}{"severity*": {}},
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	// Scoped alert (has appName) — would normally be excluded from structural.
	// But wildcard query makes it broad → structural noise.
	alert := makeAlert("bq-1", "logs_immediate", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("scoped alert with wildcard query should be structural noise: got %v", noisy)
	}
}

func TestFindNoiseAlerts_broadQuery_lowIDF_structural(t *testing.T) {
	idf := idfTable{luceneQuery: map[string]float64{"error": 0.1, "failed": 0.1}}
	v := featureVector{
		alertName:   "Generic Scoped Alert",
		luceneQuery: map[string]struct{}{"error": {}, "failed": {}},
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	alert := makeAlert("bq-2", "logs_threshold", false, true, nil, "my-app", "")
	// threshold=0.2 means avgIDF(0.1) < 0.2 → broad
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idf, 0.2)
	if len(noisy) != 1 || noisy[0].NoiseType != "structural" {
		t.Errorf("scoped alert with low-IDF query should be structural noise: got %v", noisy)
	}
}

func TestFindNoiseAlerts_specificQuery_notStructural(t *testing.T) {
	idf := idfTable{luceneQuery: map[string]float64{"admin": 0.9, "delete": 0.8}}
	v := featureVector{
		alertName:   "Specific Scoped Alert",
		luceneQuery: map[string]struct{}{"admin": {}, "delete": {}},
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	alert := makeAlert("bq-3", "logs_threshold", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idf, 0.2)
	if len(noisy) != 0 {
		t.Errorf("scoped alert with high-IDF query should NOT be structural noise: got %v", noisy)
	}
}

func TestFindNoiseAlerts_broadQueryReason(t *testing.T) {
	v := featureVector{
		alertName:   "Wildcard Alert",
		luceneQuery: map[string]struct{}{"*": {}},
		entities:    map[string]struct{}{},
		dataSources: map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	alert := makeAlert("bq-4", "logs_threshold", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0, idfTable{}, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noise alert, got %d", len(noisy))
	}
	if !strings.Contains(noisy[0].Reason, "Broad query") {
		t.Errorf("broad-query reason should mention 'Broad query', got: %q", noisy[0].Reason)
	}
}

func TestAnalyzeNoise_nilAlerts_returnsNil(t *testing.T) {
	result := AnalyzeNoise(nil, nil, 0)
	if result != nil {
		t.Errorf("expected nil for nil alerts, got %v", result)
	}
}

func TestAnalyzeNoise_emptyAlerts_returnsNil(t *testing.T) {
	result := AnalyzeNoise([]*models.AlertDef{}, nil, 0)
	if result != nil {
		t.Errorf("expected nil for empty alerts, got %v", result)
	}
}

func TestAnalyzeNoise_unscopedAlert_returnsStructuralNoise(t *testing.T) {
	alert := makeAlert("u-1", "logs_threshold", false, true, nil, "", "")
	result := AnalyzeNoise([]*models.AlertDef{alert}, map[string]int{}, 0)
	if len(result) != 1 {
		t.Fatalf("expected 1 noise alert for unscoped alert, got %d: %v", len(result), result)
	}
	if result[0].NoiseType != "structural" {
		t.Errorf("expected structural, got %q", result[0].NoiseType)
	}
}

func TestAnalyzeNoise_scopedAlert_notFlagged(t *testing.T) {
	alert := makeAlert("s-1", "logs_threshold", false, true, nil, "my-app", "auth")
	result := AnalyzeNoise([]*models.AlertDef{alert}, map[string]int{}, 0)
	if len(result) != 0 {
		t.Errorf("scoped alert should not be noise, got %v", result)
	}
}

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
	// Verify set membership — a merge that duplicated or dropped an ID would still
	// pass a length-only check.
	wantIDs := map[string]bool{"a0": true, "a1": true, "a2": true, "a3": true}
	for _, id := range f.AlertIDs {
		if !wantIDs[id] {
			t.Errorf("unexpected alert ID in merged family: %q", id)
		}
		delete(wantIDs, id)
	}
	for id := range wantIDs {
		t.Errorf("alert ID %q missing from merged family", id)
	}
}

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

func TestDeriveFamilyName_tier2_expanded_access(t *testing.T) {
	vecs := []featureVector{
		{alertID: "a1", alertName: "A", actions: map[string]struct{}{"read": {}}},
		{alertID: "a2", alertName: "A", actions: map[string]struct{}{"read": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Access Detections" {
		t.Errorf("want %q, got %q", "Access Detections", got)
	}
}

func TestDeriveFamilyName_tier2_expanded_configchange(t *testing.T) {
	vecs := []featureVector{
		{alertID: "cc1", alertName: "A", actions: map[string]struct{}{"modify": {}}},
		{alertID: "cc2", alertName: "A", actions: map[string]struct{}{"modify": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Configuration Change Detections" {
		t.Errorf("want %q, got %q", "Configuration Change Detections", got)
	}
}

func TestDeriveFamilyName_tier2_expanded_apiactivity(t *testing.T) {
	vecs := []featureVector{
		{alertID: "ap1", alertName: "A", actions: map[string]struct{}{"invoke": {}}},
		{alertID: "ap2", alertName: "A", actions: map[string]struct{}{"invoke": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "API Activity Detections" {
		t.Errorf("want %q, got %q", "API Activity Detections", got)
	}
}

func TestDeriveFamilyName_tier2_expanded_deployment(t *testing.T) {
	vecs := []featureVector{
		{alertID: "dp1", alertName: "A", actions: map[string]struct{}{"deploy": {}}},
		{alertID: "dp2", alertName: "A", actions: map[string]struct{}{"deploy": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Deployment Detections" {
		t.Errorf("want %q, got %q", "Deployment Detections", got)
	}
}

func TestDeriveFamilyName_tier2_expanded_dataops(t *testing.T) {
	vecs := []featureVector{
		{alertID: "do1", alertName: "A", actions: map[string]struct{}{"backup": {}}},
		{alertID: "do2", alertName: "A", actions: map[string]struct{}{"backup": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Data Operations Detections" {
		t.Errorf("want %q, got %q", "Data Operations Detections", got)
	}
}

func TestDeriveFamilyName_tier2_expanded_anomaly(t *testing.T) {
	vecs := []featureVector{
		{alertID: "an1", alertName: "A", actions: map[string]struct{}{"anomaly": {}}},
		{alertID: "an2", alertName: "A", actions: map[string]struct{}{"anomaly": {}}},
	}
	got := deriveFamilyName(vecs, []int{0, 1}, 1)
	if got != "Anomaly Detections" {
		t.Errorf("want %q, got %q", "Anomaly Detections", got)
	}
}

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
