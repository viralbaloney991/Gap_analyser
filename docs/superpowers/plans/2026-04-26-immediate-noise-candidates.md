# Immediate Noise Candidates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the gap analysis pipeline to surface unscoped `logs_immediate` security alerts as `immediate_noise_candidates` for Claude Opus to assess, flagging likely high-frequency alerts in `environment_cleanup` in the Coverage tab.

**Architecture:** A new pre-filter in `buildStructuredSignals` selects unscoped, non-BB, non-vendor-covered `logs_immediate` security alerts (capped at 30, sorted by name) and passes them as a new `immediate_noise_candidates` field in the structured JSON payload. Claude assesses Lucene query breadth and flags likely high-frequency alerts in `environment_cleanup`. Rule-confirmed noise (Noise tab) is unchanged.

**Tech Stack:** Go, `coralogix-alert-analyzer/internal/coralogix` (ExtractAppSubsystem, ExtractLuceneQuery), stdlib `sort`

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/insights/signals.go` | New `signalsImmediateCandidate` struct; `ImmediateNoiseCandidates` field on `structuredSignals`; `eventCounts map[string]int` param; pre-filter logic; `sort` + `coralogix` imports |
| `backend/internal/insights/signals_test.go` | Update 4 existing calls (add `nil` arg); 4 new tests |
| `backend/internal/insights/enrich.go` | `eventCounts map[string]int` param on `Enrich`; thread into `buildStructuredSignals`; system prompt additions |
| `backend/internal/insights/enrich_test.go` | Update 5 existing `Enrich` calls (add `nil` before provider arg) |
| `backend/internal/api/handlers.go` | `eventCounts` param on `runInsightsBackground`; pass at both `Enrich` call sites |

---

## Task 1: signals.go — struct, field, param, pre-filter logic

**Files:**
- Modify: `backend/internal/insights/signals.go`
- Modify: `backend/internal/insights/signals_test.go`

- [ ] **Step 1: Write 4 new failing tests and update 4 existing calls**

Append to `backend/internal/insights/signals_test.go` (after line 121):

```go
func TestBuildStructuredSignals_ImmediateCandidates_Included(t *testing.T) {
	alerts := []*models.AlertDef{
		{
			ID:        "alert-1",
			Name:      "Power Apps App Launched",
			AlertType: "logs_immediate",
			Features: models.AlertFeatures{
				IsSecurityAlert: true,
				IsBuildingBlock: false,
				VendorCovered:   false,
			},
			TypeDef: map[string]any{
				"logsFilter": map[string]any{
					"simpleFilter": map[string]any{
						"luceneQuery": "eventSource:PowerApps AND eventType:AppLaunched",
					},
				},
			},
		},
	}
	eventCounts := map[string]int{"alert-1": 150}
	sig := buildStructuredSignals(nil, alerts, nil, nil, eventCounts)
	if len(sig.ImmediateNoiseCandidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(sig.ImmediateNoiseCandidates))
	}
	c := sig.ImmediateNoiseCandidates[0]
	if c.Name != "Power Apps App Launched" {
		t.Errorf("name: %q", c.Name)
	}
	if c.Query != "eventSource:PowerApps AND eventType:AppLaunched" {
		t.Errorf("query: %q", c.Query)
	}
	if c.TriggerCount != 150 {
		t.Errorf("trigger_count: want 150, got %d", c.TriggerCount)
	}
}

