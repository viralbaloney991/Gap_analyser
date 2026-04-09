# World Map Client Selector — Design Spec

**Date:** 2026-04-09
**Scope:** Replace `ClientSelector` dropdown with an interactive world map; clients appear as pulsing dots positioned by their Coralogix region.
**Status:** Design approved, ready for implementation

---

## 1. Overview

The landing page replaces the inline form (dropdown + button) with a full-viewport SVG world map. Each client is a pulsing green dot placed at the geographic centre of their Coralogix region. Hovering a dot shows a tooltip with the client name. Clicking a dot selects it and reveals the `ANALYZE →` button. The rest of the terminal aesthetic (corner marks, grid background, wordmark, pulsing status dot) is preserved unchanged.

---

## 2. Map Rendering

**Library:** `react-simple-maps` + `world-atlas` (npm packages, bundled — no CDN calls).

`react-simple-maps` wraps D3-geo and renders Natural Earth 110m TopoJSON as React SVG components. It handles geographic projection, country outlines, and marker placement natively.

**Projection:** `Equal Earth` (`geoEqualEarth`) — area-accurate, visually balanced, no Mercator distortion at poles. Scale `160`.

**Geography data:** `world-atlas/countries-110m.json` imported directly into the bundle.

---

## 3. Region → Coordinates

Static lookup table in frontend (`REGION_COORDS`). Uses `[longitude, latitude]` (GeoJSON convention, as required by react-simple-maps).

| Region | Location | Coordinates |
|--------|----------|-------------|
| `eu1`  | Ireland (Dublin) | `[-8.2, 53.3]` |
| `eu2`  | Europe 2 (Stockholm) | `[18.1, 59.3]` |
| `us1`  | US East (Virginia) | `[-77.5, 37.8]` |
| `us2`  | US West (Oregon) | `[-122.8, 45.5]` |
| `ap1`  | India (Mumbai) | `[72.9, 19.1]` |
| `ap2`  | Singapore | `[103.8, 1.3]` |
| `ap3`  | Asia Pacific 3 (Tokyo) | `[139.7, 35.7]` |

If a client's region is not in the table, the dot is omitted and the client falls back to a compact list below the map (edge case, not current issue).

---

## 4. Backend Change — `/api/clients`

Currently returns `[]string`. Changed to return `[]ClientInfo`:

```json
[{"name": "Deel", "region": "eu1"}]
```

**Go:** Add `ClientInfo` struct in `handlers.go`. Add `ClientsWithRegion()` method to `config.Config`. `HandleClients` writes `ClientsWithRegion()` instead of `ClientNames()`.

This is a breaking API change — `api.ts` `fetchClients()` is the only consumer, and it is updated in the same task.

---

## 5. Component Design — `ClientSelector.tsx`

### 5.1 Layout (preserved from current design)

```
[full viewport, --bg background]
  [dot-grid overlay, fades toward bottom]
  [corner mark top-left: "CX_ALERTS v2.1"]
  [corner mark top-right: [status-dot] ONLINE]
  [ComposableMap — fills most of viewport]
    [Geographies — faint green country fills + borders]
    [Marker per client — pulsing dot, hover tooltip, click selects]
  [bottom-left content block]
    [selected client name (or "Select a client" if none)]
    [wordmark: "Alert Analyzer"]
    [subtitle: "CORALOGIX INTEGRATION INTELLIGENCE"]
    [ANALYZE → button — only visible when a client is selected]
```

### 5.2 Client Dot

Each dot is a `<Marker>` containing:
- `<circle r={5}>` — solid `#00ff64`
- `<circle r={5}>` animated ring — opacity + scale pulse (CSS animation class `map-ring-pulse`)
- `onMouseEnter` / `onMouseLeave` → controls tooltip state
- `onClick` → sets `selected` state

**Selected state:** ring stops pulsing, `r` increases to 7, `fill` shifts to white, outer ring becomes static with 1.5× scale.

### 5.3 Tooltip

Positioned via `clientX/Y` from the mouse event (not relative to SVG — avoids projection math). Renders in a `<div>` portalled to `document.body` so it never clips inside the SVG viewport.

Content: `{clientName} · {region.toUpperCase()}`

### 5.4 State

```ts
const [selected, setSelected] = useState<string>('');  // client name
const [tooltip, setTooltip] = useState<{ name: string; region: string; x: number; y: number } | null>(null);
```

`onAnalyze(selected)` called when the button is submitted. No form element needed — single button click.

---

## 6. Styling

New CSS classes in `App.css`:

```css
.world-map          /* ComposableMap wrapper: fills viewport, cursor crosshair */
.map-geography      /* country fill: rgba(0,255,100,0.06), stroke rgba(0,255,100,0.22) */
.map-dot            /* base dot circle */
.map-dot-ring       /* animated ring circle — class map-ring-pulse keyframe */
.map-dot--selected  /* selected state override */
.map-tooltip        /* absolute tooltip div, z-index 100 */
```

`@keyframes map-ring-pulse`: opacity 0.8→0 + scale 1→1.8 over 2s ease-in-out.

Existing `.client-selector`, `.client-selector-grid`, `.client-selector-corner`, `.landing-wordmark`, `.landing-subtitle`, `.landing-form`, `.status-dot` classes are **removed** from App.css (replaced by the map layout). The grid background and corner marks are rebuilt inside the new component.

---

## 7. Files Changed

| File | Change |
|------|--------|
| `frontend/package.json` | Add `react-simple-maps`, `world-atlas` |
| `frontend/src/types/index.ts` | Add `ClientInfo { name: string; region: string }` |
| `frontend/src/services/api.ts` | `fetchClients()` → `Promise<ClientInfo[]>` |
| `frontend/src/components/ClientSelector.tsx` | Full rewrite — world map layout |
| `frontend/src/App.css` | Remove old ClientSelector classes; add map classes |
| `backend/internal/config/config.go` | Add `ClientsWithRegion()` method |
| `backend/internal/api/handlers.go` | `HandleClients` uses `ClientsWithRegion()` |

`App.tsx` is **unchanged** — it calls `onAnalyze(client)` through the same prop interface.

---

## 8. Out of Scope

- Zoom / pan interactions on the map
- Multiple client selection
- Fallback list UI for unknown regions (no current clients have unknown regions)
- Mobile responsiveness changes
