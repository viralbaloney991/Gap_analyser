# World Map Client Selector — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the ClientSelector dropdown with an interactive world map where client dots are placed by Coralogix region, hovering shows the client name, and clicking selects the client.

**Architecture:** Backend adds region to the `/api/clients` response. Frontend installs `react-simple-maps`, copies world TopoJSON to `public/`, and rewrites `ClientSelector.tsx` to render a full-viewport SVG world map with animated client markers. The `onAnalyze` prop interface to `App.tsx` is unchanged.

**Tech Stack:** React 19, TypeScript 5, react-simple-maps v3, world-atlas (dev dep for TopoJSON data), Vite 8, Go 1.x

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/config/config.go` | Add `ClientInfo` struct + `ClientsWithRegion()` method |
| `backend/internal/config/config_test.go` | New — unit test for `ClientsWithRegion()` |
| `backend/internal/api/handlers.go` | `HandleClients` calls `ClientsWithRegion()` instead of `ClientNames()` |
| `frontend/src/types/index.ts` | Add `ClientInfo` interface |
| `frontend/src/services/api.ts` | `fetchClients()` returns `ClientInfo[]` |
| `frontend/public/world-110m.json` | New — Natural Earth 110m TopoJSON (copied from world-atlas) |
| `frontend/package.json` | Add `react-simple-maps`; add `world-atlas` as devDependency |
| `frontend/src/components/ClientSelector.tsx` | Full rewrite — world map with markers |
| `frontend/src/App.css` | Remove old ClientSelector classes; add world map classes |

---

## Task 1: Backend — expose region in `/api/clients`

**Files:**
- Modify: `backend/internal/config/config.go`
- Create: `backend/internal/config/config_test.go`
- Modify: `backend/internal/api/handlers.go:48-55`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/config_test.go`:

```go
package config_test

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestClientsWithRegion_sortedByName(t *testing.T) {
	cfg := &config.Config{
		Clients: map[string]config.ClientConfig{
			"Zebra": {APIKey: "k1", Region: "eu1"},
			"Apple": {APIKey: "k2", Region: "us1"},
			"Mango": {APIKey: "k3", Region: "ap2"},
		},
	}
	got := cfg.ClientsWithRegion()
	if len(got) != 3 {
		t.Fatalf("want 3 clients, got %d", len(got))
	}
	if got[0].Name != "Apple" || got[0].Region != "us1" {
		t.Errorf("index 0: want Apple/us1, got %s/%s", got[0].Name, got[0].Region)
	}
	if got[1].Name != "Mango" || got[1].Region != "ap2" {
		t.Errorf("index 1: want Mango/ap2, got %s/%s", got[1].Name, got[1].Region)
	}
	if got[2].Name != "Zebra" || got[2].Region != "eu1" {
		t.Errorf("index 2: want Zebra/eu1, got %s/%s", got[2].Name, got[2].Region)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/config/... -run TestClientsWithRegion -v
```

Expected: `FAIL — config.ClientInfo undefined` (or similar compile error)

- [ ] **Step 3: Add `ClientInfo` struct and `ClientsWithRegion()` to `config.go`**

In `backend/internal/config/config.go`, add after the closing brace of `ClientNames()` (after line 132):

```go
// ClientInfo holds the name and region of a client, used in the /api/clients response.
type ClientInfo struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// ClientsWithRegion returns client info (name + region) sorted by name.
func (c *Config) ClientsWithRegion() []ClientInfo {
	clients := make([]ClientInfo, 0, len(c.Clients))
	for name, cfg := range c.Clients {
		clients = append(clients, ClientInfo{Name: name, Region: cfg.Region})
	}
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].Name < clients[j].Name
	})
	return clients
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/config/... -run TestClientsWithRegion -v
```

Expected: `PASS`

- [ ] **Step 5: Update `HandleClients` in `handlers.go`**

In `backend/internal/api/handlers.go`, change line 54:

```go
// Before:
writeJSON(w, http.StatusOK, h.config.ClientNames())

// After:
writeJSON(w, http.StatusOK, h.config.ClientsWithRegion())
```

- [ ] **Step 6: Verify backend compiles**

```bash
cd backend && go build ./...
```

Expected: no errors

- [ ] **Step 7: Commit**

```bash
cd backend
git add internal/config/config.go internal/config/config_test.go internal/api/handlers.go
git commit -m "feat: expose region in /api/clients response"
```

---

## Task 2: Frontend — `ClientInfo` type and updated `fetchClients()`

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: Add `ClientInfo` interface to `types/index.ts`**

Add at the top of `frontend/src/types/index.ts` (before `IntegrationInfo`):

```ts
export interface ClientInfo {
  name: string;
  region: string;
}
```

- [ ] **Step 2: Update `fetchClients()` in `api.ts`**

In `frontend/src/services/api.ts`, replace lines 5-9:

```ts
// Before:
export async function fetchClients(): Promise<string[]> {
  const res = await fetch(`${API_BASE}/api/clients`);
  if (!res.ok) throw new Error('Failed to fetch clients');
  return res.json();
}

// After:
import type { ClientInfo } from '../types';

export async function fetchClients(): Promise<ClientInfo[]> {
  const res = await fetch(`${API_BASE}/api/clients`);
  if (!res.ok) throw new Error('Failed to fetch clients');
  return res.json();
}
```

Note: `import type { ClientInfo }` goes at the top of the file with the existing `import type { AnalyzeResponse, SuggestionsResponse }`. Merge it into that line:

```ts
import type { AnalyzeResponse, ClientInfo, SuggestionsResponse } from '../types';
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit
```

