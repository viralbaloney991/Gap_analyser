# Adaptive Parallel Pipeline Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hardcoded goroutine pools with an adaptive `pipeline.Run[T]` fan-out backed by a shared global semaphore, and fire suggestion pre-warming + insights enrichment concurrently after `/api/analyze` responds.

**Architecture:** A new `internal/pipeline` package provides `Semaphore` (global LLM call cap) and `Run[T]` (adaptive fan-out that scales workers with input volume). `nvidia.go` gains a 3-attempt retry loop for 429 responses. `mitre_mapper.go` and `prewarm/worker.go` replace their bespoke pools with `pipeline.Run`. `HandleAnalyze` launches pre-warm and insights as concurrent goroutines sharing the same semaphore.

**Tech Stack:** Go 1.25 generics, `sync`, `net/http/httptest` (for 429 fake server), existing `internal/llm`, `internal/insights`, `internal/prewarm` packages.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/pipeline/semaphore.go` | Create | `Semaphore` type — channel-backed slot pool |
| `internal/pipeline/run.go` | Create | `Run[T]` — generic adaptive fan-out |
| `internal/pipeline/pipeline_test.go` | Create | Unit tests for both primitives |
| `internal/config/config.go` | Modify | Add `PipelineGlobalCap`, `PipelineBatchSize` fields + env overrides |
| `internal/llm/nvidia.go` | Modify | Add 429 retry loop (max 3 attempts, Retry-After sleep) |
| `internal/llm/nvidia_retry_test.go` | Create | Fake HTTP server tests for 429 backoff |
| `internal/llm/mitre_mapper.go` | Modify | Replace goroutine pool with `pipeline.Run`; delete `mitreWorkers` |
| `internal/prewarm/worker.go` | Modify | Replace semaphore with `pipeline.Run`; delete `prewarmWorkers` |
| `internal/api/handlers.go` | Modify | Add `sem` to `Handler`; fire insights goroutine after response |
| `cmd/server/main.go` | Modify | Create `*pipeline.Semaphore` at startup; pass to `NewHandler` |

---

## Task 1: Add pipeline config fields

**Files:**
- Modify: `backend/internal/config/config.go`

- [ ] **Step 1: Add fields to `LLMConfig`**

In `internal/config/config.go`, add two fields to the `LLMConfig` struct after the `InsightsModel` field (line 56):

```go
// PipelineGlobalCap is the max number of concurrent LLM calls across all pipeline stages.
// Default: 20. Increase for paid API tiers.
PipelineGlobalCap int `yaml:"pipeline_global_cap"`
// PipelineBatchSize is the target number of inputs per worker for adaptive sizing.
// workers = clamp(len(inputs)/PipelineBatchSize, 2, PipelineGlobalCap)
// Default: 10.
PipelineBatchSize int `yaml:"pipeline_batch_size"`
```

- [ ] **Step 2: Add env var overrides and defaults in `Load()`**

In the `Load()` function in `internal/config/config.go`, after the existing env var block (after line 107 `cfg.LLM.NvidiaSuggestionAPIKey = v`), add:

```go
if v := os.Getenv("PIPELINE_GLOBAL_CAP"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 {
        cfg.LLM.PipelineGlobalCap = n
    }
}
if v := os.Getenv("PIPELINE_BATCH_SIZE"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 {
        cfg.LLM.PipelineBatchSize = n
    }
}
```

Also add defaults after the `cfg.LLM.DefaultProvider` default block (after line 113):

```go
if cfg.LLM.PipelineGlobalCap <= 0 {
    cfg.LLM.PipelineGlobalCap = 20
}
if cfg.LLM.PipelineBatchSize <= 0 {
    cfg.LLM.PipelineBatchSize = 10
}
```

Add `"strconv"` to the import block.

- [ ] **Step 3: Verify the server still compiles**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/config/config.go
git commit -m "feat(config): add PipelineGlobalCap and PipelineBatchSize fields"
```

---

## Task 2: Create `pipeline.Semaphore` with TDD

**Files:**
- Create: `backend/internal/pipeline/semaphore.go`
- Create: `backend/internal/pipeline/pipeline_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/pipeline/pipeline_test.go`:

