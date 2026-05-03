# Force Graph Gap Exploration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the non-interactive "+N uncovered" summary node in the force graph with a Covered/Gaps toggle strip, so users can explore and click individual uncovered techniques just like in the heatmap.

**Architecture:** All changes live inside the `ForceGraph` function component in `MITREHeatmap.tsx`. A new `graphView` state switches between rendering covered vs uncovered technique nodes. A bottom toolbar strip (HTML below the SVG) provides the toggle UI. No parent component, backend, or type changes needed.

**Tech Stack:** React + TypeScript, SVG for graph, CSS in `App.css`

---

## File Map

| File | Change |
|---|---|
| `frontend/src/components/MITREHeatmap.tsx` | 3 edits: state + derived values (lines 151–204), handleTacticClick (lines 206–210), SVG node rendering (lines 242–343) + new toolbar HTML |
| `frontend/src/App.css` | Add 6 CSS classes for the toolbar strip after line 862 |

---

### Task 1: Add `graphView` state and update derived values

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:151-204`

This task adds the new state variable, resets it correctly on tactic changes, and replaces the old `uncoveredCount`/`nodeCount` derivation with `uncoveredTechs`/`displayTechs`/`nodeCount`.

- [ ] **Step 1: Add `graphView` state after `expandedTactic` on line 151**

In `MITREHeatmap.tsx`, find:
```tsx
  const [expandedTactic, setExpandedTactic] = useState<string | null>(null);
```
Replace with:
```tsx
  const [expandedTactic, setExpandedTactic] = useState<string | null>(null);
  const [graphView, setGraphView] = useState<'covered' | 'gaps'>('covered');
```

- [ ] **Step 2: Reset `graphView` in the existing `useEffect` for technique changes (lines 163–167)**

Find:
```tsx
  useEffect(() => {
    // Reset expanded tactic when the technique dataset changes (e.g. client switch)
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setExpandedTactic(null);
  }, [techniques]);
```
Replace with:
```tsx
  useEffect(() => {
    // Reset expanded tactic when the technique dataset changes (e.g. client switch)
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setExpandedTactic(null);
    setGraphView('covered');
  }, [techniques]);
```

- [ ] **Step 3: Update derived values block (lines 191–204)**

Find:
```tsx
  const expandedTechs  = expandedTactic ? (tacticMap[expandedTactic] ?? []) : [];
  const coveredTechs   = expandedTechs.filter(t => t.score > 0);
  const uncoveredCount = expandedTechs.length - coveredTechs.length;
  const nodeCount      = coveredTechs.length + (uncoveredCount > 0 ? 1 : 0);
  const expandedCenter = expandedTactic ? gridPos[expandedTactic] : null;
  const techPos = expandedCenter
    ? techniqueRadialPositions(
        expandedCenter.cx,
        expandedCenter.cy,
        nodeCount,
        dims.width,
        dims.height,
      )
    : [];
```
Replace with:
```tsx
  const expandedTechs   = expandedTactic ? (tacticMap[expandedTactic] ?? []) : [];
  const coveredTechs    = expandedTechs.filter(t => t.score > 0);
  const uncoveredTechs  = expandedTechs.filter(t => t.score === 0);
  const displayTechs    = graphView === 'covered' ? coveredTechs : uncoveredTechs;
  const nodeCount       = displayTechs.length;
  const expandedCenter  = expandedTactic ? gridPos[expandedTactic] : null;
  const techPos = expandedCenter
    ? techniqueRadialPositions(
        expandedCenter.cx,
        expandedCenter.cy,
        nodeCount,
        dims.width,
        dims.height,
      )
    : [];
```

- [ ] **Step 4: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -30
```
Expected: no errors mentioning `graphView`, `uncoveredTechs`, or `displayTechs`.

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "feat(force-graph): add graphView state and displayTechs derivation"
```

---

### Task 2: Update `handleTacticClick` to set correct view on expand

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:206-210`

When expanding a new tactic that has no covered techniques, we auto-switch to gaps view so the user isn't shown an empty ring.

- [ ] **Step 1: Replace `handleTacticClick` (lines 206–210)**

