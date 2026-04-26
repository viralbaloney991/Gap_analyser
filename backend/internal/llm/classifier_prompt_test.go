package llm

import (
	"testing"
)

func TestParseClassifierResponse_ValidArray(t *testing.T) {
	result := parseClassifierResponse(`["T1059.001", "T1078"]`)
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d: %v", len(result), result)
	}
}

func TestParseClassifierResponse_EmptyArray(t *testing.T) {
	result := parseClassifierResponse(`[]`)
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestParseClassifierResponse_HallucinatedIDDropped(t *testing.T) {
	result := parseClassifierResponse(`["T1059.001", "T9999.999"]`)
	if len(result) != 1 || result[0] != "T1059.001" {
		t.Errorf("expected only T1059.001, got %v", result)
	}
}

func TestParseClassifierResponse_MalformedJSON(t *testing.T) {
	result := parseClassifierResponse(`not json`)
	if result != nil {
		t.Errorf("expected nil for malformed JSON, got %v", result)
	}
}

func TestParseClassifierResponse_MarkdownWrapped(t *testing.T) {
	result := parseClassifierResponse("```json\n[\"T1059.001\"]\n```")
	if len(result) != 1 || result[0] != "T1059.001" {
		t.Errorf("expected [T1059.001], got %v", result)
	}
}

func TestBuildClassifierMessage_FullAlert(t *testing.T) {
	inp := AlertInput{
		ID:        "a1",
		Name:      "Azure - Audit - Suspicious Login",
		Query:     "action:login AND result:failure",
		App:       "azure",
		Subsystem: "audit",
	}
	msg := buildClassifierMessage(inp)
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	for _, want := range []string{"Azure - Audit - Suspicious Login", "azure", "audit", "action:login"} {
		found := false
		for i := 0; i <= len(msg)-len(want); i++ {
			if msg[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("message missing %q\nGot: %s", want, msg)
		}
	}
}