```go
package pipeline_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/pipeline"
)

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := pipeline.NewSemaphore(2)

	// Can acquire up to cap.
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	// Third acquire should block; release one slot and verify it unblocks.
	released := make(chan struct{})
	acquired := make(chan struct{})
	go func() {
		<-released
		sem.Release()
	}()
	go func() {
		if err := sem.Acquire(context.Background()); err != nil {
			t.Errorf("third acquire after release: %v", err)
		}
		close(acquired)
	}()

	// Give the goroutine time to block on Acquire.
	time.Sleep(20 * time.Millisecond)
	close(released) // triggers Release

	select {
	case <-acquired:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("third acquire did not unblock after Release")
	}
}

func TestSemaphore_ContextCancellation(t *testing.T) {
	sem := pipeline.NewSemaphore(1)
	// Drain the one slot.
	_ = sem.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- sem.Acquire(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled ctx, got nil")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Acquire did not return after ctx cancellation")
	}
}

func TestSemaphore_Cap(t *testing.T) {
	sem := pipeline.NewSemaphore(7)
	if sem.Cap() != 7 {
		t.Fatalf("Cap() = %d, want 7", sem.Cap())
	}
}

func TestRun_AdaptiveWorkerCount(t *testing.T) {
	cases := []struct {
		inputs  int
		batch   int
		cap     int
		wantMin int
		wantMax int
	}{
		{inputs: 5, batch: 10, cap: 20, wantMin: 2, wantMax: 2},   // floor: 5/10=0 → min 2
		{inputs: 50, batch: 10, cap: 20, wantMin: 5, wantMax: 5},  // 50/10 = 5
		{inputs: 200, batch: 10, cap: 20, wantMin: 20, wantMax: 20}, // cap: 200/10=20
		{inputs: 500, batch: 10, cap: 20, wantMin: 20, wantMax: 20}, // cap: 500/10=50 → capped at 20
	}
	for _, tc := range cases {
		sem := pipeline.NewSemaphore(tc.cap)
		var maxInFlight atomic.Int64
		var current atomic.Int64

		inputs := make([]int, tc.inputs)
		errs := pipeline.Run(context.Background(), sem, inputs, tc.batch, func(_ context.Context, _ int) error {
			cur := current.Add(1)
			if cur > maxInFlight.Load() {
				maxInFlight.Store(cur)
			}
			time.Sleep(5 * time.Millisecond)
			current.Add(-1)
			return nil
		})
		if len(errs) > 0 {
			t.Errorf("inputs=%d: unexpected errors: %v", tc.inputs, errs)
		}
		got := maxInFlight.Load()
		if got < int64(tc.wantMin) || got > int64(tc.wantMax) {
			t.Errorf("inputs=%d batch=%d cap=%d: max in-flight = %d, want [%d, %d]",
				tc.inputs, tc.batch, tc.cap, got, tc.wantMin, tc.wantMax)
		}
	}
}

func TestRun_ErrorCollection(t *testing.T) {
	sem := pipeline.NewSemaphore(5)
	inputs := []int{1, 2, 3, 4, 5}

	errs := pipeline.Run(context.Background(), sem, inputs, 1, func(_ context.Context, n int) error {
		if n%2 == 0 {
			return fmt.Errorf("even: %d", n)
		}
		return nil
	})
	// 2 and 4 fail → 2 errors
	if len(errs) != 2 {
		t.Fatalf("want 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestRun_ConcurrencyBound(t *testing.T) {
	const cap = 4
	sem := pipeline.NewSemaphore(cap)

	var inFlight atomic.Int64
	inputs := make([]int, 40)

	errs := pipeline.Run(context.Background(), sem, inputs, 2, func(_ context.Context, _ int) error {
		cur := inFlight.Add(1)
		if cur > cap {
			t.Errorf("in-flight %d exceeds cap %d", cur, cap)
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	})
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestRun_EmptyInputs(t *testing.T) {
	sem := pipeline.NewSemaphore(5)
	errs := pipeline.Run(context.Background(), sem, []int{}, 10, func(_ context.Context, _ int) error {
		t.Fatal("fn should not be called for empty inputs")
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("want no errors for empty inputs, got %v", errs)
	}
}
```

