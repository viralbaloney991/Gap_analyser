package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

const gapAnalysisSystemPrompt = `You are a senior detection engineer analysing an organisation's security alert library.
You will receive a JSON object with these fields:
- alert_count, integration_count: library size
- tactic_coverage: per-tactic {pct, alerts} — pct is coverage percent (0–100)
- technique_coverage: per T-code {name, alerts, weak} — weak=true means covered but unscoped
- integration_gaps: integrations with zero alerts [{name, alerts}]
- noise_alerts: alert names flagged as noisy (with type and trigger count)
- duplicate_groups: number of duplicate alert groups

Respond with ONLY valid JSON matching this exact schema — no prose, no markdown:
{
  "summary": "<2-4 sentences: start with strengths then key gaps>",
  "environment_cleanup": ["<string>"],
  "no_detection": ["<string>"],
  "poor_tactic_coverage": ["<string>"],
  "weak_detection_quality": ["<string>"],
  "advanced_use_cases": ["<string>"],
  "missing_source_alerts": ["<string>"]
}

Rules:
- Every category field must be a JSON array — use [] if nothing applies, never null or omit the field
- Only reference techniques, tactics, and alert names present in the input — never fabricate names
- poor_tactic_coverage: flag any tactic with pct < 25
- weak_detection_quality: only flag techniques where weak=true in the input
- advanced_use_cases: reason over technique type; only flag when threshold/count alerts exist but no anomaly layer
- summary: prose only (no bullet points), 2-4 sentences`

// Enrich takes a completed SimilarityResult, assembles structured signals, sends them
// to Claude Opus, and returns an InsightsReport with 6-category gap analysis.
// Returns nil, nil if the result has no meaningful content to enrich.
// Returns nil, err on LLM failure (caller treats as non-fatal).
func Enrich(
	ctx context.Context,
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	provider llm.Provider,
) (*models.InsightsReport, error) {
	if result == nil {
		return nil, nil
	}

	signals := buildStructuredSignals(result, alerts, integrations, mitreCoverage, nil)
	signalsJSON, err := json.Marshal(signals)
	if err != nil {
		return nil, fmt.Errorf("insights signals marshal: %w", err)
	}

	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: gapAnalysisSystemPrompt,
		UserMessage:  string(signalsJSON),
		MaxTokens:    2048,
	})
	if err != nil {
		return nil, fmt.Errorf("insights LLM call: %w", err)
	}

	report := parseGapCategoriesResponse(raw)
	if report == nil {
		return nil, fmt.Errorf("insights JSON parse: malformed response")
	}
	return report, nil
}

// parseGapCategoriesResponse parses the Claude Opus JSON output into an InsightsReport.
// Strips markdown fences if present. Missing categories become empty slices.
// Returns nil on malformed JSON.
func parseGapCategoriesResponse(raw string) *models.InsightsReport {
	raw = strings.TrimSpace(raw)
	// Extract the JSON object from anywhere in the response — handles markdown
	// fences, preamble prose, and any trailing content Claude may add.
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 {
		raw = raw[:i+1]
	}

	var loose struct {
		Summary              string   `json:"summary"`
		EnvironmentCleanup   []string `json:"environment_cleanup"`
		NoDetection          []string `json:"no_detection"`
		PoorTacticCoverage   []string `json:"poor_tactic_coverage"`
		WeakDetectionQuality []string `json:"weak_detection_quality"`
		AdvancedUseCases     []string `json:"advanced_use_cases"`
		MissingSourceAlerts  []string `json:"missing_source_alerts"`
	}
	if err := json.Unmarshal([]byte(raw), &loose); err != nil {
		return nil
	}

	coerce := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}

	return &models.InsightsReport{
		Summary: loose.Summary,
		GapCategories: models.GapCategories{
			EnvironmentCleanup:   coerce(loose.EnvironmentCleanup),
			NoDetection:          coerce(loose.NoDetection),
			PoorTacticCoverage:   coerce(loose.PoorTacticCoverage),
			WeakDetectionQuality: coerce(loose.WeakDetectionQuality),
			AdvancedUseCases:     coerce(loose.AdvancedUseCases),
			MissingSourceAlerts:  coerce(loose.MissingSourceAlerts),
		},
	}
}
