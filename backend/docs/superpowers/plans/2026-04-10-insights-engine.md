# Insights Engine Enhancement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add noise detection to the similarity engine, a new LLM enrichment layer that produces an `InsightsReport`, and a two-pane Command Center frontend for the Alert Insights view.

**Architecture:** 7 tasks — models first (data contracts), then backend algorithmic work (TDD), then the new `insights` package (TDD), then handler wiring, then frontend (types → CSS → component). Each task is independently buildable.

**Tech Stack:** Go 1.22, React 19, TypeScript, IBM Plex Mono, CSS custom properties; no new dependencies.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/models/models.go` | Add `NoiseAlerts` to `SimilarityResult`; add `InsightsReport` struct; add `InsightsReport` field to `AnalyzeResponse` |
| `backend/internal/similarity/engine.go` | Expand `commonCategories` (7→20); add `findNoiseAlerts()`; call it in `Analyze()` as Step 8 |
| `backend/internal/similarity/engine_test.go` | New — unit tests for `findNoiseAlerts()` |
| `backend/internal/insights/enrich.go` | New file — `Enrich()` function with prompt construction, JSON parsing |
| `backend/internal/insights/enrich_test.go` | New — unit tests for `Enrich()` using mock provider |
| `backend/internal/api/handlers.go` | Add `computeInsightsCacheKey()`; call `insights.Enrich()` with cache check after `similarity.Analyze()` |
| `frontend/src/types/index.ts` | Add `InsightsReport` interface; add `noise_alerts` to `SimilarityResult`; add `insights_report` to `AnalyzeResponse` |
| `frontend/src/App.tsx` | Pass `report` prop to `AlertInsights` |
| `frontend/src/App.css` | Update `.alert-insights` selector; add insights grid/panel classes |
| `frontend/src/components/AlertInsights.tsx` | Full rewrite — two-pane layout, Noise tab, LLM-enriched explanations with fallback |

---

## Task 1: Models

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Add `NoiseAlerts` to `SimilarityResult`**

In `backend/internal/models/models.go`, find:

```go
type SimilarityResult struct {
	Families         []DetectionFamily   `json:"families"`
	Duplicates       []DuplicateGroup    `json:"duplicates"`
	MergeSuggestions []MergeSuggestion   `json:"merge_suggestions"`
	CoverageInsights []string            `json:"coverage_insights"`
	UniqueDetections []string            `json:"unique_detections"`
}
```

Replace with:

```go
type SimilarityResult struct {
	Families         []DetectionFamily   `json:"families"`
	Duplicates       []DuplicateGroup    `json:"duplicates"`
	MergeSuggestions []MergeSuggestion   `json:"merge_suggestions"`
	CoverageInsights []string            `json:"coverage_insights"`
	UniqueDetections []string            `json:"unique_detections"`
	NoiseAlerts      []string            `json:"noise_alerts"`
}
```

- [ ] **Step 2: Add `InsightsReport` struct**

In `backend/internal/models/models.go`, after the `MergeSuggestion` struct (around line 100), add:

```go
// InsightsReport is the LLM-generated analyst report for a SimilarityResult.
type InsightsReport struct {
	Summary         string   `json:"summary"`
	TopPriority     []string `json:"top_priority"`
	Strengths       []string `json:"strengths"`
	Recommendations []string `json:"recommendations"`
	EnrichedDups    []string `json:"enriched_dups"`
	EnrichedGaps    []string `json:"enriched_gaps"`
}
```

- [ ] **Step 3: Add `InsightsReport` field to `AnalyzeResponse`**

In `backend/internal/models/models.go`, find:

```go
type AnalyzeResponse struct {
	Integrations  []IntegrationInfo    `json:"integrations"`
	Stats         AnalysisStats        `json:"stats"`
	MITRECoverage *MITRECoverageResult `json:"mitre_coverage"`
	AlertInsights *SimilarityResult    `json:"alert_insights"`
	Cached        bool                 `json:"cached"`
}
```

Replace with:

```go
type AnalyzeResponse struct {
	Integrations   []IntegrationInfo    `json:"integrations"`
	Stats          AnalysisStats        `json:"stats"`
	MITRECoverage  *MITRECoverageResult `json:"mitre_coverage"`
	AlertInsights  *SimilarityResult    `json:"alert_insights"`
	InsightsReport *InsightsReport      `json:"insights_report,omitempty"`
	Cached         bool                 `json:"cached"`
}
```

- [ ] **Step 4: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/models.go
git commit -m "feat: add NoiseAlerts to SimilarityResult and InsightsReport model"
```

---

## Task 2: Similarity Engine — Noise Detection

**Files:**
- Create: `backend/internal/similarity/engine_test.go`
- Modify: `backend/internal/similarity/engine.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/similarity/engine_test.go`:

```go
package similarity

import (
	"testing"
)

func TestFindNoiseAlerts_returnsNoisyAlerts(t *testing.T) {
	vectors := []featureVector{
		{
			alertName:   "Noisy",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{},
			actions:     map[string]struct{}{},
			conditions:  map[string]struct{}{},
			techniques:  map[string]struct{}{},
		},
		{
			alertName:   "RichAlert",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{"user": {}},
			actions:     map[string]struct{}{"login": {}},
			conditions:  map[string]struct{}{"failed": {}},
			techniques:  map[string]struct{}{"t1078": {}},
		},
	}
	noisy := findNoiseAlerts(vectors)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noisy alert, got %d: %v", len(noisy), noisy)
	}
	if noisy[0] != "Noisy" {
		t.Errorf("expected \"Noisy\", got %q", noisy[0])
	}
}

func TestFindNoiseAlerts_nilInput(t *testing.T) {
	noisy := findNoiseAlerts(nil)
	if noisy != nil {
		t.Errorf("expected nil for nil input, got %v", noisy)
	}
}

func TestFindNoiseAlerts_atThreshold(t *testing.T) {
	// Total = 3 means NOT noise (threshold is strictly < 3)
	vectors := []featureVector{
		{
			alertName:   "AtThreshold",
			dataSources: map[string]struct{}{"logs": {}},
			entities:    map[string]struct{}{"user": {}},
			actions:     map[string]struct{}{"login": {}},
			conditions:  map[string]struct{}{},
			techniques:  map[string]struct{}{},
		},
	}
	noisy := findNoiseAlerts(vectors)
	if len(noisy) != 0 {
		t.Errorf("expected no noise for exactly 3 tokens, got %v", noisy)
	}
}

func TestFindNoiseAlerts_isSorted(t *testing.T) {
	vectors := []featureVector{
		{alertName: "ZAlert", dataSources: map[string]struct{}{"x": {}}, entities: map[string]struct{}{}, actions: map[string]struct{}{}, conditions: map[string]struct{}{}, techniques: map[string]struct{}{}},
		{alertName: "AAlert", dataSources: map[string]struct{}{"y": {}}, entities: map[string]struct{}{}, actions: map[string]struct{}{}, conditions: map[string]struct{}{}, techniques: map[string]struct{}{}},
	}
	noisy := findNoiseAlerts(vectors)
	if len(noisy) != 2 {
		t.Fatalf("expected 2 noisy alerts, got %d", len(noisy))
	}
	if noisy[0] != "AAlert" || noisy[1] != "ZAlert" {
		t.Errorf("expected sorted [AAlert, ZAlert], got %v", noisy)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/similarity/... -run TestFindNoiseAlerts -v
```

Expected: `undefined: findNoiseAlerts` — confirms the function doesn't exist yet.

- [ ] **Step 3: Expand `commonCategories`**

In `backend/internal/similarity/engine.go`, find:

```go
var commonCategories = []string{
	"login anomalies",
	"privilege escalation",
	"data exfiltration",
	"lateral movement",
	"token abuse",
	"mfa bypass",
	"api abuse",
}
```

Replace with:

```go
var commonCategories = []string{
	// Identity
	"login anomalies",
	"mfa bypass",
	"credential stuffing",
	"token abuse",
	"session hijacking",
	// Endpoint
	"malware execution",
	"persistence",
	"privilege escalation",
	// Cloud
	"iam abuse",
	"storage exfiltration",
	"resource abuse",
	// Network
	"lateral movement",
	"port scanning",
	"c2 traffic",
	// Data
	"data exfiltration",
	"sensitive data access",
	"api abuse",
	// Additional
	"ransomware",
	"supply chain",
	"insider threat",
}
```

- [ ] **Step 4: Add `findNoiseAlerts()` function**

In `backend/internal/similarity/engine.go`, add the following after the `findUniqueDetections()` function (after line 809):

```go
// ---------------------------------------------------------------------------
// Step 8: Noise Detection
// ---------------------------------------------------------------------------

// findNoiseAlerts returns names of alerts whose total unique feature token
// count is below the noise threshold (sparse = likely threshold-only alert).
func findNoiseAlerts(vectors []featureVector) []string {
	const noiseThreshold = 3
	var noisy []string
	for _, v := range vectors {
		total := len(v.dataSources) + len(v.entities) + len(v.actions) +
			len(v.conditions) + len(v.techniques)
		if total < noiseThreshold {
			noisy = append(noisy, v.alertName)
		}
	}
	sort.Strings(noisy)
	return noisy
}
```

- [ ] **Step 5: Call `findNoiseAlerts()` in `Analyze()`**

In `backend/internal/similarity/engine.go`, find:

