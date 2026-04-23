# Frontend Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the neon-green terminal aesthetic with a refined dark design system (Red Hat Display + IBM Plex Mono, emerald accent, navy-black backgrounds) across all four views, and add loading/error/empty states plus Framer Motion view transitions.

**Architecture:** Full App.css rewrite with new design tokens; targeted TSX updates per component to adopt new class names, add functional states, and restructure key JSX sections (ClientSelector search/CTA, AlertInsights card types, MITREHeatmap toolbar).

**Tech Stack:** React 19, TypeScript, Vite, Framer Motion (new), Red Hat Display via Google Fonts (new), IBM Plex Mono (existing)

**Spec:** `docs/superpowers/specs/2026-04-23-frontend-redesign.md`

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `frontend/index.html` | Modify | Add Red Hat Display Google Fonts `<link>` |
| `frontend/package.json` | Modify | Add `framer-motion` dependency |
| `frontend/src/App.css` | Full rewrite | New design system — all tokens, components, view styles |
| `frontend/src/App.tsx` | Modify | AnimatePresence transitions, header breadcrumb, new classes |
| `frontend/src/components/ClientSelector.tsx` | Modify | Search filter, sticky CTA, new class names |
| `frontend/src/components/IntegrationSummary.tsx` | Modify | New class names, header restructure, error/empty states |
| `frontend/src/components/AlertInsights.tsx` | Modify | Model selector, card types, signals grid, skeleton/error/empty |
| `frontend/src/components/MITREHeatmap.tsx` | Modify | New coverage colors, toolbar, hover tooltip, suggestion error |

---

## Task 1: Install Dependencies

**Files:**
- Modify: `frontend/index.html`
- Modify: `frontend/package.json`

- [ ] **Step 1: Add Red Hat Display font to index.html**

Open `frontend/index.html`. Replace the existing `<link>` tags in `<head>` (currently only IBM Plex Mono) with:

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Red+Hat+Display:wght@300;400;500;600;700;800;900&family=IBM+Plex+Mono:wght@300;400;500&display=swap" rel="stylesheet">
```

- [ ] **Step 2: Install framer-motion**

```bash
cd frontend && npm install framer-motion
```

Expected: `added N packages` with no errors.

- [ ] **Step 3: Verify dev server starts**

```bash
cd frontend && npm run dev
```

Expected: `VITE ready in Xms` with no TypeScript errors. Open `http://localhost:5173` and confirm the UI loads (old styles are fine — CSS rewrite is next).

- [ ] **Step 4: Commit**

```bash
cd frontend && git add index.html package.json package-lock.json
git commit -m "chore(frontend): add Red Hat Display font and framer-motion"
```

---

## Task 2: Rewrite App.css

**Files:**
- Full rewrite: `frontend/src/App.css`

This replaces the entire 1437-line file with the new design system.

- [ ] **Step 1: Replace App.css with the new design system**

Overwrite `frontend/src/App.css` with:

