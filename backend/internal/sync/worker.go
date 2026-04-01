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