Add `"fmt"` to the import block in the test file.

- [ ] **Step 2: Run the tests — confirm they fail**

```bash
cd backend && go test ./internal/pipeline/... -v 2>&1 | head -20
```

Expected: `cannot find package "coralogix-alert-analyzer/internal/pipeline"` or compile error.

- [ ] **Step 3: Implement `Semaphore`**

Create `backend/internal/pipeline/semaphore.go`:

```go
package pipeline

import (
	"context"
	"fmt"
)

// Semaphore is a counting semaphore backed by a buffered channel.
// It enforces a global cap on concurrent LLM calls across all pipeline stages.
// One instance should be created at server startup and shared across all stages.
type Semaphore struct {
	slots chan struct{}
	cap   int
}

// NewSemaphore creates a Semaphore with the given capacity. Panics if cap <= 0.
func NewSemaphore(cap int) *Semaphore {
	if cap <= 0 {
		panic(fmt.Sprintf("pipeline: semaphore cap must be > 0, got %d", cap))
	}
	s := &Semaphore{
		slots: make(chan struct{}, cap),
		cap:   cap,
	}
	for i := 0; i < cap; i++ {
		s.slots <- struct{}{}
	}
	return s
}

// Acquire blocks until a slot is available or ctx is cancelled.
// Returns ctx.Err() if the context is cancelled before a slot is obtained.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case <-s.slots:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot to the semaphore. Must be called once per successful Acquire.
func (s *Semaphore) Release() {
	s.slots <- struct{}{}
}

// Cap returns the total capacity of the semaphore.
func (s *Semaphore) Cap() int {
	return s.cap
}
```

- [ ] **Step 4: Implement `Run[T]`**

Create `backend/internal/pipeline/run.go`:

```go
package pipeline

import (
	"context"
	"sync"
)

const minWorkers = 2

// Run fans out fn over inputs using an adaptive worker count derived from input size.
//
// Worker count formula: clamp(len(inputs)/batch, minWorkers=2, sem.Cap())
//
// Each worker acquires a semaphore slot before calling fn and releases it after.
// Errors are collected per-item; the batch never aborts on partial failure.
// Returns nil if inputs is empty.
func Run[T any](ctx context.Context, sem *Semaphore, inputs []T, batch int, fn func(context.Context, T) error) []error {
	if len(inputs) == 0 {
		return nil
	}
	if batch <= 0 {
		batch = 1
	}

	workers := len(inputs) / batch
	if workers < minWorkers {
		workers = minWorkers
	}
	if workers > sem.Cap() {
		workers = sem.Cap()
	}

	jobs := make(chan T, len(inputs))
	for _, inp := range inputs {
		jobs <- inp
	}
	close(jobs)

	var mu sync.Mutex
	var errs []error

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for inp := range jobs {
				if err := sem.Acquire(ctx); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					continue
				}
				err := fn(ctx, inp)
				sem.Release()
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return errs
}
```

- [ ] **Step 5: Run the tests — confirm they pass**

```bash
cd backend && go test ./internal/pipeline/... -v
```

Expected: all 6 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/pipeline/
git commit -m "feat(pipeline): add Semaphore and Run[T] adaptive fan-out"
```

---

## Task 3: Add 429 retry to `nvidia.go`

**Files:**
- Modify: `backend/internal/llm/nvidia.go`
- Create: `backend/internal/llm/nvidia_retry_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/llm/nvidia_retry_test.go`:

```go
package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNvidia_429BackoffRespectsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p := &nvidiaProvider{apiKey: "test", model: "test-model", endpoint: srv.URL}
	start := time.Now()
	result, err := p.Complete(context.Background(), CompletionRequest{
		UserMessage: "hello",
		FastMode:    true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("want 'ok', got %q", result)
	}
	if calls.Load() != 3 {
		t.Fatalf("want 3 calls, got %d", calls.Load())
	}
	// 2 retries × 1s Retry-After = at least 2s total
	if elapsed < 2*time.Second {
		t.Fatalf("expected at least 2s elapsed (2 retry sleeps), got %s", elapsed)
	}
}

