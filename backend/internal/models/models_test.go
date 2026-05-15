package models

import (
	"encoding/json"
	"testing"
)

func TestBuildDetectionAlertJSON(t *testing.T) {
	raw := `{
		"name": "Detect Credential Dump via LSASS",
		"sigma_rule": "title: test\nlogsource:\n  product: windows",
		"falsepositives": ["Security scanner"],
		"logic": "process.name:lsass.exe",
		"window": "5m",
		"windowReason": "cred access stage",
		"source": "EDR",
		"severity": "critical"
	}`
	var a BuildDetectionAlert
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.SigmaRule != "title: test\nlogsource:\n  product: windows" {
		t.Errorf("SigmaRule: got %q", a.SigmaRule)
	}
	if len(a.Falsepositives) != 1 || a.Falsepositives[0] != "Security scanner" {
		t.Errorf("Falsepositives: got %v", a.Falsepositives)
	}
}
