package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// MaxSuggestions is the hard cap on suggestions returned per technique.
const MaxSuggestions = 6

// Suggestion is a single alert recommendation returned by the LLM.
// Legacy fields (AlertName, QueryHint, Priority) retain their original JSON tags
// so old rows cached in the database before the rename still unmarshal correctly.
type Suggestion struct {
	Title            string   `json:"title"`
	LogSource        string   `json:"log_source"`
	Description      string   `json:"description"`
	LuceneQuery      string   `json:"lucene_query"`
	Severity         string   `json:"severity"`
	SigmaRule        string   `json:"sigma_rule,omitempty"`
	LogSourceProduct string   `json:"log_source_product,omitempty"`
	Window           string   `json:"window,omitempty"`
	WindowReason     string   `json:"window_reason,omitempty"`
	Falsepositives   []string `json:"falsepositives,omitempty"`
	MitreTechniqueID string   `json:"mitre_technique_id,omitempty"`
	// Legacy read-only — populated only when reading pre-rename cached rows.
	AlertName string `json:"alert_name,omitempty"`
	QueryHint string `json:"query_hint,omitempty"`
	Priority  string `json:"priority,omitempty"`
}

// SuggestionsResult is the response from the suggestion engine.
type SuggestionsResult struct {
	Provider    string       `json:"provider"`
	Suggestions []Suggestion `json:"suggestions"`
}

// TechniqueInput describes the single technique to generate suggestions for.
type TechniqueInput struct {
	ID     string
	Name   string
	Tactic string
}

// GapInput describes the context for generating alert suggestions.
type GapInput struct {
	// LogSources are the client's available log sources (from Monday + alert data sources).
	LogSources []string
	// Technique is the single uncovered technique to suggest alerts for.
	Technique TechniqueInput
}

const systemPrompt = `You are a SIEM detection engineering expert specialising in MITRE ATT&CK coverage.

Your job: given a client's available log sources and ONE specific uncovered MITRE ATT&CK technique, suggest up to 6 concrete detection alerts that can detect this technique using the available logs.

Rules:
- Only suggest alerts REALISTICALLY detectable from the available log sources
- Each suggestion must reference a specific log source the client already has
- Alert title must follow the pattern "<Verb> <Subject> via <Method>" — e.g. "Detect Credential Dump via LSASS Memory Access"
- Suggest DIFFERENT detection approaches (different log sources, different indicators)
- Prefer a partial or imperfect alert over returning nothing
- Return at most 6 suggestions, ordered by detection quality (best first)
- Severity: critical = direct code execution/exfil; high = priv-esc/lateral/cred theft; medium = discovery/persistence; low = informational anomaly
- Correlation window by stage: 1m = execution; 5m = persistence/priv-esc; 15m = initial access; 30m = lateral/C2; 6h = discovery

FIELD NAMES — STRICT RULE:
All field names in lucene_query and sigma detection blocks MUST use ECS (Elastic Common Schema) paths.
NEVER use vendor-specific field names. The logsource.product field in the Sigma block handles vendor translation automatically.

FORBIDDEN vendor fields → correct ECS replacement:
  CrowdStrike  event_type:ProcessRollup2           → event.action:"Process" or event.category:"process"
  CrowdStrike  process_name / CommandLine           → process.name / process.command_line
  CrowdStrike  ParentImageFileName                  → process.parent.executable
  GuardDuty    detail.type                          → event.action
  GuardDuty    detail.service.action.*              → destination.ip, destination.port, source.ip
  GuardDuty    detail.resource.instanceDetails.*    → cloud.instance.id, host.id
  Sysmon       EventID:1/3/11                       → handled by logsource.service:sysmon; use process.name, network.*, file.path
  Sysmon       TargetFilename / SourceImage         → file.path / process.executable
  Windows      EventID:4624/4625                    → handled by logsource.service:security; use event.action, user.name
  Windows      SubjectUserName / TargetUserName     → user.name / user.target.name
  Okta         eventType / outcome.result           → event.action / event.outcome
  Okta         actor.id / target[].id               → user.id / user.target.id
  CloudTrail   eventName / sourceIPAddress          → event.action / source.ip
  CloudTrail   userIdentity.arn / requestParameters → aws.cloudtrail.user_identity.arn (ECS extension)
  K8s          requestObject / responseObject        → kubernetes.audit.requestObject (ECS extension)

Correct ECS examples:
  process.name:"powershell.exe" AND process.command_line:*-enc*
  event.action:"user.session.start" AND user.name:* AND source.ip:*
  file.path:*\\AppData\\Roaming\\* AND process.name:"wscript.exe"
  destination.port:4444 AND network.direction:"egress"

Respond ONLY with a JSON array. No markdown, no explanation.
Each object must have exactly these fields:
{
  "title": "Detect <Subject> via <Method>",
  "log_source": "Human-readable log source name",
  "log_source_product": "vendor-slug (windows|okta|crowdstrike-falcon|cloudtrail|etc.)",
  "description": "What the alert detects and why it maps to this technique",
  "lucene_query": "Lucene query using ECS field paths",
  "sigma_rule": "Full Sigma YAML block as a single string with title, status, logsource (product), detection, falsepositives, level, and tags",
  "window": "correlation window e.g. 5m",
  "window_reason": "One sentence explaining the window choice",
  "severity": "critical|high|medium|low",
  "falsepositives": ["At least one realistic false positive scenario"],
  "mitre_technique_id": "T..."
}`

