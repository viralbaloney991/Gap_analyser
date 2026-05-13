package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMapTacticsRequestRoundtrip(t *testing.T) {
	req := MapTacticsRequest{
		Client:    "acme",
		Prose:     "No detection for lateral movement via RDP",
		LogSource: "windows_security",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MapTacticsRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, req) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, req)
	}
}

func TestMapTacticsResponseRoundtrip(t *testing.T) {
	resp := MapTacticsResponse{
		TacticIDs:    []string{"TA0008", "TA0001"},
		TechniqueIDs: []string{"T1021.001", "T1566.001"},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MapTacticsResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, resp) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, resp)
	}
}
