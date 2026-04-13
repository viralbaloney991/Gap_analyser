package similarity

import (
	"encoding/json"
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
	noisy := findNoiseAlerts(vectors)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].Name != "Noisy" {
		t.Errorf("expected \"Noisy\", got %q", noisy[0].Name)
	}
}

func TestFindNoiseAlerts_nilInput(t *testing.T) {
	noisy := findNoiseAlerts(nil)
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
	noisy := findNoiseAlerts(vectors)
	if len(noisy) != 0 {
		t.Errorf("expected no noise for exactly 3 tokens, got %v", noisy)
	}
}

func TestFindNoiseAlerts_isSorted(t *testing.T) {
	vectors := []featureVector{
		{alertName: "ZAlert", dataSources: map[string]struct{}{"x": {}}, entities: map[string]struct{}{}, actions: map[string]struct{}{}, conditions: map[string]struct{}{}, techniques: map[string]struct{}{}},
		{alertName: "AAlert", dataSources: map[string]struct{}{"y": {}}, entities: map[string]struct{}{}, actions: map[string]struct{}{}, conditions: map[string]struct{}{}, techniques: map[string]struct{}{}},
	}
	noisy := findNoiseAlerts(vectors)
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
