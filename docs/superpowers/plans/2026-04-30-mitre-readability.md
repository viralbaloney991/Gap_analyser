# MITRE Graph Readability — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make heatmap technique cells legible by enlarging them and showing inline names, and eliminate force graph clutter by collapsing uncovered techniques into a single summary node.

**Architecture:** Two independent changes to `MITREHeatmap.tsx` and `App.css`. Task 1 adds a `shortName` helper, updates CSS, and adds a `.tech-name` span to the heatmap cell JSX. Task 2 splits the force graph's `expandedTechs` array into covered/uncovered, updates the radial node count, and replaces the raw loop with a covered-node loop plus a summary node. No backend changes.

**Tech Stack:** React + TypeScript, CSS (no test framework for components — verified visually via dev server).

---

## File Map

| File | Change |
|------|--------|
| `frontend/src/App.css` | Update `.tech-cell`, `.tech-id`; add `.tech-name` |
| `frontend/src/components/MITREHeatmap.tsx` | Add `shortName` helper; add `.tech-name` span in heatmap JSX; split `expandedTechs` into `coveredTechs`/`uncoveredCount`; update edge and node rendering |

---

## Task 1: Heatmap cell readability (Part A)

**Files:**
- Modify: `frontend/src/App.css` (line 766 — `.tech-cell`; line 770 — `.tech-id`)
- Modify: `frontend/src/components/MITREHeatmap.tsx` (after line 68 — add `shortName`; line 721 — heatmap cell JSX)

### Context

The heatmap cell JSX is at line 720–722 of `MITREHeatmap.tsx`:
```tsx
<div
  key={`${t.techniqueID}-${t.tactic}`}
  className={`tech-cell${isActive ? ' tech-cell--selected' : ''}`}
  ...
>
  <span className="tech-id">{t.techniqueID}</span>
</div>
```

The CSS for these cells is at line 766–770 of `App.css`:
```css
.tech-cell { height: 26px; border-radius: 3px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: opacity 0.1s; position: relative; }
.tech-id { font-family: var(--font-mono); font-size: 0.55rem; color: rgba(255,255,255,0.7); }
```

- [ ] **Step 1: Update `.tech-cell` CSS**

In `frontend/src/App.css`, replace:
```css
.tech-cell { height: 26px; border-radius: 3px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: opacity 0.1s; position: relative; }
```
With:
```css
.tech-cell { height: 38px; border-radius: 3px; cursor: pointer; display: flex; flex-direction: column; align-items: flex-start; justify-content: center; gap: 1px; padding: 0 6px; transition: opacity 0.1s; position: relative; }
```

- [ ] **Step 2: Update `.tech-id` CSS**

In `frontend/src/App.css`, replace:
```css
.tech-id { font-family: var(--font-mono); font-size: 0.55rem; color: rgba(255,255,255,0.7); }
```
With:
```css
.tech-id { font-family: var(--font-mono); font-size: 0.68rem; color: rgba(255,255,255,0.9); }
```

- [ ] **Step 3: Add `.tech-name` CSS rule**

In `frontend/src/App.css`, add immediately after the `.tech-id` rule (after line 770):
```css
.tech-name { font-size: 0.58rem; opacity: 0.6; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; width: 100%; color: #fff; }
```

- [ ] **Step 4: Add `shortName` helper to `MITREHeatmap.tsx`**

In `frontend/src/components/MITREHeatmap.tsx`, add after the `coverageColor` and `priorityColor` helpers (after line 77, before the `// ---` divider at line 82):

```ts
function shortName(name: string): string {
  const trimmed = name.split('(')[0].split('/')[0].trim();
  return trimmed.length > 18 ? trimmed.slice(0, 17) + '\u2026' : trimmed;
}
```

- [ ] **Step 5: Add `.tech-name` span to heatmap cell JSX**

In `frontend/src/components/MITREHeatmap.tsx`, replace:
```tsx
                          <span className="tech-id">{t.techniqueID}</span>
```
With:
```tsx
                          <span className="tech-id">{t.techniqueID}</span>
                          <span className="tech-name">{shortName(t.name ?? '')}</span>
```

- [ ] **Step 6: Start dev server and verify visually**

```bash
cd frontend && npm run dev
```

