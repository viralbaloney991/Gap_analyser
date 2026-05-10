# Detection Builder — Design Spec

**Date:** 2026-05-10  
**Status:** Approved  
**Reference prototype:** `/Users/aviral.baloni/Downloads/design_handoff_detection_builder/`

---

## Overview

A new CXAlert view (`'builder'`) where a SOC analyst drags MITRE ATT&CK techniques into an ordered chain, clicks **Generate alerts**, and receives 3–4 LLM-generated **flow alerts** with per-stage time windows plus a final **correlation rule** tying them together.

The design is fully specified in the handoff prototype. This document maps it to the existing CXAlert codebase patterns.

---

## 1. New View: `'builder'`

### 1.1 `App.tsx` changes

**`View` type** (line 15):
```ts
type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder';
```

**Navigation entry point:** Add a "Build detections" button to `IntegrationSummary` (alongside the existing "View MITRE coverage" button). It calls `onViewBuilder(() => setView('builder'))` — prop pattern identical to `onViewMITRE`.

**`goBack()` logic** (lines 85-96): `'builder'` navigates back to `'summary'`.

**Breadcrumb label:** `'builder'` → `"Build detections"` (same pattern as other views, lines 108-117).

**Render block** (inside `AnimatePresence`, after line 207):
```tsx
{view === 'builder' && data && (
  <motion.div key="builder" {...FADE_UP} transition={FADE_UP_TRANSITION}>
    <DetectionBuilder
      clientName={clientName}
      mitreCoverage={data.mitre_coverage}
    />
  </motion.div>
)}
```

---

## 2. Frontend: `DetectionBuilder` Component

**File:** `frontend/src/components/DetectionBuilder.tsx`

### 2.1 Props

```ts
interface DetectionBuilderProps {
  clientName: string;
  mitreCoverage: MITRECoverageResult; // used to mark covered techniques in the grid
}
```

### 2.2 State

```ts
const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
const [query, setQuery] = useState('');
const [generating, setGenerating] = useState(false);
const [result, setResult] = useState<GenerationResult | null>(null);
```

Derived:
```ts
const selected = useMemo(() =>
  MITRE_TECHNIQUES.filter(t => selectedIds.has(t.id)), [selectedIds]);
```

### 2.3 Layout

```
┌─────────────────────────────────────────────────────────────────┐
│  Page intro (2-col): description left, search right             │
├──────────────────────────────────┬──────────────────────────────┤
│  Source matrix (1fr)             │  Right column (480px sticky) │
│  MITRE tactic columns            │  - Basket (drop zone)        │
│  Draggable tech cards            │  - GeneratedPanel (below)    │
└──────────────────────────────────┴──────────────────────────────┘
```

Grid: `grid-template-columns: 1fr 480px`. Below 1100px: single column.

### 2.4 Drag-and-drop wiring

- `SourceMatrix` cards: `draggable={!used}`, `onDragStart` sets `dataTransfer` with tech ID
- `Basket` drop target: `onDrop` fires `window.dispatchEvent(new CustomEvent('basket-drop', { detail: id }))`
- `DetectionBuilder` listens for `basket-drop` and calls `addTech()`
- Double-click on source card also calls `addTech()` (keyboard-friendly fallback)

### 2.5 Generate flow

1. `setGenerating(true); setResult(null)`
2. Call `buildDetection(clientName, selected.map(t => t.id))`
3. `setTimeout(() => { setResult(out); setGenerating(false); }, 600)` — ensures loading state is perceptible
4. On API failure: fall back to `mockGenerate(selected, MITRE_TACTICS)` (imported from `mitre-catalog.ts`)

---

## 3. Sub-components

### 3.1 `SourceMatrix`

Groups `MITRE_TECHNIQUES` by tactic, filters by `query`, renders tactic columns.

Each **tech card** (`.tech-card`):
- `T-id` (mono, `--cx-accent-2`), name, source hint, optional ✓ badge
- Hover: `translateY(-1px)`, border → `--cx-accent`
- Used state: `opacity: 0.4`, dashed border, `cursor: not-allowed`, ✓ badge top-right
- Drag: `effectAllowed = 'copy'`, transfers tech ID

