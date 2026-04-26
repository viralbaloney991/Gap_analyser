package api

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestResolveInsightsProvider_UsesClaudeOpus(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			InsightsProvider: "claude",
			InsightsModel:    "claude-opus-4-7",
			AnthropicAPIKey:  "test-key",
		},
	}
	p, err := resolveInsightsProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "Claude" {
		t.Errorf("expected provider name Claude, got %q", p.Name())
	}
}

func TestResolveInsightsProvider_FallsBackToDefaultProvider(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			InsightsProvider: "",
			DefaultProvider:  "claude",
			InsightsModel:    "claude-opus-4-7",
			AnthropicAPIKey:  "test-key",
		},
	}
	p, err := resolveInsightsProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "Claude" {
		t.Errorf("expected provider Claude on empty InsightsProvider, got %q", p.Name())
	}
}
