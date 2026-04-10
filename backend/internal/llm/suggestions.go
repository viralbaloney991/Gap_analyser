package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// MaxSuggestions is the hard cap on suggestions returned per technique.
const MaxSuggestions = 6

// Suggestion is a single alert recommendation.
type Suggestion struct {
	LogSource   string `json:"log_source"`
	AlertName   string `json:"alert_name"`
	Description string `json:"description"`
	QueryHint   string `json:"query_hint"`
	Priority    string `json:"priority"`
}

// SuggestionsResult is the response from the suggestion engine.
type SuggestionsResult struct {
	Provider    string       `json:"provider"`
	Suggestions []Suggestion `json:"suggestions"`
}

// TechniqueInput describes the single technique to generate suggestions for.
type TechniqueInput struct {
	ID     string
	Name   string
	Tactic string
}

// GapInput describes the context for generating alert suggestions.
type GapInput struct {
	// LogSources are the client's available log sources (from Monday + alert data sources).
	LogSources []string
	// Technique is the single uncovered technique to suggest alerts for.
	Technique TechniqueInput
}

const systemPrompt = `You are a Coralogix SIEM alert engineering expert specializing in MITRE ATT&CK coverage.

Your job: given a client's available log sources and ONE specific uncovered MITRE ATT&CK technique, suggest up to 6 concrete Coralogix alerts that can detect this technique using the available logs.

Rules:
- Only suggest alerts that are REALISTICALLY detectable from the available log sources
- Each suggestion must reference a specific log source the client already has
- Provide a concrete Lucene/DataPrime query hint for each alert
- Be specific about what fields/events to look for in the log source
- Keep alert names concise: "[LogSource] - [Behavior Description]"
- Suggest DIFFERENT detection approaches (different log sources, different indicators) — do not repeat the same idea
- If the technique CANNOT be detected with any available log source, return an empty array []
- Return at most 6 suggestions, ordered by detection quality (best first)

Respond ONLY with a JSON array. No markdown, no explanation, just the JSON array.
Each object must have exactly these fields:
{
  "log_source": "Which available log source to use",
  "alert_name": "Suggested alert name",
  "description": "What the alert detects and why it maps to this technique",
  "query_hint": "Lucene or DataPrime query pattern to use",
  "priority": "critical|high|medium|low"
}`

// GenerateSuggestions uses the LLM to suggest alerts for one uncovered technique.
func GenerateSuggestions(ctx context.Context, provider Provider, input GapInput) (*SuggestionsResult, error) {
	userMsg := buildUserMessage(input)

	log.Printf("INFO [suggestions] requesting %s for technique %s (%s) with %d log sources",
		provider.Name(), input.Technique.ID, input.Technique.Name, len(input.LogSources))

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: systemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    4096,
		FastMode:     true, // disable thinking/reasoning mode — suggestions need speed, not chain-of-thought
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	suggestions, err := parseSuggestions(resp)
	if err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}

	// Hard cap at MaxSuggestions
	if len(suggestions) > MaxSuggestions {
		suggestions = suggestions[:MaxSuggestions]
	}

	return &SuggestionsResult{
		Provider:    provider.Name(),
		Suggestions: suggestions,
	}, nil
}

func buildUserMessage(input GapInput) string {
	var sb strings.Builder

	sb.WriteString("## Available Log Sources\n")
	sb.WriteString("The client has these log sources onboarded in Coralogix:\n")
	for _, ls := range input.LogSources {
		sb.WriteString("- ")
		sb.WriteString(ls)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\n## Uncovered Technique\n"))
	sb.WriteString(fmt.Sprintf("**%s: %s** (Tactic: %s)\n\n",
		input.Technique.ID, input.Technique.Name, input.Technique.Tactic))
	sb.WriteString("Suggest up to 6 alerts that can detect this technique using the available log sources above.")

	return sb.String()
}

func parseSuggestions(raw string) ([]Suggestion, error) {
	// Strip markdown code fences if present
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

	var suggestions []Suggestion
	if err := json.Unmarshal([]byte(cleaned), &suggestions); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w\nRaw response:\n%s", err, raw[:min(len(raw), 500)])
	}

	return suggestions, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
