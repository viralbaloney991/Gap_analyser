# Suggestions Panel Scroll Fix — Design Spec
**Date:** 2026-04-24  
**Status:** Approved for implementation

---

## Problem

The `.detail-panel` (technique detail panel in `MITREHeatmap.tsx`) has no `max-height`, so when the `SuggestionsPanel` renders multiple suggestion cards the panel grows beyond the viewport bottom. No scrollbar appears — cards are clipped by the viewport edge and unreachable.

Root cause: `.detail-panel` is `position: fixed; bottom: 24px` with `overflow: hidden` but no height constraint, so it expands to fit content. `.detail-panel-body` has no `overflow-y: auto`.

---

## Solution

Two CSS-only changes to `App.css`. No JSX changes, no new files.

1. **Cap the panel height and switch to flex-column layout** on `.detail-panel`
2. **Make `.detail-panel-body` fill remaining space and scroll**

The `.detail-panel-header` (technique ID, name, tactic, close button) stays pinned at the top as a natural-height flex child. The body scrolls beneath it.

---

## Architecture

### Files changed

| File | Change |
|------|--------|
| `frontend/src/App.css` | Add `max-height`, `display:flex`, `flex-direction:column` to `.detail-panel`; add `flex:1`, `overflow-y:auto`, `min-height:0` to `.detail-panel-body` |

### No other files touched.

---

## Implementation Detail

### `.detail-panel` (line 741)

```css
/* Before: */
.detail-panel {
  position: fixed;
  bottom: 24px; right: 24px;
  width: 320px;
  background: var(--surface);
  border: 1px solid var(--border-bright);
  border-radius: var(--radius-xl);
  box-shadow: 0 8px 32px rgba(0,0,0,0.4), 0 0 0 1px rgba(16,185,129,0.08);
  overflow: hidden;
  z-index: 100;
}

/* After: */
.detail-panel {
  position: fixed;
  bottom: 24px; right: 24px;
  width: 320px;
  max-height: calc(100vh - 48px);
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border: 1px solid var(--border-bright);
  border-radius: var(--radius-xl);
  box-shadow: 0 8px 32px rgba(0,0,0,0.4), 0 0 0 1px rgba(16,185,129,0.08);
  overflow: hidden;
  z-index: 100;
}
```

`max-height: calc(100vh - 48px)` — 24px for `bottom` offset + 24px top breathing room. On any viewport, the panel never taller than the visible screen.

### `.detail-panel-body` (line 760)

```css
/* Before: */
.detail-panel-body { padding: 14px 16px; }

/* After: */
.detail-panel-body { padding: 14px 16px; flex: 1; overflow-y: auto; min-height: 0; }
```

`flex: 1` — body grows to fill all space below the pinned header.  
`overflow-y: auto` — scrollbar appears only when content exceeds available height.  
`min-height: 0` — required: flex children default to `min-height: auto` (content size), which prevents shrinking. Without this the body still overflows despite `flex: 1`.

---

## Behaviour after fix

| Scenario | Before | After |
|----------|--------|-------|
| 1 suggestion card | Panel fits, no scroll | Same — no scroll needed |
| 3–5 suggestion cards | Panel grows past viewport, cards clipped | Panel capped at `100vh - 48px`, body scrolls |
| Window resize | Panel grows unconstrained | Cap recomputed via `calc(100vh - 48px)` — always fits |
| Header visibility | Header scrolls away with content | Header always pinned at top |

---

## Out of Scope

- Changes to suggestion card layout or content
- Changes to `SuggestionsPanel` JSX
- Changes to `MITREHeatmap.tsx`
- Any other component