func TestBuildStructuredSignals_ImmediateCandidates_Excluded(t *testing.T) {
	alerts := []*models.AlertDef{
		// Wrong type
		{
			ID: "a1", Name: "MetricAlert", AlertType: "metric_threshold",
			Features: models.AlertFeatures{IsSecurityAlert: true},
		},
		// Not security
		{
			ID: "a2", Name: "OpsAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: false},
		},
		// Building block
		{
			ID: "a3", Name: "BBAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true, IsBuildingBlock: true},
		},
		// Vendor covered
		{
			ID: "a4", Name: "VendorAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true, VendorCovered: true},
		},
		// Scoped (has app filter)
		{
			ID: "a5", Name: "ScopedAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true},
			TypeDef: map[string]any{
				"logsFilter": map[string]any{
					"simpleFilter": map[string]any{
						"labelFilters": map[string]any{
							"applicationName": []any{
								map[string]any{"value": "MyApp"},
							},
						},
					},
				},
			},
		},
		// Has entity filter
		{
			ID: "a6", Name: "EntityAlert", AlertType: "logs_immediate",
			Features: models.AlertFeatures{
				IsSecurityAlert: true,
				Entities:        []string{"user:alice"},
			},
		},
	}
	sig := buildStructuredSignals(nil, alerts, nil, nil, nil)
	if len(sig.ImmediateNoiseCandidates) != 0 {
		t.Errorf("want 0 candidates, got %d: %v", len(sig.ImmediateNoiseCandidates), sig.ImmediateNoiseCandidates)
	}
}

func TestBuildStructuredSignals_ImmediateCandidates_Cap(t *testing.T) {
	alerts := make([]*models.AlertDef, 35)
	for i := range alerts {
		alerts[i] = &models.AlertDef{
			ID:        fmt.Sprintf("id-%02d", i),
			Name:      fmt.Sprintf("Alert-%02d", i),
			AlertType: "logs_immediate",
			Features:  models.AlertFeatures{IsSecurityAlert: true},
		}
	}
	sig := buildStructuredSignals(nil, alerts, nil, nil, nil)
	if len(sig.ImmediateNoiseCandidates) != 30 {
		t.Errorf("want 30 candidates (cap), got %d", len(sig.ImmediateNoiseCandidates))
	}
	// Verify sorted by name
	if sig.ImmediateNoiseCandidates[0].Name != "Alert-00" {
		t.Errorf("first after sort: %q", sig.ImmediateNoiseCandidates[0].Name)
	}
	if sig.ImmediateNoiseCandidates[29].Name != "Alert-29" {
		t.Errorf("last after sort: %q", sig.ImmediateNoiseCandidates[29].Name)
	}
}

func TestBuildStructuredSignals_ImmediateCandidates_NilEventCounts(t *testing.T) {
	alerts := []*models.AlertDef{
		{
			ID: "a1", Name: "NoCount", AlertType: "logs_immediate",
			Features: models.AlertFeatures{IsSecurityAlert: true},
		},
	}
	sig := buildStructuredSignals(nil, alerts, nil, nil, nil) // nil eventCounts
	if len(sig.ImmediateNoiseCandidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(sig.ImmediateNoiseCandidates))
	}
	if sig.ImmediateNoiseCandidates[0].TriggerCount != 0 {
		t.Errorf("trigger_count: want 0 for nil eventCounts, got %d", sig.ImmediateNoiseCandidates[0].TriggerCount)
	}
}
```

Also update the 4 existing `buildStructuredSignals` calls in the same file to add `nil` as the 5th argument (eventCounts):

Line 40: `sig := buildStructuredSignals(result, alerts, integrations, mitreCoverage, nil)`
Line 78: `sig := buildStructuredSignals(result, nil, nil, nil, nil)`
Line 94: `sig := buildStructuredSignals(&models.SimilarityResult{}, nil, nil, mitreCoverage, nil)`
Line 108: `sig := buildStructuredSignals(result, nil, nil, nil, nil)`

- [ ] **Step 2: Run to confirm compile failure**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/ 2>&1 | head -20
```

Expected: compile error — `buildStructuredSignals` has too many arguments / `ImmediateNoiseCandidates` undefined

- [ ] **Step 3: Implement signals.go changes**

Replace the entire content of `backend/internal/insights/signals.go`:

```go
package insights

import (
	"fmt"
	"sort"

	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/models"
)

const maxImmediateCandidates = 30

// structuredSignals is the JSON payload sent to Claude Opus for gap analysis.
type structuredSignals struct {
	AlertCount               int                              `json:"alert_count"`
	IntegrationCount         int                              `json:"integration_count"`
	TacticCoverage           map[string]signalsTacticEntry    `json:"tactic_coverage"`
	TechniqueCoverage        map[string]signalsTechniqueEntry `json:"technique_coverage"`
	IntegrationGaps          []signalsIntegrationGap          `json:"integration_gaps"`
	NoiseAlerts              []string                         `json:"noise_alerts"`
	DuplicateGroups          int                              `json:"duplicate_groups"`
	ImmediateNoiseCandidates []signalsImmediateCandidate      `json:"immediate_noise_candidates"`
}

type signalsTacticEntry struct {
	Pct    float64 `json:"pct"`
	Alerts int     `json:"alerts"`
}

type signalsTechniqueEntry struct {
	Name   string `json:"name"`
	Alerts int    `json:"alerts"`
	Weak   bool   `json:"weak,omitempty"`
}

type signalsIntegrationGap struct {
	Name   string `json:"name"`
	Alerts int    `json:"alerts"`
}

type signalsImmediateCandidate struct {
	Name         string `json:"name"`
	Query        string `json:"query"`
	TriggerCount int    `json:"trigger_count,omitempty"`
}

// buildStructuredSignals assembles the ~1–2k token JSON payload for Claude Opus.
// All parameters are optional — nil inputs produce empty but valid signals.
// eventCounts may be nil; candidates will have TriggerCount 0 (omitted from JSON).
func buildStructuredSignals(
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	eventCounts map[string]int,
) structuredSignals {
	sig := structuredSignals{
		TacticCoverage:    make(map[string]signalsTacticEntry),
		TechniqueCoverage: make(map[string]signalsTechniqueEntry),
	}

	sig.AlertCount = len(alerts)
	sig.IntegrationCount = len(integrations)

	if result != nil {
		sig.DuplicateGroups = len(result.Duplicates)

		for _, na := range result.NoiseAlerts {
			label := na.Name
			switch {
			case na.TriggerCount > 0 && na.NoiseType != "":
				label = fmt.Sprintf("%s [%s, %d×]", na.Name, na.NoiseType, na.TriggerCount)
			case na.NoiseType != "":
				label = fmt.Sprintf("%s [%s]", na.Name, na.NoiseType)
			}
			sig.NoiseAlerts = append(sig.NoiseAlerts, label)
		}
	}

	if mitreCoverage != nil {
		for tactic, tc := range mitreCoverage.Summary.TacticBreakdown {
			sig.TacticCoverage[tactic] = signalsTacticEntry{
				Pct:    tc.Percent,
				Alerts: tc.Covered,
			}
		}
		for id, entry := range mitreCoverage.TechniqueCoverage {
			sig.TechniqueCoverage[id] = signalsTechniqueEntry{
				Name:   entry.Name,
				Alerts: entry.AlertCount,
				Weak:   entry.Weak,
			}
		}
	}

	for _, integ := range integrations {
		if integ.AlertCount == 0 {
			sig.IntegrationGaps = append(sig.IntegrationGaps, signalsIntegrationGap{
				Name:   integ.Name,
				Alerts: 0,
			})
		}
	}

	// Pre-filter: unscoped logs_immediate security alerts with no entity filter.
	var candidates []signalsImmediateCandidate
	for _, alert := range alerts {
		if alert.AlertType != "logs_immediate" {
			continue
		}
		if !alert.Features.IsSecurityAlert || alert.Features.IsBuildingBlock || alert.Features.VendorCovered {
			continue
		}
		app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
		if app != "" || sub != "" {
			continue
		}
		if len(alert.Features.Entities) > 0 {
			continue
		}
		candidates = append(candidates, signalsImmediateCandidate{
			Name:         alert.Name,
			Query:        coralogix.ExtractLuceneQuery(alert.TypeDef),
			TriggerCount: eventCounts[alert.ID],
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	if len(candidates) > maxImmediateCandidates {
		candidates = candidates[:maxImmediateCandidates]
	}
	sig.ImmediateNoiseCandidates = candidates

	return sig
}
```

