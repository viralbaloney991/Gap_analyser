package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const geminiAPIBase = "https://generativelanguage.googleapis.com/v1beta/models"

// gemini free tier: 15 RPM. Retry up to 4 times with exponential backoff on 429.
const geminiMaxRetries = 4

type geminiProvider struct {
	apiKey string
	model  string
}

func (g *geminiProvider) Name() string { return "Gemini" }

func (g *geminiProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}

	body := map[string]any{
		"contents": []content{
			{Role: "user", Parts: []part{{Text: req.UserMessage}}},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": maxTokens,
		},
	}
	if req.SystemPrompt != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []part{{Text: req.SystemPrompt}},
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPIBase, g.model, g.apiKey)

	backoff := 5 * time.Second
	for attempt := 0; attempt <= geminiMaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("INFO [gemini] rate limited, waiting %s before retry %d/%d", backoff, attempt, geminiMaxRetries)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		log.Printf("INFO [gemini] sending request (model=%s, attempt=%d)", g.model, attempt+1)

		resp, err := llmHTTPClient.Do(httpReq)
		if err != nil {
			return "", fmt.Errorf("gemini API: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == geminiMaxRetries {
				return "", fmt.Errorf("gemini API rate limit exceeded after %d retries", geminiMaxRetries)
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var result struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("parse gemini response: %w", err)
		}

		if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
			return "", fmt.Errorf("gemini returned empty response")
		}

		text := result.Candidates[0].Content.Parts[0].Text
		log.Printf("INFO [gemini] response=%d chars", len(text))
		return text, nil
	}

	return "", fmt.Errorf("gemini API rate limit exceeded after %d retries", geminiMaxRetries)
}