func TestNvidia_429MaxRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &nvidiaProvider{apiKey: "test", model: "test-model", endpoint: srv.URL}
	_, err := p.Complete(context.Background(), CompletionRequest{
		UserMessage: "hello",
		FastMode:    true,
	})
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
}

func TestNvidia_NonRetryableErrorFastFails(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	p := &nvidiaProvider{apiKey: "test", model: "test-model", endpoint: srv.URL}
	_, err := p.Complete(context.Background(), CompletionRequest{
		UserMessage: "hello",
		FastMode:    true,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls.Load() != 1 {
		t.Fatalf("want 1 call (no retry for 500), got %d", calls.Load())
	}
}
```

- [ ] **Step 2: Run the tests — confirm they fail**

```bash
cd backend && go test ./internal/llm/ -run TestNvidia_ -v 2>&1 | head -30
```

Expected: `TestNvidia_429BackoffRespectsRetryAfter` and `TestNvidia_429MaxRetries` FAIL (no retry logic exists).

- [ ] **Step 3: Add retry constants and a `doWithRetry` helper to `nvidia.go`**

At the top of `backend/internal/llm/nvidia.go`, after the import block, add:

```go
const (
	nvidiaMaxRetries       = 3
	nvidiaDefaultRetryWait = 5 * time.Second
)
```

Add the following helper function after the `completeStreaming` function (at the end of the file):

```go
// parseRetryAfter reads the Retry-After header and returns the wait duration.
// Defaults to nvidiaDefaultRetryWait if the header is absent or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return nvidiaDefaultRetryWait
}
```

Add `"strconv"` to the import block in `nvidia.go`.

- [ ] **Step 4: Wrap `completeNonStreaming` with retry in `Complete()`**

Replace the body of the `Complete()` function in `nvidia.go` (lines 29–45) with:

```go
func (n *nvidiaProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.UserMessage})

	var lastErr error
	for attempt := 0; attempt < nvidiaMaxRetries; attempt++ {
		var text string
		var err error
		if req.FastMode {
			text, err = n.completeNonStreaming(ctx, messages, maxTokens)
		} else {
			text, err = n.completeStreaming(ctx, messages, maxTokens)
		}
		if err == nil {
			return text, nil
		}
		lastErr = err
		// Only retry on rate-limit errors; fail fast on everything else.
		re, ok := err.(*rateLimitError)
		if !ok {
			return "", err
		}
		log.Printf("WARN [nvidia] rate limited (attempt %d/%d), retrying in %s", attempt+1, nvidiaMaxRetries, re.wait)
		select {
		case <-time.After(re.wait):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("nvidia NIM: max retries exceeded: %w", lastErr)
}

// rateLimitError is returned by completeNonStreaming/completeStreaming on HTTP 429.
type rateLimitError struct {
	wait time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.wait)
}
```

- [ ] **Step 5: Return `*rateLimitError` from `completeNonStreaming` on 429**

In `completeNonStreaming`, replace the current non-200 error block (lines 74–76):

```go
// OLD:
if resp.StatusCode != 200 {
    return "", fmt.Errorf("nvidia NIM API returned %d: %s", resp.StatusCode, string(respBody))
}
```

With:

```go
if resp.StatusCode == http.StatusTooManyRequests {
    return "", &rateLimitError{wait: parseRetryAfter(resp.Header)}
}
if resp.StatusCode != 200 {
    return "", fmt.Errorf("nvidia NIM API returned %d: %s", resp.StatusCode, string(respBody))
}
```

- [ ] **Step 6: Return `*rateLimitError` from `completeStreaming` on 429**

In `completeStreaming`, replace the non-200 error block (lines 124–127):

```go
// OLD:
if resp.StatusCode != 200 {
    respBody, _ := io.ReadAll(resp.Body)
    return "", fmt.Errorf("nvidia NIM API returned %d: %s", resp.StatusCode, string(respBody))
}
```

With:

```go
if resp.StatusCode == http.StatusTooManyRequests {
    return "", &rateLimitError{wait: parseRetryAfter(resp.Header)}
}
if resp.StatusCode != 200 {
    respBody, _ := io.ReadAll(resp.Body)
    return "", fmt.Errorf("nvidia NIM API returned %d: %s", resp.StatusCode, string(respBody))
}
```

- [ ] **Step 7: Run the tests — confirm they pass**

```bash
cd backend && go test ./internal/llm/ -run TestNvidia_ -v
```

Expected: all 3 PASS. Note: `TestNvidia_429BackoffRespectsRetryAfter` will take ~2s due to the sleep.

- [ ] **Step 8: Verify the rest of the llm package still compiles and passes**

```bash
cd backend && go test ./internal/llm/... -v
```

Expected: all existing tests pass.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/llm/nvidia.go backend/internal/llm/nvidia_retry_test.go
git commit -m "feat(llm): add 429 retry with Retry-After backoff to nvidia provider"
```