```go
	// Step 7: Unique detections.
	uniqueDetections := findUniqueDetections(vectors, matrix, n)

	return &models.SimilarityResult{
		Families:         families,
		Duplicates:       duplicates,
		MergeSuggestions: mergeSuggestions,
		CoverageInsights: coverageInsights,
		UniqueDetections: uniqueDetections,
	}
```

Replace with:

```go
	// Step 7: Unique detections.
	uniqueDetections := findUniqueDetections(vectors, matrix, n)

	// Step 8: Noise detection.
	noiseAlerts := findNoiseAlerts(vectors)

	return &models.SimilarityResult{
		Families:         families,
		Duplicates:       duplicates,
		MergeSuggestions: mergeSuggestions,
		CoverageInsights: coverageInsights,
		UniqueDetections: uniqueDetections,
		NoiseAlerts:      noiseAlerts,
	}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd backend && go test ./internal/similarity/... -run TestFindNoiseAlerts -v
```

Expected: all 4 tests PASS.

- [ ] **Step 7: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat: add noise detection and expand coverage categories in similarity engine"
```

---

## Task 3: Insights Package — Enrich()

**Files:**
- Create: `backend/internal/insights/enrich.go`
- Create: `backend/internal/insights/enrich_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/insights/enrich_test.go`:

```go
package insights

import (
	"context"
	"errors"
	"testing"

	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/models"
)

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (string, error) {
	return m.response, m.err
}
func (m *mockProvider) Name() string { return "mock" }

func TestEnrich_nilResult_returnsNilNil(t *testing.T) {
	report, err := Enrich(context.Background(), nil, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil; got %v, %v", report, err)
	}
}

func TestEnrich_emptyResult_returnsNilNil(t *testing.T) {
	result := &models.SimilarityResult{}
	report, err := Enrich(context.Background(), result, nil, &mockProvider{})
	if report != nil || err != nil {
		t.Errorf("expected nil, nil for empty result; got %v, %v", report, err)
	}
}

func TestEnrich_validResponse_parsesReport(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{
			{AlertNames: []string{"A", "B"}, Similarity: 0.95},
		},
	}
	jsonResp := `{
		"summary": "Test summary",
		"top_priority": ["Fix A"],
		"strengths": ["Good B"],
		"recommendations": ["Add C"],
		"enriched_dups": ["A and B overlap in login detection"],
		"enriched_gaps": []
	}`
	report, err := Enrich(context.Background(), result, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Summary != "Test summary" {
		t.Errorf("expected summary \"Test summary\", got %q", report.Summary)
	}
	if len(report.TopPriority) != 1 || report.TopPriority[0] != "Fix A" {
		t.Errorf("unexpected top_priority: %v", report.TopPriority)
	}
	if len(report.EnrichedDups) != 1 {
		t.Errorf("expected 1 enriched dup, got %d", len(report.EnrichedDups))
	}
}

