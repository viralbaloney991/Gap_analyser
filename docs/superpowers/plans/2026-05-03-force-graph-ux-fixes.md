# Force Graph UX Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 10 UX issues in the MITRE ATT&CK force graph — accessibility, touch targets, keyboard navigation, tooltips, legend styling, and motion preferences.

**Architecture:** All changes are in `frontend/src/components/MITREHeatmap.tsx` (`ForceGraph` component) and `frontend/src/App.css`. No new components, no backend changes. The `.tech-tooltip` CSS class already exists (`position: fixed`) and can be reused directly inside `ForceGraph`.

**Tech Stack:** React + TypeScript, SVG, CSS (CSS custom properties / design tokens)

---

## File Map

| File | Changes |
|---|---|
| `frontend/src/components/MITREHeatmap.tsx` | Tasks 1–4: role, aria, type, font sizes, touch target, keyboard nav, tooltips |
| `frontend/src/App.css` | Task 5: legend CSS + prefers-reduced-motion |

---

### Task 1: SVG role, aria-label, type="button" — quick attribute fixes

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:247-248, 329-335, 377-390`

Fixes issues 3 (tactic `role`), 9 (`type="button"`), 10 (dynamic SVG `aria-label`).

- [ ] **Step 1: Make SVG `aria-label` dynamic (line 248)**

Find:
```tsx
        aria-label="MITRE technique force graph"
```
Replace with:
```tsx
        aria-label={expandedTactic
          ? `MITRE force graph — ${TACTIC_LABELS[expandedTactic] ?? expandedTactic} expanded, ${graphView === 'covered' ? coveredTechs.length + ' covered techniques' : uncoveredTechs.length + ' gap techniques'}`
          : 'MITRE ATT&CK technique force graph'}
```

- [ ] **Step 2: Add `role="button"` to tactic `<g>` elements (line 334)**

Find:
```tsx
              className="force-node force-node--tactic"
              aria-label={`${label}: ${covered} of ${total} covered`}
```
Replace with:
```tsx
              className="force-node force-node--tactic"
              role="button"
              aria-label={`${label}: ${covered} of ${total} covered`}
```

- [ ] **Step 3: Add `type="button"` and `aria-pressed` to tab strip buttons (lines 377–390)**

Find:
```tsx
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
```
Replace with:
```tsx
          <button
            type="button"
            aria-pressed={graphView === 'covered'}
            className={`graph-tab${graphView === 'covered' ? ' graph-tab--active graph-tab--covered' : ''}`}
            disabled={coveredTechs.length === 0}
            onClick={() => { setGraphView('covered'); onSelectTechnique(null); }}
          >
            Covered ({coveredTechs.length})
          </button>
          <button
            type="button"
            aria-pressed={graphView === 'gaps'}
            className={`graph-tab${graphView === 'gaps' ? ' graph-tab--active graph-tab--gaps' : ''}`}
            disabled={uncoveredTechs.length === 0}
            onClick={() => { setGraphView('gaps'); onSelectTechnique(null); }}
          >
            Gaps ({uncoveredTechs.length})
          </button>
```

- [ ] **Step 4: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -20
```
Expected: zero errors.

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "fix(force-graph): add role=button, dynamic aria-label, type=button, aria-pressed"
```

---

### Task 2: Font sizes + transparent touch-target circle

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:287-303, 343-358`

Fixes issues 1 (touch target too small) and 6 (SVG font sizes 6.5–8px too small).

- [ ] **Step 1: Add transparent hit-area circle to technique nodes (line 287)**

The `<g>` click area is bounded by its children. Adding a transparent `r=22` circle expands the clickable area to 44px diameter without affecting visuals.

Find:
```tsx
            >
              <circle
                r={16}
                fill={t.color}
                stroke={isSelected ? '#fff' : unselectedStroke}
                strokeWidth={isSelected ? 2 : 1.5}
              />
```
Replace with:
```tsx
            >
              <circle r={22} fill="transparent" />
              <circle
                r={16}
                fill={t.color}
                stroke={isSelected ? '#fff' : unselectedStroke}
                strokeWidth={isSelected ? 2 : 1.5}
              />
```

- [ ] **Step 2: Increase technique ID font size (line 296)**

Find:
```tsx
                fontSize={7.5}
```
Replace with:
```tsx
                fontSize={9}
```

- [ ] **Step 3: Increase tactic label font size (line 347)**