---

## Task 4: Refactor `mitre_mapper.go` to use `pipeline.Run`

**Files:**
- Modify: `backend/internal/llm/mitre_mapper.go`

- [ ] **Step 1: Update `BatchClassifyAndValidate` signature to accept `*pipeline.Semaphore`**

Replace the full `BatchClassifyAndValidate` function in `backend/internal/llm/mitre_mapper.go` with:

```go
// BatchClassifyAndValidate runs the two-stage MITRE mapping pipeline:
// 1. Classifier sidecar → top-3 semantic candidates per alert
// 2. Llama validator → confirmed technique IDs
// Results are cached per-alert in Redis for 7 days.
// Falls back gracefully: if sidecar is down, candidates are empty and validator is skipped.
func BatchClassifyAndValidate(
	ctx context.Context,
	classifierClient ClassifierClientIface,
	validatorProvider Provider,
	store MITRECacheStore,
	sem *pipeline.Semaphore,
	inputs []AlertInput,
) map[string][]string {
	result := make(map[string][]string, len(inputs))
	var mu sync.Mutex

	// Check cache first.
	var uncached []AlertInput
	for _, inp := range inputs {
		key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
		if val, ok := store.GetString(ctx, key); ok {
			var techs []string
			if err := json.Unmarshal([]byte(val), &techs); err == nil {
				mu.Lock()
				result[inp.ID] = techs
				mu.Unlock()
				continue
			}
		}
		uncached = append(uncached, inp)
	}

	log.Printf("INFO [classifier] total=%d cached=%d to_map=%d", len(inputs), len(inputs)-len(uncached), len(uncached))

	if len(uncached) == 0 {
		return result
	}

	pipeline.Run(ctx, sem, uncached, 1, func(ctx context.Context, inp AlertInput) error {
		key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
		techs := classifyAndValidateSingle(ctx, classifierClient, validatorProvider, inp)
		if data, err := json.Marshal(techs); err == nil {
			store.SetString(ctx, key, string(data), mitreCacheTTL)
		}
		mu.Lock()
		result[inp.ID] = techs
		mu.Unlock()
		return nil
	})

	return result
}
```

- [ ] **Step 2: Delete `mitreWorkers` constant and remove `sync` from direct use in this function**

Remove the `mitreWorkers = 5` line from the `const` block (the block will still have `mitreCacheTTL` and `mitreCachePrefix`).

The `sync` import is still needed for `sync.Mutex` used above — keep it.

- [ ] **Step 3: Add `pipeline` import**

Add to the import block in `mitre_mapper.go`:

```go
"coralogix-alert-analyzer/internal/pipeline"
```

- [ ] **Step 4: Fix the call site in `handlers.go`**

In `backend/internal/api/handlers.go`, find the call to `llm.BatchClassifyAndValidate` (around line 189) and add `h.sem` as the new parameter:

```go
// OLD:
llmMappings = llm.BatchClassifyAndValidate(ctx, classifierClient, validatorProvider, h.cache, inputs)

// NEW:
llmMappings = llm.BatchClassifyAndValidate(ctx, classifierClient, validatorProvider, h.cache, h.sem, inputs)
```

(The `h.sem` field will be added in Task 6 — for now add it here; the compile will fail until Task 6.)

- [ ] **Step 5: Verify the package compiles (ignore handler error for now)**

```bash
cd backend && go build ./internal/llm/... 2>&1
```