Find:
```tsx
  const handleTacticClick = (tactic: string) => {
    if ((tacticMap[tactic]?.length ?? 0) === 0) return;
    setExpandedTactic((prev) => (prev === tactic ? null : tactic));
    onSelectTechnique(null);
  };
```
Replace with:
```tsx
  const handleTacticClick = (tactic: string) => {
    if ((tacticMap[tactic]?.length ?? 0) === 0) return;
    const isCollapsing = expandedTactic === tactic;
    setExpandedTactic((prev) => (prev === tactic ? null : tactic));
    onSelectTechnique(null);
    if (isCollapsing) {
      setGraphView('covered');
    } else {
      const hasCovered = (tacticMap[tactic] ?? []).some(t => t.score > 0);
      setGraphView(hasCovered ? 'covered' : 'gaps');
    }
  };
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -30
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "feat(force-graph): auto-switch to gaps view when tactic has no covered techniques"
```

---

### Task 3: Update SVG node rendering — remove summary node, use `displayTechs`

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:242-343`

This removes the old static summary node and its edge, and makes both the edge list and node list render `displayTechs` (either covered or uncovered depending on `graphView`). The edge and node stroke colours adapt to the view.

- [ ] **Step 1: Replace covered-tech edges block (lines 242–255)**

Find:
```tsx
        {/* Edges: expanded tactic → covered techniques + optional summary node */}
        {expandedCenter && coveredTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={`edge:${t.techniqueID}:${t.tactic}`}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(0,255,100,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}
        {expandedCenter && uncoveredCount > 0 && (() => {
          const pos = techPos[coveredTechs.length];
          if (!pos) return null;
          return (
            <line
              key="edge:uncovered-summary"
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(128,128,128,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })()}
```
Replace with:
```tsx
        {/* Edges: expanded tactic → technique nodes (covered or gap depending on view) */}
        {expandedCenter && displayTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={`edge:${t.techniqueID}:${t.tactic}`}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke={graphView === 'gaps' ? 'rgba(180,0,0,0.3)' : 'rgba(0,255,100,0.3)'}
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}
```

- [ ] **Step 2: Replace covered technique nodes + summary node blocks (lines 272–343)**

Find:
```tsx
        {/* Technique nodes — covered techniques for the expanded tactic */}
        {coveredTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          const nodeId = `tech:${t.techniqueID}:${t.tactic}`;
          const isSelected = selectedId === nodeId;
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
        {/* Summary node for uncovered techniques */}
        {expandedCenter && uncoveredCount > 0 && (() => {
          const pos = techPos[coveredTechs.length];
          if (!pos) return null;
          return (
            <g
              key="uncovered-summary"
              transform={`translate(${pos.cx},${pos.cy})`}
              style={{ pointerEvents: 'none' }}
              aria-label={`${uncoveredCount} uncovered techniques`}
            >
              <circle r={14} fill="var(--surface-2)" />
              <text
                dy="0.35em"
                textAnchor="middle"
                fontSize={7}
                fill="#fff"
                fontFamily="'IBM Plex Mono', monospace"
                fontWeight="600"
                style={{ userSelect: 'none' }}
              >
                +{uncoveredCount}
              </text>
              <text
                textAnchor="middle"
                y={22}
                fontSize={5.5}
                fill="rgba(255,255,255,0.6)"
                fontFamily="'IBM Plex Mono', monospace"
                style={{ userSelect: 'none' }}
              >
                uncovered
              </text>
            </g>
          );
        })()}
```
Replace with:
```tsx
        {/* Technique nodes — covered or gap techniques depending on graphView */}
        {displayTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          const nodeId = `tech:${t.techniqueID}:${t.tactic}`;
          const isSelected = selectedId === nodeId;
          const textFill = t.score <= 50 ? '#fff' : '#000';
          const unselectedStroke = graphView === 'gaps'
            ? 'rgba(255,80,80,0.5)'
            : 'rgba(0,255,100,0.5)';
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
                stroke={isSelected ? '#fff' : unselectedStroke}
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
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -30
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "feat(force-graph): replace summary node with interactive gap nodes using displayTechs"
```

---

### Task 4: Add bottom toolbar HTML and wire toggle

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:400-410` (the legend + closing div)

The toolbar strip goes between the SVG and the closing `</div>` of the `force-graph-container`. It renders only when `expandedTactic !== null`.

- [ ] **Step 1: Replace the legend + closing div block (lines 402–409)**

Find:
```tsx
      <div className="force-graph-legend">
        <span className="force-legend-item force-legend-item--tactic">Tactic</span>
        <span className="force-legend-item force-legend-item--covered">Covered</span>
        <span className="force-legend-item force-legend-item--partial">Partial</span>
        <span className="force-legend-item force-legend-item--uncovered">Uncovered</span>
      </div>
    </div>
```
Replace with:
```tsx
      <div className="force-graph-legend">
        <span className="force-legend-item force-legend-item--tactic">Tactic</span>
        <span className="force-legend-item force-legend-item--covered">Covered</span>
        <span className="force-legend-item force-legend-item--partial">Partial</span>
        <span className="force-legend-item force-legend-item--uncovered">Uncovered</span>
      </div>

      {expandedTactic && (
        <div className="graph-tab-strip">
          <span className="graph-tab-strip__label">
            {TACTIC_LABELS[expandedTactic] ?? expandedTactic}
          </span>
          <button
            className={`graph-tab${graphView === 'covered' ? ' graph-tab--active graph-tab--covered' : ''}`}
            disabled={coveredTechs.length === 0}
            onClick={() => { setGraphView('covered'); onSelectTechnique(null); }}
          >
            Covered ({coveredTechs.length})
          </button>
          <button
            className={`graph-tab${graphView === 'gaps' ? ' graph-tab--active graph-tab--gaps' : ''}`}
            disabled={uncoveredTechs.length === 0}
            onClick={() => { setGraphView('gaps'); onSelectTechnique(null); }}
          >
            Gaps ({uncoveredTechs.length})
          </button>
        </div>
      )}
    </div>
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -30
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "feat(force-graph): add Covered/Gaps toggle toolbar strip"
```

---

### Task 5: Add CSS for the toolbar strip

**Files:**
- Modify: `frontend/src/App.css:862` (after `.force-graph-container` rule)

- [ ] **Step 1: Add CSS after the `.force-graph-container` rule (line 862)**

Find:
```css
.force-graph-container { flex: 1; overflow: hidden; position: relative; }
```
Replace with:
```css
.force-graph-container { flex: 1; overflow: hidden; position: relative; }

/* Force graph tab strip (Covered / Gaps toggle) */
.graph-tab-strip { display: flex; align-items: center; gap: 6px; padding: 6px 12px; border-top: 1px solid var(--border); background: var(--surface); }
.graph-tab-strip__label { font-family: var(--font-mono); font-size: 0.6rem; letter-spacing: 0.07em; text-transform: uppercase; color: var(--text-dim); margin-right: 4px; }
.graph-tab { font-family: var(--font-mono); font-size: 0.62rem; letter-spacing: 0.05em; padding: 3px 10px; border-radius: var(--radius-sm); border: 1px solid var(--border-bright); background: transparent; color: var(--text-sec); cursor: pointer; transition: background 0.12s, color 0.12s, border-color 0.12s; }
.graph-tab:hover:not(:disabled) { background: var(--surface-2); color: var(--text); }
.graph-tab:disabled { opacity: 0.35; cursor: default; }
.graph-tab--active.graph-tab--covered { background: rgba(0,255,100,0.15); border-color: #00ff64; color: #00ff64; font-weight: 600; }
.graph-tab--active.graph-tab--gaps { background: rgba(127,29,29,0.35); border-color: rgba(255,80,80,0.7); color: #f87171; font-weight: 600; }
```

- [ ] **Step 2: Build to verify no CSS/TS errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run build 2>&1 | tail -20
```
Expected: `built in X.Xs` with no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/App.css && git commit -m "feat(force-graph): add CSS for Covered/Gaps tab strip"
```

---

### Task 6: Visual verification

No code changes — confirm the feature works end-to-end in the browser.

- [ ] **Step 1: Start the dev server**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run dev
```

- [ ] **Step 2: Open the app and navigate to the MITRE view**

Switch to Graph mode. Click any tactic node (e.g. "Initial Access").

- [ ] **Step 3: Verify covered view**

- Covered technique nodes fan out (green circles)
- Bottom toolbar shows: `Initial Access  [ ● Covered (N) ]  [ Gaps (N) ]`
- "Covered" tab is active (green fill)
- Click a covered node → `TechniqueDetailPanel` opens showing existing alerts or suggestions

- [ ] **Step 4: Click "Gaps" tab**

- Covered nodes disappear, red nodes fan out (one per uncovered technique)
- "Gaps" tab is active (red fill)
- Edges are muted red
- Click a red node → `TechniqueDetailPanel` opens showing only `SuggestionsPanel` (no "Covered by" section)

- [ ] **Step 5: Verify edge cases**

- Click a different tactic while in Gaps view → new tactic expands in Covered view (or Gaps if it has no covered)
- Click same tactic again → collapses, toolbar disappears
- Click SVG background → tactic collapses, toolbar disappears

- [ ] **Step 6: Commit verification note (no code change needed)**

If visual verification passes, no commit required. If any visual issue found, fix and commit before marking complete.
