# MITRE Heatmap Scroll Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix MITRE heatmap tactic columns so every column with more techniques than can fit in the viewport scrolls independently, on any screen size, by replacing the fragile CSS height chain with a ResizeObserver measurement in the component.

**Architecture:** Two refs (`containerRef` on `.mitre-heatmap`, `toolbarRef` on `.mitre-toolbar`) feed a ResizeObserver that computes `scrollHeight = containerHeight − toolbarHeight` and stores it in state. This value is applied as an explicit `height` inline style on `.heatmap-scroll-wrap`, giving all child tactic columns a definite, content-independent height. The CSS is simplified to a plain flex-column layout — no grid, no `min-height: 0` chain.

**Tech Stack:** React 19, TypeScript, Vite, CSS custom properties. No new packages.

---

## Files

| File | Action |
|------|--------|
| `frontend/src/App.css` | Modify lines 651–710 — replace grid `.mitre-heatmap` with flex, clean up scroll rules |
| `frontend/src/components/MITREHeatmap.tsx` | Modify — add `containerRef`, `toolbarRef`, `heatmapBodyHeight` state, ResizeObserver effect, inline style on scroll wrap |

---

## Task 1 — Simplify App.css heatmap section

**Files:**
- Modify: `frontend/src/App.css` (lines 651–710)

- [ ] **Step 1: Replace the MITRE HEATMAP CSS block**

Find the block starting with `/* ================================================================ MITRE HEATMAP` and replace everything from `.mitre-heatmap {` down to and including `.tactic-techniques { ... }` with:

```css
/* ================================================================
   MITRE HEATMAP
   ================================================================ */

/*
 * .mitre-heatmap is a flex item in the outer layout (flex: 1 = take
 * remaining height). Its internal layout is a simple flex column —
 * no grid needed, because MITREHeatmap.tsx uses a ResizeObserver to
 * measure and explicitly set the scroll container's height.
 */
.mitre-heatmap { flex: 1; min-height: 0; display: flex; flex-direction: column; }

/* Toolbar */
.mitre-toolbar { display: flex; align-items: center; justify-content: space-between; padding: 12px 24px; border-bottom: 1px solid var(--border); gap: 16px; flex-shrink: 0; }

.mitre-stats         { display: flex; align-items: center; gap: 20px; }
.mitre-stat          { display: flex; flex-direction: column; gap: 2px; }
.mitre-stat-val      { font-family: var(--font-mono); font-size: 1.1rem; font-weight: 500; color: var(--text); letter-spacing: -0.02em; }
.mitre-stat-val--accent { color: var(--accent); }
.mitre-stat-val--warn   { color: var(--warn); }
.mitre-stat-label    { font-family: var(--font-mono); font-size: 0.55rem; text-transform: uppercase; letter-spacing: 0.12em; color: var(--text-dim); }
.mitre-stat-divider  { width: 1px; height: 32px; background: var(--border); }

.mitre-toolbar-right { display: flex; align-items: center; gap: 12px; }

.mitre-legend        { display: flex; align-items: center; gap: 10px; }
.mitre-legend-item   { display: flex; align-items: center; gap: 5px; }
.mitre-legend-dot    { width: 10px; height: 10px; border-radius: 2px; flex-shrink: 0; }
.mitre-legend-label  { font-family: var(--font-mono); font-size: 0.58rem; color: var(--text-dim); }

/* View toggle */
.view-toggle     { display: flex; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
.view-toggle-btn { font-family: var(--font-mono); font-size: 0.62rem; letter-spacing: 0.06em; text-transform: uppercase; padding: 6px 14px; background: none; border: none; color: var(--text-dim); cursor: pointer; transition: all 0.15s; }
.view-toggle-btn--active { background: var(--accent-dim); color: var(--accent); }

/*
 * Heatmap scroll container — height is set explicitly via inline style
 * by MITREHeatmap.tsx (ResizeObserver). overflow-x: auto provides
 * horizontal scroll across all 14 tactic columns. overflow-y: hidden
 * prevents the container from scrolling as a unit — each column's
 * .tactic-techniques element scrolls independently.
 * align-items: stretch propagates the explicit height to all columns.
 */
.heatmap-scroll-wrap { overflow-x: auto; overflow-y: hidden; display: flex; align-items: stretch; }
.heatmap-columns     { display: flex; align-items: stretch; flex-shrink: 0; }

/* Tactic columns */
.tactic-col { display: flex; flex-direction: column; border-right: 1px solid var(--border); flex-shrink: 0; width: 140px; }
.tactic-col:last-child { border-right: none; }

.tactic-header { padding: 10px 10px 8px; border-bottom: 1px solid var(--border); flex-shrink: 0; position: relative; }
.tactic-header::before { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 3px; background: var(--cov-color, var(--cov-none)); }

.tactic-name  { font-family: var(--font-display); font-size: 0.72rem; font-weight: 600; color: var(--text); line-height: 1.3; margin-bottom: 4px; word-break: break-word; }
.tactic-count { font-family: var(--font-mono); font-size: 0.58rem; color: var(--text-dim); }
.tactic-count em { font-style: normal; color: var(--accent); }
.tactic-count--zero    { color: var(--danger); }
.tactic-count--zero em { color: var(--danger); }

/*
 * .tactic-techniques uses flex: 1 to fill the remaining column height
 * after the header. Because .tactic-col has an explicit height (via
 * align-self: stretch from parent), flex: 1 here is bounded — and
 * overflow-y: auto creates a per-column scrollbar when needed.
 */
.tactic-techniques { flex: 1; min-height: 0; overflow-y: auto; padding: 6px; display: flex; flex-direction: column; gap: 3px; }
```

