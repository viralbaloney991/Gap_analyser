# Correlation Alert Suggestions — Design Spec

## Goal

For each Advanced Use Cases gap item in the Gaps tab, provide on-demand LLM-generated suggestions covering two layers: cross-tactic correlation rules (linking this gap technique to already-covered techniques) and anomaly detection rules (behavioral baseline for the specific technique). Results are cached per client+gap in NeonDB and displayed in a slide-in drawer.

## Architecture

**Backend:** New `POST /api/correlations` endpoint. New `correlation_cache` NeonDB table (append-only, same pattern as `suggestion_cache`). New `GenerateCorrelations` LLM function.

**Frontend:** "Suggest correlations →" button on each Advanced Use Cases item. Slide-in drawer (fixed right, ~420px) shows results grouped into Correlation Rules and Anomaly Rules sections. State managed inline in `AlertInsights.tsx`.

**No changes to:** existing `/api/suggestions`, `suggestion_cache`, `EnrichActionable`, or the main insights pipeline.

---

## Backend

### New Types — `backend/internal/models/models.go`

```go
type CorrelationsRequest struct {
    Client             string   `json:"client"`
    GapProse           string   `json:"gap_prose"`
    LogSources         []string `json:"log_sources"`
    CoveredTechniques  []string `json:"covered_techniques"`
    Provider           string   `json:"provider"`  // empty = use configured default
    Force              bool     `json:"force"`
}

type CorrelationSuggestion struct {
    Type               string   `json:"type"`                // "correlation" | "anomaly"
    Title              string   `json:"title"`
    Description        string   `json:"description"`
    InvolvedTechniques []string `json:"involved_techniques"` // min 1 from covered_techniques for type=correlation
    QuerySkeleton      string   `json:"query_skeleton"`      // valid Lucene
    Priority           string   `json:"priority"`            // "critical"|"high"|"medium"|"low"
}

type CorrelationsResponse struct {
    Suggestions []CorrelationSuggestion `json:"suggestions"`
    Provider    string                  `json:"provider"`
    Cached      bool                    `json:"cached"`
}
```

### Cache Table — `backend/internal/store/store.go`

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

**Cache key:** `SHA256(client + "|" + strings.ToLower(strings.TrimSpace(gap_prose)))`

**Store methods:**
```go
func (s *Store) GetCachedCorrelations(ctx context.Context, cacheKey string) ([]CorrelationRow, error)
func (s *Store) AppendCachedCorrelations(ctx context.Context, row CorrelationRow) error

type CorrelationRow struct {
    CacheKey    string
    Client      string
    Suggestions json.RawMessage
    Provider    string
    GeneratedAt time.Time
}
```

Merge strategy (same as `suggestion_cache`): deduplicate by lowercase title, latest `generated_at` wins; sort by priority then title.

### LLM Function — `backend/internal/llm/correlations.go`

```go
func GenerateCorrelations(
    ctx context.Context,
    provider Provider,
    gapProse string,
    logSources []string,
    coveredTechniques []string,
) ([]models.CorrelationSuggestion, error)
```

**Prompt contract:**
- Input context: gap description, client's covered techniques, available log sources
- Output: max 5 suggestions total (up to 3 `type=correlation`, up to 2 `type=anomaly`)
- Constraints:
  - `type=correlation`: `involved_techniques` must include at least one technique from `covered_techniques`
  - `query_skeleton`: valid Lucene syntax
  - `priority`: one of `critical|high|medium|low`
  - No fabricated log sources — use only those in `log_sources` list

### Handler — `backend/internal/api/handlers.go`

```go
func (h *Handler) HandleCorrelations(w http.ResponseWriter, r *http.Request)
```

Flow:
1. Decode `CorrelationsRequest`; validate `client` non-empty, `gap_prose` non-empty
2. Resolve LLM provider (default to configured default if `provider` field absent)
3. Build cache key: `SHA256(client|normalised_gap_prose)`
4. If `!force` and store available: call `GetCachedCorrelations`; if rows found, merge and return with `cached: true`
5. Cache miss or force: call `llm.GenerateCorrelations`; on success, call `AppendCachedCorrelations`
6. Return `CorrelationsResponse`