Find:
```tsx
                fontSize={8}
                fill="#00ff64"
```
Replace with:
```tsx
                fontSize={9}
                fill="#00ff64"
```

- [ ] **Step 4: Increase tactic coverage ratio font size (line 356)**

Find:
```tsx
                <tspan x={0} dy={lineH} fontSize={6.5} fill={coverageColor(pct)} fontWeight="400">
```
Replace with:
```tsx
                <tspan x={0} dy={lineH} fontSize={8} fill={coverageColor(pct)} fontWeight="400">
```

- [ ] **Step 5: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -20
```
Expected: zero errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "fix(force-graph): enlarge touch targets and increase SVG font sizes for readability"
```

---

### Task 3: Keyboard navigation for SVG nodes

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:278-285, 325-335`

Fixes issue 2 — nodes have `role="button"` but aren't focusable by keyboard. Adding `tabIndex={0}` makes them Tab-reachable; `onKeyDown` handles Enter/Space activation.

- [ ] **Step 1: Add `tabIndex` and `onKeyDown` to technique nodes (lines 278–285)**

Find:
```tsx
            <g
              key={nodeId}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); onSelectTechnique(isSelected ? null : t); }}
              style={{ cursor: 'pointer' }}
              className={`force-node${isSelected ? ' force-node--selected' : ''}`}
              role="button"
              aria-label={t.techniqueID}
            >
```
Replace with:
```tsx
            <g
              key={nodeId}
              tabIndex={0}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); onSelectTechnique(isSelected ? null : t); }}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); onSelectTechnique(isSelected ? null : t); } }}
              style={{ cursor: 'pointer' }}
              className={`force-node${isSelected ? ' force-node--selected' : ''}`}
              role="button"
              aria-label={t.techniqueID}
            >
```

- [ ] **Step 2: Add `tabIndex` and `onKeyDown` to tactic nodes (lines 325–335)**

Find:
```tsx
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
              role="button"
              aria-label={`${label}: ${covered} of ${total} covered`}
            >
```
Replace with:
```tsx
            <g
              key={tactic}
              tabIndex={total > 0 ? 0 : -1}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); handleTacticClick(tactic); }}
              onKeyDown={(e) => { if (total > 0 && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); e.stopPropagation(); handleTacticClick(tactic); } }}
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
                transition: 'opacity 0.2s ease',
              }}
              className="force-node force-node--tactic"
              role="button"
              aria-label={`${label}: ${covered} of ${total} covered`}
            >
```

Note: tactic nodes with `total === 0` get `tabIndex={-1}` (unreachable but programmatically focusable), consistent with `cursor: default` and the early return in `handleTacticClick`.

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -20
```
Expected: zero errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "fix(force-graph): add keyboard navigation to SVG tactic and technique nodes"
```

---

### Task 4: Hover tooltips on graph nodes

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx:149-153, 209-220, 268-306, 309-362, 363-394`

Fixes issue 5 — no hover tooltips. Reuses the existing `.tech-tooltip` CSS class (already `position: fixed`). Adds local `tooltip` state inside `ForceGraph`.

- [ ] **Step 1: Add `tooltip` state to `ForceGraph` (after line 152)**

Find:
```tsx
  const [graphView, setGraphView] = useState<'covered' | 'gaps'>('covered');
```
Replace with:
```tsx
  const [graphView, setGraphView] = useState<'covered' | 'gaps'>('covered');
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);
```

- [ ] **Step 2: Clear tooltip in `handleTacticClick` (line 213)**

Find:
```tsx
    onSelectTechnique(null);
    if (isCollapsing) {
```
Replace with:
```tsx
    onSelectTechnique(null);
    setTooltip(null);
    if (isCollapsing) {
```

- [ ] **Step 3: Add `onMouseEnter`/`onMouseLeave` to technique nodes (lines 278–285)**

Find:
```tsx
            <g
              key={nodeId}
              tabIndex={0}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); onSelectTechnique(isSelected ? null : t); }}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); onSelectTechnique(isSelected ? null : t); } }}
              style={{ cursor: 'pointer' }}
              className={`force-node${isSelected ? ' force-node--selected' : ''}`}
              role="button"
              aria-label={t.techniqueID}
            >
```
Replace with:
```tsx
            <g
              key={nodeId}
              tabIndex={0}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); onSelectTechnique(isSelected ? null : t); }}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); onSelectTechnique(isSelected ? null : t); } }}
              onMouseEnter={(e) => setTooltip({ x: e.clientX + 12, y: e.clientY - 8, text: `${t.techniqueID} · ${t.name ?? t.techniqueID}` })}
              onMouseLeave={() => setTooltip(null)}
              style={{ cursor: 'pointer' }}
              className={`force-node${isSelected ? ' force-node--selected' : ''}`}
              role="button"
              aria-label={t.techniqueID}
            >
```

