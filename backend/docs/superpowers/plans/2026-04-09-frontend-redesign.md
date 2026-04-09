# Frontend Redesign — Terminal/War Room Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the generic indigo/Inter dark theme with a Terminal/War Room aesthetic — neon green on near-black, IBM Plex Mono throughout, with a redesigned landing page and operational visual hierarchy.

**Architecture:** Pure CSS/markup changes across 5 files. No logic changes, no new dependencies beyond a Google Fonts link. Each task is independently verifiable by running the dev server (`npm run dev` in `frontend/`) and checking the browser.

**Tech Stack:** React + TypeScript + Vite, plain CSS (no CSS-in-JS), Google Fonts (IBM Plex Mono)

---

## Dev Server

Start once before Task 1 and keep running throughout:

```bash
cd frontend && npm run dev
```

Open http://localhost:5173 (or whatever port Vite picks). After each task, reload the browser to verify.

---

## Task 1: Google Fonts + Design Tokens

**Files:**
- Modify: `frontend/index.html:6` (after favicon link)
- Modify: `frontend/src/App.css:1-19` (`:root` block + body)

- [ ] **Step 1: Add IBM Plex Mono to index.html**

Replace the `<head>` block in `frontend/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>CX Alert Analyzer</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@300;400;500;600;700&display=swap" rel="stylesheet">
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: Replace CSS variables and base styles in App.css**

Replace the entire `/* ── Reset & Base ─── */` section and `body` rule (lines 1–26) with:

```css
/* ── Reset & Base ─────────────────────────────────── */
* { margin: 0; padding: 0; box-sizing: border-box; }

:root {
  /* Backgrounds */
  --bg:             #020b05;
  --bg-mid:         #0a1f0f;
  --surface:        #0d2412;
  --surface-hover:  #122b17;
  --border:         rgba(0, 255, 100, 0.12);
  --border-bright:  rgba(0, 255, 100, 0.3);

  /* Text */
  --text:           #e0ffe8;
  --text-dim:       rgba(0, 255, 100, 0.45);
  --text-code:      #b0ffca;

  /* Accent */
  --accent:         #00ff64;
  --accent-dim:     rgba(0, 255, 100, 0.15);

  /* Semantic */
  --danger:         #ff4d4d;
  --danger-dim:     rgba(255, 77, 77, 0.12);
  --warn:           #ffb347;

  --radius:         3px;
  --font-mono:      'IBM Plex Mono', 'JetBrains Mono', monospace;
}

body {
  font-family: var(--font-mono);
  background: var(--bg);
  color: var(--text);
  line-height: 1.6;
}
```

- [ ] **Step 3: Verify in browser**

Reload http://localhost:5173. The page should now be deep green-black with green-tinted text. Font should be IBM Plex Mono (check DevTools if unsure — Elements → Computed → font-family).

- [ ] **Step 4: Commit**

```bash
git add frontend/index.html frontend/src/App.css
git commit -m "feat: apply Terminal/War Room design tokens and IBM Plex Mono"
```

---

## Task 2: Header — Wordmark + Status Dot

**Files:**
- Modify: `frontend/src/App.tsx:54-66` (header JSX)
- Modify: `frontend/src/App.css:28-44` (`.app-header` section)

- [ ] **Step 1: Update header JSX in App.tsx**

Replace the `<header className="app-header">` block (lines 54–66):

```tsx
<header className="app-header">
  {view !== 'form' ? (
    <button className="btn btn-small" onClick={goBack}>
      ← Back
    </button>
  ) : (
    <div />
  )}
  <h1 onClick={goHome}>
    <sup>CX</sup>Alert <strong>Analyzer</strong>
  </h1>
  <div className="header-status">
    <span className="status-dot" />
    ONLINE
  </div>
</header>
```

- [ ] **Step 2: Replace header styles in App.css**

Replace the `/* ── App Layout ── */` section (`.app`, `.app-header`, `.app-header h1` rules):

```css
/* ── App Layout ──────────────────────────────────── */
.app { min-height: 100vh; display: flex; flex-direction: column; }

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 32px;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}

.app-header h1 {
  font-size: 1.1rem;
  font-weight: 300;
  color: var(--text);
  cursor: pointer;
  letter-spacing: 0.02em;
}

.app-header h1 strong {
  font-weight: 700;
  color: var(--accent);
}

.app-header h1 sup {
  font-size: 0.55rem;
  color: var(--text-dim);
  letter-spacing: 0.12em;
  margin-right: 5px;
  vertical-align: super;
}

.app-main {
  flex: 1;
  padding: 32px;
  max-width: 1400px;
  margin: 0 auto;
  width: 100%;
}

