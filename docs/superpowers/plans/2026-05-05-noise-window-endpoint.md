# Noise Window Endpoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dedicated `POST /api/noise` endpoint so changing the time window in the Noise tab reruns only noise detection (~2s) instead of the full analysis pipeline (15–30s).

**Architecture:** Three layers of change — (1) a new exported `AnalyzeNoise` function in the similarity engine that runs only the noise step, (2) a new `HandleNoise` handler that loads alerts store-first, fetches event counts, and calls `AnalyzeNoise`, and (3) frontend wires `handleReanalyze` to call this endpoint and patch only `noise_alerts` in state, leaving all other tabs untouched.

**Tech Stack:** Go 1.21, React 18, TypeScript 5, Vite; existing `fetchEventCounts`, `validateLookbackDays`, `coralogix.ExtractFeatures`, `similarity.AnalyzeNoise` helpers follow the same patterns as `HandleInsights`.

---

## File Map

| File | Change |
|---|---|
| `backend/internal/similarity/engine.go` | Add exported `AnalyzeNoise` function |
| `backend/internal/similarity/engine_test.go` | Add 4 `TestAnalyzeNoise_*` tests |
| `backend/internal/models/models.go` | Add `NoiseRequest` and `NoiseResponse` types |
| `backend/internal/api/handlers.go` | Add `HandleNoise` handler method |
| `backend/cmd/server/main.go` | Register `/api/noise` route |
| `frontend/src/types/index.ts` | Add `NoiseResponse` interface |
| `frontend/src/services/api.ts` | Add `fetchNoise` function + import |
| `frontend/src/App.tsx` | Add `noiseLoading` state + `handleReanalyze`; pass to `AlertInsights` |
| `frontend/src/components/AlertInsights.tsx` | Add `noiseLoading?: boolean` prop; pass to `NoisePills` |

---

### Task 1: `AnalyzeNoise` exported function + tests (TDD)

**Context:** `Analyze` in `engine.go` runs the full O(n²) pipeline (pairwise scoring, families, duplicates, merges, uniques, noise). `AnalyzeNoise` runs only the noise step — it is a thin wrapper over `findNoiseAlerts` using the same internal helpers. Tests live in `engine_test.go` which already imports `models` and has helpers `makeAlert` and `sparseVector`.

**Files:**
- Modify: `backend/internal/similarity/engine_test.go` (append after last `TestFindNoiseAlerts_*` test)
- Modify: `backend/internal/similarity/engine.go` (append after line 239, after the closing `}` of `Analyze`)

- [ ] **Step 1: Write the four failing tests**

Append after the last `TestFindNoiseAlerts_*` test in `backend/internal/similarity/engine_test.go`:

```go
func TestAnalyzeNoise_nilAlerts_returnsNil(t *testing.T) {
	result := AnalyzeNoise(nil, nil, 0)
	if result != nil {
		t.Errorf("expected nil for nil alerts, got %v", result)
	}
}

func TestAnalyzeNoise_emptyAlerts_returnsNil(t *testing.T) {
	result := AnalyzeNoise([]*models.AlertDef{}, nil, 0)
	if result != nil {
		t.Errorf("expected nil for empty alerts, got %v", result)
	}
}

func TestAnalyzeNoise_unscopedAlert_returnsStructuralNoise(t *testing.T) {
	alert := makeAlert("u-1", "logs_threshold", false, true, nil, "", "")
	result := AnalyzeNoise([]*models.AlertDef{alert}, map[string]int{}, 0)
	if len(result) != 1 {
		t.Fatalf("expected 1 noise alert for unscoped alert, got %d: %v", len(result), result)
	}
	if result[0].NoiseType != "structural" {
		t.Errorf("expected structural, got %q", result[0].NoiseType)
	}
}

func TestAnalyzeNoise_scopedAlert_notFlagged(t *testing.T) {
	alert := makeAlert("s-1", "logs_threshold", false, true, nil, "my-app", "auth")
	result := AnalyzeNoise([]*models.AlertDef{alert}, map[string]int{}, 0)
	if len(result) != 0 {
		t.Errorf("scoped alert should not be noise, got %v", result)
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run "TestAnalyzeNoise" -v
```

