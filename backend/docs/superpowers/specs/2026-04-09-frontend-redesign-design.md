# Frontend Redesign — Terminal/War Room Aesthetic

**Date:** 2026-04-09
**Scope:** Full visual redesign of `frontend/src/App.css` + component markup adjustments
**Status:** Design approved, ready for implementation

---

## 1. Design Direction

**Aesthetic:** Terminal / War Room
**Tone:** Operational, alive, precise. Feels like a real SOC tool, not a SaaS dashboard.
**Unforgettable element:** The pulsing green status dot + blinking cursor in the header signals the system is live. Every number feels like live intelligence data.

---

## 2. Design Tokens

Replace all current CSS variables in `App.css`:

```css
:root {
  /* Backgrounds */
  --bg:             #020b05;   /* near-black with green tint */
  --bg-mid:         #0a1f0f;   /* slightly lighter surface */
  --surface:        #0d2412;   /* card/panel background */
  --surface-hover:  #122b17;   /* hover state */
  --border:         rgba(0, 255, 100, 0.12);
  --border-bright:  rgba(0, 255, 100, 0.3);

  /* Text */
  --text:           #e0ffe8;   /* near-white with green tint */
  --text-dim:       rgba(0, 255, 100, 0.45);
  --text-code:      #b0ffca;   /* slightly dimmer for inline code */

  /* Accent */
  --accent:         #00ff64;   /* primary neon green */
  --accent-dim:     rgba(0, 255, 100, 0.15);

  /* Semantic */
  --danger:         #ff4d4d;
  --danger-dim:     rgba(255, 77, 77, 0.12);
  --warn:           #ffb347;
  --warn-dim:       rgba(255, 179, 71, 0.12);
  /* --ok is intentionally the same as --accent; use --accent directly */

  /* Shared */
  --radius:         3px;       /* tighter radius for terminal feel */
  --font-mono:      'IBM Plex Mono', 'JetBrains Mono', monospace;
}
```

**Remove:** `--accent-hover`, `--red`, `--orange`, `--yellow`, `--green` (consolidate into semantic tokens above).

---

## 3. Typography

**Font:** IBM Plex Mono loaded via Google Fonts. One font family throughout — weight variations create hierarchy.

```html
<!-- in index.html <head> -->
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@300;400;500;600;700&display=swap" rel="stylesheet">
```

**Scale:**

| Role | Weight | Size | Usage |
|------|--------|------|-------|
| Wordmark | 300 | 1.6rem | App title in header |
| Section heading | 600 | 1.1rem | Page-level h2 |
| Label | 500 | 0.72rem + 0.15em tracking | Form labels, table headers |
| Body | 400 | 0.88rem | Table cells, descriptions |
| Monospace code | 400 | 0.78rem | Alert names, queries |
| Stat number | 700 | 1.8rem | Stat cards |

`font-family` on `body` changes to: `var(--font-mono)`.

---

## 4. Components

### 4.1 Header

**Layout:** 3-column flex (same as current), but restyled.

- **Left:** `← Back` button — plain text with `>_` prefix style, no box
- **Center:** Wordmark — `Alert` (weight 300) + `Analyzer` (weight 700), small `CX` superscript in `--text-dim`. Clickable to go home.
- **Right:** Status indicator — `<span class="status-dot"></span> ONLINE` in `--text-dim` at 0.7rem, letter-spacing 0.15em

**Background:** `var(--bg)` with bottom border `1px solid var(--border)`.
**No** background fill change from `--surface` — flattens it against the page.

**Animation — Status dot:**
```css
.status-dot {
  width: 6px; height: 6px;
  background: var(--accent);
  border-radius: 50%;
  animation: pulse-dot 2s ease-in-out infinite;
}
@keyframes pulse-dot {
  0%, 100% { opacity: 1; box-shadow: 0 0 4px var(--accent); }
  50%       { opacity: 0.3; box-shadow: none; }
}
```

---

### 4.2 ClientSelector (Landing Page)

**Layout:** Full-viewport centered column, grid background behind content.

**Structure:**
```
[full viewport, --bg background]
  [dot-grid overlay, masked to fade at bottom]
  [corner mark top-left: "CX_ALERTS v2.1" in --text-dim]
  [corner status top-right: [dot] ONLINE]
  [bottom-anchored content block, left-aligned, max-width 480px, centered horizontally]
    [wordmark: "Alert Analyzer"]  ← weight 300 / 700 split
    [subtitle: "CORALOGIX INTEGRATION INTELLIGENCE"]
    [inline form: [select▼] [ANALYZE →]]
```

