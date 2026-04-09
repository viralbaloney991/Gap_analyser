# Integration Summary Redesign — Command Center Layout

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current linear summary layout with a two-column Command Center — stats + actions in a fixed left panel, integration table filling the right.

**Architecture:** Two tasks — CSS first (add new classes, remove old ones), then component restructure. No backend changes. No new logic — pure layout and render changes.

**Tech Stack:** React 19, TypeScript, CSS custom properties (no new dependencies)

---

## File Map

| File | Change |
|------|--------|
| `frontend/src/App.css` | Add `.summary-grid`, `.summary-panel`, `.summary-panel-header`, `.summary-panel-stats`, `.summary-panel-actions`, `.summary-table-panel`, `.status-tag`, `.status-tag--ok`, `.status-tag--blind`; remove `.stats-row`, `.stat-card`, `.stat-number`, `.stat-desc`, `.action-buttons` |
| `frontend/src/components/IntegrationSummary.tsx` | Full layout restructure; drop Subsystem column; replace emoji with `[OK]`/`[!!]` tags |

---

## Task 1: CSS — New Grid Classes

**Files:**
- Modify: `frontend/src/App.css` (lines 301–445 — the Integration Summary section)

- [ ] **Step 1: Replace the Integration Summary CSS block**

In `frontend/src/App.css`, find the section between the comments `/* ── Integration Summary ─────────────────────────── */` and `/* ── MITRE Heatmap ───────────────────────────────── */` (lines 301–474). Replace the entire Integration Summary block with:

```css
/* ── Integration Summary ─────────────────────────── */
.integration-summary {
  height: calc(100vh - 53px - 64px); /* viewport minus header minus app-main padding */
  display: flex;
  flex-direction: column;
}

/* Two-column command center grid */
.summary-grid {
  display: grid;
  grid-template-columns: 210px 1fr;
  gap: 0;
  flex: 1;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
}

/* Left: Command Panel */
.summary-panel {
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border-right: 1px solid var(--border);
  padding: 18px 16px;
  gap: 0;
}

.summary-panel-header {
  margin-bottom: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.summary-panel-header h2 {
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--accent);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin-bottom: 8px;
  line-height: 1.4;
}

.summary-panel-header .cache-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.cache-badge {
  padding: 2px 7px;
  background: var(--accent-dim);
  color: var(--accent);
  border-radius: 2px;
  font-size: 0.62rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}

/* Vertical stat cards */
.summary-panel-stats {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1;
}

.stat-card {
  background: transparent;
  border-left: 2px solid var(--border-bright);
  padding: 6px 10px;
}

.stat-card.stat-card--danger {
  border-left-color: var(--danger);
  background: var(--danger-dim);
}

.stat-number {
  font-size: 1.3rem;
  font-weight: 700;
  color: var(--text);
  line-height: 1;
}

.stat-card--danger .stat-number {
  color: var(--danger);
}

.stat-desc {
  font-size: 0.6rem;
  color: var(--text-dim);
  margin-top: 2px;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}

/* Action buttons — anchored to bottom of panel */
.summary-panel-actions {
  margin-top: auto;
  padding-top: 14px;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.btn-action {
  width: 100%;
  padding: 10px 12px;
  background: transparent;
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
  background: rgba(0, 255, 100, 0.05);
}

.action-title {
  font-size: 0.75rem;
  font-weight: 600;
  margin-bottom: 3px;
  color: var(--accent);
}

.action-desc {
  font-size: 0.65rem;
  color: var(--text-dim);
  line-height: 1.3;
}

/* Right: Table panel */
.summary-table-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow-y: auto;
  background: var(--bg);
}

.integration-table-wrap {
  flex: 1;
}

.integration-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.84rem;
}

.integration-table th {
  text-align: left;
  padding: 10px 14px;
  background: var(--surface);
  color: var(--text-dim);
  font-size: 0.66rem;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  font-weight: 600;
  position: sticky;
  top: 0;
  z-index: 1;
}

.integration-table td {
  padding: 9px 14px;
  border-top: 1px solid var(--border);
}

.integration-table .alert-count {
  font-weight: 600;
  text-align: right;
}

.row-blind-spot td {
  color: var(--danger);
  background: var(--danger-dim);
}

/* Status tags — replace emoji */
.status-tag {
  display: inline-block;
  font-size: 0.65rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  padding: 1px 5px;
  border: 1px solid;
  border-radius: 2px;
  font-family: var(--font-mono);
}

.status-tag--ok {
  border-color: var(--accent);
  color: var(--accent);
}

.status-tag--blind {
  border-color: var(--danger);
  color: var(--danger);
}
```