### 3.2 `Basket`

Drop zone that auto-orders techniques by `tacticOrder` and groups by tactic into stage cards.

**Empty state:** Dashed border tile, `⊕` icon, explanatory text.

**Filled state (`.basket-flow`):**
- Groups techniques by tactic → render as numbered stage cards (`01`, `02`, …)
- `→` arrow separators between stages
- Each chip shows T-id (mono) + name + `×` remove button

**Header actions:**
- "Clear" ghost button (only when count > 0)
- "Generate alerts →" primary button — disabled when `selected.length < 2 || generating`
- While generating: spinner + "Generating…" label

**Drag-over visual:** `rgba(99,102,241,0.06)` tint + 2px inset `--cx-accent` ring

### 3.3 `GeneratedPanel`

Appears (slide-up, 220ms `cubic-bezier(0.2,0.8,0.2,1)`) after generation completes.

Sections in order:
1. **Panel header:** eyebrow "GENERATED DETECTION CHAIN", title = `result.correlation.name`, "Regenerate" ghost + `×` close
2. **Validation card** (`val-ok` / `val-warnings` / `val-invalid`): colored border + dot, verdict label, bulleted findings with level icons (`i` / `!` / `✕`)
3. **Flow alerts** section: "FLOW ALERTS `<count>`" header, list of alert cards
4. **Correlation rule** card: indigo gradient bg, name, mono logic string, global window pill
5. **Action row:** "Save to detections →" primary + "Export YAML" secondary (both fire TBD endpoint for now)

**Alert card layout:**
- Left 3px severity stripe (`SEV_COLOR[severity]`)
- Step number pill (`01`, `02`, …), name, description, severity badge
- Logic block (1px border, 12px, line-height 1.5)
- Meta row: `Technique T-id` · `Source` · **`Window Xm`** (tinted accent pill — headline feature)
- **"Why this window:"** sub-line (11px, `--cx-fg-3`) — surfaces model's time-window reasoning

### 3.4 `GenSkeleton`

Four shimmer blocks (`linear-gradient` animating `background-position`, 1.4s loop) shown while `generating === true`.

---

## 4. MITRE Catalog

**File:** `frontend/src/data/mitre-catalog.ts`

Static TypeScript module exporting `MITRE_TACTICS: Tactic[]` and `MITRE_TECHNIQUES: Technique[]`. Ported directly from `mitre-catalog.js` in the handoff.

```ts
export interface Tactic {
  id: string;      // 'TA0001'
  name: string;    // 'Initial Access'
  short: string;   // column label
  order: number;   // 0..12 for kill-chain ordering
}

export interface MITRETechnique {
  id: string;          // 'T1566'
  name: string;
  tactic: string;      // 'TA0001'
  tacticName: string;  // 'Initial Access' — denormalized for prompt building
  tacticOrder: number; // copied from parent tactic
  source: string;      // telemetry hint e.g. 'Email gateway / EDR'
}
```

Also export `mockGenerate(selectedTechs, tactics): GenerationResult` for offline/failure fallback.

Also export `buildPrompt(selectedTechs, tactics): string` for use by the backend integration (keep frontend copy for reference only; backend uses the Go port).

---

## 5. TypeScript Types

**File:** `frontend/src/types/index.ts` — add:

```ts
export interface ValidationFinding {
  level: 'info' | 'warn' | 'error';
  message: string;
}

export interface FlowAlert {
  name: string;
  description: string;
  techniqueId: string;
  logic: string;
  window: '5m' | '15m' | '30m' | '1h' | '6h' | '12h' | '24h';
  windowReason: string;
  source: 'EDR' | 'CloudTrail' | 'IdP' | 'Email' | 'Network' | 'WAF';
  severity: 'critical' | 'high' | 'medium' | 'low';
}

export interface CorrelationRule {
  name: string;
  logic: string;
  window: '1h' | '24h' | '72h';
  severity: 'critical' | 'high';
}

export interface GenerationResult {
  validation: {
    verdict: 'ok' | 'warnings' | 'invalid';
    findings: ValidationFinding[];
  };
  alerts: FlowAlert[];
  correlation: CorrelationRule;
}
```

---

## 6. Frontend API: `services/api.ts`

