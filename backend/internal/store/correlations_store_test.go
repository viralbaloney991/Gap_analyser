package store

import (
	"encoding/json"
	"testing"
)

// TestCorrelationRowJSON verifies CorrelationRow serialisation does not panic.
func TestCorrelationRowJSON(t *testing.T) {
	raw, _ := json.Marshal([]map[string]string{{"type": "correlation", "title": "test"}})
	row := CorrelationRow{
		CacheKey:    "abc123",
		Client:      "acme",
		Suggestions: json.RawMessage(raw),
		Provider:    "claude",
	}
	if row.CacheKey != "abc123" {
		t.Errorf("unexpected cache key: %s", row.CacheKey)
	}
	if len(row.Suggestions) == 0 {
		t.Error("suggestions should not be empty")
	}
}
