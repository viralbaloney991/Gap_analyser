package llm

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/mitre"
)

var (
	classifierSysPromptOnce  sync.Once
	classifierSysPromptCache string
)

func buildClassifierSystemPrompt() string {
	classifierSysPromptOnce.Do(func() {
		classifierSysPromptCache = `You are a MITRE ATT&CK expert. Given a security alert, identify which techniques from the JSON below the alert actively detects.

Rules:
- Return ONLY a JSON array of technique IDs, e.g. ["T1078.004", "T1110"]
- Only use IDs that appear in the JSON — no others
- Sub-techniques use suffix keys: suffix "001" under parent "T1059" means technique ID "T1059.001"
- Prefer sub-techniques over parent techniques when the alert is specific enough
- Return at most 5 technique IDs
- If the alert does not clearly detect any listed technique, return []
- No markdown, no explanation, no other text

MITRE ATT&CK techniques grouped by tactic (compact JSON):
` + mitre.BuildTechniqueJSON()
	})
	return classifierSysPromptCache
}

func buildClassifierMessage(inp AlertInput) string {
	var sb strings.Builder
	sb.WriteString("Alert: ")
	sb.WriteString(inp.Name)
	if inp.App != "" {
		sb.WriteString(" | App: ")
		sb.WriteString(inp.App)
	}
	if inp.Subsystem != "" {
		sb.WriteString(" | Subsystem: ")
		sb.WriteString(inp.Subsystem)
	}
	if inp.Query != "" {
		sb.WriteString("\nQuery: ")
		sb.WriteString(inp.Query)
	}
	return sb.String()
}

func parseClassifierResponse(raw string) []string {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var ids []string
	if err := json.Unmarshal([]byte(cleaned), &ids); err != nil {
		log.Printf("WARN [classifier] parse response: %v (raw: %.100s)", err, raw)
		return nil
	}

	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if mitre.ValidTechniqueID(id) {
			valid = append(valid, id)
		} else {
			log.Printf("DEBUG [classifier] dropped unknown technique ID: %q", id)
		}
	}
	return valid
}

func classifySingle(ctx context.Context, provider Provider, inp AlertInput) []string {
	req := CompletionRequest{
		SystemPrompt: buildClassifierSystemPrompt(),
		UserMessage:  buildClassifierMessage(inp),
		MaxTokens:    256,
		FastMode:     true,
	}
	resp, err := provider.Complete(ctx, req)
	if err != nil {
		log.Printf("WARN [classifier] alert=%s: %v", inp.ID, err)
		return nil
	}
	result := parseClassifierResponse(resp)
	if result == nil {
		return nil
	}
	return result
}