Add:
```ts
export async function buildDetection(
  client: string,
  techniques: MITRETechnique[], // full objects from catalog (frontend has them)
  provider?: string,
  force?: boolean
): Promise<GenerationResult> {
  const res = await fetch('/api/build-detection', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client,
      techniques: techniques.map(t => ({
        id: t.id, name: t.name,
        tactic_id: t.tactic, tactic_name: t.tacticName,
        tactic_order: t.tacticOrder, source: t.source,
      })),
      provider,
      force,
    }),
  });
  if (!res.ok) throw new Error(`buildDetection failed: ${res.status}`);
  return res.json();
}
```

Pattern follows `fetchCorrelations` in `api.ts` (lines 93-117).

---

## 7. Backend: `POST /api/build-detection`

### 7.1 Route registration

In `backend/internal/api/handlers.go` (route setup block, near line 56):
```go
mux.HandleFunc("POST /api/build-detection", h.HandleBuildDetection)
```

### 7.2 Models (`backend/internal/models/models.go`)

```go
type BuildDetectionTechnique struct {
  ID          string `json:"id"`           // "T1078"
  Name        string `json:"name"`         // "Valid Accounts"
  TacticID    string `json:"tactic_id"`    // "TA0001"
  TacticName  string `json:"tactic_name"`  // "Initial Access"
  TacticOrder int    `json:"tactic_order"` // 0..12
  Source      string `json:"source"`       // "IdP / EDR"
}

type BuildDetectionRequest struct {
  Client     string                    `json:"client"`
  Techniques []BuildDetectionTechnique `json:"techniques"` // full objects, not just IDs
  Provider   string                    `json:"provider"`   // empty = use configured default
  Force      bool                      `json:"force"`
}

type BuildDetectionFinding struct {
  Level   string `json:"level"`   // "info" | "warn" | "error"
  Message string `json:"message"`
}

type BuildDetectionAlert struct {
  Name         string `json:"name"`
  Description  string `json:"description"`
  TechniqueId  string `json:"techniqueId"`
  Logic        string `json:"logic"`
  Window       string `json:"window"`       // "5m" | "30m" | "1h" | "6h" | "24h"
  WindowReason string `json:"windowReason"`
  Source       string `json:"source"`       // "EDR" | "CloudTrail" | "IdP" | "Email" | "Network" | "WAF"
  Severity     string `json:"severity"`     // "critical" | "high" | "medium" | "low"
}

type BuildDetectionCorrelation struct {
  Name     string `json:"name"`
  Logic    string `json:"logic"`
  Window   string `json:"window"`   // "1h" | "24h" | "72h"
  Severity string `json:"severity"` // "critical" | "high"
}

type BuildDetectionResponse struct {
  Validation struct {
    Verdict  string                  `json:"verdict"` // "ok" | "warnings" | "invalid"
    Findings []BuildDetectionFinding `json:"findings"`
  } `json:"validation"`
  Alerts      []BuildDetectionAlert     `json:"alerts"`
  Correlation BuildDetectionCorrelation `json:"correlation"`
  Cached      bool                      `json:"cached"`
  Provider    string                    `json:"provider"`
}
```

### 7.3 Handler: `HandleBuildDetection`

Pattern follows `HandleCorrelations` (line 1142):

1. Decode `BuildDetectionRequest`
2. Validate: `len(req.TechniqueIds) >= 2`, client non-empty
3. Cache check: SHA256 of `(client, sorted techniqueIds joined)` → return cached if exists and `!req.Force`
4. Resolve provider (same pattern as `HandleSuggestions` lines 1169-1205)
5. Build prompt via `buildDetectionPrompt(techniqueIds)` — Go port of `buildPrompt()` from handoff
6. Call LLM, parse JSON response into `BuildDetectionResponse`
7. On parse failure: call `mockBuildDetection(techniqueIds)` — Go port of `mockGenerate()` from handoff
8. Cache result, return JSON

### 7.4 LLM prompt (`buildDetectionPrompt`)

