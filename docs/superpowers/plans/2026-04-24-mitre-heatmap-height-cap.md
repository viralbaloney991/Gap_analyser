# MITRE Heatmap Height Cap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cap the heatmap scroll container at 55% of viewport height so tactic columns with many techniques always scroll, on any screen size.

**Architecture:** One line change in the existing ResizeObserver `update()` function in `MITREHeatmap.tsx`. The computed height (`container − toolbar`) is wrapped in `Math.min(..., window.innerHeight * 0.55)` so it is never taller than 55vh. No CSS changes, no new files.

**Tech Stack:** React 19, TypeScript. No new packages.

---

## Files

| File | Action |
|------|--------|
| `frontend/src/components/MITREHeatmap.tsx` | Modify line 565 — add `Math.min` cap; update comment on lines 554–559 |

---

## Task 1 — Apply the 55vh height cap

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx` (lines 554–572)

**Context:** The `useEffect` block starting at line 560 sets up a `ResizeObserver` on `.mitre-heatmap` (`containerRef`) and `.mitre-toolbar` (`toolbarRef`). The `update()` function computes `total - toolbar` and stores it as `heatmapBodyHeight`. This value is applied as `height` on `.heatmap-scroll-wrap` via inline style, giving all 14 tactic columns a definite, viewport-bounded height.

The bug: on large monitors the computed height can be 1300–1900px, which is taller than even the longest tactic column (Recon ~1308px), so no column ever scrolls. The fix: cap the height at `window.innerHeight * 0.55`.

- [ ] **Step 1: Apply the change**

Find the block at lines 554–572 in `frontend/src/components/MITREHeatmap.tsx`:

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

Replace it with:

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
      setHeatmapBodyHeight(Math.min(total - toolbar, window.innerHeight * 0.55));
    };
    const obs = new ResizeObserver(update);
    if (containerRef.current) obs.observe(containerRef.current);
    if (toolbarRef.current)   obs.observe(toolbarRef.current);
    update();
    return () => obs.disconnect();
  }, []);
```

- [ ] **Step 2: Verify TypeScript compiles cleanly**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output (no errors). `window.innerHeight` is typed as `number` in `lib.dom.d.ts` which Vite includes by default.

- [ ] **Step 3: Start the dev server and verify visually**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run dev
```

Open the browser. Navigate to the MITRE view. Verify:

1. **Recon** (43 techniques) has a vertical scrollbar on the right edge of the column — content overflows the capped height
2. **All other columns** that have more techniques than fit in the capped height also scroll independently
3. **Short columns** (e.g. Lateral Movement with ~9 techniques) do not scroll — their content fits within the cap
4. Scrolling within a column moves only that column's techniques; toolbar and all other columns stay fixed
5. Horizontal scroll across all 14 tactic columns still works
6. Resize the browser window — column heights update immediately (ResizeObserver fires, cap recomputed)
7. Switch to Graph view and back to Heatmap — no errors, scrolling still works correctly

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/MITREHeatmap.tsx
git commit -m "fix(heatmap): cap scroll container at 55vh so tall columns always scroll on large monitors"
```

---

## Self-Review

**Spec coverage:**
- ✅ `Math.min(total - toolbar, window.innerHeight * 0.55)` → Task 1 Step 1
- ✅ Cap recomputed on every ResizeObserver fire → comment in Step 1 explains this, same `update()` call site
- ✅ No CSS changes → only `.tsx` modified
- ✅ Cell size unchanged (26px) → not touched
- ✅ Column width unchanged (140px) → not touched

**Placeholder scan:** No TBDs. All code is complete. Visual verification steps are specific with exact expected outcomes.

**Type consistency:** `heatmapBodyHeight` is `number` (from `useState<number>`). `Math.min(number, number)` returns `number`. Applied to `style={{ height: number }}` — consistent throughout.
