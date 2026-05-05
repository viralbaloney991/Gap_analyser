# Correlation Alert Suggestions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add on-demand LLM-generated correlation and anomaly alert suggestions for each Advanced Use Cases gap item, displayed in a slide-in drawer, cached per client+gap in NeonDB.

**Architecture:** New `POST /api/correlations` endpoint backed by `GenerateCorrelations` LLM function and `correlation_cache` NeonDB table (append-only, same pattern as `suggestion_cache`). Frontend adds a "Suggest correlations →" button to each Advanced Use Cases item and renders results in a fixed-right slide-in drawer inside `AlertInsights.tsx`.

**Tech Stack:** Go 1.25 (backend), React + TypeScript + Vite (frontend), NeonDB/pgx (cache), Anthropic Claude / NVIDIA NIM / Google Gemini (LLM)

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/internal/models/models.go` | Modify | Add `CorrelationsRequest`, `CorrelationSuggestion`, `CorrelationsResponse` types |
| `backend/internal/store/store.go` | Modify | Add `correlation_cache` table migration + `CorrelationRow` type + `GetCachedCorrelations` + `AppendCachedCorrelations` |
| `backend/internal/llm/correlations.go` | **Create** | `GenerateCorrelations`, `buildCorrelationsUserMessage`, `parseCorrelations` |
| `backend/internal/llm/correlations_test.go` | **Create** | Unit tests for `parseCorrelations` |
| `backend/internal/api/handlers.go` | Modify | Add `HandleCorrelations`, `buildCorrelationCacheKey`, `mergeCorrelations` |
| `backend/internal/api/correlations_handler_test.go` | **Create** | Validation unit tests for `HandleCorrelations` |
| `backend/cmd/server/main.go` | Modify | Register `/api/correlations` route |
| `frontend/src/types/index.ts` | Modify | Add `CorrelationSuggestion`, `CorrelationsResponse` interfaces |
| `frontend/src/services/api.ts` | Modify | Add `fetchCorrelations` function |
| `frontend/src/components/AlertInsights.tsx` | Modify | Add drawer state, `coveredTechniques` memo, `openCorrelationDrawer`, "Suggest correlations →" button, drawer JSX |
| `frontend/src/App.css` | Modify | Add drawer + button CSS |

---

## Task 1: Backend Models

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Write failing test** — verify the new types compile

Create `backend/internal/models/models_correlations_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
)

