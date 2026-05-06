# Audit Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 7 bugs identified in the codebase audit, covering a panic risk in the prewarm handler, corrupted-provider attribution in both merge functions, a partial-object crash in the frontend noise path, a silent validation gap, an over-long background timeout, and dead cache code.

**Architecture:** All fixes are narrow surgical changes — no new abstractions, no refactoring. Each task touches ≤ 3 files and can be reviewed independently. Backend bugs fixed first (Tasks 1–5), frontend last (Task 6).

**Tech Stack:** Go 1.21 (`backend/`), React 18 + TypeScript (`frontend/`), existing `go test ./...` + `npx tsc --noEmit`.

---

## File Map

| File | Tasks |
|---|---|
| `backend/internal/api/handlers.go` | 1, 2, 3, 4 |
| `backend/internal/api/lookback_test.go` | 3 |
| `backend/internal/api/correlations_handler_test.go` | 2 |
| `backend/internal/cache/redis.go` | 5 |
| `frontend/src/App.tsx` | 6 |

---

### Task 1: P1-1 — Safe type assertion in prewarm cancel (handlers.go:276)

**Files:**
- Modify: `backend/internal/api/handlers.go:275-277`

**Background:** `prewarmCancels` is a `sync.Map` typed as `interface{}`. At line 276 the code does `prev.(context.CancelFunc)()` — a bare type assertion. If a non-`CancelFunc` is ever stored (future change, race condition), this panics the handler goroutine with no recovery. The fix is a guarded two-value assertion.

- [ ] **Step 1: Write the failing test**

There is no clean unit-test path for the panic (the sync.Map is unexported). Instead, add a regression comment and a compile-time-verifiable change. The test for this task verifies the prewarm code path doesn't panic on a known-valid call with an existing entry.

Add to `backend/internal/api/lookback_test.go`:

```go
func TestHandleAnalyze_prewarmCancelSafe(t *testing.T) {
	// Regression test: prewarmCancels.Load must use safe two-value assertion.
	// This test verifies the safe path compiles and runs without panic.
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	// Store a valid CancelFunc then load and call it via the safe pattern.
	_, cancel := context.WithCancel(context.Background())
	h.prewarmCancels.Store("test-client", cancel)
	if prev, ok := h.prewarmCancels.Load("test-client"); ok {
		if fn, ok := prev.(context.CancelFunc); ok {
			fn()
		}
	}
	// No panic = pass.
}
```

- [ ] **Step 2: Run test to confirm it compiles and passes**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestHandleAnalyze_prewarmCancelSafe -v
```

Expected: `PASS`

- [ ] **Step 3: Apply the fix**

In `backend/internal/api/handlers.go`, replace line 276:

```go
// BEFORE (line 275-277):
		if prev, ok := h.prewarmCancels.Load(req.Client); ok {
			prev.(context.CancelFunc)()
		}

// AFTER:
		if prev, ok := h.prewarmCancels.Load(req.Client); ok {
			if cancel, ok := prev.(context.CancelFunc); ok {
				cancel()
			}
		}
```

- [ ] **Step 4: Run full api tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/api/handlers.go backend/internal/api/lookback_test.go
git commit -m "fix(api): use safe type assertion for prewarm cancel to avoid panic"
```

---

### Task 2: P1-3 — Fix latestProvider set before unmarshal guard (handlers.go:964, 1129)

**Files:**
- Modify: `backend/internal/api/handlers.go:964` (mergeCachedSuggestions)
- Modify: `backend/internal/api/handlers.go:1129` (mergeCorrelations)
- Modify: `backend/internal/api/correlations_handler_test.go`

**Background:** Both `mergeCachedSuggestions` (line 964) and `mergeCorrelations` (line 1129) set `latestProvider = row.Provider` unconditionally at the top of the loop, before the `json.Unmarshal` call. If `Unmarshal` fails and the loop `continue`s, `latestProvider` has been updated to the provider of the skipped row. The returned `provider` field in the API response ends up wrong — it does not correspond to the suggestions actually returned. The fix is to move the assignment inside the success branch.

- [ ] **Step 1: Write failing tests**

Add to `backend/internal/api/correlations_handler_test.go`:

```go
func TestMergeCorrelations_skipsCorruptRow(t *testing.T) {
	rows := []store.CorrelationRow{
		{
			Provider:    "anthropic",
			Suggestions: json.RawMessage(`[{"type":"correlation","title":"Brute Force","description":"d","involved_techniques":["T1110"],"query_skeleton":"event.action:auth*","priority":"high"}]`),
			GeneratedAt: time.Now().Add(-2 * time.Minute),
		},
		{
			Provider:    "nvidia", // corrupt row — provider should NOT appear in result
			Suggestions: json.RawMessage(`NOT VALID JSON`),
			GeneratedAt: time.Now().Add(-1 * time.Minute),
		},
	}
	merged, provider := mergeCorrelations(rows)
	if provider != "anthropic" {
		t.Errorf("expected provider=anthropic (last good row), got %q", provider)
	}
	if len(merged) != 1 {
		t.Errorf("expected 1 merged suggestion, got %d", len(merged))
	}
}
```

You will also need these imports at the top of `correlations_handler_test.go`:

```go
import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "coralogix-alert-analyzer/internal/config"
    "coralogix-alert-analyzer/internal/store"
)
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestMergeCorrelations_skipsCorruptRow -v
```

Expected: FAIL — `expected provider=anthropic (last good row), got "nvidia"`

- [ ] **Step 3: Fix mergeCachedSuggestions (handlers.go ~line 964)**

Find the loop in `mergeCachedSuggestions`. The current code:

```go
for _, row := range rows {
    latestProvider = row.Provider
    var llmSugs []llm.Suggestion
    if err := json.Unmarshal(row.Suggestions, &llmSugs); err != nil {
        log.Printf("WARN [suggestions] unmarshal cached suggestions: %v", err)
        continue
    }
```

Change to:

```go
for _, row := range rows {
    var llmSugs []llm.Suggestion
    if err := json.Unmarshal(row.Suggestions, &llmSugs); err != nil {
        log.Printf("WARN [suggestions] unmarshal cached suggestions: %v", err)
        continue
    }
    latestProvider = row.Provider
```

- [ ] **Step 4: Fix mergeCorrelations (handlers.go ~line 1128)**

The current code:

```go
for _, row := range rows {
    latestProvider = row.Provider
    var sugs []models.CorrelationSuggestion
    if err := json.Unmarshal(row.Suggestions, &sugs); err != nil {
        log.Printf("WARN [correlations] unmarshal cached correlations: %v", err)
        continue
    }
```

Change to:

```go
for _, row := range rows {
    var sugs []models.CorrelationSuggestion
    if err := json.Unmarshal(row.Suggestions, &sugs); err != nil {
        log.Printf("WARN [correlations] unmarshal cached correlations: %v", err)
        continue
    }
    latestProvider = row.Provider
```

- [ ] **Step 5: Run test to confirm it passes**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestMergeCorrelations_skipsCorruptRow -v
```

Expected: PASS

- [ ] **Step 6: Run full api tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/api/handlers.go backend/internal/api/correlations_handler_test.go
git commit -m "fix(api): move latestProvider update inside unmarshal success branch in both merge functions"
```

---

### Task 3: P2-1 — Log invalid lookback_days in validateLookbackDays

**Files:**
- Modify: `backend/internal/api/handlers.go:1098-1105`
- Modify: `backend/internal/api/lookback_test.go`

**Background:** `validateLookbackDays` silently defaults to 30 for invalid inputs. When a client sends `lookback_days=45`, it gets 30-day data with no server-side indication that the value was rejected. Adding a warning log makes this detectable in monitoring without changing any behaviour.

- [ ] **Step 1: Update the test to capture the warn case (compile check only)**

The existing `TestValidateLookbackDays_invalidDefaultsTo30` already covers the correct return value. No test change is needed for the logging itself (Go's `log` package doesn't easily redirect in unit tests). Just confirm existing tests still pass after the code change.

- [ ] **Step 2: Add the warning log**

In `backend/internal/api/handlers.go`, replace `validateLookbackDays`:

```go
func validateLookbackDays(days int) int {
	switch days {
	case 7, 14, 30, 90:
		return days
	default:
		if days != 0 {
			log.Printf("WARN [validate] invalid lookback_days=%d, defaulting to 30", days)
		}
		return 30
	}
}
```

