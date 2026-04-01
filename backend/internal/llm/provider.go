package llm

import (
	"context"
	"fmt"
)

// Provider is the interface for LLM backends.
type Provider interface {
	// Complete sends a prompt and returns the model's response text.
	Complete(ctx context.Context, req CompletionRequest) (string, error)
	// Name returns the provider display name (e.g. "Claude", "NVIDIA NIM").
	Name() string
}

// CompletionRequest is a model-agnostic chat completion request.
type CompletionRequest struct {
	SystemPrompt string
	UserMessage  string
	MaxTokens    int
	// FastMode disables extended thinking/reasoning for providers that support it.
	// Set this for classification tasks where speed matters over depth.
	FastMode bool
}

// NewProvider creates an LLM provider by name.
// Supported: "claude", "nvidia", "gemini".
func NewProvider(name string, cfg ProviderConfig) (Provider, error) {
	switch name {
	case "claude":
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is required for Claude provider")
		}
		return &claudeProvider{
			apiKey: cfg.AnthropicAPIKey,
			model:  withDefault(cfg.ClaudeModel, "claude-haiku-4-5-20251001"),
		}, nil
	case "nvidia":
		if cfg.NvidiaAPIKey == "" {
			return nil, fmt.Errorf("NVIDIA_API_KEY is required for NVIDIA NIM provider")
		}
		return &nvidiaProvider{
			apiKey:   cfg.NvidiaAPIKey,
			model:    withDefault(cfg.NvidiaModel, "mistralai/mistral-large-3-675b-instruct-2512"),
			endpoint: withDefault(cfg.NvidiaEndpoint, "https://integrate.api.nvidia.com/v1/chat/completions"),
		}, nil
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY is required for Gemini provider")
		}
		return &geminiProvider{
			apiKey: cfg.GeminiAPIKey,
			model:  withDefault(cfg.GeminiModel, "gemini-2.0-flash"),
		}, nil
	default:
		return nil, fmt.Errorf("unknown LLM provider: %q (supported: claude, nvidia, gemini)", name)
	}
}

// ProviderConfig holds credentials and model settings for all providers.
type ProviderConfig struct {
	AnthropicAPIKey string
	ClaudeModel     string
	NvidiaAPIKey    string
	NvidiaModel     string
	NvidiaEndpoint  string
	GeminiAPIKey    string
	GeminiModel     string
}

// NewClassifierProvider builds a provider for fast classification tasks using
// the classifier_provider/classifier_model config fields (falling back to defaults).
func NewClassifierProvider(classifierProvider, classifierModel string, base ProviderConfig) (Provider, error) {
	if classifierProvider == "" {
		// Determine default from which keys are set
		if base.NvidiaAPIKey != "" {
			classifierProvider = "nvidia"
		} else {
			classifierProvider = "claude"
		}
	}
	cfg := base
	switch classifierProvider {
	case "nvidia":
		if classifierModel != "" {
			cfg.NvidiaModel = classifierModel
		}
	case "claude":
		if classifierModel != "" {
			cfg.ClaudeModel = classifierModel
		}
	case "gemini":
		if classifierModel != "" {
			cfg.GeminiModel = classifierModel
		}
	}
	return NewProvider(classifierProvider, cfg)
}

func withDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