func TestCorrelationsRequestRoundtrip(t *testing.T) {
	req := CorrelationsRequest{
		Client:            "acme",
		GapProse:          "T1059 has threshold alert but no anomaly layer",
		LogSources:        []string{"sysmon"},
		CoveredTechniques: []string{"T1078", "T1110"},
		Provider:          "claude",
		Force:             false,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got CorrelationsRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Client != req.Client || got.GapProse != req.GapProse {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}

func TestCorrelationSuggestionRoundtrip(t *testing.T) {
	sug := CorrelationSuggestion{
		Type:               "correlation",
		Title:              "Execution → Persistence chain",
		Description:        "Fire when T1059 and T1547 both seen for same entity within 30 min",
		InvolvedTechniques: []string{"T1059", "T1547"},
		QuerySkeleton:      "alert_name:*scripting* AND ...",
		Priority:           "high",
	}
	data, err := json.Marshal(sug)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got CorrelationSuggestion
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != sug.Type || got.Priority != sug.Priority {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/models/... -run TestCorrelations -v
```

Expected: FAIL — `CorrelationsRequest undefined`

- [ ] **Step 3: Add the three types to `backend/internal/models/models.go`**

Append after the existing `NoiseResponse` type at the bottom of the file:

```go
// CorrelationsRequest is the payload for POST /api/correlations.
type CorrelationsRequest struct {
	Client            string   `json:"client"`
	GapProse          string   `json:"gap_prose"`
	LogSources        []string `json:"log_sources"`
	CoveredTechniques []string `json:"covered_techniques"`
	Provider          string   `json:"provider"` // empty = use configured default
	Force             bool     `json:"force"`
}

// CorrelationSuggestion is a single LLM-generated correlation or anomaly rule.
type CorrelationSuggestion struct {
	Type               string   `json:"type"`                // "correlation" | "anomaly"
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	InvolvedTechniques []string `json:"involved_techniques"` // for type=correlation, must include >= 1 from covered_techniques
	QuerySkeleton      string   `json:"query_skeleton"`      // valid Lucene
	Priority           string   `json:"priority"`            // "critical"|"high"|"medium"|"low"
}

// CorrelationsResponse is the payload returned by POST /api/correlations.
type CorrelationsResponse struct {
	Suggestions []CorrelationSuggestion `json:"suggestions"`
	Provider    string                  `json:"provider"`
	Cached      bool                    `json:"cached"`
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/models/... -run TestCorrelations -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/models/models.go internal/models/models_correlations_test.go
git commit -m "feat(correlations): add CorrelationsRequest/Suggestion/Response models"
```

---

## Task 2: Store — correlation_cache Table and Methods

**Files:**
- Modify: `backend/internal/store/store.go`

- [ ] **Step 1: Write failing test** — compile test for new store methods

Create `backend/internal/store/correlations_store_test.go`:

```go
package store

import (
	"encoding/json"
	"testing"
)

// TestCorrelationRowJSON verifies CorrelationRow serialisation does not panic.
func TestCorrelationRowJSON(t *testing.T) {
	raw, _ := json.Marshal([]map[string]string{{"type": "correlation", "title": "test"}})
	row := CorrelationRow{
		CacheKey:    "abc123",
		Client:      "acme",
		Suggestions: json.RawMessage(raw),
		Provider:    "claude",
	}
	if row.CacheKey != "abc123" {
		t.Errorf("unexpected cache key: %s", row.CacheKey)
	}
	if len(row.Suggestions) == 0 {
		t.Error("suggestions should not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/store/... -run TestCorrelationRow -v
```

Expected: FAIL — `CorrelationRow undefined`

- [ ] **Step 3: Add `correlation_cache` to the migrate function**

In `backend/internal/store/store.go`, find the `migrate` function. The current SQL string ends with:
```
CREATE INDEX IF NOT EXISTS suggestion_cache_key_idx ON suggestion_cache(cache_key);
```

Extend it by appending before the closing backtick:

```sql
		CREATE TABLE IF NOT EXISTS correlation_cache (
			id           BIGSERIAL   PRIMARY KEY,
			cache_key    TEXT        NOT NULL,
			client       TEXT        NOT NULL,
			suggestions  JSONB       NOT NULL,
			provider     TEXT        NOT NULL,
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS correlation_cache_key_idx ON correlation_cache(cache_key);
```

- [ ] **Step 4: Add `CorrelationRow` type and the two store methods**

Append after `AppendCachedSuggestions` at the end of `backend/internal/store/store.go`:

```go
// CorrelationRow is one generation of LLM correlation suggestions for a (client, gap_prose) pair.
type CorrelationRow struct {
	CacheKey    string
	Client      string
	Suggestions json.RawMessage // serialised []models.CorrelationSuggestion
	Provider    string
	GeneratedAt time.Time
}

// GetCachedCorrelations returns all correlation rows for a cache key ordered ASC by generated_at.
// Returns an empty (non-nil) slice when no rows exist.
func (s *Store) GetCachedCorrelations(ctx context.Context, cacheKey string) ([]CorrelationRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cache_key, client, suggestions, provider, generated_at
		FROM correlation_cache
		WHERE cache_key = $1
		ORDER BY generated_at ASC
	`, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("query correlation_cache: %w", err)
	}
	defer rows.Close()

	var result []CorrelationRow
	for rows.Next() {
		var row CorrelationRow
		var suggestions []byte
		if err := rows.Scan(&row.CacheKey, &row.Client, &suggestions, &row.Provider, &row.GeneratedAt); err != nil {
			return nil, fmt.Errorf("scan correlation_cache row: %w", err)
		}
		row.Suggestions = json.RawMessage(suggestions)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("correlation_cache rows error: %w", err)
	}
	if result == nil {
		result = []CorrelationRow{}
	}
	return result, nil
}

// AppendCachedCorrelations inserts one new correlation generation row.
// Existing rows are never modified — the table is append-only.
func (s *Store) AppendCachedCorrelations(ctx context.Context, row CorrelationRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO correlation_cache (cache_key, client, suggestions, provider, generated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, row.CacheKey, row.Client, string(row.Suggestions), row.Provider, row.GeneratedAt)
	if err != nil {
		return fmt.Errorf("insert correlation_cache: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/store/... -run TestCorrelationRow -v
```

Expected: PASS

- [ ] **Step 6: Verify build**

```bash
cd backend && go build ./...
```

Expected: no errors

- [ ] **Step 7: Commit**

```bash
cd backend && git add internal/store/store.go internal/store/correlations_store_test.go
git commit -m "feat(correlations): add correlation_cache table migration and store methods"
```

---

## Task 3: LLM — GenerateCorrelations

**Files:**
- Create: `backend/internal/llm/correlations.go`
- Create: `backend/internal/llm/correlations_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/llm/correlations_test.go`:

```go
package llm

import (
	"strings"
	"testing"
)

func TestParseCorrelations_validJSON(t *testing.T) {
	raw := `[
	  {
	    "type": "correlation",
	    "title": "Execution + Persistence chain",
	    "description": "T1059 followed by T1547 for same entity within 30 min",
	    "involved_techniques": ["T1059", "T1547"],
	    "query_skeleton": "alert_name:*script* AND ...",
	    "priority": "high"
	  },
	  {
	    "type": "anomaly",
	    "title": "T1059 frequency baseline",
	    "description": "Alert when command execution count exceeds 2 std dev from 7-day baseline",
	    "involved_techniques": ["T1059"],
	    "query_skeleton": "subsystemName:sysmon AND ...",
	    "priority": "medium"
	  }
	]`

	got, err := parseCorrelations(raw)
	if err != nil {
		t.Fatalf("parseCorrelations failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 correlations, got %d", len(got))
	}
	if got[0].Type != "correlation" {
		t.Errorf("expected type=correlation, got %q", got[0].Type)
	}
	if got[1].Type != "anomaly" {
		t.Errorf("expected type=anomaly, got %q", got[1].Type)
	}
	if got[0].Priority != "high" {
		t.Errorf("expected priority=high, got %q", got[0].Priority)
	}
}

func TestParseCorrelations_markdownFenced(t *testing.T) {
	raw := "```json\n[\n  {\"type\":\"anomaly\",\"title\":\"T\",\"description\":\"D\",\"involved_techniques\":[\"T1059\"],\"query_skeleton\":\"q\",\"priority\":\"low\"}\n]\n```"

	got, err := parseCorrelations(raw)
	if err != nil {
		t.Fatalf("parseCorrelations with fences failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 correlation, got %d", len(got))
	}
	if got[0].Title != "T" {
		t.Errorf("unexpected title: %q", got[0].Title)
	}
}

func TestParseCorrelations_literalNewlineInString(t *testing.T) {
	// Some LLMs emit literal newlines inside JSON strings.
	raw := "{\"type\":\"correlation\",\"title\":\"T\",\"description\":\"line1\nline2\",\"involved_techniques\":[\"T1059\"],\"query_skeleton\":\"q\",\"priority\":\"high\"}"
	wrapped := "[\n  " + raw + "\n]"

	got, err := parseCorrelations(wrapped)
	if err != nil {
		t.Fatalf("parseCorrelations with literal newline failed: %v", err)
	}
	if !strings.Contains(got[0].Description, "line1") {
		t.Errorf("expected description to contain 'line1', got %q", got[0].Description)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/llm/... -run TestParseCorrelations -v
```

Expected: FAIL — `parseCorrelations undefined`

- [ ] **Step 3: Create `backend/internal/llm/correlations.go`**

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"coralogix-alert-analyzer/internal/models"
)

// MaxCorrelations is the hard cap on suggestions returned per gap.
const MaxCorrelations = 5

const correlationsSystemPrompt = `You are a Coralogix SIEM alert engineering expert specialising in MITRE ATT&CK correlation and anomaly detection.

You are given:
1. A specific detection gap — a technique that only has threshold-based alerts and lacks a deeper detection layer.
2. The client's already-covered MITRE techniques — techniques that have existing alerts.
3. The client's available log sources.

Your job: produce up to 5 suggestions split into two types.

**type: "correlation"** (up to 3)
- Link the gap technique to at least one technique from the already-covered list.
- The rule fires when BOTH are seen for the same entity within a time window.
- involved_techniques must include at least one technique from the covered list.

**type: "anomaly"** (up to 2)
- Stay focused on the gap technique itself.
- Suggest a behavioural baseline or statistical anomaly rule on top of the existing threshold.

Rules:
- query_skeleton must be valid Lucene.
- priority: "critical" | "high" | "medium" | "low".
- title: concise, action-oriented (under 10 words).
- description: one sentence explaining what the rule detects and why it matters.
- Use only log sources from the provided list; do not fabricate sources.
- Return at most 5 objects total (3 correlation + 2 anomaly).

Respond ONLY with a JSON array. No markdown, no explanation, just the array.
Each object must have exactly these fields:
{
  "type": "correlation" or "anomaly",
  "title": "Short rule title",
  "description": "One sentence description",
  "involved_techniques": ["T1059", ...],
  "query_skeleton": "Lucene query",
  "priority": "high"
}`

// CorrelationInput is the context for generating correlation suggestions.
type CorrelationInput struct {
	GapProse          string
	LogSources        []string
	CoveredTechniques []string
}

// GenerateCorrelations uses the LLM to produce correlation and anomaly suggestions for a gap.
func GenerateCorrelations(ctx context.Context, provider Provider, input CorrelationInput) ([]models.CorrelationSuggestion, error) {
	userMsg := buildCorrelationsUserMessage(input)

	log.Printf("INFO [correlations] requesting %s for gap=%q log_sources=%d covered=%d",
		provider.Name(), truncate(input.GapProse, 60), len(input.LogSources), len(input.CoveredTechniques))

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: correlationsSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    4096,
		FastMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	suggestions, err := parseCorrelations(resp)
	if err != nil {
		return nil, fmt.Errorf("parse correlations: %w", err)
	}

	if len(suggestions) > MaxCorrelations {
		suggestions = suggestions[:MaxCorrelations]
	}

	return suggestions, nil
}

func buildCorrelationsUserMessage(input CorrelationInput) string {
	var sb strings.Builder

	sb.WriteString("## Detection Gap\n")
	sb.WriteString(input.GapProse)
	sb.WriteString("\n\n")

	sb.WriteString("## Already-Covered Techniques\n")
	if len(input.CoveredTechniques) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, t := range input.CoveredTechniques {
			sb.WriteString("- ")
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n## Available Log Sources\n")
	if len(input.LogSources) == 0 {
		sb.WriteString("(none specified — suggest based on gap technique)\n")
	} else {
		for _, ls := range input.LogSources {
			sb.WriteString("- ")
			sb.WriteString(ls)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\nSuggest up to 5 correlation and anomaly rules (3 correlation + 2 anomaly max).")

	return sb.String()
}

func parseCorrelations(raw string) ([]models.CorrelationSuggestion, error) {
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

	cleaned = sanitizeJSONStrings(cleaned)

	var suggestions []models.CorrelationSuggestion
	if err := json.Unmarshal([]byte(cleaned), &suggestions); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w\nRaw response:\n%s", err, raw[:min(len(raw), 500)])
	}

	return suggestions, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && go test ./internal/llm/... -run TestParseCorrelations -v
```

Expected: all 3 PASS

- [ ] **Step 5: Verify full build**

```bash
cd backend && go build ./...
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/llm/correlations.go internal/llm/correlations_test.go
git commit -m "feat(correlations): add GenerateCorrelations LLM function"
```

---

## Task 4: Handler + Route

**Files:**
- Modify: `backend/internal/api/handlers.go`
- Create: `backend/internal/api/correlations_handler_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/api/correlations_handler_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestHandleCorrelations_rejectsGet(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/correlations", nil)
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleCorrelations_missingClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"gap_prose":"some gap"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/correlations", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCorrelations_missingGapProse(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"acme"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/correlations", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCorrelations_unknownClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"ghost","gap_prose":"T1059 has no anomaly layer"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/correlations", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestBuildCorrelationCacheKey_stable(t *testing.T) {
	key1 := buildCorrelationCacheKey("acme", "T1059 threshold alert, no anomaly layer")
	key2 := buildCorrelationCacheKey("acme", "T1059 threshold alert, no anomaly layer")
	if key1 != key2 {
		t.Error("buildCorrelationCacheKey should be deterministic")
	}
	key3 := buildCorrelationCacheKey("acme", "  T1059 threshold alert, no anomaly layer  ")
	if key1 != key3 {
		t.Error("buildCorrelationCacheKey should trim whitespace before hashing")
	}
	keyOther := buildCorrelationCacheKey("other-client", "T1059 threshold alert, no anomaly layer")
	if key1 == keyOther {
		t.Error("buildCorrelationCacheKey should differ for different clients")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/api/... -run "TestHandleCorrelations|TestBuildCorrelationCacheKey" -v
```

Expected: FAIL — `HandleCorrelations undefined`

- [ ] **Step 3: Add `HandleCorrelations`, `buildCorrelationCacheKey`, `mergeCorrelations` to `backend/internal/api/handlers.go`**

Append these three functions after `buildSuggestionCacheKey` (around line 948 — after the closing brace of `mergeCachedSuggestions`):

```go
// buildCorrelationCacheKey returns a stable SHA256 hex key for a (client, gap_prose) pair.
func buildCorrelationCacheKey(client, gapProse string) string {
	normalised := strings.ToLower(strings.TrimSpace(gapProse))
	raw := client + "|" + normalised
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// mergeCorrelations flattens correlation rows into a deduplicated list.
// rows must be ordered ASC by generated_at so later rows win dedup conflicts.
// Returns merged suggestions sorted by priority then title, and the provider of the most recent row.
func mergeCorrelations(rows []store.CorrelationRow) ([]models.CorrelationSuggestion, string) {
	type entry struct {
		sug   models.CorrelationSuggestion
		genAt time.Time
	}
	seen := make(map[string]entry)
	priorityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	var latestProvider string

	for _, row := range rows {
		latestProvider = row.Provider
		var sugs []models.CorrelationSuggestion
		if err := json.Unmarshal(row.Suggestions, &sugs); err != nil {
			log.Printf("WARN [correlations] unmarshal cached correlations: %v", err)
			continue
		}
		for _, s := range sugs {
			key := strings.ToLower(s.Title)
			existing, exists := seen[key]
			if !exists || row.GeneratedAt.After(existing.genAt) {
				seen[key] = entry{sug: s, genAt: row.GeneratedAt}
			}
		}
	}

	merged := make([]models.CorrelationSuggestion, 0, len(seen))
	for _, e := range seen {
		merged = append(merged, e.sug)
	}
	sort.Slice(merged, func(i, j int) bool {
		pi := priorityOrder[strings.ToLower(merged[i].Priority)]
		pj := priorityOrder[strings.ToLower(merged[j].Priority)]
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(merged[i].Title) < strings.ToLower(merged[j].Title)
	})
	return merged, latestProvider
}

// HandleCorrelations handles POST /api/correlations.
// It returns LLM-generated correlation and anomaly suggestions for a single Advanced Use Cases gap item.
func (h *Handler) HandleCorrelations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.CorrelationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	req.GapProse = strings.TrimSpace(req.GapProse)
	if req.GapProse == "" {
		writeError(w, http.StatusBadRequest, "missing required field: gap_prose")
		return
	}

	if _, ok := h.config.Clients[req.Client]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	// Resolve LLM provider — same logic as HandleSuggestions.
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	if h.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
	}

	var provider llm.Provider
	var err error
	if req.Provider != "" {
		provider, err = llm.NewProvider(req.Provider, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	} else {
		providerName := h.config.LLM.SuggestionProvider
		if providerName == "" {
			providerName = h.config.LLM.DefaultProvider
		}
		provider, err = llm.NewClassifierProvider(providerName, h.config.LLM.SuggestionModel, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	cacheKey := buildCorrelationCacheKey(req.Client, req.GapProse)

	// Cache hit path.
	if !req.Force && h.alertStore != nil {
		cached, cErr := h.alertStore.GetCachedCorrelations(ctx, cacheKey)
		if cErr != nil {
			log.Printf("WARN HandleCorrelations client=%s cache lookup: %v", req.Client, cErr)
		} else if len(cached) > 0 {
			merged, latestProvider := mergeCorrelations(cached)
			log.Printf("INFO HandleCorrelations client=%s cache=hit suggestions=%d", req.Client, len(merged))
			writeJSON(w, http.StatusOK, models.CorrelationsResponse{
				Suggestions: merged,
				Provider:    latestProvider,
				Cached:      true,
			})
			return
		}
	}

	// Cache miss — call LLM.
	input := llm.CorrelationInput{
		GapProse:          req.GapProse,
		LogSources:        req.LogSources,
		CoveredTechniques: req.CoveredTechniques,
	}
	sugs, llmErr := llm.GenerateCorrelations(ctx, provider, input)
	if llmErr != nil {
		log.Printf("WARN HandleCorrelations client=%s llm error: %v", req.Client, llmErr)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("LLM generation failed: %v", llmErr))
		return
	}

	effectiveProvider := req.Provider
	if effectiveProvider == "" {
		effectiveProvider = h.config.LLM.SuggestionProvider
		if effectiveProvider == "" {
			effectiveProvider = h.config.LLM.DefaultProvider
		}
	}
	log.Printf("INFO HandleCorrelations client=%s cache=miss provider=%s suggestions=%d", req.Client, effectiveProvider, len(sugs))

	if len(sugs) > 0 && h.alertStore != nil {
		sugsJSON, _ := json.Marshal(sugs)
		if appendErr := h.alertStore.AppendCachedCorrelations(ctx, store.CorrelationRow{
			CacheKey:    cacheKey,
			Client:      req.Client,
			Suggestions: json.RawMessage(sugsJSON),
			Provider:    effectiveProvider,
			GeneratedAt: time.Now().UTC(),
		}); appendErr != nil {
			log.Printf("WARN HandleCorrelations client=%s cache append: %v", req.Client, appendErr)
		}
	}

	// For force requests, re-fetch merged pool; otherwise return LLM result directly.
	if req.Force && h.alertStore != nil {
		allRows, fetchErr := h.alertStore.GetCachedCorrelations(ctx, cacheKey)
		if fetchErr == nil && len(allRows) > 0 {
			merged, latestProvider := mergeCorrelations(allRows)
			writeJSON(w, http.StatusOK, models.CorrelationsResponse{
				Suggestions: merged,
				Provider:    latestProvider,
				Cached:      false,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, models.CorrelationsResponse{
		Suggestions: sugs,
		Provider:    effectiveProvider,
		Cached:      false,
	})
}
```

- [ ] **Step 4: Register the route in `backend/cmd/server/main.go`**

Find the line:
```go
mux.HandleFunc("/api/suggestions", handler.HandleSuggestions)
```

Add immediately after it:
```go
mux.HandleFunc("/api/correlations", handler.HandleCorrelations)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/api/... -run "TestHandleCorrelations|TestBuildCorrelationCacheKey" -v
```

Expected: all 5 PASS

- [ ] **Step 6: Verify full build**

```bash
cd backend && go build ./...
```

Expected: no errors

- [ ] **Step 7: Run all backend tests**

```bash
cd backend && go test ./...
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
cd backend && git add internal/api/handlers.go internal/api/correlations_handler_test.go cmd/server/main.go
git commit -m "feat(correlations): add HandleCorrelations endpoint and /api/correlations route"
```

---

## Task 5: Frontend Types and API Function

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: Add types to `frontend/src/types/index.ts`**

Append after the `NoiseResponse` interface at the end of the file:

```typescript
export interface CorrelationSuggestion {
  type: 'correlation' | 'anomaly';
  title: string;
  description: string;
  involved_techniques: string[];
  query_skeleton: string;
  priority: 'critical' | 'high' | 'medium' | 'low';
}

export interface CorrelationsResponse {
  suggestions: CorrelationSuggestion[];
  provider: string;
  cached: boolean;
}
```

- [ ] **Step 2: Add `fetchCorrelations` to `frontend/src/services/api.ts`**

First update the import at the top — find:
```typescript
import type { AnalyzeResponse, ClientInfo, ExportNarrativeReport, InsightsReport, NoiseAlert, NoiseResponse, SuggestionsResponse } from '../types';
```
Replace with:
```typescript
import type { AnalyzeResponse, ClientInfo, CorrelationsResponse, ExportNarrativeReport, InsightsReport, NoiseAlert, NoiseResponse, SuggestionsResponse } from '../types';
```

Then append after `fetchNoise` at the end of the file:

```typescript
export async function fetchCorrelations(
  client: string,
  gapProse: string,
  logSources: string[],
  coveredTechniques: string[],
  force = false,
): Promise<CorrelationsResponse> {
  const res = await fetch(`${API_BASE}/api/correlations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client,
      gap_prose: gapProse,
      log_sources: logSources,
      covered_techniques: coveredTechniques,
      force,
    }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Correlations failed' }));
    throw new Error(err.error || 'Failed to generate correlation suggestions');
  }
  return res.json();
}
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
cd frontend && git add src/types/index.ts src/services/api.ts
git commit -m "feat(correlations): add CorrelationSuggestion types and fetchCorrelations API function"
```

---

## Task 6: Frontend UI — Button, Drawer, CSS

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`
- Modify: `frontend/src/App.css`

- [ ] **Step 1: Update imports in `AlertInsights.tsx`**

Find the existing import line:
```typescript
import type { SimilarityResult, InsightsReport, MITRECoverageResult, DetectionFamily, ActionableRecommendation } from '../types';
```
Replace with:
```typescript
import type { SimilarityResult, InsightsReport, MITRECoverageResult, DetectionFamily, ActionableRecommendation, CorrelationSuggestion } from '../types';
```

Find:
```typescript
import { fetchInsights, fetchExportNarrative } from '../services/api';
```
Replace with:
```typescript
import { fetchInsights, fetchExportNarrative, fetchCorrelations } from '../services/api';
```

- [ ] **Step 2: Add drawer state and `coveredTechniques` memo**

In `AlertInsights.tsx`, find the line:
```typescript
  const [noiseFilter, setNoiseFilter] = useState<'all' | 'behavioral' | 'structural'>('all');
```

Add immediately after it:

```typescript
  const [correlationDrawer, setCorrelationDrawer] = useState<{
    gapProse: string;
    suggestions: CorrelationSuggestion[];
    loading: boolean;
    cached: boolean;
  } | null>(null);

  const coveredTechniques = useMemo(
    () =>
      Object.entries(mitreCoverage.technique_coverage ?? {})
        .filter(([, v]) => v.alert_count > 0)
        .map(([k]) => k),
    [mitreCoverage],
  );
```

- [ ] **Step 3: Add `openCorrelationDrawer` function**

Find the `toggleQuery` function:
```typescript
  const toggleQuery = (key: string) => {
```

Add immediately before it:

```typescript
  const openCorrelationDrawer = async (rec: ActionableRecommendation) => {
    setCorrelationDrawer({ gapProse: rec.prose, suggestions: [], loading: true, cached: false });
    try {
      const logSources = rec.log_source ? [rec.log_source] : [];
      const result = await fetchCorrelations(client, rec.prose, logSources, coveredTechniques);
      setCorrelationDrawer({ gapProse: rec.prose, suggestions: result.suggestions, loading: false, cached: result.cached });
    } catch (e) {
      console.warn('[correlations]', e);
      setCorrelationDrawer(prev => prev ? { ...prev, loading: false } : null);
    }
  };

```

- [ ] **Step 4: Add `onCorrelate` parameter to `renderActionableSection`**

Find the function signature:
```typescript
  const renderActionableSection = (
    title: string,
    actionable: ActionableRecommendation[] | undefined,
    fallback: string[] | undefined
  ) => {
```
Replace with:
```typescript
  const renderActionableSection = (
    title: string,
    actionable: ActionableRecommendation[] | undefined,
    fallback: string[] | undefined,
    onCorrelate?: (rec: ActionableRecommendation) => void,
  ) => {
```

Inside the function, find the block that renders each actionable item. After the copy button closing `</button>`, and before the closing `</div>` of the expanded query block, add the correlations button. The exact location is after:

```typescript
                  <button
                    onClick={() => navigator.clipboard.writeText(item.query_skeleton)}
                    style={{ marginTop: 4, background: 'none', border: '1px solid #4b5563', cursor: 'pointer', color: '#9ca3af', fontSize: 11, padding: '2px 8px', borderRadius: 4 }}
                  >
                    Copy
                  </button>
```

But to add it outside the expanded query block (always visible), add just before the final closing `</div>` of the insight card but after the expandable section. Find the end of the card — the block ends with:
```typescript
              </div>
            );
          })}
```

Instead, add the correlations button after the `{expanded && (...)}` block and before the card's closing `</div>`:

Find:
```typescript
                {expanded && (
                  <div style={{ marginTop: 6 }}>
                    <pre style={{ background: '#1e1e2e', color: '#cdd6f4', padding: '8px 12px', borderRadius: 4, fontSize: 12, overflowX: 'auto', margin: 0 }}>
                      {item.query_skeleton}
                    </pre>
                    <button
                      onClick={() => navigator.clipboard.writeText(item.query_skeleton)}
                      style={{ marginTop: 4, background: 'none', border: '1px solid #4b5563', cursor: 'pointer', color: '#9ca3af', fontSize: 11, padding: '2px 8px', borderRadius: 4 }}
                    >
                      Copy
                    </button>
                  </div>
                )}
              </div>
            );
          })}
```

Replace with:
```typescript
                {expanded && (
                  <div style={{ marginTop: 6 }}>
                    <pre style={{ background: '#1e1e2e', color: '#cdd6f4', padding: '8px 12px', borderRadius: 4, fontSize: 12, overflowX: 'auto', margin: 0 }}>
                      {item.query_skeleton}
                    </pre>
                    <button
                      onClick={() => navigator.clipboard.writeText(item.query_skeleton)}
                      style={{ marginTop: 4, background: 'none', border: '1px solid #4b5563', cursor: 'pointer', color: '#9ca3af', fontSize: 11, padding: '2px 8px', borderRadius: 4 }}
                    >
                      Copy
                    </button>
                  </div>
                )}
                {onCorrelate && (
                  <button
                    type="button"
                    className="corr-suggest-btn"
                    onClick={() => onCorrelate(item)}
                  >
                    Suggest correlations →
                  </button>
                )}
              </div>
            );
          })}
```

- [ ] **Step 5: Pass `onCorrelate` for the Advanced Use Cases section**

Find the Advanced Use Cases call:
```tsx
                {renderActionableSection('Advanced Use Cases', effectiveReport?.actionable_gaps?.advanced_use_cases, effectiveReport?.gap_categories.advanced_use_cases)}
```
Replace with:
```tsx
                {renderActionableSection('Advanced Use Cases', effectiveReport?.actionable_gaps?.advanced_use_cases, effectiveReport?.gap_categories.advanced_use_cases, openCorrelationDrawer)}
```

- [ ] **Step 6: Add the correlation drawer JSX**

Find the closing `</div>` of the `alert-insights` root element:
```tsx
    </div>
  );
}
```

Add the drawer and backdrop just before the outer closing `</div>`:

```tsx
      {/* Correlation suggestions drawer */}
      {correlationDrawer && (
        <>
          <div className="corr-backdrop" onClick={() => setCorrelationDrawer(null)} />
          <div className="corr-drawer">
            <div className="corr-drawer-header">
              <span className="corr-drawer-title" title={correlationDrawer.gapProse}>
                {correlationDrawer.gapProse.length > 80
                  ? correlationDrawer.gapProse.slice(0, 80) + '…'
                  : correlationDrawer.gapProse}
              </span>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexShrink: 0 }}>
                {correlationDrawer.cached && <span className="corr-cached-badge">Cached</span>}
                <button type="button" className="corr-close-btn" onClick={() => setCorrelationDrawer(null)}>✕</button>
              </div>
            </div>
            <div className="corr-drawer-body">
              {correlationDrawer.loading ? (
                <>
                  <div className="corr-skeleton skeleton" />
                  <div className="corr-skeleton skeleton" style={{ width: '80%' }} />
                  <div className="corr-skeleton skeleton" style={{ width: '90%' }} />
                </>
              ) : correlationDrawer.suggestions.length === 0 ? (
                <div className="state-empty">
                  <div className="state-empty__icon">◎</div>
                  <div className="state-empty__title">No suggestions generated</div>
                  <div className="state-empty__body">The LLM could not produce correlation rules for this gap. Try regenerating.</div>
                </div>
              ) : (
                <>
                  {(['correlation', 'anomaly'] as const).map(type => {
                    const items = correlationDrawer.suggestions.filter(s => s.type === type);
                    if (!items.length) return null;
                    return (
                      <div key={type} className="corr-section">
                        <div className="corr-section-title">
                          {type === 'correlation' ? 'Correlation Rules' : 'Anomaly Rules'}
                        </div>
                        {items.map((sug, i) => {
                          const corrKey = `${type}-${i}`;
                          const isOpen = expandedQueries.has(`corr-${corrKey}`);
                          return (
                            <div key={corrKey} className="corr-card">
                              <div className="corr-card-meta">
                                <span
                                  className="badge"
                                  style={{ backgroundColor: severityColors[sug.priority] ?? '#6b7280', color: sug.priority === 'medium' ? '#000' : '#fff', fontSize: '0.58rem' }}
                                >
                                  {sug.priority.toUpperCase()}
                                </span>
                                <span className="corr-card-title">{sug.title}</span>
                              </div>
                              <p className="corr-card-desc">{sug.description}</p>
                              {sug.involved_techniques.length > 0 && (
                                <div className="corr-techniques">
                                  {sug.involved_techniques.map(t => (
                                    <span key={t} className="corr-technique-chip">{t}</span>
                                  ))}
                                </div>
                              )}
                              <button
                                type="button"
                                onClick={() => toggleQuery(`corr-${corrKey}`)}
                                style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#60a5fa', fontSize: 12, padding: 0, marginTop: 4 }}
                              >
                                {isOpen ? '▼ Hide query' : '▶ Show query'}
                              </button>
                              {isOpen && (
                                <div style={{ marginTop: 6 }}>
                                  <pre style={{ background: '#1e1e2e', color: '#cdd6f4', padding: '8px 12px', borderRadius: 4, fontSize: 12, overflowX: 'auto', margin: 0 }}>
                                    {sug.query_skeleton}
                                  </pre>
                                  <button
                                    type="button"
                                    onClick={() => navigator.clipboard.writeText(sug.query_skeleton)}
                                    style={{ marginTop: 4, background: 'none', border: '1px solid #4b5563', cursor: 'pointer', color: '#9ca3af', fontSize: 11, padding: '2px 8px', borderRadius: 4 }}
                                  >
                                    Copy
                                  </button>
                                </div>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    );
                  })}
                </>
              )}
            </div>
          </div>
        </>
      )}
```

- [ ] **Step 7: Add CSS to `frontend/src/App.css`**

Append at the end of `frontend/src/App.css`:

```css
/* ── Correlation suggestions drawer ── */

.corr-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  z-index: 49;
}

.corr-drawer {
  position: fixed;
  top: 0;
  right: 0;
  width: 420px;
  height: 100vh;
  background: #111827;
  border-left: 1px solid #374151;
  z-index: 50;
  display: flex;
  flex-direction: column;
  animation: corrSlideIn 0.22s ease-out;
}

@media (prefers-reduced-motion: reduce) {
  .corr-drawer { animation: none; }
}

@keyframes corrSlideIn {
  from { transform: translateX(100%); }
  to   { transform: translateX(0); }
}

.corr-drawer-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 14px 16px 12px;
  border-bottom: 1px solid #1f2937;
}

.corr-drawer-title {
  font-size: 12px;
  font-weight: 600;
  color: #e5e7eb;
  line-height: 1.4;
}

.corr-cached-badge {
  font-size: 10px;
  font-weight: 700;
  color: #34d399;
  background: rgba(52, 211, 153, 0.12);
  border: 1px solid rgba(52, 211, 153, 0.3);
  border-radius: 4px;
  padding: 1px 6px;
  white-space: nowrap;
}

.corr-close-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: #6b7280;
  font-size: 14px;
  padding: 0 2px;
  line-height: 1;
}

.corr-close-btn:hover { color: #e5e7eb; }

.corr-drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 14px 16px;
}

.corr-section { margin-bottom: 20px; }

.corr-section-title {
  font-size: 10px;
  font-weight: 700;
  color: #f97316;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  margin-bottom: 10px;
}

.corr-card {
  background: #0f172a;
  border: 1px solid #1f2937;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
}

.corr-card-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.corr-card-title {
  font-size: 12px;
  font-weight: 600;
  color: #e5e7eb;
}

.corr-card-desc {
  font-size: 12px;
  color: #9ca3af;
  margin: 0 0 6px;
  line-height: 1.5;
}

.corr-techniques {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 6px;
}

.corr-technique-chip {
  font-size: 10px;
  font-weight: 600;
  color: #60a5fa;
  background: rgba(96, 165, 250, 0.1);
  border: 1px solid rgba(96, 165, 250, 0.2);
  border-radius: 3px;
  padding: 1px 5px;
}

.corr-skeleton {
  height: 56px;
  border-radius: 6px;
  margin-bottom: 10px;
  width: 100%;
}

.corr-suggest-btn {
  margin-top: 8px;
  background: none;
  border: 1px solid #374151;
  cursor: pointer;
  color: #f97316;
  font-size: 11px;
  padding: 3px 10px;
  border-radius: 4px;
  transition: border-color 0.15s, color 0.15s;
}

.corr-suggest-btn:hover {
  border-color: #f97316;
  color: #fb923c;
}
```

- [ ] **Step 8: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors

- [ ] **Step 9: Start the dev server and verify**

```bash
cd frontend && npm run dev
```

Open `http://localhost:5173` in a browser. Steps to verify:
1. Load a client that has Advanced Use Cases gaps in the Gaps tab.
2. Confirm each Advanced Use Cases item now shows a "Suggest correlations →" button.
3. Click the button — confirm the backdrop and drawer slide in from the right.
4. Confirm loading skeleton appears while the API call is in-flight.
5. Confirm correlation and anomaly rule cards render with priority badges, title, description, technique chips.
6. Click "▶ Show query" on a suggestion — confirm the Lucene query expands; "Copy" copies to clipboard.
7. Click the backdrop or ✕ — confirm drawer closes.
8. Click "Suggest correlations →" again on the same item — on second call, confirm "Cached" badge appears (if NeonDB is configured) or suggestions appear again.
9. Confirm no regressions in other Advanced Use Cases items, other gap categories, or other tabs.

- [ ] **Step 10: Commit**

```bash
cd frontend && git add src/components/AlertInsights.tsx src/App.css src/types/index.ts src/services/api.ts
git commit -m "feat(correlations): add correlation suggestions drawer to Advanced Use Cases gaps"
```