```css
/* =====================================================================
   CXAlert Analyzer — Design System
   Aesthetic: Refined dark + emerald brand green
   Fonts: Red Hat Display (display) + IBM Plex Mono (data/code)
   ===================================================================== */

/* ---- DESIGN TOKENS ---- */
:root {
  /* Backgrounds */
  --bg:            #080d14;
  --bg-mid:        #0e1520;
  --surface:       #131c2b;
  --surface-2:     #1a2537;

  /* Borders */
  --border:        rgba(16, 185, 129, 0.12);
  --border-bright: rgba(16, 185, 129, 0.32);

  /* Accent — Emerald */
  --accent:        #10b981;
  --accent-dim:    rgba(16, 185, 129, 0.10);

  /* Text — Slate scale */
  --text:          #f1f5f9;
  --text-sec:      #94a3b8;
  --text-dim:      #64748b;

  /* Semantic */
  --danger:        #ef4444;
  --danger-dim:    rgba(239, 68, 68, 0.10);
  --warn:          #f59e0b;
  --warn-dim:      rgba(245, 158, 11, 0.10);
  --indigo:        #818cf8;
  --indigo-dim:    rgba(99, 102, 241, 0.12);
  --sky:           #38bdf8;
  --sky-dim:       rgba(56, 189, 248, 0.10);

  /* Typography */
  --font-display:  'Red Hat Display', sans-serif;
  --font-mono:     'IBM Plex Mono', 'JetBrains Mono', monospace;

  /* Shape */
  --radius-sm:     4px;
  --radius-md:     6px;
  --radius-lg:     8px;
  --radius-xl:     10px;

  /* Coverage colours (MITREHeatmap) */
  --cov-none:      #1e2535;
  --cov-low:       #7c2d12;
  --cov-partial:   #92400e;
  --cov-good:      #065f46;
  --cov-full:      #10b981;
}

/* ---- RESET & BASE ---- */
*, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }

html, body, #root {
  height: 100%;
  background: var(--bg);
  color: var(--text);
  font-family: var(--font-display);
  font-size: 14px;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

::-webkit-scrollbar { width: 6px; height: 6px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--border-bright); border-radius: 3px; }
::-webkit-scrollbar-thumb:hover { background: var(--accent); }

/* ---- APP SHELL ---- */
.app { height: 100vh; display: flex; flex-direction: column; overflow: hidden; }

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  height: 52px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  background: var(--bg);
  z-index: 50;
}

.header-left  { display: flex; align-items: center; gap: 16px; }
.header-right { display: flex; align-items: center; gap: 10px; }

.app-logo {
  font-family: var(--font-display);
  font-size: 1rem;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--text);
  cursor: pointer;
  background: none;
  border: none;
}

.app-logo em { color: var(--accent); font-style: normal; }

.app-breadcrumb {
  font-family: var(--font-mono);
  font-size: 0.62rem;
  color: var(--text-dim);
  display: flex;
  align-items: center;
  gap: 6px;
}

.app-breadcrumb span { color: var(--text-sec); }
.app-breadcrumb-sep  { color: var(--border-bright); }

.header-status {
  font-family: var(--font-mono);
  font-size: 0.62rem;
  color: var(--accent);
  display: flex;
  align-items: center;
  gap: 6px;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 6px var(--accent);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.app-main {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.app-main--landing { overflow-y: auto; }

.error-banner {
  margin: 16px 24px;
  padding: 12px 16px;
  background: var(--danger-dim);
  border: 1px solid rgba(239, 68, 68, 0.25);
  border-radius: var(--radius-lg);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  color: var(--danger);
}

/* ---- SHARED COMPONENTS ---- */

/* Buttons */
.btn {
  font-family: var(--font-mono);
  font-size: 0.72rem;
  font-weight: 500;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  padding: 9px 18px;
  border-radius: var(--radius-sm);
  border: none;
  cursor: pointer;
  transition: all 0.15s;
}

.btn-primary { background: var(--accent); color: var(--bg); }
.btn-primary:hover { opacity: 0.9; }

.btn-secondary {
  background: transparent;
  color: var(--accent);
  border: 1px solid var(--border-bright);
}
.btn-secondary:hover { background: var(--accent-dim); }

.btn-small {
  font-family: var(--font-mono);
  font-size: 0.62rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding: 5px 11px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-bright);
  background: transparent;
  color: var(--accent);
  cursor: pointer;
  transition: all 0.15s;
}
.btn-small:hover { background: var(--accent-dim); }
.btn-small:disabled { opacity: 0.5; cursor: not-allowed; }

/* Badges */
.badge {
  display: inline-block;
  font-family: var(--font-mono);
  font-size: 0.58rem;
  font-weight: 500;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding: 3px 8px;
  border-radius: var(--radius-sm);
}

.badge--green  { background: var(--accent-dim);              color: var(--accent); }
.badge--red    { background: var(--danger-dim);              color: var(--danger); }
.badge--amber  { background: var(--warn-dim);                color: var(--warn); }
.badge--slate  { background: rgba(100,116,139,0.18);         color: var(--text-sec); }
.badge--indigo { background: var(--indigo-dim);              color: var(--indigo); }
.badge--sky    { background: var(--sky-dim);                 color: var(--sky); }

.cache-badge {
  font-family: var(--font-mono);
  font-size: 0.58rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  padding: 3px 8px;
  border-radius: var(--radius-sm);
  background: var(--indigo-dim);
  color: var(--indigo);
  border: 1px solid rgba(99, 102, 241, 0.2);
}

/* Skeleton shimmer */
.skeleton {
  background: linear-gradient(
    90deg,
    var(--surface) 25%,
    var(--surface-2) 50%,
    var(--surface) 75%
  );
  background-size: 200% 100%;
  animation: shimmer 1.4s ease-in-out infinite;
  border-radius: var(--radius-sm);
}

@keyframes shimmer {
  0%   { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

/* Error state */
.state-error {
  padding: 16px;
  background: var(--danger-dim);
  border: 1px solid rgba(239, 68, 68, 0.25);
  border-radius: var(--radius-lg);
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.state-error__icon  { color: var(--danger); font-size: 1rem; flex-shrink: 0; margin-top: 1px; }
.state-error__title { font-size: 0.875rem; font-weight: 600; color: var(--danger); margin-bottom: 4px; }
.state-error__body  { font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-sec); line-height: 1.5; }
.state-error__retry { font-family: var(--font-mono); font-size: 0.65rem; color: var(--accent); cursor: pointer; margin-top: 6px; text-decoration: underline; background: none; border: none; padding: 0; display: block; }

/* Empty state */
.state-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 48px;
  text-align: center;
}

.state-empty__icon  { font-size: 2rem; opacity: 0.3; color: var(--text-dim); }
.state-empty__title { font-size: 0.9rem; font-weight: 600; color: var(--text-sec); }
.state-empty__body  { font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-dim); line-height: 1.6; max-width: 260px; }

/* Eyebrow label */
.eyebrow {
  font-family: var(--font-mono);
  font-size: 0.58rem;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--text-dim);
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.eyebrow::after { content: ''; flex: 1; height: 1px; background: var(--border); }

/* ================================================================
   CLIENT SELECTOR
   ================================================================ */
.client-selector {
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-start;
  padding: 64px 32px 100px;
  position: relative;
}

/* Subtle grid background */
.client-selector::before {
  content: '';
  position: fixed;
  inset: 0;
  pointer-events: none;
  background-image:
    linear-gradient(rgba(16, 185, 129, 0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(16, 185, 129, 0.04) 1px, transparent 1px);
  background-size: 40px 40px;
  mask-image: radial-gradient(ellipse 80% 60% at 50% 50%, black, transparent);
}

/* Hero */
.cs-hero {
  text-align: center;
  margin-bottom: 40px;
  position: relative;
  z-index: 1;
}

.cs-eyebrow {
  font-family: var(--font-mono);
  font-size: 0.65rem;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--accent);
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
}
.cs-eyebrow::before,
.cs-eyebrow::after { content: ''; display: block; width: 32px; height: 1px; background: var(--border-bright); }

.cs-title {
  font-family: var(--font-display);
  font-size: 3rem;
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1.1;
  color: var(--text);
}
.cs-title em { color: var(--accent); font-style: normal; }

.cs-subtitle {
  margin-top: 12px;
  font-size: 0.875rem;
  color: var(--text-sec);
  font-weight: 400;
  line-height: 1.6;
}

/* Search bar */
.cs-search {
  display: flex;
  align-items: center;
  border: 1px solid var(--border-bright);
  border-radius: var(--radius-md);
  background: var(--surface);
  overflow: hidden;
  margin-bottom: 36px;
  width: 100%;
  max-width: 480px;
  position: relative;
  z-index: 1;
}
.cs-search__icon  { font-family: var(--font-mono); font-size: 0.8rem; color: var(--text-dim); padding: 0 12px; border-right: 1px solid var(--border); line-height: 38px; }
.cs-search__input { flex: 1; background: transparent; border: none; outline: none; font-family: var(--font-mono); font-size: 0.78rem; color: var(--text); padding: 10px 12px; }
.cs-search__input::placeholder { color: var(--text-dim); }
.cs-search__count { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); padding: 0 12px; border-left: 1px solid var(--border); white-space: nowrap; }

/* Content */
.cs-content { width: 100%; max-width: 960px; position: relative; z-index: 1; }

/* Region groups */
.region-group        { margin-bottom: 32px; }
.region-group-label  {
  font-family: var(--font-mono);
  font-size: 0.6rem;
  letter-spacing: 0.15em;
  text-transform: uppercase;
  color: var(--text-dim);
  margin-bottom: 12px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.region-group-label::after { content: ''; flex: 1; height: 1px; background: var(--border); }

.region-group-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 8px;
}

/* Client cards */
.region-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: 16px;
  cursor: pointer;
  transition: all 0.15s;
  position: relative;
  overflow: hidden;
  text-align: left;
  width: 100%;
}
.region-card:hover              { border-color: var(--border-bright); background: var(--surface-2); transform: translateY(-1px); }
.region-card:disabled           { opacity: 0.5; cursor: not-allowed; transform: none; }
.region-card--selected          { border-color: var(--accent); background: var(--accent-dim); }
.region-card--selected::before  { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 2px; background: var(--accent); }
.region-card--skeleton          { background: var(--surface); border-color: var(--border); pointer-events: none; height: 76px; }

.region-card__name   { font-family: var(--font-display); font-weight: 600; font-size: 0.875rem; color: var(--text); margin-bottom: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.region-card__meta   { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); display: flex; align-items: center; justify-content: space-between; }
.region-card__alerts { font-family: var(--font-mono); font-size: 0.62rem; color: var(--accent); margin-top: 8px; }
.region-card__dot    { position: absolute; top: 14px; right: 14px; width: 5px; height: 5px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 5px var(--accent); animation: pulse 2.5s ease-in-out infinite; }

/* Sticky CTA */
.cs-cta {
  position: fixed;
  bottom: 0; left: 0; right: 0;
  padding: 16px 32px;
  background: linear-gradient(0deg, var(--bg) 70%, transparent);
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 16px;
  z-index: 20;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
}
.cs-cta--visible { opacity: 1; pointer-events: all; }
.cs-cta__hint    { font-family: var(--font-mono); font-size: 0.65rem; color: var(--text-dim); }
.cs-cta__btn     { font-family: var(--font-mono); font-size: 0.75rem; font-weight: 500; letter-spacing: 0.1em; text-transform: uppercase; background: var(--accent); color: var(--bg); border: none; border-radius: var(--radius-sm); padding: 12px 28px; cursor: pointer; transition: opacity 0.15s; }
.cs-cta__btn:hover    { opacity: 0.9; }
.cs-cta__btn:disabled { opacity: 0.5; cursor: not-allowed; }

/* Loading skeletons */
.cs-skeletons { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 8px; }

/* Empty search result */
.cs-empty { text-align: center; padding: 48px 24px; color: var(--text-dim); font-family: var(--font-mono); font-size: 0.78rem; }

/* Fetch error */
.map-error { margin: 0 0 16px; }

/* ================================================================
   INTEGRATION SUMMARY
   ================================================================ */
.integration-summary {
  flex: 1;
  display: grid;
  grid-template-columns: 240px 1fr;
  min-height: 0;
}

/* Left panel */
.summary-panel { border-right: 1px solid var(--border); display: flex; flex-direction: column; overflow-y: auto; padding: 24px 20px; }

.summary-panel-header { margin-bottom: 24px; }

.summary-client-label { font-family: var(--font-mono); font-size: 0.58rem; text-transform: uppercase; letter-spacing: 0.14em; color: var(--text-dim); margin-bottom: 6px; }
.summary-client-name  { font-family: var(--font-display); font-size: 1.1rem; font-weight: 700; letter-spacing: -0.01em; color: var(--text); line-height: 1.2; }

/* Stats */
.summary-panel-stats { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-bottom: 24px; }

.stat-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: 14px 12px;
  position: relative;
  overflow: hidden;
}
.stat-card::before { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 1px; background: linear-gradient(90deg, transparent, var(--accent), transparent); opacity: 0.4; }
.stat-card--danger { background: var(--danger-dim); border-color: rgba(239, 68, 68, 0.25); }
.stat-card--danger::before { background: linear-gradient(90deg, transparent, var(--danger), transparent); }

.stat-number { font-family: var(--font-display); font-size: 1.6rem; font-weight: 800; letter-spacing: -0.03em; color: var(--text); line-height: 1; margin-bottom: 4px; }
.stat-card--danger .stat-number { color: var(--danger); }
.stat-desc   { font-family: var(--font-mono); font-size: 0.58rem; text-transform: uppercase; letter-spacing: 0.1em; color: var(--text-dim); }

/* Actions */
.summary-panel-actions-label { font-family: var(--font-mono); font-size: 0.58rem; text-transform: uppercase; letter-spacing: 0.14em; color: var(--text-dim); margin-bottom: 10px; padding-bottom: 6px; border-bottom: 1px solid var(--border); }
.summary-panel-actions { display: flex; flex-direction: column; gap: 6px; }

.btn-action {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-sec);
  cursor: pointer;
  transition: all 0.15s;
  width: 100%;
  text-align: left;
}
.btn-action:hover      { background: var(--accent-dim); border-color: var(--border-bright); color: var(--text); }
.btn-action--active    { background: var(--accent-dim); border-color: var(--border-bright); color: var(--accent); }

.action-icon { width: 28px; height: 28px; border-radius: 5px; background: var(--surface-2); display: flex; align-items: center; justify-content: center; font-size: 0.8rem; flex-shrink: 0; }
.btn-action--active .action-icon { background: rgba(16, 185, 129, 0.18); }

.action-title { font-family: var(--font-display); font-size: 0.8rem; font-weight: 600; display: block; }
.action-desc  { font-family: var(--font-mono); font-size: 0.58rem; color: var(--text-dim); margin-top: 2px; display: block; }
.btn-action--active .action-desc { color: var(--accent); opacity: 0.7; }

/* Right panel */
.summary-table-panel  { display: flex; flex-direction: column; overflow: hidden; }
.summary-table-header { padding: 20px 24px 16px; border-bottom: 1px solid var(--border); flex-shrink: 0; display: flex; align-items: center; justify-content: space-between; }
.summary-table-title  { font-family: var(--font-display); font-size: 1rem; font-weight: 700; }
.summary-table-sub    { font-family: var(--font-mono); font-size: 0.65rem; color: var(--text-dim); margin-top: 3px; }
.coverage-pct         { font-family: var(--font-display); font-size: 1.5rem; font-weight: 800; letter-spacing: -0.03em; color: var(--accent); }
.coverage-label       { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.1em; margin-top: 2px; }

.integration-table-wrap { flex: 1; overflow-y: auto; }

.integration-table { width: 100%; border-collapse: collapse; }
.integration-table thead tr { position: sticky; top: 0; background: var(--bg); border-bottom: 1px solid var(--border); }
.integration-table th { font-family: var(--font-mono); font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.12em; color: var(--text-dim); font-weight: 400; padding: 10px 16px; text-align: left; }
.integration-table th:last-child { text-align: right; }
.integration-table td { padding: 12px 16px; border-bottom: 1px solid rgba(255,255,255,0.03); vertical-align: middle; }
.integration-table tr:hover td { background: var(--surface); }
.row-blind-spot td { background: rgba(239, 68, 68, 0.04); }
.row-blind-spot:hover td { background: rgba(239, 68, 68, 0.08); }

.integration-name { font-family: var(--font-display); font-size: 0.875rem; font-weight: 600; color: var(--text); }
.integration-type { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); margin-top: 2px; }
.alert-count      { font-family: var(--font-mono); font-size: 0.8rem; font-weight: 500; color: var(--text); text-align: right; }

.status-tag       { display: inline-block; font-family: var(--font-mono); font-size: 0.58rem; font-weight: 500; letter-spacing: 0.06em; text-transform: uppercase; padding: 3px 7px; border-radius: var(--radius-sm); }
.status-tag--ok   { background: var(--accent-dim); color: var(--accent); }
.status-tag--blind{ background: var(--danger-dim); color: var(--danger); }

.summary-state-wrap { padding: 20px 24px; }

/* ================================================================
   ALERT INSIGHTS
   ================================================================ */
.alert-insights {
  flex: 1;
  display: grid;
  grid-template-columns: 260px 1fr;
  min-height: 0;
}

/* Left panel */
.insights-panel { border-right: 1px solid var(--border); display: flex; flex-direction: column; overflow: hidden; }

.insights-model-header { padding: 14px 16px; border-bottom: 1px solid var(--border); flex-shrink: 0; display: flex; align-items: center; gap: 8px; }

.insights-model-select { flex: 1; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 7px 10px; font-family: var(--font-mono); font-size: 0.68rem; color: var(--text); appearance: none; cursor: pointer; }

.insights-regenerate-btn { flex-shrink: 0; padding: 7px 12px; border-radius: var(--radius-sm); background: var(--accent); border: none; cursor: pointer; font-family: var(--font-mono); font-size: 0.68rem; color: var(--bg); font-weight: 500; transition: opacity 0.15s; }
.insights-regenerate-btn:disabled   { opacity: 0.5; cursor: not-allowed; }
.insights-regenerate-btn:hover:not(:disabled) { opacity: 0.9; }

.insights-panel-scroll { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 20px; }

.insights-summary-text { font-family: var(--font-display); font-size: 0.8rem; color: var(--text-sec); line-height: 1.65; }

/* Skeleton lines in left panel */
.insights-skeleton { height: 12px; margin-bottom: 8px; }
.insights-skeleton:last-child { width: 70% !important; margin-bottom: 0; }

/* Priority list */
.insights-priority-list { list-style: none; display: flex; flex-direction: column; gap: 6px; }

.insights-priority-item { display: flex; gap: 10px; align-items: flex-start; padding: 10px 12px; border-radius: var(--radius-md); background: var(--surface); border: 1px solid var(--border); }
.insights-priority-num  { font-family: var(--font-mono); font-size: 0.62rem; color: var(--accent); font-weight: 500; flex-shrink: 0; margin-top: 1px; }
.insights-priority-text { font-family: var(--font-display); font-size: 0.78rem; color: var(--text-sec); line-height: 1.5; }

/* Signals grid */
.insights-signals-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
.insights-signal-pill  { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 8px 10px; display: flex; flex-direction: column; gap: 2px; }
.signal-count          { font-family: var(--font-mono); font-size: 0.95rem; font-weight: 500; color: var(--text); }
.signal-label          { font-family: var(--font-mono); font-size: 0.56rem; text-transform: uppercase; letter-spacing: 0.1em; color: var(--text-dim); }

/* Tabs */
.insights-tabs { display: flex; border-bottom: 1px solid var(--border); flex-shrink: 0; padding: 0 20px; gap: 2px; overflow-x: auto; }

.tab-btn { font-family: var(--font-mono); font-size: 0.65rem; letter-spacing: 0.06em; text-transform: uppercase; padding: 14px 14px 12px; color: var(--text-dim); cursor: pointer; border: none; background: none; border-bottom: 2px solid transparent; transition: all 0.15s; white-space: nowrap; display: flex; align-items: center; gap: 7px; }
.tab-btn:hover     { color: var(--text-sec); }
.tab-btn--active   { color: var(--accent); border-bottom-color: var(--accent); }

.tab-count { background: var(--surface-2); border-radius: 10px; padding: 1px 6px; font-size: 0.58rem; color: var(--text-dim); min-width: 18px; text-align: center; }
.tab-btn--active .tab-count { background: var(--accent-dim); color: var(--accent); }

/* Tab content */
.insights-tab-content { flex: 1; overflow-y: auto; padding: 20px; display: flex; flex-direction: column; gap: 12px; }

/* Base insight card */
.insight-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 16px;
  position: relative;
  overflow: hidden;
}

/* Card type left-border variants — no top-edge gradient for these */
.insight-card--duplicate { border-left: 3px solid var(--indigo); }
.insight-card--noise     { border-left: 3px solid var(--danger); }
.insight-card--family    { border-left: 3px solid var(--accent); }
.insight-card--merge     { border-left: 3px solid var(--warn);   }
.insight-card--coverage  { border-left: 3px solid var(--sky);    }

.insight-card-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.insight-card-title  { font-family: var(--font-display); font-size: 0.875rem; font-weight: 600; color: var(--text); }
.insight-card-body   { font-family: var(--font-display); font-size: 0.8rem; color: var(--text-sec); line-height: 1.6; margin-top: 10px; }

/* Alert pair (duplicate) */
.alert-pair     { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.alert-tag      { font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-sec); background: var(--surface-2); padding: 4px 8px; border-radius: var(--radius-sm); border: 1px solid var(--border); }
.alert-pair-sep { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); }

/* Similarity bar */
.sim-bar-wrap  { display: flex; align-items: center; gap: 10px; margin-top: 10px; }
.sim-bar-track { flex: 1; height: 4px; background: var(--surface-2); border-radius: 2px; overflow: hidden; }
.sim-bar-fill  { height: 100%; border-radius: 2px; }
.sim-bar-label { font-family: var(--font-mono); font-size: 0.65rem; color: var(--text-sec); flex-shrink: 0; min-width: 70px; }

/* Missing features (noise) */
.missing-features { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.missing-tag { font-family: var(--font-mono); font-size: 0.6rem; background: var(--danger-dim); color: var(--danger); border: 1px solid rgba(239, 68, 68, 0.2); padding: 3px 8px; border-radius: var(--radius-sm); }

/* Recommendation items */
.rec-item { display: flex; gap: 14px; align-items: flex-start; padding: 14px 16px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.rec-num  { font-family: var(--font-display); font-size: 1.4rem; font-weight: 800; color: var(--text); opacity: 0.2; flex-shrink: 0; line-height: 1; min-width: 32px; }
.rec-text { font-family: var(--font-display); font-size: 0.85rem; color: var(--text-sec); line-height: 1.6; padding-top: 2px; }

/* Inline code in card body */
.insight-card-body code { font-family: var(--font-mono); font-size: 0.75rem; color: var(--accent); background: var(--accent-dim); padding: 1px 5px; border-radius: 3px; }

/* ================================================================
   MITRE HEATMAP
   ================================================================ */
.mitre-heatmap { flex: 1; display: flex; flex-direction: column; overflow: hidden; }

/* Toolbar */
.mitre-toolbar { display: flex; align-items: center; justify-content: space-between; padding: 12px 24px; border-bottom: 1px solid var(--border); flex-shrink: 0; gap: 16px; }

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

/* Heatmap scroll */
.heatmap-scroll-wrap { flex: 1; overflow-x: auto; overflow-y: hidden; display: flex; }
.heatmap-columns     { display: flex; flex: 1; }

/* Tactic columns */
.tactic-col { display: flex; flex-direction: column; border-right: 1px solid var(--border); min-width: 110px; flex: 1; overflow: hidden; }
.tactic-col:last-child { border-right: none; }

.tactic-header { padding: 10px 10px 8px; border-bottom: 1px solid var(--border); flex-shrink: 0; position: relative; }
.tactic-header::before { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 3px; background: var(--cov-color, var(--cov-none)); }

.tactic-name       { font-family: var(--font-display); font-size: 0.72rem; font-weight: 600; color: var(--text); line-height: 1.3; margin-bottom: 4px; }
.tactic-count      { font-family: var(--font-mono); font-size: 0.58rem; color: var(--text-dim); }
.tactic-count em   { font-style: normal; color: var(--accent); }
.tactic-count--zero { color: var(--danger); }
.tactic-count--zero em { color: var(--danger); }

/* Technique cells */
.tactic-techniques { flex: 1; overflow-y: auto; padding: 6px; display: flex; flex-direction: column; gap: 3px; }

.tech-cell { height: 26px; border-radius: 3px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: opacity 0.1s; position: relative; }
.tech-cell:hover          { opacity: 0.75; }
.tech-cell--selected      { outline: 2px solid #fff; outline-offset: -1px; }

.tech-id { font-family: var(--font-mono); font-size: 0.55rem; color: rgba(255,255,255,0.7); }

/* Hover tooltip */
.tech-tooltip {
  position: fixed;
  background: var(--surface-2);
  border: 1px solid var(--border-bright);
  border-radius: var(--radius-sm);
  padding: 6px 10px;
  font-family: var(--font-mono);
  font-size: 0.65rem;
  color: var(--text);
  pointer-events: none;
  z-index: 200;
  white-space: nowrap;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
}

/* Detail panel */
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

.detail-panel-header { padding: 14px 16px; border-bottom: 1px solid var(--border); display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; }
.detail-tech-id   { font-family: var(--font-mono); font-size: 0.68rem; color: var(--accent); margin-bottom: 4px; }
.detail-tech-name { font-family: var(--font-display); font-size: 0.875rem; font-weight: 600; color: var(--text); }
.detail-tactic    { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); margin-top: 3px; }
.detail-close     { background: none; border: none; color: var(--text-dim); cursor: pointer; font-size: 1rem; flex-shrink: 0; padding: 2px; line-height: 1; }
.detail-close:hover { color: var(--text); }

.detail-panel-body       { padding: 14px 16px; }
.detail-panel-empty      { padding: 24px 16px; text-align: center; color: var(--text-dim); font-family: var(--font-mono); font-size: 0.68rem; }
.detail-suggestion-bar   { display: flex; gap: 6px; margin-bottom: 10px; }
.detail-provider-select  { flex: 1; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 6px 8px; font-family: var(--font-mono); font-size: 0.65rem; color: var(--text); }

.btn-generate { background: var(--accent); border: none; border-radius: var(--radius-sm); padding: 6px 10px; font-family: var(--font-mono); font-size: 0.65rem; color: var(--bg); font-weight: 500; cursor: pointer; white-space: nowrap; transition: opacity 0.15s; }
.btn-generate:hover    { opacity: 0.9; }
.btn-generate:disabled { opacity: 0.5; cursor: not-allowed; }

.query-block    { background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 10px 12px; margin-bottom: 8px; }
.query-provider { font-family: var(--font-mono); font-size: 0.56rem; color: var(--accent); text-transform: uppercase; letter-spacing: 0.1em; margin-bottom: 6px; }
.query-text     { font-family: var(--font-mono); font-size: 0.65rem; color: var(--text-sec); line-height: 1.6; }
.query-field    { color: var(--accent); }

.suggestion-card        { background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 10px 12px; margin-bottom: 8px; }
.suggestion-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; }
.suggestion-name        { font-family: var(--font-display); font-size: 0.78rem; font-weight: 600; color: var(--text); }
.suggestion-desc        { font-family: var(--font-display); font-size: 0.75rem; color: var(--text-sec); line-height: 1.5; margin-bottom: 8px; }

/* Force graph container */
.force-graph-container { flex: 1; overflow: hidden; position: relative; }

/* Download button */
.mitre-download-btn { font-family: var(--font-mono); font-size: 0.62rem; letter-spacing: 0.06em; text-transform: uppercase; padding: 5px 11px; border-radius: var(--radius-sm); border: 1px solid var(--border-bright); background: transparent; color: var(--accent); cursor: pointer; transition: all 0.15s; }
.mitre-download-btn:hover { background: var(--accent-dim); }
```

