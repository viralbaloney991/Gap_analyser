package mitre

import (
	"encoding/json"
	"testing"
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
