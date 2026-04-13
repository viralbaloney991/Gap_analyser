# Suggestion Pre-Warm Design

**Date:** 2026-04-13  
**Status:** Approved

## Problem

`minimaxai/minimax-m2.7` (NVIDIA NIM) takes ~92s per suggestion generation call. With 50+ uncovered MITRE techniques per client, a cold cache means every first click on a technique cell blocks the user for 92s. The NeonDB suggestion cache already handles repeat clicks instantly — the problem is exclusively the cold-cache first call.

## Goal

Eliminate the 92s wait for typical user sessions by pre-warming the NeonDB suggestion cache automatically after each `/api/analyze` call, using a rate-limited background worker that does not block the main request path.

## Out of Scope

- Streaming suggestions to the frontend
- A user-facing progress indicator for warmup status
- Pre-warming insights or MITRE classification caches
- Changing the suggestion LLM model

---

## Design

### Trigger

`HandleAnalyze` fires the pre-warm goroutine immediately after writing the response to the client. The user gets their analyze result in ~9s; pre-warming begins in the background concurrently.

```
POST /api/analyze
  → pipeline runs (~9s)
  → writeJSON(response)          ← user sees result
  → cancel previous prewarm job for this client (if any)
  → go worker.Start(ctx, ...)    ← background, non-blocking
```

This is the only trigger. There is no `/api/prewarm` endpoint and no scheduled worker involvement.

### Cancellation

`Handler` gains one new field:

```go
prewarmCancels sync.Map  // key: client string → value: context.CancelFunc
```

On each analyze call, the handler:
1. Looks up any existing cancel func for the client and calls it
2. Creates a new `context.WithCancel` derived from the server's root context
3. Stores the new cancel func in the map
4. Passes the new context to `worker.Start`

This ensures a re-analyze or refresh always starts a fresh pre-warm job, cancelling the stale one cleanly.

### Worker Internals

```
worker.Start(ctx, client, uncoveredTechniques, deps):

  1. Fetch log sources once:
       - Monday integrations (clientCfg.MondayGroupID)
       - Alert data sources (from stored/live alerts)
       - Merge + deduplicate, cap at 30

  2. For each uncovered technique in uncoveredTechniques:
       a. Compute cache key = buildSuggestionCacheKey(techniqueID, logSources)
       b. Check NeonDB: GetCachedSuggestions(ctx, cacheKey)
          → len > 0: log skip, continue
       c. Acquire semaphore slot (buffered chan, capacity 3)
       d. go func(technique):
            defer release slot
            result, err := llm.GenerateSuggestions(ctx, provider, gapInput)
            if err: log WARN [prewarm], continue
            AppendCachedSuggestions(ctx, cacheKey, result)
            log INFO [prewarm] client=X technique=Y warmed
       e. Check ctx.Done() between iterations — exit early on cancel
```

### Concurrency

A buffered channel of size 3 acts as a semaphore:

```go
const prewarmWorkers = 3

sem := make(chan struct{}, prewarmWorkers)
// acquire: sem <- struct{}{}
// release: <-sem
```

3 workers × 92s/technique = ~26 minutes to warm all 50+ techniques. The first 3 techniques are cached within ~92s, so users who explore the heatmap overview for a minute before drilling into techniques will typically hit warm cache.

### LLM Provider

Re-uses the same `SuggestionProvider`/`SuggestionModel` config as the live suggestions path (`minimaxai/minimax-m2.7` via NVIDIA NIM, using `NvidiaSuggestionAPIKey`). No new provider or config fields required.

### Error Handling

- Per-technique LLM errors: logged as `WARN [prewarm]`, worker continues to next technique
- Log source fetch error: logged, pre-warm aborts entirely (nothing to key on)
- Context cancellation: goroutines check `ctx.Done()` between technique iterations and exit cleanly
- NeonDB unavailable: pre-warm skips entirely (guarded at startup in `HandleAnalyze`)

### Observability

```
INFO [prewarm] client=Deel starting techniques=52 workers=3
INFO [prewarm] client=Deel technique=T1078 status=skipped (cached)
INFO [prewarm] client=Deel technique=T1059 status=warmed (3.2s)
WARN [prewarm] client=Deel technique=T1486 error=nvidia NIM API: context deadline exceeded
INFO [prewarm] client=Deel done warmed=48 skipped=3 errors=1 elapsed=25m14s
```

---

## Code Structure

### New file

**`internal/prewarm/worker.go`**

```go
type Worker struct {
    config     *config.Config
    alertStore *store.Store
    monday     *monday.Client
}

func New(cfg *config.Config, alertStore *store.Store, mondayClient *monday.Client) *Worker

func (w *Worker) Start(ctx context.Context, client string, clientCfg config.ClientConfig, techniques []models.MITRETechnique)
```

### Modified file

**`internal/api/handlers.go`**

- Add `prewarmCancels sync.Map` field to `Handler`
- Add `prewarmWorker *prewarm.Worker` field to `Handler`
- Initialise in `NewHandler`
- At end of `HandleAnalyze`: cancel previous, start new goroutine

**`cmd/server/main.go`**

- Pass `prewarm.New(cfg, neonStore, mondayClient)` into `api.NewHandler`

---

## Data Flow Summary

```
HandleAnalyze
  │
  ├─► writeJSON(response)  ←── user sees this immediately
  │
  └─► go prewarm.Start()
        │
        ├─ fetch log sources (Monday + alerts)
        │
        ├─ [for each uncovered technique]
        │     ├─ cache hit? → skip
        │     └─ acquire semaphore
        │           └─ GenerateSuggestions → AppendCachedSuggestions
        │
        └─ done (or ctx cancelled by next analyze)
```

---

## Acceptance Criteria

1. After `/api/analyze`, suggestions for all uncovered techniques are cached in NeonDB within ~26 minutes (3 workers × 92s/technique)
2. A re-analyze cancels and restarts the pre-warm job cleanly
3. Per-technique LLM failures do not crash or stall the worker
4. The existing `/api/suggestions` path is unchanged — it still checks NeonDB cache first
5. Pre-warm only runs when NeonDB is available (`alertStore != nil`)
