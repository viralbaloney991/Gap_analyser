# Suggestions Panel Scroll Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the technique detail panel scroll when suggestion cards overflow the viewport, with the panel header (technique ID/name) pinned at the top.

**Architecture:** Two CSS rule changes in `App.css`. `.detail-panel` gets `max-height: calc(100vh - 48px)` + `display:flex; flex-direction:column` so it never exceeds the viewport. `.detail-panel-body` gets `flex:1; overflow-y:auto; min-height:0` so it fills the remaining space below the pinned header and scrolls. No JSX changes, no new files.

**Tech Stack:** CSS, React 19, TypeScript (Vite). No new packages.

---

## Files

| File | Action |
|------|--------|
| `frontend/src/App.css` | Modify `.detail-panel` (line 741) and `.detail-panel-body` (line 760) |

---

## Task 1 — Apply the two CSS changes

**Files:**
- Modify: `frontend/src/App.css` (lines 741–751 and line 760)

**Context:** `.detail-panel` is `position:fixed; bottom:24px; right:24px` with `overflow:hidden` but no height constraint, so it grows with content past the viewport bottom. The fix is to cap it with `max-height` and use flex layout so the header stays pinned and the body scrolls.

- [ ] **Step 1: Update `.detail-panel`**

Find this block at line 741 in `frontend/src/App.css`:

```css
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
```

Replace with:

```css
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

- [ ] **Step 2: Update `.detail-panel-body`**

Find this line at line 760 in `frontend/src/App.css`:

```css
.detail-panel-body       { padding: 14px 16px; }
```

Replace with:

```css
.detail-panel-body       { padding: 14px 16px; flex: 1; overflow-y: auto; min-height: 0; }
```

`min-height: 0` is required — without it, flex children default to `min-height: auto` (their content size), which prevents the body from shrinking to trigger scroll.

- [ ] **Step 3: Verify TypeScript compiles cleanly**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output (no errors). CSS changes have no TypeScript impact; this confirms no accidental file corruption.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/App.css
git commit -m "fix(detail-panel): pin header, scroll body when suggestions overflow viewport"
```

---

## Self-Review

**Spec coverage:**
- ✅ `max-height: calc(100vh - 48px)` on `.detail-panel` → Step 1
- ✅ `display:flex; flex-direction:column` on `.detail-panel` → Step 1
- ✅ `flex:1; overflow-y:auto; min-height:0` on `.detail-panel-body` → Step 2
- ✅ Header stays pinned (natural-height flex child, no grow) → achieved by Steps 1+2 together
- ✅ No JSX changes → only `.css` modified
- ✅ No other files touched → single file, single task

**Placeholder scan:** No TBDs. All CSS is complete and exact.

**Type consistency:** Pure CSS — no types involved.
