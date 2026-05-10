package mitre

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestAnalyzeCoverage_AlertRulesPopulated(t *testing.T) {
	alert := &models.AlertDef{
		Name: "Brute Force Detector",
		Features: models.AlertFeatures{
			Techniques: []string{"T1110"},
		},
	}
	result := AnalyzeCoverage([]*models.AlertDef{alert})
	entry, ok := result.TechniqueCoverage["T1110"]
	if !ok {
		t.Fatal("T1110 not in TechniqueCoverage")
	}
	if len(entry.AlertRules) != 1 {
		t.Fatalf("expected 1 AlertRule, got %d: %v", len(entry.AlertRules), entry.AlertRules)
	}
	if entry.AlertRules[0] != "Brute Force Detector" {
		t.Errorf("expected AlertRules[0] = %q, got %q", "Brute Force Detector", entry.AlertRules[0])
	}
}

func TestAnalyzeCoverage_AlertRulesSubTechniqueCreditsParent(t *testing.T) {
	alert := &models.AlertDef{
		Name: "PS Script Block",
		Features: models.AlertFeatures{
			Techniques: []string{"T1059.001"},
		},
	}
	result := AnalyzeCoverage([]*models.AlertDef{alert})
	parent, ok := result.TechniqueCoverage["T1059"]
	if !ok {
		t.Fatal("T1059 not in TechniqueCoverage")
	}
	if len(parent.AlertRules) == 0 {
		t.Fatal("expected parent T1059 to have AlertRules from sub-technique alert")
	}
	if parent.AlertRules[0] != "PS Script Block" {
		t.Errorf("expected AlertRules[0] = %q, got %q", "PS Script Block", parent.AlertRules[0])
	}
}
