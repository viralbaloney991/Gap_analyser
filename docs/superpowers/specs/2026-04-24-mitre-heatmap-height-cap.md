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

Recon has 43 techniques (10 parent + 33 sub). Content height in `.tactic-techniques`: 43 × 26px + 42 × 3px gap + 12px padding = 1256px. Plus `.tactic-header` (~52px) = **1308px total column content**.

| Monitor | Viewport height | 55% cap | Recon column (1308px) | Scrolls? |
|---------|-----------------|---------|------------------------|----------|
| 1080p   | 1080px          | 594px   | 1308px > 594px         | ✅ Yes   |
| 1440p   | 1440px          | 792px   | 1308px > 792px         | ✅ Yes   |
| 4K      | 2160px          | 1188px  | 1308px > 1188px        | ✅ Yes   |

55% is the largest percentage that still forces Recon to scroll on 4K. At 56%: cap = 1210px, still < 1308px ✅. At 61%: cap = 1318px > 1308px ❌ (Recon no longer scrolls on 4K). 55% leaves a comfortable margin.

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
| Recon (43 techniques) on 4K monitor | No scroll — 1308px fits in 1900px+ height | Scrolls — height capped at 1188px |
| Impact (22 techniques) on 1440p | No scroll — ~689px fits in 1320px height | No scroll — 689px < 792px cap (all 22 fit) |
| Impact (22 techniques) on 1080p | No scroll — ~689px fits in 960px height | Scrolls — 689px > 594px cap |
| Short columns (≤10 techniques) | Large dead space | Same dead space — no change to column behaviour |
| Window resize | Height updates via ResizeObserver | Cap recomputes alongside height — same latency |

---

## Out of Scope

- Cell size changes (26px stays)
- Column width changes (140px stays)
- CSS changes
- Any other component
