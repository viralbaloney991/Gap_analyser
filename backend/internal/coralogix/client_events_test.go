package coralogix

import (
	"testing"
)

func TestParseEventCountResponse_happyPath(t *testing.T) {
	raw := []byte(`{
		"alertsEventsCounts": [
			{"alertId": "abc123", "count": 47},
			{"alertId": "def456", "count": 5}
		]
	}`)
	got, err := parseEventCountResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["abc123"] != 47 {
		t.Errorf("abc123: want 47, got %d", got["abc123"])
	}
	if got["def456"] != 5 {
		t.Errorf("def456: want 5, got %d", got["def456"])
	}
}

func TestParseEventCountResponse_emptyResponse(t *testing.T) {
	raw := []byte(`{"alertsEventsCounts": []}`)
	got, err := parseEventCountResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseEventCountResponse_missingAlertInResponse(t *testing.T) {
	raw := []byte(`{"alertsEventsCounts": [{"alertId": "abc123", "count": 10}]}`)
	got, err := parseEventCountResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["missing-id"]; ok {
		t.Errorf("missing-id should not be in map")
	}
	if got["abc123"] != 10 {
		t.Errorf("abc123: want 10, got %d", got["abc123"])
	}
}

func TestParseEventCountResponse_invalidJSON(t *testing.T) {
	_, err := parseEventCountResponse([]byte(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
