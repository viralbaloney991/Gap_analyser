# Suggestion Pre-Warm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically pre-warm the NeonDB suggestion cache for all uncovered MITRE techniques after each `/api/analyze` call, using a 3-worker background goroutine so users never wait 92s on first click.

**Architecture:** `HandleAnalyze` spawns a goroutine after writing its response. A new `internal/prewarm` package owns the worker logic: it fetches log sources once, checks NeonDB for each uncovered technique, and calls `llm.GenerateSuggestions` only on cache misses via a 3-slot semaphore. A `sync.Map` on `Handler` stores per-client cancel funcs so re-analyze restarts the job cleanly.

**Tech Stack:** Go, `sync.Map`, buffered-channel semaphore, `coralogix-alert-analyzer/internal/{llm,store,mitre,monday,coralogix,config}`

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| **Create** | `internal/prewarm/worker.go` | `Worker` struct + `Start` method — all pre-warm logic |
| **Create** | `internal/prewarm/worker_test.go` | Unit tests for worker (semaphore, skip cached, cancel) |
| **Modify** | `internal/api/handlers.go` | Add `prewarmCancels sync.Map`, `prewarmWorker *prewarm.Worker`; trigger after `writeJSON` in `HandleAnalyze` |
| **Modify** | `cmd/server/main.go` | Pass `prewarm.New(...)` into `api.NewHandler` |

---

## Task 1: Create `internal/prewarm/worker.go` skeleton

**Files:**
- Create: `backend/internal/prewarm/worker.go`

- [ ] **Step 1: Create the file with the Worker struct and New constructor**

```go
package prewarm

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/mitre"
	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/store"
)

const (
	prewarmWorkers = 3
	maxLogSources  = 30
)

// Worker pre-warms the NeonDB suggestion cache for uncovered MITRE techniques.
// It runs in the background after each /api/analyze call.
type Worker struct {
	config     *config.Config
	alertStore *store.Store
	monday     *monday.Client
}

// New creates a Worker. alertStore must be non-nil before calling Start.
func New(cfg *config.Config, alertStore *store.Store, mondayClient *monday.Client) *Worker {
	return &Worker{
		config:     cfg,
		alertStore: alertStore,
		monday:     mondayClient,
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd backend && go build ./internal/prewarm/
```
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/prewarm/worker.go
git commit -m "feat(prewarm): add Worker skeleton"
```

---

## Task 2: Implement log source fetching helper

**Files:**
- Modify: `backend/internal/prewarm/worker.go`

- [ ] **Step 1: Add `fetchLogSources` to `worker.go`**

Add this function after the `New` constructor:

```go
// fetchLogSources returns a deduplicated, capped list of log sources for a client.
// Monday integrations take priority; alert data sources supplement them.
func (w *Worker) fetchLogSources(ctx context.Context, client string, clientCfg config.ClientConfig) ([]string, error) {
	var integrations []monday.Integration
	var alerts []*models.AlertDef

	var wg sync.WaitGroup
	var mondayErr, alertsErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		integrations, mondayErr = w.monday.FetchIntegrations(ctx, clientCfg.MondayGroupID)
	}()
	go func() {
		defer wg.Done()
		if w.alertStore != nil {
			stored, err := w.alertStore.LoadAlerts(ctx, client)
			if err == nil && len(stored) > 0 {
				alerts = stored
				return
			}
		}
		// No live fetch here — prewarm uses cached data only to avoid duplicate API calls.
	}()
	wg.Wait()

	if mondayErr != nil {
		log.Printf("WARN [prewarm] monday fetch client=%s: %v", client, mondayErr)
	}
	if alertsErr != nil {
		return nil, fmt.Errorf("fetch alerts: %w", alertsErr)
	}

	seen := make(map[string]bool)
	var logSources []string

	for _, integ := range integrations {
		if integ.Name != "" && !seen[integ.Name] {
			seen[integ.Name] = true
			logSources = append(logSources, integ.Name)
		}
	}
	coralogix.ExtractFeatures(alerts, nil)
	for _, alert := range alerts {
		for _, ds := range alert.Features.DataSources {
			if !seen[ds] {
				seen[ds] = true
				logSources = append(logSources, ds)
			}
		}
	}
	if len(logSources) > maxLogSources {
		logSources = logSources[:maxLogSources]
	}
	return logSources, nil
}
```

Also add `"fmt"` to the import block.

- [ ] **Step 2: Compile**

```bash
cd backend && go build ./internal/prewarm/
```
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/prewarm/worker.go
git commit -m "feat(prewarm): add fetchLogSources helper"
```