Expected: compile error — `undefined: AnalyzeNoise`.

- [ ] **Step 3: Add `AnalyzeNoise` to engine.go**

In `backend/internal/similarity/engine.go`, insert after the closing `}` of `Analyze` (after line 239):

```go
// AnalyzeNoise runs only the noise detection step of the similarity pipeline.
// It skips pairwise scoring, family grouping, duplicate detection, and merge
// suggestions — making it suitable for re-running noise with a different lookback
// window without re-running the full O(n²) analysis.
func AnalyzeNoise(
	alerts []*models.AlertDef,
	eventCounts map[string]int,
	integrationCount int,
) []models.NoiseAlert {
	if len(alerts) == 0 {
		return nil
	}
	vectors := buildFeatureVectors(alerts)
	idf := buildIDF(vectors)
	threshold := computeQueryIDFThreshold(vectors, idf)
	return findNoiseAlerts(vectors, alerts, eventCounts, integrationCount, idf, threshold)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -run "TestAnalyzeNoise" -v
```

Expected: 4 tests PASS. Sample output:
```
--- PASS: TestAnalyzeNoise_nilAlerts_returnsNil
--- PASS: TestAnalyzeNoise_emptyAlerts_returnsNil
--- PASS: TestAnalyzeNoise_unscopedAlert_returnsStructuralNoise
--- PASS: TestAnalyzeNoise_scopedAlert_notFlagged
ok  	coralogix-alert-analyzer/internal/similarity
```

- [ ] **Step 5: Run full similarity suite to check for regressions**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/... -v 2>&1 | tail -10
```

Expected: all tests pass, `ok coralogix-alert-analyzer/internal/similarity`.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "$(cat <<'EOF'
feat(noise): add AnalyzeNoise — noise-only step, skips O(n²) pipeline

Exported wrapper over findNoiseAlerts that skips pairwise scoring,
family grouping, duplicates, and merges. Used by the upcoming
POST /api/noise endpoint to re-run noise in ~2s on window change.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Backend — models, handler, route

**Context:** `HandleInsights` (line 391 in `handlers.go`) is the closest existing handler to follow — it also loads alerts store-first, calls `coralogix.ExtractFeatures(alerts, nil)`, then does analysis. `NoiseRequest`/`NoiseResponse` go in `models.go` alongside the existing `NoiseAlert` type (line 120). The route is registered in `main.go` alongside `/api/insights` (line 110). `validateLookbackDays` and `fetchEventCounts` are package-level helpers already in `handlers.go`.

**Files:**
- Modify: `backend/internal/models/models.go` (after the `SimilarityResult` struct, ~line 138)
- Modify: `backend/internal/api/handlers.go` (new method after `HandleInsights`)
- Modify: `backend/cmd/server/main.go` (after line 110)

- [ ] **Step 1: Add request/response models to models.go**

In `backend/internal/models/models.go`, add after the closing `}` of `SimilarityResult` (after line ~138):

```go
// NoiseRequest is the request body for POST /api/noise.
type NoiseRequest struct {
	Client       string `json:"client"`
	LookbackDays int    `json:"lookback_days"`
}

