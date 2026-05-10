package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
- immediate_noise_candidates: unscoped logs_immediate security alerts with no entity filter
  [{name, query, trigger_count}] — trigger_count is absent when event data unavailable
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
- advanced_use_cases: flag up to 3 high-value upgrade opportunities — e.g. techniques covered only by threshold/count alerts where sequence or anomaly detection would improve precision, techniques in no_detection or weak_detection_quality that are high-value (credential access, lateral movement, exfiltration, persistence), or gaps where a multi-stage correlation across two or more covered techniques would catch an attack chain; always include the technique ID and name
- summary: prose only (no bullet points), 2-4 sentences
- immediate_noise_candidates: for each entry, assess whether the Lucene query targets a
  high-frequency event (common user actions, broad field matches, platform lifecycle events).
  If yes, flag in environment_cleanup with a specific recommendation to add app/subsystem
  scoping or an entity filter. If the query is narrow enough to be low-frequency by nature,
  do not flag it. Use trigger_count as a signal when present; reason from query semantics when absent.`

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
	eventCounts map[string]int,
	provider llm.Provider,
) (*models.InsightsReport, error) {
	if result == nil {
		return nil, nil
	}

	signals := buildStructuredSignals(result, alerts, integrations, mitreCoverage, eventCounts)
	signalsJSON, err := json.Marshal(signals)
	if err != nil {
		return nil, fmt.Errorf("insights signals marshal: %w", err)
	}

	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: gapAnalysisSystemPrompt,
		UserMessage:  string(signalsJSON),
		MaxTokens:    4096,
	})
	if err != nil {
		return nil, fmt.Errorf("insights LLM call: %w", err)
	}

	report := parseGapCategoriesResponse(raw)
	if report == nil {
		log.Printf("WARN [insights] malformed response (len=%d): %.200s", len(raw), raw)
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
