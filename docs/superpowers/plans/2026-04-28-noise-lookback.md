# Noise Lookback Window Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a configurable 7/14/30/90-day lookback window for behavioral noise detection, surfaced as pill toggles on the home screen and AlertInsights toolbar.

**Architecture:** `lookback_days` flows from UI state (persisted to localStorage) → `POST /api/analyze` body → `fetchEventCounts(days)` → `findNoiseAlerts`. Insights and Export endpoints keep 30 hardcoded. A new `NoisePills` component is shared between the home screen and AlertInsights toolbar.

**Tech Stack:** Go (backend), React + TypeScript (frontend), localStorage for persistence.

---

## File Map

| File | Role |
|------|------|
| `backend/internal/models/models.go:182-184` | Add `LookbackDays int` to `ClientAnalyzeRequest` |
| `backend/internal/api/handlers.go:997-1008` | Add `days int` param to `fetchEventCounts`; validate in `HandleAnalyze` |
| `backend/internal/api/lookback_test.go` | New test file for lookback validation logic |
| `frontend/src/services/api.ts:11-23` | Add `lookbackDays` param to `analyzeClient` |
| `frontend/src/components/NoisePills.tsx` | New reusable pill-group component |
| `frontend/src/App.tsx` | `lookbackDays` state + `updateLookback`; pass to `handleAnalyze`, render `NoisePills` on home screen, pass `onReanalyze` to `AlertInsights` |
| `frontend/src/components/AlertInsights.tsx` | Add `lookbackDays` + `onReanalyze` props; render `NoisePills` in toolbar |
| `frontend/src/App.css` | Styles for `.noise-pills` |

---

### Task 1: Backend — add `days` param to `fetchEventCounts` and validate in `HandleAnalyze`

**Files:**
- Modify: `backend/internal/models/models.go:182-184`
- Modify: `backend/internal/api/handlers.go:64-219`
- Create: `backend/internal/api/lookback_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/api/lookback_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestValidateLookbackDays_validValues(t *testing.T) {
	for _, days := range []int{7, 14, 30, 90} {
		got := validateLookbackDays(days)
		if got != days {
			t.Errorf("validateLookbackDays(%d) = %d, want %d", days, got, days)
		}
	}
}

func TestValidateLookbackDays_invalidDefaultsTo30(t *testing.T) {
	for _, days := range []int{0, 1, 15, 45, 100, -1, 365} {
		got := validateLookbackDays(days)
		if got != 30 {
			t.Errorf("validateLookbackDays(%d) = %d, want 30", days, got)
		}
	}
}

func TestHandleAnalyze_rejectsGet(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/analyze", nil)
	w := httptest.NewRecorder()
	h.HandleAnalyze(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleAnalyze_unknownClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"ghost","lookback_days":30}`)
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleAnalyze(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/api/... -run "TestValidateLookbackDays|TestHandleAnalyze" -v 2>&1
```

Expected: `FAIL — validateLookbackDays undefined`

- [ ] **Step 3: Update `ClientAnalyzeRequest` in models.go**

In `backend/internal/models/models.go`, change lines 182-184:

```go
type ClientAnalyzeRequest struct {
	Client       string `json:"client"`
	LookbackDays int    `json:"lookback_days"`
}
```

- [ ] **Step 4: Add `validateLookbackDays` and update `fetchEventCounts` in handlers.go**

Add this function anywhere in `backend/internal/api/handlers.go` (e.g. near `fetchEventCounts`):

```go
// validateLookbackDays returns days if it is one of the allowed windows {7,14,30,90},
// or 30 if the value is missing or not in the whitelist.
func validateLookbackDays(days int) int {
	switch days {
	case 7, 14, 30, 90:
		return days
	default:
		return 30
	}
}
```

Update `fetchEventCounts` signature (currently at line ~997) to accept `days int`:

```go
// fetchEventCounts fetches trigger counts for the given alert IDs over the past [days] days.
// Returns nil on any error so callers fall back to structural-only noise detection.
func fetchEventCounts(ctx context.Context, region, apiKey string, alertIDs []string, days int) map[string]int {
	client, err := coralogix.NewClient(region, apiKey)
	if err != nil {
		return nil
	}
	defer client.Close()
	counts, err := client.FetchAlertEventCounts(ctx, alertIDs, days)
	if err != nil {
		log.Printf("DEBUG [noise] event count fetch failed: %v", err)
		return nil
	}
	return counts
}
```

- [ ] **Step 5: Update all three `fetchEventCounts` call sites in handlers.go**

**`HandleAnalyze`** (line ~212): read and validate `lookback_days`, then pass it:

```go
lookback := validateLookbackDays(req.LookbackDays)

// Fetch trigger counts for behavioral noise detection.
alertIDs := make([]string, len(alerts))
for i, a := range alerts {
	alertIDs[i] = a.ID
}
eventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, alertIDs, lookback)
if eventCounts == nil {
	log.Printf("WARN [noise] event counts unavailable for client=%s — structural-only noise", req.Client)
}
```

**`HandleInsights`** (line ~427): keep hardcoded 30:
```go
insightsEventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, insightsAlertIDs, 30)
```

**`HandleExportNarrative`** (line ~551): keep hardcoded 30:
```go
exportEventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, exportAlertIDs, 30)
```

- [ ] **Step 6: Run tests — all must pass**

```bash
cd backend && go test ./internal/api/... -run "TestValidateLookbackDays|TestHandleAnalyze" -v 2>&1
```

Expected: all PASS.

- [ ] **Step 7: Full build check**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output (clean build).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/models/models.go backend/internal/api/handlers.go backend/internal/api/lookback_test.go
git commit -m "feat(noise): add configurable lookback_days to analyze endpoint"
```