// GenerateSuggestions uses the LLM to suggest alerts for one uncovered technique.
func GenerateSuggestions(ctx context.Context, provider Provider, input GapInput) (*SuggestionsResult, error) {
	userMsg := buildUserMessage(input)

	log.Printf("INFO [suggestions] requesting %s for technique %s (%s) with %d log sources",
		provider.Name(), input.Technique.ID, input.Technique.Name, len(input.LogSources))

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: systemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    16384,
		FastMode:     true, // disable thinking/reasoning mode — suggestions need speed, not chain-of-thought
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	suggestions, err := parseSuggestions(resp)
	if err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}

	suggestions = filterValidSuggestions(suggestions)

	// Hard cap at MaxSuggestions
	if len(suggestions) > MaxSuggestions {
		suggestions = suggestions[:MaxSuggestions]
	}

	return &SuggestionsResult{
		Provider:    provider.Name(),
		Suggestions: suggestions,
	}, nil
}

func buildUserMessage(input GapInput) string {
	var sb strings.Builder

	sb.WriteString("## Available Log Sources\n")
	sb.WriteString("The client has these log sources onboarded in Coralogix:\n")
	for _, ls := range input.LogSources {
		sb.WriteString("- ")
		sb.WriteString(ls)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\n## Uncovered Technique\n"))
	sb.WriteString(fmt.Sprintf("**%s: %s** (Tactic: %s)\n\n",
		input.Technique.ID, input.Technique.Name, input.Technique.Tactic))
	sb.WriteString("Suggest up to 6 alerts that can detect this technique using the available log sources above.")

	return sb.String()
}

func parseSuggestions(raw string) ([]Suggestion, error) {
	// Strip markdown code fences if present
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx > 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	// Sanitize literal control characters inside JSON strings.
	// Some models (e.g. minimax-m2.7) emit raw newlines/tabs inside string
	// values, which json.Unmarshal rejects as invalid.
	cleaned = sanitizeJSONStrings(cleaned)

	var suggestions []Suggestion
	if err := json.Unmarshal([]byte(cleaned), &suggestions); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w\nRaw response:\n%s", err, raw[:min(len(raw), 500)])
	}

	return suggestions, nil
}

// sanitizeJSONStrings replaces literal control characters inside JSON string
// values with their JSON escape sequences. It uses a simple state machine and
// only modifies characters that are inside a quoted string, leaving structural
// whitespace (newlines between keys/values) untouched.
func sanitizeJSONStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for _, c := range s {
		if escaped {
			b.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' && inString {
			b.WriteRune(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteRune(c)
			continue
		}
		if inString {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				b.WriteRune(c)
			}
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
