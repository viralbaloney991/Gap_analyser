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

func TestAlertSuggestionNewFields(t *testing.T) {
	raw := `{
		"title": "Detect Lateral Movement via Pass-the-Hash",
		"log_source": "Windows Security",
		"description": "Detects PtH activity",
		"lucene_query": "event.action:pass-the-hash",
		"severity": "high",
		"sigma_rule": "title: Detect LM\nlogsource:\n  product: windows",
		"log_source_product": "windows",
		"window": "30m",
		"window_reason": "lateral movement stage",
		"falsepositives": ["Admin lateral movement"],
		"mitre_technique_id": "T1550.002"
	}`
	var s AlertSuggestion
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Title != "Detect Lateral Movement via Pass-the-Hash" {
		t.Errorf("Title: got %q", s.Title)
	}
	if s.LuceneQuery != "event.action:pass-the-hash" {
		t.Errorf("LuceneQuery: got %q", s.LuceneQuery)
	}
	if s.Severity != "high" {
		t.Errorf("Severity: got %q", s.Severity)
	}
	if s.SigmaRule == "" {
		t.Error("SigmaRule should not be empty")
	}
	if len(s.Falsepositives) != 1 {
		t.Errorf("Falsepositives: got %v", s.Falsepositives)
	}
}