- [ ] **Step 4: Run all signals tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/ -run TestBuildStructuredSignals -v 2>&1
```

Expected: All 8 tests PASS (4 existing + 4 new)

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && git add internal/insights/signals.go internal/insights/signals_test.go && git commit -m "feat(insights): add immediate_noise_candidates pre-filter to structured signals"
```

---

## Task 2: enrich.go — eventCounts param + system prompt additions

**Files:**
- Modify: `backend/internal/insights/enrich.go`
- Modify: `backend/internal/insights/enrich_test.go`

- [ ] **Step 1: Update 5 existing Enrich calls in enrich_test.go**

In `backend/internal/insights/enrich_test.go`, all 5 `Enrich` calls need `nil` inserted as the 6th argument (before the provider). The current call pattern is:

```go
Enrich(context.Background(), <result>, <alerts>, <integrations>, <mitreCoverage>, &mockProvider{...})
```

Change all 5 to:

```go
Enrich(context.Background(), <result>, <alerts>, <integrations>, <mitreCoverage>, nil, &mockProvider{...})
```

Concretely, the 5 lines to update:

Line 115: `report, err := Enrich(context.Background(), nil, nil, nil, nil, &mockProvider{})`
→ `report, err := Enrich(context.Background(), nil, nil, nil, nil, nil, &mockProvider{})`

Line 134: `report, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: jsonResp})`
→ `report, err := Enrich(context.Background(), result, nil, nil, nil, nil, &mockProvider{response: jsonResp})`

Line 162: `report, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: jsonResp})`
→ `report, err := Enrich(context.Background(), result, nil, nil, nil, nil, &mockProvider{response: jsonResp})`

Line 183: `_, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{err: errors.New("network error")})`
→ `_, err := Enrich(context.Background(), result, nil, nil, nil, nil, &mockProvider{err: errors.New("network error")})`

Line 191: `_, err := Enrich(context.Background(), result, nil, nil, nil, &mockProvider{response: "not json"})`
→ `_, err := Enrich(context.Background(), result, nil, nil, nil, nil, &mockProvider{response: "not json"})`

- [ ] **Step 2: Run to confirm compile failure**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/ 2>&1 | head -10
```

Expected: compile error — `Enrich` called with too many arguments

- [ ] **Step 3: Update enrich.go**

Replace the entire content of `backend/internal/insights/enrich.go`:

```go
package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

const gapAnalysisSystemPrompt = `You are a senior detection engineer analysing an organisation's security alert library.
You will receive a JSON object with these fields:
- alert_count, integration_count: library size
- tactic_coverage: per-tactic {pct, alerts} — pct is coverage percent (0–100)
- technique_coverage: per T-code {name, alerts, weak} — weak=true means covered but unscoped
- integration_gaps: integrations with zero alerts [{name, alerts}]
- noise_alerts: alert names flagged as noisy (with type and trigger count)
- duplicate_groups: number of duplicate alert groups
- immediate_noise_candidates: unscoped logs_immediate security alerts with no entity filter
  [{name, query, trigger_count}] — trigger_count is 0 when event data unavailable

Respond with ONLY valid JSON matching this exact schema — no prose, no markdown:
{
  "summary": "<2-4 sentences: start with strengths then key gaps>",
  "environment_cleanup": ["<string>"],
  "no_detection": ["<string>"],
  "poor_tactic_coverage": ["<string>"],
  "weak_detection_quality": ["<string>"],
  "advanced_use_cases": ["<string>"],
  "missing_source_alerts": ["<string>"]
}

Rules:
- Every category field must be a JSON array — use [] if nothing applies, never null or omit the field
- Only reference techniques, tactics, and alert names present in the input — never fabricate names
- poor_tactic_coverage: flag any tactic with pct < 25
- weak_detection_quality: only flag techniques where weak=true in the input
- advanced_use_cases: reason over technique type; only flag when threshold/count alerts exist but no anomaly layer
- summary: prose only (no bullet points), 2-4 sentences
- immediate_noise_candidates: for each entry, assess whether the Lucene query targets a
  high-frequency event (common user actions, broad field matches, platform lifecycle events).
  If yes, flag in environment_cleanup with a specific recommendation to add app/subsystem
  scoping or an entity filter. If the query is narrow enough to be low-frequency by nature,
  do not flag it. Use trigger_count as a signal when > 0; reason from query semantics when 0.`

