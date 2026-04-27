package mitre

import (
	"encoding/json"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestBuildTechniqueJSON_IsValidJSON(t *testing.T) {
	result := BuildTechniqueJSON()
	if result == "" {
		t.Fatal("BuildTechniqueJSON returned empty string")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("BuildTechniqueJSON returned invalid JSON: %v", err)
	}
}

func TestBuildTechniqueJSON_ContainsAllTactics(t *testing.T) {
	result := BuildTechniqueJSON()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	expected := []string{
		"reconnaissance", "resource-development", "initial-access",
		"execution", "persistence", "privilege-escalation",
		"defense-evasion", "credential-access", "discovery",
		"lateral-movement", "collection", "command-and-control",
		"exfiltration", "impact",
	}
	for _, tactic := range expected {
		if _, ok := parsed[tactic]; !ok {
			t.Errorf("missing tactic: %s", tactic)
		}
	}
}

func TestBuildTechniqueJSON_SubTechniqueSuffixFormat(t *testing.T) {
	result := BuildTechniqueJSON()
	type techEntry struct {
		N string            `json:"n"`
		S map[string]string `json:"s"`
	}
	var parsed map[string]map[string]techEntry
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec, ok := parsed["execution"]
	if !ok {
		t.Fatal("missing execution tactic")
	}
	t1059, ok := exec["T1059"]
	if !ok {
		t.Fatal("missing T1059 in execution")
	}
	if t1059.S["001"] != "PowerShell" {
		t.Errorf("expected T1059 suffix 001 = PowerShell, got %q", t1059.S["001"])
	}
}

func TestValidTechniqueID_KnownParent(t *testing.T) {
	if !ValidTechniqueID("T1059") {
		t.Error("T1059 should be valid")
	}
}

func TestValidTechniqueID_KnownSubTechnique(t *testing.T) {
	if !ValidTechniqueID("T1059.001") {
		t.Error("T1059.001 should be valid")
	}
}

func TestValidTechniqueID_UnknownParent(t *testing.T) {
	if ValidTechniqueID("T9999") {
		t.Error("T9999 should not be valid")
	}
}

func TestValidTechniqueID_UnknownSubTechnique(t *testing.T) {
	if ValidTechniqueID("T1059.999") {
		t.Error("T1059.999 should not be valid")
	}
}

func TestAnalyzeCoverage_TechniqueLevel(t *testing.T) {
	// Unscoped alert (no DataSources, no Entities) — should mark T1059 as weak
	unscopedAlert := &models.AlertDef{
		Name: "Broad CMD Alert",
		Features: models.AlertFeatures{
			Techniques:  []string{"T1059"},
			DataSources: nil,
			Entities:    nil,
		},
	}
	// Scoped alert (has DataSources) — T1078 should NOT be weak
	scopedAlert := &models.AlertDef{
		Name: "Valid Accounts Scoped",
		Features: models.AlertFeatures{
			Techniques:  []string{"T1078"},
			DataSources: []string{"windows-security"},
		},
	}

	result := AnalyzeCoverage([]*models.AlertDef{unscopedAlert, scopedAlert})

	if result.TechniqueCoverage == nil {
		t.Fatal("TechniqueCoverage map is nil")
	}

	t1059, ok := result.TechniqueCoverage["T1059"]
	if !ok {
		t.Fatal("T1059 missing from TechniqueCoverage")
	}
	if t1059.AlertCount != 1 {
		t.Errorf("T1059: want AlertCount=1, got %d", t1059.AlertCount)
	}
	if !t1059.Weak {
		t.Error("T1059: want Weak=true (unscoped alert), got false")
	}
	if t1059.Name == "" {
		t.Error("T1059: Name must be populated")
	}

	t1078, ok := result.TechniqueCoverage["T1078"]
	if !ok {
		t.Fatal("T1078 missing from TechniqueCoverage")
	}
	if t1078.Weak {
		t.Error("T1078: want Weak=false (scoped alert), got true")
	}
	if t1078.AlertCount != 1 {
		t.Errorf("T1078: want AlertCount=1, got %d", t1078.AlertCount)
	}

	// Uncovered technique must be present with AlertCount=0
	t1566, ok := result.TechniqueCoverage["T1566"]
	if !ok {
		t.Fatal("T1566 (Phishing) missing from TechniqueCoverage — all parent techniques must be present")
	}
	if t1566.AlertCount != 0 {
		t.Errorf("T1566: want AlertCount=0, got %d", t1566.AlertCount)
	}
	if t1566.Weak {
		t.Error("T1566: Weak must be false when AlertCount=0")
	}
}

func TestAnalyzeCoverage_TechniqueCoverageHasTactic(t *testing.T) {
	alerts := []*models.AlertDef{
		{
			Features: models.AlertFeatures{
				Techniques:  []string{"T1078"},
				DataSources: []string{"cloudtrail"},
			},
		},
	}
	result := AnalyzeCoverage(alerts)
	entry, ok := result.TechniqueCoverage["T1078"]
	if !ok {
		t.Fatal("expected T1078 in TechniqueCoverage")
	}
	if entry.Tactic == "" {
		t.Error("expected Tactic to be populated, got empty string")
	}
	if entry.Tactic != "initial-access" {
		t.Errorf("expected tactic=initial-access, got %q", entry.Tactic)
	}
}