---

## Task 3: Implement `buildCacheKey` and `Start` method

**Files:**
- Modify: `backend/internal/prewarm/worker.go`

- [ ] **Step 1: Add `buildCacheKey` helper**

Add after `fetchLogSources`:

```go
// buildCacheKey returns the same stable SHA256 key used by HandleSuggestions.
func buildCacheKey(techniqueID string, logSources []string) string {
	sorted := make([]string, len(logSources))
	copy(sorted, logSources)
	sort.Strings(sorted)
	raw := techniqueID + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
```

Add to imports: `"crypto/sha256"` and `"encoding/hex"`.

- [ ] **Step 2: Add the `Start` method**

```go
// Start pre-warms the suggestion cache for all uncovered techniques for the given client.
// It runs until all techniques are processed or ctx is cancelled.
// Intended to be called as a goroutine.
func (w *Worker) Start(ctx context.Context, client string, clientCfg config.ClientConfig, techniques []mitre.UncoveredTechnique) {
	log.Printf("INFO [prewarm] client=%s starting techniques=%d workers=%d", client, len(techniques), prewarmWorkers)
	start := time.Now()

	logSources, err := w.fetchLogSources(ctx, client, clientCfg)
	if err != nil {
		log.Printf("WARN [prewarm] client=%s log source fetch failed: %v — aborting", client, err)
		return
	}
	if len(logSources) == 0 {
		log.Printf("WARN [prewarm] client=%s no log sources found — aborting", client)
		return
	}

	// Build suggestion LLM provider (same config as HandleSuggestions).
	nvidiaKey := w.config.LLM.NvidiaAPIKey
	if w.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = w.config.LLM.NvidiaSuggestionAPIKey
	}
	provider, err := llm.NewClassifierProvider(w.config.LLM.SuggestionProvider, w.config.LLM.SuggestionModel, llm.ProviderConfig{
		AnthropicAPIKey: w.config.LLM.AnthropicAPIKey,
		ClaudeModel:     w.config.LLM.ClaudeModel,
		NvidiaAPIKey:    nvidiaKey,
		NvidiaModel:     w.config.LLM.NvidiaModel,
		NvidiaEndpoint:  w.config.LLM.NvidiaEndpoint,
		GeminiAPIKey:    w.config.LLM.GeminiAPIKey,
		GeminiModel:     w.config.LLM.GeminiModel,
	})
	if err != nil {
		log.Printf("WARN [prewarm] client=%s provider init failed: %v — aborting", client, err)
		return
	}

	sem := make(chan struct{}, prewarmWorkers)
	var wg sync.WaitGroup
	warmed, skipped, errors := 0, 0, 0
	var mu sync.Mutex

	for _, tech := range techniques {
		// Exit early on cancellation.
		select {
		case <-ctx.Done():
			log.Printf("INFO [prewarm] client=%s cancelled after %d techniques", client, warmed+skipped)
			wg.Wait()
			return
		default:
		}

		cacheKey := buildCacheKey(tech.ID, logSources)

		// Skip if already cached.
		rows, err := w.alertStore.GetCachedSuggestions(ctx, cacheKey)
		if err == nil && len(rows) > 0 {
			mu.Lock()
			skipped++
			mu.Unlock()
			log.Printf("INFO [prewarm] client=%s technique=%s status=skipped", client, tech.ID)
			continue
		}

		// Acquire semaphore slot.
		sem <- struct{}{}
		wg.Add(1)

		go func(t mitre.UncoveredTechnique, key string) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := llm.GenerateSuggestions(ctx, provider, llm.GapInput{
				LogSources: logSources,
				Technique: llm.TechniqueInput{
					ID:     t.ID,
					Name:   t.Name,
					Tactic: t.Tactic,
				},
			})
			if err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				log.Printf("WARN [prewarm] client=%s technique=%s error=%v", client, t.ID, err)
				return
			}

			sugJSON, err := json.Marshal(result.Suggestions)
			if err != nil {
				log.Printf("WARN [prewarm] client=%s technique=%s marshal error=%v", client, t.ID, err)
				return
			}

			row := store.SuggestionRow{
				CacheKey:    key,
				TechniqueID: t.ID,
				LogSources:  logSources,
				Suggestions: sugJSON,
				Provider:    result.Provider,
				GeneratedAt: time.Now(),
			}
			if err := w.alertStore.AppendCachedSuggestions(ctx, row); err != nil {
				log.Printf("WARN [prewarm] client=%s technique=%s store error=%v", client, t.ID, err)
				return
			}

			mu.Lock()
			warmed++
			mu.Unlock()
			log.Printf("INFO [prewarm] client=%s technique=%s status=warmed", client, t.ID)
		}(tech, cacheKey)
	}

	wg.Wait()
	log.Printf("INFO [prewarm] client=%s done warmed=%d skipped=%d errors=%d elapsed=%s",
		client, warmed, skipped, errors, time.Since(start).Round(time.Second))
}
```

