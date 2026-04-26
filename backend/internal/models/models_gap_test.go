package models

import (
	"encoding/json"
	"testing"
)

func TestGapCategories_JSONRoundTrip(t *testing.T) {
	gc := GapCategories{
		EnvironmentCleanup:   []string{"Alert A duplicates Alert B"},
		NoDetection:          []string{"T1078: no coverage"},
		PoorTacticCoverage:   []string{},
		WeakDetectionQuality: []string{},
		AdvancedUseCases:     []string{},
		MissingSourceAlerts:  []string{"Azure AD: 0 alerts"},
	}
	b, err := json.Marshal(gc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out GapCategories
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.EnvironmentCleanup) != 1 || out.EnvironmentCleanup[0] != "Alert A duplicates Alert B" {
		t.Errorf("environment_cleanup roundtrip failed: %v", out.EnvironmentCleanup)
	}
	if len(out.NoDetection) != 1 || out.NoDetection[0] != "T1078: no coverage" {
		t.Errorf("no_detection roundtrip failed: %v", out.NoDetection)
	}
	if len(out.MissingSourceAlerts) != 1 || out.MissingSourceAlerts[0] != "Azure AD: 0 alerts" {
		t.Errorf("missing_source_alerts roundtrip failed: %v", out.MissingSourceAlerts)
	}
}

func TestTechniqueCoverageEntry_JSONRoundTrip(t *testing.T) {
	entry := TechniqueCoverageEntry{Name: "Valid Accounts", AlertCount: 2, Weak: true}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out TechniqueCoverageEntry
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Weak || out.AlertCount != 2 {
		t.Errorf("unexpected: %+v", out)
	}
}

func TestInsightsReport_GapCategoriesField(t *testing.T) {
	ir := InsightsReport{
		Summary: "ok",
		GapCategories: GapCategories{
			NoDetection: []string{"T1059"},
		},
	}
	b, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out InsightsReport
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.GapCategories.NoDetection) != 1 {
		t.Error("GapCategories not preserved through JSON")
	}
}
