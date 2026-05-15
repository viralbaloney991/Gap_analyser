# SIEM Alert Analyser

A full-stack AI-powered platform for security operations teams to audit detection coverage, reduce alert noise, map MITRE ATT&CK gaps, and run AI-assisted threat hunts — all from a single interface.

Built for teams managing large detection libraries who need answers to: *"What are we missing? What is firing too much? What is duplicated?"*

---

## Features

| Feature | Description |
|---------|-------------|
| **Similarity Engine** | Groups duplicate and near-duplicate detections using TF-IDF weighted multi-dimensional vectors |
| **MITRE ATT&CK Heatmap** | Visualises tactic and technique coverage; highlights uncovered areas by integration |
| **Noise Detection** | Flags high-volume, burst, periodic, accelerating, and persistent noisy alerts using multi-window behavioural scoring (7d/14d/21d/30d) |
| **Alert Insights** | LLM-generated actionable recommendations per detection — tuning suggestions, suppression candidates, enrichment ideas |
| **Threat Graph** | Force-directed graph of alert relationships, integration coverage, and detection families |
| **Detection Builder** | Step-by-step wizard to build new detection rules from selected MITRE techniques, with generated query output |
| **AI Threat Hunt** | Two-pass streaming hunt: schema discovery → findings report with field mapping, query translation, false positive analysis, and follow-up queries |
| **Correlation Engine** | Identifies integration gaps — log sources with no associated detections |
| **Export** | LLM-generated executive narrative summarising the full analysis as a PDF-ready report |
| **Issue Tracker Sync** | Pushes gap findings to a project management board (Monday.com by default, configurable) |

---

## Architecture

```
┌─────────────────────────────────────────────┐
│               React + TypeScript             │  frontend/
│  TenantSelector → Analyze → Results tabs     │
└────────────────────┬────────────────────────┘
                     │ REST / SSE
┌────────────────────▼────────────────────────┐
│              Go API Server                   │  backend/
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐ │
│  │Similarity│  │  MITRE   │  │   LLM     │ │
│  │ Engine   │  │ Coverage │  │ Pipeline  │ │
│  └──────────┘  └──────────┘  └───────────┘ │
│                                              │
│  Redis (L1 cache)   PostgreSQL (L2 store)    │
└──────────┬──────────────────────────────────┘
           │ API
┌──────────▼──────────────────────────────────┐
│        SIEM Platform (alert/detection source)│
│   Splunk · Sentinel · Chronicle · Coralogix  │
└─────────────────────────────────────────────┘
```

---

## Project Structure

```
.
├── backend/
│   ├── cmd/server/          # Entry point, HTTP routing, CORS middleware
│   └── internal/
│       ├── api/             # HTTP handlers (analyze, noise, hunt, export …)
│       ├── coralogix/       # SIEM client — alert fetch, event counts, feature extraction
│       ├── similarity/      # Similarity engine, noise scoring, merge logic
│       ├── mitre/           # ATT&CK coverage computation
│       ├── insights/        # LLM enrichment pipeline (insights, suggestions, correlations)
│       ├── llm/             # Provider adapters: Claude, NVIDIA NIM, Gemini
│       ├── models/          # Shared domain types
│       ├── monday/          # Issue tracker client
│       ├── pipeline/        # Semaphore-bounded parallel LLM pipeline
│       ├── cache/           # Redis L1 cache
│       ├── store/           # PostgreSQL persistent alert store
│       ├── sync/            # Background worker: DB ↔ cache sync
│       └── prewarm/         # Suggestion pre-warm worker
├── frontend/
│   └── src/
│       ├── components/
│       │   ├── ClientSelector.tsx     # Tenant/environment picker
│       │   ├── AlertInsights.tsx      # Insights, noise, suggestions, gaps
│       │   ├── MITREHeatmap.tsx       # ATT&CK coverage heatmap
│       │   ├── ThreatGraph.tsx        # Force-directed alert relationship graph
│       │   ├── DetectionBuilder.tsx   # Detection rule wizard
│       │   ├── HuntView.tsx           # AI threat hunt streaming UI
│       │   ├── IntegrationSummary.tsx # Integration ↔ alert coverage table
│       │   └── NoisePills.tsx         # Lookback window selector
│       ├── services/api.ts            # API client + SSE streaming
│       └── types/index.ts             # Shared TypeScript types
└── dev.sh                             # Dev startup script
```

---

## Requirements

- **Go** 1.21+
- **Node.js** 18+ with npm
- **Redis** (optional, recommended) — `localhost:6379` for response caching
- **PostgreSQL** (optional) — for persistent alert storage and pre-warming
- SIEM platform credentials (API key + region/endpoint)
- At least one LLM provider key (Anthropic Claude, NVIDIA NIM, or Google Gemini)