Log lines:
- `INFO HandleCorrelations client=%s cache=hit suggestions=%d`
- `INFO HandleCorrelations client=%s cache=miss provider=%s suggestions=%d`
- `WARN HandleCorrelations client=%s llm error: %v` (return 500)

### Route Registration — `backend/cmd/server/main.go`

```go
mux.HandleFunc("/api/correlations", handler.HandleCorrelations)
```

---

## Frontend

### New Types — `frontend/src/types/index.ts`

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

### API Function — `frontend/src/services/api.ts`

```typescript
export async function fetchCorrelations(
  client: string,
  gapProse: string,
  logSources: string[],
  coveredTechniques: string[],
  force = false,
): Promise<CorrelationsResponse>
```

Calls `POST /api/correlations`.

### State — `AlertInsights.tsx`

```typescript
const [correlationDrawer, setCorrelationDrawer] = useState<{
  gapProse: string;
  suggestions: CorrelationSuggestion[];
  loading: boolean;
  cached: boolean;
} | null>(null);
```

`coveredTechniques` derived inline from `mitreCoverage.technique_coverage`:
```typescript
const coveredTechniques = useMemo(
  () => Object.entries(mitreCoverage.technique_coverage ?? {})
    .filter(([, v]) => v.alert_count > 0)
    .map(([k]) => k),
  [mitreCoverage]
);
```

`logSources` for a given gap item: use `rec.log_source` from the `ActionableRecommendation`; fallback to empty array if unavailable.

### UI Changes — `AlertInsights.tsx`

**Button** added to each Advanced Use Cases `ActionableRecommendation` item (alongside existing query skeleton toggle):

```tsx
<button
  type="button"
  className="corr-suggest-btn"
  onClick={() => openCorrelationDrawer(rec)}
>
  Suggest correlations →
</button>
```

**Drawer** rendered at the bottom of the Gaps tab section (outside the scrollable list, fixed to viewport right):

```tsx
{correlationDrawer && (
  <div className="corr-drawer">
    <div className="corr-drawer-header">
      <span className="corr-drawer-title">{correlationDrawer.gapProse}</span>
      {correlationDrawer.cached && <span className="corr-cached-badge">Cached</span>}
      <button type="button" onClick={() => setCorrelationDrawer(null)}>✕</button>
    </div>
    <div className="corr-drawer-body">
      {correlationDrawer.loading ? <CorrelationSkeleton /> : (
        <>
          <CorrelationSection
            title="Correlation Rules"
            items={correlationDrawer.suggestions.filter(s => s.type === 'correlation')}
          />
          <CorrelationSection
            title="Anomaly Rules"
            items={correlationDrawer.suggestions.filter(s => s.type === 'anomaly')}
          />
        </>
      )}
    </div>
  </div>
)}
```

Drawer CSS in `App.css`: slides in from right with `transform: translateX(100%)` → `translateX(0)` transition; `z-index: 50` over main content; semi-transparent backdrop.

Each suggestion card shows: priority badge (using existing `severityColors`), title, description, expandable query skeleton with copy button (same pattern as existing query skeleton in `renderActionableSection`).

---

## Data Flow

```
User clicks "Suggest correlations →" on an Advanced Use Cases item
  → openCorrelationDrawer(rec) sets drawer state (loading: true)
  → fetchCorrelations(client, rec.prose, [rec.log_source], coveredTechniques)
      → POST /api/correlations
          → cache hit? return cached rows (merged)
          → cache miss? call llm.GenerateCorrelations → append row → return
  → setCorrelationDrawer({ suggestions, loading: false, cached })
  → Drawer renders Correlation Rules + Anomaly Rules sections
```

---

## Out of Scope

- Bulk "generate for all" button across all advanced use cases at once
- Exporting correlation suggestions to PDF/navigator layer
- Per-suggestion feedback or rating
- Auto-generation on page load
