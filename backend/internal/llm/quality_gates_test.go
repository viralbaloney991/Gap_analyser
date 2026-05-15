package llm

import (
	"encoding/json"
	"testing"
)

func TestSuggestionBackwardCompat(t *testing.T) {
	// Old cached JSON format uses alert_name, query_hint, priority.
	// After struct update, these must still unmarshal into the legacy fields.
	oldJSON := `[{"log_source":"Windows","alert_name":"Old Name","description":"desc","query_hint":"x:y","priority":"high"}]`
	var sugs []Suggestion
	if err := json.Unmarshal([]byte(oldJSON), &sugs); err != nil {
		t.Fatalf("unmarshal old format: %v", err)
	}
	if len(sugs) != 1 {
		t.Fatalf("expected 1, got %d", len(sugs))
	}
	if sugs[0].AlertName != "Old Name" {
		t.Errorf("AlertName compat: got %q", sugs[0].AlertName)
	}
	if sugs[0].QueryHint != "x:y" {
		t.Errorf("QueryHint compat: got %q", sugs[0].QueryHint)
	}
}