- [ ] **Step 2: Verify dev server starts without CSS errors**

```bash
cd frontend && npm run dev
```

Open `http://localhost:5173`. The app should load. Styles will be broken because TSX class names haven't been updated yet — that's expected. Check the browser console has no CSS parse errors.

- [ ] **Step 3: Commit**

```bash
cd frontend && git add src/App.css
git commit -m "feat(frontend): rewrite App.css with new design system tokens and components"
```

---

## Task 3: Update App.tsx — Header, Breadcrumb, Framer Motion Transitions

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Replace App.tsx**

```tsx
import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import ClientSelector from './components/ClientSelector';
import IntegrationSummary from './components/IntegrationSummary';
import MITREHeatmap from './components/MITREHeatmap';
import AlertInsights from './components/AlertInsights';
import { analyzeClient, fetchInsights } from './services/api';
import type { AnalyzeResponse, InsightsReport } from './types';
import './App.css';

type View = 'form' | 'summary' | 'mitre' | 'insights';

const FADE_UP = {
  initial:    { opacity: 0, y: 8 },
  animate:    { opacity: 1, y: 0 },
  exit:       { opacity: 0, y: -8 },
  transition: { duration: 0.2, ease: 'easeOut' },
};

function App() {
  const [view, setView]                   = useState<View>('form');
  const [loading, setLoading]             = useState(false);
  const [error, setError]                 = useState('');
  const [data, setData]                   = useState<AnalyzeResponse | null>(null);
  const [clientName, setClientName]       = useState('');
  const [insightsReport, setInsightsReport] = useState<InsightsReport | null>(null);
  const [insightsError, setInsightsError] = useState(false);

  const handleAnalyze = async (client: string, refresh = false) => {
    setLoading(true);
    setError('');
    setInsightsReport(null);
    setInsightsError(false);
    try {
      const result = await analyzeClient(client, refresh);
      setData(result);
      setClientName(client);
      setView('summary');
      fetchInsights(client)
        .then(setInsightsReport)
        .catch((e) => { console.warn('[insights]', e); setInsightsError(true); });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Analysis failed');
    } finally {
      setLoading(false);
    }
  };

  const goBack = () => {
    if (view === 'mitre' || view === 'insights') {
      setView('summary');
    } else {
      setView('form');
      setData(null);
      setClientName('');
      setInsightsReport(null);
      setInsightsError(false);
    }
    setError('');
  };

  const goHome = () => {
    setView('form');
    setData(null);
    setClientName('');
    setInsightsReport(null);
    setInsightsError(false);
    setError('');
  };

  // Build breadcrumb segments for non-landing views
  const breadcrumb: { label: string }[] = view !== 'form' && clientName
    ? [
        { label: clientName },
        ...(view === 'summary'  ? [] : [
            { label: view === 'mitre' ? 'MITRE Coverage' : 'Alert Insights' }
           ]),
      ]
    : [];

  return (
    <div className="app">
      <header className="app-header">
        <div className="header-left">
          <button className="app-logo" onClick={goHome}>
            CX<em>Alert</em>
          </button>
          {breadcrumb.length > 0 && (
            <div className="app-breadcrumb">
              {breadcrumb.map((seg, i) => (
                <span key={i}>
                  {i > 0 && <span className="app-breadcrumb-sep">/</span>}
                  <span>{seg.label}</span>
                </span>
              ))}
            </div>
          )}
        </div>

        <div className="header-right">
          {view !== 'form' && (
            <button className="btn-small" onClick={goBack}>
              ← Back
            </button>
          )}
          {data?.cached && <span className="cache-badge">Cached</span>}
          <div className="header-status">
            <span className="status-dot" />
            ONLINE
          </div>
        </div>
      </header>

      <main className={`app-main${view === 'form' ? ' app-main--landing' : ''}`}>
        {error && <div className="error-banner">{error}</div>}

        <AnimatePresence mode="wait">
          {view === 'form' && (
            <motion.div key="form" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
              <ClientSelector onAnalyze={handleAnalyze} loading={loading} />
            </motion.div>
          )}

          {view === 'summary' && data && (
            <motion.div key="summary" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <IntegrationSummary
                data={data}
                clientName={clientName}
                loading={loading}
                onViewMITRE={() => setView('mitre')}
                onViewInsights={() => setView('insights')}
                onRefresh={() => handleAnalyze(clientName, true)}
              />
            </motion.div>
          )}

          {view === 'mitre' && data && (
            <motion.div key="mitre" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <MITREHeatmap data={data.mitre_coverage} clientName={clientName} />
            </motion.div>
          )}

          {view === 'insights' && data && (
            <motion.div key="insights" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <AlertInsights
                data={data.alert_insights}
                report={insightsReport}
                insightsError={insightsError}
                client={clientName}
              />
            </motion.div>
          )}
        </AnimatePresence>
      </main>
    </div>
  );
}

export default App;
```