Expected: `internal/llm` compiles cleanly. `internal/api` will fail until Task 6.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/llm/mitre_mapper.go backend/internal/api/handlers.go
git commit -m "feat(mitre): replace goroutine pool with pipeline.Run adaptive fan-out"
```

---

## Task 5: Refactor `prewarm/worker.go` to use `pipeline.Run`

**Files:**
- Modify: `backend/internal/prewarm/worker.go`

- [ ] **Step 1: Add `sem *pipeline.Semaphore` to the `Worker` struct**

In `backend/internal/prewarm/worker.go`, update the `Worker` struct:

```go
type Worker struct {
	config     *config.Config
	alertStore *store.Store
	monday     *monday.Client
	sem        *pipeline.Semaphore
}
```

- [ ] **Step 2: Update `New()` to accept and store the semaphore**

```go
func New(cfg *config.Config, alertStore *store.Store, mondayClient *monday.Client, sem *pipeline.Semaphore) *Worker {
	return &Worker{
		config:     cfg,
		alertStore: alertStore,
		monday:     mondayClient,
		sem:        sem,
	}
}
```

- [ ] **Step 3: Replace the semaphore + goroutine pool in `Start()` with `pipeline.Run`**

Replace the entire section from `sem := make(chan struct{}, prewarmWorkers)` through `wg.Wait()` (lines 148–224) with:

```go
type prewarmJob struct {
	tech     mitre.UncoveredTechnique
	cacheKey string
}

var jobs []prewarmJob
for _, tech := range techniques {
	select {
	case <-ctx.Done():
		log.Printf("INFO [prewarm] client=%s cancelled before processing", client)
		return
	default:
	}
	key := buildCacheKey(tech.ID, logSources)
	rows, err := w.alertStore.GetCachedSuggestions(ctx, key)
	if err == nil && len(rows) > 0 {
		skipped++
		log.Printf("INFO [prewarm] client=%s technique=%s status=skipped", client, tech.ID)
		continue
	}
	jobs = append(jobs, prewarmJob{tech: tech, cacheKey: key})
}

var mu sync.Mutex
pipeline.Run(ctx, w.sem, jobs, 1, func(ctx context.Context, j prewarmJob) error {
	result, err := llm.GenerateSuggestions(ctx, provider, llm.GapInput{
		LogSources: logSources,
		Technique: llm.TechniqueInput{
			ID:     j.tech.ID,
			Name:   j.tech.Name,
			Tactic: j.tech.Tactic,
		},
	})
	if err != nil {
		mu.Lock()
		errors++
		mu.Unlock()
		log.Printf("WARN [prewarm] client=%s technique=%s error=%v", client, j.tech.ID, err)
		return err
	}
	sugJSON, err := json.Marshal(result.Suggestions)
	if err != nil {
		log.Printf("WARN [prewarm] client=%s technique=%s marshal error=%v", client, j.tech.ID, err)
		return err
	}
	row := store.SuggestionRow{
		CacheKey:    j.cacheKey,
		TechniqueID: j.tech.ID,
		LogSources:  logSources,
		Suggestions: sugJSON,
		Provider:    result.Provider,
		GeneratedAt: time.Now(),
	}
	if err := w.alertStore.AppendCachedSuggestions(ctx, row); err != nil {
		log.Printf("WARN [prewarm] client=%s technique=%s store error=%v", client, j.tech.ID, err)
		return err
	}
	mu.Lock()
	warmed++
	mu.Unlock()
	log.Printf("INFO [prewarm] client=%s technique=%s status=warmed", client, j.tech.ID)
	return nil
})
```

Also declare `warmed, skipped, errors := 0, 0, 0` and `var mu sync.Mutex` before the jobs loop.

- [ ] **Step 4: Delete the `prewarmWorkers` constant**

Remove `prewarmWorkers = 3` from the `const` block.

- [ ] **Step 5: Add `pipeline` import**

Add to the import block:

```go
"coralogix-alert-analyzer/internal/pipeline"
```

- [ ] **Step 6: Verify the prewarm package compiles**

```bash
cd backend && go build ./internal/prewarm/... 2>&1
```

Expected: compiles cleanly (ignore api package errors for now).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/prewarm/worker.go
git commit -m "feat(prewarm): replace semaphore pool with pipeline.Run adaptive fan-out"
```