/* ── Header Status ───────────────────────────────── */
.header-status {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 0.68rem;
  letter-spacing: 0.18em;
  color: var(--text-dim);
  text-transform: uppercase;
}

.status-dot {
  width: 6px;
  height: 6px;
  background: var(--accent);
  border-radius: 50%;
  flex-shrink: 0;
  animation: pulse-dot 2s ease-in-out infinite;
}

@keyframes pulse-dot {
  0%, 100% { opacity: 1; box-shadow: 0 0 5px var(--accent); }
  50%       { opacity: 0.25; box-shadow: none; }
}
```

- [ ] **Step 3: Verify in browser**

Reload. The header should show:
- Left: `← Back` button (only on non-form views)
- Center: `CX` (tiny dim superscript) + `Alert Analyzer` (light/bold split)
- Right: pulsing green dot + `ONLINE` in dim uppercase

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.tsx frontend/src/App.css
git commit -m "feat: redesign header with wordmark and pulsing status dot"
```

---

## Task 3: Button Styles

**Files:**
- Modify: `frontend/src/App.css:54-87` (button rules)

- [ ] **Step 1: Replace all button rules in App.css**

Replace the `/* ── Buttons ── */` section:

```css
/* ── Buttons ─────────────────────────────────────── */
.btn {
  padding: 8px 20px;
  border: none;
  border-radius: var(--radius);
  font-family: var(--font-mono);
  font-size: 0.82rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
  letter-spacing: 0.05em;
}

.btn:disabled { opacity: 0.4; cursor: not-allowed; }

.btn-primary {
  background: var(--accent);
  color: var(--bg);
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.btn-primary:hover:not(:disabled) { background: #33ff80; }

.btn-secondary {
  background: transparent;
  color: var(--accent);
  border: 1px solid var(--border-bright);
}
.btn-secondary:hover:not(:disabled) { background: var(--accent-dim); }

.btn-small {
  padding: 5px 12px;
  font-size: 0.78rem;
  background: transparent;
  color: var(--accent);
  border: 1px solid var(--border-bright);
}
.btn-small:hover { background: var(--accent-dim); }
```

- [ ] **Step 2: Verify in browser**