---

## Configuration

The backend reads from `backend/clients.yaml` (excluded from version control). Copy the example and fill in your values:

```bash
cp backend/clients.yaml.example backend/clients.yaml
```

Structure:

```yaml
monday_api_token: ""          # issue tracker token (or set MONDAY_API_TOKEN)
monday_board_id: 0            # board/project ID for gap findings

llm:
  default_provider: claude    # claude | nvidia | gemini
  claude_model: claude-opus-4-7
  pipeline_global_cap: 5      # max concurrent LLM calls
  pipeline_batch_size: 10

clients:
  - name: my-environment
    region: EU1               # SIEM region/endpoint
    api_key: ""               # set via CLIENT_<NAME>_API_KEY env var
    monday_group_id: ""       # issue tracker group for this tenant
```

### Environment variables

All secrets should be supplied via environment variables:

| Variable | Purpose |
|----------|---------|
| `ANTHROPIC_API_KEY` | Claude LLM |
| `NVIDIA_API_KEY` | NVIDIA NIM LLM |
| `GEMINI_API_KEY` | Google Gemini LLM |
| `MONDAY_API_TOKEN` | Issue tracker |
| `CLIENT_<NAME>_API_KEY` | Per-tenant SIEM API key |
| `NEON_DSN` | PostgreSQL connection string |
| `REDIS_ADDR` | Redis address (default: `localhost:6379`) |
| `PORT` | API server port (default: `8080`) |
| `CORS_ORIGIN` | Allowed frontend origin (default: `http://localhost:5173`) |
| `CONFIG_PATH` | Path to config file (default: `clients.yaml`) |
| `FRONTEND_DIST` | Path to built frontend (default: `../../frontend/dist`) |
| `CX_BIN_PATH` | Path to SIEM CLI binary (used by threat hunt) |

---

## Running Locally

```bash
# Backend
cd backend
go run ./cmd/server

# Frontend (separate terminal)
cd frontend
npm install
npm run dev
```

Frontend dev server: `http://localhost:5173`  
API server: `http://localhost:8080`

To serve the frontend from the Go server (production mode):

```bash
cd frontend && npm run build
cd ../backend && go run ./cmd/server
```

---

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/clients` | List configured tenants |
| `POST` | `/api/analyze` | Full analysis pipeline (similarity, MITRE, noise, insights) |
| `POST` | `/api/noise` | Noise-only re-run with configurable lookback |
| `POST` | `/api/insights` | Insights-only re-run |
| `POST` | `/api/suggestions` | LLM detection suggestions for uncovered techniques |
| `POST` | `/api/correlations` | Integration gap correlations |
| `POST` | `/api/build-detection` | Generate detection rule from MITRE technique |
| `POST` | `/api/map-tactics` | Map free-text query to MITRE tactics |
| `GET` | `/api/mitre-catalog` | Full local MITRE ATT&CK technique catalog |
| `GET` | `/api/hunt/stream` | SSE: AI threat hunt (two-pass, streaming) |
| `POST` | `/api/hunt/export` | Export hunt report as markdown |
| `POST` | `/api/export/narrative` | LLM executive narrative for full report |

---

## Noise Detection

The noise engine uses **multi-window behavioural scoring** across four fixed lookback windows (7d / 14d / 21d / 30d) to classify alert firing patterns:

| Pattern | Signal | Threshold |
|---------|--------|-----------|
| `high_volume` | 30d count > 10 | Existing volume threshold |
| `burst` | Recent week ÷ expected weekly share | > 2.5× |
| `periodic` | Deviation from even firing rate | < 20% delta |
| `accelerating` | Gradual ramp in recent window | > 1.5× |
| `persistent` | Fires every window, total ≤ 10 | Monotone growth |

Each noisy alert shows a pattern badge with a tooltip displaying per-window counts.

---

## Similarity Weights

The similarity engine uses a 10-dimension TF-IDF weighted model. Weights sum to exactly 1.00:

| Dimension | Weight |
|-----------|--------|
| Detection query (Lucene) | 0.25 |
| Alert name tokens | 0.15 |
| Data sources | 0.10 |
| Entities | 0.10 |
| Actions | 0.10 |
| MITRE techniques | 0.10 |
| Conditions | 0.05 |
| Group-by categories | 0.05 |
| Alert type | 0.05 |
| Time window | 0.05 |

---

## Tech Stack

**Backend:** Go · Redis · PostgreSQL  
**Frontend:** React · TypeScript · Vite  
**LLM:** Anthropic Claude · NVIDIA NIM · Google Gemini  
**Issue Tracking:** Monday.com (REST API, swappable)
