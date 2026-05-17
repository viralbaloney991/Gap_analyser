# Detection Library & Export — Design Spec

## Goal

Persist generated detections (from the Detection Builder and MITRE Suggestions panel) in a global library, allow filtering by client and severity, export as a Sigma `.yml` bundle, and push individual detections directly to Coralogix as live alert rules.

## Architecture

NeonDB-backed REST API (`/api/library`) with a new `Library` top-level view in React. Detections are stored globally and tagged with client name. Save is one click from any detection card. Export and push are explicit user actions — no background sync.

## Tech Stack

- **Backend:** Go, NeonDB (PostgreSQL), existing `store` package patterns
- **Frontend:** React + TypeScript, new `LibraryView` + `DetectionCard` components
- **Export:** server-side zip via `archive/zip`
- **Push:** Coralogix Alerts REST API, client API key from existing config

---

## Section 1 — Data Model

New table in NeonDB:

```sql
CREATE TABLE saved_detections (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  client         TEXT NOT NULL,
  source         TEXT NOT NULL CHECK (source IN ('builder', 'suggestions')),
  title          TEXT NOT NULL,
  technique_id   TEXT NOT NULL,
  tactic         TEXT NOT NULL,
  lucene_query   TEXT NOT NULL,
  sigma_rule     TEXT NOT NULL,
  severity       TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low')),
  log_source     TEXT NOT NULL,
  falsepositives TEXT[],
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX saved_detections_client_idx ON saved_detections (client);
CREATE INDEX saved_detections_technique_idx ON saved_detections (technique_id);
CREATE INDEX saved_detections_created_idx ON saved_detections (created_at DESC);
```

`client` is a plain text tag (no FK) so any client name works without a separate clients table. `source` distinguishes builder-generated chains from per-technique gap suggestions.

New methods on `store.Store`:

```go
func (s *Store) SaveDetection(ctx context.Context, d SavedDetection) (uuid.UUID, error)
func (s *Store) ListDetections(ctx context.Context, filter DetectionFilter) ([]SavedDetection, error)
func (s *Store) DeleteDetection(ctx context.Context, id uuid.UUID) error
func (s *Store) GetDetection(ctx context.Context, id uuid.UUID) (*SavedDetection, error)
```

`DetectionFilter`:

```go
type DetectionFilter struct {
  Client      string // empty = all clients
  TechniqueID string // empty = all techniques
  Severity    string // empty = all severities
  Limit       int    // default 100
}
```

---

## Section 2 — Backend API

Five new endpoints registered in `main.go`:

| Method   | Path                      | Description |
|----------|---------------------------|-------------|
| `POST`   | `/api/library`            | Save a detection |
| `GET`    | `/api/library`            | List with optional `?client=&technique=&severity=` |
| `DELETE` | `/api/library/{id}`       | Remove by UUID |
| `GET`    | `/api/library/export`     | Download Sigma `.zip` (respects `?client=` filter) |
| `POST`   | `/api/library/{id}/push`  | Push to Coralogix as a live alert rule |

### POST /api/library

Request body:

```json
{
  "client": "Deel",
  "source": "builder",
  "title": "Detect Valid Account Abuse via Anomalous Source Logon",
  "technique_id": "T1078",
  "tactic": "initial-access",
  "lucene_query": "event.category:authentication AND ...",
  "sigma_rule": "title: Detect Valid Account...\n...",
  "severity": "high",
  "log_source": "EDR",
  "falsepositives": ["Break-glass admin usage"]
}
```

Response: `{"id": "<uuid>", "created_at": "<iso8601>"}` with HTTP 201.

### GET /api/library

Response:

```json
{
  "detections": [
    {
      "id": "<uuid>",
      "client": "Deel",
      "source": "builder",
      "title": "...",
      "technique_id": "T1078",
      "tactic": "initial-access",
      "severity": "high",
      "log_source": "EDR",
      "lucene_query": "...",
      "sigma_rule": "...",
      "falsepositives": ["..."],
      "created_at": "..."
    }
  ],
  "total": 24
}
```

### GET /api/library/export

Streams a `application/zip` response. Filename: `detections-<client>-<date>.zip` (or `detections-all-<date>.zip` if no client filter). Each detection becomes `<technique_id>-<slug>.yml` inside the zip. The file content is the raw `sigma_rule` string.

### POST /api/library/{id}/push

Constructs a Coralogix alert rule payload from `lucene_query`, `severity`, and `log_source`. Calls the Coralogix Alerts API (`POST /api/v1/external/alerts`) using the client's API key from config. If no API key is configured for the detection's client, returns HTTP 422 with `{"error": "no API key configured for client <X>"}`.

Success response:

```json
{
  "coralogix_alert_id": "<id>",
  "url": "https://dashboard.coralogix.com/alerts/<id>"
}
```

The handler lives in a new file `backend/internal/api/library.go` (not added to the already-large `handlers.go`).

---

## Section 3 — Frontend

### New view type

