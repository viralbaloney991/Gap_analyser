# Alert Persistence Layer — Design Spec

**Goal:** Replace live-per-request Coralogix alert fetching with a NeonDB-backed persistent store, refreshed every 24 hours via a background sync worker, with Redis remaining as the fast L1 cache.

**Architecture:** Two-tier read path — Redis (30-min TTL for processed results) in front of NeonDB (24-h refresh of raw alerts). Coralogix gRPC is only called by the background sync worker, never by request handlers directly.

**Tech Stack:** Go, NeonDB (serverless Postgres via `pgx/v5`), Redis (existing)

---

## Data Flow

### Normal request (after first sync)
```
HandleAnalyze
  → Redis GET                          → hit: return (21ms)
  → Redis miss → store.LoadAlerts      → NeonDB (~50ms)
               + monday.FetchIntegrations (live, ~2s)
  → MITRE pipeline (per-alert Redis cache, 7-day TTL)
  → Redis SET (30-min TTL)
  → return
```

### Background sync (every 24h per client)
```
SyncClient(client)
  → coralogix.FetchActiveAlerts        (live gRPC, ~7s)
  → store.UpsertAlerts                 (bulk upsert to NeonDB)
  → store.SetLastSynced
  → redis.Invalidate(client)           (bust stale cache)
```

### First boot (DB empty, sync not yet complete)
```
HandleAnalyze
  → Redis miss
  → store.LoadAlerts → empty
  → fall back: coralogix.FetchActiveAlerts (live, same as today)
  → process + cache normally
```

---

## Schema

```sql
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
```

- `client_alerts`: one row per alert per client. Full `AlertDef` serialised to JSONB.
- `sync_state`: one row per client. Tracks last successful sync time.
- Migrations run automatically at server startup via `store.New()`.

---

## New Packages

### `internal/store/store.go`

NeonDB client. Pure data access — no business logic.

| Function | Description |
|---|---|
| `New(dsn string) (*Store, error)` | Connect, ping, run `CREATE TABLE IF NOT EXISTS` migrations |
| `LoadAlerts(ctx, client) ([]*models.AlertDef, error)` | Read all rows for client, deserialise JSONB |
| `UpsertAlerts(ctx, client, alerts) error` | Bulk `INSERT … ON CONFLICT (client, alert_id) DO UPDATE SET data=EXCLUDED.data, fetched_at=NOW()` |
| `GetLastSynced(ctx, client) (time.Time, bool, error)` | Read from `sync_state`; bool=false if never synced |
| `SetLastSynced(ctx, client, t) error` | Upsert into `sync_state` |

### `internal/sync/worker.go`

Background sync scheduler. One goroutine per server process.

| Function | Description |
|---|---|
| `New(store, coralogixClients map[string]config.ClientConfig, cache) *Worker` | Constructor |
| `Start(ctx)` | For each client: if never synced or last sync >24h ago, fire `SyncClient` immediately as a goroutine. Then start a 24h ticker. |
| `SyncClient(ctx, clientName, clientCfg)` | Fetch from Coralogix → upsert to NeonDB → invalidate Redis |

---

## Modified Files

| File | Change |
|---|---|
| `internal/config/config.go` | Add `NeonDSN string \`yaml:"neon_dsn"\`` to top-level `Config`. Read `NEON_DSN` env var override. Validate non-empty at startup. |
| `internal/api/handlers.go` | `Handler` gains `store *store.Store` field. `HandleAnalyze` calls `store.LoadAlerts` instead of `fetchAlerts`; falls back to live fetch if result is empty. `HandleSuggestions` unchanged (already fetches alerts live for log-source discovery — acceptable at 7s, infrequent). |
| `cmd/server/main.go` | Init `store.New(cfg.NeonDSN)`, init `sync.New(...)`, call `worker.Start(ctx)`. |
| `clients.yaml` | Add `neon_dsn: "postgres://…"` at top level. |
| `go.mod` | Add `github.com/jackc/pgx/v5`. |

---

## Edge Cases

| Scenario | Behaviour |
|---|---|
| First boot, no alerts in DB | Fall back to live Coralogix fetch; sync completes in background |
| NeonDB unreachable | Log error, fall back to live fetch; never fail user request |
| Coralogix unreachable during sync | Log error, keep existing DB data; retry on next 24h tick |
| Server restart | Startup check re-syncs any client whose `last_synced` is >24h old |
| Sync races with in-flight request | Safe — upsert is per-row atomic; partial sync serves valid old rows |
| Redis cache warm, DB just synced | Redis invalidated post-sync; next request reprocesses from fresh DB data |

---

## Configuration

`clients.yaml` addition:
```yaml
neon_dsn: "postgresql://neondb_owner:<password>@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require"
```

Can also be set via `NEON_DSN` environment variable (takes precedence over yaml).

---

## Out of Scope

- `HandleSuggestions` continues fetching alerts live (used for log-source discovery, called infrequently, acceptable latency)
- Monday.com integrations remain live (2s, 12 items, no persistence needed)
- Per-alert MITRE mapping cache (already in Redis with 7-day TTL, unchanged)
