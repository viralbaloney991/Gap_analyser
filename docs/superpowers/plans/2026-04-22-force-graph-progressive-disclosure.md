# Force Graph Progressive Disclosure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the unusable all-nodes-at-once force graph with a progressive disclosure layout: 14 tactic nodes by default, click a tactic to expand its techniques radially.

**Architecture:** Delete the D3 force simulation and replace with two pure position functions (`tacticGridPositions`, `techniqueRadialPositions`). The `ForceGraph` component gains a single `expandedTactic` state — clicking a tactic fans its techniques out around it; clicking again collapses. All changes are confined to `MITREHeatmap.tsx`; no other files change.

**Tech Stack:** React 19, TypeScript, SVG (no D3 after this change)

---

## File map

| File | What changes |
|---|---|
| `frontend/src/components/MITREHeatmap.tsx` | Remove D3 simulation code; add position helpers; rewrite `ForceGraph` body |

No other files change.

---

### Task 1: Replace simulation code with pure position helpers

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:1-211`

This task removes the D3 import, unused React hooks (`useCallback`, `useMemo`), and all simulation types/functions (`ForceNode`, `ForceEdge`, `buildForceGraph`, `useForceSimulation`). It adds two pure helper functions in their place.

- [ ] **Step 1: Update the import line (line 9) and remove D3 import (line 10)**

Replace lines 1–13 of `frontend/src/components/MITREHeatmap.tsx` with:

```tsx
/**
 * MITREHeatmap — MITRE ATT&CK coverage view.
 *
 * Layout: two modes toggled by the user.
 *   1. Heatmap grid  — the original tactic-column layout (default).
 *   2. Force graph   — progressive disclosure: 14 tactic nodes, click to expand techniques.
 */

import { useState, useRef, useEffect } from 'react';
import type { MITRECoverageResult, NavigatorTechnique, SuggestionsResponse } from '../types';
import { fetchSuggestions } from '../services/api';

interface Props {
  data: MITRECoverageResult;
  clientName: string;
}
```

- [ ] **Step 2: Delete the entire simulation block (lines 85–211)**

Delete from the comment `// ---------------------------------------------------------------------------` before `// Minimal force-directed simulation` all the way through the closing `}` of `useForceSimulation`. This removes:
- `interface ForceNode { ... }`
- `interface ForceEdge { ... }`
- `function buildForceGraph(...) { ... }`
- `function useForceSimulation(...) { ... }`

After deletion, line 85 should now be the `// ---------------------------------------------------------------------------` comment that precedes `// Force Graph component`.

- [ ] **Step 3: Insert the two pure position helpers before `// Force Graph component`**

Add the following block immediately before the `// ---------------------------------------------------------------------------\n// Force Graph component` comment:

```tsx
// ---------------------------------------------------------------------------
// Position helpers (pure — no simulation)
// ---------------------------------------------------------------------------

/**
 * Places `tactics` in a 7-column × 2-row grid, evenly spaced.
 * Returns a map of tactic slug → { cx, cy } centre coordinates.
 */
function tacticGridPositions(
  tactics: string[],
  width: number,
  height: number,
): Record<string, { cx: number; cy: number }> {
  const colCount = 7;
  const rowCount = Math.ceil(tactics.length / colCount);
  const colSpacing = width / (colCount + 1);
  const rowSpacing = height / (rowCount + 1);
  const result: Record<string, { cx: number; cy: number }> = {};
  tactics.forEach((tactic, i) => {
    result[tactic] = {
      cx: colSpacing * ((i % colCount) + 1),
      cy: rowSpacing * (Math.floor(i / colCount) + 1),
    };
  });
  return result;
}

/**
 * Fans `count` technique nodes evenly around a circle centred on (cx, cy).
 * Radius = max(90, sqrt(count) * 30). Clamps nodes to stay within canvas bounds.
 */
function techniqueRadialPositions(
  cx: number,
  cy: number,
  count: number,
  canvasWidth: number,
  canvasHeight: number,
): Array<{ cx: number; cy: number }> {
  if (count === 0) return [];
  const pad = 20;
  const nodeR = 16;
  const radius = Math.max(90, Math.sqrt(count) * 30);
  return Array.from({ length: count }, (_, i) => {
    const angle = -Math.PI / 2 + (i / count) * 2 * Math.PI;
    return {
      cx: Math.max(pad + nodeR, Math.min(canvasWidth  - pad - nodeR, cx + radius * Math.cos(angle))),
      cy: Math.max(pad + nodeR, Math.min(canvasHeight - pad - nodeR, cy + radius * Math.sin(angle))),
    };
  });
}
```

