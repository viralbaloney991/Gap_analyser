# Suggestion Cache Design Spec

**Date:** 2026-04-01
**Feature:** Persistent suggestion pool — NeonDB-backed cache for LLM-generated alert suggestions

---

## Problem

`HandleSuggestions` calls the LLM on every request. If two clients have the same uncovered MITRE technique and identical log sources, the same expensive LLM call (~21s on Qwen) is made twice. Suggestions are deterministic by input — there is no reason to regenerate unless the user explicitly asks.

---

## Goal

Store LLM-generated suggestions in NeonDB, keyed by technique + log sources. On cache hit, return stored results immediately. Allow users to force a new generation, which appends to the pool rather than overwriting it. Over time, the pool accumulates multiple generations of suggestions that are served merged and deduplicated to all clients.

---

## Cache Key

```
cache_key = SHA256(technique_id + "|" + join(sort(log_sources), ","))
```

- Provider-agnostic: one pool per (technique, log_sources) combo regardless of which LLM generated it
- The stored `provider` field records which model produced each generation (informational only)

---

## Schema

```sql
CREATE TABLE IF NOT EXISTS suggestion_cache (
    id           BIGSERIAL PRIMARY KEY,
    cache_key    TEXT NOT NULL,
    technique_id TEXT NOT NULL,
    log_sources  TEXT[] NOT NULL,
    suggestions  JSONB NOT NULL,
    provider     TEXT NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS suggestion_cache_key_idx ON suggestion_cache(cache_key);
```

Multiple rows per `cache_key` are allowed — each represents one generation. Rows are never updated or deleted (append-only).

---

## Data Flow

### Normal request (no force)

1. Build `cache_key` from `technique_id` + sorted `log_sources`
2. Query all rows where `cache_key = $1` ordered by `generated_at ASC`
3. If any rows exist: flatten all `suggestions` arrays, deduplicate by `alert_name` (keep most recent on conflict), return merged result
4. If no rows: call `llm.GenerateSuggestions`, insert one row, return result

### Force regenerate (`force: true`)

1. Build `cache_key`
2. Call `llm.GenerateSuggestions` (skip cache lookup)
3. Insert new row (do not delete existing rows)
4. Fetch all rows for `cache_key`, merge + deduplicate as above, return full merged pool

### Third client (same key, pool already populated)

- Hits step 3 above — gets merged suggestions from all prior generations immediately, no LLM call

---

## API Change

`POST /api/suggestions` — one new optional field:

```json
{
  "client":       "Deel",
  "technique_id": "T1021.003",
  "log_sources":  ["firewall", "endpoint"],
  "provider":     "nvidia",
  "force":        false
}
```

`force` defaults to `false`. When `true`, bypasses cache lookup and appends a new generation.

Response shape is unchanged — `SuggestionsResult` with `Provider` and `[]Suggestion`.
`Provider` in the response reflects the provider of the most recent generation.

---

## Deduplication

When merging suggestions from multiple rows:
- Deduplicate by `alert_name` (case-insensitive)
- On conflict, keep the entry from the most recently generated row
- Order of merged output: sorted by `priority` (Critical → High → Medium → Low), then alphabetically by `alert_name`

---

## Frontend Change

- Suggestions panel shows cached results with a **"Regenerate"** button
- Clicking regenerate calls `POST /api/suggestions` with `force: true`
- Response replaces the panel content with the new merged pool
- No loading indicator change needed — same UX as initial generation

---

## New Store Methods

```go
// GetCachedSuggestions returns all suggestion rows for a cache key, ordered by generated_at ASC.
// Returns empty slice (not error) when no rows exist.
func (s *Store) GetCachedSuggestions(ctx context.Context, cacheKey string) ([]SuggestionRow, error)

// AppendCachedSuggestions inserts a new suggestion generation row.
func (s *Store) AppendCachedSuggestions(ctx context.Context, row SuggestionRow) error
```

```go
type SuggestionRow struct {
    CacheKey    string
    TechniqueID string
    LogSources  []string
    Suggestions []llm.Suggestion  // serialized as JSONB
    Provider    string
    GeneratedAt time.Time
}
```

---

## Files Changed

| File | Change |
|------|--------|
| `internal/store/store.go` | Add `suggestion_cache` table migration, `GetCachedSuggestions`, `AppendCachedSuggestions`, `SuggestionRow` type |
| `internal/store/store_test.go` | Integration tests for suggestion cache methods |
| `internal/api/handlers.go` | `HandleSuggestions`: add cache lookup + append; accept `force` field |
| `internal/models/suggestions.go` | Add `force` field to suggestions request model (or inline in handler) |
| `frontend/src/components/MITREHeatmap.tsx` | Add "Regenerate" button, pass `force: true` on click |

---

## Out of Scope

- Admin endpoint to delete cached suggestions (can add later)
- Per-client suggestion isolation (cache is intentionally cross-client)
- Automatic expiry / TTL (suggestions are permanent until regenerated)
