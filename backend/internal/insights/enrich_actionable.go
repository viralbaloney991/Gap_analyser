package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

const actionableSystemPrompt = `You are a senior detection engineer generating actionable alert recommendations.

You will receive a JSON object with:
- gaps: four category arrays (no_detection, weak_detection_quality, missing_source_alerts, advanced_use_cases) — each a list of gap descriptions
- integrations: the client's actual log source integrations [{name, alert_count}]

For each gap item in each category, generate one actionable recommendation using ONLY log source names from the integrations list.
If no integration is a suitable match for a gap item, omit that item from the output entirely.

Respond with ONLY valid JSON matching this exact schema — no prose, no markdown:
{
  "no_detection": [{"prose": "<string>", "log_source": "<string>", "severity": "critical|high|medium|low", "query_skeleton": "<lucene>"}],
  "weak_detection_quality": [...],
  "missing_source_alerts": [...],
  "advanced_use_cases": [...]
}

Rules:
- log_source must exactly match the name field of an integration from the input list
- severity is one of: critical, high, medium, low — choose based on MITRE technique criticality
- query_skeleton is valid Lucene syntax targeting fields available in the chosen log_source
- prose is one sentence, action-oriented, naming the specific technique or gap by ID and name
- Every array must be present in the response — use [] if nothing applies, never null or omit the field
- Do not fabricate log source names, technique IDs, or field names not present in the input`

type actionableInput struct {
	Gaps         actionableGapsInput    `json:"gaps"`
	Integrations []integrationForPrompt `json:"integrations"`
}

type actionableGapsInput struct {
	NoDetection          []string `json:"no_detection"`
	WeakDetectionQuality []string `json:"weak_detection_quality"`
	MissingSourceAlerts  []string `json:"missing_source_alerts"`
	AdvancedUseCases     []string `json:"advanced_use_cases"`
}

type integrationForPrompt struct {
	Name       string `json:"name"`
	AlertCount int    `json:"alert_count"`
}

// EnrichActionable generates structured, actionable recommendations for four gap
// categories, grounding query skeletons in the client's actual integration list.
// Returns nil, nil when all four input categories are empty.
// Returns nil, err on LLM failure or JSON parse failure — caller logs and continues.
func EnrichActionable(
	ctx context.Context,
	gaps models.GapCategories,
	integrations []models.IntegrationInfo,
	provider llm.Provider,
) (*models.ActionableGapCategories, error) {
	if len(gaps.NoDetection) == 0 &&
		len(gaps.WeakDetectionQuality) == 0 &&
		len(gaps.MissingSourceAlerts) == 0 &&
		len(gaps.AdvancedUseCases) == 0 {
		return nil, nil
	}

	promptIntegrations := make([]integrationForPrompt, 0, len(integrations))
	for _, integ := range integrations {
		promptIntegrations = append(promptIntegrations, integrationForPrompt{
			Name:       integ.Name,
			AlertCount: integ.AlertCount,
		})
	}

	input := actionableInput{
		Gaps: actionableGapsInput{
			NoDetection:          gaps.NoDetection,
			WeakDetectionQuality: gaps.WeakDetectionQuality,
			MissingSourceAlerts:  gaps.MissingSourceAlerts,
			AdvancedUseCases:     gaps.AdvancedUseCases,
		},
		Integrations: promptIntegrations,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("actionable enrichment marshal: %w", err)
	}

	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: actionableSystemPrompt,
		UserMessage:  string(inputJSON),
		MaxTokens:    4096,
	})
	if err != nil {
		return nil, fmt.Errorf("actionable enrichment LLM call: %w", err)
	}

	return parseActionableResponse(raw)
}

func parseActionableResponse(raw string) (*models.ActionableGapCategories, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 {
		raw = raw[:i+1]
	}

	var loose struct {
		NoDetection          []models.ActionableRecommendation `json:"no_detection"`
		WeakDetectionQuality []models.ActionableRecommendation `json:"weak_detection_quality"`
		MissingSourceAlerts  []models.ActionableRecommendation `json:"missing_source_alerts"`
		AdvancedUseCases     []models.ActionableRecommendation `json:"advanced_use_cases"`
	}
	if err := json.Unmarshal([]byte(raw), &loose); err != nil {
		return nil, fmt.Errorf("actionable enrichment JSON parse: %w", err)
	}

	coerce := func(s []models.ActionableRecommendation) []models.ActionableRecommendation {
		if s == nil {
			return []models.ActionableRecommendation{}
		}
		return s
	}

	return &models.ActionableGapCategories{
		NoDetection:          coerce(loose.NoDetection),
		WeakDetectionQuality: coerce(loose.WeakDetectionQuality),
		MissingSourceAlerts:  coerce(loose.MissingSourceAlerts),
		AdvancedUseCases:     coerce(loose.AdvancedUseCases),
	}, nil
}
