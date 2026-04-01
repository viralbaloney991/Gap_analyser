package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"coralogix-alert-analyzer/internal/models"

	"gopkg.in/yaml.v3"
)

// ClassifierConfig holds settings for the local MITRE classifier sidecar.
type ClassifierConfig struct {
	Endpoint string `yaml:"endpoint"` // e.g. "http://localhost:8001"
}

// Config holds the application configuration.
type Config struct {
	MondayAPIToken string                  `yaml:"monday_api_token"`
	MondayBoardID  int64                   `yaml:"monday_board_id"`
	Clients        map[string]ClientConfig `yaml:"clients"`
	LLM            LLMConfig               `yaml:"llm"`
	Classifier     ClassifierConfig        `yaml:"classifier"`
}

// LLMConfig holds settings for LLM-powered suggestions.
// API keys can also be set via ANTHROPIC_API_KEY / NVIDIA_API_KEY env vars.
type LLMConfig struct {
	DefaultProvider string `yaml:"default_provider"` // "claude" or "nvidia"
	AnthropicAPIKey string `yaml:"anthropic_api_key"`
	ClaudeModel     string `yaml:"claude_model"`
	NvidiaAPIKey    string `yaml:"nvidia_api_key"`
	NvidiaModel     string `yaml:"nvidia_model"`
	NvidiaEndpoint  string `yaml:"nvidia_endpoint"`

	// ClassifierProvider/Model is a fast model used only for MITRE technique
	// classification. Defaults to DefaultProvider + NvidiaModel/ClaudeModel if unset.
	ClassifierProvider string `yaml:"classifier_provider"`
	ClassifierModel    string `yaml:"classifier_model"`

	// ValidatorProvider/Model: Llama used to confirm/reject classifier candidates.
	ValidatorProvider string `yaml:"validator_provider"`
	ValidatorModel    string `yaml:"validator_model"`

	// SuggestionProvider/Model: model used for gap alert suggestion generation.
	SuggestionProvider string `yaml:"suggestion_provider"`
	SuggestionModel    string `yaml:"suggestion_model"`
}

// ClientConfig holds per-client settings.
type ClientConfig struct {
	APIKey        string `yaml:"api_key"`
	Region        string `yaml:"region"`
	MondayGroupID string `yaml:"monday_group_id"`
}

// Load reads and validates the config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.MondayAPIToken == "" {
		return nil, fmt.Errorf("config: monday_api_token is required")
	}
	if cfg.MondayBoardID == 0 {
		return nil, fmt.Errorf("config: monday_board_id is required")
	}

	for name, client := range cfg.Clients {
		if client.APIKey == "" {
			return nil, fmt.Errorf("config: client %q missing api_key", name)
		}
		region := strings.ToLower(client.Region)
		if _, ok := models.Regions[region]; !ok {
			return nil, fmt.Errorf("config: client %q has invalid region %q", name, client.Region)
		}
	}

	// LLM: env vars override yaml for API keys (secrets shouldn't be in yaml)
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.LLM.AnthropicAPIKey = v
	}
	if v := os.Getenv("NVIDIA_API_KEY"); v != "" {
		cfg.LLM.NvidiaAPIKey = v
	}
	if cfg.LLM.DefaultProvider == "" {
		cfg.LLM.DefaultProvider = "claude"
	}

	return &cfg, nil
}

// ClientNames returns sorted client names.
func (c *Config) ClientNames() []string {
	names := make([]string, 0, len(c.Clients))
	for name := range c.Clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
