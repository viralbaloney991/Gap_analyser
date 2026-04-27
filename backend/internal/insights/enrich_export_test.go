package insights

import (
	"context"
	"errors"
	"testing"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

type mockExportProvider struct {
	response string
	err      error
}

func (m *mockExportProvider) Complete(_ context.Context, _ llm.CompletionRequest) (string, error) {
	return m.response, m.err
}
func (m *mockExportProvider) Name() string { return "mock-export" }

var validExportJSON = `{
  "executive_summary": "The client has strong coverage in initial-access tactics with 12 alerts. However, significant gaps exist in lateral movement and collection techniques. Noise levels are elevated with 8 alerts triggering frequently without scoping. Overall posture requires prioritized remediation of the top gaps.",
  "key_findings": [
    "MITRE coverage at 34.5% across 14 tactics",
    "8 noise alerts identified, 3 behavioral",
    "12 no-detection gaps in critical techniques"
  ],
  "recommended_actions": [
    "Add T1078 detection on AWS CloudTrail with high severity",
    "Scope noise alerts to specific subsystems to reduce false positives"
  ]
}`

func exportInsightsReportFixture() *models.InsightsReport {
	return &models.InsightsReport{
		GapCategories: models.GapCategories{
			NoDetection:          []string{"Build detections for T1078", "Build detections for T1059"},
			WeakDetectionQuality: []string{"Improve T1136 detection scoping"},
		},
		ActionableGaps: &models.ActionableGapCategories{
			NoDetection: []models.ActionableRecommendation{
				{Prose: "Add T1078 detection on AWS CloudTrail.", LogSource: "AWS CloudTrail", Severity: "high", QuerySkeleton: "eventSource=iam.amazonaws.com"},
			},
		},
	}
}

func exportMitreCoverageFixture() *models.MITRECoverageResult {
	return &models.MITRECoverageResult{
		Summary: models.MITRECoverageSummary{
			CoveragePercent: 34.5,
			TacticBreakdown: map[string]models.TacticCoverage{
				"initial-access": {TacticName: "Initial Access", Covered: 3, Total: 9, Percent: 33.3},
			},
		},
	}
}

func exportSimilarityFixture() *models.SimilarityResult {
	return &models.SimilarityResult{
		NoiseAlerts: []models.NoiseAlert{
			{Name: "Noisy Alert 1"},
			{Name: "Noisy Alert 2"},
		},
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"Alert A", "Alert B"}},
		},
	}
}

func TestEnrichExportNarrative_validResponse_parsed(t *testing.T) {
	provider := &mockExportProvider{response: validExportJSON}
	report, err := EnrichExportNarrative(
		context.Background(),
		nil,
		nil,
		exportMitreCoverageFixture(),
		exportSimilarityFixture(),
		exportInsightsReportFixture(),
		provider,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.ExecutiveSummary == "" {
		t.Error("expected non-empty ExecutiveSummary")
	}
	if len(report.KeyFindings) != 3 {
		t.Errorf("expected 3 key findings, got %d", len(report.KeyFindings))
	}
	if len(report.RecommendedActions) != 2 {
		t.Errorf("expected 2 recommended actions, got %d", len(report.RecommendedActions))
	}
}

func TestEnrichExportNarrative_nilInsightsReport_returnsNilNil(t *testing.T) {
	provider := &mockExportProvider{response: validExportJSON}
	report, err := EnrichExportNarrative(
		context.Background(),
		nil,
		nil,
		exportMitreCoverageFixture(),
		exportSimilarityFixture(),
		nil,
		provider,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report != nil {
		t.Errorf("expected nil report, got %v", report)
	}
}

func TestEnrichExportNarrative_malformedJSON_returnsError(t *testing.T) {
	provider := &mockExportProvider{response: `{not valid json`}
	_, err := EnrichExportNarrative(
		context.Background(),
		nil,
		nil,
		exportMitreCoverageFixture(),
		exportSimilarityFixture(),
		exportInsightsReportFixture(),
		provider,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnrichExportNarrative_llmError_propagates(t *testing.T) {
	provider := &mockExportProvider{err: errors.New("LLM timeout")}
	_, err := EnrichExportNarrative(
		context.Background(),
		nil,
		nil,
		exportMitreCoverageFixture(),
		exportSimilarityFixture(),
		exportInsightsReportFixture(),
		provider,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnrichExportNarrative_markdownFence_stripped(t *testing.T) {
	fenced := "```json\n" + validExportJSON + "\n```"
	provider := &mockExportProvider{response: fenced}
	report, err := EnrichExportNarrative(
		context.Background(),
		nil,
		nil,
		exportMitreCoverageFixture(),
		exportSimilarityFixture(),
		exportInsightsReportFixture(),
		provider,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
}