Expected: errors only in `ClientSelector.tsx` (which still imports `fetchClients` as `string[]` — will be fixed in Task 4). If there are no errors yet, that's fine too.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/services/api.ts
git commit -m "feat: add ClientInfo type and update fetchClients return type"
```

---

## Task 3: Install dependencies and copy world data

**Files:**
- Modify: `frontend/package.json`
- Create: `frontend/public/world-110m.json`

- [ ] **Step 1: Install `react-simple-maps`**

```bash
cd frontend && npm install react-simple-maps
```

Expected: `react-simple-maps` appears in `package.json` `dependencies`.

- [ ] **Step 2: Install `world-atlas` as devDependency**

```bash
cd frontend && npm install --save-dev world-atlas
```

Expected: `world-atlas` appears in `package.json` `devDependencies`.

- [ ] **Step 3: Copy world TopoJSON to public/**

```bash
cp frontend/node_modules/world-atlas/countries-110m.json frontend/public/world-110m.json
```

Expected: `frontend/public/world-110m.json` exists (~100KB).

- [ ] **Step 4: Verify the file is accessible in dev server**

```bash
cd frontend && npm run dev &
sleep 3
curl -s http://localhost:5173/world-110m.json | head -c 100
kill %1
```

Expected: JSON starting with `{"type":"Topology",...`

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/public/world-110m.json
git commit -m "feat: add react-simple-maps and world-atlas TopoJSON data"
```

---

## Task 4: World map component and CSS

**Files:**
- Modify: `frontend/src/components/ClientSelector.tsx` (full rewrite)
- Modify: `frontend/src/App.css` (replace ClientSelector section)

- [ ] **Step 1: Rewrite `ClientSelector.tsx`**

Replace the entire contents of `frontend/src/components/ClientSelector.tsx` with:

```tsx
import { useState, useEffect, useCallback } from 'react';
import { ComposableMap, Geographies, Geography, Marker } from 'react-simple-maps';
import { fetchClients } from '../services/api';
import type { ClientInfo } from '../types';

const GEO_URL = '/world-110m.json';

const REGION_COORDS: Record<string, [number, number]> = {
  eu1: [-8.2,   53.3],   // Dublin (EU1)
  eu2: [18.1,   59.3],   // Stockholm (EU2)
  us1: [-77.5,  37.8],   // Virginia (US1)
  us2: [-122.8, 45.5],   // Oregon (US2)
  ap1: [72.9,   19.1],   // Mumbai (AP1)
  ap2: [103.8,   1.3],   // Singapore (AP2)
  ap3: [139.7,  35.7],   // Tokyo (AP3)
};

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

interface TooltipState {
  name: string;
  region: string;
  x: number;
  y: number;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients] = useState<ClientInfo[]>([]);
  const [selected, setSelected] = useState('');
  const [fetchError, setFetchError] = useState('');
  const [tooltip, setTooltip] = useState<TooltipState | null>(null);

  useEffect(() => {
    fetchClients()
      .then((list) => {
        setClients(list);
        if (list.length > 0) setSelected(list[0].name);
      })
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  const handleDotClick = useCallback((name: string) => {
    setSelected(name);
    setTooltip(null);
  }, []);

  const handleAnalyze = () => {
    if (selected && !loading) onAnalyze(selected);
  };

  return (
    <div className="client-selector">
      <div className="client-selector-grid" />

      <span className="client-selector-corner top-left">CX_ALERTS v2.1</span>
      <span className="client-selector-corner top-right">
        <span className="status-dot" />
        ONLINE
      </span>

      {fetchError && <div className="error-banner map-error">{fetchError}</div>}

      <ComposableMap
        className="world-map"
        projection="geoEqualEarth"
        projectionConfig={{ scale: 160 }}
      >
        <Geographies geography={GEO_URL}>
          {({ geographies }) =>
            geographies.map((geo) => (
              <Geography
                key={geo.rsmKey}
                geography={geo}
                className="map-geography"
              />
            ))
          }
        </Geographies>

        {clients.map((client) => {
          const coords = REGION_COORDS[client.region];
          if (!coords) return null;
          const isSelected = client.name === selected;
          return (
            <Marker
              key={client.name}
              coordinates={coords}
              onClick={() => handleDotClick(client.name)}
              onMouseEnter={(e: React.MouseEvent) =>
                setTooltip({ name: client.name, region: client.region, x: e.clientX, y: e.clientY })
              }
              onMouseLeave={() => setTooltip(null)}
            >
              <circle
                r={isSelected ? 7 : 5}
                className={`map-dot${isSelected ? ' map-dot--selected' : ''}`}
              />
              <circle
                r={isSelected ? 7 : 5}
                className={`map-dot-ring${isSelected ? ' map-dot-ring--selected' : ''}`}
              />
            </Marker>
          );
        })}
      </ComposableMap>

      {tooltip && (
        <div
          className="map-tooltip"
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          {tooltip.name} · {tooltip.region.toUpperCase()}
        </div>
      )}

      <div className="client-selector-content">
        <div className="selected-client-label">
          {selected ? `▶ ${selected}` : 'Select a client on the map'}
        </div>
        <h2 className="landing-wordmark"><strong>Alert</strong> Analyzer</h2>
        <p className="landing-subtitle">Coralogix Integration Intelligence</p>
        {selected && (
          <button
            className="btn btn-primary"
            onClick={handleAnalyze}
            disabled={loading || !selected}
          >
            {loading ? 'ANALYZING...' : `ANALYZE ${selected} →`}
          </button>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Replace the ClientSelector CSS section in `App.css`**

In `frontend/src/App.css`, find and replace the entire block from `/* ── Client Selector ──` through the end of `.landing-form select:disabled` (lines 144–237 based on current file) with:

```css
/* ── Client Selector — World Map ─────────────────── */
.client-selector {
  position: relative;
  height: calc(100vh - 53px);
  overflow: hidden;
}

.client-selector-grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(var(--border) 1px, transparent 1px),
    linear-gradient(90deg, var(--border) 1px, transparent 1px);
  background-size: 28px 28px;
  mask-image: linear-gradient(to bottom, transparent 0%, black 55%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 55%);
  pointer-events: none;
  z-index: 1;
}

