package models

import (
	"encoding/json"
	"testing"
)

func TestCorrelationsRequestRoundtrip(t *testing.T) {
	req := CorrelationsRequest{
		Client:            "acme",
		GapProse:          "T1059 has threshold alert but no anomaly layer",
		LogSources:        []string{"sysmon"},
		CoveredTechniques: []string{"T1078", "T1110"},
		Provider:          "claude",
		Force:             false,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got CorrelationsRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Client != req.Client || got.GapProse != req.GapProse {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}

func TestCorrelationSuggestionRoundtrip(t *testing.T) {
	sug := CorrelationSuggestion{
		Type:               "correlation",
		Title:              "Execution → Persistence chain",
		Description:        "Fire when T1059 and T1547 both seen for same entity within 30 min",
		InvolvedTechniques: []string{"T1059", "T1547"},
		QuerySkeleton:      "alert_name:*scripting* AND ...",
		Priority:           "high",
	}
	data, err := json.Marshal(sug)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got CorrelationSuggestion
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != sug.Type || got.Priority != sug.Priority {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}
