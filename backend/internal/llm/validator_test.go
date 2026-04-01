package llm

import (
	"testing"

	"coralogix-alert-analyzer/internal/classifier"
)

func TestParseValidationResult_ConfirmedAndRejected(t *testing.T) {
	raw := `{"confirmed": ["T1078.004", "T1110"], "rejected": ["T1021"]}`
	confirmed, err := parseValidationResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(confirmed) != 2 {
		t.Fatalf("expected 2 confirmed, got %d", len(confirmed))
	}
	if confirmed[0] != "T1078.004" || confirmed[1] != "T1110" {
		t.Errorf("unexpected confirmed: %v", confirmed)
	}
}

func TestParseValidationResult_EmptyConfirmed(t *testing.T) {
	raw := `{"confirmed": [], "rejected": ["T1021", "T1110"]}`
	confirmed, err := parseValidationResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(confirmed) != 0 {
		t.Errorf("expected 0 confirmed, got %d", len(confirmed))
	}
}

func TestParseValidationResult_StripsMarkdownFences(t *testing.T) {
	raw := "```json\n{\"confirmed\": [\"T1562.008\"], \"rejected\": []}\n```"
	confirmed, err := parseValidationResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(confirmed) != 1 || confirmed[0] != "T1562.008" {
		t.Errorf("unexpected confirmed: %v", confirmed)
	}
}

func TestBuildValidatorUserMessage(t *testing.T) {
	candidates := []classifier.Candidate{
		{TechniqueID: "T1110", Name: "Brute Force", Score: 0.91},
		{TechniqueID: "T1078", Name: "Valid Accounts", Score: 0.74},
	}
	msg := buildValidatorMessage("Okta Brute Force", "failed_logins:>5", "okta", "okta-audit", candidates)

	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	// Must contain alert name
	if !containsStr(msg, "Okta Brute Force") {
		t.Error("message missing alert name")
	}
	// Must contain both candidate IDs
	if !containsStr(msg, "T1110") || !containsStr(msg, "T1078") {
		t.Error("message missing candidate IDs")
	}
	// Must contain scores
	if !containsStr(msg, "0.91") {
		t.Error("message missing score")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