- [ ] **Step 4: Replace the existing `ForceGraph` function with a compile stub**

Delete everything from `function ForceGraph({` through its closing `}` and replace with this temporary stub so the file compiles cleanly:

```tsx
function ForceGraph({
  techniques: _techniques,
  onSelectTechnique: _onSelectTechnique,
  selectedId: _selectedId,
}: {
  techniques: NavigatorTechnique[];
  onSelectTechnique: (t: NavigatorTechnique | null) => void;
  selectedId: string | null;
}) {
  return (
    <div className="force-graph-container">
      <svg width={800} height={540} className="force-graph-svg">
        <text
          x={400} y={270}
          textAnchor="middle"
          fill="#00ff6466"
          fontSize={14}
          fontFamily="'IBM Plex Mono', monospace"
        >
          Loading graph...
        </text>
      </svg>
    </div>
  );
}
```

- [ ] **Step 5: Verify the build is clean**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: `✓ built in` with zero TypeScript errors. The force graph tab will show "Loading graph..." — that is expected and fixed in Task 2.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/MITREHeatmap.tsx
git commit -m "refactor(graph): remove D3 simulation, add tacticGridPositions and techniqueRadialPositions"
```

---

### Task 2: Rewrite the ForceGraph component

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx` — the `ForceGraph` function (currently lines ~217–341, now shifted due to Task 1 deletions)

This task replaces the compile stub from Task 1 with the full progressive disclosure implementation. The function signature is unchanged.

- [ ] **Step 1: Replace the entire `ForceGraph` function**

Delete everything from `function ForceGraph({` through its closing `}` (inclusive) and replace with:

```tsx
function ForceGraph({
  techniques,
  onSelectTechnique,
  selectedId,
}: {
  techniques: NavigatorTechnique[];
  onSelectTechnique: (t: NavigatorTechnique | null) => void;
  selectedId: string | null;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ width: 800, height: 540 });
  const [expandedTactic, setExpandedTactic] = useState<string | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;
    const obs = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDims({ width: Math.max(400, width), height: Math.max(300, height) });
    });
    obs.observe(containerRef.current);
    return () => obs.disconnect();
  }, []);

  // Group techniques by tactic
  const tacticMap: Record<string, NavigatorTechnique[]> = {};
  for (const t of techniques) {
    if (!t.tactic) continue;
    if (!tacticMap[t.tactic]) tacticMap[t.tactic] = [];
    tacticMap[t.tactic].push(t);
  }

  const activeTactics = TACTICS_ORDER.filter((tac) => tacticMap[tac] !== undefined);
  const gridPos = tacticGridPositions(activeTactics, dims.width, dims.height);

  // Positions for the currently expanded tactic's techniques
  const expandedTechs = expandedTactic ? (tacticMap[expandedTactic] ?? []) : [];
  const expandedCenter = expandedTactic ? gridPos[expandedTactic] : null;
  const techPos = expandedCenter
    ? techniqueRadialPositions(
        expandedCenter.cx,
        expandedCenter.cy,
        expandedTechs.length,
        dims.width,
        dims.height,
      )
    : [];

  const handleTacticClick = (tactic: string) => {
    if ((tacticMap[tactic]?.length ?? 0) === 0) return;
    setExpandedTactic((prev) => (prev === tactic ? null : tactic));
    onSelectTechnique(null);
  };

  if (techniques.length === 0) {
    return (
      <div ref={containerRef} className="force-graph-container">
        <svg width={dims.width} height={dims.height} className="force-graph-svg">
          <text
            x={dims.width / 2}
            y={dims.height / 2}
            textAnchor="middle"
            fill="#00ff6466"
            fontSize={14}
            fontFamily="'IBM Plex Mono', monospace"
          >
            No technique data
          </text>
        </svg>
      </div>
    );
  }

  return (
    <div ref={containerRef} className="force-graph-container">
      <svg
        width={dims.width}
        height={dims.height}
        className="force-graph-svg"
        role="img"
        aria-label="MITRE technique force graph"
        onClick={() => setExpandedTactic(null)}
      >
        {/* Edges: expanded tactic → its techniques */}
        {expandedCenter && expandedTechs.map((_, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={i}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(0,255,100,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}

        {/* Technique nodes — only for the expanded tactic */}
        {expandedTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          const nodeId = `tech:${t.techniqueID}:${t.tactic}`;
          const isSelected = selectedId === nodeId;
          // White text on red/orange (score ≤ 50), black on yellow/green
          const textFill = t.score <= 50 ? '#fff' : '#000';
          return (
            <g
              key={nodeId}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); onSelectTechnique(isSelected ? null : t); }}
              style={{ cursor: 'pointer' }}
              className={`force-node${isSelected ? ' force-node--selected' : ''}`}
              role="button"
              aria-label={t.techniqueID}
            >
              <circle
                r={16}
                fill={t.color}
                stroke={isSelected ? '#fff' : 'rgba(0,255,100,0.5)'}
                strokeWidth={isSelected ? 2 : 1.5}
              />
              <text
                dy="0.35em"
                textAnchor="middle"
                fontSize={7.5}
                fill={textFill}
                fontFamily="'IBM Plex Mono', monospace"
                fontWeight="600"
                style={{ pointerEvents: 'none', userSelect: 'none' }}
              >
                {t.techniqueID}
              </text>
            </g>
          );
        })}

        {/* Tactic nodes — always visible, dimmed when another is expanded */}
        {activeTactics.map((tactic) => {
          const pos = gridPos[tactic];
          if (!pos) return null;
          const techs  = tacticMap[tactic] ?? [];
          const covered = techs.filter((t) => t.score > 0).length;
          const total   = techs.length;
          const pct     = total > 0 ? (covered / total) * 100 : 0;
          const isExpanded = expandedTactic === tactic;
          const isDimmed   = expandedTactic !== null && !isExpanded;
          const label  = TACTIC_LABELS[tactic] ?? tactic;
          const words  = label.split(' ');
          const lineH  = 10;
          // Vertically centre the word stack, then add coverage line below
          const startDy = -(words.length - 1) * lineH / 2;

          return (
            <g
              key={tactic}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); handleTacticClick(tactic); }}
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
                transition: 'opacity 0.2s ease',
              }}
              className="force-node force-node--tactic"
              aria-label={`${label}: ${covered} of ${total} covered`}
            >
              <circle
                r={34}
                fill={isExpanded ? 'rgba(0,255,100,0.15)' : 'rgba(0,255,100,0.08)'}
                stroke="#00ff64"
                strokeWidth={isExpanded ? 2 : 1.5}
              />
              <text
                textAnchor="middle"
                fontFamily="'IBM Plex Mono', monospace"
                fontWeight="600"
                fontSize={8}
                fill="#00ff64"
                style={{ pointerEvents: 'none', userSelect: 'none' }}
              >
                {words.map((w, wi) => (
                  <tspan key={wi} x={0} dy={wi === 0 ? startDy : lineH}>
                    {w}
                  </tspan>
                ))}
                <tspan x={0} dy={lineH} fontSize={6.5} fill={coverageColor(pct)} fontWeight="400">
                  {covered}/{total}
                </tspan>
              </text>
            </g>
          );
        })}
      </svg>

      <div className="force-graph-legend">
        <span className="force-legend-item force-legend-item--tactic">Tactic</span>
        <span className="force-legend-item force-legend-item--covered">Covered</span>
        <span className="force-legend-item force-legend-item--partial">Partial</span>
        <span className="force-legend-item force-legend-item--uncovered">Uncovered</span>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify the TypeScript build is clean**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected output (exact wording varies):
```
✓ 8 modules transformed.
dist/index.html                   x.xx kB
dist/assets/index-[hash].css      x.xx kB
dist/assets/index-[hash].js       xxx.xx kB │ gzip: xx.xx kB
✓ built in x.xxs
```

Zero TypeScript errors. If you see `error TS`: the most likely cause is a stale import of `useCallback` or `useMemo` — remove them from the import line if present.

- [ ] **Step 3: Manual smoke test**

```bash
cd /path/to/project && bash dev.sh
```

Open `http://localhost:5173`, run an analysis for any client, click "MITRE ATT&CK Coverage" → "Force Graph".

Verify:
1. 14 tactic nodes visible in a 2-row grid, each showing a name and `covered/total` ratio
2. Click any tactic → techniques fan out around it, other tactics dim to 30%
3. Clicked techniques ID is readable (≥7px font, 16px radius)
4. Click expanded tactic again → collapses
5. Click a technique node → detail panel opens on the right
6. Click SVG background → expansion collapses
7. No console errors in browser devtools

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/MITREHeatmap.tsx
git commit -m "feat(graph): progressive disclosure — 14 tactics by default, click to expand techniques"
```