---

### Task 2: Frontend API — thread `lookbackDays` through `analyzeClient`

**Files:**
- Modify: `frontend/src/services/api.ts:11-23`

- [ ] **Step 1: Update `analyzeClient` in `frontend/src/services/api.ts`**

Replace the existing `analyzeClient` function (lines 11-23):

```typescript
export async function analyzeClient(
  client: string,
  refresh = false,
  lookbackDays = 30,
): Promise<AnalyzeResponse> {
  const url = refresh
    ? `${API_BASE}/api/analyze?refresh=true`
    : `${API_BASE}/api/analyze`;
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, lookback_days: lookbackDays }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Analysis failed' }));
    throw new Error(err.error || 'Analysis failed');
  }
  return res.json();
}
```

- [ ] **Step 2: TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/services/api.ts
git commit -m "feat(noise): pass lookback_days in analyzeClient API call"
```

---

### Task 3: Frontend — `NoisePills` component + CSS

**Files:**
- Create: `frontend/src/components/NoisePills.tsx`
- Modify: `frontend/src/App.css`

- [ ] **Step 1: Create `frontend/src/components/NoisePills.tsx`**

```tsx
const WINDOWS = [7, 14, 30, 90] as const;
type Window = typeof WINDOWS[number];

interface Props {
  days: number;
  onChange: (days: number) => void;
  disabled?: boolean;
}

