package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/mitre"
)

const mapTacticsSystemPrompt = `You are a MITRE ATT&CK expert. Given a security detection gap, identify the most relevant MITRE ATT&CK tactics and techniques.

Rules:
- tactic_ids: TA-prefixed IDs only (e.g. TA0001, TA0008). Maximum 3.
- technique_ids: T-prefixed IDs, include subtechnique suffix when applicable (e.g. T1566.001, T1021.001). Maximum 5.
- Only return techniques that belong to the identified tactics.
- Return empty arrays when no clear mapping exists.

Respond with JSON only — no markdown, no explanation:
{"tactic_ids": ["TA0001"], "technique_ids": ["T1566.001"]}`

// MapTacticsInput is the context for MITRE tactic mapping.
type MapTacticsInput struct {
	Prose     string
	LogSource string
}

// MapTacticsResult holds the parsed LLM output for tactic/technique mapping.
type MapTacticsResult struct {
	TacticIDs    []string `json:"tactic_ids"`
	TechniqueIDs []string `json:"technique_ids"`
}

// parseMapTactics extracts a MapTacticsResult from raw LLM output, stripping
// markdown code fences if present.
func parseMapTactics(raw string) (*MapTacticsResult, error) {
	s := strings.TrimSpace(raw)
	// Strip markdown fences: find first '{' and last '}'
	if start := strings.Index(s, "{"); start > 0 {
		s = s[start:]
	}
	if end := strings.LastIndex(s, "}"); end >= 0 && end < len(s)-1 {
		s = s[:end+1]
	}
	var r MapTacticsResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("parse map tactics JSON: %w", err)
	}
	// Defensive caps
	if len(r.TacticIDs) > 3 {
		r.TacticIDs = r.TacticIDs[:3]
	}
	if len(r.TechniqueIDs) > 5 {
		r.TechniqueIDs = r.TechniqueIDs[:5]
	}
	// Filter out LLM-hallucinated IDs not present in the MITRE catalog.
	validTactics := r.TacticIDs[:0]
	for _, id := range r.TacticIDs {
		if mitre.ValidTacticID(id) {
			validTactics = append(validTactics, id)
		}
	}
	r.TacticIDs = validTactics

	validTechs := r.TechniqueIDs[:0]
	for _, id := range r.TechniqueIDs {
		if mitre.ValidTechniqueID(id) {
			validTechs = append(validTechs, id)
		}
	}
	r.TechniqueIDs = validTechs

	return &r, nil
}

// GenerateMapTactics uses the LLM to map a gap prose and log source to MITRE
// ATT&CK tactic and technique IDs.
func GenerateMapTactics(ctx context.Context, provider Provider, input MapTacticsInput) (*MapTacticsResult, error) {
	userMsg := fmt.Sprintf("Gap: %s\nLog source: %s", input.Prose, input.LogSource)

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: mapTacticsSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    256,
		FastMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	return parseMapTactics(resp)
}
