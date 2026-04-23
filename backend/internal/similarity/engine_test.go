package similarity

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestFindNoiseAlerts_returnsNoisyAlerts(t *testing.T) {
	vectors := []featureVector{
		{
			alertName:   "Noisy",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{},
			actions:     map[string]struct{}{},
			conditions:  map[string]struct{}{},
			techniques:  map[string]struct{}{},
		},
		{
			alertName:   "RichAlert",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{"user": {}},
			actions:     map[string]struct{}{"login": {}},
			conditions:  map[string]struct{}{"failed": {}},
			techniques:  map[string]struct{}{"t1078": {}},
		},
	}
	noisy := findNoiseAlerts(vectors, nil)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].Name != "Noisy" {
		t.Errorf("expected \"Noisy\", got %q", noisy[0].Name)
	}
}

func TestFindNoiseAlerts_nilInput(t *testing.T) {
	noisy := findNoiseAlerts(nil, nil)
	if noisy != nil {
		t.Errorf("expected nil for nil input, got %v", noisy)
	}
}

func TestFindNoiseAlerts_atThreshold(t *testing.T) {
	// Total = 3 means NOT noise (threshold is strictly < 3)
	vectors := []featureVector{
		{
			alertName:   "AtThreshold",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{"user": {}},
			actions:     map[string]struct{}{"login": {}},
			conditions:  map[string]struct{}{},
			techniques:  map[string]struct{}{},
		},
	}
	noisy := findNoiseAlerts(vectors, nil)
	if len(noisy) != 0 {
		t.Errorf("expected no noise for exactly 3 tokens, got %v", noisy)
	}
}

func TestFindNoiseAlerts_isSorted(t *testing.T) {
	vectors := []featureVector{
		{alertName: "ZAlert", dataSources: map[string]struct{}{"x": {}}, entities: map[string]struct{}{}, actions: map[string]struct{}{}, conditions: map[string]struct{}{}, techniques: map[string]struct{}{}},
		{alertName: "AAlert", dataSources: map[string]struct{}{"y": {}}, entities: map[string]struct{}{}, actions: map[string]struct{}{}, conditions: map[string]struct{}{}, techniques: map[string]struct{}{}},
	}
	noisy := findNoiseAlerts(vectors, nil)
	if len(noisy) != 2 {
		t.Fatalf("expected 2 noisy alerts, got %d", len(noisy))
	}
	if noisy[0].Name != "AAlert" || noisy[1].Name != "ZAlert" {
		t.Errorf("expected sorted [AAlert, ZAlert], got %v", noisy)
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

func TestWeightsSumToOne(t *testing.T) {
	const sum = weightDataSources + weightEntities + weightActions +
		weightConditions + weightTechniques + weightGroupBy + weightAlertType +
		weightLuceneQuery + weightTimeWindow
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("similarity weights sum to %.10f, want exactly 1.0", sum)
	}
}

func TestFindNoiseAlerts_missingFeaturesPopulated(t *testing.T) {
	// "Noisy" has only dataSources set — the other four dimensions are empty.
	vectors := []featureVector{
		{
			alertName:   "Noisy",
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
	missing := noisy[0].MissingFeatures
	want := []string{"entities", "actions", "conditions", "techniques"}
	if len(missing) != len(want) {
		t.Fatalf("MissingFeatures = %v, want %v", missing, want)
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	for _, m := range missing {
		if !wantSet[m] {
			t.Errorf("unexpected MissingFeature %q", m)
		}
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
	score := scorePair(guestUser, apiEvent)
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