- [ ] **Step 3: Compile**

```bash
cd backend && go build ./internal/prewarm/
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/prewarm/worker.go
git commit -m "feat(prewarm): implement Start method with 3-worker semaphore"
```

---

## Task 4: Write unit tests for the worker

**Files:**
- Create: `backend/internal/prewarm/worker_test.go`

- [ ] **Step 1: Write tests**

```go
package prewarm

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

func TestBuildCacheKey_Deterministic(t *testing.T) {
	// Same inputs in different order must produce same key.
	sources1 := []string{"Okta", "CloudTrail", "GitHub"}
	sources2 := []string{"GitHub", "Okta", "CloudTrail"}

	key1 := buildCacheKey("T1078", sources1)
	key2 := buildCacheKey("T1078", sources2)

	if key1 != key2 {
		t.Errorf("expected same key for same inputs regardless of order, got %s vs %s", key1, key2)
	}
}

func TestBuildCacheKey_DifferentTechnique(t *testing.T) {
	sources := []string{"Okta", "CloudTrail"}

	key1 := buildCacheKey("T1078", sources)
	key2 := buildCacheKey("T1059", sources)

	if key1 == key2 {
		t.Errorf("expected different keys for different technique IDs")
	}
}

func TestBuildCacheKey_MatchesHandlerLogic(t *testing.T) {
	// Verify our key matches the algorithm used in handlers.go buildSuggestionCacheKey.
	techniqueID := "T1078"
	logSources := []string{"GitHub", "Okta", "CloudTrail"}

	sorted := make([]string, len(logSources))
	copy(sorted, logSources)
	sort.Strings(sorted)
	raw := techniqueID + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	expected := hex.EncodeToString(sum[:])

	got := buildCacheKey(techniqueID, logSources)
	if got != expected {
		t.Errorf("buildCacheKey = %s, want %s", got, expected)
	}
}

func TestPrewarmWorkers_Constant(t *testing.T) {
	if prewarmWorkers != 3 {
		t.Errorf("prewarmWorkers = %d, want 3", prewarmWorkers)
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd backend && go test ./internal/prewarm/ -v
```
Expected:
```
--- PASS: TestBuildCacheKey_Deterministic
--- PASS: TestBuildCacheKey_DifferentTechnique
--- PASS: TestBuildCacheKey_MatchesHandlerLogic
--- PASS: TestPrewarmWorkers_Constant
PASS
```

- [ ] **Step 3: Commit**

```bash
git add backend/internal/prewarm/worker_test.go
git commit -m "test(prewarm): add cache key and constant tests"
```

---

## Task 5: Wire Worker into Handler and HandleAnalyze

**Files:**
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Add import and fields to Handler**

In the import block, add:
```go
"coralogix-alert-analyzer/internal/mitre"
"coralogix-alert-analyzer/internal/prewarm"
```

Change the `Handler` struct from:
```go
type Handler struct {
	config       *config.Config
	mondayClient *monday.Client
	cache        *cache.Store
	alertStore   *store.Store
}
```
To:
```go
type Handler struct {
	config         *config.Config
	mondayClient   *monday.Client
	cache          *cache.Store
	alertStore     *store.Store
	prewarmWorker  *prewarm.Worker
	prewarmCancels sync.Map // client string → context.CancelFunc
}
```