---

## Task 6: Wire `*pipeline.Semaphore` into `Handler` and `main.go`

**Files:**
- Modify: `backend/internal/api/handlers.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add `sem` field to `Handler` struct in `handlers.go`**

In `backend/internal/api/handlers.go`, update the `Handler` struct:

```go
type Handler struct {
	config         *config.Config
	mondayClient   *monday.Client
	cache          *cache.Store
	alertStore     *store.Store
	prewarmWorker  *prewarm.Worker
	prewarmCancels sync.Map
	sem            *pipeline.Semaphore
}
```

- [ ] **Step 2: Update `NewHandler` to accept and store the semaphore**

```go
func NewHandler(cfg *config.Config, redisStore *cache.Store, alertStore *store.Store, prewarmWorker *prewarm.Worker, sem *pipeline.Semaphore) *Handler {
	return &Handler{
		config:        cfg,
		mondayClient:  monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:         redisStore,
		alertStore:    alertStore,
		prewarmWorker: prewarmWorker,
		sem:           sem,
	}
}
```

- [ ] **Step 3: Add `pipeline` import to `handlers.go`**

Add to the import block:

```go
"coralogix-alert-analyzer/internal/pipeline"
```

- [ ] **Step 4: Create semaphore and update call sites in `main.go`**

In `backend/cmd/server/main.go`, after loading config (after `log.Printf("loaded config: %d clients", len(cfg.Clients))`), add:

```go
// Pipeline semaphore — global cap on concurrent LLM calls across all stages.
sem := pipeline.NewSemaphore(cfg.LLM.PipelineGlobalCap)
log.Printf("INFO [pipeline] semaphore cap=%d batch=%d", cfg.LLM.PipelineGlobalCap, cfg.LLM.PipelineBatchSize)
```

Add import: `"coralogix-alert-analyzer/internal/pipeline"`

Update the `alertprewarm.New` call to pass the semaphore:

```go
// OLD:
prewarmWorker = alertprewarm.New(cfg, neonStore, monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID))

// NEW:
prewarmWorker = alertprewarm.New(cfg, neonStore, monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID), sem)
```

Update the `api.NewHandler` call to pass the semaphore:

```go
// OLD:
handler := api.NewHandler(cfg, redisStore, neonStore, prewarmWorker)

// NEW:
handler := api.NewHandler(cfg, redisStore, neonStore, prewarmWorker, sem)
```

- [ ] **Step 5: Verify the full project compiles**

```bash
cd backend && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 6: Run all tests**

```bash
cd backend && go test ./... 2>&1
```

