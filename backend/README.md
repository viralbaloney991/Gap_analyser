# Backend — SIEM Alert Analyser

Go API server powering the SIEM Alert Analyser platform.

## Stack

- **Go 1.21+**
- **net/http** — HTTP server and routing
- **Redis** — L1 response cache
- **PostgreSQL (NeonDB)** — L2 persistent alert store
- **gRPC** — SIEM platform client
- **Anthropic Claude / NVIDIA NIM / Google Gemini** — LLM providers

## Structure

```
backend/
├── cmd/server/              # main.go — entry point, routing, CORS, graceful shutdown
└── internal/
    ├── api/
    │   ├── handlers.go      # All HTTP handlers (analyze, noise, insights, suggestions …)
    │   └── hunt.go          # Threat hunt handler — two-pass SSE streaming
    ├── coralogix/           # SIEM client
    │   ├── client.go        # Alert fetch, event counts (single + multi-window)
    │   ├── features.go      # Feature extraction from alert definitions
    │   └── mapper.go        # Alert definition → domain model mapping
    ├── similarity/
    │   ├── engine.go        # Similarity scoring, dedup, family grouping, noise analysis
    │   └── noise_scoring.go # Multi-window behavioural scoring functions
    ├── mitre/
    │   └── mitre.go         # ATT&CK coverage computation against technique catalog
    ├── insights/
    │   ├── enrich.go        # LLM enrichment pipeline orchestration
    │   ├── enrich_actionable.go  # Actionable insight generation
    │   └── signals.go       # Signal extraction for LLM prompts
    ├── llm/
    │   ├── provider.go      # Provider interface
    │   ├── claude.go        # Anthropic Claude adapter
    │   ├── nvidia.go        # NVIDIA NIM adapter
    │   ├── gemini.go        # Google Gemini adapter
    │   ├── suggestions.go   # Detection suggestion prompts
    │   ├── correlations.go  # Integration gap correlation prompts
    │   ├── detection_builder.go  # Detection rule generation prompts
    │   └── map_tactics.go   # MITRE tactic mapping prompts
    ├── models/
    │   └── models.go        # All shared domain types and API request/response structs
    ├── monday/
    │   └── client.go        # Issue tracker REST client
    ├── pipeline/
    │   ├── run.go           # Parallel LLM pipeline runner
    │   └── semaphore.go     # Global concurrency cap
    ├── cache/
    │   └── redis.go         # Redis L1 cache (get/set/invalidate)
    ├── store/
    │   └── store.go         # PostgreSQL alert persistence
    ├── sync/
    │   └── worker.go        # Background DB ↔ cache sync worker
    └── prewarm/
        └── worker.go        # Suggestion pre-warm on startup
```

## Running

```bash
cd backend
go run ./cmd/server
```

Requires `backend/clients.yaml` or the environment variables listed in the root README.

## Testing

```bash
go test ./...                          # all packages
go test ./internal/similarity/ -v      # specific package
go test ./internal/api/ -run TestNoise # specific test
```

All packages have table-driven unit tests. Integration tests that hit live APIs are excluded from the default test run.

## Configuration

The server reads `clients.yaml` (path overridable via `CONFIG_PATH`). See the root README for the full schema and environment variable reference.

## API Endpoints

| Method | Path | Handler |
|--------|------|---------|
| `GET` | `/api/health` | `HandleHealth` |
| `GET` | `/api/clients` | `HandleClients` |
| `POST` | `/api/analyze` | `HandleAnalyze` |
| `POST` | `/api/noise` | `HandleNoise` |
| `POST` | `/api/insights` | `HandleInsights` |
| `POST` | `/api/suggestions` | `HandleSuggestions` |
| `POST` | `/api/correlations` | `HandleCorrelations` |
| `POST` | `/api/build-detection` | `HandleBuildDetection` |
| `POST` | `/api/map-tactics` | `HandleMapTactics` |
| `GET` | `/api/mitre-catalog` | `HandleMitreCatalog` |
| `GET` | `/api/hunt/stream` | `HandleHuntStream` (SSE) |
| `POST` | `/api/hunt/export` | `HandleHuntExport` |
| `POST` | `/api/export/narrative` | `HandleExportNarrative` |

## Key Invariants

- Similarity weights in `engine.go` must always sum to exactly **1.00**
- All detection logic must be tenant-agnostic — no client-specific code in the engine
- `AnalyzeNoise` (single-window) is kept unchanged for the full analysis pipeline; `AnalyzeNoiseMultiWindow` is used by `HandleNoise` only
- Redis cache is optional — all handlers fall back gracefully when unavailable
