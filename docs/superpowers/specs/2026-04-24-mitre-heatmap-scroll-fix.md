# MITRE Heatmap Scroll Fix — Design Spec
**Date:** 2026-04-24  
**Status:** Approved for implementation

---

## Problem

The MITRE heatmap's tactic columns do not scroll consistently:

1. **On large / high-res monitors** — the CSS `1fr` grid row gives `.heatmap-scroll-wrap` a height equal to the full remaining viewport (e.g. 1800px on 4K). Reconnaissance has ~43 techniques × 29px ≈ 1247px of content, which fits without scrolling. The user expects scroll but the column is simply tall enough to show everything.

2. **Empty dead space** — columns with few techniques (e.g. Resource Development: 7 techniques) have large empty areas below their last technique cell, making the heatmap look unbalanced.

3. **Root cause of all prior CSS-only failures** — every fix relied on `flex: 1; min-height: 0` propagating a definite height to nested flex/grid children. Chromium resolves the cross-axis of a flex container by looking at intrinsic content size when the container has no *explicitly set* height, so the bounded height never reliably reached the technique list.

---

## Solution — ResizeObserver-based explicit height

Replace the fragile CSS height chain with a JavaScript measurement. The component measures its own available body height at runtime and sets it as an explicit `height` on the scroll container. This is the same pattern already used by `ForceGraph` for SVG sizing.

---

## Architecture

### Files changed

| File | Change |
|------|--------|
| `frontend/src/components/MITREHeatmap.tsx` | Add `containerRef`, `toolbarRef`, `ResizeObserver`, `heatmapBodyHeight` state |
| `frontend/src/App.css` | Simplify heatmap CSS — remove grid, remove min-height chain |

### No other files touched.

---

## Implementation Detail

### MITREHeatmap.tsx

Add two refs and one piece of state at the top of the `MITREHeatmap` component:

```tsx
const containerRef = useRef<HTMLDivElement>(null);
const toolbarRef   = useRef<HTMLDivElement>(null);
const [heatmapBodyHeight, setHeatmapBodyHeight] = useState(0);
```

Add a `useEffect` that attaches a single `ResizeObserver` to both elements:

```tsx
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
  update(); // run immediately on mount
  return () => obs.disconnect();
}, []);
```

Attach refs to the JSX:

```tsx
<div className="mitre-heatmap" ref={containerRef}>
  <div className="mitre-toolbar" ref={toolbarRef}>
    ...
  </div>
  {viewMode === 'heatmap' && (
    <div
      className="heatmap-scroll-wrap"
      style={{ height: heatmapBodyHeight > 0 ? heatmapBodyHeight : undefined }}
    >
```

**Why explicit `height` and not `max-height`:**  
`align-items: stretch` on the parent flex container propagates the cross-axis size to all columns only when the container has a *definite* height. An explicit `height` is definite. `max-height` alone is not definite for cross-axis stretch purposes — the browser still uses intrinsic content size as the floor.

### App.css — MITRE section

Replace the current grid-based rules with a simple flex column:

```css
.mitre-heatmap { flex: 1; min-height: 0; display: flex; flex-direction: column; }

.heatmap-scroll-wrap { overflow-x: auto; overflow-y: hidden; display: flex; align-items: stretch; }
.heatmap-columns     { display: flex; align-items: stretch; flex-shrink: 0; }

.tactic-col { display: flex; flex-direction: column; border-right: 1px solid var(--border); flex-shrink: 0; width: 140px; }
.tactic-col:last-child { border-right: none; }

.tactic-header { padding: 10px 10px 8px; border-bottom: 1px solid var(--border); flex-shrink: 0; position: relative; }
.tactic-header::before { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 3px; background: var(--cov-color, var(--cov-none)); }

.tactic-techniques { flex: 1; min-height: 0; overflow-y: auto; padding: 6px; display: flex; flex-direction: column; gap: 3px; }
```

The `flex: 1; min-height: 0` on `.mitre-heatmap` keeps it participating correctly in the outer flex chain. The internal layout is now just flex column — no grid needed, since JS provides the height.

---

## Behaviour after fix

| Scenario | Before | After |
|----------|--------|-------|
| Recon (43 techniques) on 4K monitor | No scroll — all fit in tall 1fr row | Scrolls — container measured at pixel-perfect height |
| Resource Dev (7 techniques) | Huge empty space below | Short column, no dead space, no scroll needed |
| Persistence (50+ techniques) | Scrolls (sometimes) | Always scrolls, same height as all other columns |
| Window resize | Height not updated | ResizeObserver fires, height recomputed |
| Graph view toggle | N/A | Refs persist, no effect |

---

## Out of Scope

- Column width changes (140px stays)
- ForceGraph layout (unchanged)
- Any other component
