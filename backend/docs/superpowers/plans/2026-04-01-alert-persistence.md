# Alert Persistence Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace live-per-request Coralogix alert fetching with NeonDB-backed persistence, refreshed every 24 hours by a background sync worker, with Redis remaining as the L1 cache.

**Architecture:** Two new packages (`internal/store` for NeonDB CRUD, `internal/sync` for the background ticker). `HandleAnalyze` reads alerts from NeonDB instead of Coralogix directly; the sync worker is the only code that ever calls Coralogix. Falls back to live Coralogix fetch on first boot before sync completes.

**Tech Stack:** Go, NeonDB (serverless Postgres) via `github.com/jackc/pgx/v5`, Redis (existing)

---

## File Map

| Action | Path | Responsibility |
|---|---|---|
| Create | `internal/store/store.go` | NeonDB connection pool, migrations, CRUD |
| Create | `internal/store/store_test.go` | Integration tests (skip without `NEON_DSN`) |
| Create | `internal/sync/worker.go` | 24h background sync ticker |
| Modify | `internal/config/config.go` | Add `NeonDSN` field + `NEON_DSN` env override |
| Modify | `internal/api/handlers.go` | Add `alertStore` field; read alerts from store |
| Modify | `cmd/server/main.go` | Init store + worker, cancel ctx on shutdown |
| Modify | `clients.yaml` | Add `neon_dsn` key |

---

## Task 1: Add pgx dependency and NeonDSN config field

**Files:**
- Modify: `go.mod` (via `go get`)
- Modify: `internal/config/config.go`
- Modify: `clients.yaml`

- [ ] **Step 1: Install pgx**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go get github.com/jackc/pgx/v5
```

Expected: `go.mod` and `go.sum` updated with `github.com/jackc/pgx/v5`.

- [ ] **Step 2: Add NeonDSN to Config struct**

In `internal/config/config.go`, add `NeonDSN` to the `Config` struct (after `Classifier`):

```go
// Config holds the application configuration.
type Config struct {
	MondayAPIToken string                  `yaml:"monday_api_token"`
	MondayBoardID  int64                   `yaml:"monday_board_id"`
	Clients        map[string]ClientConfig `yaml:"clients"`
	LLM            LLMConfig               `yaml:"llm"`
	Classifier     ClassifierConfig        `yaml:"classifier"`
	NeonDSN        string                  `yaml:"neon_dsn"`
}
```

- [ ] **Step 3: Add NEON_DSN env override in Load()**

In `internal/config/config.go`, add inside `Load()` after the existing env var overrides (after the `GEMINI_API_KEY` block, before the validation section):

```go
	if v := os.Getenv("NEON_DSN"); v != "" {
		cfg.NeonDSN = v
	}
```

- [ ] **Step 4: Add neon_dsn to clients.yaml**

Add as the first line of `clients.yaml` (before `monday_api_token`):

```yaml
neon_dsn: "postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require"
```

- [ ] **Step 5: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: no output (clean build).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config/config.go clients.yaml
git commit -m "feat: add pgx dependency and NeonDSN config field"
```

---

## Task 2: Implement internal/store package

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/store/store_test.go`:

```go
package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/store"
)

