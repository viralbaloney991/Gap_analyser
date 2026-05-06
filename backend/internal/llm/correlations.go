package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"coralogix-alert-analyzer/internal/models"
)

// MaxCorrelations is the hard cap on suggestions returned per gap.
const MaxCorrelations = 5

const correlationsSystemPrompt = `You are a Coralogix SIEM alert engineering expert specialising in MITRE ATT&CK correlation and anomaly detection.

You are given:
1. A specific detection gap — a technique that only has threshold-based alerts and lacks a deeper detection layer.
2. The client's already-covered MITRE techniques — techniques that have existing alerts.
3. The client's available log sources.

Your job: produce up to 5 suggestions split into two types.

**type: "correlation"** (up to 3)
- Link the gap technique to at least one technique from the already-covered list.
- The rule fires when BOTH are seen for the same entity within a time window.
- involved_techniques must include at least one technique from the covered list.

**type: "anomaly"** (up to 2)
- Stay focused on the gap technique itself.
- Suggest a behavioural baseline or statistical anomaly rule on top of the existing threshold.

Rules:
- query_skeleton must be valid Lucene.
- priority: "critical" | "high" | "medium" | "low".
- title: concise, action-oriented (under 10 words).
- description: one sentence explaining what the rule detects and why it matters.
- Use only log sources from the provided list; do not fabricate sources.
- Return at most 5 objects total (3 correlation + 2 anomaly).

Respond ONLY with a JSON array. No markdown, no explanation, just the array.
Each object must have exactly these fields:
{
  "type": "correlation" or "anomaly",
  "title": "Short rule title",
  "description": "One sentence description",
  "involved_techniques": ["T1059", ...],
  "query_skeleton": "Lucene query",
  "priority": "high"
}`

// CorrelationInput is the context for generating correlation suggestions.
type CorrelationInput struct {
	GapProse          string
	LogSources        []string
	CoveredTechniques []string
}

// GenerateCorrelations uses the LLM to produce correlation and anomaly suggestions for a gap.
func GenerateCorrelations(ctx context.Context, provider Provider, input CorrelationInput) ([]models.CorrelationSuggestion, error) {
	userMsg := buildCorrelationsUserMessage(input)

	log.Printf("INFO [correlations] requesting %s for gap=%q log_sources=%d covered=%d",
		provider.Name(), truncate(input.GapProse, 60), len(input.LogSources), len(input.CoveredTechniques))

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: correlationsSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    4096,
		FastMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	suggestions, err := parseCorrelations(resp)
	if err != nil {
		return nil, fmt.Errorf("parse correlations: %w", err)
	}

	if len(suggestions) > MaxCorrelations {
		suggestions = suggestions[:MaxCorrelations]
	}

	const maxCorrelationType = 3
	const maxAnomalyType = 2

	var corrCount, anomalyCount int
	capped := suggestions[:0]
	for _, s := range suggestions {
		if s.Type == "correlation" && corrCount < maxCorrelationType {
			capped = append(capped, s)
			corrCount++
		} else if s.Type == "anomaly" && anomalyCount < maxAnomalyType {
			capped = append(capped, s)
			anomalyCount++
		}
	}
	suggestions = capped

	return suggestions, nil
}

func buildCorrelationsUserMessage(input CorrelationInput) string {
	var sb strings.Builder

	sb.WriteString("## Detection Gap\n")
	sb.WriteString(input.GapProse)
	sb.WriteString("\n\n")

	sb.WriteString("## Already-Covered Techniques\n")
	if len(input.CoveredTechniques) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, t := range input.CoveredTechniques {
			sb.WriteString("- ")
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n## Available Log Sources\n")
	if len(input.LogSources) == 0 {
		sb.WriteString("(none specified — suggest based on gap technique)\n")
	} else {
		for _, ls := range input.LogSources {
			sb.WriteString("- ")
			sb.WriteString(ls)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\nSuggest up to 5 correlation and anomaly rules (3 correlation + 2 anomaly max).")

	return sb.String()
}

func parseCorrelations(raw string) ([]models.CorrelationSuggestion, error) {
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

	cleaned = sanitizeJSONStrings(cleaned)

	var suggestions []models.CorrelationSuggestion
	if err := json.Unmarshal([]byte(cleaned), &suggestions); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w\nRaw response:\n%s", err, raw[:min(len(raw), 500)])
	}

	valid := suggestions[:0]
	for _, s := range suggestions {
		if s.Type != "correlation" && s.Type != "anomaly" {
			log.Printf("WARN parseCorrelations: discarding suggestion with invalid type %q (title=%q)", s.Type, s.Title)
			continue
		}
		valid = append(valid, s)
	}
	suggestions = valid

	return suggestions, nil
}

