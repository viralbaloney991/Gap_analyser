package coralogix

import (
	"encoding/json"
	"strings"
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

func TestEventCountReqBody_camelCaseFieldNames(t *testing.T) {
	// Coralogix proto3 JSON transcoding requires camelCase field names.
	// This test ensures the request struct tags match the API expectation.
	body := eventCountReqBody{
		AlertIDs: []string{"id-1", "id-2"},
		Pagination: eventCountReqPagination{PageSize: 1000},
	}
	body.TimestampRange.From = "2024-01-01T00:00:00Z"
	body.TimestampRange.To = "2024-01-31T00:00:00Z"

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)

	for _, want := range []string{`"alertIds"`, `"timestampRange"`, `"pageSize"`} {
		if !strings.Contains(s, want) {
			t.Errorf("request JSON missing camelCase key %s\nfull JSON: %s", want, s)
		}
	}
	for _, bad := range []string{`"alert_ids"`, `"timestamp_range"`, `"page_size"`} {
		if strings.Contains(s, bad) {
			t.Errorf("request JSON must not contain snake_case key %s\nfull JSON: %s", bad, s)
		}
	}

	// page must be omitted when empty (omitempty)
	if strings.Contains(s, `"page"`) {
		t.Errorf("page key must be omitted when empty\nfull JSON: %s", s)
	}

	// page must appear when set
	body.Pagination.Page = "tok-abc"
	b2, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal with page: %v", err)
	}
	if !strings.Contains(string(b2), `"page":"tok-abc"`) {
		t.Errorf("page key must appear when non-empty\nfull JSON: %s", string(b2))
	}
}

func TestParseAlertEventsResponse_multiPage(t *testing.T) {
	// alertId is nested inside cxEventPayload per actual API response shape.
	raw := []byte(`{
		"events": [
			{"cxEventPayload": {"alertId": "abc123"}},
			{"cxEventPayload": {"alertId": "abc123"}},
			{"cxEventPayload": {"alertId": "def456"}}
		],
		"pagination": {"nextPage": "page-token-2"}
	}`)
	counts := make(map[string]int)
	next, err := parseAlertEventsResponse(raw, counts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "page-token-2" {
		t.Errorf("nextPage: want page-token-2, got %q", next)
	}
	if counts["abc123"] != 2 {
		t.Errorf("abc123: want 2, got %d", counts["abc123"])
	}
	if counts["def456"] != 1 {
		t.Errorf("def456: want 1, got %d", counts["def456"])
	}
}

func TestParseAlertEventsResponse_lastPage(t *testing.T) {
	raw := []byte(`{"events": [{"cxEventPayload": {"alertId": "abc123"}}], "pagination": {}}`)
	counts := make(map[string]int)
	next, err := parseAlertEventsResponse(raw, counts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "" {
		t.Errorf("nextPage: want empty string for last page, got %q", next)
	}
	if counts["abc123"] != 1 {
		t.Errorf("abc123: want 1, got %d", counts["abc123"])
	}
}
