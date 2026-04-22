# Adaptive Parallel Pipeline Execution Design

**Date:** 2026-04-22
**Status:** Approved
**Topic:** Subagent-driven execution — adaptive worker pool with shared global semaphore

---

## Problem Statement

The current pipeline has three issues:

1. **Hardcoded concurrency** — `mitreWorkers=5` and `prewarmWorkers=3` are constants that do not scale with input size. A 10-alert run and a 500-alert run use the same worker count.
2. **Sequential stage fire-off** — MITRE mapping, suggestion pre-warming, and insights enrichment are never concurrent. Suggestions and insights are triggered by separate HTTP calls, not automatically after analysis completes.
3. **No rate-limit resilience** — On a paid NVIDIA API tier, the system has no mechanism to handle 429 responses gracefully or yield concurrency slots during backoff.

---

## Goals

- Scale worker concurrency proportionally to input volume at runtime
- Run suggestion pre-warming and insights enrichment concurrently after MITRE mapping completes (fire-and-forget)
- Enforce a single global cap on concurrent LLM calls across all stages
- Handle NVIDIA 429 responses with slot-yielding backoff
- Be forward-compatible with paid API tier (bump a config value, nothing else)

---

## Non-Goals

- DAG-based pipeline orchestrator (3 stages do not justify it)
- Multi-client fan-out (separate concern, separate design)
- Multi-provider consensus (separate concern, separate design)
- Changes to frontend or API contracts

---

## Architecture

### New Package: `internal/pipeline`

Two exported primitives.

#### `Semaphore`

A shared slot pool backed by `chan struct{}`. One instance is created at server startup from config and passed to every stage that makes LLM calls.

```go
type Semaphore struct {
    slots chan struct{}
    cap   int
}

func NewSemaphore(cap int) *Semaphore
func (s *Semaphore) Acquire(ctx context.Context) error  // blocks until slot free or ctx cancelled
func (s *Semaphore) Release()
func (s *Semaphore) Cap() int
```

#### `Run[T]`

Generic adaptive fan-out function. Computes worker count from input size and semaphore capacity, fans out work across goroutines, each of which acquires a semaphore slot before invoking the user function.

```go
func Run[T any](
    ctx    context.Context,
    sem    *Semaphore,
    inputs []T,
    batch  int,                           // target inputs per worker
    fn     func(context.Context, T) error,
) []error
```

Worker count formula:
```
workers = clamp(len(inputs) / batch, minWorkers=2, sem.Cap())
```

Each goroutine:
1. `sem.Acquire(ctx)` — blocks if global cap is reached
2. `fn(ctx, input)`
3. `sem.Release()`

Errors are collected per-item and returned. The batch never aborts on partial failure.

---

## Stage Ordering & Data Flow

```
POST /api/analyze
│
├─ Stage 1: MITRE Mapping  [synchronous — result required for response]
│   pipeline.Run(ctx, sem, uncachedAlerts, batchSize, classifyAndValidateSingle)
│   workers = clamp(len(uncachedAlerts)/batchSize, 2, globalCap)
│   → produces: map[alertID][]techniqueIDs
│
├─ Build analysis result (similarity, coverage gaps, stats)
│   → return HTTP 200 to client
│
└─ goroutine (detached context): Stage 2 + Stage 3 run concurrently
    ├─ Stage 2: Suggestion Pre-warm
    │   pipeline.Run(bgCtx, sem, uncoveredTechniques, batchSize, generateSuggestion)
    │   workers = clamp(len(uncoveredTechniques)/batchSize, 2, globalCap)
    │
    └─ Stage 3: Insights Enrichment  [single LLM call]
        sem.Acquire(bgCtx) → enrich.Enrich() → sem.Release()
```

**Key decisions:**
- Stage 1 is synchronous — the HTTP response depends on MITRE labels
- Stages 2 & 3 are fire-and-forget — launched after the response is sent, using `context.Background()` so client disconnect does not abort pre-warming
- Stages 2 & 3 share the global semaphore — combined in-flight LLM calls never exceed `globalCap`
- `/api/insights` and `/api/suggestions` endpoints remain unchanged — they hit the caches populated by background stages

---

## Error Handling