func TestEnrich_llmError_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	report, err := Enrich(context.Background(), result, nil, &mockProvider{err: errors.New("network error")})
	if report != nil {
		t.Error("expected nil report on error")
	}
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEnrich_invalidJSON_returnsError(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	report, err := Enrich(context.Background(), result, nil, &mockProvider{response: "not json at all"})
	if report != nil {
		t.Error("expected nil report on parse error")
	}
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestEnrich_markdownFence_stripped(t *testing.T) {
	result := &models.SimilarityResult{
		Duplicates: []models.DuplicateGroup{{AlertNames: []string{"A", "B"}}},
	}
	jsonResp := "```json\n{\"summary\":\"ok\",\"top_priority\":[],\"strengths\":[],\"recommendations\":[],\"enriched_dups\":[],\"enriched_gaps\":[]}\n```"
	report, err := Enrich(context.Background(), result, nil, &mockProvider{response: jsonResp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil || report.Summary != "ok" {
		t.Errorf("expected summary \"ok\", got %v", report)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/insights/... -v
```

Expected: build error — package `insights` doesn't exist yet.

- [ ] **Step 3: Implement `enrich.go`**

Create `backend/internal/insights/enrich.go`:

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

// Enrich takes a completed SimilarityResult and alert list, sends one
// structured prompt to the LLM, and returns an InsightsReport.
// Returns nil, nil if the result has no meaningful content to enrich.
// Returns nil, err on LLM failure (caller treats as non-fatal).
func Enrich(
	ctx context.Context,
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	provider llm.Provider,
) (*models.InsightsReport, error) {
	if result == nil || (len(result.Duplicates) == 0 && len(result.Families) == 0 &&
		len(result.CoverageInsights) == 0 && len(result.NoiseAlerts) == 0) {
		return nil, nil
	}

	prompt := buildPrompt(result, alerts)
	raw, err := provider.Complete(ctx, llm.CompletionRequest{
		UserMessage: prompt,
		MaxTokens:   1024,
	})
	if err != nil {
		return nil, fmt.Errorf("insights LLM call: %w", err)
	}

	// Strip markdown code fence if present.
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i != -1 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}

	var report models.InsightsReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, fmt.Errorf("insights JSON parse: %w", err)
	}
	return &report, nil
}

func buildPrompt(result *models.SimilarityResult, alerts []*models.AlertDef) string {
	var sb strings.Builder

	sb.WriteString("You are a security detection engineer reviewing a SIEM alert library.\n\n")
	sb.WriteString(fmt.Sprintf("Alert library (%d alerts):\n", len(alerts)))
	for _, a := range alerts {
		sb.WriteString(fmt.Sprintf("- %s: sources=%v, actions=%v, techniques=%v\n",
			a.Name, a.Features.DataSources, a.Features.Actions, a.Features.Techniques))
	}

	sb.WriteString("\nSimilarity analysis results:\n")

	sb.WriteString(fmt.Sprintf("- Duplicates (%d):", len(result.Duplicates)))
	for _, d := range result.Duplicates {
		if len(d.AlertNames) >= 2 {
			sb.WriteString(fmt.Sprintf(" %s ≈ %s (%.0f%%),", d.AlertNames[0], d.AlertNames[1], d.Similarity*100))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("- Detection families (%d):", len(result.Families)))
	for _, f := range result.Families {
		sb.WriteString(fmt.Sprintf(" %s: %s;", f.Name, strings.Join(f.AlertNames, ", ")))
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("- Coverage gaps: %s\n", strings.Join(result.CoverageInsights, "; ")))
	sb.WriteString(fmt.Sprintf("- Noise alerts (sparse feature vectors): %s\n", strings.Join(result.NoiseAlerts, ", ")))

	sb.WriteString(`
Respond with JSON only:
{
  "summary": "2-3 sentence overview of the detection posture",
  "top_priority": ["ordered list of 3-5 most important actions"],
  "strengths": ["2-3 things well covered"],
  "recommendations": ["3-5 specific actionable items"],
  "enriched_dups": ["one sentence per duplicate pair explaining business impact"],
  "enriched_gaps": ["one sentence per coverage gap explaining risk"]
}`)

	return sb.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && go test ./internal/insights/... -v
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/insights/
git commit -m "feat: add insights.Enrich() with LLM prompt and JSON parsing"
```

---

## Task 4: Handler Integration

**Files:**
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Add `insights` import**

In `backend/internal/api/handlers.go`, find the import block. Add `"coralogix-alert-analyzer/internal/insights"` to the internal imports group:

```go
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/cache"
	"coralogix-alert-analyzer/internal/classifier"
	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/insights"
	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/merge"
	"coralogix-alert-analyzer/internal/mitre"
	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/similarity"
	"coralogix-alert-analyzer/internal/store"
)
```

- [ ] **Step 2: Add `computeInsightsCacheKey` helper**

In `backend/internal/api/handlers.go`, add the following after the `writeError` helper at the bottom of the file (or just before `HandleClients`):

```go
// computeInsightsCacheKey returns a stable Redis key for the insights report,
// derived from the SimilarityResult JSON content.
func computeInsightsCacheKey(clientName string, result *models.SimilarityResult) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("insights_v1:%s:%s", clientName, hex.EncodeToString(h[:])[:12]), nil
}
```

- [ ] **Step 3: Call `insights.Enrich()` in `HandleAnalyze`**

In `backend/internal/api/handlers.go`, find:

```go
	// Run similarity analysis.
	alertInsights := similarity.Analyze(alerts)

	// Build integration info for response.
```

Replace with:

```go
	// Run similarity analysis.
	alertInsights := similarity.Analyze(alerts)

	// LLM insights enrichment — non-fatal.
	var insightsReport *models.InsightsReport
	{
		var insightsCacheKey string
		if h.cache != nil {
			if key, err := computeInsightsCacheKey(req.Client, alertInsights); err == nil {
				insightsCacheKey = key
				if cached, ok := h.cache.GetString(ctx, key); ok {
					var ir models.InsightsReport
					if json.Unmarshal([]byte(cached), &ir) == nil {
						insightsReport = &ir
						log.Printf("INFO [analyze] insights cache HIT client=%s", req.Client)
					}
				}
			}
		}
		if insightsReport == nil {
			nvidiaKey := h.config.LLM.NvidiaAPIKey
			if h.config.LLM.NvidiaSuggestionAPIKey != "" {
				nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
			}
			insightsProvider, providerErr := llm.NewClassifierProvider(
				h.config.LLM.SuggestionProvider,
				h.config.LLM.SuggestionModel,
				llm.ProviderConfig{
					AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
					ClaudeModel:     h.config.LLM.ClaudeModel,
					NvidiaAPIKey:    nvidiaKey,
					NvidiaModel:     h.config.LLM.NvidiaModel,
					NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
					GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
					GeminiModel:     h.config.LLM.GeminiModel,
				},
			)
			if providerErr != nil {
				log.Printf("WARN [analyze] insights provider unavailable: %v", providerErr)
			} else {
				ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, insightsProvider)
				if enrichErr != nil {
					log.Printf("WARN [analyze] insights enrich client=%s: %v", req.Client, enrichErr)
				}
				if ir != nil {
					insightsReport = ir
					if h.cache != nil && insightsCacheKey != "" {
						if data, marshalErr := json.Marshal(ir); marshalErr == nil {
							h.cache.SetString(ctx, insightsCacheKey, string(data), 24*time.Hour)
						}
					}
				}
			}
		}
	}

	// Build integration info for response.
```

- [ ] **Step 4: Add `InsightsReport` to the response struct**

In `backend/internal/api/handlers.go`, find:

```go
	resp := &models.AnalyzeResponse{
		Integrations: integrationInfos,
		Stats: models.AnalysisStats{
			TotalIntegrations:      len(integrations),
			DoneIntegrations:       len(integrations),
			TotalAlerts:            len(alerts),
			SecurityAlerts:         securityCount,
			VendorCoveredAlerts:    vendorCoveredCount,
			IntegrationsWithAlerts: withAlerts,
		},
		MITRECoverage: mitreCoverage,
		AlertInsights: alertInsights,
		Cached:        false,
	}
```

Replace with:

```go
	resp := &models.AnalyzeResponse{
		Integrations: integrationInfos,
		Stats: models.AnalysisStats{
			TotalIntegrations:      len(integrations),
			DoneIntegrations:       len(integrations),
			TotalAlerts:            len(alerts),
			SecurityAlerts:         securityCount,
			VendorCoveredAlerts:    vendorCoveredCount,
			IntegrationsWithAlerts: withAlerts,
		},
		MITRECoverage:  mitreCoverage,
		AlertInsights:  alertInsights,
		InsightsReport: insightsReport,
		Cached:         false,
	}
```

- [ ] **Step 5: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/handlers.go
git commit -m "feat: integrate insights.Enrich() into analyze pipeline with Redis caching"
```

---

## Task 5: Frontend Types + App.tsx Prop

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Add `noise_alerts` to `SimilarityResult`**

In `frontend/src/types/index.ts`, find:

```ts
export interface SimilarityResult {
  families: DetectionFamily[];
  duplicates: DuplicateGroup[];
  merge_suggestions: MergeSuggestion[];
  coverage_insights: string[];
  unique_detections: string[];
}
```

Replace with:

```ts
export interface SimilarityResult {
  families: DetectionFamily[];
  duplicates: DuplicateGroup[];
  merge_suggestions: MergeSuggestion[];
  coverage_insights: string[];
  unique_detections: string[];
  noise_alerts: string[];
}
```

- [ ] **Step 2: Add `InsightsReport` interface**

In `frontend/src/types/index.ts`, add the following before the `AnalyzeResponse` interface:

```ts
export interface InsightsReport {
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  enriched_gaps: string[];
}
```

- [ ] **Step 3: Add `insights_report` to `AnalyzeResponse`**

In `frontend/src/types/index.ts`, find:

```ts
export interface AnalyzeResponse {
  integrations: IntegrationInfo[];
  stats: AnalysisStats;
  mitre_coverage: MITRECoverageResult;
  alert_insights: SimilarityResult;
  cached: boolean;
}
```

Replace with:

```ts
export interface AnalyzeResponse {
  integrations: IntegrationInfo[];
  stats: AnalysisStats;
  mitre_coverage: MITRECoverageResult;
  alert_insights: SimilarityResult;
  insights_report?: InsightsReport | null;
  cached: boolean;
}
```

- [ ] **Step 4: Pass `report` prop in `App.tsx`**

In `frontend/src/App.tsx`, find:

```tsx
        {view === 'insights' && data && (
          <AlertInsights data={data.alert_insights} />
        )}
```

Replace with:

```tsx
        {view === 'insights' && data && (
          <AlertInsights data={data.alert_insights} report={data.insights_report ?? null} />
        )}
```

- [ ] **Step 5: Build**

```bash
cd frontend && npm run build
```

Expected: TypeScript error in `AlertInsights.tsx` — `report` prop not accepted yet. This is expected; the component will be updated in Task 7.

If the error is only `Property 'report' does not exist on type 'Props'`, proceed. Fix it now only if there are other unexpected errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/App.tsx
git commit -m "feat: add InsightsReport types and pass report prop to AlertInsights"
```

---

## Task 6: Frontend CSS — Insights Grid Classes

**Files:**
- Modify: `frontend/src/App.css`

- [ ] **Step 1: Update `.alert-insights` selector**

In `frontend/src/App.css`, find:

```css
/* ── Alert Insights ──────────────────────────────── */
.alert-insights h2 {
  font-size: 1.3rem;
  margin-bottom: 20px;
}
```

Replace with:

```css
/* ── Alert Insights ──────────────────────────────── */
.alert-insights {
  height: calc(100vh - 53px - 64px);
  display: flex;
  flex-direction: column;
}
```

- [ ] **Step 2: Add insights grid/panel classes**

In `frontend/src/App.css`, find:

```css
.empty-state {
  text-align: center;
  padding: 40px;
  color: var(--text-dim);
  font-size: 0.95rem;
}

/* ── Scrollbar ───────────────────────────────────── */
```

Replace with:

```css
.empty-state {
  text-align: center;
  padding: 40px;
  color: var(--text-dim);
  font-size: 0.95rem;
}

/* Two-pane grid */
.insights-grid {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
  flex: 1;
  min-height: 0;
}

.insights-panel {
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border-right: 1px solid var(--border);
  padding: 16px 14px;
  overflow-y: auto;
}

.insights-panel-summary {
  font-size: 0.72rem;
  color: var(--text-dim);
  line-height: 1.5;
  margin-bottom: 14px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.insights-panel-section {
  margin-bottom: 12px;
}

.insights-panel-section-title {
  font-size: 0.52rem;
  color: var(--text-dim);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  border-bottom: 1px solid var(--border);
  padding-bottom: 3px;
  margin-bottom: 6px;
}

.insights-panel-item {
  font-size: 0.62rem;
  margin-bottom: 3px;
  padding-left: 8px;
  border-left: 2px solid var(--border-bright);
  color: var(--text);
}

.insights-panel-item--priority {
  border-left-color: var(--accent);
  color: var(--accent);
}

.insights-panel-item--danger {
  color: var(--danger);
}

.insights-tabs-panel {
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  padding: 12px 16px;
}

.insights-skeleton {
  background: linear-gradient(90deg, var(--surface) 25%, var(--surface-hover) 50%, var(--surface) 75%);
  background-size: 200% 100%;
  animation: skeleton-pulse 1.5s ease-in-out infinite;
  height: 0.7rem;
  border-radius: 2px;
  margin-bottom: 6px;
}

@keyframes skeleton-pulse {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

/* ── Scrollbar ───────────────────────────────────── */
```

- [ ] **Step 3: Build**

```bash
cd frontend && npm run build
```

Expected: same TypeScript error about `report` prop (from Task 5 — still expected). CSS change itself must not add new errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.css
git commit -m "style: add insights two-pane grid and panel classes"
```

---

## Task 7: Frontend AlertInsights — Two-Pane Component

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`

- [ ] **Step 1: Replace `AlertInsights.tsx` with two-pane layout**

Full replacement of `frontend/src/components/AlertInsights.tsx`:

```tsx
import { useState } from 'react';
import type { SimilarityResult, InsightsReport } from '../types';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
}

type Tab = 'duplicates' | 'families' | 'merge' | 'coverage' | 'noise' | 'unique';

export default function AlertInsights({ data, report }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('duplicates');

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'duplicates', label: 'Duplicates', count: data.duplicates?.length || 0 },
    { key: 'families', label: 'Families', count: data.families?.length || 0 },
    { key: 'merge', label: 'Merge', count: data.merge_suggestions?.length || 0 },
    { key: 'coverage', label: 'Coverage', count: data.coverage_insights?.length || 0 },
    { key: 'noise', label: 'Noise', count: data.noise_alerts?.length || 0 },
    { key: 'unique', label: 'Unique', count: data.unique_detections?.length || 0 },
  ];

  const gapCount = data.coverage_insights?.length || 0;
  const noiseCount = data.noise_alerts?.length || 0;

  return (
    <div className="alert-insights">
      <div className="insights-grid">

        {/* Left Panel */}
        <div className="insights-panel">

          {/* Summary */}
          <div className="insights-panel-summary">
            {report ? report.summary : (
              <>
                <div className="insights-skeleton" style={{ width: '100%' }} />
                <div className="insights-skeleton" style={{ width: '85%' }} />
                <div className="insights-skeleton" style={{ width: '70%' }} />
              </>
            )}
          </div>

          {/* TOP PRIORITY */}
          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Top Priority</div>
            {report ? report.top_priority.map((item, i) => (
              <div key={i} className="insights-panel-item insights-panel-item--priority">
                {i + 1}. {item}
              </div>
            )) : (
              <>
                <div className="insights-skeleton" style={{ width: '90%' }} />
                <div className="insights-skeleton" style={{ width: '80%' }} />
              </>
            )}
          </div>

          {/* STRENGTHS */}
          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Strengths</div>
            {report ? report.strengths.map((s, i) => (
              <div key={i} className="insights-panel-item">• {s}</div>
            )) : (
              <>
                <div className="insights-skeleton" style={{ width: '75%' }} />
                <div className="insights-skeleton" style={{ width: '65%' }} />
              </>
            )}
          </div>

          {/* SIGNALS */}
          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Signals</div>
            <div className="insights-panel-item">
              [{data.duplicates?.length || 0}] duplicates
            </div>
            <div className="insights-panel-item">
              [{data.families?.length || 0}] families
            </div>
            <div className={`insights-panel-item${noiseCount > 0 ? ' insights-panel-item--danger' : ''}`}>
              [{noiseCount}{noiseCount > 0 ? '!' : ''}] noise
            </div>
            <div className={`insights-panel-item${gapCount > 0 ? ' insights-panel-item--danger' : ''}`}>
              [{gapCount}{gapCount > 0 ? '!' : ''}] gaps
            </div>
          </div>

        </div>

        {/* Right Panel */}
        <div className="insights-tabs-panel">
          <div className="insights-tabs">
            {tabs.map((tab) => (
              <button
                key={tab.key}
                className={`tab-btn ${activeTab === tab.key ? 'active' : ''}`}
                onClick={() => setActiveTab(tab.key)}
              >
                {tab.label}
                <span className="tab-count">{tab.count}</span>
              </button>
            ))}
          </div>

          <div className="tab-content">
            {activeTab === 'duplicates' && <DuplicatesView data={data} report={report} />}
            {activeTab === 'families' && <FamiliesView data={data} />}
            {activeTab === 'merge' && <MergeView data={data} />}
            {activeTab === 'coverage' && <CoverageView data={data} report={report} />}
            {activeTab === 'noise' && <NoiseView data={data} />}
            {activeTab === 'unique' && <UniqueView data={data} />}
          </div>
        </div>

      </div>
    </div>
  );
}

function DuplicatesView({ data, report }: { data: SimilarityResult; report: InsightsReport | null }) {
  if (!data.duplicates?.length) {
    return <div className="empty-state">No duplicate detections found.</div>;
  }
  return (
    <div className="card-list">
      {data.duplicates.map((dup, i) => (
        <div key={i} className="insight-card duplicate-card">
          <div className="card-header">
            <span className="similarity-badge" style={badgeStyle(dup.similarity)}>
              {(dup.similarity * 100).toFixed(0)}% Similar
            </span>
          </div>
          <div className="card-alerts">
            {dup.alert_names?.map((name, j) => (
              <div key={j} className="alert-name">{name}</div>
            ))}
          </div>
          <div className="card-explanation">
            {report?.enriched_dups?.[i] ?? dup.explanation}
          </div>
        </div>
      ))}
    </div>
  );
}

function FamiliesView({ data }: { data: SimilarityResult }) {
  if (!data.families?.length) {
    return <div className="empty-state">No detection families identified.</div>;
  }
  return (
    <div className="card-list">
      {data.families.map((fam, i) => (
        <div key={i} className="insight-card family-card">
          <div className="card-header">
            <h3>{fam.name}</h3>
            <span className="member-count">{fam.alert_names?.length || 0} detections</span>
          </div>
          <div className="card-alerts">
            {fam.alert_names?.map((name, j) => (
              <div key={j} className="alert-name">{name}</div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function MergeView({ data }: { data: SimilarityResult }) {
  if (!data.merge_suggestions?.length) {
    return <div className="empty-state">No merge suggestions. Your detections are well-organized.</div>;
  }
  return (
    <div className="card-list">
      {data.merge_suggestions.map((sug, i) => (
        <div key={i} className="insight-card merge-card">
          <div className="card-header">
            <h3>Merge {sug.alert_names?.length || 0} rules into 1</h3>
          </div>
          <div className="card-reason">{sug.reason}</div>
          <div className="card-alerts">
            {sug.alert_names?.map((name, j) => (
              <div key={j} className="alert-name">{name}</div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function CoverageView({ data, report }: { data: SimilarityResult; report: InsightsReport | null }) {
  if (!data.coverage_insights?.length) {
    return <div className="empty-state">No coverage insights available.</div>;
  }
  return (
    <div className="card-list">
      {data.coverage_insights.map((insight, i) => (
        <div key={i} className="insight-card coverage-card">
          <p>{report?.enriched_gaps?.[i] ?? insight}</p>
        </div>
      ))}
    </div>
  );
}

function NoiseView({ data }: { data: SimilarityResult }) {
  if (!data.noise_alerts?.length) {
    return <div className="empty-state">No noise alerts detected.</div>;
  }
  return (
    <div className="card-list">
      {data.noise_alerts.map((name, i) => (
        <div key={i} className="insight-card">
          <div className="alert-name">[!!] {name}</div>
          <div className="card-explanation">
            Sparse feature vector — likely a threshold-only rule. Review for contextual conditions.
          </div>
        </div>
      ))}
    </div>
  );
}

function UniqueView({ data }: { data: SimilarityResult }) {
  if (!data.unique_detections?.length) {
    return <div className="empty-state">No unique (standalone) detections found.</div>;
  }
  return (
    <div className="card-list">
      {data.unique_detections.map((name, i) => (
        <div key={i} className="insight-card unique-card">
          <div className="alert-name">{name}</div>
        </div>
      ))}
    </div>
  );
}

function badgeStyle(similarity: number): React.CSSProperties {
  const r = Math.round(255 * similarity);
  const g = Math.round(255 * (1 - similarity));
  return {
    backgroundColor: `rgba(${r}, ${g}, 60, 0.15)`,
    color: `rgb(${r}, ${g}, 60)`,
    border: `1px solid rgba(${r}, ${g}, 60, 0.3)`,
  };
}
```

- [ ] **Step 2: Build**

```bash
cd frontend && npm run build
```

Expected: `✓ built in Xms` — no TypeScript or CSS errors.

- [ ] **Step 3: Manual visual check**

Start the dev server and run a full analysis:

```bash
./dev.sh start
```

Open `http://localhost:5173`, select a client, click `[ ANALYZE ... ]`, then `→ Alert Insights`.

Verify:
- [ ] Two-pane layout: left panel (220px) + right tabs panel
- [ ] Left panel shows skeleton placeholders when `insights_report` is null (server-side LLM not connected)
- [ ] Left panel shows summary / top priority / strengths / signals when `insights_report` is present
- [ ] SIGNALS section shows `[N!] noise` and `[N!] gaps` in red when counts > 0
- [ ] Right panel has 6 tabs: Duplicates, Families, Merge, Coverage, Noise, Unique
- [ ] Noise tab lists `[!!] alertName` with explanation text
- [ ] Duplicates tab falls back to algorithmic `dup.explanation` when `report` is null
- [ ] Coverage tab falls back to raw coverage_insights strings when `report` is null
- [ ] Full-height layout: insights grid fills viewport correctly

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/AlertInsights.tsx
git commit -m "feat: redesign AlertInsights as two-pane command center with Noise tab and LLM enrichment"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| Expand `commonCategories` to 20 entries | Task 2 Step 3 |
| `findNoiseAlerts()` function | Task 2 Step 4 |
| Call as Step 8 in `Analyze()` | Task 2 Step 5 |
| `NoiseAlerts []string` on `SimilarityResult` | Task 1 Step 1 |
| `InsightsReport` struct | Task 1 Step 2 |
| `InsightsReport` field on `AnalyzeResponse` | Task 1 Step 3 |
| `insights.Enrich()` function with LLM prompt | Task 3 Step 3 |
| JSON parsing + markdown fence stripping | Task 3 Step 3 |
| Cache key `insights_v1:{client}:{sha256[:12]}` | Task 4 Step 2 |
| Handler calls `Enrich()` after `similarity.Analyze()` | Task 4 Step 3 |
| `InsightsReport` in `AnalyzeResponse` response | Task 4 Step 4 |
| Frontend `InsightsReport` interface | Task 5 Step 2 |
| `noise_alerts` on `SimilarityResult` TS type | Task 5 Step 1 |
| `insights_report` on `AnalyzeResponse` TS type | Task 5 Step 3 |
| Pass `report` prop to `AlertInsights` in App.tsx | Task 5 Step 4 |
| `.insights-grid`, `.insights-panel`, all CSS classes | Task 6 Step 2 |
| `.insights-skeleton` pulsing placeholder | Task 6 Step 2 |
| Left panel: summary, top_priority, strengths, signals | Task 7 Step 1 |
| Right panel: 6 tabs including Noise | Task 7 Step 1 |
| Noise tab: `[!!]` format + static explanation | Task 7 Step 1 |
| Duplicates: `enriched_dups[i]` with fallback | Task 7 Step 1 |
| Coverage: `enriched_gaps[i]` with fallback | Task 7 Step 1 |
| Skeleton shown when `report` is null | Task 7 Step 1 |
| Danger red for noise/gap counts > 0 | Task 7 Step 1 |

All spec requirements covered. No gaps found.