- [ ] **Step 2: Verify build passes**

```bash
cd frontend && npm run build
```

Expected: `✓ built in Xms` — no TypeScript or CSS errors. (The component still uses old class names so there will be no visual breakage yet — the old classes are simply no longer in the stylesheet. The build must still succeed.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.css
git commit -m "style: add command center grid classes for summary redesign"
```

---

## Task 2: Component Restructure

**Files:**
- Modify: `frontend/src/components/IntegrationSummary.tsx`

- [ ] **Step 1: Replace IntegrationSummary.tsx with the new layout**

Full replacement of `frontend/src/components/IntegrationSummary.tsx`:

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

  const sorted = [...integrations].sort((a, b) => b.alert_count - a.alert_count);
  const blindSpots = integrations.filter((i) => i.alert_count === 0);

  return (
    <div className="integration-summary">
      <div className="summary-grid">

        {/* Left: Command Panel */}
        <div className="summary-panel">
          <div className="summary-panel-header">
            <h2>[ {clientName} ]<br />Integration Summary</h2>
            <div className="cache-info">
              {data.cached && <span className="cache-badge">Cached</span>}
              <button
                className="btn btn-small"
                onClick={onRefresh}
                disabled={loading}
              >
                {loading ? 'Refreshing...' : 'Refresh'}
              </button>
            </div>
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
            <div className={`stat-card${blindSpots.length > 0 ? ' stat-card--danger' : ''}`}>
              <div className="stat-number">{blindSpots.length}</div>
              <div className="stat-desc">Blind Spots</div>
            </div>
          </div>

          <div className="summary-panel-actions">
            <button className="btn-action" onClick={onViewMITRE}>
              <div className="action-title">→ MITRE ATT&CK Coverage</div>
              <div className="action-desc">Technique-level detection coverage across the ATT&CK matrix</div>
            </button>
            <button className="btn-action" onClick={onViewInsights}>
              <div className="action-title">→ Alert Insights</div>
              <div className="action-desc">Find duplicates, gaps, and merge opportunities</div>
            </button>
          </div>
        </div>

        {/* Right: Table Panel */}
        <div className="summary-table-panel">
          <div className="integration-table-wrap">
            <table className="integration-table">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Integration</th>
                  <th>Application</th>
                  <th>Alerts</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((integ, i) => (
                  <tr key={i} className={integ.alert_count === 0 ? 'row-blind-spot' : ''}>
                    <td>
                      {integ.alert_count > 0
                        ? <span className="status-tag status-tag--ok">[OK]</span>
                        : <span className="status-tag status-tag--blind">[!!]</span>
                      }
                    </td>
                    <td>{integ.name}</td>
                    <td><code>{integ.application || '—'}</code></td>
                    <td className="alert-count">{integ.alert_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify build passes with no TypeScript errors**

```bash
cd frontend && npm run build
```

Expected output:
```
✓ built in Xms
```

No errors. If TypeScript complains about a missing property on `AnalyzeResponse`, check `frontend/src/types/index.ts` — `stats.integrations_with_alerts` must exist on the `Stats` type. If it doesn't, use `stats.integrations_with_alerts ?? 0`.

- [ ] **Step 3: Manual visual check**

Run the dev server and navigate to the summary page:

```bash
./dev.sh start   # or: cd frontend && npm run dev
```

Open `http://localhost:5173`, select a client, click Analyze. Verify:
- [ ] Two-column layout: left panel (~210px) + table filling the right
- [ ] Client name shows as `[ CLIENTNAME ]` + line break + `Integration Summary`
- [ ] Stat cards render in a vertical stack with left green border
- [ ] Blind Spots card shows in red when count > 0
- [ ] Action buttons are at the bottom of the left panel
- [ ] Table has 4 columns: Status, Integration, Application, Alerts (no Subsystem)
- [ ] `[OK]` tag is green-bordered, `[!!]` tag is red-bordered
- [ ] Blind spot rows show in red with red background

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/IntegrationSummary.tsx
git commit -m "feat: redesign IntegrationSummary as two-column command center

- Left panel: stat cards + action buttons (anchored bottom)
- Right panel: integration table, full height, scrollable
- Replace emoji (✅/⚠️) with terminal [OK]/[!!] tags
- Drop Subsystem column
- Client name displayed as [ NAME ] bracket style"
```