- [ ] **Step 2: Verify transitions work**

```bash
cd frontend && npm run dev
```

Open `http://localhost:5173`. Select a client and click Analyze. The view should fade-and-slide between screens. Check the breadcrumb shows `ClientName` on the summary view and `ClientName / Alert Insights` on the insights view. No TypeScript errors in console.

- [ ] **Step 3: Commit**

```bash
cd frontend && git add src/App.tsx
git commit -m "feat(frontend): add Framer Motion view transitions and breadcrumb header"
```

---

## Task 4: ClientSelector — Search Filter + Sticky CTA + New Classes

**Files:**
- Modify: `frontend/src/components/ClientSelector.tsx`

- [ ] **Step 1: Replace ClientSelector.tsx**

```tsx
import { useState, useEffect, useRef } from 'react';
import { fetchClients } from '../services/api';
import type { ClientInfo } from '../types';

const REGION_META: Record<string, { label: string; city: string; group: string }> = {
  eu1: { label: 'EU1', city: 'Dublin',    group: 'Europe'       },
  eu2: { label: 'EU2', city: 'Stockholm', group: 'Europe'       },
  us1: { label: 'US1', city: 'Virginia',  group: 'Americas'     },
  us2: { label: 'US2', city: 'Oregon',    group: 'Americas'     },
  ap1: { label: 'AP1', city: 'Mumbai',    group: 'Asia Pacific' },
  ap2: { label: 'AP2', city: 'Singapore', group: 'Asia Pacific' },
  ap3: { label: 'AP3', city: 'Tokyo',     group: 'Asia Pacific' },
};

const GROUP_ORDER = ['Europe', 'Americas', 'Asia Pacific'];

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients]       = useState<ClientInfo[]>([]);
  const [selected, setSelected]     = useState('');
  const [fetchError, setFetchError] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const slowTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    fetchClients()
      .then(setClients)
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  // Filter clients by search query (case-insensitive, matches name or region)
  const filteredClients = searchQuery.trim()
    ? clients.filter((c) =>
        c.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        c.region.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : clients;

  // Group filtered clients
  const grouped: Record<string, ClientInfo[]> = {};
  for (const client of filteredClients) {
    const meta = REGION_META[client.region];
    const group = meta?.group ?? 'Other';
    if (!grouped[group]) grouped[group] = [];
    grouped[group].push(client);
  }

  const allGroups = [...GROUP_ORDER, 'Other'].filter((g) => grouped[g]?.length);
  const totalVisible = filteredClients.length;
  const hasNoResults = searchQuery.trim() !== '' && totalVisible === 0;

  return (
    <div className="client-selector">
      {/* Hero */}
      <div className="cs-hero">
        <div className="cs-eyebrow">Alert Analysis Engine</div>
        <h1 className="cs-title">Select a <em>client</em></h1>
        <p className="cs-subtitle">Analyze detection coverage, identify gaps, and reduce alert fatigue.</p>
      </div>

      {/* Search bar */}
      <div className="cs-search">
        <span className="cs-search__icon">⌕</span>
        <input
          className="cs-search__input"
          type="text"
          placeholder="Filter clients..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          autoComplete="off"
        />
        <span className="cs-search__count">
          {clients.length === 0 ? 'Loading...' : `${totalVisible} client${totalVisible !== 1 ? 's' : ''}`}
        </span>
      </div>

      {fetchError && (
        <div className="error-banner map-error">{fetchError}</div>
      )}

      {/* Region groups */}
      <div className="cs-content">
        {clients.length === 0 && !fetchError && (
          <div className="cs-skeletons">
            {[1,2,3,4,5,6].map((i) => (
              <div key={i} className="region-card region-card--skeleton skeleton" aria-hidden="true" />
            ))}
          </div>
        )}

        {hasNoResults && (
          <div className="cs-empty">
            No clients match &ldquo;{searchQuery}&rdquo;
          </div>
        )}

        {allGroups.map((group) => {
          const groupClients = grouped[group];
          if (!groupClients?.length) return null;
          return (
            <div key={group} className="region-group">
              <div className="region-group-label">{group}</div>
              <div className="region-group-cards">
                {groupClients.map((client) => {
                  const meta = REGION_META[client.region];
                  const isSelected = client.name === selected;
                  return (
                    <button
                      key={client.name}
                      className={`region-card${isSelected ? ' region-card--selected' : ''}`}
                      onClick={() => setSelected(isSelected ? '' : client.name)}
                      disabled={loading}
                    >
                      {isSelected && <span className="region-card__dot" aria-hidden="true" />}
                      <div className="region-card__name">{client.name}</div>
                      <div className="region-card__meta">
                        <span>{meta?.city ?? client.region}</span>
                        <span>{meta?.label ?? client.region.toUpperCase()}</span>
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>

      {/* Sticky CTA — only visible when a client is selected */}
      <div className={`cs-cta${selected ? ' cs-cta--visible' : ''}`}>
        <span className="cs-cta__hint">{selected} selected</span>
        <button
          className="cs-cta__btn"
          onClick={() => { if (selected && !loading) onAnalyze(selected); }}
          disabled={loading || !selected}
        >
          {loading ? 'Analyzing...' : `Analyze ${selected} →`}
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify ClientSelector renders correctly**

```bash
cd frontend && npm run dev
```

Open `http://localhost:5173`. Check:
- Hero text shows "Select a *client*" with green "client"
- Search bar is visible below hero
- Region groups render with fade-line separator
- Clicking a card selects it (green border + top bar)
- Sticky CTA appears at bottom when a card is selected
- Typing in search filters the card list
- Searching for something with no matches shows "No clients match..."