**Per-item failures (Stages 1 & 2):**
- `pipeline.Run` collects errors without aborting the batch
- Failed alerts → empty technique list (existing fallback behaviour preserved)
- Failed techniques → skipped in suggestion cache
- Logged as `WARN [pipeline] stage=<name> input=<id> error=<msg>`

**429 Backoff in `nvidia.go`:**
```
LLM call → 429 response
  → parse Retry-After header (default 5s if absent)
  → sem.Release()             ← yield slot so other workers proceed
  → sleep(retryAfter)
  → sem.Acquire(ctx)          ← reacquire before retry
  → retry (max 3 attempts, then return error)
```

Yielding the slot during sleep ensures a throttled worker does not block other in-flight workers.

**Non-429 errors (500, malformed JSON):** fail fast, log, do not retry.

**Stage 3 failure:** log `WARN [insights]`, leave Redis empty. Frontend polling retries on the next `/api/insights` call as today.

**Context cancellation:** `sem.Acquire` respects `ctx`. Stages 1 uses the request context; Stages 2 & 3 use a detached `context.Background()`-derived context with a generous timeout (e.g. 10 minutes).

---

## Configuration

Two new fields added to `LLMConfig` in `internal/config/config.go`:

```yaml
llm:
  pipeline_global_cap: 20    # max concurrent LLM calls across all stages (default: 20)
  pipeline_batch_size: 10    # target inputs per worker for adaptive sizing (default: 10)
```

Environment variable overrides: `PIPELINE_GLOBAL_CAP`, `PIPELINE_BATCH_SIZE`.

**Adaptive sizing examples (defaults):**

| Input count | Workers spawned |
|---|---|
| 5 | 2 (min floor) |
| 50 | 5 |
| 100 | 10 |
| 200 | 20 (capped) |
| 500 | 20 (capped) |

**Paid API upgrade path:** increase `pipeline_global_cap` in `clients.yaml` — no code changes required.

**Removed:** hardcoded constants `mitreWorkers=5` (`mitre_mapper.go`) and `prewarmWorkers=3` (`prewarm/worker.go`) are deleted.

---

## Files Changed

| File | Change |
|---|---|
| `internal/pipeline/semaphore.go` | New — `Semaphore` type |
| `internal/pipeline/run.go` | New — `Run[T]` generic fan-out |
| `internal/pipeline/run_test.go` | New — unit tests |
| `internal/config/config.go` | Add `PipelineGlobalCap`, `PipelineBatchSize` fields |
| `internal/llm/nvidia.go` | Add 429 backoff with slot-yield; accept `*pipeline.Semaphore` |
| `internal/llm/mitre_mapper.go` | Replace goroutine pool with `pipeline.Run`; delete `mitreWorkers` const |
| `internal/prewarm/worker.go` | Replace semaphore with `pipeline.Run`; delete `prewarmWorkers` const |
| `internal/insights/enrich.go` | Accept `*pipeline.Semaphore`; acquire/release around single LLM call |
| `cmd/server/main.go` | Create `*pipeline.Semaphore` at startup; pass to all stages |
| `internal/api/handlers.go` | Wire Stage 2+3 as concurrent goroutines after Stage 1 |

---

## Testing

### `internal/pipeline` (unit, no LLM calls)

- `TestSemaphore_AcquireRelease` — acquire to cap, verify block beyond cap, verify release unblocks
- `TestSemaphore_ContextCancellation` — cancelled ctx unblocks blocked Acquire
- `TestRun_AdaptiveWorkerCount` — verify worker count formula across small/medium/large inputs
- `TestRun_ErrorCollection` — partial failures collected, batch completes
- `TestRun_ConcurrencyBound` — atomic counter verifies in-flight count never exceeds globalCap

### `nvidia.go` backoff (fake HTTP server)

- `TestNvidia_429BackoffRespectsRetryAfter` — fake server returns 429+Retry-After, verify retry timing
- `TestNvidia_429MaxRetries` — always-429 server, verify 3-attempt limit
- `TestNvidia_ReleasesSlotDuringSleep` — verify semaphore slot released before sleep, reacquired after

### Existing tests

- `mitre_mapper_test.go` and `suggestions_test.go` updated to pass a `*pipeline.Semaphore` — no behaviour changes