**Dot grid:**
```css
background-image:
  linear-gradient(var(--border) 1px, transparent 1px),
  linear-gradient(90deg, var(--border) 1px, transparent 1px);
background-size: 28px 28px;
mask-image: linear-gradient(to bottom, transparent 0%, black 50%);
```

**Inline form:** The select and button sit on the same line. Select gets `flex: 1`, button is fixed width. No separate "label" above — the form is self-evident.

**Animation — Blinking cursor:** Append a `::after` pseudo-element scoped to the landing page wordmark only (class `.landing-wordmark`, not the header `h1`):
```css
.landing-wordmark::after {
  content: '_';
  color: var(--accent);
  animation: blink 1s step-end infinite;
}
@keyframes blink {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0; }
}
```

---

### 4.3 Stats Row

Keep 6 stat cards but introduce **visual hierarchy by criticality:**

| Stat | Color rule |
|------|-----------|
| Integrations | `--text` (neutral) |
| Active Alerts | `--text` (neutral) |
| Security Alerts | `--accent` (highlighted) |
| Vendor Covered | `--text-dim` (de-emphasised) |
| With Coverage | `--accent` |
| Blind Spots | `--danger` if > 0, else `--accent` |

**Card style change:** Replace equal-weight boxes with a left-border accent:
```css
.stat-card {
  border-left: 2px solid var(--border-bright);
  background: var(--accent-dim);
  border-radius: var(--radius);
  padding: 14px 18px;
}
```
Blind spot card overrides to `border-left-color: var(--danger)` and `background: var(--danger-dim)` when count > 0.
This is applied inline via `style` prop in `IntegrationSummary.tsx`.

---

### 4.4 Integration Table

**Blind spot rows:** Replace `opacity: 0.6` with explicit danger styling:
```css
.row-blind-spot td { color: var(--danger); }
.row-blind-spot td:first-child::before { content: ''; }
```
The `⚠️` emoji stays, but row text shifts to `--danger` so it stands out instead of fading.

**Table header:** All-caps, `--text-dim`, 0.12em letter-spacing (same as now but matches new font).

**`code` elements** (application/subsystem): Inherit `--font-mono` naturally — no change needed.

---

### 4.5 Action Buttons (MITRE / Insights)

Replace generic bordered boxes with terminal-style cards:

```css
.btn-action {
  border-left: 3px solid var(--border-bright);
  background: var(--accent-dim);
  transition: border-left-color 0.15s, background 0.15s;
}
.btn-action:hover {
  border-left-color: var(--accent);
  background: rgba(0, 255, 100, 0.08);
}
```

**Title:** Prepend `→ ` before the action title text (done in JSX, not CSS).
**Description:** `--text-dim`, 0.82rem. Same as current.

---

### 4.6 Buttons

```css
.btn-primary {
  background: var(--accent);
  color: var(--bg);           /* dark text on bright green */
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.btn-secondary, .btn-small {
  background: transparent;
  border: 1px solid var(--border-bright);
  color: var(--accent);
}
.btn-secondary:hover, .btn-small:hover {
  background: var(--accent-dim);
}
```

---

### 4.7 Error Banner

Update colors only:
```css
.error-banner {
  border-color: rgba(255, 77, 77, 0.3);
  background: var(--danger-dim);
  color: var(--danger);
}
```

---

## 5. Animations Summary

| Element | Animation | Duration |
|---------|-----------|----------|
| Header status dot | `pulse-dot` — opacity + glow fade | 2s ease-in-out infinite |
| Landing page wordmark | `blink` cursor (`_`) — step-end | 1s step-end infinite |

No other animations. Everything else is instant or uses `transition: 0.15s`.

---

## 6. Files Changed

| File | Change type |
|------|------------|
| `frontend/index.html` | Add Google Fonts `<link>` for IBM Plex Mono |
| `frontend/src/App.css` | Full rewrite of design tokens, base styles, all component styles |
| `frontend/src/App.tsx` | Add `.status-dot` + "ONLINE" span to header; update `<h1>` markup for wordmark |
| `frontend/src/components/ClientSelector.tsx` | Full layout change to grid/edge-anchored pattern |
| `frontend/src/components/IntegrationSummary.tsx` | Stat card inline `style` for blind spot count; action button title `→` prefix; blind spot row class rename |

No other files change. MITRE heatmap, AlertInsights, and api service are untouched.

---

## 7. Out of Scope

- Loading skeleton states (separate future task)
- Page transitions / route animations
- Mobile responsiveness changes
- MITRE heatmap or AlertInsights visual redesign