Open the MITRE heatmap. Check:
- Each technique cell is taller (~38px) and shows the technique ID on one line, a short name beneath it
- `"Valid Accounts (Local)"` → shows `"Valid Accounts"` (no parenthetical)
- `"Exploit Public-Facing Application"` → shows `"Exploit Public-Fa…"` (truncated at 17 chars + ellipsis)
- IDs are more readable than before (larger, brighter)
- Hover tooltip still appears as before

- [ ] **Step 7: Commit**

```bash
git add frontend/src/App.css frontend/src/components/MITREHeatmap.tsx
git commit -m "feat(mitre): enlarge heatmap cells and show inline technique names"
```

---

## Task 2: Force graph de-clutter (Part C)

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx` (lines 185–196 — expanded tech state; lines 234–286 — edge + node rendering)

### Context

The current force graph computes positions and renders ALL techniques when a tactic is expanded. The relevant code:

**Lines 185–196** — expanded tech state and radial positions:
```ts
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
```

**Lines 234–246** — edge rendering:
```tsx
        {/* Edges: expanded tactic → its techniques */}
        {expandedCenter && expandedTechs.map((_, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={`edge:${expandedTechs[i].techniqueID}:${expandedTechs[i].tactic}`}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(0,255,100,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}
```

**Lines 250–286** — technique node rendering:
```tsx
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
```

- [ ] **Step 1: Split expanded techs into covered and uncovered**

In `frontend/src/components/MITREHeatmap.tsx`, replace lines 185–196:
```ts
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
```
With:
```ts
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

- [ ] **Step 2: Update edge rendering to use coveredTechs + summary edge**

In `frontend/src/components/MITREHeatmap.tsx`, replace the edge rendering block (lines 233–247):
```tsx
        {/* Edges: expanded tactic → its techniques */}
        {expandedCenter && expandedTechs.map((_, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={`edge:${expandedTechs[i].techniqueID}:${expandedTechs[i].tactic}`}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(0,255,100,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}
```
With:
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

- [ ] **Step 3: Update technique node rendering to use coveredTechs + summary node**

In `frontend/src/components/MITREHeatmap.tsx`, replace the technique nodes block (lines 249–286):
```tsx
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
```
With:
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
        {uncoveredCount > 0 && (() => {
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

- [ ] **Step 4: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: no errors.

- [ ] **Step 5: Start dev server and verify visually**

```bash
cd frontend && npm run dev
```

Switch to the Force Graph view. Click a tactic that has both covered and uncovered techniques (e.g. Defense Evasion). Check:
- Only covered techniques fan out as coloured nodes — far fewer than before
- One grey `+N uncovered` node appears at the end of the radial fan
- Covered nodes are clickable (detail panel opens)
- `+N uncovered` node is not clickable
- Click the tactic again — all nodes collapse

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/MITREHeatmap.tsx
git commit -m "feat(mitre): collapse uncovered techniques into summary node in force graph"
```

---

## Self-Review

**Spec coverage:**
- ✅ `.tech-cell` height 26px → 38px → Task 1 Step 1
- ✅ `.tech-id` font 0.55rem → 0.68rem, opacity 0.7 → 0.9 → Task 1 Step 2
- ✅ `.tech-name` CSS rule added → Task 1 Step 3
- ✅ `shortName` helper: splits on `(` and `/`, truncates at 18 chars → Task 1 Step 4
- ✅ `.tech-name` span added to heatmap JSX → Task 1 Step 5
- ✅ `coveredTechs` / `uncoveredCount` / `nodeCount` split → Task 2 Step 1
- ✅ `nodeCount` passed to `techniqueRadialPositions` → Task 2 Step 1
- ✅ Edge loop uses `coveredTechs` → Task 2 Step 2
- ✅ Summary edge to last radial position → Task 2 Step 2
- ✅ Node loop uses `coveredTechs` → Task 2 Step 3
- ✅ Summary node: grey fill, `+N` label, `uncovered` sub-label, no click handler → Task 2 Step 3
- ✅ No backend changes → nothing in plan

**Placeholder scan:** None found.

**Type consistency:** `coveredTechs` is `NavigatorTechnique[]` (filtered from `expandedTechs: NavigatorTechnique[]`). `uncoveredCount` is `number`. `nodeCount` is `number`. `shortName` takes `string` and returns `string`. All consistent.