export default function NoisePills({ days, onChange, disabled = false }: Props) {
  return (
    <div className={`noise-pills${disabled ? ' noise-pills--disabled' : ''}`}>
      <span className="noise-pills__label">Noise window</span>
      <div className="noise-pills__group">
        {WINDOWS.map(w => (
          <button
            key={w}
            className={`noise-pills__pill${days === w ? ' noise-pills__pill--active' : ''}`}
            onClick={() => onChange(w)}
            disabled={disabled}
          >
            {w}d
          </button>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Add CSS to `frontend/src/App.css`**

Add at the end of the file:

```css
.noise-pills { display: flex; align-items: center; gap: 10px; }
.noise-pills__label { font-family: var(--font-mono); font-size: 0.65rem; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-sec); }
.noise-pills__group { display: flex; gap: 4px; }
.noise-pills__pill { font-family: var(--font-mono); font-size: 0.65rem; letter-spacing: 0.04em; padding: 4px 9px; border-radius: var(--radius-sm); border: 1px solid var(--border-bright); background: transparent; color: var(--text-sec); cursor: pointer; transition: background 0.12s, color 0.12s, border-color 0.12s; }
.noise-pills__pill:hover:not(:disabled) { background: var(--surface-2); color: var(--text); }
.noise-pills__pill--active { background: var(--accent); border-color: var(--accent); color: var(--bg); font-weight: 600; }
.noise-pills__pill--active:hover:not(:disabled) { background: var(--accent); }
.noise-pills--disabled { opacity: 0.5; pointer-events: none; }
```

- [ ] **Step 3: TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/NoisePills.tsx frontend/src/App.css
git commit -m "feat(noise): add NoisePills reusable pill-group component"
```

---

### Task 4: Wire `lookbackDays` state into App.tsx + home screen

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Add `lookbackDays` state and `updateLookback` helper**

In `frontend/src/App.tsx`, add the import at the top:

```tsx
import NoisePills from './components/NoisePills';
```

After the existing `useState` declarations (after line 30), add:

```tsx
const [lookbackDays, setLookbackDays] = useState<number>(() => {
  const stored = localStorage.getItem('noise_lookback_days');
  const parsed = stored ? Number(stored) : 30;
  return [7, 14, 30, 90].includes(parsed) ? parsed : 30;
});

const updateLookback = (days: number) => {
  setLookbackDays(days);
  localStorage.setItem('noise_lookback_days', String(days));
};
```

- [ ] **Step 2: Update `handleAnalyze` to accept and pass `days`**

Replace the existing `handleAnalyze` function (lines 32-50):

```tsx
const handleAnalyze = async (client: string, refresh = false, days = lookbackDays) => {
  setLoading(true);
  setError('');
  setInsightsReport(null);
  setInsightsError(false);
  try {
    const result = await analyzeClient(client, refresh, days);
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
```

- [ ] **Step 3: Add `NoisePills` to the home screen and new props to `AlertInsights`**

In the JSX, in the `view === 'form'` branch (around line 122-125), update `ClientSelector` to include pills above it. Replace:

```tsx
<motion.div key="form" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
  <ClientSelector onAnalyze={handleAnalyze} loading={loading} />
</motion.div>
```

With:

```tsx
<motion.div key="form" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
  <ClientSelector
    onAnalyze={handleAnalyze}
    loading={loading}
    lookbackDays={lookbackDays}
    onLookbackChange={updateLookback}
  />
</motion.div>
```

In the `view === 'insights'` branch (around line 148-155), add the two new props to `AlertInsights`:

```tsx
<AlertInsights
  data={data.alert_insights}
  report={insightsReport}
  insightsError={insightsError}
  client={clientName}
  mitreCoverage={data.mitre_coverage}
  totalAlerts={data.stats.total_alerts}
  lookbackDays={lookbackDays}
  onReanalyze={(days) => { updateLookback(days); handleAnalyze(clientName, false, days); }}
/>
```

- [ ] **Step 4: TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: errors about `ClientSelector` missing props and `AlertInsights` missing props — these are fixed in Tasks 5 and 6.

- [ ] **Step 5: Commit (after Tasks 5 and 6 complete and tsc passes)**

```bash
git add frontend/src/App.tsx
git commit -m "feat(noise): wire lookbackDays state and re-analyze handler in App"
```

---

### Task 5: Add `NoisePills` to `ClientSelector`

**Files:**
- Modify: `frontend/src/components/ClientSelector.tsx`

- [ ] **Step 1: Add props and render `NoisePills`**

In `frontend/src/components/ClientSelector.tsx`, update the `Props` interface (currently at line 17-20):

```tsx
import NoisePills from './NoisePills';

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
  lookbackDays: number;
  onLookbackChange: (days: number) => void;
}
```

Update the function signature (line 22):

```tsx
export default function ClientSelector({ onAnalyze, loading, lookbackDays, onLookbackChange }: Props) {
```

Find the heading section — wherever "Select a client" title is rendered — and add `NoisePills` below it. Search for the title text in ClientSelector.tsx and add after the subtitle/description element:

```tsx
<NoisePills days={lookbackDays} onChange={onLookbackChange} disabled={loading} />
```

- [ ] **Step 2: TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: only errors from `AlertInsights` missing props (fixed in Task 6).

- [ ] **Step 3: Commit (after Task 6 completes and tsc passes cleanly)**

```bash
git add frontend/src/components/ClientSelector.tsx
git commit -m "feat(noise): add NoisePills to ClientSelector home screen"
```

---

### Task 6: Add `NoisePills` to `AlertInsights` toolbar

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`

- [ ] **Step 1: Add new props to the `Props` interface**

In `frontend/src/components/AlertInsights.tsx`, update the `Props` interface (lines 6-13):

```tsx
interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
  client: string;
  mitreCoverage: MITRECoverageResult;
  totalAlerts: number;
  lookbackDays: number;
  onReanalyze: (days: number) => void;
}
```

Update the function signature (line 33):

```tsx
export default function AlertInsights({ data, report, insightsError = false, client, mitreCoverage, totalAlerts, lookbackDays, onReanalyze }: Props) {
```

Add the import at the top of the file (line 1 area):

```tsx
import NoisePills from './NoisePills';
```

- [ ] **Step 2: Render `NoisePills` in the toolbar**

In the toolbar area — the `div` that contains the Regenerate and Export buttons (around line 255-295) — add `NoisePills` to the left of the existing buttons:

```tsx
<div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
  <NoisePills days={lookbackDays} onChange={onReanalyze} disabled={isRegenerating || isExporting} />
  <button
    className="insights-regenerate-btn"
    onClick={handleRegenerate}
    disabled={isRegenerating || !client}
    title="Regenerate insights"
  >
    {isRegenerating ? '…' : '↺'}
  </button>
  {/* ... Export dropdown unchanged ... */}
</div>
```

- [ ] **Step 3: TypeScript check — must be clean**

```bash
cd frontend && npx tsc --noEmit 2>&1
```

Expected: no errors.

- [ ] **Step 4: Commit all frontend tasks together**

```bash
git add frontend/src/components/AlertInsights.tsx frontend/src/components/ClientSelector.tsx frontend/src/App.tsx frontend/src/components/NoisePills.tsx frontend/src/App.css frontend/src/services/api.ts
git commit -m "feat(noise): configurable lookback window pills in home screen and AlertInsights toolbar"
```

---

## Self-Review

**Spec coverage check:**
- ✅ Pill group 7/14/30/90, default 30 → Task 3 (`NoisePills`)
- ✅ Home screen pill group → Task 5 (`ClientSelector`)
- ✅ AlertInsights toolbar pill group → Task 6
- ✅ Changing pills in results re-runs analysis → Task 4 (`onReanalyze`)
- ✅ localStorage persistence → Task 4 (`updateLookback`)
- ✅ `lookback_days` in POST body → Task 2 (`analyzeClient`)
- ✅ Backend validation whitelist → Task 1 (`validateLookbackDays`)
- ✅ `fetchEventCounts` days param → Task 1
- ✅ Insights/Export stay at 30 → Task 1 (hardcoded at other two call sites)
- ✅ Disabled state during re-analyze → Tasks 5 & 6 (`disabled={isRegenerating || isExporting}`)

**Placeholder scan:** No TBDs, TODOs, or vague steps found.

**Type consistency:** `lookbackDays: number` used consistently across all tasks. `onReanalyze: (days: number) => void` matches definition in Task 4 and usage in Task 6.
