package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var llmHTTPClient = &http.Client{Timeout: 180 * time.Second}

const (
	nvidiaMaxRetries       = 3
	nvidiaDefaultRetryWait = 5 * time.Second
)

// rateLimitError is returned by completeNonStreaming/completeStreaming on HTTP 429.
type rateLimitError struct {
	wait time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.wait)
}

// parseRetryAfter reads the Retry-After header and returns the wait duration.
// Defaults to nvidiaDefaultRetryWait if the header is absent or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return nvidiaDefaultRetryWait
}

type nvidiaProvider struct {
	apiKey   string
	model    string
	endpoint string
}

func (n *nvidiaProvider) Name() string { return "NVIDIA NIM" }

// Complete uses the OpenAI-compatible chat completions API.
// FastMode uses non-streaming (faster, no thinking budget) — used for suggestions.
// Normal mode uses streaming SSE to avoid header timeouts on long reasoning requests.
func (n *nvidiaProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.UserMessage})

	var lastErr error
	for attempt := 0; attempt < nvidiaMaxRetries; attempt++ {
		var text string
		var err error
		if req.FastMode {
			text, err = n.completeNonStreaming(ctx, messages, maxTokens)
		} else {
			text, err = n.completeStreaming(ctx, messages, maxTokens)
		}
		if err == nil {
			return text, nil
		}
		lastErr = err
		// Only retry on rate-limit errors; fail fast on everything else.
		re, ok := err.(*rateLimitError)
		if !ok {
			return "", err
		}
		log.Printf("WARN [nvidia] rate limited (attempt %d/%d), retrying in %s", attempt+1, nvidiaMaxRetries, re.wait)
		select {
		case <-time.After(re.wait):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("nvidia NIM: max retries exceeded: %w", lastErr)
}

func (n *nvidiaProvider) completeNonStreaming(ctx context.Context, messages []map[string]string, maxTokens int) (string, error) {
	body := map[string]any{
		"model":       n.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": 0.6,
		"top_p":       0.95,
		"stream":      false,
	}
	payload, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", n.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+n.apiKey)

	log.Printf("INFO [nvidia] sending non-streaming request (model=%s, payload=%d bytes)", n.model, len(payload))

	resp, err := llmHTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("nvidia NIM API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", &rateLimitError{wait: parseRetryAfter(resp.Header)}
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("nvidia NIM API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse nvidia response: %w", err)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("nvidia NIM returned empty response")
	}

	text := result.Choices[0].Message.Content
	log.Printf("INFO [nvidia] non-streaming complete, response=%d chars", len(text))
	return text, nil
}

func (n *nvidiaProvider) completeStreaming(ctx context.Context, messages []map[string]string, maxTokens int) (string, error) {
	body := map[string]any{
		"model":                n.model,
		"max_tokens":           maxTokens,
		"messages":             messages,
		"temperature":          1,
		"top_p":                0.95,
		"stream": true,
	}
	payload, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", n.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+n.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	log.Printf("INFO [nvidia] sending streaming request (model=%s, payload=%d bytes)", n.model, len(payload))

	resp, err := llmHTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("nvidia NIM API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", &rateLimitError{wait: parseRetryAfter(resp.Header)}
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("nvidia NIM API returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Read SSE stream and accumulate content, skipping reasoning_content (thinking traces)
	var content strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content.WriteString(chunk.Choices[0].Delta.Content)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading stream: %w", err)
	}

	result := content.String()
	log.Printf("INFO [nvidia] streaming complete, response=%d chars", len(result))

	if result == "" {
		return "", fmt.Errorf("nvidia NIM returned empty response")
	}
	return result, nil
}