Navigate to summary view (select a client and run analysis). The `← Back` and `Refresh` buttons should be ghost-style with green border. The Analyze button on the landing page should be solid green with dark text.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.css
git commit -m "feat: update button styles to terminal ghost/solid variants"
```

---

## Task 4: ClientSelector — Grid/Edge-Anchored Layout

**Files:**
- Modify: `frontend/src/components/ClientSelector.tsx` (full JSX rewrite)
- Modify: `frontend/src/App.css:89-130` (`.client-selector` section)

- [ ] **Step 1: Rewrite ClientSelector.tsx**

Replace the entire `return (...)` block and remove the now-unused `form-group` class references:

```tsx
import { useState, useEffect } from 'react';
import { fetchClients } from '../services/api';

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients] = useState<string[]>([]);
  const [selected, setSelected] = useState('');
  const [fetchError, setFetchError] = useState('');

  useEffect(() => {
    fetchClients()
      .then((list) => {
        setClients(list);
        if (list.length > 0) setSelected(list[0]);
      })
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (selected) onAnalyze(selected);
  };

  return (
    <div className="client-selector">
      <div className="client-selector-grid" />
      <span className="client-selector-corner top-left">CX_ALERTS v2.1</span>
      <span className="client-selector-corner top-right">
        <span className="status-dot" />
        ONLINE
      </span>
      <div className="client-selector-content">
        <h2 className="landing-wordmark"><strong>Alert</strong> Analyzer</h2>
        <p className="landing-subtitle">Coralogix Integration Intelligence</p>
        {fetchError && <div className="error-banner">{fetchError}</div>}
        <form className="landing-form" onSubmit={handleSubmit}>
          <select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
            disabled={loading || clients.length === 0}
          >
            {clients.length === 0 && <option value="">Loading...</option>}
            {clients.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
          <button
            type="submit"
            className="btn btn-primary"
            disabled={loading || !selected}
          >
            {loading ? 'ANALYZING...' : 'ANALYZE →'}
          </button>
        </form>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Replace ClientSelector CSS in App.css**

Replace the `/* ── Client Selector ── */` section (and remove the `.form-group` rules that are no longer used):

```css
/* ── Client Selector ─────────────────────────────── */
.client-selector {
  position: relative;
  min-height: calc(100vh - 53px);
  display: flex;
  align-items: flex-end;
  padding: 48px;
  overflow: hidden;
}

.client-selector-grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(var(--border) 1px, transparent 1px),
    linear-gradient(90deg, var(--border) 1px, transparent 1px);
  background-size: 28px 28px;
  mask-image: linear-gradient(to bottom, transparent 0%, black 55%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 55%);
  pointer-events: none;
}

.client-selector-corner {
  position: absolute;
  font-size: 0.68rem;
  letter-spacing: 0.2em;
  color: var(--text-dim);
  text-transform: uppercase;
}
.client-selector-corner.top-left  { top: 20px; left: 28px; }
.client-selector-corner.top-right {
  top: 20px; right: 28px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.client-selector-content {
  position: relative;
  max-width: 480px;
}

.landing-wordmark {
  font-size: 2.4rem;
  font-weight: 300;
  color: var(--text);
  line-height: 1;
  margin-bottom: 8px;
}
.landing-wordmark strong {
  font-weight: 700;
  color: var(--accent);
}
.landing-wordmark::after {
  content: '_';
  color: var(--accent);
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0; }
}

.landing-subtitle {
  font-size: 0.68rem;
  letter-spacing: 0.2em;
  color: var(--text-dim);
  text-transform: uppercase;
  margin-bottom: 28px;
}

.landing-form {
  display: flex;
  gap: 8px;
  align-items: stretch;
}

.landing-form select {
  flex: 1;
  padding: 10px 14px;
  background: rgba(0, 0, 0, 0.5);
  border: 1px solid var(--border-bright);
  border-radius: var(--radius);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 0.9rem;
  outline: none;
  transition: border-color 0.15s;
  cursor: pointer;
  appearance: auto;
}
.landing-form select:focus { border-color: var(--accent); }
.landing-form select:disabled { opacity: 0.4; cursor: not-allowed; }
```

- [ ] **Step 3: Verify in browser**

The landing page should show:
- Dot grid fading in from the top
- `CX_ALERTS v2.1` top-left corner, pulsing dot + `ONLINE` top-right
- Bottom-left: large `Alert Analyzer` wordmark (light/bold) with blinking `_`
- Below: dim uppercase subtitle
- Inline form: `[select ▾] [ANALYZE →]`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ClientSelector.tsx frontend/src/App.css
git commit -m "feat: redesign landing page with grid background and edge-anchored layout"
```

---

## Task 5: Integration Summary — Stats Row

**Files:**
- Modify: `frontend/src/App.css` (`.stat-card`, `.stat-number`, `.stat-desc` rules)
- Modify: `frontend/src/components/IntegrationSummary.tsx:34-67` (stat cards JSX)

- [ ] **Step 1: Replace stat card CSS in App.css**

Replace the `.stats-row`, `.stat-card`, `.stat-number`, `.stat-desc` rules in the `/* ── Integration Summary ── */` section:

```css
.stats-row {
  display: flex;
  gap: 10px;
  margin-bottom: 24px;
}

.stat-card {
  flex: 1;
  background: var(--accent-dim);
  border: 1px solid var(--border);
  border-left: 2px solid var(--border-bright);
  border-radius: var(--radius);
  padding: 14px 18px;
}

.stat-number {
  font-size: 1.8rem;
  font-weight: 700;
  color: var(--text);
  line-height: 1;
}

.stat-desc {
  font-size: 0.68rem;
  color: var(--text-dim);
  margin-top: 5px;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}
```

- [ ] **Step 2: Update stat cards JSX in IntegrationSummary.tsx**

Replace the `<div className="stats-row">` block (lines 34–67) to add color hierarchy and danger styling for blind spots:

```tsx
<div className="stats-row">
  <div className="stat-card">
    <div className="stat-number">{stats.done_integrations}</div>
    <div className="stat-desc">Integrations</div>
  </div>
  <div className="stat-card">
    <div className="stat-number">{stats.total_alerts}</div>
    <div className="stat-desc">Active Alerts</div>
  </div>
  <div className="stat-card">
    <div className="stat-number" style={{ color: 'var(--accent)' }}>{stats.security_alerts}</div>
    <div className="stat-desc">Security Alerts</div>
  </div>
  {stats.vendor_covered_alerts > 0 && (
    <div className="stat-card">
      <div className="stat-number" style={{ color: 'var(--text-dim)' }}>
        {stats.vendor_covered_alerts}
      </div>
      <div className="stat-desc">Vendor Covered</div>
    </div>
  )}
  <div className="stat-card">
    <div className="stat-number" style={{ color: 'var(--accent)' }}>
      {stats.integrations_with_alerts}
    </div>
    <div className="stat-desc">With Coverage</div>
  </div>
  <div
    className="stat-card"
    style={blindSpots.length > 0
      ? { borderLeftColor: 'var(--danger)', background: 'var(--danger-dim)' }
      : {}}
  >
    <div className="stat-number" style={{ color: blindSpots.length > 0 ? 'var(--danger)' : 'var(--accent)' }}>
      {blindSpots.length}
    </div>
    <div className="stat-desc">Blind Spots</div>
  </div>
</div>
```

- [ ] **Step 3: Verify in browser**

Navigate to a client's summary. Stats row should show left-border cards. The Blind Spots card should turn red with a red border when count > 0, green when 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.css frontend/src/components/IntegrationSummary.tsx
git commit -m "feat: update stats row with terminal left-border cards and danger coloring"
```

---

## Task 6: Integration Table — Blind Spot Rows

**Files:**
- Modify: `frontend/src/App.css` (`.row-blind-spot` rule)

- [ ] **Step 1: Replace `.row-blind-spot` in App.css**

Find and replace:
```css
.row-blind-spot {
  opacity: 0.6;
}
```

With:
```css
.row-blind-spot td {
  color: var(--danger);
}
```

- [ ] **Step 2: Verify in browser**

In the integration table, rows with 0 alerts should now show in red instead of fading out. They should be clearly visible as warnings, not de-emphasised.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.css
git commit -m "feat: show blind spot table rows in danger red instead of faded"
```

---

## Task 7: Action Buttons (MITRE / Insights)

**Files:**
- Modify: `frontend/src/App.css` (`.btn-action`, `.action-title`, `.action-desc` rules)
- Modify: `frontend/src/components/IntegrationSummary.tsx:94-103` (action buttons JSX)

- [ ] **Step 1: Replace action button CSS in App.css**

Replace the `.action-buttons`, `.btn-action`, `.action-title`, `.action-desc` rules:

```css
.action-buttons {
  display: flex;
  gap: 12px;
}

.btn-action {
  flex: 1;
  padding: 20px 24px;
  background: var(--accent-dim);
  border: 1px solid var(--border);
  border-left: 3px solid var(--border-bright);
  border-radius: var(--radius);
  color: var(--text);
  cursor: pointer;
  text-align: left;
  font-family: var(--font-mono);
  transition: border-left-color 0.15s, background 0.15s;
}

.btn-action:hover {
  border-left-color: var(--accent);
  background: rgba(0, 255, 100, 0.08);
}

.action-title {
  font-size: 0.9rem;
  font-weight: 600;
  margin-bottom: 6px;
  color: var(--accent);
}

.action-desc {
  font-size: 0.8rem;
  color: var(--text-dim);
}
```

- [ ] **Step 2: Update action button JSX in IntegrationSummary.tsx**

Replace the `<div className="action-buttons">` block (lines 94–103):

```tsx
<div className="action-buttons">
  <button className="btn btn-action" onClick={onViewMITRE}>
    <div className="action-title">→ MITRE ATT&CK Coverage</div>
    <div className="action-desc">Technique-level detection coverage across the ATT&CK matrix</div>
  </button>
  <button className="btn btn-action" onClick={onViewInsights}>
    <div className="action-title">→ Alert Insights</div>
    <div className="action-desc">Find duplicates, gaps, and merge opportunities</div>
  </button>
</div>
```

- [ ] **Step 3: Verify in browser**

Action buttons should show green `→` titles with dim descriptions, left-border accent that brightens on hover.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.css frontend/src/components/IntegrationSummary.tsx
git commit -m "feat: update action buttons to terminal left-border style with arrow prefix"
```

---

## Task 8: Error Banner + Remaining CSS Cleanup

**Files:**
- Modify: `frontend/src/App.css` (error rules, scrollbar, misc)

- [ ] **Step 1: Update error banner and error message in App.css**

Replace the `.error-message` and `.error-banner` rules:

```css
.error-message {
  color: var(--danger);
  font-size: 0.82rem;
  margin-top: 8px;
  padding: 8px 12px;
  background: var(--danger-dim);
  border-radius: var(--radius);
}

.error-banner {
  color: var(--danger);
  padding: 12px 16px;
  background: var(--danger-dim);
  border: 1px solid rgba(255, 77, 77, 0.25);
  border-radius: var(--radius);
  margin-bottom: 24px;
}
```

- [ ] **Step 2: Update scrollbar colors in App.css**

Replace the `/* ── Scrollbar ── */` section:

```css
/* ── Scrollbar ───────────────────────────────────── */
::-webkit-scrollbar { width: 5px; height: 5px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--border-bright); border-radius: 2px; }
::-webkit-scrollbar-thumb:hover { background: var(--accent); }
```

- [ ] **Step 3: Update summary header in App.css**

The `.summary-header`, `.cache-badge`, `.integration-table-wrap`, `.integration-table` rules — update surface/border references to match new tokens:

```css
.integration-summary {
  max-width: 1000px;
  margin: 0 auto;
}

.summary-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.summary-header h2 {
  font-size: 1.3rem;
  font-weight: 600;
  margin: 0;
}

.cache-info {
  display: flex;
  align-items: center;
  gap: 10px;
}

.cache-badge {
  padding: 3px 9px;
  background: var(--accent-dim);
  color: var(--accent);
  border-radius: 2px;
  font-size: 0.7rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.integration-table-wrap {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
  margin-bottom: 24px;
}

.integration-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.86rem;
}

.integration-table th {
  text-align: left;
  padding: 9px 14px;
  background: var(--bg);
  color: var(--text-dim);
  font-size: 0.68rem;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  font-weight: 600;
}

.integration-table td {
  padding: 9px 14px;
  border-top: 1px solid var(--border);
}

.integration-table code {
  font-size: 0.78rem;
  color: var(--text-code);
}

.integration-table .alert-count {
  font-weight: 600;
  text-align: center;
}
```

- [ ] **Step 4: Verify full app flow in browser**

Walk through the full flow:
1. Landing page: grid bg, edge wordmark, blinking cursor ✓
2. Header: wordmark, pulsing dot ✓
3. Summary page: stat cards with left-border, blind spots red ✓
4. Integration table: blind spot rows in red ✓
5. Action buttons: green `→` titles, hover effect ✓
6. Error states: red with dim background (trigger by killing backend) ✓

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.css
git commit -m "feat: update error states, scrollbar, and integration table to new tokens"
```

---

## Task 9: Fix Orphaned Variable References

Three places in App.css still reference removed tokens (`--green`, `--accent-hover`) or hardcoded indigo (`rgba(99, 102, 241,...)`). These will silently fall back to browser defaults (black/transparent) if not fixed.

**Files:**
- Modify: `frontend/src/App.css` (suggestions, tabs, alerts sections)

- [ ] **Step 1: Fix `.suggestion-query code` color (was `var(--green)`, removed)**

Find:
```css
.suggestion-query code {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 0.72rem;
  color: var(--green);
  white-space: pre-wrap;
  word-break: break-all;
}
```

Replace `color: var(--green)` with `color: var(--accent)`.

- [ ] **Step 2: Fix `.btn-generate:hover` (was `var(--accent-hover)`, removed)**

Find:
```css
.btn-generate:hover:not(:disabled) {
  background: var(--accent-hover);
}
```

Replace with:
```css
.btn-generate:hover:not(:disabled) {
  background: #33ff80;
}
```

- [ ] **Step 3: Fix hardcoded indigo in tabs**

Find:
```css
.tab-btn.active .tab-count {
  background: rgba(99, 102, 241, 0.2);
  color: var(--accent);
}
```

Replace `background: rgba(99, 102, 241, 0.2)` with `background: var(--accent-dim)`.

- [ ] **Step 4: Fix hardcoded indigo in unique alert cards**

Find:
```css
.unique-card .alert-name {
  background: rgba(99, 102, 241, 0.08);
  border-left: 3px solid var(--accent);
}
```

Replace `background: rgba(99, 102, 241, 0.08)` with `background: var(--accent-dim)`.

- [ ] **Step 5: Fix hardcoded red in suggestions error (optional polish)**

Find:
```css
.suggestions-error {
  margin-top: 8px;
  padding: 8px;
  background: rgba(239, 68, 68, 0.15);
  border: 1px solid var(--danger);
  border-radius: var(--radius);
  color: var(--danger);
  font-size: 0.8rem;
}
```

Replace `background: rgba(239, 68, 68, 0.15)` with `background: var(--danger-dim)`.

- [ ] **Step 6: Verify in browser**

Navigate to Alert Insights (tab view) — active tab count badge should be green, not purple. Click through to a suggestion card — the code block text should be neon green.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/App.css
git commit -m "fix: replace removed CSS variable references with new token equivalents"
```

---

## Post-Implementation Check

Run the dev server and confirm no console errors and no TypeScript errors:

```bash
cd frontend && npm run build
```

Expected: build succeeds with no errors. Zero warnings about unused CSS variables (the old `--accent-hover`, `--red`, `--orange`, `--yellow`, `--green` are gone).

If the build reports TypeScript errors in `IntegrationSummary.tsx` or `ClientSelector.tsx`, check that the JSX is syntactically valid — no missing closing tags.
