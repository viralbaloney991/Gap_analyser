# Noise Lookback Window Design

**Date:** 2026-04-28
**Status:** Approved

## Summary

Add a configurable lookback window (7 / 14 / 30 / 90 days) for behavioral noise detection. The selection is surfaced as a pill-group toggle on the home (client picker) screen and in the AlertInsights toolbar. Changing either triggers a fresh `/api/analyze` call with the selected window. Insights and Export endpoints remain hardcoded at 30 days.

---

## Scope

**In scope:**
- `POST /api/analyze` — accepts `lookback_days` (7 / 14 / 30 / 90); defaults to 30 if absent or invalid
- Noise tab in UI — reflects the chosen window
- Pill group on home screen — sets the default before client selection
- Pill group in AlertInsights toolbar — re-runs analysis with a new window

**Out of scope:**
- `/api/insights` — always uses 30 days
- `/api/export/narrative` — always uses 30 days
- Structural noise detection — unaffected (event counts only drive behavioral noise)
- Cache key — not changed; different day windows produce independent cache misses (acceptable)

---

## UX

### Home screen
A pill group labeled **"Noise window"** sits below the "Select a client" heading, above the client list:

```
Noise window:  [ 7d ]  [ 14d ]  [ 30d ✓ ]  [ 90d ]
```

- Default: 30d
- Persisted to `localStorage` key `noise_lookback_days`
- Selection carries into the analysis when a client is clicked

### AlertInsights toolbar
The same pill group appears in the right side of the panel header, next to the Regenerate and Export buttons:

```
[ 7d ] [ 14d ] [ 30d ✓ ] [ 90d ]   ↺   Export ▾
```

- Changing a pill immediately re-runs analysis (calls `onReanalyze(days)` prop)
- While re-running, pills are disabled and the existing results stay visible until the new ones arrive
- The selected pill updates `localStorage` so the home screen stays in sync

---

## Data Flow

```
User picks pill (days)
  → lookbackDays state in App.tsx (synced to localStorage)
  → handleAnalyze(client, refresh=false, days)
  → analyzeClient(client, refresh, days)  [api.ts]
  → POST /api/analyze { client, lookback_days: days }
  → HandleAnalyze reads lookback_days (validates whitelist; default 30)
  → fetchEventCounts(ctx, region, apiKey, alertIDs, days)
  → similarity.Analyze(alerts, eventCounts, ...)
  → NoiseAlerts reflect chosen window
  → SimilarityResult returned to frontend → Noise tab updated
```

---

## Backend

### `backend/internal/api/handlers.go`

**Request body for `HandleAnalyze`:**
```go
var req struct {
    Client       string `json:"client"`
    LookbackDays int    `json:"lookback_days"`
}
```

Validation — `lookbackDays` must be one of `{7, 14, 30, 90}`; any other value (including 0) defaults to 30:
```go
validWindows := map[int]bool{7: true, 14: true, 30: true, 90: true}
if !validWindows[req.LookbackDays] {
    req.LookbackDays = 30
}
```

**`fetchEventCounts` signature change:**
```go
func fetchEventCounts(ctx context.Context, region, apiKey string, alertIDs []string, days int) map[string]int
```

All three call sites updated:
- `HandleAnalyze` — passes `req.LookbackDays`
- `HandleInsights` — passes hardcoded `30`
- `HandleExportNarrative` — passes hardcoded `30`

---

## Frontend

### `frontend/src/services/api.ts`

```typescript
export async function analyzeClient(
  client: string,
  refresh = false,
  lookbackDays = 30,
): Promise<AnalyzeResponse>
```

Request body: `{ client, lookback_days: lookbackDays }` (plus `?refresh=true` query param when refresh is true).

### `frontend/src/App.tsx`

New state:
```typescript
const [lookbackDays, setLookbackDays] = useState<number>(() => {
  const stored = localStorage.getItem('noise_lookback_days');
  return stored ? Number(stored) : 30;
});
```

`setLookbackDays` persists to localStorage:
```typescript
const updateLookback = (days: number) => {
  setLookbackDays(days);
  localStorage.setItem('noise_lookback_days', String(days));
};
```

`handleAnalyze` updated to accept and pass `days`:
```typescript
const handleAnalyze = async (client: string, refresh = false, days = lookbackDays) => { ... }
```

`AlertInsights` receives two new props:
- `lookbackDays: number`
- `onReanalyze: (days: number) => void`

Home screen renders `<NoisePills days={lookbackDays} onChange={updateLookback} />` above client list.

### `frontend/src/components/NoisePills.tsx` (new file)

```typescript
const WINDOWS = [7, 14, 30, 90] as const;

interface Props {
  days: number;
  onChange: (days: number) => void;
  disabled?: boolean;
}

export default function NoisePills({ days, onChange, disabled }: Props)
```

Renders four pill buttons. Active pill uses `var(--accent)` background + `var(--bg)` text. Inactive uses `var(--surface-2)` background + `var(--text-sec)` text + `var(--border-bright)` border. Disabled state: `opacity: 0.5; pointer-events: none`.

### `frontend/src/components/AlertInsights.tsx`

New props added:
```typescript
lookbackDays: number;
onReanalyze: (days: number) => void;
```

In the toolbar (next to Regenerate/Export):
```tsx
<NoisePills
  days={lookbackDays}
  onChange={onReanalyze}
  disabled={isRegenerating}
/>
```

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| Invalid `lookback_days` from client | Backend silently defaults to 30 |
| Re-analyze fails | Existing results stay; error toast shown (existing pattern) |
| localStorage unavailable | Falls back to 30 (in-memory only) |

---

## Tests

| Location | What to test |
|----------|-------------|
| `handlers_test.go` | `HandleAnalyze` with valid `lookback_days` (7/14/30/90) passes correct value to event count fetch; invalid/missing value defaults to 30 |
| `NoisePills` (manual) | Active pill highlighted; onChange fires; disabled state blocks clicks |