// NoiseResponse is the response for POST /api/noise.
type NoiseResponse struct {
	NoiseAlerts  []NoiseAlert `json:"noise_alerts"`
	LookbackDays int          `json:"lookback_days"`
}
```

- [ ] **Step 2: Build to confirm no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 3: Add `HandleNoise` to handlers.go**

In `backend/internal/api/handlers.go`, append a new method after the closing `}` of `HandleInsights`. The method follows the exact same store-first alert loading pattern:

```go
// HandleNoise re-runs only the noise detection step for a different lookback window.
// POST /api/noise { "client": "X", "lookback_days": 7 }
// Response: { "noise_alerts": [...], "lookback_days": 7 }
func (h *Handler) HandleNoise(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.NoiseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}

	clientCfg, ok := h.config.Clients[req.Client]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	lookback := validateLookbackDays(req.LookbackDays)
	ctx := r.Context()

	// Load alerts — store-first, same strategy as HandleInsights.
	var alerts []*models.AlertDef
	if h.alertStore != nil {
		stored, err := h.alertStore.LoadAlerts(ctx, req.Client)
		if err == nil && len(stored) > 0 {
			alerts = stored
		}
	}
	if len(alerts) == 0 {
		var err error
		alerts, err = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch alerts: %v", err))
			return
		}
	}

	alertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		alertIDs[i] = a.ID
	}
	eventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, alertIDs, lookback)

	coralogix.ExtractFeatures(alerts, nil)

	noiseAlerts := similarity.AnalyzeNoise(alerts, eventCounts, 0)
	if noiseAlerts == nil {
		noiseAlerts = []models.NoiseAlert{}
	}

	writeJSON(w, http.StatusOK, models.NoiseResponse{
		NoiseAlerts:  noiseAlerts,
		LookbackDays: lookback,
	})
}
```

- [ ] **Step 4: Register the route in main.go**

In `backend/cmd/server/main.go`, add after line 110 (`mux.HandleFunc("/api/insights", handler.HandleInsights)`):

```go
mux.HandleFunc("/api/noise", handler.HandleNoise)
```

- [ ] **Step 5: Build the full backend**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 6: Run the full backend test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... 2>&1 | tail -10
```

Expected: all packages `ok`, no failures.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/models/models.go backend/internal/api/handlers.go backend/cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(noise): add POST /api/noise endpoint — noise-only reanalysis

New HandleNoise handler loads alerts store-first, fetches event counts
for the requested lookback window, then calls similarity.AnalyzeNoise.
Returns {noise_alerts, lookback_days} without rerunning pairwise scoring,
families, duplicates, or MITRE coverage. Response time ~2s vs 15-30s.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Frontend — wire `handleReanalyze` to new endpoint

