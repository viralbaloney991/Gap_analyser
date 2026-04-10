# Suggestion Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cache LLM-generated alert suggestions in NeonDB keyed by `SHA256(technique_id + sorted log_sources)`, serving merged results from an append-only pool on cache hits and calling the LLM only on misses or `force: true`.

**Architecture:** New append-only `suggestion_cache` table in NeonDB. `GetCachedSuggestions` returns all rows for a key ordered ASC by `generated_at`. `AppendCachedSuggestions` inserts one new row per generation. `HandleSuggestions` computes the cache key after building log sources, short-circuits on cache hit, appends LLM results to the pool, and for `force: true` requests re-fetches and merges the full pool. Merge deduplicates by `alert_name` (case-insensitive, keeping most-recent on conflict) then sorts by priority → name. Store methods use `json.RawMessage` to avoid circular imports between `store` and `llm`.

**Tech Stack:** Go, NeonDB (pgx/v5), React/TypeScript

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Modify | `internal/store/store.go` | `suggestion_cache` table migration, `SuggestionRow` type, `GetCachedSuggestions`, `AppendCachedSuggestions` |
| Modify | `internal/store/store_test.go` | Integration tests for the two new store methods |
| Modify | `internal/models/models.go` | Add `Force bool` field to `SuggestionsRequest` |
| Modify | `internal/api/handlers.go` | `buildSuggestionCacheKey`, `mergeCachedSuggestions` helpers; cache-aware `HandleSuggestions` |
| Modify | `frontend/src/services/api.ts` | Add `force` parameter to `fetchSuggestions` |
| Modify | `frontend/src/components/MITREHeatmap.tsx` | "Regenerate" button passes `force: true` |

---

## Task 1: Add suggestion_cache table and store methods

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Step 1: Add suggestion_cache table to migrate()**

In `internal/store/store.go`, replace the `migrate` function:

```go
func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS client_alerts (
			client     TEXT        NOT NULL,
			alert_id   TEXT        NOT NULL,
			data       JSONB       NOT NULL,
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (client, alert_id)
		);
		CREATE TABLE IF NOT EXISTS sync_state (
			client      TEXT        PRIMARY KEY,
			last_synced TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS suggestion_cache (
			id           BIGSERIAL   PRIMARY KEY,
			cache_key    TEXT        NOT NULL,
			technique_id TEXT        NOT NULL,
			log_sources  TEXT[]      NOT NULL,
			suggestions  JSONB       NOT NULL,
			provider     TEXT        NOT NULL,
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS suggestion_cache_key_idx ON suggestion_cache(cache_key);
	`)
	return err
}
```

- [ ] **Step 2: Add SuggestionRow type and the two store methods**

Append to the end of `internal/store/store.go` (before the final newline):

```go
// SuggestionRow is one generation of LLM suggestions for a (technique, log_sources) pair.
// Suggestions is a raw JSON array — kept as json.RawMessage to avoid import cycles with the llm package.
type SuggestionRow struct {
	CacheKey    string
	TechniqueID string
	LogSources  []string
	Suggestions json.RawMessage // serialised []llm.Suggestion
	Provider    string
	GeneratedAt time.Time
}