- [ ] **Step 3: Commit**

```bash
cd frontend && git add src/components/ClientSelector.tsx
git commit -m "feat(frontend): redesign ClientSelector with search filter and sticky CTA"
```

---

## Task 5: IntegrationSummary — New Classes + Error/Empty States

**Files:**
- Modify: `frontend/src/components/IntegrationSummary.tsx`

- [ ] **Step 1: Replace IntegrationSummary.tsx**

```tsx
import type { AnalyzeResponse } from '../types';

interface Props {
  data: AnalyzeResponse;
  clientName: string;
  loading: boolean;
  onViewMITRE: () => void;
  onViewInsights: () => void;
  onRefresh: () => void;
}

export default function IntegrationSummary({ data, clientName, loading, onViewMITRE, onViewInsights, onRefresh }: Props) {
  const { integrations, stats } = data;
  const sorted     = [...integrations].sort((a, b) => b.alert_count - a.alert_count);
  const blindSpots = integrations.filter((i) => i.alert_count === 0);

  // Coverage % for the header
  const coveragePct = stats.done_integrations > 0
    ? Math.round((stats.integrations_with_alerts / stats.done_integrations) * 100)
    : 0;

  return (
    <div className="integration-summary">

      {/* ── Left panel ── */}
      <div className="summary-panel">
        <div className="summary-panel-header">
          <div className="summary-client-label">Analyzing</div>
          <div className="summary-client-name">{clientName}</div>
        </div>

        <div className="summary-panel-stats">
          <div className="stat-card">
            <div className="stat-number">{stats.done_integrations}</div>
            <div className="stat-desc">Integrations</div>
          </div>
          <div className="stat-card">
            <div className="stat-number">{stats.total_alerts}</div>
            <div className="stat-desc">Active Alerts</div>
          </div>
          <div className="stat-card">
            <div className="stat-number">{stats.security_alerts}</div>
            <div className="stat-desc">Security Alerts</div>
          </div>
          {stats.vendor_covered_alerts > 0 && (
            <div className="stat-card">
              <div className="stat-number">{stats.vendor_covered_alerts}</div>
              <div className="stat-desc">Vendor Covered</div>
            </div>
          )}
          <div className="stat-card">
            <div className="stat-number">{stats.integrations_with_alerts}</div>
            <div className="stat-desc">With Coverage</div>
          </div>
          <div className="stat-card stat-card--danger">
            <div className="stat-number">{blindSpots.length}</div>
            <div className="stat-desc">Blind Spots</div>
          </div>
        </div>

        <div className="summary-panel-actions-label">Explore</div>
        <div className="summary-panel-actions">
          <button className="btn-action" onClick={onViewMITRE}>
            <div className="action-icon">▦</div>
            <div>
              <span className="action-title">MITRE Coverage</span>
              <span className="action-desc">ATT&CK heatmap &amp; gaps</span>
            </div>
          </button>
          <button className="btn-action" onClick={onViewInsights}>
            <div className="action-icon">◈</div>
            <div>
              <span className="action-title">Alert Insights</span>
              <span className="action-desc">AI-powered analysis</span>
            </div>
          </button>
          <button className="btn-action" onClick={onRefresh} disabled={loading}>
            <div className="action-icon">↻</div>
            <div>
              <span className="action-title">Refresh</span>
              <span className="action-desc">{loading ? 'Refreshing...' : 'Re-fetch live data'}</span>
            </div>
          </button>
        </div>
      </div>

      {/* ── Right panel ── */}
      <div className="summary-table-panel">
        <div className="summary-table-header">
          <div>
            <div className="summary-table-title">Integrations</div>
            <div className="summary-table-sub">
              {sorted.length} integration{sorted.length !== 1 ? 's' : ''} · sorted by alert volume
            </div>
          </div>
          <div style={{ textAlign: 'right' }}>
            <div className="coverage-pct">{coveragePct}%</div>
            <div className="coverage-label">Coverage</div>
          </div>
        </div>

        {sorted.length === 0 ? (
          <div className="summary-state-wrap">
            <div className="state-empty">
              <div className="state-empty__icon">◎</div>
              <div className="state-empty__title">No integrations found</div>
              <div className="state-empty__body">This client has no integrations configured in the analyzer.</div>
            </div>
          </div>
        ) : (
          <div className="integration-table-wrap">
            <table className="integration-table">
              <thead>
                <tr>
                  <th>Integration</th>
                  <th>Status</th>
                  <th>Coverage</th>
                  <th>Alerts</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((integration) => {
                  const isBlind = integration.alert_count === 0;
                  return (
                    <tr key={integration.name} className={isBlind ? 'row-blind-spot' : ''}>
                      <td>
                        <div className="integration-name">{integration.name}</div>
                        <div className="integration-type">{integration.application} · {integration.subsystem}</div>
                      </td>
                      <td>
                        <span className="status-tag status-tag--ok">Active</span>
                      </td>
                      <td>
                        <span className={`status-tag ${isBlind ? 'status-tag--blind' : 'status-tag--ok'}`}>
                          {isBlind ? 'Blind Spot' : 'Covered'}
                        </span>
                      </td>
                      <td>
                        <div className="alert-count" style={isBlind ? { color: 'var(--danger)' } : {}}>
                          {integration.alert_count}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify IntegrationSummary**

After selecting and analyzing a client, check:
- Left panel shows client name + 6 stat cards, blind spots card in red
- "Explore" section has 3 action buttons (MITRE, Insights, Refresh)
- Coverage % shown in top-right of right panel
- Table renders with integration name + type stacked, status badges, alert count
- Blind spot rows have red background

- [ ] **Step 3: Commit**

```bash
cd frontend && git add src/components/IntegrationSummary.tsx
git commit -m "feat(frontend): redesign IntegrationSummary with new layout and empty state"
```

---

## Task 6: AlertInsights — Card Types + Signals Grid + Loading/Error/Empty States

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`