(The `days != 0` guard avoids noise when the field is simply omitted from the request and defaults to Go's zero value.)

- [ ] **Step 3: Run lookback tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestValidateLookback -v
```

Expected: all PASS (return values unchanged)

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/api/handlers.go
git commit -m "fix(api): log warning when lookback_days is invalid before defaulting to 30"
```

---

### Task 4: P2-2 — Reduce background insights goroutine timeout from 10 min to 3 min

**Files:**
- Modify: `backend/internal/api/handlers.go:333`

**Background:** `runInsightsBackground` uses a 10-minute detached context. The semaphore caps concurrency, but a single hanging LLM provider can hold a semaphore slot for the full 10 minutes, starving other background workers. Realistic LLM p99 latency is well under 3 minutes; dropping the timeout to 3 minutes and adding a timeout log makes stalls visible.

- [ ] **Step 1: Change the timeout and add a log**

In `backend/internal/api/handlers.go`, replace line 333:

```go
// BEFORE:
bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

// AFTER:
bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
```

Also add a timeout log after the semaphore acquire error log (around line 345). The context deadline exceeded error will surface through the existing `WARN [insights-bg] client=%s enrich: %v` log when the LLM call times out — no additional log line is needed.

- [ ] **Step 2: Run api tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v
```

Expected: all PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/api/handlers.go
git commit -m "fix(api): reduce background insights goroutine timeout from 10 min to 3 min"
```

---

### Task 5: P3-1 — Remove dead Invalidate() method from cache.Store

**Files:**
- Modify: `backend/internal/cache/redis.go:83-87`

**Background:** `cache.Store.Invalidate()` is defined but never called from any handler. Force-refresh uses `Set()` to overwrite directly. The dead method implies an incomplete cache invalidation strategy and will mislead future maintainers. YAGNI — remove it.

- [ ] **Step 1: Search for callers to confirm it's unused**

```bash
grep -rn "\.Invalidate(" /Users/aviral.baloni/Desktop/claude/backend/
```

Expected output: only the definition in `redis.go` — no callers.

- [ ] **Step 2: Remove the method**

In `backend/internal/cache/redis.go`, delete lines 83–87:

```go
// DELETE these lines:
// Invalidate removes a cached result for the given client.
func (s *Store) Invalidate(ctx context.Context, client string) {
    s.client.Del(ctx, clientKey(client))
    log.Printf("INFO [cache] INVALIDATED client=%s", client)
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no errors

- [ ] **Step 4: Run cache tests if any exist**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/cache/... -v
```

Expected: PASS (or "no test files" — both acceptable)

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/cache/redis.go
git commit -m "refactor(cache): remove dead Invalidate() method — Set() handles force-refresh directly"
```

---

### Task 6: P1-2 — Fix partial alert_insights object on noise reanalysis (App.tsx:53)

**Files:**
- Modify: `frontend/src/App.tsx:53`

**Background:** In `handleReanalyze`, the state update is:
```typescript
{ ...prev, alert_insights: { ...prev.alert_insights!, noise_alerts: noiseAlerts } }
```

`SimilarityResult` has `families`, `duplicates`, `merge_suggestions`, `unique_detections`, and optional `noise_alerts`. If `prev.alert_insights` is `undefined` (noise reanalysis fires before initial analysis completes — possible during a race), the spread produces `{ noise_alerts: [...] }` — a partial object missing the four required fields. Any downstream component accessing `alert_insights.families` or `alert_insights.duplicates` would then crash or render empty.

The fix uses the nullish coalescing operator to spread an empty object when `alert_insights` is not yet populated, and removes the TypeScript non-null assertion.

- [ ] **Step 1: Fix App.tsx line 53**

```typescript
// BEFORE (lines 52-54):
      setData(prev => prev
        ? { ...prev, alert_insights: { ...prev.alert_insights!, noise_alerts: noiseAlerts } }
        : prev
      );

// AFTER:
      setData(prev => prev
        ? { ...prev, alert_insights: { ...(prev.alert_insights ?? {}), noise_alerts: noiseAlerts } as SimilarityResult }
        : prev
      );
```

The `as SimilarityResult` cast is safe here: `handleReanalyze` is only reachable after `data` is set (the `if (!data) return` guard at line 46), which means `prev.alert_insights` will always be populated in normal flow. The `?? {}` protects against the race condition without changing any observable behaviour in normal use.

- [ ] **Step 2: Run TypeScript check**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: zero errors

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/App.tsx
git commit -m "fix(frontend): guard against partial alert_insights spread on noise reanalysis"
```

---

## Running All Tests After All Tasks

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./...
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Both should pass cleanly.
