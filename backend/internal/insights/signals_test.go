package insights

import (
	"fmt"
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

	sig := buildStructuredSignals(result, alerts, integrations, mitreCoverage, nil)

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
	sig := buildStructuredSignals(result, nil, nil, nil, nil)
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
	sig := buildStructuredSignals(&models.SimilarityResult{}, nil, nil, mitreCoverage, nil)
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
	sig := buildStructuredSignals(result, nil, nil, nil, nil)
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

func TestBuildStructuredSignals_ImmediateCandidates_Included(t *testing.T) {
	alerts := []*models.AlertDef{
		{
			ID:        "alert-1",
			Name:      "Power Apps App Launched",
			AlertType: "logs_immediate",
			Features: models.AlertFeatures{
				IsSecurityAlert: true,
				IsBuildingBlock: false,
				VendorCovered:   false,
			},
			TypeDef: map[string]any{
				"logsFilter": map[string]any{
					"simpleFilter": map[string]any{
						"luceneQuery": "eventSource:PowerApps AND eventType:AppLaunched",
					},
				},
			},
		},
	}
	eventCounts := map[string]int{"alert-1": 150}
	sig := buildStructuredSignals(nil, alerts, nil, nil, eventCounts)
	if len(sig.ImmediateNoiseCandidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(sig.ImmediateNoiseCandidates))
	}
	c := sig.ImmediateNoiseCandidates[0]
	if c.Name != "Power Apps App Launched" {
		t.Errorf("name: %q", c.Name)
	}
	if c.Query != "eventSource:PowerApps AND eventType:AppLaunched" {
		t.Errorf("query: %q", c.Query)
	}
	if c.TriggerCount != 150 {
		t.Errorf("trigger_count: want 150, got %d", c.TriggerCount)
	}
}

func TestBuildStructuredSignals_ImmediateCandidates_Excluded(t *testing.T) {
	alerts := []*models.AlertDef{
		// Wrong type
		{
			ID: "a1", Name: "MetricAlert", AlertType: "metric_threshold",
			Features: models.AlertFeatures{IsSecurityAlert: true},
		},
		// Not security
		{
			ID: "a2", Name: "OpsAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: false},
		},
		// Building block
		{
			ID: "a3", Name: "BBAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true, IsBuildingBlock: true},
		},
		// Vendor covered
		{
			ID: "a4", Name: "VendorAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true, VendorCovered: true},
		},
		// Scoped (has app filter)
		{
			ID: "a5", Name: "ScopedAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true},
			TypeDef: map[string]any{
				"logsFilter": map[string]any{
					"simpleFilter": map[string]any{
						"labelFilters": map[string]any{
							"applicationName": []any{
								map[string]any{"value": "MyApp"},
							},
						},
					},
				},
			},
		},
		// Has entity filter
		{
			ID: "a6", Name: "EntityAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{
				IsSecurityAlert: true,
				Entities:        []string{"user:alice"},
			},
		},
	}
	sig := buildStructuredSignals(nil, alerts, nil, nil, nil)
	if len(sig.ImmediateNoiseCandidates) != 0 {
		t.Errorf("want 0 candidates, got %d: %v", len(sig.ImmediateNoiseCandidates), sig.ImmediateNoiseCandidates)
	}
}

func TestBuildStructuredSignals_ImmediateCandidates_Cap(t *testing.T) {
	alerts := make([]*models.AlertDef, 35)
	for i := range alerts {
		alerts[i] = &models.AlertDef{
			ID:        fmt.Sprintf("id-%02d", i),
			Name:      fmt.Sprintf("Alert-%02d", i),
			AlertType: "logs_immediate",
			Features:  models.AlertFeatures{IsSecurityAlert: true},
		}
	}
	sig := buildStructuredSignals(nil, alerts, nil, nil, nil)
	if len(sig.ImmediateNoiseCandidates) != 30 {
		t.Errorf("want 30 candidates (cap), got %d", len(sig.ImmediateNoiseCandidates))
	}
	// Verify sorted by name
	if sig.ImmediateNoiseCandidates[0].Name != "Alert-00" {
		t.Errorf("first after sort: %q", sig.ImmediateNoiseCandidates[0].Name)
	}
	if sig.ImmediateNoiseCandidates[29].Name != "Alert-29" {
		t.Errorf("last after sort: %q", sig.ImmediateNoiseCandidates[29].Name)
	}
}

func TestBuildStructuredSignals_ImmediateCandidates_NilEventCounts(t *testing.T) {
	alerts := []*models.AlertDef{
		{
			ID: "a1", Name: "NoCount", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true},
		},
	}
	sig := buildStructuredSignals(nil, alerts, nil, nil, nil) // nil eventCounts
	if len(sig.ImmediateNoiseCandidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(sig.ImmediateNoiseCandidates))
	}
	if sig.ImmediateNoiseCandidates[0].TriggerCount != 0 {
		t.Errorf("trigger_count: want 0 for nil eventCounts, got %d", sig.ImmediateNoiseCandidates[0].TriggerCount)
	}
}
