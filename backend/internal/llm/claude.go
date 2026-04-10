package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const anthropicAPI = "https://api.anthropic.com/v1/messages"

type claudeProvider struct {
	apiKey string
	model  string
}

func (c *claudeProvider) Name() string { return "Claude" }

func (c *claudeProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	body := map[string]any{
		"model":      c.model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": req.UserMessage},
		},
	}
	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	payload, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", anthropicAPI, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("claude API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("claude API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse claude response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("claude returned empty content")
	}

	return result.Content[0].Text, nil
}
