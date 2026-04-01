package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/classifier"
)

var validatorSystemPrompt = `You are a MITRE ATT&CK expert. A semantic classifier has identified candidate techniques for a security alert. Your job is to:
1. Confirm candidates that are genuinely detected by this alert's query logic
2. Reject candidates that do not match what the alert actually detects
3. Where the evidence is specific enough, promote a parent technique to a sub-technique (e.g. T1078 → T1078.004 for cloud account activity)

Respond ONLY with JSON in this exact format:
{"confirmed": ["T1078.004"], "rejected": ["T1021"]}

No markdown, no explanation. If all candidates are wrong, return {"confirmed": [], "rejected": [...]}.`

type validationResult struct {
	Confirmed []string `json:"confirmed"`
	Rejected  []string `json:"rejected"`
}

// ValidateCandidates asks Llama to confirm/reject/promote classifier candidates for an alert.
// Returns the confirmed technique IDs. Uses FastMode (no extended reasoning needed).
func ValidateCandidates(ctx context.Context, provider Provider, name, query, app, subsystem string, candidates []classifier.Candidate) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	userMsg := buildValidatorMessage(name, query, app, subsystem, candidates)

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: validatorSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    256,
		FastMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("validator LLM: %w", err)
	}

	return parseValidationResult(resp)
}

func buildValidatorMessage(name, query, app, subsystem string, candidates []classifier.Candidate) string {
	var sb strings.Builder
	sb.WriteString("Alert: ")
	sb.WriteString(name)
	if app != "" {
		sb.WriteString(" | App: ")
		sb.WriteString(app)
	}
	if subsystem != "" {
		sb.WriteString(" | Subsystem: ")
		sb.WriteString(subsystem)
	}
	if query != "" {
		sb.WriteString("\nQuery: ")
		sb.WriteString(query)
	}
	sb.WriteString("\n\nCandidates:\n")
	for i, c := range candidates {
		sb.WriteString(fmt.Sprintf("%d. %s - %s (score: %.2f)\n", i+1, c.TechniqueID, c.Name, c.Score))
	}
	return sb.String()
}

func parseValidationResult(raw string) ([]string, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx > 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var result validationResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse validation result: %w (raw: %.100s)", err, raw)
	}
	if result.Confirmed == nil {
		return []string{}, nil
	}
	return result.Confirmed, nil
}
