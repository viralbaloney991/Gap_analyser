# ThreatGraph Improvements — Design Spec
**Date:** 2026-05-10
**Status:** Approved

## Problem

The ThreatGraph bipartite visualization renders two disconnected columns — no edges appear. The root cause is that `deriveTids()` uses regex keyword matching on integration names like "CloudTrail" and "CrowdStrike", which never fire. The backend already computes `techToAlerts` (technique ID → alert names) but does not expose it in the API response. Secondary issues: the initial zoom fit is cramped (~25%) for large clients, and the graph lacks an orientation aid.

## Scope

Three improvements in one iteration:

1. **API-driven edges** — replace keyword heuristics with ground-truth data from the backend
2. **Gradient edge rendering** — edges visualize severity→coverage direction with colour gradients
3. **Tactic jump-list** — narrow sidebar for orientation and one-click tactic navigation

## Out of Scope

- New API endpoints or routes
- Edge bundling / hierarchical routing
- Minimap thumbnail
- Changes to bipartite layout algorithm, viewport hook, drill panels, posture bar, severity filter chips

---

## Section 1: Architecture & Data Flow

```
Backend (Go)                         Frontend (React/TS)
─────────────────                    ──────────────────────────────
TechniqueCoverageEntry               buildAlertRules()
  + AlertRules []string  ──────────▶   invert technique_coverage
    (from techToAlerts)                 alertName → Set<tid>
                                        a.tids = [...alertToTids.get(int.name)]
                                    ↓
                                    GraphCanvas edges
                                      <linearGradient> per edge in <defs>
                                      severity colour → coverage colour
                                    ↓
                                    TacticJumpList (new component)
                                      13 tactic pills → setVp() on click
```

**Nothing changes:** bipartite layout, viewport hook, drill panels, severity filters, zoom controls, posture bar.

---

## Section 2: Backend Change

**`backend/internal/models/models.go`**
```go
type TechniqueCoverageEntry struct {
    Name       string   `json:"name"`
    Tactic     string   `json:"tactic"`
    AlertCount int      `json:"alert_count"`
    Weak       bool     `json:"weak,omitempty"`
    AlertRules []string `json:"alert_rules,omitempty"` // ← new
}
```

**`backend/internal/mitre/mitre.go`** (line ~532)
```go
techniqueCoverage[t.ID] = models.TechniqueCoverageEntry{
    Name:       t.Name,
    Tactic:     t.Tactic,
    AlertCount: alertCount,
    Weak:       alertCount > 0 && !techHasScoped[t.ID],
    AlertRules: techToAlerts[t.ID], // ← new, already computed above
}
```

**`frontend/src/types/index.ts`**
```ts
export interface TechniqueCoverageEntry {
    name: string;
    tactic: string;
    alert_count: number;
    weak?: boolean;
    alert_rules?: string[]; // ← new
}
```

No new tests required beyond confirming the field appears in the JSON response; `techToAlerts` correctness is already covered via `AlertCount`.

---

## Section 3: Frontend Edge Wiring

**`buildAlertRules()` in `ThreatGraph.tsx`**

Replace `deriveTids(int.name)` with an inversion of `technique_coverage`:

```ts
function buildAlertRules(data: AnalyzeResponse): AlertRule[] {
  const techCov = data.mitre_coverage.technique_coverage ?? {};

  // Invert: alertName → Set<techniqueID>
  const alertToTids = new Map<string, Set<string>>();
  for (const [tid, entry] of Object.entries(techCov)) {
    for (const name of entry.alert_rules ?? []) {
      if (!alertToTids.has(name)) alertToTids.set(name, new Set());
      alertToTids.get(name)!.add(tid);
    }
  }

  return data.integrations
    .filter(int => int.alert_count > 0)
    .map((int, i) => ({
      // ... all existing fields unchanged ...
      tids: [...(alertToTids.get(int.name) ?? [])],
    }));
}
```

**Delete:** `KW_TIDS` constant and `deriveTids()` function — dead code once `alert_rules` is wired.

**Backward compatibility:** if `alert_rules` is absent (old backend), `alertToTids` stays empty and `tids` is `[]`. Graph renders with no edges rather than crashing.

