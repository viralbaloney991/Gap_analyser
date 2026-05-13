package api

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// ── Data models ──────────────────────────────────────────────────────────────

type huntLogEvent struct {
	Timestamp string `json:"timestamp"`
	Host      string `json:"host"`
	User      string `json:"user"`
	Command   string `json:"command"`
}

type queryDoneData struct {
	Hits         int            `json:"hits"`
	Hosts        int            `json:"hosts"`
	LastSeen     string         `json:"last_seen"`
	UniqueUsers  int            `json:"unique_users"`
	SampleEvents []huntLogEvent `json:"sample_events"`
	CxCommand    string         `json:"cx_command"`
}

type ollyDoneData struct {
	Sections map[string]string `json:"sections"`
}

type huntFinding struct {
	Text     string `json:"text"`
	Severity string `json:"severity"`
}

type huntAction struct {
	Priority    int    `json:"priority"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:"level"`
}

type huntAlertDef struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Condition string `json:"condition"`
	Severity  string `json:"severity"`
	GroupBy   string `json:"group_by"`
}

type huntStats struct {
	Hits         string `json:"hits"`
	Hosts        string `json:"hosts"`
	AttackWindow string `json:"attack_window"`
	C2Flagged    bool   `json:"c2_flagged"`
}

type huntReport struct {
	Verdict       string        `json:"verdict"`
	Confidence    string        `json:"confidence"`
	Title         string        `json:"title"`
	Subtitle      string        `json:"subtitle"`
	Stats         huntStats     `json:"stats"`
	Findings      []huntFinding `json:"findings"`
	Actions       []huntAction  `json:"actions"`
	AlertDef      huntAlertDef  `json:"alert_def"`
	RunDurationMs int64         `json:"run_duration_ms"`
	Timestamp     string        `json:"timestamp"`
}

type huntErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Input sanitization ───────────────────────────────────────────────────────

var queryAllowlist = regexp.MustCompile(`^[\x20-\x7E]+$`)
var queryForbidden = regexp.MustCompile(`[$` + "`" + `;|\\&><\n\r]`)

func sanitizeQuery(q string) error {
	if len(q) > 1000 {
		return errors.New("query exceeds 1000 character limit")
	}
	if !queryAllowlist.MatchString(q) {
		return errors.New("query contains non-printable characters")
	}
	if queryForbidden.MatchString(q) {
		return errors.New("query contains forbidden characters")
	}
	return nil
}

// ── Olly prompt ───────────────────────────────────────────────────────────────

const ollyPromptTemplate = `You are a Coralogix threat-hunting specialist. Analyse the detection query and log results below and return a structured threat-hunting report.

Your response MUST follow this exact structure with section headers:

## §1 Hunt Summary
[Provide: Severity (Critical/High/Medium/Low), Confidence (High/Medium/Low), Search Window, Hunt Objective, MITRE ATT&CK Technique/Tactic]

## §2 Original Query
[Echo the exact Lucene query provided]

## §3 Schema Mapping
[Table with columns: Original Field | CX Log Path | Application/Subsystem | Gaps]

## §4 Translated Query — DataPrime
[The query translated to Coralogix DataPrime syntax]

## §5 Translated Query — Lucene
[Optimised Lucene query for Coralogix Explore]

## §6 Detection Logic Explained
[Plain-language explanation of what the query detects and why]

## §7 Hunt Workflow
[Step-by-step manual investigation workflow for a tier-2 analyst]

## §8 Suggested Aggregation / Pivot Query
[DataPrime or Lucene aggregation query to reveal attack patterns]

## §9 False Positive Considerations
[List likely false positive sources with suppression suggestions]

## §10 Visibility Gaps & Assumptions
[List missing log sources, fields, or coverage gaps that limit this hunt]

## §11 Recommended Follow-up Hunts
[3-5 related hunts with brief description and query sketch]

## §12 Alert Definition Skeleton
[Key-value block: Name, Type, Condition, Severity, Group-by fields]

---

## QUERY TO HUNT
{{.LuceneQuery}}

## LOG RESULTS (cx logs output)
Total hits: {{.HitCount}}
Sample events:
{{.SampleEvents}}`

type ollyPromptData struct {
	LuceneQuery  string
	HitCount     int
	SampleEvents string
}

var ollyTmpl = template.Must(template.New("olly").Parse(ollyPromptTemplate))

func buildOllyPrompt(luceneQuery string, hitCount int, sampleEvents string) (string, error) {
	var buf bytes.Buffer
	err := ollyTmpl.Execute(&buf, ollyPromptData{
		LuceneQuery:  luceneQuery,
		HitCount:     hitCount,
		SampleEvents: sampleEvents,
	})
	if err != nil {
		return "", fmt.Errorf("render olly prompt: %w", err)
	}
	return buf.String(), nil
}

// ── Section parser ────────────────────────────────────────────────────────────

var sectionHeaderRe = regexp.MustCompile(`(?m)^##\s+§(\d+)\s+`)

func parseOllySections(output string) map[string]string {
	matches := sectionHeaderRe.FindAllStringIndex(output, -1)
	nums := sectionHeaderRe.FindAllStringSubmatch(output, -1)
	sections := make(map[string]string, 12)
	for i, loc := range matches {
		start := loc[1] // after the header
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(output)
		}
		content := strings.TrimSpace(output[start:end])
		sections[nums[i][1]] = content
	}
	return sections
}

// ── Verdict derivation ────────────────────────────────────────────────────────

var severityRe = regexp.MustCompile(`(?i)Severity:\s*(\w+)`)
var confidenceRe = regexp.MustCompile(`(?i)Confidence:\s*(\w+)`)

func deriveVerdict(hits int, section1 string) (verdict, confidence string) {
	sev := "medium"
	conf := "medium"

	if m := severityRe.FindStringSubmatch(section1); len(m) > 1 {
		sev = strings.ToLower(m[1])
	}
	if m := confidenceRe.FindStringSubmatch(section1); len(m) > 1 {
		conf = strings.ToLower(m[1])
	}

	if hits == 0 {
		return "clean", conf
	}

	isCritical := sev == "critical"
	isHighSev := sev == "high"
	isHighConf := conf == "high"

	if isCritical || (isHighSev && isHighConf) {
		return "threat", conf
	}
	return "suspicious", conf
}