- [ ] **Step 4: Add `onMouseEnter`/`onMouseLeave` to tactic nodes (lines 325–335)**

Find:
```tsx
            <g
              key={tactic}
              tabIndex={total > 0 ? 0 : -1}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); handleTacticClick(tactic); }}
              onKeyDown={(e) => { if (total > 0 && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); e.stopPropagation(); handleTacticClick(tactic); } }}
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
                transition: 'opacity 0.2s ease',
              }}
              className="force-node force-node--tactic"
              role="button"
              aria-label={`${label}: ${covered} of ${total} covered`}
            >
```
Replace with:
```tsx
            <g
              key={tactic}
              tabIndex={total > 0 ? 0 : -1}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); handleTacticClick(tactic); }}
              onKeyDown={(e) => { if (total > 0 && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); e.stopPropagation(); handleTacticClick(tactic); } }}
              onMouseEnter={(e) => setTooltip({ x: e.clientX + 12, y: e.clientY - 8, text: `${label}: ${covered}/${total} covered (${Math.round(pct)}%)` })}
              onMouseLeave={() => setTooltip(null)}
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
                transition: 'opacity 0.2s ease',
              }}
              className="force-node force-node--tactic"
              role="button"
              aria-label={`${label}: ${covered} of ${total} covered`}
            >
```

- [ ] **Step 5: Render tooltip div inside the container (after the closing `</svg>` tag, before the legend div, around line 364)**

Find:
```tsx
      <div className="force-graph-legend">
```
Replace with:
```tsx
      {tooltip && (
        <div className="tech-tooltip" style={{ left: tooltip.x, top: tooltip.y }}>
          {tooltip.text}
        </div>
      )}

      <div className="force-graph-legend">
```

- [ ] **Step 6: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -20
```
Expected: zero errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx && git commit -m "fix(force-graph): add hover tooltips to technique and tactic nodes"
```

---

### Task 5: Legend CSS + prefers-reduced-motion

**Files:**
- Modify: `frontend/src/App.css:862` (after `.force-graph-container`)
- Modify: `frontend/src/components/MITREHeatmap.tsx:330-333` (remove inline transition from tactic node)

Fixes issues 4 (legend has no CSS) and 8 (opacity transition ignores `prefers-reduced-motion`).

- [ ] **Step 1: Remove the inline `transition` from the tactic node style (line 332)**

Find:
```tsx
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
                transition: 'opacity 0.2s ease',
              }}
```
Replace with:
```tsx
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
              }}
```

- [ ] **Step 2: Add legend CSS and force-node transition to `App.css` (after line 862)**

Find:
```css
.force-graph-container { flex: 1; overflow: hidden; position: relative; }
```
Replace with:
```css
.force-graph-container { flex: 1; overflow: hidden; position: relative; }

/* Force graph legend */
.force-graph-legend { display: flex; align-items: center; gap: 14px; padding: 6px 12px; border-top: 1px solid var(--border); background: var(--surface); }
.force-legend-item { display: flex; align-items: center; gap: 5px; font-family: var(--font-mono); font-size: 0.6rem; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-sec); }
.force-legend-item::before { content: ''; display: inline-block; width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
.force-legend-item--tactic::before { background: rgba(0,255,100,0.15); border: 1.5px solid #00ff64; }
.force-legend-item--covered::before { background: #10b981; }
.force-legend-item--partial::before { background: #065f46; }
.force-legend-item--uncovered::before { background: #7f1d1d; }

/* Tactic node opacity transition — respects prefers-reduced-motion */
.force-node--tactic { transition: opacity 0.2s ease; }
@media (prefers-reduced-motion: reduce) { .force-node--tactic { transition: none; } }
```

- [ ] **Step 3: Build to verify no errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run build 2>&1 | tail -10
```
Expected: `✓ built in X.Xs` with no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/components/MITREHeatmap.tsx frontend/src/App.css && git commit -m "fix(force-graph): add legend CSS and honour prefers-reduced-motion for tactic dimming"
```
