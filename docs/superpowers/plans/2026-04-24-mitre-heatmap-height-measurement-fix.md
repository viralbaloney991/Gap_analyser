# MITRE Heatmap Height Measurement Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the heatmap scroll container height so it fills the full available screen on MacBook Pro / 1080p while still forcing per-column scroll on large (1440p+) monitors.

**Architecture:** Two changes to `MITREHeatmap.tsx`: (1) rename the constant from `HEATMAP_MAX_HEIGHT_VH = 0.55` to `HEATMAP_BODY_MAX_PX = 1100` (fixed pixel cap instead of viewport fraction); (2) rewrite the `update()` function inside the ResizeObserver `useEffect` to measure `window.innerHeight - toolbarRef.getBoundingClientRect().bottom` instead of `containerRef.getBoundingClientRect().height - toolbarRef.getBoundingClientRect().height`. No CSS changes, no other files.

**Tech Stack:** React 19, TypeScript. No new packages.

---

## Files

| File | Action |
|------|--------|
| `frontend/src/components/MITREHeatmap.tsx` | Rename constant at line 56; rewrite `update()` at lines 565–569; update comment at lines 556–563 |

---

## Task 1 — Fix the constant and the measurement

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx` (lines 56, 556–569)

**Context:** The current code has two bugs:

1. **Wrong constant:** `HEATMAP_MAX_HEIGHT_VH = 0.55` is a viewport-fraction cap. On an 800px viewport: `800 × 0.55 = 440px` — barely half the screen. Recon scrolls but the heatmap looks compressed.

2. **Wrong measurement:** `containerRef.current.getBoundingClientRect().height` measures `.mitre-heatmap`'s layout box, which on first render (before the ResizeObserver fires) can be the content-inflated height (e.g. 1400px) rather than the viewport-bounded height. `window.innerHeight - toolbarRef.getBoundingClientRect().bottom` is always correct — it measures the exact pixels from the toolbar's bottom edge to the viewport bottom, independently of content size.

The fix: switch to a fixed pixel cap (`HEATMAP_BODY_MAX_PX = 1100`) that only activates when `available > 1100px` (≥ 1440p monitors). On MacBook Pro (~680px available) no cap applies and the heatmap fills the full available space.

- [ ] **Step 1: Rename the constant at line 56**

Find line 56 in `frontend/src/components/MITREHeatmap.tsx`:
```tsx
const HEATMAP_MAX_HEIGHT_VH = 0.55; // 55 % of viewport — keeps all tactic columns shorter than their content so per-column scroll triggers
```

Replace with:
```tsx
const HEATMAP_BODY_MAX_PX = 1100; // px — only activates on tall monitors (≥ 1440p); keeps columns shorter than the tallest tactic column's ~1308px of content
```

- [ ] **Step 2: Rewrite the comment and `update()` at lines 556–569**

Find this block (lines 556–569):
```tsx
  // Measure available height for the heatmap scroll area.
  // containerRef watches .mitre-heatmap; toolbarRef watches .mitre-toolbar.
  // scrollHeight = total container height − toolbar height, capped at 55vh.
  // The 55vh cap ensures that on large/4K monitors the scroll container is
  // never tall enough to show all techniques without scrolling — Recon has
  // ~1308px of content, and 55% of a 2160px (4K) viewport = 1188px < 1308px.
  // The cap is recomputed on every ResizeObserver fire so window resizes are
  // handled automatically.
  useEffect(() => {
    const update = () => {
      if (!containerRef.current || !toolbarRef.current) return;
      const total   = containerRef.current.getBoundingClientRect().height;
      const toolbar = toolbarRef.current.getBoundingClientRect().height;
      setHeatmapBodyHeight(Math.min(total - toolbar, window.innerHeight * HEATMAP_MAX_HEIGHT_VH));
    };
```

Replace with:
```tsx
  // Measure available height for the heatmap scroll area.
  // Uses window.innerHeight − toolbar.bottom (viewport-relative) instead of
  // container.height − toolbar.height, which can be content-inflated on first
  // render before the ResizeObserver fires.
  // HEATMAP_BODY_MAX_PX caps the height on tall monitors (≥ 1440p) to keep
  // all tactic columns shorter than their tallest content (~1308px) so that
  // per-column overflow-y:auto always triggers scroll. On smaller monitors
  // (MacBook Pro, 1080p) the available height is already < 1100px so no cap
  // is applied and the heatmap fills the full available viewport.
  useEffect(() => {
    const update = () => {
      if (!toolbarRef.current) return;
      const toolbarBottom = toolbarRef.current.getBoundingClientRect().bottom;
      const available     = window.innerHeight - toolbarBottom;
      setHeatmapBodyHeight(Math.min(available, HEATMAP_BODY_MAX_PX));
    };
```

- [ ] **Step 3: Verify TypeScript compiles cleanly**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output (no errors). `window.innerHeight` and `getBoundingClientRect().bottom` are both typed in `lib.dom.d.ts`.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/MITREHeatmap.tsx
git commit -m "fix(heatmap): use viewport-bottom measurement and fixed px cap — fills screen on MacBook Pro, scrolls on 1440p+"
```

---

## Self-Review

**Spec coverage:**
- ✅ `window.innerHeight - toolbar.bottom` replaces `container.height - toolbar.height` → Step 2
- ✅ `HEATMAP_BODY_MAX_PX = 1100` replaces `HEATMAP_MAX_HEIGHT_VH = 0.55` → Steps 1 & 2
- ✅ `containerRef` guard removed from `update()` (not read there) → Step 2
- ✅ `containerRef` still attached to div and observed by ResizeObserver (so resize events still fire) — unchanged, not touched
- ✅ No CSS changes → no CSS task
- ✅ No other files touched → single file, single task

**Placeholder scan:** No TBDs. All code is complete and exact.

**Type consistency:** `HEATMAP_BODY_MAX_PX` is `number` (1100). `Math.min(number, number)` returns `number`. `setHeatmapBodyHeight` accepts `number`. Consistent throughout.
