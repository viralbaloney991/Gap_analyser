package insights

import (
	"context"
	"errors"
	"testing"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (string, error) {
	return m.response, m.err
}
func (m *mockProvider) Name() string { return "mock" }

// ── parseGapCategoriesResponse ────────────────────────────────────────────────

func TestParseGapCategoriesResponse_ValidJSON(t *testing.T) {
	raw := `{
		"summary": "Strong credential-access coverage.",
		"environment_cleanup": ["Alert A duplicates Alert B"],
		"no_detection": ["T1078: no coverage"],
		"poor_tactic_coverage": [],
		"weak_detection_quality": ["T1059 unscoped"],
		"advanced_use_cases": [],
		"missing_source_alerts": ["Azure AD: 0 alerts"]
	}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Summary != "Strong credential-access coverage." {
		t.Errorf("wrong summary: %q", report.Summary)
	}
	if len(report.GapCategories.EnvironmentCleanup) != 1 {
		t.Errorf("environment_cleanup: want 1, got %d", len(report.GapCategories.EnvironmentCleanup))
	}
	if len(report.GapCategories.NoDetection) != 1 {
		t.Errorf("no_detection: want 1, got %d", len(report.GapCategories.NoDetection))
	}
	if len(report.GapCategories.PoorTacticCoverage) != 0 {
		t.Errorf("poor_tactic_coverage: want empty slice, got %v", report.GapCategories.PoorTacticCoverage)
	}
	if report.GapCategories.WeakDetectionQuality[0] != "T1059 unscoped" {
		t.Errorf("weak_detection_quality: %v", report.GapCategories.WeakDetectionQuality)
	}
}

func TestParseGapCategoriesResponse_MissingCategory_FillsEmptySlice(t *testing.T) {
	raw := `{"summary": "partial", "no_detection": ["T1059"]}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil even with missing categories")
	}
	if report.GapCategories.EnvironmentCleanup == nil {
		t.Error("missing category must produce empty slice, not nil")
	}
	if len(report.GapCategories.EnvironmentCleanup) != 0 {
		t.Errorf("want 0 items, got %d", len(report.GapCategories.EnvironmentCleanup))
	}
}

func TestParseGapCategoriesResponse_MalformedJSON_ReturnsNil(t *testing.T) {
	report := parseGapCategoriesResponse("{not valid}")
	if report != nil {
		t.Error("want nil for malformed JSON")
	}
}

func TestParseGapCategoriesResponse_MarkdownWrapped_Stripped(t *testing.T) {
	raw := "```json\n{\"summary\": \"ok\", \"no_detection\": [\"T1059\"]}\n```"
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("should strip markdown fence and parse")
	}
	if report.Summary != "ok" {
		t.Errorf("wrong summary after strip: %q", report.Summary)
	}
}

func TestParseGapCategoriesResponse_NullCategoryCoercedToEmpty(t *testing.T) {
	raw := `{"summary": "ok", "no_detection": null, "environment_cleanup": []}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil")
	}
	if report.GapCategories.NoDetection == nil {
		t.Error("null no_detection should become empty slice")
	}
}

// ── Enrich ────────────────────────────────────────────────────────────────────

func TestEnrich_nilResult_returnsNilNil(t *testing.T) {
	report, err := Enrich(context.Background(), nil, nil, nil, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil; got %v, %v", report, err)
	}
}

func TestEnrich_emptyResult_returnsNilNil(t *testing.T) {
	result := &models.SimilarityResult{}
	report, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil for empty result; got %v, %v", report, err)
	}
}

func TestEnrich_validResponse_parsesGapCategories(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"A", "B"}, Similarity: 0.95},
		},
	}
	jsonResp := `{
		"summary": "Good baseline coverage.",
		"environment_cleanup": [],
		"no_detection": ["T1078: no alerts"],
		"poor_tactic_coverage": ["Reconnaissance: 0%"],
		"weak_detection_quality": [],
		"advanced_use_cases": [],
		"missing_source_alerts": []
	}`
	report, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Summary != "Good baseline coverage." {
		t.Errorf("wrong summary: %q", report.Summary)
	}
	if len(report.GapCategories.NoDetection) != 1 {
		t.Errorf("no_detection: want 1, got %d", len(report.GapCategories.NoDetection))
	}
	if len(report.GapCategories.PoorTacticCoverage) != 1 {
		t.Errorf("poor_tactic_coverage: want 1, got %d", len(report.GapCategories.PoorTacticCoverage))
	}
}

func TestEnrich_llmError_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	_, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{err: errors.New("network error")})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEnrich_invalidJSON_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	_, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: "not json"})
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