// Enrich takes a completed SimilarityResult, assembles structured signals, sends them
// to Claude Opus, and returns an InsightsReport with 6-category gap analysis.
// Returns nil, nil if the result has no meaningful content to enrich.
// Returns nil, err on LLM failure (caller treats as non-fatal).
func Enrich(
	ctx context.Context,
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	eventCounts map[string]int,
	provider llm.Provider,
) (*models.InsightsReport, error) {
	if result == nil {
		return nil, nil
	}

	signals := buildStructuredSignals(result, alerts, integrations, mitreCoverage, eventCounts)
	signalsJSON, err := json.Marshal(signals)
	if err != nil {
		return nil, fmt.Errorf("insights signals marshal: %w", err)
	}

	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: gapAnalysisSystemPrompt,
		UserMessage:  string(signalsJSON),
		MaxTokens:    2048,
	})
	if err != nil {
		return nil, fmt.Errorf("insights LLM call: %w", err)
	}

	report := parseGapCategoriesResponse(raw)
	if report == nil {
		return nil, fmt.Errorf("insights JSON parse: malformed response")
	}
	return report, nil
}

// parseGapCategoriesResponse parses the Claude Opus JSON output into an InsightsReport.
// Strips markdown fences if present. Missing categories become empty slices.
// Returns nil on malformed JSON.
func parseGapCategoriesResponse(raw string) *models.InsightsReport {
	raw = strings.TrimSpace(raw)
	// Extract the JSON object from anywhere in the response — handles markdown
	// fences, preamble prose, and any trailing content Claude may add.
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 {
		raw = raw[:i+1]
	}

	var loose struct {
		Summary              string   `json:"summary"`
		EnvironmentCleanup   []string `json:"environment_cleanup"`
		NoDetection          []string `json:"no_detection"`
		PoorTacticCoverage   []string `json:"poor_tactic_coverage"`
		WeakDetectionQuality []string `json:"weak_detection_quality"`
		AdvancedUseCases     []string `json:"advanced_use_cases"`
		MissingSourceAlerts  []string `json:"missing_source_alerts"`
	}
	if err := json.Unmarshal([]byte(raw), &loose); err != nil {
		return nil
	}

	coerce := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}

	return &models.InsightsReport{
		Summary: loose.Summary,
		GapCategories: models.GapCategories{
			EnvironmentCleanup:   coerce(loose.EnvironmentCleanup),
			NoDetection:          coerce(loose.NoDetection),
			PoorTacticCoverage:   coerce(loose.PoorTacticCoverage),
			WeakDetectionQuality: coerce(loose.WeakDetectionQuality),
			AdvancedUseCases:     coerce(loose.AdvancedUseCases),
			MissingSourceAlerts:  coerce(loose.MissingSourceAlerts),
		},
	}
}
```

- [ ] **Step 4: Run all insights tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/ -v 2>&1
```

