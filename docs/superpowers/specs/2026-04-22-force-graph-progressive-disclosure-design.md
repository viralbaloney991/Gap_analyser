# Force Graph — Progressive Disclosure Redesign

**Date:** 2026-04-22
**Status:** Approved
**Topic:** Replace the unusable all-nodes-at-once force graph with a click-to-expand progressive disclosure layout

---

## Problem Statement

The current Force Graph view renders all MITRE ATT&CK techniques simultaneously — roughly 500 nodes. The D3 force simulation cannot meaningfully separate this many nodes in a fixed canvas, resulting in walls of tiny (~10px radius, 5.5px font) unreadable, unclickable circles that pile up at the viewport edges. The view is completely unusable.

---

## Goals

- Show all 14 tactics at a glance in a readable default state
- Reveal techniques for a single tactic on demand (click to expand)
- Technique nodes large enough to read and click reliably
- Preserve the existing detail panel + suggestions flow unchanged
- Remove the D3 force simulation (it adds complexity without benefit at this scale)

## Non-Goals

- Zoom/pan support
- Showing multiple tactics expanded simultaneously
- Sub-technique nesting within the graph
- Changes to the Heatmap view
- Changes to `TechniqueDetailPanel` or `SuggestionsPanel`

---

## Architecture

### Component changes — `ForceGraph` only

All changes are confined to `frontend/src/components/MITREHeatmap.tsx` within the `ForceGraph` component and its helpers. No other files change.

**Removed:**

| Symbol | Reason |
|---|---|
| `interface ForceNode` | Replaced by simpler tactic/technique node types |
| `interface ForceEdge` | Replaced |
| `buildForceGraph()` | Replaced by pure position helpers |
| `useForceSimulation()` | D3 simulation not needed |
| D3 import (if unused after change) | Remove if no longer needed |

**New types:**

```ts
interface TacticNode {
  id: string;          // tactic slug, e.g. "initial-access"
  label: string;       // "Initial Access"
  cx: number;
  cy: number;
  covered: number;
  total: number;
  coveragePct: number;
}

interface TechNode {
  id: string;          // "tech:T1078:initial-access"
  techniqueID: string; // "T1078"
  tactic: string;
  cx: number;
  cy: number;
  color: string;
  score: number;
}
```

**New pure helpers:**

```ts
function tacticGridPositions(
  tactics: string[],
  width: number,
  height: number,
): Record<string, { cx: number; cy: number }>
```

Places 14 tactic nodes in a 7-column × 2-row grid, evenly spaced, vertically centred in the canvas.

```
colCount = 7
rowCount = ceil(tactics.length / colCount)   // = 2
colSpacing = width / (colCount + 1)
rowSpacing = height / (rowCount + 1)
node[i].cx = colSpacing * ((i % colCount) + 1)
node[i].cy = rowSpacing * (floor(i / colCount) + 1)
```

```ts
function techniqueRadialPositions(
  cx: number,
  cy: number,
  techniques: NavigatorTechnique[],
  radius: number,   // distance from parent tactic centre
): Array<{ cx: number; cy: number }>
```

Fans `n` technique nodes evenly around a circle of the given radius centred on `(cx, cy)`. Angle starts at `-π/2` (top) and increments by `2π/n`.

```
angle[i] = -π/2 + (i / n) * 2π
cx[i]    = parentCx + radius * cos(angle[i])
cy[i]    = parentCy + radius * sin(angle[i])
```

Radius is computed as `max(90, sqrt(n) * 30)` so larger tactic clusters spread further out.

---

## State Machine

`ForceGraph` holds one piece of state:

```ts
const [expandedTactic, setExpandedTactic] = useState<string | null>(null);
```

| Event | New state |
|---|---|
| Click tactic (collapsed) | `expandedTactic = tactic` |
| Click tactic (expanded) | `expandedTactic = null` |
| Click SVG background | `expandedTactic = null` |
| Click technique node | calls `onSelectTechnique(t)` — no tactic state change |

---

## Visual Design

**Tactic node** (always visible):
- Radius: `34`
- Fill: `rgba(0,255,100,0.08)` (expanded: `rgba(0,255,100,0.15)`)
- Stroke: `#00ff64`; stroke-width 1.5 (expanded: 2)
- Opacity: `1.0` (other tactics when one is expanded: `0.3`)
- Label line 1: tactic short name (font 8px, weight 600, `#00ff64`)
- Label line 2: `covered/total` (font 6.5px, colour = `coverageColor(pct)`)

**Technique node** (only for expanded tactic):
- Radius: `16`
- Fill: `t.color`
- Stroke: `rgba(0,255,100,0.5)`; selected: `#fff`, stroke-width 2
- Label: `t.techniqueID` (font 7.5px, fill `#fff` when red/uncovered, `#000` when green/yellow)
- Cursor: `pointer`

**Edge** (tactic → technique):
- `stroke="rgba(0,255,100,0.3)"`, `strokeWidth=1`

**Canvas height:** remains `540px` (set in `.force-graph-container` CSS — no change).

**Animation:** CSS `transition: opacity 0.2s ease` on SVG `<g>` elements via the `force-node` class already in App.css.

---

## Interaction Details

**Tactic node click:**
```
if expandedTactic === tactic.id → setExpandedTactic(null)
else → setExpandedTactic(tactic.id); setSelectedTechnique(null)
```
Deselects any open technique panel when switching tactics.

**Technique node click:**
```
onSelectTechnique(selectedId === node.id ? null : technique)
```
Identical to current behaviour.

**SVG background click:**
```
onClick={() => setExpandedTactic(null)}
```
On the root `<svg>` element.

**Overflow handling:** If a tactic's technique nodes would extend beyond canvas bounds, the radial radius is clamped so nodes stay within `padding=20px` of the SVG edges. Clamp is applied in `techniqueRadialPositions` using the tactic centre coordinates and the canvas `width`/`height`.

---

## Error Handling

- Tactics with 0 techniques: tactic node is still shown; click does nothing (no expansion)
- `techniques` prop empty: render empty SVG with a "No data" text node
- `TACTICS_ORDER` entry has no data in the layer: tactic node shows `0/0`, greyed out

---

## Files Changed

| File | Change |
|---|---|
| `frontend/src/components/MITREHeatmap.tsx` | Replace `ForceNode`, `ForceEdge`, `buildForceGraph`, `useForceSimulation` with `TacticNode`, `TechNode`, `tacticGridPositions`, `techniqueRadialPositions`; rewrite `ForceGraph` render body |

No other files change.

---

## Testing

Visual / manual:
- Default state shows 14 tactic nodes in 2 rows, all labelled with coverage ratios
- Clicking a tactic expands techniques radially, other tactics dim
- Clicking expanded tactic collapses it
- Clicking a technique opens the detail panel
- Clicking SVG background collapses expansion
- Switching from heatmap → graph → back → graph preserves no expanded state
- Large tactic (Defense Evasion, 42 techniques) fans out without overlap at canvas boundaries

No automated tests: this is a pure rendering change with no business logic. The existing `MITREHeatmap` test suite (if any) should continue to pass.
