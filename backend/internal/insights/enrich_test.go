package insights

import (
	"context"
	"errors"
	"strings"
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

func TestEnrich_nilResult_returnsNilNil(t *testing.T) {
	report, err := Enrich(context.Background(), nil, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil; got %v, %v", report, err)
	}
}

func TestEnrich_emptyResult_returnsNilNil(t *testing.T) {
	result := &models.SimilarityResult{}
	report, err := Enrich(context.Background(), result, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil for empty result; got %v, %v", report, err)
	}
}

func TestEnrich_validResponse_parsesReport(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"A", "B"}, Similarity: 0.95},
		},
	}
	jsonResp := `{
		"summary": "Test summary",
		"top_priority": ["Fix A"],
		"strengths": ["Good B"],
		"recommendations": ["Add C"],
		"enriched_dups": ["A and B overlap in login detection"],
		"enriched_gaps": []
	}`
	report, err := Enrich(context.Background(), result, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Summary != "Test summary" {
		t.Errorf("expected summary \"Test summary\", got %q", report.Summary)
	}
	if len(report.TopPriority) != 1 || report.TopPriority[0] != "Fix A" {
		t.Errorf("unexpected top_priority: %v", report.TopPriority)
	}
	if len(report.EnrichedDups) != 1 {
		t.Errorf("expected 1 enriched dup, got %d", len(report.EnrichedDups))
	}
}

func TestEnrich_llmError_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	report, err := Enrich(context.Background(), result, nil, &mockProvider{err: errors.New("network error")})
	if report != nil {
		t.Error("expected nil report on error")
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEnrich_invalidJSON_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	report, err := Enrich(context.Background(), result, nil, &mockProvider{response: "not json at all"})
	if report != nil {
		t.Error("expected nil report on parse error")
	}
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestEnrich_markdownFence_stripped(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	jsonResp := "```json\n{\"summary\":\"ok\",\"top_priority\":[],\"strengths\":[],\"recommendations\":[],\"enriched_dups\":[],\"enriched_gaps\":[]}\n```"
	report, err := Enrich(context.Background(), result, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil || report.Summary != "ok" {
		t.Errorf("expected summary \"ok\", got %v", report)
	}
}

func TestBuildPrompt_includesNoiseReason(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"A", "B"}, Similarity: 0.92},
		},
		NoiseAlerts: []models.NoiseAlert{
			{
				Name:            "SparseAlert",
				MissingFeatures: []string{"entities", "actions"},
				Reason:          "No monitored entity. No behavioral signal.",
			},
		},
	}
	prompt := buildPrompt(result, nil)
	if !strings.Contains(prompt, "SparseAlert") {
		t.Error("prompt should contain noise alert name")
	}
	if !strings.Contains(prompt, "No monitored entity") {
		t.Error("prompt should contain noise reason")
	}
	if !strings.Contains(prompt, "entities") {
		t.Error("prompt should contain missing features")
	}
}
