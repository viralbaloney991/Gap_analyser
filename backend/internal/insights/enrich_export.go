package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

const exportNarrativeSystemPrompt = `You are a senior detection engineer producing an executive briefing for a client security report.
You will receive a JSON object summarising the client's current alert library state.
Respond with ONLY valid JSON — no prose, no markdown:
{
  "executive_summary": "<3-4 paragraph narrative>",
  "key_findings": ["<string>"],
  "recommended_actions": ["<string>"]
}
Rules:
- executive_summary: 3-4 paragraphs covering coverage strengths, noise concerns, critical gaps, and overall posture
- key_findings: max 8 bulleted facts (coverage %, noise counts, gap categories with counts)
- recommended_actions: max 8 items prioritised by severity and impact
- Only reference data from the input — never fabricate statistics or technique names
- Respond with only valid JSON — no prose, no markdown fences`

type exportNarrativeInput struct {
	AlertCount          int                        `json:"alert_count"`
	IntegrationCount    int                        `json:"integration_count"`
	CoveragePercent     float64                    `json:"coverage_percent"`
	TacticBreakdown     map[string]exportTacticRow `json:"tactic_breakdown"`
	NoiseAlertCount     int                        `json:"noise_alert_count"`
	DuplicateGroupCount int                        `json:"duplicate_group_count"`
	GapSummary          map[string]int             `json:"gap_summary"`
	KeyGaps             []string                   `json:"key_gaps"`
	KeyActions          []string                   `json:"key_actions"`
}

type exportTacticRow struct {
	TacticName string  `json:"tactic_name"`
	Covered    int     `json:"covered"`
	Total      int     `json:"total"`
	Percent    float64 `json:"percent"`
}

// EnrichExportNarrative generates an executive narrative for the full PDF report.
// Returns nil, nil when insightsReport is nil (nothing meaningful to narrate).
// Returns nil, err on LLM failure or JSON parse failure.
func EnrichExportNarrative(
	ctx context.Context,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	alertInsights *models.SimilarityResult,
	insightsReport *models.InsightsReport,
	provider llm.Provider,
) (*models.ExportNarrativeReport, error) {
	if insightsReport == nil {
		return nil, nil
	}

	tacticBreakdown := make(map[string]exportTacticRow)
	if mitreCoverage != nil {
		for tacticID, tc := range mitreCoverage.Summary.TacticBreakdown {
			if tc.Covered > 0 {
				tacticBreakdown[tacticID] = exportTacticRow{
					TacticName: tc.TacticName,
					Covered:    tc.Covered,
					Total:      tc.Total,
					Percent:    tc.Percent,
				}
			}
		}
	}

	coveragePercent := 0.0
	if mitreCoverage != nil {
		coveragePercent = mitreCoverage.Summary.CoveragePercent
	}

	noiseCount, dupCount := 0, 0
	if alertInsights != nil {
		noiseCount = len(alertInsights.NoiseAlerts)
		dupCount = len(alertInsights.Duplicates)
	}

	gapCats := insightsReport.GapCategories
	gapSummary := map[string]int{
		"no_detection":           len(gapCats.NoDetection),
		"weak_detection_quality": len(gapCats.WeakDetectionQuality),
		"missing_source_alerts":  len(gapCats.MissingSourceAlerts),
		"advanced_use_cases":     len(gapCats.AdvancedUseCases),
		"environment_cleanup":    len(gapCats.EnvironmentCleanup),
		"poor_tactic_coverage":   len(gapCats.PoorTacticCoverage),
	}

	keyGaps := gapCats.NoDetection
	if len(keyGaps) > 5 {
		keyGaps = keyGaps[:5]
	}

	keyActions := collectKeyActions(insightsReport)

	input := exportNarrativeInput{
		AlertCount:          len(alerts),
		IntegrationCount:    len(integrations),
		CoveragePercent:     coveragePercent,
		TacticBreakdown:     tacticBreakdown,
		NoiseAlertCount:     noiseCount,
		DuplicateGroupCount: dupCount,
		GapSummary:          gapSummary,
		KeyGaps:             keyGaps,
		KeyActions:          keyActions,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("export narrative marshal: %w", err)
	}

	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: exportNarrativeSystemPrompt,
		UserMessage:  string(inputJSON),
		MaxTokens:    4096,
	})
	if err != nil {
		return nil, fmt.Errorf("export narrative LLM call: %w", err)
	}

	return parseExportNarrativeResponse(raw)
}

func collectKeyActions(report *models.InsightsReport) []string {
	var actions []string
	if report.ActionableGaps != nil {
		sources := [][]models.ActionableRecommendation{
			report.ActionableGaps.NoDetection,
			report.ActionableGaps.WeakDetectionQuality,
			report.ActionableGaps.MissingSourceAlerts,
			report.ActionableGaps.AdvancedUseCases,
		}
		for _, src := range sources {
			for _, rec := range src {
				actions = append(actions, rec.Prose)
				if len(actions) >= 5 {
					return actions
				}
			}
		}
	}
	if len(actions) == 0 {
		actions = report.Recommendations
		if len(actions) > 5 {
			actions = actions[:5]
		}
	}
	return actions
}

func parseExportNarrativeResponse(raw string) (*models.ExportNarrativeReport, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "{"); i >= 0 {
		s = s[i:]
	}
	if i := strings.LastIndex(s, "}"); i >= 0 {
		s = s[:i+1]
	}

	var out models.ExportNarrativeReport
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("export narrative JSON parse: %w", err)
	}
	return &out, nil
}
