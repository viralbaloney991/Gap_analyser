// backend/internal/llm/detection_builder.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"coralogix-alert-analyzer/internal/models"
)

// BuildTechnique is the internal representation used by the LLM package.
type BuildTechnique struct {
	ID          string
	Name        string
	TacticID    string
	TacticName  string
	TacticOrder int
	Source      string
}

const buildDetectionSystemPrompt = `You are a senior detection engineer at a SOC. A user has selected MITRE ATT\&CK techniques to build a multi-stage detection chain.

Your job:
1. VALIDATE the chain represents a coherent attacker kill-chain (Recon < Initial Access < Execution < Persistence < Priv Esc < Evasion < Cred Access < Discovery < Lateral < Collection < C2 < Exfiltration < Impact). Flag missing steps or implausible sequences.
2. PROPOSE 3–4 flow alerts that together would catch this attacker behaviour.
3. For each alert provide:
   - name: short stage label e.g. "Stage 1: Valid Account Login"
   - description: one-line summary
   - techniqueId: the MITRE T-id this alert primarily detects (from the user's list)
   - logic: plain-English detection logic, one sentence
   - window: realistic per-stage correlation window. Use "5m" for execution/cred-access; "15m" for initial access; "30m" for evasion; "1h" for persistence; "6h" for lateral/discovery; "12h" for collection; "24h" for C2/exfil.
   - windowReason: one sentence explaining the time window choice (this surfaces in the UI)
   - source: telemetry source — one of "EDR", "CloudTrail", "IdP", "Email", "Network", "WAF"
   - severity: "critical" | "high" | "medium" | "low"
4. One CORRELATION RULE tying all flow alerts together with the longest plausible attacker dwell window ("1h" | "24h" | "72h").
5. VALIDATION findings: list issues, warnings, or confirmations.

OUTPUT STRICT JSON ONLY — no prose, no markdown fences, just the object:
{
  "validation": {
    "verdict": "ok" | "warnings" | "invalid",
    "findings": [{"level": "info"|"warn"|"error", "message": "..."}]
  },
  "alerts": [
    {
      "name": "...", "description": "...", "techniqueId": "T...",
      "logic": "...", "window": "...", "windowReason": "...",
      "source": "...", "severity": "..."
    }
  ],
  "correlation": {
    "name": "...", "logic": "Alert 1 (T...) → Alert 2 (T...) within {window}",
    "window": "...", "severity": "critical" | "high"
  }
}`

func buildDetectionPrompt(techs []BuildTechnique) string {
	ordered := make([]BuildTechnique, len(techs))
	copy(ordered, techs)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].TacticOrder < ordered[j].TacticOrder
	})

	var sb strings.Builder
	sb.WriteString("USER'S SELECTED TECHNIQUES (kill-chain order):\n")
	for _, t := range ordered {
		sb.WriteString(fmt.Sprintf("  - %s %q — tactic: %s — telemetry: %s\n",
			t.ID, t.Name, t.TacticName, t.Source))
	}
	return sb.String()
}

// GenerateDetection calls the LLM and returns a BuildDetectionResponse.
// Falls back to mockBuildDetection if the LLM call or JSON parse fails.
func GenerateDetection(ctx context.Context, provider Provider, techs []BuildTechnique) (*models.BuildDetectionResponse, error) {
	userMsg := buildDetectionPrompt(techs)
	log.Printf("INFO [detection_builder] requesting %s for %d techniques", provider.Name(), len(techs))

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: buildDetectionSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    4096,
		FastMode:     true,
	})
	if err != nil {
		log.Printf("WARN [detection_builder] LLM error: %v — using mock fallback", err)
		mock := mockBuildDetection(techs)
		return mock, nil
	}

	result, err := parseDetectionResult(resp)
	if err != nil {
		log.Printf("WARN [detection_builder] parse error: %v — using mock fallback", err)
		mock := mockBuildDetection(techs)
		return mock, nil
	}
	return result, nil
}

func parseDetectionResult(raw string) (*models.BuildDetectionResponse, error) {
	cleaned := strings.TrimSpace(raw)
	// Strip markdown fences if present.
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
	// Find the outermost JSON object.
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	cleaned = cleaned[start : end+1]
	cleaned = sanitizeJSONStrings(cleaned) // reuse existing helper from suggestions.go

	var result models.BuildDetectionResponse
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("JSON parse: %w\nraw: %s", err, raw[:min(len(raw), 400)])
	}
	return &result, nil
}