// GetCachedSuggestions returns all suggestion rows for a cache key ordered ASC by generated_at.
// Returns an empty (non-nil) slice when no rows exist.
func (s *Store) GetCachedSuggestions(ctx context.Context, cacheKey string) ([]SuggestionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cache_key, technique_id, log_sources, suggestions, provider, generated_at
		FROM suggestion_cache
		WHERE cache_key = $1
		ORDER BY generated_at ASC
	`, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("query suggestion_cache: %w", err)
	}
	defer rows.Close()

	var result []SuggestionRow
	for rows.Next() {
		var row SuggestionRow
		var suggestions []byte
		if err := rows.Scan(
			&row.CacheKey, &row.TechniqueID, &row.LogSources,
			&suggestions, &row.Provider, &row.GeneratedAt,
		); err != nil {
			return nil, fmt.Errorf("scan suggestion_cache row: %w", err)
		}
		row.Suggestions = json.RawMessage(suggestions)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("suggestion_cache rows error: %w", err)
	}
	if result == nil {
		result = []SuggestionRow{}
	}
	return result, nil
}

// AppendCachedSuggestions inserts one new suggestion generation row.
// Existing rows are never modified — the table is append-only.
func (s *Store) AppendCachedSuggestions(ctx context.Context, row SuggestionRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO suggestion_cache (cache_key, technique_id, log_sources, suggestions, provider, generated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, row.CacheKey, row.TechniqueID, row.LogSources, string(row.Suggestions), row.Provider, row.GeneratedAt)
	if err != nil {
		return fmt.Errorf("insert suggestion_cache: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: clean build, no output.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/store/store.go
git commit -m "feat: add suggestion_cache table and store methods"
```

---

## Task 2: Integration tests for suggestion cache store methods

**Files:**
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Add three test functions**

Append to the end of `internal/store/store_test.go`:

```go
func TestGetCachedSuggestions_Empty(t *testing.T) {
	s := newStore(t)
	rows, err := s.GetCachedSuggestions(context.Background(), "nonexistent-cache-key")
	if err != nil {
		t.Fatalf("GetCachedSuggestions: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want 0 rows, got %d", len(rows))
	}
}

func TestAppendAndGetCachedSuggestions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "test-suggest-" + t.Name()

	now := time.Now().UTC().Truncate(time.Millisecond)
	sugsJSON := json.RawMessage(`[{"log_source":"firewall","alert_name":"FW Brute Force","description":"Brute force","query_hint":"event:login_failed","priority":"high"}]`)

	row := store.SuggestionRow{
		CacheKey:    key,
		TechniqueID: "T1021",
		LogSources:  []string{"firewall", "endpoint"},
		Suggestions: sugsJSON,
		Provider:    "nvidia",
		GeneratedAt: now,
	}
	if err := s.AppendCachedSuggestions(ctx, row); err != nil {
		t.Fatalf("AppendCachedSuggestions: %v", err)
	}

	got, err := s.GetCachedSuggestions(ctx, key)
	if err != nil {
		t.Fatalf("GetCachedSuggestions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].TechniqueID != "T1021" {
		t.Errorf("want TechniqueID T1021, got %s", got[0].TechniqueID)
	}
	if got[0].Provider != "nvidia" {
		t.Errorf("want Provider nvidia, got %s", got[0].Provider)
	}
	if !got[0].GeneratedAt.Equal(now) {
		t.Errorf("want GeneratedAt %v, got %v", now, got[0].GeneratedAt)
	}
	if string(got[0].Suggestions) != string(sugsJSON) {
		t.Errorf("suggestions JSON mismatch:\nwant %s\n got %s", sugsJSON, got[0].Suggestions)
	}
}

func TestAppendCachedSuggestions_AccumulatesRows(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "test-accumulate-" + t.Name()

	first := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	second := time.Now().UTC().Truncate(time.Millisecond)

	row1 := store.SuggestionRow{
		CacheKey: key, TechniqueID: "T1021",
		LogSources: []string{"firewall"}, Suggestions: json.RawMessage(`[]`),
		Provider: "nvidia", GeneratedAt: first,
	}
	row2 := store.SuggestionRow{
		CacheKey: key, TechniqueID: "T1021",
		LogSources: []string{"firewall"}, Suggestions: json.RawMessage(`[]`),
		Provider: "claude", GeneratedAt: second,
	}

	if err := s.AppendCachedSuggestions(ctx, row1); err != nil {
		t.Fatalf("AppendCachedSuggestions (1): %v", err)
	}
	if err := s.AppendCachedSuggestions(ctx, row2); err != nil {
		t.Fatalf("AppendCachedSuggestions (2): %v", err)
	}

	got, err := s.GetCachedSuggestions(ctx, key)
	if err != nil {
		t.Fatalf("GetCachedSuggestions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	// Rows ordered ASC by generated_at
	if !got[0].GeneratedAt.Equal(first) {
		t.Errorf("want first row GeneratedAt %v, got %v", first, got[0].GeneratedAt)
	}
	if !got[1].GeneratedAt.Equal(second) {
		t.Errorf("want second row GeneratedAt %v, got %v", second, got[1].GeneratedAt)
	}
	if got[0].Provider != "nvidia" {
		t.Errorf("want first row Provider nvidia, got %s", got[0].Provider)
	}
	if got[1].Provider != "claude" {
		t.Errorf("want second row Provider claude, got %s", got[1].Provider)
	}
}
```

You also need to add `"encoding/json"` to the imports in `store_test.go`. Replace the existing import block:

```go
import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/store"
)
```

- [ ] **Step 2: Run tests — verify they fail before implementation**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
NEON_DSN="postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require" \
  go test ./internal/store/... -run "TestGetCachedSuggestions|TestAppendAndGet|TestAppendCachedSuggestions_Accumulates" -v 2>&1 | head -20
```

Expected: compile succeeds (store methods were added in Task 1), all 3 tests **PASS** — they exercise real DB.

- [ ] **Step 3: Run all store tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
NEON_DSN="postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require" \
  go test ./internal/store/... -v
```

Expected: all 7 tests pass (`TestLoadAlerts_Empty`, `TestUpsertAndLoad`, `TestSyncState_NeverSynced`, `TestSyncState_SetAndGet`, plus the 3 new ones).

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/store/store_test.go
git commit -m "test: add integration tests for suggestion cache store methods"
```

---

## Task 3: Add Force field to SuggestionsRequest

**Files:**
- Modify: `internal/models/models.go`

- [ ] **Step 1: Add Force field**

In `internal/models/models.go`, replace the `SuggestionsRequest` struct:

```go
// SuggestionsRequest is the request body for per-technique alert suggestions.
type SuggestionsRequest struct {
	Client      string `json:"client"`
	Provider    string `json:"provider"`     // "claude" or "nvidia"; empty = default
	TechniqueID string `json:"technique_id"` // e.g. "T1059"
	Tactic      string `json:"tactic"`       // e.g. "execution"
	Force       bool   `json:"force"`        // if true, bypass cache and append a new generation
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/models/models.go
git commit -m "feat: add force field to SuggestionsRequest for cache bypass"
```

---

## Task 4: Cache-aware HandleSuggestions

**Files:**
- Modify: `internal/api/handlers.go`

- [ ] **Step 1: Update the import block**

Replace the existing import block in `internal/api/handlers.go`:

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
	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/merge"
	"coralogix-alert-analyzer/internal/mitre"
	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/similarity"
	"coralogix-alert-analyzer/internal/store"
)
```

- [ ] **Step 2: Add buildSuggestionCacheKey helper**

Add this function just before `fetchAlerts` at the bottom of `handlers.go`:

```go
// buildSuggestionCacheKey returns a stable SHA256 hex key for a (technique, log_sources) pair.
// Log sources are sorted before hashing so insertion order does not affect the key.
func buildSuggestionCacheKey(techniqueID string, logSources []string) string {
	sorted := make([]string, len(logSources))
	copy(sorted, logSources)
	sort.Strings(sorted)
	raw := techniqueID + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 3: Add mergeCachedSuggestions helper**

Add this function just after `buildSuggestionCacheKey`:

```go
// mergeCachedSuggestions flattens all suggestion rows into a single deduplicated list.
// rows must be ordered ASC by generated_at so that later rows win dedup conflicts.
// Returns the merged suggestions sorted by priority then alert_name, and the provider
// of the most recent row.
func mergeCachedSuggestions(rows []store.SuggestionRow) ([]models.AlertSuggestion, string) {
	type entry struct {
		sug     models.AlertSuggestion
		genAt   time.Time
	}
	seen := make(map[string]entry)
	var latestProvider string

	priorityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}

	for _, row := range rows {
		latestProvider = row.Provider
		var llmSugs []llm.Suggestion
		if err := json.Unmarshal(row.Suggestions, &llmSugs); err != nil {
			log.Printf("WARN [suggestions] unmarshal cached suggestions: %v", err)
			continue
		}
		for _, s := range llmSugs {
			key := strings.ToLower(s.AlertName)
			existing, exists := seen[key]
			if !exists || row.GeneratedAt.After(existing.genAt) {
				seen[key] = entry{
					sug: models.AlertSuggestion{
						LogSource:   s.LogSource,
						AlertName:   s.AlertName,
						Description: s.Description,
						QueryHint:   s.QueryHint,
						Priority:    s.Priority,
					},
					genAt: row.GeneratedAt,
				}
			}
		}
	}

	merged := make([]models.AlertSuggestion, 0, len(seen))
	for _, e := range seen {
		merged = append(merged, e.sug)
	}
	sort.Slice(merged, func(i, j int) bool {
		pi := priorityOrder[strings.ToLower(merged[i].Priority)]
		pj := priorityOrder[strings.ToLower(merged[j].Priority)]
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(merged[i].AlertName) < strings.ToLower(merged[j].AlertName)
	})
	return merged, latestProvider
}
```

- [ ] **Step 4: Replace the tail of HandleSuggestions (cache key + LLM call + response)**

In `HandleSuggestions`, find and replace from the `gapInput` block through to `writeJSON(w, http.StatusOK, resp)` at the end of the function.

Replace this block:

```go
	gapInput := llm.GapInput{
		LogSources: logSources,
		Technique: llm.TechniqueInput{
			ID:     req.TechniqueID,
			Name:   techniqueName,
			Tactic: tactic,
		},
	}

	result, err := llm.GenerateSuggestions(ctx, provider, gapInput)
	if err != nil {
		log.Printf("ERROR [suggestions] LLM call failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM suggestion failed: %v", err))
		return
	}

	// Map to response model
	suggestions := make([]models.AlertSuggestion, len(result.Suggestions))
	for i, s := range result.Suggestions {
		suggestions[i] = models.AlertSuggestion{
			LogSource:   s.LogSource,
			AlertName:   s.AlertName,
			Description: s.Description,
			QueryHint:   s.QueryHint,
			Priority:    s.Priority,
		}
	}

	resp := models.SuggestionsResponse{
		Provider:      result.Provider,
		TechniqueID:   req.TechniqueID,
		TechniqueName: techniqueName,
		Suggestions:   suggestions,
		LogSources:    logSources,
	}

	writeJSON(w, http.StatusOK, resp)
}
```

With:

```go
	gapInput := llm.GapInput{
		LogSources: logSources,
		Technique: llm.TechniqueInput{
			ID:     req.TechniqueID,
			Name:   techniqueName,
			Tactic: tactic,
		},
	}

	// Build cache key (requires logSources to be finalised above).
	var cacheKey string
	if h.alertStore != nil {
		cacheKey = buildSuggestionCacheKey(req.TechniqueID, logSources)
	}

	// Cache hit path — skip when force=true or store unavailable.
	if !req.Force && h.alertStore != nil {
		cached, err := h.alertStore.GetCachedSuggestions(ctx, cacheKey)
		if err != nil {
			log.Printf("WARN [suggestions] cache lookup client=%s technique=%s: %v", req.Client, req.TechniqueID, err)
		} else if len(cached) > 0 {
			merged, latestProvider := mergeCachedSuggestions(cached)
			log.Printf("INFO [suggestions] cache hit client=%s technique=%s rows=%d merged=%d",
				req.Client, req.TechniqueID, len(cached), len(merged))
			writeJSON(w, http.StatusOK, models.SuggestionsResponse{
				Provider:      latestProvider,
				TechniqueID:   req.TechniqueID,
				TechniqueName: techniqueName,
				Suggestions:   merged,
				LogSources:    logSources,
			})
			return
		}
	}

	// Cache miss or force — call the LLM.
	result, err := llm.GenerateSuggestions(ctx, provider, gapInput)
	if err != nil {
		log.Printf("ERROR [suggestions] LLM call failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM suggestion failed: %v", err))
		return
	}

	// Append to persistent cache.
	if h.alertStore != nil && cacheKey != "" {
		sugsJSON, _ := json.Marshal(result.Suggestions)
		appendErr := h.alertStore.AppendCachedSuggestions(ctx, store.SuggestionRow{
			CacheKey:    cacheKey,
			TechniqueID: req.TechniqueID,
			LogSources:  logSources,
			Suggestions: json.RawMessage(sugsJSON),
			Provider:    result.Provider,
			GeneratedAt: time.Now().UTC(),
		})
		if appendErr != nil {
			log.Printf("WARN [suggestions] cache append client=%s technique=%s: %v", req.Client, req.TechniqueID, appendErr)
		}
	}

	// For force requests, re-fetch the full pool and return the merged result.
	// For cache-miss requests, return the LLM result directly (single row, no merge needed).
	var (
		finalSuggestions []models.AlertSuggestion
		finalProvider    = result.Provider
	)

	if req.Force && h.alertStore != nil && cacheKey != "" {
		allRows, fetchErr := h.alertStore.GetCachedSuggestions(ctx, cacheKey)
		if fetchErr != nil {
			log.Printf("WARN [suggestions] force pool fetch client=%s technique=%s: %v", req.Client, req.TechniqueID, fetchErr)
		} else {
			finalSuggestions, finalProvider = mergeCachedSuggestions(allRows)
		}
	}

	if finalSuggestions == nil {
		finalSuggestions = make([]models.AlertSuggestion, len(result.Suggestions))
		for i, s := range result.Suggestions {
			finalSuggestions[i] = models.AlertSuggestion{
				LogSource:   s.LogSource,
				AlertName:   s.AlertName,
				Description: s.Description,
				QueryHint:   s.QueryHint,
				Priority:    s.Priority,
			}
		}
	}

	writeJSON(w, http.StatusOK, models.SuggestionsResponse{
		Provider:      finalProvider,
		TechniqueID:   req.TechniqueID,
		TechniqueName: techniqueName,
		Suggestions:   finalSuggestions,
		LogSources:    logSources,
	})
}
```

- [ ] **Step 5: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: clean build.

- [ ] **Step 6: Run all tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./...
```

