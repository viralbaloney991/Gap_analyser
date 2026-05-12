# Design: Threat Graph Interactivity, Noise Calculation, Advanced Use Cases → Build Detection

**Date:** 2026-05-12  
**Status:** Approved

---

## 1. Threat Graph Interactivity Fixes

### Problems

1. **Trackpad pan captured as zoom** — the wheel handler applies zoom on all wheel events. Trackpad two-finger scroll (which emits `wheel` without `ctrlKey`) should pan, not zoom.
2. **Hover doesn't reliably highlight connected nodes** — SVG node hit areas are small, and fast mouse movement drops `hoveredId` before the connected highlight renders. The visual result is that edges and linked nodes appear un-highlighted even while the cursor is near a node.

### Solution

**Wheel handler** (`useViewport`): Distinguish pinch-to-zoom from two-finger scroll.
- If `e.ctrlKey === true` → zoom (current behaviour, keep as-is)
- Otherwise → translate (pan) using `e.deltaX` / `e.deltaY` directly, with the same clamping logic already in place

**Hover reliability** — two changes:
1. Add an invisible `<rect>` hit-area overlay (padding ~12px around each node circle/rect) with `pointerEvents="all"` and `fill="transparent"`. The visible node itself sets `pointerEvents="none"`. Larger hit target means the cursor doesn't have to land pixel-perfect.
2. On `onMouseLeave`, delay clearing `hoveredId` by ~80 ms using a `clearTimeout` / `setTimeout` ref. If the cursor enters another node before the timeout fires, cancel the clear. This prevents flicker during fast movement between adjacent nodes.

**Files changed:** `frontend/src/components/ThreatGraph.tsx` only (CSS may get a `cursor: grab` tweak).

### Out of scope
- Touch events (mobile) — separate concern, not requested
- D3-zoom migration

---

## 2. Noise Calculation — Real Signal

### Problem

`noisePct` in `ThreatGraph.tsx:buildAlertRules()` is entirely fabricated via `deterministicN()` — a hash of the integration name. It is shown in the drill panel as "Noise share" and used to decide the fake noise range. The backend already returns the real data.

### Real noise model

| Type | Definition | Backend field |
|---|---|---|
| Behavioral | Alert fired **> 10 times** in the selected lookback window | `noise_alerts[].trigger_count` |
| Structural | Query too broad / global (not scoped by design) | `noise_alerts[].noise_type === 'structural'` |

### Solution

Remove `noisePct` and `deterministicN` from `buildAlertRules()`. Replace with a proper join:

```
noiseMap: Map<name → { trigger_count, noise_type }>
```

Built from `data.alert_insights.noise_alerts`. Each `AlertRule` gains two fields:
- `noiseType: 'behavioral' | 'structural' | 'both' | null`
- `triggerCount: number` (0 if not in noise_alerts)

**Drill panel** replaces the "Noise share: X%" stat with:
- If `noiseType === null`: "No noise signal"
- If behavioral: "Fired {triggerCount}× in {lookbackDays}d · Behavioral"
- If structural: "Query too broad · Structural"
- If both: "Fired {triggerCount}× · Behavioral + Structural"

Node visual: nodes in `noise_alerts` keep a distinct accent (existing red tint) — no change to the visual encoding, only the data source.

**Files changed:** `frontend/src/components/ThreatGraph.tsx` only. No backend changes.

---

## 3. Advanced Use Cases → Build Detection Link

### Problem

Gap items under "Advanced Use Cases" in the Gaps tab (`AlertInsights`) have prose, a query skeleton, and a "Suggest correlations" button — but no path to the Detection Builder. The user has to navigate there manually with no context.

### Flow

1. Each Advanced Use Cases card gets a **"Build Detection →"** button (alongside "Suggest correlations").
2. On click: call `POST /api/map-tactics` with `{ prose, log_source, client }`.
3. Show an inline loading state on the button (spinner, disabled).
4. Backend LLM returns `{ tactic_ids: string[], technique_ids: string[] }`.
5. `App.tsx` navigates to the `DetectionBuilder` view, passing `preselectedTactics` and `preselectedTechniques` as initial state.
6. `DetectionBuilder` mounts with those tactics/techniques already in the basket. User clicks Generate.

### Backend — new endpoint

`POST /api/map-tactics`

Request:
```json
{ "prose": "...", "log_source": "...", "client": "..." }
```

Response:
```json
{ "tactic_ids": ["initial-access", "execution"], "technique_ids": ["T1566.001", "T1059.001"] }
```

LLM prompt: given the gap description and log source, identify the most relevant MITRE ATT&CK tactic IDs and technique IDs from the standard catalog. Return JSON only. Max 3 tactics, max 5 techniques.

Fallback: if LLM returns unparseable JSON or empty, return `{ "tactic_ids": [], "technique_ids": [] }` — builder opens empty (user can select manually).

### Frontend changes

**`AlertInsights.tsx`:**
- Add `onBuildDetection: (tacticIds: string[], techniqueIds: string[]) => void` prop
- Add `fetchMapTactics(prose, logSource, client): Promise<{tactic_ids, technique_ids}>` to `services/api.ts`
- "Build Detection →" button on each Advanced Use Cases card; loading spinner while calling `/api/map-tactics`

**`App.tsx`:**
- `onBuildDetection` handler: stores `preselectedTactics` + `preselectedTechniques` in state, then calls `navigate('builder')`
- Pass those as props to `DetectionBuilder`

**`DetectionBuilder.tsx`:**
- Accept optional `preselectedTactics?: string[]` and `preselectedTechniques?: string[]` props
- On mount, if provided, add matching techniques to the basket automatically

### Error handling
- Network error on `/api/map-tactics`: show "Couldn't map tactics — builder will open empty" toast, navigate anyway
- LLM parse failure: same fallback (open builder empty)

### Out of scope
- Saving the originating gap item context into the detection
- Reverse navigation back to the specific gap item

---

## Summary of changes

| Area | Files | Backend? |
|---|---|---|
| Threat Graph hover + trackpad | `ThreatGraph.tsx` | No |
| Noise real signal | `ThreatGraph.tsx` | No |
| Build Detection link | `AlertInsights.tsx`, `App.tsx`, `DetectionBuilder.tsx`, `services/api.ts` | Yes — new `/api/map-tactics` endpoint |
