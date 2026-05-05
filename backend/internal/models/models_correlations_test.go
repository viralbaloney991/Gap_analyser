package models

import (
	"encoding/json"
	"reflect"
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
	if !reflect.DeepEqual(got, req) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, req)
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
	if !reflect.DeepEqual(got, sug) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, sug)
	}
}

func TestCorrelationsResponseRoundtrip(t *testing.T) {
	resp := CorrelationsResponse{
		Suggestions: []CorrelationSuggestion{
			{
				Type:               "anomaly",
				Title:              "T1059 baseline deviation",
				Description:        "Alert when command execution exceeds 2 std dev from 7-day baseline",
				InvolvedTechniques: []string{"T1059"},
				QuerySkeleton:      "subsystemName:sysmon AND ...",
				Priority:           "medium",
			},
		},
		Provider: "claude",
		Cached:   true,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got CorrelationsResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, resp) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, resp)
	}
}