- [ ] **Step 1: Read the full current AlertInsights.tsx to understand the tab rendering**

The file is large. Key sections to understand before editing:
- Lines 1–58: props, state, tab definitions (already read)
- Lines 60–200: left panel (model header, summary, priority, strengths, recommendations, signals)
- Lines 200+: right panel (tabs + tab content for each tab)

Read `frontend/src/components/AlertInsights.tsx` offset 200, limit 200 to see tab content structure before replacing.

- [ ] **Step 2: Replace AlertInsights.tsx**

Replace the full file. The key changes are:
1. Model selector: `<select>` instead of custom dropdown
2. Left panel sections: eyebrow + new class names + signals grid
3. Tab buttons: `.tab-btn` / `.tab-btn--active` replacing old classes
4. Card types: `.insight-card--duplicate`, `--noise`, `--family`, `--merge`, `--coverage`
5. Duplicate cards: alert pair + similarity bar
6. Noise cards: missing features pill list
7. Empty states per tab
8. Error state using `.state-error`

```tsx
import { useState, useEffect } from 'react';
import type { SimilarityResult, InsightsReport, NoiseAlert } from '../types';
import { fetchInsights } from '../services/api';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
  client: string;
}

type Tab = 'duplicates' | 'families' | 'merge' | 'coverage' | 'noise' | 'unique' | 'recommendations';

/** Returns a CSS gradient colour for a similarity bar (0–1 scale) */
function simBarGradient(score: number): string {
  if (score >= 0.85) return 'linear-gradient(90deg, #f59e0b, #10b981)';
  if (score >= 0.65) return 'linear-gradient(90deg, #ef4444, #f59e0b)';
  return 'linear-gradient(90deg, #ef4444, #ef4444)';
}

export default function AlertInsights({ data, report, insightsError = false, client }: Props) {
  const [activeTab, setActiveTab]         = useState<Tab>('duplicates');
  const [localReport, setLocalReport]     = useState<InsightsReport | null>(report);
  const [selectedModel, setSelectedModel] = useState<'mistral' | 'gemma'>('mistral');
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [regenError, setRegenError]       = useState(false);

  useEffect(() => {
    if (report !== null && !isRegenerating) setLocalReport(report);
  }, [report]); // eslint-disable-line react-hooks/exhaustive-deps

  const effectiveReport = localReport;
  const isLoading = !effectiveReport && !insightsError && !regenError;

  const handleRegenerate = async () => {
    setIsRegenerating(true);
    setRegenError(false);
    try {
      const newReport = await fetchInsights(client, selectedModel);
      setLocalReport(newReport);
    } catch (e) {
      console.warn('[insights regen]', e);
      setRegenError(true);
    } finally {
      setIsRegenerating(false);
    }
  };

  const noiseCount = data.noise_alerts?.length ?? 0;
  const gapCount   = data.coverage_insights?.length ?? 0;
  const recsCount  = effectiveReport?.recommendations?.length ?? 0;

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'duplicates',      label: 'Duplicates',      count: data.duplicates?.length       ?? 0 },
    { key: 'families',        label: 'Families',        count: data.families?.length          ?? 0 },
    { key: 'merge',           label: 'Merge',           count: data.merge_suggestions?.length ?? 0 },
    { key: 'coverage',        label: 'Coverage',        count: gapCount                           },
    { key: 'noise',           label: 'Noise',           count: noiseCount                         },
    { key: 'unique',          label: 'Unique',          count: data.unique_detections?.length ?? 0 },
    { key: 'recommendations', label: 'Recommendations', count: recsCount                          },
  ];

  const hasError = insightsError || regenError;

  return (
    <div className="alert-insights">

      {/* ══ LEFT PANEL ══ */}
      <div className="insights-panel">

        {/* Model selector */}
        <div className="insights-model-header">
          <select
            className="insights-model-select"
            value={selectedModel}
            onChange={(e) => setSelectedModel(e.target.value as 'mistral' | 'gemma')}
            disabled={isRegenerating}
          >
            <option value="mistral">Mistral Small 3.1</option>
            <option value="gemma">Gemma 3 27B</option>
          </select>
          <button
            className="insights-regenerate-btn"
            onClick={handleRegenerate}
            disabled={isRegenerating || !client}
            title="Regenerate insights with selected model"
          >
            {isRegenerating ? '…' : '↺'}
          </button>
        </div>

        <div className="insights-panel-scroll">

          {/* Summary */}
          <div>
            <div className="eyebrow">Summary</div>
            {isRegenerating || isLoading ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '100%' }} />
                <div className="insights-skeleton skeleton" style={{ width: '85%' }} />
                <div className="insights-skeleton skeleton" style={{ width: '70%' }} />
              </>
            ) : hasError ? (
              <div className="state-error">
                <span className="state-error__icon">⚠</span>
                <div>
                  <div className="state-error__title">Insights unavailable</div>
                  <div className="state-error__body">LLM enrichment failed. Check your provider configuration.</div>
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry with {selectedModel === 'mistral' ? 'Mistral Small' : 'Gemma 3 27B'}</button>
                </div>
              </div>
            ) : (
              <p className="insights-summary-text">
                {effectiveReport?.summary || 'Enrichment unavailable — check LLM provider configuration.'}
              </p>
            )}
          </div>

          {/* Top Priority */}
          <div>
            <div className="eyebrow">Top Priority</div>
            {isRegenerating || isLoading ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '90%', marginBottom: 8 }} />
                <div className="insights-skeleton skeleton" style={{ width: '80%' }} />
              </>
            ) : hasError ? null : (
              <ul className="insights-priority-list">
                {effectiveReport?.top_priority?.length ? (
                  effectiveReport.top_priority.map((item, i) => (
                    <li key={i} className="insights-priority-item">
                      <span className="insights-priority-num">{String(i + 1).padStart(2, '0')}</span>
                      <span className="insights-priority-text">{item}</span>
                    </li>
                  ))
                ) : (
                  <li className="insights-priority-item">
                    <span className="insights-priority-text" style={{ color: 'var(--text-dim)' }}>No priorities flagged.</span>
                  </li>
                )}
              </ul>
            )}
          </div>

          {/* Signals */}
          <div>
            <div className="eyebrow">Signals</div>
            <div className="insights-signals-grid">
              <div className="insights-signal-pill">
                <span className="signal-count">{data.duplicates?.length ?? 0}</span>
                <span className="signal-label">Duplicates</span>
              </div>
              <div className="insights-signal-pill">
                <span className="signal-count">{data.families?.length ?? 0}</span>
                <span className="signal-label">Families</span>
              </div>
              <div className="insights-signal-pill">
                <span className="signal-count">{noiseCount}</span>
                <span className="signal-label">Noise</span>
              </div>
              <div className="insights-signal-pill">
                <span className="signal-count">{gapCount}</span>
                <span className="signal-label">Gaps</span>
              </div>
            </div>
          </div>

        </div>
      </div>

      {/* ══ RIGHT PANEL ══ */}
      <div style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>

        {/* Tabs */}
        <div className="insights-tabs">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              className={`tab-btn${activeTab === tab.key ? ' tab-btn--active' : ''}`}
              onClick={() => setActiveTab(tab.key)}
            >
              {tab.label}
              <span className="tab-count">{tab.count}</span>
            </button>
          ))}
        </div>

        {/* Tab content */}
        <div className="insights-tab-content">

          {/* ── DUPLICATES ── */}
          {activeTab === 'duplicates' && (
            data.duplicates?.length ? (
              data.duplicates.map((dup, i) => {
                const enriched = effectiveReport?.enriched_dups?.[i];
                const simPct = Math.round((dup.similarity ?? 0) * 100);
                return (
                  <div key={i} className="insight-card insight-card--duplicate">
                    <div className="insight-card-header">
                      <div className="insight-card-title">{dup.alert_names[0]}</div>
                      <span className="badge badge--indigo">Duplicate</span>
                    </div>
                    <div className="alert-pair">
                      {dup.alert_names.map((name, j) => (
                        <span key={j}>
                          {j > 0 && <span className="alert-pair-sep">↔</span>}
                          <span className="alert-tag">{name}</span>
                        </span>
                      ))}
                    </div>
                    <div className="sim-bar-wrap">
                      <span className="sim-bar-label">{simPct}% similar</span>
                      <div className="sim-bar-track">
                        <div
                          className="sim-bar-fill"
                          style={{ width: `${simPct}%`, background: simBarGradient(dup.similarity ?? 0) }}
                        />
                      </div>
                    </div>
                    {(enriched?.explanation || dup.explanation) && (
                      <p className="insight-card-body">{enriched?.explanation ?? dup.explanation}</p>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No duplicates found</div>
                <div className="state-empty__body">All alerts have sufficiently distinct detection logic.</div>
              </div>
            )
          )}

          {/* ── FAMILIES ── */}
          {activeTab === 'families' && (
            data.families?.length ? (
              data.families.map((fam, i) => (
                <div key={i} className="insight-card insight-card--family">
                  <div className="insight-card-header">
                    <div className="insight-card-title">{fam.name}</div>
                    <span className="badge badge--green">{fam.alert_ids.length} alerts</span>
                  </div>
                  <div className="alert-pair" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
                    {fam.alert_names.map((name, j) => (
                      <span key={j} className="alert-tag">{name}</span>
                    ))}
                  </div>
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No families found</div>
                <div className="state-empty__body">No alert groupings detected in this client's stack.</div>
              </div>
            )
          )}

          {/* ── MERGE ── */}
          {activeTab === 'merge' && (
            data.merge_suggestions?.length ? (
              data.merge_suggestions.map((sug, i) => (
                <div key={i} className="insight-card insight-card--merge">
                  <div className="insight-card-header">
                    <div className="insight-card-title">Merge Suggestion</div>
                    <span className="badge badge--amber">{sug.alert_ids.length} alerts</span>
                  </div>
                  <div className="alert-pair" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
                    {sug.alert_names.map((name, j) => (
                      <span key={j} className="alert-tag">{name}</span>
                    ))}
                  </div>
                  {sug.reason && <p className="insight-card-body">{sug.reason}</p>}
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No merge suggestions</div>
                <div className="state-empty__body">No consolidation opportunities identified.</div>
              </div>
            )
          )}

          {/* ── COVERAGE ── */}
          {activeTab === 'coverage' && (
            data.coverage_insights?.length ? (
              data.coverage_insights.map((gap, i) => {
                const enriched = effectiveReport?.enriched_gaps?.[i];
                return (
                  <div key={i} className="insight-card insight-card--coverage">
                    <div className="insight-card-header">
                      <div className="insight-card-title">{enriched?.tactic ?? gap}</div>
                      <span className="badge badge--sky">Gap</span>
                    </div>
                    {enriched?.explanation && (
                      <p className="insight-card-body">{enriched.explanation}</p>
                    )}
                    {!enriched && <p className="insight-card-body">{gap}</p>}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No coverage gaps</div>
                <div className="state-empty__body">Full MITRE ATT&CK coverage detected across all tactics.</div>
              </div>
            )
          )}

          {/* ── NOISE ── */}
          {activeTab === 'noise' && (
            data.noise_alerts?.length ? (
              data.noise_alerts.map((noise: NoiseAlert, i) => {
                const explanation = effectiveReport?.noise_explanations?.[i];
                return (
                  <div key={i} className="insight-card insight-card--noise">
                    <div className="insight-card-header">
                      <div className="insight-card-title">{noise.name}</div>
                      <span className="badge badge--red">Noisy</span>
                    </div>
                    {(explanation?.reason || noise.reason) && (
                      <p className="insight-card-body">{explanation?.reason ?? noise.reason}</p>
                    )}
                    {noise.missing_features?.length > 0 && (
                      <div className="missing-features">
                        {noise.missing_features.map((feat, j) => (
                          <span key={j} className="missing-tag">{feat}</span>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No noisy alerts</div>
                <div className="state-empty__body">All alerts have sufficient field coverage for reliable detection.</div>
              </div>
            )
          )}

          {/* ── UNIQUE ── */}
          {activeTab === 'unique' && (
            data.unique_detections?.length ? (
              data.unique_detections.map((name, i) => (
                <div key={i} className="insight-card">
                  <div className="insight-card-title">{name}</div>
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No unique detections</div>
                <div className="state-empty__body">All alerts overlap with at least one other detection rule.</div>
              </div>
            )
          )}

          {/* ── RECOMMENDATIONS ── */}
          {activeTab === 'recommendations' && (
            isLoading || isRegenerating ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
              </>
            ) : hasError ? (
              <div className="state-error">
                <span className="state-error__icon">⚠</span>
                <div>
                  <div className="state-error__title">Recommendations unavailable</div>
                  <div className="state-error__body">LLM enrichment failed. Try regenerating with a different model.</div>
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry</button>
                </div>
              </div>
            ) : effectiveReport?.recommendations?.length ? (
              effectiveReport.recommendations.map((rec, i) => (
                <div key={i} className="rec-item">
                  <div className="rec-num">{String(i + 1).padStart(2, '0')}</div>
                  <div className="rec-text">{rec}</div>
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No recommendations</div>
                <div className="state-empty__body">Run insights generation to get AI-powered recommendations.</div>
              </div>
            )
          )}

        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Verify AlertInsights**

Navigate to Alert Insights. Check:
- Left panel: model `<select>` + regenerate button, eyebrow headings, priority numbered list, 2×2 signals grid
- All text ≥ 0.78rem (no tiny illegible labels)
- Tabs render with count badges; clicking switches content
- Duplicate cards show alert pair + similarity bar with gradient
- Noise cards show missing feature pill list in red
- Empty state shown when a tab has 0 items
- Error state shown if insights fail (trigger by killing backend temporarily)

- [ ] **Step 4: Commit**

```bash
cd frontend && git add src/components/AlertInsights.tsx
git commit -m "feat(frontend): redesign AlertInsights with card types, signals grid, and functional states"
```

---

## Task 7: MITREHeatmap — Coverage Colors + Toolbar + Hover Tooltip + Suggestion Error

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx`