Expected: all existing tests pass; store tests skip without NEON_DSN.

- [ ] **Step 7: Smoke-test the cache flow**

Start the server and call the suggestions endpoint twice for the same technique:

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
pkill -f "go run ./cmd/server" 2>/dev/null; sleep 1
go run ./cmd/server > /tmp/backend.log 2>&1 &
sleep 5

# First call — should hit LLM then cache-append
curl -s -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel","technique_id":"T1059","tactic":"execution"}' \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('provider:', d['provider'], '| count:', len(d['suggestions']))"

# Second call — should be a cache hit (no LLM call)
curl -s -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel","technique_id":"T1059","tactic":"execution"}' \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('provider:', d['provider'], '| count:', len(d['suggestions']))"

grep "\[suggestions\]" /tmp/backend.log | tail -10
```

Expected log output (second call shows "cache hit", not "requesting"):
```
INFO [suggestions] client=Deel technique=T1059 ... requesting nvidia for technique T1059 ...
INFO [suggestions] client=Deel technique=T1059 ... cache hit client=Deel technique=T1059 rows=1 merged=N
```

- [ ] **Step 8: Smoke-test force regenerate**

```bash
curl -s -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel","technique_id":"T1059","tactic":"execution","force":true}' \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('provider:', d['provider'], '| count:', len(d['suggestions']))"

