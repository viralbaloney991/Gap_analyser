package insights

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestBuildStructuredSignals_IntegrationGaps(t *testing.T) {
	result := &models.SimilarityResult{
		NoiseAlerts: []models.NoiseAlert{
			{Name: "Alert A", NoiseType: "structural", TriggerCount: 0},
		},
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"X", "Y"}},
			{AlertNames: []string{"P", "Q"}},
		},
	}
	alerts := []*models.AlertDef{
		{Name: "Alert One"},
		{Name: "Alert Two"},
	}
	integrations := []models.IntegrationInfo{
		{Name: "Azure AD", AlertCount: 0},
		{Name: "AWS CloudTrail", AlertCount: 5},
	}
	mitreCoverage := &models.MITRECoverageResult{
		TechniqueCoverage: map[string]models.TechniqueCoverageEntry{
			"T1078": {Name: "Valid Accounts", AlertCount: 0},
			"T1059": {Name: "Command Interpreter", AlertCount: 2, Weak: true},
		},
		Summary: models.MITRECoverageSummary{
			TacticBreakdown: map[string]models.TacticCoverage{
				"initial-access": {TacticName: "Initial Access", Percent: 11.11, Covered: 1, Total: 9},
				"reconnaissance": {TacticName: "Reconnaissance", Percent: 0, Covered: 0, Total: 10},
			},
		},
	}

	sig := buildStructuredSignals(result, alerts, integrations, mitreCoverage)

	if sig.AlertCount != 2 {
		t.Errorf("alert_count: want 2, got %d", sig.AlertCount)
	}
	if sig.IntegrationCount != 2 {
		t.Errorf("integration_count: want 2, got %d", sig.IntegrationCount)
	}
	if sig.DuplicateGroups != 2 {
		t.Errorf("duplicate_groups: want 2, got %d", sig.DuplicateGroups)
	}
	if len(sig.IntegrationGaps) != 1 {
		t.Fatalf("integration_gaps: want 1, got %d", len(sig.IntegrationGaps))
	}
	if sig.IntegrationGaps[0].Name != "Azure AD" {
		t.Errorf("wrong gap: %s", sig.IntegrationGaps[0].Name)
	}
	if len(sig.NoiseAlerts) != 1 || sig.NoiseAlerts[0] != "Alert A [structural]" {
		t.Errorf("noise_alerts: %v", sig.NoiseAlerts)
	}

	tc, ok := sig.TechniqueCoverage["T1059"]
	if !ok {
		t.Fatal("T1059 missing from technique_coverage")
	}
	if !tc.Weak {
		t.Error("T1059 should be weak")
	}
	if _, ok := sig.TechniqueCoverage["T1078"]; !ok {
		t.Error("T1078 (uncovered) should appear in technique_coverage")
	}
}

func TestBuildStructuredSignals_NilMitre(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	// Should not panic with nil mitreCoverage
	sig := buildStructuredSignals(result, nil, nil, nil)
	if sig.AlertCount != 0 {
		t.Errorf("want 0 alerts, got %d", sig.AlertCount)
	}
	if len(sig.TechniqueCoverage) != 0 {
		t.Error("want empty technique_coverage for nil mitre")
	}
}

func TestBuildStructuredSignals_WeakFlagPreserved(t *testing.T) {
	mitreCoverage := &models.MITRECoverageResult{
		TechniqueCoverage: map[string]models.TechniqueCoverageEntry{
			"T1110": {Name: "Brute Force", AlertCount: 1, Weak: false},
		},
		Summary: models.MITRECoverageSummary{},
	}
	sig := buildStructuredSignals(&models.SimilarityResult{}, nil, nil, mitreCoverage)
	if sig.TechniqueCoverage["T1110"].Weak {
		t.Error("T1110 should not be weak")
	}
}

func TestBuildStructuredSignals_NoiseAlertLabels(t *testing.T) {
	result := &models.SimilarityResult{
		NoiseAlerts: []models.NoiseAlert{
			{Name: "FreqAlert", NoiseType: "behavioral", TriggerCount: 45},
			{Name: "StructAlert", NoiseType: "structural", TriggerCount: 0},
			{Name: "UnclassAlert"},
		},
	}
	sig := buildStructuredSignals(result, nil, nil, nil)
	if len(sig.NoiseAlerts) != 3 {
		t.Fatalf("want 3 noise alerts, got %d", len(sig.NoiseAlerts))
	}
	if sig.NoiseAlerts[0] != "FreqAlert [behavioral, 45×]" {
		t.Errorf("wrong label for behavioral: %q", sig.NoiseAlerts[0])
	}
	if sig.NoiseAlerts[1] != "StructAlert [structural]" {
		t.Errorf("wrong label for structural: %q", sig.NoiseAlerts[1])
	}
	if sig.NoiseAlerts[2] != "UnclassAlert" {
		t.Errorf("wrong label for unclassified: %q", sig.NoiseAlerts[2])
	}
}