- [ ] **Step 1: Update coverageColor helper to new palette**

Find the `coverageColor` function (currently at line 60–65) and replace it:

```typescript
function coverageColor(percent: number): string {
  if (percent === 0)  return '#1e2535';  // --cov-none
  if (percent < 25)  return '#7c2d12';  // --cov-low
  if (percent < 50)  return '#92400e';  // --cov-partial
  if (percent < 75)  return '#065f46';  // --cov-good
  return '#10b981';                      // --cov-full
}
```

- [ ] **Step 2: Update coverageLabel and priorityColor helpers**

```typescript
function coverageLabel(percent: number): string {
  if (percent === 0)  return 'None';
  if (percent < 25)  return 'Low';
  if (percent < 50)  return 'Partial';
  if (percent < 75)  return 'Good';
  return 'Full';
}

function priorityColor(priority: string): string {
  switch (priority) {
    case 'critical': return 'var(--danger)';
    case 'high':     return 'var(--warn)';
    case 'medium':   return 'var(--accent)';
    default:         return 'var(--text-dim)';
  }
}
```

- [ ] **Step 3: Add tooltip state to MITREHeatmap component state**

Inside the `MITREHeatmap` component function, find the existing `useState` declarations and add:

```typescript
const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);
```

- [ ] **Step 4: Update the heatmap toolbar JSX**

