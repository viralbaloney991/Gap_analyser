package insights

import (
	"context"
	"errors"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestEnrichActionable_emptyGaps_returnsNilNil(t *testing.T) {
	result, err := EnrichActionable(context.Background(), models.GapCategories{}, nil, &mockProvider{})
	if result != nil || err != nil {
		t.Errorf("expected nil, nil for empty gaps; got %v, %v", result, err)
	}
}

func TestEnrichActionable_validResponse_parsed(t *testing.T) {
	gaps := models.GapCategories{
		NoDetection: []string{"T1078: no coverage"},
	}
	integrations := []models.IntegrationInfo{{Name: "AWS CloudTrail", AlertCount: 0}}
	jsonResp := `{
		"no_detection": [{"prose": "Build T1078 detection on AWS CloudTrail.", "log_source": "AWS CloudTrail", "severity": "critical", "query_skeleton": "eventSource=iam.amazonaws.com AND eventName=CreateUser"}],
		"weak_detection_quality": [],
		"missing_source_alerts": [],
		"advanced_use_cases": []
	}`
	result, err := EnrichActionable(context.Background(), gaps, integrations, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.NoDetection) != 1 {
		t.Errorf("no_detection: want 1, got %d", len(result.NoDetection))
	}
	rec := result.NoDetection[0]
	if rec.LogSource != "AWS CloudTrail" {
		t.Errorf("log_source: want 'AWS CloudTrail', got %q", rec.LogSource)
	}
	if rec.Severity != "critical" {
		t.Errorf("severity: want 'critical', got %q", rec.Severity)
	}
	if rec.QuerySkeleton == "" {
		t.Error("query_skeleton must not be empty")
	}
	if rec.Prose == "" {
		t.Error("prose must not be empty")
	}
	if rec.Prose != "Build T1078 detection on AWS CloudTrail." {
		t.Errorf("prose: got %q", rec.Prose)
	}
	if len(result.WeakDetectionQuality) != 0 {
		t.Errorf("weak_detection_quality: want 0, got %d", len(result.WeakDetectionQuality))
	}
}

func TestEnrichActionable_nullCategoryCoercedToEmpty(t *testing.T) {
	gaps := models.GapCategories{NoDetection: []string{"T1059"}}
	jsonResp := `{"no_detection": [], "weak_detection_quality": null, "missing_source_alerts": [], "advanced_use_cases": []}`
	result, err := EnrichActionable(context.Background(), gaps, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WeakDetectionQuality == nil {
		t.Error("null weak_detection_quality must coerce to empty slice, not nil")
	}
}

func TestEnrichActionable_malformedJSON_returnsError(t *testing.T) {
	gaps := models.GapCategories{NoDetection: []string{"T1078"}}
	_, err := EnrichActionable(context.Background(), gaps, nil, &mockProvider{response: "not json"})
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestEnrichActionable_llmError_returnsError(t *testing.T) {
	gaps := models.GapCategories{NoDetection: []string{"T1078"}}
	_, err := EnrichActionable(context.Background(), gaps, nil, &mockProvider{err: errors.New("network error")})
	if err == nil {
		t.Error("expected error on LLM failure, got nil")
	}
}

func TestEnrichActionable_markdownWrapped_stripped(t *testing.T) {
	gaps := models.GapCategories{NoDetection: []string{"T1078"}}
	jsonResp := "```json\n{\"no_detection\": [{\"prose\": \"p\", \"log_source\": \"L\", \"severity\": \"high\", \"query_skeleton\": \"q\"}], \"weak_detection_quality\": [], \"missing_source_alerts\": [], \"advanced_use_cases\": []}\n```"
	result, err := EnrichActionable(context.Background(), gaps, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.NoDetection) != 1 {
		t.Errorf("no_detection: want 1 after markdown strip, got %d", len(result.NoDetection))
	}
}