grep "\[suggestions\]" /tmp/backend.log | tail -5
```

Expected: LLM is called again ("requesting"), a second row is appended, and the merged pool is returned.

- [ ] **Step 9: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/api/handlers.go
git commit -m "feat: add suggestion cache — NeonDB-backed pool with force regenerate"
```

---

## Task 5: Update frontend

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/MITREHeatmap.tsx`

- [ ] **Step 1: Add force parameter to fetchSuggestions in api.ts**

Replace the `fetchSuggestions` function in `frontend/src/services/api.ts`:

```typescript
export async function fetchSuggestions(
  client: string,
  techniqueId: string,
  tactic: string,
  provider?: string,
  force = false,
): Promise<SuggestionsResponse> {
  const res = await fetch(`${API_BASE}/api/suggestions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client,
      technique_id: techniqueId,
      tactic,
      provider: provider || '',
      force,
    }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Suggestions failed' }));
    throw new Error(err.error || 'Failed to generate suggestions');
  }
  return res.json();
}
```

- [ ] **Step 2: Add force parameter to handleGenerateAlerts in MITREHeatmap.tsx**

In `frontend/src/components/MITREHeatmap.tsx`, replace `handleGenerateAlerts`:

```tsx
  const handleGenerateAlerts = async (technique: NavigatorTechnique, force = false) => {
    setSuggestions(null);
    setSuggestionsError(null);
    setSuggestionsLoading(true);
    try {
      const result = await fetchSuggestions(clientName, technique.techniqueID, technique.tactic, selectedProvider || undefined, force);
      setSuggestions(result);
    } catch (err: unknown) {
      setSuggestionsError(err instanceof Error ? err.message : 'Failed to generate suggestions');
    } finally {
      setSuggestionsLoading(false);
    }
  };
```

- [ ] **Step 3: Add Regenerate button to the suggestions-header**

In `frontend/src/components/MITREHeatmap.tsx`, replace the `suggestions-header` div inside the `{suggestions && (` block:

```tsx
                    <div className="suggestions-header">
                      <span>{suggestions.suggestions.length} suggestion{suggestions.suggestions.length !== 1 ? 's' : ''}</span>
                      <span className="suggestions-provider">via {suggestions.provider}</span>
                      <button
                        className="btn btn-small"
                        onClick={() => handleGenerateAlerts(selectedTechnique, true)}
                        disabled={suggestionsLoading}
                      >
                        Regenerate
                      </button>
                    </div>
```

- [ ] **Step 4: Verify frontend builds**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
npm run build 2>&1 | tail -10
```

Expected: `✓ built in ...` with no TypeScript errors.

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/services/api.ts src/components/MITREHeatmap.tsx
git commit -m "feat: add Regenerate button for suggestion cache force-refresh"
```
