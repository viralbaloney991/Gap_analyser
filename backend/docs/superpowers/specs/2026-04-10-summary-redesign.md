# Integration Summary Redesign — Command Center Layout

**Date:** 2026-04-10
**Scope:** Redesign `IntegrationSummary` component from a linear vertical layout to a two-column Command Center layout.
**Status:** Design approved, ready for implementation

---

## 1. Layout

Two-column CSS grid. Left panel fixed at `200px`, right panel `1fr`.

```
┌─────────────────────┬────────────────────────────────────┐
│ [ CLIENT ] — SUMMARY│  Status  Integration  App  Alerts  │
│ CACHED · [REFRESH]  │ ─────────────────────────────────  │
│                     │  [OK]   k8s-prod      kube   312   │
│  12  Integrations   │  [OK]   nginx-gw      nginx  201   │
│ 847  Active Alerts  │  [!!]   vault         —        0   │
│ 203  Security       │                                    │
│   9  With Coverage  │                                    │
│   3  Blind Spots ◀  │                                    │
│      (danger red)   │                                    │
│                     │                                    │
│ → MITRE Coverage    │                                    │
│   Technique heatmap │                                    │
│ → Alert Insights    │                                    │
│   Gaps & duplicates │                                    │
└─────────────────────┴────────────────────────────────────┘
```

---

## 2. Left Panel — Command Panel

Flexbox column, `justify-content: space-between` so stats fill top and actions anchor to bottom.

### 2.1 Header

```
[ CLIENT ] — INTEGRATION SUMMARY
CACHED · [ REFRESH ]
```

- Client name wrapped in brackets: `[ ${clientName} ]`
- `CACHED` label appears only when `data.cached === true`
- Refresh button: `[ REFRESH ]` / `[ REFRESHING... ]` when loading

### 2.2 Stat Cards

Vertical stack, each card: left-border `2px solid var(--accent)`, padding `6px 10px`.

| Stat | Condition |
|------|-----------|
| Integrations | always |
| Active Alerts | always |
| Security Alerts | always |
| Vendor Covered | only when `> 0` |
| With Coverage | always |
| Blind Spots | always; border + bg switches to `var(--danger)` / `var(--danger-dim)` when `> 0` |

### 2.3 Action Buttons

Two `btn-action` buttons at the bottom of the left panel (anchored with `margin-top: auto` on the actions wrapper):

- `→ MITRE ATT&CK Coverage` — subtitle: "Technique-level detection coverage"
- `→ Alert Insights` — subtitle: "Find duplicates, gaps, and merge opportunities"

---

## 3. Right Panel — Integration Table

Full-height table, `overflow-y: auto` for long client lists.

**Columns (4):** Status · Integration · Application · Alerts

**Status column:** Replace emoji with terminal tags:
- `alert_count > 0` → `[OK]` — green bordered tag (`var(--accent)`)
- `alert_count === 0` → `[!!]` — red bordered tag (`var(--danger)`)

**Blind spot rows:** `color: var(--danger)`, `background: var(--danger-dim)` — existing `.row-blind-spot` class retained.

**Subsystem column removed.**

---

## 4. CSS Changes

New classes in `App.css`:

```
.summary-grid          /* two-column grid wrapper */
.summary-panel         /* left command panel — flex column */
.summary-panel-header  /* client name + refresh block */
.summary-panel-stats   /* vertical stat card stack */
.summary-panel-actions /* action buttons, margin-top: auto */
.summary-table-panel   /* right panel, overflow-y: auto */
.status-tag            /* [OK] / [!!] base styles */
.status-tag--ok        /* accent border + color */
.status-tag--blind     /* danger border + color */
```

Existing classes removed: `.stats-row`, `.action-buttons`, `.stat-card` (replaced by new panel layout).

---

## 5. Files Changed

| File | Change |
|------|--------|
| `frontend/src/components/IntegrationSummary.tsx` | Full layout restructure; drop Subsystem column; replace emoji with `[OK]`/`[!!]` tags |
| `frontend/src/App.css` | Add new grid/panel classes; remove `.stats-row`, `.action-buttons`, `.stat-card` |
