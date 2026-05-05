package llm

import (
	"strings"
	"testing"
)

func TestParseCorrelations_validJSON(t *testing.T) {
	raw := `[
	  {
	    "type": "correlation",
	    "title": "Execution + Persistence chain",
	    "description": "T1059 followed by T1547 for same entity within 30 min",
	    "involved_techniques": ["T1059", "T1547"],
	    "query_skeleton": "alert_name:*script* AND ...",
	    "priority": "high"
	  },
	  {
	    "type": "anomaly",
	    "title": "T1059 frequency baseline",
	    "description": "Alert when command execution count exceeds 2 std dev from 7-day baseline",
	    "involved_techniques": ["T1059"],
	    "query_skeleton": "subsystemName:sysmon AND ...",
	    "priority": "medium"
	  }
	]`

	got, err := parseCorrelations(raw)
	if err != nil {
		t.Fatalf("parseCorrelations failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 correlations, got %d", len(got))
	}
	if got[0].Type != "correlation" {
		t.Errorf("expected type=correlation, got %q", got[0].Type)
	}
	if got[1].Type != "anomaly" {
		t.Errorf("expected type=anomaly, got %q", got[1].Type)
	}
	if got[0].Priority != "high" {
		t.Errorf("expected priority=high, got %q", got[0].Priority)
	}
}

func TestParseCorrelations_markdownFenced(t *testing.T) {
	raw := "```json\n[\n  {\"type\":\"anomaly\",\"title\":\"T\",\"description\":\"D\",\"involved_techniques\":[\"T1059\"],\"query_skeleton\":\"q\",\"priority\":\"low\"}\n]\n```"

	got, err := parseCorrelations(raw)
	if err != nil {
		t.Fatalf("parseCorrelations with fences failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 correlation, got %d", len(got))
	}
	if got[0].Title != "T" {
		t.Errorf("unexpected title: %q", got[0].Title)
	}
}

func TestParseCorrelations_literalNewlineInString(t *testing.T) {
	// Some LLMs emit literal newlines inside JSON strings.
	raw := "{\"type\":\"correlation\",\"title\":\"T\",\"description\":\"line1\nline2\",\"involved_techniques\":[\"T1059\"],\"query_skeleton\":\"q\",\"priority\":\"high\"}"
	wrapped := "[\n  " + raw + "\n]"

	got, err := parseCorrelations(wrapped)
	if err != nil {
		t.Fatalf("parseCorrelations with literal newline failed: %v", err)
	}
	if !strings.Contains(got[0].Description, "line1") {
		t.Errorf("expected description to contain 'line1', got %q", got[0].Description)
	}
}