Expected: all tests pass (some may be skipped if they require external services).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/handlers.go backend/cmd/server/main.go
git commit -m "feat(server): wire pipeline.Semaphore into Handler and main startup"
```

---

## Task 7: Fire insights enrichment concurrently in `HandleAnalyze`

**Files:**
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Add a helper method `runInsightsBackground` to `handlers.go`**

Add this method to the `Handler` type (can go after `HandleHealth`):

```go
// runInsightsBackground runs LLM insights enrichment as a fire-and-forget goroutine.
// Uses a detached context so that client disconnect does not abort the work.
// Results are stored in Redis for /api/insights to serve.
func (h *Handler) runInsightsBackground(client string, clientCfg config.ClientConfig, alerts []*models.AlertDef) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	go func() {
		defer cancel()

		alertInsights := similarity.Analyze(alerts)

		insightsProviderName := h.config.LLM.InsightsProvider
		if insightsProviderName == "" {
			insightsProviderName = h.config.LLM.SuggestionProvider
		}
		insightsModel := h.config.LLM.InsightsModel
		if insightsModel == "" {
			insightsModel = h.config.LLM.NvidiaModel
		}
		insightsProvider, err := llm.NewClassifierProvider(
			insightsProviderName, "",
			llm.ProviderConfig{
				AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
				ClaudeModel:     h.config.LLM.ClaudeModel,
				NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
				NvidiaModel:     insightsModel,
				NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
				GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
				GeminiModel:     h.config.LLM.GeminiModel,
			},
		)
		if err != nil {
			log.Printf("WARN [insights-bg] client=%s provider init failed: %v", client, err)
			return
		}

		// Acquire one semaphore slot for the single insights LLM call.
		if err := h.sem.Acquire(bgCtx); err != nil {
			log.Printf("WARN [insights-bg] client=%s semaphore acquire: %v", client, err)
			return
		}
		ir, enrichErr := insights.Enrich(bgCtx, alertInsights, alerts, insightsProvider)
		h.sem.Release()

		if enrichErr != nil {
			log.Printf("WARN [insights-bg] client=%s enrich: %v", client, enrichErr)
			return
		}
		if ir == nil {
			return
		}

		if h.cache != nil {
			if key, err := computeInsightsCacheKey(client, alertInsights); err == nil {
				if data, marshalErr := json.Marshal(ir); marshalErr == nil {
					h.cache.SetString(bgCtx, key, string(data), 24*time.Hour)
					log.Printf("INFO [insights-bg] client=%s cached insights", client)
				}
			}
		}
	}()
}
```

- [ ] **Step 2: Call `runInsightsBackground` from `HandleAnalyze` after writing the response**

In `HandleAnalyze`, after the existing prewarm goroutine block (after line 268), add:

```go
// Trigger background insights enrichment concurrently with pre-warm.
// Uses a detached context; semaphore ensures it doesn't crowd out pre-warm workers.
if h.sem != nil {
	h.runInsightsBackground(req.Client, clientCfg, alerts)
}
```

- [ ] **Step 3: Verify the project still compiles**

```bash
cd backend && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./... 2>&1
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers.go
git commit -m "feat(analyze): fire insights enrichment concurrently with pre-warm after response"
```

---

## Task 8: End-to-end smoke test

**Files:** none modified — manual verification

- [ ] **Step 1: Start the server locally**

```bash
cd backend && CONFIG_PATH=clients.yaml go run ./cmd/server/
```

Expected log lines at startup:
```
loaded config: N clients
INFO [pipeline] semaphore cap=20 batch=10
server starting on :8080
```

- [ ] **Step 2: Trigger an analysis and verify concurrent background stages start**

```bash
curl -s -X POST http://localhost:8080/api/analyze \
  -H 'Content-Type: application/json' \
  -d '{"client":"<your-client>"}' | jq .stats
```

Expected in server logs (in approximate order):
```
INFO [analyze] MITRE pipeline: N/M alerts need classification
INFO [classifier] total=N cached=X to_map=Y
INFO [analyze] ... (response sent)
INFO [prewarm] client=... starting techniques=Z workers=...
INFO [insights-bg] client=... cached insights   ← NEW concurrent line
```

- [ ] **Step 3: Verify adaptive worker count scales with input volume**

Check that the prewarm log shows different worker counts for clients with different numbers of uncovered techniques:
- Small client (< 10 techniques): workers = 2
- Medium client (50 techniques): workers = 5
- Large client (200+ techniques): workers = 20

- [ ] **Step 4: Final commit**

```bash
git add .
git commit -m "chore: verify adaptive pipeline smoke test complete"
```

---

## Self-Review Checklist

- [x] All 10 files in the spec's file map are covered by tasks
- [x] `mitreWorkers=5` deleted in Task 4; `prewarmWorkers=3` deleted in Task 5
- [x] `PipelineGlobalCap` / `PipelineBatchSize` env vars added in Task 1
- [x] 429 retry covers both `completeNonStreaming` and `completeStreaming` in Task 3
- [x] Semaphore passed through: `main.go` → `NewHandler` → `prewarm.New` → `BatchClassifyAndValidate`
- [x] Insights goroutine uses `context.Background()` detached context (10-min timeout)
- [x] No placeholder steps — all code blocks are complete
- [x] Type consistency: `pipeline.Semaphore`, `pipeline.Run`, `*pipeline.Semaphore` used consistently across all tasks
- [x] `pipeline.Run` batch param is `1` in mitre_mapper (1 alert per worker slot) and prewarm (1 technique per slot); the `PipelineBatchSize` config drives worker count in the formula but the job granularity is per-item
