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
		MaxTokens:   4096,
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

const (
	maxPromptDuplicates = 10 // reduced from 15
	maxPromptFamilies   = 8  // reduced from 15
	maxPromptNoise      = 12 // reduced from 20
)

func buildPrompt(result *models.SimilarityResult, alerts []*models.AlertDef) string {
	var sb strings.Builder

	sb.WriteString("Role: Senior detection engineer. Task: Analyze alert library quality.\n\n")
	sb.WriteString(fmt.Sprintf("Library: %d alerts | %d duplicates | %d families | %d noisy alerts\n\n",
		len(alerts), len(result.Duplicates), len(result.Families), len(result.NoiseAlerts)))

	// Duplicates section.
	dups := result.Duplicates
	if len(dups) > maxPromptDuplicates {
		dups = dups[:maxPromptDuplicates]
	}
	if len(dups) > 0 {
		sb.WriteString("Duplicates:\n")
		for _, d := range dups {
			if len(d.AlertNames) >= 2 {
				sb.WriteString(fmt.Sprintf("- %s ≈ %s (%.0f%%)\n", d.AlertNames[0], d.AlertNames[1], d.Similarity*100))
			}
		}
		sb.WriteString("\n")
	}

	// Families section.
	families := result.Families
	if len(families) > maxPromptFamilies {
		families = families[:maxPromptFamilies]
	}
	if len(families) > 0 {
		sb.WriteString("Families: ")
		parts := make([]string, len(families))
		for i, f := range families {
			parts[i] = fmt.Sprintf("%s (%s)", f.Name, strings.Join(f.AlertNames, ", "))
		}
		sb.WriteString(strings.Join(parts, " | "))
		sb.WriteString("\n\n")
	}

	// Coverage gaps section.
	if len(result.CoverageInsights) > 0 {
		sb.WriteString("Coverage gaps: ")
		sb.WriteString(strings.Join(result.CoverageInsights, "; "))
		sb.WriteString("\n\n")
	}

	// Noisy alerts section — includes missing features and reason.
	noiseAlerts := result.NoiseAlerts
	if len(noiseAlerts) > maxPromptNoise {
		noiseAlerts = noiseAlerts[:maxPromptNoise]
	}
	if len(noiseAlerts) > 0 {
		sb.WriteString("Noisy alerts:\n")
		for _, na := range noiseAlerts {
			line := fmt.Sprintf("- %s", na.Name)
			if na.NoiseType != "" {
				if na.TriggerCount > 0 {
					line += fmt.Sprintf(" [%s, %d×]", na.NoiseType, na.TriggerCount)
				} else {
					line += fmt.Sprintf(" [%s]", na.NoiseType)
				}
			}
			if len(na.MissingFeatures) > 0 {
				line += fmt.Sprintf(": no %s", strings.Join(na.MissingFeatures, ", no "))
			}
			if na.Reason != "" {
				line += fmt.Sprintf(" — %s", na.Reason)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("STRICT RULES for output:\n")
	sb.WriteString("- Use ONLY alert names, counts, and patterns from the data above — never invent statistics.\n")
	sb.WriteString("- Do NOT reference any client name, company name, or product name (you do not know them).\n")
	sb.WriteString("- Recommendations must describe structural patterns (e.g. 'alerts lacking data source binding'), never specific alert counts you invent.\n")
	sb.WriteString("- noise_explanations MUST contain exactly one entry per noisy alert listed above — never omit or truncate this field when noisy alerts are present.\n\n")
	sb.WriteString("Return JSON only — no prose, no markdown:\n")
	sb.WriteString(`{"summary":"<2-3 sentences>","top_priority":["<3-5 items>"],"strengths":["<2-3 items>"],"recommendations":["<3-5 items>"],"enriched_dups":["<1 sentence each>"],"enriched_gaps":["<1 sentence each>"],"noise_explanations":["<mandatory — one entry per noisy alert, explain the behavioral or structural signal>"]}`)

	return sb.String()
}