func testDSN(t *testing.T) string {
	t.Helper()
	d := os.Getenv("NEON_DSN")
	if d == "" {
		t.Skip("NEON_DSN not set — skipping integration test")
	}
	return d
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.New(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestLoadAlerts_Empty(t *testing.T) {
	s := newStore(t)
	alerts, err := s.LoadAlerts(context.Background(), "no-such-client")
	if err != nil {
		t.Fatalf("LoadAlerts: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("want 0 alerts, got %d", len(alerts))
	}
}

func TestUpsertAndLoad(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	client := "test-upsert-" + t.Name()

	input := []*models.AlertDef{
		{ID: "a1", Name: "Alert One", Enabled: true, AlertType: "logs_immediate"},
		{ID: "a2", Name: "Alert Two", Enabled: false, AlertType: "logs_threshold"},
	}

	if err := s.UpsertAlerts(ctx, client, input); err != nil {
		t.Fatalf("UpsertAlerts: %v", err)
	}

	got, err := s.LoadAlerts(ctx, client)
	if err != nil {
		t.Fatalf("LoadAlerts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 alerts, got %d", len(got))
	}

	// Upsert again with updated name — should replace, not duplicate
	input[0].Name = "Alert One Updated"
	if err := s.UpsertAlerts(ctx, client, input); err != nil {
		t.Fatalf("UpsertAlerts (second): %v", err)
	}
	got2, _ := s.LoadAlerts(ctx, client)
	if len(got2) != 2 {
		t.Errorf("want 2 after re-upsert, got %d", len(got2))
	}
	foundUpdated := false
	for _, a := range got2 {
		if a.ID == "a1" && a.Name == "Alert One Updated" {
			foundUpdated = true
		}
	}
	if !foundUpdated {
		t.Error("upsert did not update existing row")
	}
}

func TestSyncState_NeverSynced(t *testing.T) {
	s := newStore(t)
	_, ok, err := s.GetLastSynced(context.Background(), "never-synced-client")
	if err != nil {
		t.Fatalf("GetLastSynced: %v", err)
	}
	if ok {
		t.Error("want ok=false for client never synced")
	}
}

func TestSyncState_SetAndGet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	client := "test-syncstate-" + t.Name()

	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SetLastSynced(ctx, client, now); err != nil {
		t.Fatalf("SetLastSynced: %v", err)
	}

	got, ok, err := s.GetLastSynced(ctx, client)
	if err != nil {
		t.Fatalf("GetLastSynced: %v", err)
	}
	if !ok {
		t.Fatal("want ok=true after SetLastSynced")
	}
	if !got.Equal(now) {
		t.Errorf("want %v, got %v", now, got)
	}

	// Idempotent update
	later := now.Add(time.Hour)
	if err := s.SetLastSynced(ctx, client, later); err != nil {
		t.Fatalf("SetLastSynced (update): %v", err)
	}
	got2, _, _ := s.GetLastSynced(ctx, client)
	if !got2.Equal(later) {
		t.Errorf("want updated time %v, got %v", later, got2)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
NEON_DSN="postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require" \
  go test ./internal/store/... -v 2>&1 | head -20
```

Expected: compile error — `internal/store` package does not exist yet.

- [ ] **Step 3: Implement store.go**

Create `internal/store/store.go`:

```go
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"coralogix-alert-analyzer/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a NeonDB (Postgres) connection pool for alert persistence.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to NeonDB, pings, and runs CREATE TABLE IF NOT EXISTS migrations.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("neondb ping: %w", err)
	}

	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("neondb migrate: %w", err)
	}

	log.Printf("connected to NeonDB")
	return s, nil
}

// Close shuts down the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// migrate creates tables if they don't exist. Safe to call on every startup.
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
	`)
	return err
}

// LoadAlerts returns all stored alerts for a client.
// Returns an empty (non-nil) slice if the client has no stored alerts.
func (s *Store) LoadAlerts(ctx context.Context, client string) ([]*models.AlertDef, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT data FROM client_alerts WHERE client = $1`, client)
	if err != nil {
		return nil, fmt.Errorf("query client_alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*models.AlertDef
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		var alert models.AlertDef
		if err := json.Unmarshal(data, &alert); err != nil {
			return nil, fmt.Errorf("unmarshal alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	if alerts == nil {
		alerts = []*models.AlertDef{}
	}
	return alerts, nil
}

// UpsertAlerts bulk-upserts all alerts for a client.
// Existing rows are replaced; new rows are inserted.
func (s *Store) UpsertAlerts(ctx context.Context, client string, alerts []*models.AlertDef) error {
	if len(alerts) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, alert := range alerts {
		data, err := json.Marshal(alert)
		if err != nil {
			return fmt.Errorf("marshal alert %s: %w", alert.ID, err)
		}
		batch.Queue(`
			INSERT INTO client_alerts (client, alert_id, data, fetched_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (client, alert_id)
			DO UPDATE SET data = EXCLUDED.data, fetched_at = NOW()
		`, client, alert.ID, string(data))
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for range alerts {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("upsert batch exec: %w", err)
		}
	}
	return nil
}

// GetLastSynced returns when client was last synced.
// ok=false if this client has never been synced.
func (s *Store) GetLastSynced(ctx context.Context, client string) (time.Time, bool, error) {
	var t time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT last_synced FROM sync_state WHERE client = $1`, client).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query sync_state: %w", err)
	}
	return t, true, nil
}

// SetLastSynced records a successful sync time for client.
func (s *Store) SetLastSynced(ctx context.Context, client string, t time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sync_state (client, last_synced)
		VALUES ($1, $2)
		ON CONFLICT (client)
		DO UPDATE SET last_synced = EXCLUDED.last_synced
	`, client, t)
	if err != nil {
		return fmt.Errorf("upsert sync_state: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
NEON_DSN="postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require" \
  go test ./internal/store/... -v
```

Expected:
```
--- PASS: TestLoadAlerts_Empty
--- PASS: TestUpsertAndLoad
--- PASS: TestSyncState_NeverSynced
--- PASS: TestSyncState_SetAndGet
PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add NeonDB store package with alert persistence"
```

---

## Task 3: Implement internal/sync worker

**Files:**
- Create: `internal/sync/worker.go`

- [ ] **Step 1: Create the worker**

Create `internal/sync/worker.go`:

```go
package sync

import (
	"context"
	"log"
	"time"

	"coralogix-alert-analyzer/internal/cache"
	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/store"
)

const syncInterval = 24 * time.Hour

// Worker runs background syncs of Coralogix alerts into NeonDB.
type Worker struct {
	store   *store.Store
	cache   *cache.Store // may be nil
	clients map[string]config.ClientConfig
}

// New creates a Worker. cache may be nil if Redis is unavailable.
func New(s *store.Store, c *cache.Store, clients map[string]config.ClientConfig) *Worker {
	return &Worker{store: s, cache: c, clients: clients}
}

// Start fires an immediate sync for any client that is stale (never synced or >24h ago),
// then ticks every 24h to sync all clients.
// Blocks until ctx is cancelled — run in a goroutine.
func (w *Worker) Start(ctx context.Context) {
	for name, cfg := range w.clients {
		lastSynced, ok, err := w.store.GetLastSynced(ctx, name)
		if err != nil {
			log.Printf("WARN [sync] check last_synced client=%s: %v", name, err)
		}
		if !ok || time.Since(lastSynced) > syncInterval {
			clientCfg := cfg // capture loop variable
			clientName := name
			go w.SyncClient(ctx, clientName, clientCfg)
		} else {
			log.Printf("INFO [sync] client=%s is fresh (last synced %s ago), skipping startup sync",
				name, time.Since(lastSynced).Round(time.Minute))
		}
	}

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("INFO [sync] worker stopping")
			return
		case <-ticker.C:
			log.Printf("INFO [sync] 24h tick — syncing all clients")
			for name, cfg := range w.clients {
				clientCfg := cfg
				clientName := name
				go w.SyncClient(ctx, clientName, clientCfg)
			}
		}
	}
}

// SyncClient fetches alerts from Coralogix for one client, upserts them into NeonDB,
// and invalidates the Redis cache so the next request reprocesses fresh data.
func (w *Worker) SyncClient(ctx context.Context, clientName string, cfg config.ClientConfig) {
	log.Printf("INFO [sync] starting sync client=%s", clientName)

	cx, err := coralogix.NewClient(cfg.Region, cfg.APIKey)
	if err != nil {
		log.Printf("ERROR [sync] create coralogix client=%s: %v", clientName, err)
		return
	}
	defer cx.Close()

	alerts, err := cx.FetchActiveAlerts(ctx)
	if err != nil {
		log.Printf("ERROR [sync] fetch alerts client=%s: %v — keeping existing DB data", clientName, err)
		return
	}

	if err := w.store.UpsertAlerts(ctx, clientName, alerts); err != nil {
		log.Printf("ERROR [sync] upsert alerts client=%s: %v", clientName, err)
		return
	}

	if err := w.store.SetLastSynced(ctx, clientName, time.Now()); err != nil {
		log.Printf("ERROR [sync] set last_synced client=%s: %v", clientName, err)
		return
	}

	if w.cache != nil {
		w.cache.Invalidate(ctx, clientName)
	}

	log.Printf("INFO [sync] completed client=%s alerts=%d", clientName, len(alerts))
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: clean build, no output.

- [ ] **Step 3: Commit**

```bash
git add internal/sync/
git commit -m "feat: add background sync worker for 24h Coralogix alert refresh"
```

---

## Task 4: Update handlers to read alerts from store

**Files:**
- Modify: `internal/api/handlers.go`

The `Handler` struct currently has 3 fields. We add `alertStore *store.Store` (nil-safe — falls back to live fetch).

- [ ] **Step 1: Add the import and alertStore field**

In `internal/api/handlers.go`, add the store import:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

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

- [ ] **Step 2: Update Handler struct and NewHandler**

Replace:

```go
// Handler holds dependencies for HTTP handlers.
type Handler struct {
	config       *config.Config
	mondayClient *monday.Client
	cache        *cache.Store
}

// NewHandler creates a new Handler with the given config and cache.
func NewHandler(cfg *config.Config, store *cache.Store) *Handler {
	return &Handler{
		config:       cfg,
		mondayClient: monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:        store,
	}
}
```

With:

```go
// Handler holds dependencies for HTTP handlers.
type Handler struct {
	config       *config.Config
	mondayClient *monday.Client
	cache        *cache.Store
	alertStore   *store.Store // NeonDB; nil if unavailable — falls back to live fetch
}

// NewHandler creates a new Handler.
// redisStore and alertStore may each be nil if the respective service is unavailable.
func NewHandler(cfg *config.Config, redisStore *cache.Store, alertStore *store.Store) *Handler {
	return &Handler{
		config:       cfg,
		mondayClient: monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:        redisStore,
		alertStore:   alertStore,
	}
}
```

- [ ] **Step 3: Replace live alert fetch in HandleAnalyze**

In `HandleAnalyze`, find the goroutine that fetches alerts:

```go
go func() {
    defer wg.Done()
    alerts, alertsErr = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
}()
```

Replace with:

```go
go func() {
    defer wg.Done()
    if h.alertStore != nil {
        stored, err := h.alertStore.LoadAlerts(ctx, req.Client)
        if err != nil {
            log.Printf("WARN [analyze] load from store client=%s: %v — falling back to live", req.Client, err)
        } else if len(stored) > 0 {
            log.Printf("INFO [analyze] loaded %d alerts from store client=%s", len(stored), req.Client)
            alerts = stored
            return
        } else {
            log.Printf("INFO [analyze] store empty for client=%s — fetching live (first boot)", req.Client)
        }
    }
    alerts, alertsErr = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
}()
```

- [ ] **Step 4: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: compile error — `NewHandler` call in `cmd/server/main.go` passes 2 args, now needs 3. We'll fix that in Task 5.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go
git commit -m "feat: handlers read alerts from NeonDB store with live fallback"
```

---

## Task 5: Wire up main.go

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Replace main.go**

Replace the full contents of `cmd/server/main.go` with:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"coralogix-alert-analyzer/internal/api"
	"coralogix-alert-analyzer/internal/cache"
	"coralogix-alert-analyzer/internal/config"
	alertstore "coralogix-alert-analyzer/internal/store"
	alertsync "coralogix-alert-analyzer/internal/sync"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "http://localhost:5173"
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "clients.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	log.Printf("loaded config: %d clients", len(cfg.Clients))

	// Context cancelled on shutdown — used by sync worker.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Redis (L1 cache).
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisStore, err := cache.NewStore(redisAddr)
	if err != nil {
		log.Printf("WARN: Redis unavailable (%v) — running without cache", err)
	}
	if redisStore != nil {
		defer redisStore.Close()
	}

	// NeonDB (L2 persistent store).
	var neonStore *alertstore.Store
	if cfg.NeonDSN != "" {
		initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
		neonStore, err = alertstore.New(initCtx, cfg.NeonDSN)
		initCancel()
		if err != nil {
			log.Printf("WARN: NeonDB unavailable (%v) — running without alert persistence", err)
		} else {
			defer neonStore.Close()

			// Start background sync worker.
			worker := alertsync.New(neonStore, redisStore, cfg.Clients)
			go worker.Start(ctx)
		}
	} else {
		log.Printf("INFO: neon_dsn not configured — alert persistence disabled")
	}

	handler := api.NewHandler(cfg, redisStore, neonStore)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", handler.HandleHealth)
	mux.HandleFunc("/api/clients", handler.HandleClients)
	mux.HandleFunc("/api/analyze", handler.HandleAnalyze)
	mux.HandleFunc("/api/suggestions", handler.HandleSuggestions)

	// Serve static frontend files in production.
	frontendDist := os.Getenv("FRONTEND_DIST")
	if frontendDist == "" {
		frontendDist = filepath.Join(".", "..", "..", "frontend", "dist")
	}
	if info, err := os.Stat(frontendDist); err == nil && info.IsDir() {
		fs := http.FileServer(http.Dir(frontendDist))
		mux.Handle("/", fs)
		log.Printf("serving static files from %s", frontendDist)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("coralogix-alert-analyzer API server"))
		})
	}

	wrapped := corsMiddleware(corsOrigin)(mux)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      wrapped,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 180 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("server starting on :%s (CORS origin: %s)", port, corsOrigin)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %s, shutting down gracefully...", sig)

	cancel() // stop sync worker

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server stopped")
}

// corsMiddleware returns middleware that handles CORS headers and preflight requests.
func corsMiddleware(allowOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			allowed := false
			for _, o := range strings.Split(allowOrigin, ",") {
				if strings.TrimSpace(o) == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 2: Build and verify**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: clean build, no output.

- [ ] **Step 3: Run integration tests**

```bash
NEON_DSN="postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require" \
  go test ./internal/store/... -v
```

Expected: all 4 tests pass.

- [ ] **Step 4: Run full test suite**

```bash
go test ./... 2>&1
```

Expected: all existing tests pass (store tests skip without NEON_DSN env set in regular test run).

- [ ] **Step 5: Start server and watch logs**

```bash
pkill -f "go run ./cmd/server" 2>/dev/null; sleep 1
go run ./cmd/server > /tmp/backend.log 2>&1 &
sleep 4 && tail -20 /tmp/backend.log
```

Expected log output (order may vary):
```
loaded config: 1 clients
connected to Redis at localhost:6379
connected to NeonDB
INFO [sync] starting sync client=Deel
server starting on :8080 ...
INFO [sync] completed client=Deel alerts=959
```

- [ ] **Step 6: Verify analyze uses store after sync**

```bash
# Wait for sync to complete (watch logs), then:
curl -s -X POST http://localhost:8080/api/analyze \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel"}' | python3 -c "
import sys, json
d = json.load(sys.stdin)
print('cached:', d.get('cached'))
print('alerts:', d.get('stats', {}).get('total_alerts'))
"
```

Expected:
```
cached: False   (first request after sync, Redis not yet warm)
alerts: 959
```

And in backend logs:
```
INFO [analyze] loaded 959 alerts from store client=Deel
```

NOT:
```
INFO [analyze] MITRE pipeline: ... (should not call Coralogix live)
```

- [ ] **Step 7: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire NeonDB store and sync worker into server startup"
```