`App.tsx` — add `'library'` to the `View` type:

```ts
type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder' | 'hunt' | 'library';
```

**Entry point:** A `Library` button is added to the top-right app header (alongside the existing `BACK` / `CACHED` / `ONLINE` indicators), visible whenever `view !== 'form'` (i.e., once a client is selected). Clicking it navigates to the `library` view. Breadcrumb label: `"Detection Library"`.

### New components

**`frontend/src/components/LibraryView.tsx`**

Full-page Library view. Layout:

```
┌─────────────────────────────────────────────────────┐
│  DETECTION LIBRARY                    [↓ Export Sigma (.zip)]  │
│  [🔍 Search…] [Client ▾] [Severity ▾]               │
├─────────────────────────────────────────────────────┤
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ │
│  │ HIGH builder │ │ MED suggest  │ │ HIGH suggest │ │
│  │ Title…       │ │ Title…       │ │ Title…       │ │
│  │ T1078·EDR    │ │ T1053·Win    │ │ T1003·EDR    │ │
│  │ [View][Push] │ │ [View][Push] │ │ [View][Push] │ │
│  └──────────────┘ └──────────────┘ └──────────────┘ │
└─────────────────────────────────────────────────────┘
```

State: `detections`, `loading`, `filter` (client, severity, search text), `pushState` map (`uuid → idle|pushing|pushed|error`).

Filter bar does client-side filtering on the loaded list (no re-fetch per keystroke). Export button calls `GET /api/library/export` with current client filter and triggers browser download.

**`frontend/src/components/DetectionCard.tsx`**

Reusable card used both in `LibraryView` and as an inline save-preview. Props:

```ts
interface DetectionCardProps {
  detection: SavedDetection;
  onDelete?: (id: string) => void;
  onPush?: (id: string) => Promise<void>;
  pushState?: 'idle' | 'pushing' | 'pushed' | 'error';
  pushUrl?: string; // link shown after successful push
}
```

Card shows: severity chip, source badge (`builder` / `suggestions`), title, `technique_id · log_source · client · relative time`, then `[View] [Push →CX] [✕]` action row. "View" expands an inline `<pre>` showing the Sigma YAML.

### Save button

A `Save` button is added to:

1. Each alert card in `DetectionBuilder.tsx` generated panel (inside `.ac-body`)
2. Each suggestion card in `MITREHeatmap.tsx` `SuggestionsPanel`

Click handler calls `POST /api/library` with the detection's fields + current `clientName`. On success: button changes to `✓ Saved` (disabled) for 2 seconds, then resets. On error: button text changes to `✗ Error`, resets after 3 seconds.

### New API service methods

`frontend/src/services/api.ts`:

```ts
saveDetection(payload: SaveDetectionRequest): Promise<{ id: string; created_at: string }>
listDetections(filter?: { client?: string; severity?: string }): Promise<LibraryResponse>
deleteDetection(id: string): Promise<void>
pushDetection(id: string): Promise<{ coralogix_alert_id: string; url: string }>
exportDetections(client?: string): Promise<Blob> // triggers download
```

---

## Section 4 — Coralogix Push Detail

The push handler maps app severity to Coralogix alert severity:

| App severity | Coralogix severity |
|---|---|
| `critical` | `CRITICAL` |
| `high` | `HIGH` |
| `medium` | `MEDIUM` |
| `low` | `LOW` |

The Coralogix alert payload sets:

- `name` → detection `title`
- `description` → `"Auto-generated from CXAlert Detection Library — technique <technique_id>"`
- `filters.filterType` → `LUCENE`
- `filters.luceneQuery` → `lucene_query`
- `severity` → mapped severity
- `isActive` → `true`

If the Coralogix API returns an error, the push endpoint logs it and returns HTTP 502 with the Coralogix error message forwarded to the frontend.

---

## Files Touched

| File | Change |
|---|---|
| `backend/internal/store/detections.go` | New file — `SavedDetection` model, `SaveDetection`, `ListDetections`, `DeleteDetection`, `GetDetection` |
| `backend/internal/models/library.go` | New file — `SaveDetectionRequest`, `LibraryResponse`, `PushResponse` |
| `backend/internal/api/library.go` | New file — 5 handler functions |
| `backend/cmd/server/main.go` | Register 5 new routes |
| `frontend/src/components/LibraryView.tsx` | New file — Library view |
| `frontend/src/components/DetectionCard.tsx` | New file — reusable card |
| `frontend/src/components/DetectionBuilder.tsx` | Add Save button to generated alert cards |
| `frontend/src/components/MITREHeatmap.tsx` | Add Save button to suggestion cards |
| `frontend/src/services/api.ts` | Add 5 new API methods |
| `frontend/src/types/index.ts` | Add `SavedDetection`, `SaveDetectionRequest`, `LibraryResponse` types |
| `frontend/src/App.tsx` | Add `'library'` view, nav entry |
| `frontend/src/App.css` | Library view styles, detection card styles |
