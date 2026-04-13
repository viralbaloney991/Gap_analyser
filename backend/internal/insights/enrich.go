package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

// Enrich takes a completed SimilarityResult and alert list, sends one
// structured prompt to the LLM, and returns an InsightsReport.
// Returns nil, nil if the result has no meaningful content to enrich.
// Returns nil, err on LLM failure (caller treats as non-fatal).
func Enrich(
	ctx context.Context,
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	provider llm.Provider,
) (*models.InsightsReport, error) {
	if result == nil || (len(result.Duplicates) == 0 && len(result.Families) == 0 &&
		len(result.CoverageInsights) == 0 && len(result.NoiseAlerts) == 0) {
		return nil, nil
	}

	prompt := buildPrompt(result, alerts)
	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		UserMessage: prompt,
		MaxTokens:   1024,
	})
	if err != nil {
		return nil, fmt.Errorf("insights LLM call: %w", err)
	}

	// Strip markdown code fence if present.
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i != -1 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}

	var report models.InsightsReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, fmt.Errorf("insights JSON parse: %w", err)
	}
	return &report, nil
}

func buildPrompt(result *models.SimilarityResult, alerts []*models.AlertDef) string {
	var sb strings.Builder

	sb.WriteString("You are a security detection engineer reviewing a SIEM alert library.\n\n")
	sb.WriteString(fmt.Sprintf("Alert library (%d alerts):\n", len(alerts)))
	for _, a := range alerts {
		sb.WriteString(fmt.Sprintf("- %s: sources=%v, actions=%v, techniques=%v\n",
			a.Name, a.Features.DataSources, a.Features.Actions, a.Features.Techniques))
	}

	sb.WriteString("\nSimilarity analysis results:\n")

	sb.WriteString(fmt.Sprintf("- Duplicates (%d):", len(result.Duplicates)))
	for _, d := range result.Duplicates {
		if len(d.AlertNames) >= 2 {
			sb.WriteString(fmt.Sprintf(" %s ≈ %s (%.0f%%),", d.AlertNames[0], d.AlertNames[1], d.Similarity*100))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("- Detection families (%d):", len(result.Families)))
	for _, f := range result.Families {
		sb.WriteString(fmt.Sprintf(" %s: %s;", f.Name, strings.Join(f.AlertNames, ", ")))
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("- Coverage gaps: %s\n", strings.Join(result.CoverageInsights, "; ")))
	noiseNames := make([]string, len(result.NoiseAlerts))
	for i, na := range result.NoiseAlerts {
		noiseNames[i] = na.Name
	}
	sb.WriteString(fmt.Sprintf("- Noise alerts (sparse feature vectors): %s\n", strings.Join(noiseNames, ", ")))

	sb.WriteString(`
Respond with JSON only:
{
  "summary": "2-3 sentence overview of the detection posture",
  "top_priority": ["ordered list of 3-5 most important actions"],
  "strengths": ["2-3 things well covered"],
  "recommendations": ["3-5 specific actionable items"],
  "enriched_dups": ["one sentence per duplicate pair explaining business impact"],
  "enriched_gaps": ["one sentence per coverage gap explaining risk"],
  "noise_explanations": ["one sentence per noisy alert explaining specific gaps"]
}`)

	return sb.String()
}
