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
	if len(out.NoDetection) != 1 || out.NoDetection[0] != "T1078: no coverage" {
		t.Errorf("no_detection roundtrip failed: %v", out.NoDetection)
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
	b, _ := json.Marshal(ir)
	var out InsightsReport
	json.Unmarshal(b, &out)
	if len(out.GapCategories.NoDetection) != 1 {
		t.Error("GapCategories not preserved through JSON")
	}
}
