# MITRE Graph Readability — Design

**Date:** 2026-04-30
**Status:** Approved

## Summary

Two readability problems in `MITREHeatmap.tsx`: (A) heatmap technique cells are too small to scan — 26px height, 7.7px IDs, no technique names visible without hover; (C) force graph becomes cluttered when a tactic is expanded — all techniques fan out radially, causing node overlap for high-count tactics like Defense Evasion (~40 techniques).

Fix A enlarges cells and adds truncated technique names inline. Fix C collapses uncovered techniques into a single summary node so only relevant nodes are rendered.

---

## Scope

**In scope:**
- Heatmap cell height, font size, and inline technique name (Part A)
- Force graph uncovered-technique collapse when a tactic is expanded (Part C)

**Out of scope:**
- Search/filter for techniques
- Changes to the detail panel or suggestions panel
- Changes to the heatmap column width (stays 140px)
- Changes to tactic node rendering in the force graph

---

## Design

### Part A — Heatmap cell readability

**File:** `frontend/src/App.css` (`.tech-cell`, `.tech-id`) and `frontend/src/components/MITREHeatmap.tsx` (technique cell JSX)

#### CSS changes

`.tech-cell`:
- Height: `26px → 38px`
- Add `flex-direction: column`, `align-items: flex-start`, `justify-content: center`, `gap: 1px`

`.tech-id`:
- Font size: `0.55rem → 0.68rem`
- Opacity: `0.7 → 0.9`

Add new `.tech-name` rule:
```css
.tech-name {
  font-size: 0.58rem;
  opacity: 0.6;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  width: 100%;
  color: #fff;
}
```

#### JSX change

Inside the technique cell render (currently renders only `.tech-id`), add `.tech-name` below it:

```tsx
<span className="tech-id">{t.techniqueID}</span>
<span className="tech-name">{shortName(t.name)}</span>
```

#### `shortName` helper

Add a pure helper function in `MITREHeatmap.tsx`:

```ts
function shortName(name: string): string {
  const trimmed = name.split('(')[0].split('/')[0].trim();
  return trimmed.length > 18 ? trimmed.slice(0, 17) + '…' : trimmed;
}
```

Examples:
- `"Valid Accounts (Local)"` → `"Valid Accounts"`
- `"PowerShell/Empire"` → `"PowerShell"`
- `"Exploit Public-Facing Application"` → `"Exploit Public-Fa…"`

---

### Part C — Force graph de-clutter

**File:** `frontend/src/components/MITREHeatmap.tsx` (`ForceGraph` component, technique node rendering section)

When a tactic is expanded (clicked), split its techniques into two groups:

```ts
const covered = techniques.filter(t => t.score > 0);
const uncoveredCount = techniques.length - covered.length;
```

**Render covered techniques** as full coloured nodes, positioned radially as before. The radius formula `max(90, sqrt(count) * 30)` now uses `covered.length + (uncoveredCount > 0 ? 1 : 0)` as `count`, naturally shrinking the ring.

**If `uncoveredCount > 0`**, render one additional summary node at the last radial position:
- Radius: `14px`
- Fill: `var(--surface-2)` (grey)
- Label: `+N` (white, 7px, bold)
- Sub-label below node: `uncovered` (white, 5.5px)
- No click handler — `pointer-events: none`
- `aria-label`: `"${uncoveredCount} uncovered techniques"`

**No change** to tactic node rendering, detail panel, suggestions panel, or heatmap view.

---

## Error Handling

No new error paths. `shortName` handles empty strings (returns `""`). The summary node renders only when `uncoveredCount > 0`; when all techniques are covered it is absent.

---

## Tests

This is a pure frontend rendering change with no backend logic. Visual verification:
- Heatmap cells show technique ID + truncated name at the new size
- `shortName("Valid Accounts (Local)")` → `"Valid Accounts"`
- `shortName("Exploit Public-Facing Application")` → `"Exploit Public-Fa…"`
- Force graph with Defense Evasion expanded shows only covered nodes + one `+N uncovered` summary node