**Context:**
- `frontend/src/types/index.ts`: add `NoiseResponse` interface alongside `SuggestionsResponse` (line 173).
- `frontend/src/services/api.ts`: add `fetchNoise`; must also add `NoiseResponse` to the import from `../types` and `NoiseAlert` to the import (line 1).
- `frontend/src/App.tsx`: currently `onReanalyze` (line 168) is an inline lambda calling `handleAnalyze` (full pipeline). Replace with a named `handleReanalyze` that calls `fetchNoise` and patches only `noise_alerts` in state. Add `noiseLoading` state. Pass `noiseLoading` to `AlertInsights`.
- `frontend/src/components/AlertInsights.tsx`: add `noiseLoading?: boolean` to `Props` (line 7); change the `NoisePills` `disabled` prop from `disabled={isRegenerating}` to `disabled={isRegenerating || (noiseLoading ?? false)}`.

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/AlertInsights.tsx`

- [ ] **Step 1: Add `NoiseResponse` to types/index.ts**

In `frontend/src/types/index.ts`, append after the closing `}` of `SuggestionsResponse` (after line ~176):

```ts
export interface NoiseResponse {
  noise_alerts: NoiseAlert[];
  lookback_days: number;
}
```

- [ ] **Step 2: Add `fetchNoise` to services/api.ts**

In `frontend/src/services/api.ts`, update line 1 to import `NoiseAlert` and `NoiseResponse`:

```ts
import type { AnalyzeResponse, ClientInfo, ExportNarrativeReport, InsightsReport, NoiseAlert, NoiseResponse, SuggestionsResponse } from '../types';
```

Then append `fetchNoise` after `fetchExportNarrative`:

```ts
export async function fetchNoise(client: string, lookbackDays: number): Promise<NoiseAlert[]> {
  const res = await fetch(`${API_BASE}/api/noise`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, lookback_days: lookbackDays }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Noise fetch failed' }));
    throw new Error(err.error || 'Noise fetch failed');
  }
  const data: NoiseResponse = await res.json();
  return data.noise_alerts;
}
```

- [ ] **Step 3: Update App.tsx**

In `frontend/src/App.tsx`, update the import on line 8 to include `fetchNoise`:

```ts
import { analyzeClient, fetchInsights, fetchNoise } from './services/api';
```

Add `noiseLoading` state after `insightsError` state (after line 32):

```ts
const [noiseLoading, setNoiseLoading] = useState(false);
```

Add `handleReanalyze` after the `updateLookback` function (after line 42):

```ts
const handleReanalyze = async (days: number) => {
  updateLookback(days);
  if (!data) return;
  setNoiseLoading(true);
  try {
    const noiseAlerts = await fetchNoise(clientName, days);
    setData(prev => prev
      ? { ...prev, alert_insights: { ...prev.alert_insights!, noise_alerts: noiseAlerts } }
      : prev
    );
  } catch (e) {
    console.warn('[noise reanalyze]', e);
  } finally {
    setNoiseLoading(false);
  }
};
```

Replace the `AlertInsights` call (line 160–169). Change `onReanalyze` from the inline lambda to `handleReanalyze`, and add `noiseLoading`:

```tsx
<AlertInsights
  data={data.alert_insights}
  report={insightsReport}
  insightsError={insightsError}
  client={clientName}
  mitreCoverage={data.mitre_coverage}
  totalAlerts={data.stats.total_alerts}
  lookbackDays={lookbackDays}
  onReanalyze={handleReanalyze}
  noiseLoading={noiseLoading}
/>
```

- [ ] **Step 4: Update AlertInsights.tsx Props and NoisePills**

In `frontend/src/components/AlertInsights.tsx`, update the `Props` interface (line 7) to add `noiseLoading`:

```ts
interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
  client: string;
  mitreCoverage: MITRECoverageResult;
  totalAlerts: number;
  lookbackDays: number;
  onReanalyze: (days: number) => void;
  noiseLoading?: boolean;
}
```

Update the destructured props in the function signature (line 36) to include `noiseLoading`:

```ts
export default function AlertInsights({ data, report, insightsError = false, client, mitreCoverage, totalAlerts, lookbackDays, onReanalyze, noiseLoading }: Props) {
```

Update the `NoisePills` call (line 567) to disable during noise loading:

```tsx
<NoisePills days={lookbackDays} onChange={onReanalyze} disabled={isRegenerating || (noiseLoading ?? false)} />
```

- [ ] **Step 5: TypeScript build check**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: exits 0, no errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude && git add frontend/src/types/index.ts frontend/src/services/api.ts frontend/src/App.tsx frontend/src/components/AlertInsights.tsx
git commit -m "$(cat <<'EOF'
feat(noise): wire frontend to POST /api/noise on window change

handleReanalyze calls the new /api/noise endpoint and patches only
noise_alerts in state — duplicates, families, gaps, and MITRE tabs
are unaffected. noiseLoading disables NoisePills during the request
to prevent double-fires.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Verification

After all tasks:

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... -count=1 2>&1 | tail -10
```

Expected:
```
ok  	coralogix-alert-analyzer/internal/api
ok  	coralogix-alert-analyzer/internal/similarity
ok  	coralogix-alert-analyzer/internal/coralogix
...
```

Manual test: start the backend (`go run ./cmd/server`), load any client in the browser, navigate to the Noise tab, then click a different time window (e.g. 7d → 90d). The page should update in ~2s with only the Noise tab refreshed. The server log should show:
```
INFO [noise] event counts: requested=N matched=M
```

The Duplicates, Families, Gaps, and MITRE tabs should show unchanged data from the original analysis.