- [ ] **Step 2: Update NewHandler signature**

Change:
```go
func NewHandler(cfg *config.Config, redisStore *cache.Store, alertStore *store.Store) *Handler {
	return &Handler{
		config:       cfg,
		mondayClient: monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:        redisStore,
		alertStore:   alertStore,
	}
}
```
To:
```go
func NewHandler(cfg *config.Config, redisStore *cache.Store, alertStore *store.Store, prewarmWorker *prewarm.Worker) *Handler {
	return &Handler{
		config:        cfg,
		mondayClient:  monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:         redisStore,
		alertStore:    alertStore,
		prewarmWorker: prewarmWorker,
	}
}
```

- [ ] **Step 3: Add pre-warm trigger at end of HandleAnalyze**

After `writeJSON(w, http.StatusOK, resp)` (line ~252), add:

```go
	// Trigger background suggestion pre-warm (non-blocking).
	// Cancels any in-flight pre-warm for this client first.
	if h.prewarmWorker != nil && h.alertStore != nil {
		if prev, ok := h.prewarmCancels.Load(req.Client); ok {
			prev.(context.CancelFunc)()
		}
		prewarmCtx, cancel := context.WithCancel(context.Background())
		h.prewarmCancels.Store(req.Client, cancel)
		uncovered := mitre.GetUncoveredTechniques(alerts)
		go h.prewarmWorker.Start(prewarmCtx, req.Client, clientCfg, uncovered)
	}
```

- [ ] **Step 4: Compile**

```bash
cd backend && go build ./...
```
Expected: error from `main.go` (NewHandler call now needs 4 args) — that's correct, fix in next task.

- [ ] **Step 5: Commit (even with the expected build error in main)**

```bash
git add backend/internal/api/handlers.go
git commit -m "feat(prewarm): wire Worker into Handler, trigger post-analyze"
```

---

## Task 6: Update main.go to pass Worker to NewHandler

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add import and create Worker**

In `main.go`, add import:
```go
alertprewarm "coralogix-alert-analyzer/internal/prewarm"
```

After the `neonStore` block (around line 73, after `go worker.Start(ctx)`), add:

```go
	// Suggestion pre-warm worker — nil-safe if NeonDB is unavailable.
	var prewarmWorker *alertprewarm.Worker
	if neonStore != nil {
		prewarmWorker = alertprewarm.New(cfg, neonStore, monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID))
	}
```

- [ ] **Step 2: Update NewHandler call**

Change:
```go
handler := api.NewHandler(cfg, redisStore, neonStore)
```
To:
```go
handler := api.NewHandler(cfg, redisStore, neonStore, prewarmWorker)
```

- [ ] **Step 3: Build everything**

```bash
cd backend && go build ./...
```
Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(prewarm): wire Worker into server startup"
```

---

## Task 7: Smoke test end-to-end

- [ ] **Step 1: Rebuild binary and restart**

```bash
cd backend && go build -o coralogix-alert-analyzer ./cmd/server/
pkill -f coralogix-alert-analyzer 2>/dev/null; sleep 1
nohup ./coralogix-alert-analyzer > /tmp/backend.log 2>&1 &
sleep 4 && tail -8 /tmp/backend.log
```
Expected: server started, NeonDB connected.

- [ ] **Step 2: Trigger analyze and watch logs**

```bash
curl -s -X POST http://localhost:8080/api/analyze \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel"}' | jq '.stats' &
sleep 5 && grep -i prewarm /tmp/backend.log | head -20
```
Expected log lines:
```
INFO [prewarm] client=Deel starting techniques=N workers=3
INFO [prewarm] client=Deel technique=T1078 status=skipped  (or warmed)
```

- [ ] **Step 3: Verify a cached suggestion returns instantly**

Wait ~2 minutes, then check a specific technique that was logged as `warmed`:
```bash
curl -s -w "\nTIME:%{time_total}s\n" \
  -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel","technique_id":"T1078","tactic":"Initial Access"}' \
  | tail -3
```
Expected: `HTTP 200`, `TIME:<1s` (cache hit).

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./... 2>&1 | tail -20
```
Expected: all PASS.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: suggestion pre-warm background worker complete"
```