// mockBuildDetection returns a deterministic response without calling the LLM.
func mockBuildDetection(techs []BuildTechnique) *models.BuildDetectionResponse {
	ordered := make([]BuildTechnique, len(techs))
	copy(ordered, techs)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].TacticOrder < ordered[j].TacticOrder
	})

	windowByTactic := map[string]struct{ w, why string }{
		"TA0043": {"1h", "Recon usually precedes access by an hour or less."},
		"TA0001": {"15m", "Initial access events are immediate; correlate within 15 minutes."},
		"TA0002": {"5m", "Execution should follow access within minutes."},
		"TA0003": {"1h", "Persistence is typically established within an hour of access."},
		"TA0004": {"6h", "Privilege escalation often happens within a working session."},
		"TA0005": {"30m", "Defense evasion clusters around execution and persistence."},
		"TA0006": {"15m", "Credential access is usually rapid once execution succeeds."},
		"TA0007": {"6h", "Discovery commands run during the attacker reconnaissance window."},
		"TA0008": {"6h", "Lateral movement happens hours after initial foothold."},
		"TA0009": {"12h", "Collection precedes exfiltration by hours."},
		"TA0011": {"24h", "C2 channels persist as long as the attacker has a foothold."},
		"TA0010": {"24h", "Exfiltration is the final stage; window covers staged extraction."},
		"TA0040": {"15m", "Impact actions are tightly clustered once triggered."},
	}
	sevByTactic := map[string]string{
		"TA0001": "high", "TA0002": "medium", "TA0003": "high", "TA0004": "high",
		"TA0005": "medium", "TA0006": "critical", "TA0007": "low", "TA0008": "high",
		"TA0009": "medium", "TA0011": "medium", "TA0010": "critical", "TA0040": "critical",
	}

	cap := len(ordered)
	if cap > 4 {
		cap = 4
	}
	alerts := make([]models.BuildDetectionAlert, cap)
	for i, t := range ordered[:cap] {
		wt := windowByTactic[t.TacticID]
		if wt.w == "" {
			wt = struct{ w, why string }{"1h", "Standard correlation window for this stage."}
		}
		src := strings.SplitN(t.Source, "/", 2)[0]
		alerts[i] = models.BuildDetectionAlert{
			Name:         fmt.Sprintf("Stage %d: %s", i+1, t.Name),
			Description:  fmt.Sprintf("Detect %s activity (%s).", strings.ToLower(t.Name), t.TacticName),
			TechniqueID:  t.ID,
			Logic:        fmt.Sprintf("Event matching %s pattern observed via %s.", t.ID, t.Source),
			Window:       wt.w,
			WindowReason: wt.why,
			Source:       strings.TrimSpace(src),
			Severity:     sevByTactic[t.TacticID],
		}
		if alerts[i].Severity == "" {
			alerts[i].Severity = "medium"
		}
	}

	// Validation findings.
	tacticIDs := make(map[string]bool)
	for _, t := range ordered {
		tacticIDs[t.TacticID] = true
	}
	var findings []models.BuildDetectionFinding
	if !tacticIDs["TA0001"] && len(tacticIDs) > 1 {
		findings = append(findings, models.BuildDetectionFinding{
			Level:   "warn",
			Message: "No Initial Access technique — chain starts mid-attack.",
		})
	}
	if tacticIDs["TA0010"] && !tacticIDs["TA0009"] {
		findings = append(findings, models.BuildDetectionFinding{
			Level:   "info",
			Message: "Exfiltration without Collection — consider adding T1530.",
		})
	}
	if len(findings) == 0 {
		findings = append(findings, models.BuildDetectionFinding{
			Level:   "info",
			Message: "Chain is sequenced correctly across the kill-chain.",
		})
	}
	verdict := "ok"
	for _, f := range findings {
		if f.Level == "error" {
			verdict = "invalid"
			break
		} else if f.Level == "warn" {
			verdict = "warnings"
		}
	}

	dwell := len(ordered)
	corrWindow := "1h"
	if dwell >= 4 {
		corrWindow = "72h"
	} else if dwell >= 3 {
		corrWindow = "24h"
	}

	ids := make([]string, len(ordered))
	for i, t := range ordered {
		ids[i] = t.ID
	}
	logicParts := make([]string, len(alerts))
	for i, a := range alerts {
		logicParts[i] = fmt.Sprintf("Alert %d (%s)", i+1, a.TechniqueID)
	}
	corrSev := "high"
	for _, a := range alerts {
		if a.Severity == "critical" {
			corrSev = "critical"
			break
		}
	}

	var result models.BuildDetectionResponse
	result.Validation.Verdict = verdict
	result.Validation.Findings = findings
	result.Alerts = alerts
	result.Correlation = models.BuildDetectionCorrelation{
		Name:     "Multi-stage chain: " + strings.Join(ids, " → "),
		Logic:    strings.Join(logicParts, " → ") + " within " + corrWindow,
		Window:   corrWindow,
		Severity: corrSev,
	}
	return &result
}
