package similarity

import (
	"testing"
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
	if noisy[0] != "Noisy" {
		t.Errorf("expected \"Noisy\", got %q", noisy[0])
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
	if noisy[0] != "AAlert" || noisy[1] != "ZAlert" {
		t.Errorf("expected sorted [AAlert, ZAlert], got %v", noisy)
	}
}