Find the existing toolbar/summary bar in the heatmap view JSX. Replace it with a toolbar that uses the new CSS classes. The toolbar should show stats (overall coverage %, technique count, uncovered count) and the view toggle. Find the section that renders the summary stats and view toggle — typically at the top of the heatmap's return JSX — and replace with:

```tsx
{/* Toolbar */}
<div className="mitre-toolbar">
  <div className="mitre-stats">
    <div className="mitre-stat">
      <div className={`mitre-stat-val mitre-stat-val--accent`}>
        {data.summary?.overall_percent != null
          ? `${Math.round(data.summary.overall_percent)}%`
          : '—'}
      </div>
      <div className="mitre-stat-label">Overall</div>
    </div>
    <div className="mitre-stat-divider" />
    <div className="mitre-stat">
      <div className="mitre-stat-val">{data.summary?.total_techniques ?? '—'}</div>
      <div className="mitre-stat-label">Techniques</div>
    </div>
    <div className="mitre-stat-divider" />
    <div className="mitre-stat">
      <div className={`mitre-stat-val mitre-stat-val--warn`}>
        {data.summary?.total_techniques != null && data.summary?.covered_techniques != null
          ? data.summary.total_techniques - data.summary.covered_techniques
          : '—'}
      </div>
      <div className="mitre-stat-label">Uncovered</div>
    </div>
    <div className="mitre-stat-divider" />
    <div className="mitre-stat">
      <div className="mitre-stat-val">{data.summary?.total_subtechniques ?? '—'}</div>
      <div className="mitre-stat-label">Sub-techniques</div>
    </div>
  </div>

  <div className="mitre-toolbar-right">
    <div className="mitre-legend">
      {(['None','Low','Partial','Good','Full'] as const).map((lbl, i) => {
        const colors = ['#1e2535','#7c2d12','#92400e','#065f46','#10b981'];
        return (
          <div key={lbl} className="mitre-legend-item">
            <div className="mitre-legend-dot" style={{ background: colors[i] }} />
            <span className="mitre-legend-label">{lbl}</span>
          </div>
        );
      })}
    </div>
    <div className="mitre-stat-divider" />
    <div className="view-toggle">
      <button
        className={`view-toggle-btn${viewMode === 'heatmap' ? ' view-toggle-btn--active' : ''}`}
        onClick={() => setViewMode('heatmap')}
      >
        Heatmap
      </button>
      <button
        className={`view-toggle-btn${viewMode === 'graph' ? ' view-toggle-btn--active' : ''}`}
        onClick={() => setViewMode('graph')}
      >
        Graph
      </button>
    </div>
    <button className="mitre-download-btn" onClick={handleDownloadLayer}>
      ↓ ATT&CK Layer
    </button>
  </div>
</div>
```

> **Note:** Use whatever the existing state variable is named for the view mode toggle (check the file — it may be `mode`, `view`, or `viewMode`). Use whatever the existing download handler is named (check the file for the ATT&CK layer download function).

- [ ] **Step 5: Update tactic header JSX to use CSS variable for coverage color**

For each tactic column header, add the CSS variable so `::before` picks it up:

```tsx
<div
  className="tactic-header"
  style={{ '--cov-color': coverageColor(tacticData.percent) } as React.CSSProperties}
>
  <div className="tactic-name">{TACTIC_LABELS[tactic]}</div>
  <div className={`tactic-count${tacticData.covered === 0 ? ' tactic-count--zero' : ''}`}>
    <em>{tacticData.covered}</em>/{tacticData.total} covered
  </div>
</div>
```

- [ ] **Step 6: Add hover tooltip to technique cells**

For each technique cell, add mouse event handlers:

```tsx
<div
  key={tech.techniqueID}
  className={`tech-cell${selectedTech === tech.techniqueID ? ' tech-cell--selected' : ''}`}
  style={{ background: coverageColor(tech.score * 100) }}
  onClick={() => setSelectedTech(tech.techniqueID)}
  onMouseEnter={(e) => setTooltip({
    x: e.clientX + 12,
    y: e.clientY - 8,
    text: `${tech.techniqueID} · ${tech.name ?? tech.techniqueID}`,
  })}
  onMouseLeave={() => setTooltip(null)}
>
  <span className="tech-id">{tech.techniqueID}</span>
</div>
```

> **Note:** Check the existing file for the correct property names on the technique object (`techniqueID`, `score`, `name`) — these come from `NavigatorTechnique` in `types/index.ts`. Use the existing `selectedTech` state variable (or whatever it's named in the file).

- [ ] **Step 7: Render the tooltip**

In the heatmap view return JSX, add the tooltip just before the closing `</div>` of the heatmap container:

```tsx
{tooltip && (
  <div
    className="tech-tooltip"
    style={{ left: tooltip.x, top: tooltip.y }}
  >
    {tooltip.text}
  </div>
)}
```

- [ ] **Step 8: Add suggestion error state to detail panel**

In the detail panel's suggestion generation section, find where suggestion errors are shown (currently a plain span). Replace with:

```tsx
{suggestionsError && (
  <div className="state-error" style={{ marginBottom: 10 }}>
    <span className="state-error__icon">⚠</span>
    <div>
      <div className="state-error__title">Query generation failed</div>
      <div className="state-error__body">The provider returned an error. Try a different provider.</div>
    </div>
  </div>
)}
```

> **Note:** `suggestionsError` is whatever the existing error state variable is named in MITREHeatmap for suggestion fetching. Check the file.

- [ ] **Step 9: Verify MITREHeatmap**

Navigate to MITRE Coverage. Check:
- Toolbar shows Overall %, Techniques, Uncovered stats, legend, toggle
- Tactic column headers have a 3px coloured top bar matching coverage level
- Hovering a technique cell shows a tooltip with technique ID and name
- Clicking a technique opens the detail panel
- Download button still works

- [ ] **Step 10: Commit**

```bash
cd frontend && git add src/components/MITREHeatmap.tsx
git commit -m "feat(frontend): update MITREHeatmap with new coverage colors, toolbar, and hover tooltip"
```

---

## Task 8: Final Verification

- [ ] **Step 1: Run full dev server check**

```bash
cd frontend && npm run dev
```

Walk through the full user journey:
1. Landing page — hero, search filter, region cards, sticky CTA
2. Select a client → CTA appears → click Analyze
3. IntegrationSummary — stats, action buttons, table, breadcrumb in header
4. Click MITRE Coverage — toolbar, heatmap columns, colour-coded headers, hover tooltip, detail panel
5. Back → click Alert Insights — model selector, priority list, signals grid, tabs, card types, similarity bars
6. Trigger empty state: look for a tab with 0 count and verify empty state UI

- [ ] **Step 2: TypeScript check**

```bash
cd frontend && npm run build
```

Expected: Build succeeds with no TypeScript errors. Fix any type errors before marking complete.

- [ ] **Step 3: Final commit**

```bash
cd frontend && git add -A
git commit -m "feat(frontend): complete redesign verification and cleanup"
```

---

## Self-Review Against Spec

**Spec coverage check:**
- ✅ Red Hat Display + IBM Plex Mono: Task 1 (fonts) + Task 2 (CSS tokens)
- ✅ New color tokens: Task 2
- ✅ App shell header + breadcrumb: Task 3
- ✅ Framer Motion transitions: Task 3
- ✅ ClientSelector search filter: Task 4
- ✅ ClientSelector sticky CTA: Task 4
- ✅ ClientSelector empty search state: Task 4
- ✅ IntegrationSummary layout + error/empty: Task 5
- ✅ AlertInsights card type differentiation: Task 6
- ✅ AlertInsights signals grid: Task 6
- ✅ AlertInsights loading/error/empty states: Task 6
- ✅ AlertInsights similarity bar: Task 6
- ✅ MITREHeatmap coverage color update: Task 7
- ✅ MITREHeatmap toolbar restructure: Task 7
- ✅ MITREHeatmap hover tooltip: Task 7
- ✅ MITREHeatmap suggestion error state: Task 7
- ✅ Skeleton shimmer (shared `.skeleton` class): Task 2
- ✅ `.state-error` and `.state-empty` shared components: Task 2

**Type consistency:**
- `simBarGradient(score: number)` used in Task 6 with `dup.similarity` which is typed as `number` on `DuplicateGroup` ✅
- `coverageColor(percent: number)` used in Tasks 7 with `tacticData.percent` and `tech.score * 100` ✅
- `NoiseAlert` imported from types and used for noise tab ✅
- `InsightsReport` fields (`top_priority`, `enriched_dups`, `noise_explanations`) all exist in types ✅