- [ ] **Step 2: Verify the CSS file compiles without errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no output (no errors). TypeScript doesn't parse CSS, so this just confirms the dev toolchain is intact.

- [ ] **Step 3: Commit the CSS change**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && git add src/App.css
git commit -m "fix(heatmap): simplify heatmap CSS to flex-column — grid removed, height will come from ResizeObserver"
```

---

## Task 2 — Add ResizeObserver to MITREHeatmap.tsx

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx`

Context: The main component function starts at line 511. `useRef` and `useEffect` are already imported on line 9. The outer div is `<div className="mitre-heatmap">` at line 551. The toolbar is `<div className="mitre-toolbar">` at line 553. The heatmap scroll wrap is `<div className="heatmap-scroll-wrap">` inside `{viewMode === 'heatmap' && ...}`.

- [ ] **Step 1: Add refs and state inside the MITREHeatmap component**

After line 514 (after the existing `useState` declarations), add:

```tsx
const containerRef       = useRef<HTMLDivElement>(null);
const toolbarRef         = useRef<HTMLDivElement>(null);
const [heatmapBodyHeight, setHeatmapBodyHeight] = useState(0);
```

The full block of state + refs at the top of the function should look like:

```tsx
export default function MITREHeatmap({ data, clientName }: Props) {
  const [viewMode, setViewMode] = useState<ViewMode>('heatmap');
  const [selectedTechnique, setSelectedTechnique] = useState<NavigatorTechnique | null>(null);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);

  const containerRef       = useRef<HTMLDivElement>(null);
  const toolbarRef         = useRef<HTMLDivElement>(null);
  const [heatmapBodyHeight, setHeatmapBodyHeight] = useState(0);

  const { summary, navigator_layer: layer } = data;
```

- [ ] **Step 2: Add the ResizeObserver effect**

After the existing `const selectedNodeId = ...` line (around line 546) and before the `return (`, add:

```tsx
  // Measure available height for the heatmap scroll area.
  // containerRef watches .mitre-heatmap; toolbarRef watches .mitre-toolbar.
  // scrollHeight = total container height − toolbar height.
  // This explicit pixel value is applied as height on .heatmap-scroll-wrap so
  // align-items:stretch can propagate a DEFINITE height to all tactic columns,
  // making overflow-y:auto on .tactic-techniques work on any screen size.
  useEffect(() => {
    const update = () => {
      if (!containerRef.current || !toolbarRef.current) return;
      const total   = containerRef.current.getBoundingClientRect().height;
      const toolbar = toolbarRef.current.getBoundingClientRect().height;
      setHeatmapBodyHeight(total - toolbar);
    };
    const obs = new ResizeObserver(update);
    if (containerRef.current) obs.observe(containerRef.current);
    if (toolbarRef.current)   obs.observe(toolbarRef.current);
    update();
    return () => obs.disconnect();
  }, []);
```

- [ ] **Step 3: Attach containerRef to the outer div**

Find the line:
```tsx
  return (
    <div className="mitre-heatmap">
```

Change it to:
```tsx
  return (
    <div className="mitre-heatmap" ref={containerRef}>
```

- [ ] **Step 4: Attach toolbarRef to the toolbar div**

Find the line:
```tsx
      <div className="mitre-toolbar">
```

Change it to:
```tsx
      <div className="mitre-toolbar" ref={toolbarRef}>
```

- [ ] **Step 5: Apply the measured height to the scroll wrap**

Find the line (inside `{viewMode === 'heatmap' && (`):
```tsx
        <div className="heatmap-scroll-wrap">
```

Change it to:
```tsx
        <div
          className="heatmap-scroll-wrap"
          style={{ height: heatmapBodyHeight > 0 ? heatmapBodyHeight : undefined }}
        >
```

The guard `heatmapBodyHeight > 0` means no inline style on first paint (before ResizeObserver fires). The ResizeObserver calls `update()` synchronously on mount, so the height is set before the browser's first visible render — no layout flash.

- [ ] **Step 6: Check TypeScript compiles cleanly**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output. If there's a type error on the `ResizeObserver` constructor, add `// @ts-ignore` above it — ResizeObserver types are in `lib.dom.d.ts` which Vite includes by default, so this shouldn't be needed.

- [ ] **Step 7: Start the dev server and verify visually**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run dev
```

Open the browser. Navigate to the MITRE view. Verify:

1. The heatmap columns are all the same height — they stop at a consistent bottom edge, not at the bottom of the browser window
2. On any column with more than ~22 technique cells (at 26px per cell), a vertical scrollbar appears on the right edge of that column
3. Scrolling within a column scrolls only that column's techniques — the other columns and the toolbar stay fixed
4. Horizontal scroll across all 14 tactic columns still works
5. Resize the browser window — the column height updates within milliseconds (ResizeObserver fires on every resize)
6. Switch to Graph view and back — no errors, columns still scroll correctly

- [ ] **Step 8: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/MITREHeatmap.tsx
git commit -m "fix(heatmap): use ResizeObserver to set explicit scroll container height — fixes per-column vertical scroll on all screen sizes"
```

---

## Self-Review

**Spec coverage:**
- ✅ `containerRef` + `toolbarRef` + ResizeObserver → Task 2 Steps 1–2
- ✅ `heatmapBodyHeight` state → Task 2 Step 1
- ✅ Explicit `height` (not `max-height`) on `.heatmap-scroll-wrap` → Task 2 Step 5
- ✅ CSS simplified to flex-column, grid removed → Task 1 Step 1
- ✅ `update()` called immediately on mount (no flash) → Task 2 Step 2 comment
- ✅ `obs.disconnect()` in cleanup → Task 2 Step 2

**Placeholder scan:** No TBDs, no "handle edge cases", all code is complete.

**Type consistency:** `heatmapBodyHeight` is `number` (useState<number>), used as `number` in the `height` style property. `containerRef` and `toolbarRef` are both `RefObject<HTMLDivElement>`, matching the `ref` prop on `<div>` elements. Consistent throughout.