Port of `buildPrompt()` from `builder-base.jsx`. The prompt instructs the model to:
1. Validate the chain represents a coherent kill-chain (ordering by tactic stage)
2. Propose 3–4 flow alerts with: name, description, techniqueId, plain-English logic, **realistic time window + reason**, source, severity
3. Return one correlation rule with longest plausible attacker dwell window
4. List validation findings

Output: strict JSON matching `BuildDetectionResponse` shape. No prose, no code fences.

### 7.5 Mock fallback (`mockBuildDetection`)

Go port of `mockGenerate()` from `builder-base.jsx`:
- `WINDOW_BY_TACTIC` map: tactic ID → `{window, reason}` (deterministic)
- `SEV_BY_TACTIC` map: tactic ID → severity
- Generates validation findings based on tactic presence rules (e.g. warn if no Initial Access)
- Dwell window: `len >= 4` → 72h, `len >= 3` → 24h, else 1h

---

## 8. CSS Tokens and Styling

**File:** `frontend/src/App.css` — add detection builder styles in a new `/* === Detection Builder === */` section.

Design tokens (from handoff, some already exist in App.css):
```css
/* Surfaces — map to existing cx vars where possible */
--cx-bg-0: #0a0d14;
--cx-bg-1: #10141d;
--cx-bg-2: #161b27;
--cx-bg-3: #1d2333;
--cx-bg-hover: #1f2536;
--cx-border: #232a3c;        /* check if matches existing */
--cx-border-strong: #2c3447;
--cx-fg: #e8ebf3;
--cx-fg-2: #a3acc0;
--cx-fg-3: #6b7388;
/* Accent — builder uses indigo to distinguish from graph's amber */
--cx-db-accent: #6366f1;
--cx-db-accent-2: #818cf8;
/* Severity */
--cx-crit: #dc2626;
--cx-high: #f97316;
--cx-warn: #eab308;
--cx-low: #94a3b8;
```

New class names use `db-` prefix (detection builder) to avoid collision with existing `cx-` classes.

Fonts: existing `--cx-sans` (Inter) and `--cx-mono` (JetBrains Mono) — no new fonts needed.

---

## 9. Animations

All follow existing CXAlert patterns:
- **View entry:** `FADE_UP` + `FADE_UP_TRANSITION` (already defined in `App.tsx`)
- **Generated panel entry:** 220ms `cubic-bezier(0.2,0.8,0.2,1)` opacity + 6px `translateY` (class `gen-in`)
- **Tech card hover:** 100ms all-transition, `translateY(-1px)`
- **Skeleton shimmer:** 1.4s ease infinite (linear-gradient `background-position`)
- **Generate button spinner:** 0.8s linear infinite rotation, 12px ring
- **Generate button arrow:** `translateX(2px)` on hover, 150ms

---

## 10. Integration Summary

| Area | Change | Pattern to follow |
|---|---|---|
| `App.tsx` line 15 | Add `'builder'` to `View` type | Existing type |
| `App.tsx` goBack | `'builder'` → `'summary'` | Existing `goBack()` |
| `App.tsx` breadcrumb | `"Build detections"` | Lines 108-117 |
| `App.tsx` render | `<DetectionBuilder>` in AnimatePresence | Lines 176-207 |
| `IntegrationSummary.tsx` | "Build detections" button → `setView('builder')` | Existing MITRE button |
| `frontend/src/components/DetectionBuilder.tsx` | New component | `MITREHeatmap.tsx` structure |
| `frontend/src/data/mitre-catalog.ts` | Static MITRE catalog | Port from handoff JS |
| `frontend/src/types/index.ts` | Add 4 new interfaces | Existing types |
| `frontend/src/services/api.ts` | Add `buildDetection()` | `fetchCorrelations()` |
| `backend/internal/models/models.go` | Add 5 new structs | `CorrelationsRequest/Response` |
| `backend/internal/api/handlers.go` | Add `HandleBuildDetection`, register route | `HandleCorrelations` |
| `frontend/src/App.css` | Add `db-*` CSS classes | Existing `cx-*` classes |

---

## Out of Scope (v1)

- "Save to detections →" backend endpoint (button visible, wired to `console.log` for now)
- "Export YAML" endpoint (same)
- Reorder-by-drag within the basket (remove + re-add is sufficient)
- Responsive collapse below 768px (1100px breakpoint only per handoff)
