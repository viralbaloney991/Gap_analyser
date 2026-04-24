# MITRE Heatmap Column Height Cap — Design Spec
**Date:** 2026-04-24  
**Status:** Approved for implementation

---

## Problem

The MITRE heatmap tactic columns do not scroll on large/high-res monitors.

The ResizeObserver introduced in the previous fix (`5dec980`) correctly measures `.mitre-heatmap`'s bounded height and applies it as an explicit `height` on `.heatmap-scroll-wrap`. This works as intended — but on large monitors the bounded height is large enough (1200–1800px on 1440p–4K) that even the tallest tactic column (Reconnaissance, ~43 techniques = 1244px of content) fits without triggering scroll.

The result: columns with many techniques show all their content with no scrollbar on large monitors, while short columns show dead space. The user's expectation is that columns with more techniques than can comfortably fit in the viewport always scroll.

Cell size (26px height, 140px wide) is correct and must not change.

---

## Solution

Cap `heatmapBodyHeight` at 55% of the viewport height inside the existing ResizeObserver `update()` function. This ensures the scroll container is never taller than 55vh regardless of monitor size, so columns with many techniques always have content that exceeds the container and triggers per-column scroll.

---

## Why 55%

| Monitor | Viewport height | 55% cap | Recon content (1244px) | Scrolls? |
|---------|-----------------|---------|------------------------|----------|
| 1080p   | 1080px          | 594px   | 1244px > 594px         | ✅ Yes   |
| 1440p   | 900px           | 495px   | 1244px > 495px         | ✅ Yes   |
| 4K      | 2160px          | 1188px  | 1244px > 1188px        | ✅ Yes   |

55% is the smallest percentage at which 4K monitors (the hardest case) still force Recon to scroll. Values above 58% fail on 4K.

---

## Architecture

### Files changed

| File | Change |
|------|--------|
| `frontend/src/components/MITREHeatmap.tsx` | One line in the existing ResizeObserver `update()` — add `Math.min(..., window.innerHeight * 0.55)` cap |

### No other files touched.

---

## Implementation Detail

### MITREHeatmap.tsx — existing `update()` function

Current code (inside the `useEffect` ResizeObserver):

```tsx
const update = () => {
  if (!containerRef.current || !toolbarRef.current) return;
  const total   = containerRef.current.getBoundingClientRect().height;
  const toolbar = toolbarRef.current.getBoundingClientRect().height;
  setHeatmapBodyHeight(total - toolbar);
};
```

After change:

```tsx
const update = () => {
  if (!containerRef.current || !toolbarRef.current) return;
  const total   = containerRef.current.getBoundingClientRect().height;
  const toolbar = toolbarRef.current.getBoundingClientRect().height;
  setHeatmapBodyHeight(Math.min(total - toolbar, window.innerHeight * 0.55));
};
```

The cap `window.innerHeight * 0.55` is computed fresh on every ResizeObserver fire, so it automatically adapts when the user resizes the browser window.

---

## Behaviour after fix

| Scenario | Before | After |
|----------|--------|-------|
| Recon (43 techniques) on 4K monitor | No scroll — 1244px fits in 1800px height | Scrolls — height capped at 1188px |
| Impact (22 techniques) on 1080p | No scroll — 635px fits in 950px height | No scroll — 635px < 594px cap... scrolls at 1080p, does not scroll at 1440p+ |
| Short columns (≤10 techniques) | Large dead space | Same dead space — no change to column behaviour |
| Window resize | Height updates via ResizeObserver | Cap recomputes alongside height — same latency |

---

## Out of Scope

- Cell size changes (26px stays)
- Column width changes (140px stays)
- CSS changes
- Any other component
