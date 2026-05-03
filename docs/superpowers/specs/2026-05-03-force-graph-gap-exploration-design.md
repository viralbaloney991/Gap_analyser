# Force Graph Gap Exploration Design

**Date:** 2026-05-03  
**Status:** Approved  
**Scope:** `frontend/src/components/MITREHeatmap.tsx` — `ForceGraph` component only

## Problem

The force graph's "+N uncovered" summary node is non-interactive (`pointerEvents: none`). Users cannot explore individual uncovered (gap) techniques or access the suggestions panel for them, even though the heatmap view provides this for every technique cell. This creates a parity gap between the two views.

## Solution

Replace the static summary node with a **bottom toolbar toggle** (Covered / Gaps) that appears whenever a tactic is expanded. Clicking "Gaps" replaces the covered technique ring with individual clickable uncovered technique nodes. Each gap node opens the existing `TechniqueDetailPanel`, which already renders `SuggestionsPanel` for score=0 techniques.

## Behaviour

### Toggle strip

- Rendered as HTML below the SVG (not inside it), alongside the existing `.force-graph-legend` bar
- Visible only when `expandedTactic !== null`
- Format: `[ TacticLabel ]  [ ● Covered (N) ]  [ Gaps (N) ]`
- Active tab: filled pill (green for covered, red for gaps)
- Inactive tab: ghost/outline pill
- "Gaps (0)" tab: present but disabled and dimmed when no uncovered techniques exist
- "Covered (0)" tab: present but disabled and dimmed when no covered techniques exist

### View switching

- Default view on tactic expand: `covered`, **except** when `coveredTechs.length === 0` — in that case auto-switch to `gaps` so the user isn't shown an empty ring
- Switching views clears the selected technique (closes detail panel)
- `graphView` resets to `'covered'` whenever `expandedTactic` changes (auto-switch to `gaps` applied again if needed)

### Gap nodes

- Same `<g>` + `<circle>` structure as covered nodes
- Fill: `#7f1d1d` (matches heatmap's uncovered red, reuses `coverageColor(0)`)
- Stroke: `rgba(255,80,80,0.5)`
- Text: white (`#fff`), technique ID only (same as covered nodes)
- Edges: `rgba(180,0,0,0.3)` (muted red)
- Fully interactive: `cursor: pointer`, `role="button"`, `pointerEvents` enabled
- Clicking opens `TechniqueDetailPanel` — score=0 so only `SuggestionsPanel` renders

### Covered view

Identical to current behaviour. The old `+N uncovered` summary node and its edge are removed.

## State changes (ForceGraph only)

```ts
// New state
const [graphView, setGraphView] = useState<'covered' | 'gaps'>('covered');

// Reset on tactic change
useEffect(() => {
  setExpandedTactic(null);
  setGraphView('covered');         // ← add this reset
}, [techniques]);

// handleTacticClick resets graphView to 'covered' on expand
```

### Updated derived values

```ts
const expandedTechs  = expandedTactic ? (tacticMap[expandedTactic] ?? []) : [];
const coveredTechs   = expandedTechs.filter(t => t.score > 0);
const uncoveredTechs = expandedTechs.filter(t => t.score === 0);   // replaces uncoveredCount
const displayTechs   = graphView === 'covered' ? coveredTechs : uncoveredTechs;
const nodeCount      = displayTechs.length;
// techPos is already computed from nodeCount — no change needed
```

The radial layout (`techniqueRadialPositions`) already adapts to `nodeCount`, so gap nodes fan out correctly.

## Edge cases

| Scenario | Behaviour |
|---|---|
| All techniques uncovered (covered=0) | Covered tab disabled; `graphView` auto-set to `'gaps'` on expand |
| All techniques covered (gaps=0) | Gaps tab shows "(0)", disabled |
| Tactic collapses | Strip disappears, `graphView` → `'covered'`, detail panel closes |
| Different tactic clicked while in gaps view | New tactic opens in covered view |
| Gap node clicked | Detail panel opens; score=0 renders suggestions only (no "Covered by") |
| Selected gap node's tactic collapses | Detail panel closes (existing `onSelectTechnique(null)` in `handleTacticClick`) |

## Files changed

| File | Change |
|---|---|
| `frontend/src/components/MITREHeatmap.tsx` | `ForceGraph`: add `graphView` state, remove summary node, add gap node rendering, add toolbar strip |
| `frontend/src/index.css` (or equivalent) | Add `.graph-tab-strip`, `.graph-tab`, `.graph-tab--active`, `.graph-tab--disabled` classes |

## What does NOT change

- `MITREHeatmap` (parent component)
- `TechniqueDetailPanel` — already handles score=0 correctly
- `SuggestionsPanel` — unchanged
- Heatmap view — unchanged
- Backend — unchanged
