# MITRE Heatmap Height Measurement Fix — Design Spec
**Date:** 2026-04-24  
**Status:** Approved for implementation

---

## Problem

The 55vh cap introduced in `90abbe3` compressed the heatmap to ~55% of the screen on MacBook Pro / normal monitors (800–900px viewport height). On an 800px viewport: `800 × 0.55 = 440px` scroll wrap height — barely half the screen — while `.mitre-heatmap` still fills ~680px. The space below the columns is dead empty.

The cap was chosen to force Recon to scroll on 4K (2160px × 0.55 = 1188px < Recon's 1308px), but it's too aggressive on smaller screens.

**The underlying measurement bug** was also never fixed: `containerRef.getBoundingClientRect().height` measures the element's layout box height. After the initial render (before ResizeObserver fires), `.mitre-heatmap` expands to accommodate its content (because `.heatmap-scroll-wrap` has no height yet). The ResizeObserver fires, measures the content-inflated height (e.g. 1400px), and sets `heatmapBodyHeight` to that large value. The cap was masking this bug rather than fixing it.

---

## Solution

Two changes to the `update()` function in the ResizeObserver `useEffect`:

1. **Fix the measurement:** Replace `containerRef.getBoundingClientRect().height - toolbarRef.getBoundingClientRect().height` with `window.innerHeight - toolbarRef.getBoundingClientRect().bottom`. This measures the exact pixels from the toolbar's bottom edge to the viewport bottom — always viewport-relative, never content-inflated.

2. **Replace the viewport-fraction cap with a fixed pixel cap:** Change from `window.innerHeight * 0.55` to a constant `HEATMAP_BODY_MAX_PX = 1100`. A fixed 1100px cap only activates on monitors where the available height exceeds 1100px (roughly 1440p and above), leaving MacBook Pro and 1080p monitors uncapped.

---

## Why 1100px

Recon (the tallest tactic column) has 43 techniques. Total column content height:
- `.tactic-techniques`: 43 × 26px + 42 × 3px gap + 12px padding = 1256px  
- `.tactic-header`: ~52px  
- **Total: ~1308px**

For Recon to always scroll, `heatmapBodyHeight` must be < 1308px. The 1100px cap provides a 208px safety margin and only activates on viewports where `window.innerHeight - toolbar.bottom > 1100px`.

| Monitor | Available height | Cap | Applied | Recon (1308px) scrolls? |
|---------|-----------------|-----|---------|--------------------------|
| MacBook Pro (~800px viewport) | ~680px | 1100px | 680px (no cap) | ✅ 1308 > 680 |
| 1080p (~1080px viewport) | ~960px | 1100px | 960px (no cap) | ✅ 1308 > 960 |
| 1440p (~1440px viewport) | ~1320px | 1100px | 1100px (capped) | ✅ 1308 > 1100 |
| 4K (~2160px viewport) | ~2040px | 1100px | 1100px (capped) | ✅ 1308 > 1100 |

On MacBook Pro and 1080p: heatmap fills the full available screen height. On 1440p+: capped at 1100px (still 76%+ of most 1440p screens).

---

## Architecture

### Files changed

| File | Change |
|------|--------|
| `frontend/src/components/MITREHeatmap.tsx` | Rename constant `HEATMAP_MAX_HEIGHT_VH → HEATMAP_BODY_MAX_PX`, change value `0.55 → 1100`; update `update()` to use `window.innerHeight - toolbar.bottom` and the new constant |

### No other files touched.

---

## Implementation Detail

### Constant (near line 55, with other module-level constants)

```tsx
// Before:
const HEATMAP_MAX_HEIGHT_VH = 0.55; // 55 % of viewport — keeps all tactic columns shorter than their content so per-column scroll triggers

// After:
const HEATMAP_BODY_MAX_PX = 1100; // px — only activates on tall monitors (≥ 1440p); keeps columns shorter than Recon's ~1308px of content
```

### `update()` function (inside the ResizeObserver `useEffect`)

```tsx
// Before:
const update = () => {
  if (!containerRef.current || !toolbarRef.current) return;
  const total   = containerRef.current.getBoundingClientRect().height;
  const toolbar = toolbarRef.current.getBoundingClientRect().height;
  setHeatmapBodyHeight(Math.min(total - toolbar, window.innerHeight * HEATMAP_MAX_HEIGHT_VH));
};

// After:
const update = () => {
  if (!toolbarRef.current) return;
  const toolbarBottom = toolbarRef.current.getBoundingClientRect().bottom;
  const available     = window.innerHeight - toolbarBottom;
  setHeatmapBodyHeight(Math.min(available, HEATMAP_BODY_MAX_PX));
};
```

`containerRef` check is removed from the guard (no longer read in `update()`). `containerRef` stays attached to the outer div — the ResizeObserver still observes it so height updates fire on container resize (window resize, layout shift).

---

## Behaviour after fix

| Scenario | Before (55vh cap) | After (fixed px cap) |
|----------|-------------------|----------------------|
| MacBook Pro — heatmap height | 440px (55% of 800px) | 680px (fills available space) |
| MacBook Pro — Recon scrolls? | ✅ Yes | ✅ Yes (1308 > 680) |
| 1080p — heatmap height | 594px (55%) | 960px (fills available space) |
| 1080p — Recon scrolls? | ✅ Yes | ✅ Yes (1308 > 960) |
| 4K — heatmap height | 1188px (55%) | 1100px (capped) |
| 4K — Recon scrolls? | ✅ Yes | ✅ Yes (1308 > 1100) |
| Window resize | Recomputed via ResizeObserver | Same — ResizeObserver still fires |

---

## Out of Scope

- CSS changes
- Cell size (26px stays)
- Column width (140px stays)
- Any other component
