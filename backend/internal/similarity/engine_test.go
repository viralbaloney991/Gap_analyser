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
	noisy := findNoiseAlerts(nil, nil, nil, 0)
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
	score := scorePair(a, b, idfTable{})
	if score < duplicateThreshold {
		t.Errorf("identical alert with same pivot should be duplicate: score=%.4f < threshold=%.2f", score, duplicateThreshold)
	}
}

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
	result := Analyze(alerts, nil, 0)
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
		weightLuceneQuery + weightTimeWindow
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
	return &models.AlertDef{
		ID:        id,
		AlertType: alertType,
		Labels:    labels,
		TypeDef:   typeDef,
		Features: models.AlertFeatures{
			VendorCovered:   vendorCovered,
			IsSecurityAlert: isSecurityAlert,
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
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("vendor-covered alert should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_buildingBlockExcluded(t *testing.T) {
	v := sparseVector("BB Alert")
	alert := makeAlert("bb-1", "logs_threshold", false, true,
		map[string]string{"flow_alert": "building block"}, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("building block should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_nonSecurityExcluded(t *testing.T) {
	v := sparseVector("Ops Alert")
	alert := makeAlert("ops-1", "logs_threshold", false, false, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("non-security alert should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_structuralNoise_unscopedHighVolume(t *testing.T) {
	v := sparseVector("Generic Threshold")
	alert := makeAlert("t-1", "logs_threshold", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
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
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("scoped alert should not be structural noise, got %v", noisy)
	}
}

func TestFindNoiseAlerts_structuralNoise_lowVolumeTypeNotNoisy(t *testing.T) {
	v := sparseVector("Anomaly Alert")
	alert := makeAlert("t-3", "logs_anomaly", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("low-volume type alert should not be structural noise, got %v", noisy)
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
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0)
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
	counts := map[string]int{"b-2": 20}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0)
	// 20 is not > 20, so not behavioral. Has app scoping so not structural either.
	if len(noisy) != 0 {
		t.Errorf("alert at threshold should not be noisy, got %v", noisy)
	}
}

func TestFindNoiseAlerts_bothSignals(t *testing.T) {
	v := sparseVector("Double Trouble")
	alert := makeAlert("bt-1", "logs_threshold", false, true, nil, "", "")
	counts := map[string]int{"bt-1": 30}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 5)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noise alert, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "both" {
		t.Errorf("noise_type: want both, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_flowAlert_behavioralApplies(t *testing.T) {
	v := sparseVector("Flow Alert")
	alert := makeAlert("f-1", "flow", false, true, nil, "", "")
	counts := map[string]int{"f-1": 25}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0)
	if len(noisy) != 1 {
		t.Fatalf("flow alert with high count should be behavioral noise, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "behavioral" {
		t.Errorf("noise_type: want behavioral, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_flowAlert_structuralDoesNotApply(t *testing.T) {
	v := sparseVector("Flow No Triggers")
	alert := makeAlert("f-2", "flow", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("flow alert should skip structural signal, got %v", noisy)
	}
}

func TestFindNoiseAlerts_orgAmplifier_reasonContainsCount(t *testing.T) {
	v := sparseVector("Generic")
	alert := makeAlert("amp-1", "logs_threshold", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 15)
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
	noisy := findNoiseAlerts(vectors, alerts, nil, 0)
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
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
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