---

## Section 4: Gradient Edge Rendering

Each SVG edge gets a `<linearGradient>` generated in the `<defs>` block of `GraphCanvas`.

**Gradient ID:** `edge-{aid}-{tid}` — one per edge, scoped to avoid collisions.

**Gradient definition:**
```tsx
<linearGradient id={`edge-${e.aid}-${e.tid}`} x1="0%" y1="0%" x2="100%" y2="0%">
  <stop offset="0%"   stopColor={SEV_COLOR[e.sev]} stopOpacity={focusOpacity.alertStop} />
  <stop offset="100%" stopColor={COV_COLOR[covMap[e.tid]]} stopOpacity={focusOpacity.techStop} />
</linearGradient>
```

**Opacity levels** (replaces flat `opacity` prop on the path):

| State   | Alert stop opacity | Tech stop opacity |
|---------|--------------------|-------------------|
| Ambient | 0.5                | 0.12              |
| Focused | 1.0                | 0.85              |
| Dimmed  | 0.05               | 0.05              |

**Edge path:** unchanged bezier `M ax ay C cx1 ay cx2 ty tx ty`. Stroke becomes `url(#edge-{aid}-{tid})` instead of a flat colour.

**`covMap`:** a `Record<string, Coverage>` derived from `techniques` array, passed into `GraphCanvas` so gradients can resolve technique coverage colour without iterating the array per-edge.

---

## Section 5: Tactic Jump-List

**New component:** `TacticJumpList`

**Props:**
```ts
interface TacticJumpListProps {
  tacticBands: BipartiteLayout['tacticBands'];
  tacticBreakdown: Record<string, TacticCoverage>; // from MITRECoverageSummary
  vp: Vp;
  viewH: number;       // canvas container height
  onJump: (bandY: number) => void;
}
```

**Pill colour rules:**
- Coverage ≥ 60% → green (`var(--cx-ok)`)
- Coverage 20–59% → orange (`var(--cx-warn)`)
- Coverage < 20% or 0 → muted (`var(--cx-fg-3)`)

**Active detection:** a tactic band is "active" when its vertical midpoint falls within the current viewport:
```ts
const bandMid = band.y * vp.k + vp.y;
const isActive = bandMid > 0 && bandMid < viewH;
```

**Jump handler** (passed down from parent, calls `setVp`):
```ts
const handleJump = (bandY: number) => {
  setVp(v => ({
    ...v,
    y: -(bandY * v.k) + viewH / 2,
  }));
};
```

**Placement:** rendered as a fixed overlay on the left side of the canvas wrap, stacked below the existing legend overlay. Width: 96px. Scrollable if 13 tactics overflow.

**Coverage % source:** `MITRECoverageSummary.tactic_breakdown[tactic_id].percent` (`TacticCoverage.percent`) — already in the API response, no backend change needed.

---

## Error Handling & Edge Cases

| Case | Behaviour |
|------|-----------|
| `alert_rules` absent (old backend) | `tids = []`, no edges, no crash |
| Technique in `tids` not in layout | edge skipped (existing guard: `if (!alertPos[a.id] \|\| !techPos[tid]) return null`) |
| Tactic in jump-list not in `tactic_breakdown` | pill shows "—" for coverage %, still renders |
| Zero-technique tactic band | pill hidden (band not in `tacticBands`) |
| `covMap` missing entry for edge's tid | gradient falls back to `COV_COLOR['none']` |

---

## Files Changed

| File | Change |
|------|--------|
| `backend/internal/models/models.go` | Add `AlertRules []string` field |
| `backend/internal/mitre/mitre.go` | Populate `AlertRules` from `techToAlerts` |
| `frontend/src/types/index.ts` | Add `alert_rules?: string[]` to interface |
| `frontend/src/components/ThreatGraph.tsx` | Replace `deriveTids`/`KW_TIDS` with inversion; add gradient defs; add `TacticJumpList` component; pass `covMap` to `GraphCanvas` |
| `frontend/src/App.css` | Add styles for `.cx-tactic-jumplist` and pills |