.client-selector-corner {
  position: absolute;
  font-size: 0.68rem;
  letter-spacing: 0.2em;
  color: var(--text-dim);
  text-transform: uppercase;
  z-index: 10;
}
.client-selector-corner.top-left  { top: 20px; left: 28px; }
.client-selector-corner.top-right {
  top: 20px; right: 28px;
  display: flex;
  align-items: center;
  gap: 6px;
}

/* World map SVG */
.world-map {
  position: absolute;
  inset: 0;
  width: 100% !important;
  height: 100% !important;
  z-index: 2;
}

.map-geography {
  fill: rgba(0, 255, 100, 0.06);
  stroke: rgba(0, 255, 100, 0.22);
  stroke-width: 0.5;
  outline: none;
  transition: fill 0.15s;
}

/* Client marker dots */
.map-dot {
  fill: var(--accent);
  cursor: pointer;
  transition: r 0.15s;
}
.map-dot--selected {
  fill: #ffffff;
}

.map-dot-ring {
  fill: var(--accent);
  transform-box: fill-box;
  transform-origin: center;
  pointer-events: none;
  animation: map-ring-pulse 2s ease-out infinite;
}
.map-dot-ring--selected {
  animation: none;
  opacity: 0.35;
}

@keyframes map-ring-pulse {
  0%   { transform: scale(1);   opacity: 0.55; }
  100% { transform: scale(3.5); opacity: 0; }
}

/* Tooltip */
.map-tooltip {
  position: fixed;
  background: var(--surface);
  border: 1px solid var(--border-bright);
  color: var(--text);
  font-size: 0.7rem;
  letter-spacing: 0.08em;
  padding: 4px 10px;
  border-radius: var(--radius);
  pointer-events: none;
  white-space: nowrap;
  z-index: 100;
  transform: translate(-50%, calc(-100% - 10px));
}

/* Error banner over map */
.map-error {
  position: absolute;
  top: 52px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 20;
}

/* Bottom content block */
.client-selector-content {
  position: absolute;
  bottom: 40px;
  left: 48px;
  z-index: 10;
}

.selected-client-label {
  font-size: 0.65rem;
  letter-spacing: 0.15em;
  color: var(--accent);
  text-transform: uppercase;
  margin-bottom: 8px;
  min-height: 1rem;
}

.landing-wordmark {
  font-size: 2.4rem;
  font-weight: 300;
  color: var(--text);
  line-height: 1;
  margin-bottom: 8px;
}
.landing-wordmark strong {
  font-weight: 700;
  color: var(--accent);
}
.landing-wordmark::after {
  content: '_';
  color: var(--accent);
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0; }
}

.landing-subtitle {
  font-size: 0.68rem;
  letter-spacing: 0.2em;
  color: var(--text-dim);
  text-transform: uppercase;
  margin-bottom: 20px;
}
```

- [ ] **Step 3: Verify TypeScript compiles with no errors**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors

- [ ] **Step 4: Run production build to confirm no warnings**

```bash
cd frontend && npm run build
```

Expected: build succeeds, output in `dist/`, no TypeScript or Vite errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ClientSelector.tsx frontend/src/App.css
git commit -m "feat: replace ClientSelector dropdown with interactive world map"
```

---

## Self-Review Checklist

- [x] **Spec coverage:** Section 2 (map rendering) → Tasks 3+4. Section 3 (region coords) → Task 4 `REGION_COORDS`. Section 4 (backend) → Task 1. Section 5 (component) → Task 4. Section 6 (CSS) → Task 4. Section 7 (files) → all tasks.
- [x] **No placeholders** — all steps have exact code.
- [x] **Type consistency** — `ClientInfo` defined in Task 2, used in Task 4. `REGION_COORDS` uses `[number, number]` tuple matching `react-simple-maps` `coordinates` prop type. `TooltipState` defined inline in Task 4 only.
- [x] **API contract unchanged** — `App.tsx` calls `onAnalyze(selected)` where `selected` is still a `string`. No change to `App.tsx` required.