Expected: All tests PASS (8 signals tests + 9 enrich tests)

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && git add internal/insights/enrich.go internal/insights/enrich_test.go && git commit -m "feat(insights): thread eventCounts into Enrich, add immediate_noise_candidates prompt rules"
```

---

## Task 3: handlers.go — wire up both Enrich call sites

**Files:**
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Add eventCounts param to runInsightsBackground and pass to Enrich**

In `backend/internal/api/handlers.go`, update `runInsightsBackground` (currently at line 323):

Change the function signature from:
```go
func (h *Handler) runInsightsBackground(
	client string,
	alertInsights *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
) {
```
To:
```go
func (h *Handler) runInsightsBackground(
	client string,
	alertInsights *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	eventCounts map[string]int,
) {
```

Inside the function body, update the `insights.Enrich` call (currently line 346) from:
```go
ir, enrichErr := insights.Enrich(bgCtx, alertInsights, alerts, integrations, mitreCoverage, insightsProvider)
```
To:
```go
ir, enrichErr := insights.Enrich(bgCtx, alertInsights, alerts, integrations, mitreCoverage, eventCounts, insightsProvider)
```

- [ ] **Step 2: Update the runInsightsBackground call site in HandleAnalyze**

In `HandleAnalyze` (currently line 285), update the call from:
```go
h.runInsightsBackground(req.Client, alertInsights, alerts, integrationInfos, mitreCoverage)
```
To:
```go
h.runInsightsBackground(req.Client, alertInsights, alerts, integrationInfos, mitreCoverage, eventCounts)
```

(`eventCounts` is fetched at line 212 in `HandleAnalyze` and is already in scope.)

- [ ] **Step 3: Update the Enrich call in HandleInsights**

In `HandleInsights` (currently line 467), update from:
```go
ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, nil, insightsMitre, insightsProvider)
```
To:
```go
ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, nil, insightsMitre, insightsEventCounts, insightsProvider)
```

(`insightsEventCounts` is fetched at line 420 in `HandleInsights` and is already in scope.)

- [ ] **Step 4: Build and run all tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./... 2>&1 && go test ./... 2>&1
```

Expected: Clean build, all tests pass

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && git add internal/api/handlers.go && git commit -m "feat(insights): pass eventCounts through runInsightsBackground and HandleInsights to Enrich"
```

---

## Self-Review Against Spec

| Spec requirement | Covered by |
|-----------------|-----------|
| `signalsImmediateCandidate` struct with name/query/trigger_count | Task 1 Step 3 |
| `ImmediateNoiseCandidates` field on `structuredSignals` | Task 1 Step 3 |
| Pre-filter: logs_immediate + IsSecurityAlert + not BB + not VendorCovered + ExtractAppSubsystem("","") + no Entities | Task 1 Step 3 |
| Cap at 30, sorted by name | Task 1 Step 3 |
| `eventCounts` param threads through Enrich → buildStructuredSignals | Task 2 Step 3, Task 3 Steps 1-3 |
| `trigger_count` omitted from JSON when 0 (omitempty) | Task 1 Step 3 |
| System prompt: input description for immediate_noise_candidates | Task 2 Step 3 |
| System prompt: assessment rule (breadth, frequency semantics, flag in environment_cleanup) | Task 2 Step 3 |
| `TestBuildStructuredSignals_ImmediateCandidates_Included` | Task 1 Step 1 |
| `TestBuildStructuredSignals_ImmediateCandidates_Excluded` | Task 1 Step 1 |
| `TestBuildStructuredSignals_ImmediateCandidates_Cap` | Task 1 Step 1 |
| `TestBuildStructuredSignals_ImmediateCandidates_NilEventCounts` | Task 1 Step 1 |
| HandleAnalyze passes eventCounts to runInsightsBackground | Task 3 Step 2 |
| HandleInsights passes insightsEventCounts to Enrich | Task 3 Step 3 |
| findNoiseAlerts unchanged | (not touched) |
| Noise tab unchanged | (not touched) |
| Frontend unchanged | (not touched) |
