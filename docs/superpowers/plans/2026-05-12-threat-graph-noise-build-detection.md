# Threat Graph Interactivity, Noise Signal & Build Detection Link — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix ThreatGraph trackpad+hover behaviour, replace fake noise percentages with real backend data, and add a "Build Detection →" button on Advanced Use Cases gap items that pre-populates the DetectionBuilder via LLM tactic mapping.

**Architecture:** Part A (Tasks 1–3) is pure frontend, all in `ThreatGraph.tsx`. Part B (Tasks 4–10) adds one backend LLM endpoint (`/api/map-tactics`) and wires it through `services/api.ts` → `AlertInsights.tsx` → `App.tsx` → `DetectionBuilder.tsx`. Parts A and B are independent and can be executed in parallel.

**Tech Stack:** React 18 + TypeScript (frontend), Go 1.22 (backend), existing `llm.Provider` interface, existing `writeJSON` / `writeError` helpers in `handlers.go`.

---

## Part A — ThreatGraph Fixes

---

### Task 1: Replace fake noise data with real backend signal

**Files:**
- Modify: `frontend/src/components/ThreatGraph.tsx`

The `AlertRule` interface has a fake `noisePct: number` field driven by `deterministicN()`. Replace it with `noiseType` and `triggerCount` sourced from `data.alert_insights.noise_alerts`.

- [ ] **Step 1: Update the `AlertRule` interface**

In `ThreatGraph.tsx` (around line 22), find the `AlertRule` interface. Replace `noisePct: number` with two new fields:

```typescript
interface AlertRule {
  id: string;
  name: string;
  source: string;
  severity: Severity;
  tids: string[];
  count: number;
  noiseType: 'behavioral' | 'structural' | 'both' | null;
  triggerCount: number;
  lastSeenHrs: number;
  trend: number;
  owner: string;
  mttd: number;
  mttr: number;
  assets: number;
  priorityCounts: Record<string, number>;
}
```

- [ ] **Step 2: Add `noiseLabel` helper (after the `fmtNum` helper, ~line 108)**

```typescript
function noiseLabel(noiseType: 'behavioral' | 'structural' | 'both', triggerCount: number): string {
  if (noiseType === 'behavioral') return `Fired ${triggerCount}× · Behavioral`;
  if (noiseType === 'structural') return 'Too broad · Structural';
  return `Fired ${triggerCount}× · Both`;
}
```

- [ ] **Step 3: Rewrite `buildAlertRules` noise logic (~lines 154–160)**

Replace the `noiseNames` block and the `noisePct` assignment. The full changed section inside `buildAlertRules`:

```typescript
// Build real noise map from backend data
const noiseMap = new Map<string, { triggerCount: number; noiseType: 'behavioral' | 'structural' | 'both' }>();
for (const n of data.alert_insights.noise_alerts ?? []) {
  noiseMap.set(n.name, {
    triggerCount: n.trigger_count ?? 0,
    noiseType: n.noise_type ?? 'structural',
  });
}

return data.integrations
  .filter(int => int.alert_count > 0)
  .map((int, i) => {
    const noise = noiseMap.get(int.name);
    return {
      id: `int-${i}`,
      name: int.name,
      source: deriveSource(int.application, int.subsystem),
      severity: dominantSeverity(int.priority_counts ?? {}),
      tids: [...(prefixToTids.get(int.name.toLowerCase()) ?? [])],
      count: int.alert_count,
      noiseType: noise?.noiseType ?? null,
      triggerCount: noise?.triggerCount ?? 0,
      lastSeenHrs: deterministicN(int.name, 1, 0, 72),
      trend: (deterministicN(int.name, 2, 0, 200) - 100) / 100,
      owner: ['SOC Tier 1', 'SOC Tier 2', 'IR Team', 'Cloud Sec'][deterministicN(int.name, 3, 0, 3)],
      mttd: deterministicN(int.name, 4, 2, 30),
      mttr: deterministicN(int.name, 5, 15, 240),
      assets: deterministicN(int.name, 6, 1, 18),
      priorityCounts: int.priority_counts ?? {},
    };
  });
```

Note: `deterministicN` is still used for `lastSeenHrs`, `trend`, `owner`, `mttd`, `mttr`, `assets` which are genuinely derived from the alert metadata — only `noisePct` was meaningless.

- [ ] **Step 4: Update the drill panel stat grid (~line 505)**

In `AlertDrillPanel`, replace the `'Noise share'` stat:

```typescript
<StatGrid items={[
  { label: 'Volume (30d)', value: fmtNum(a.count), sub: trendStr,
    accent: trendUp ? 'crit' : trendDown ? 'ok' : undefined },
  { label: 'Assets',      value: String(a.assets), sub: 'affected' },
  { label: 'Noise',       value: a.noiseType ? noiseLabel(a.noiseType, a.triggerCount) : 'None' },
  { label: 'MTTD',        value: `${a.mttd}m`, sub: 'detect' },
  { label: 'MTTR',        value: `${a.mttr}m`, sub: 'respond' },
]} />
```

- [ ] **Step 5: Verify TypeScript compiles with no errors**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors about `noisePct` (the old field).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/ThreatGraph.tsx
git commit -m "fix(threat-graph): replace fake noisePct with real noise_alerts signal"
```

---

### Task 2: Fix hover — debounce `setHovered(null)` to eliminate flicker

**Files:**
- Modify: `frontend/src/components/ThreatGraph.tsx`

When the cursor moves between adjacent nodes it briefly enters empty SVG space, causing `onMouseLeave → setHovered(null)` to fire before the next `onMouseEnter`. A short timer prevents the flash.

- [ ] **Step 1: Add a timer ref in the root `ThreatGraph` component**

In the main `ThreatGraph` component (around line 961 where `const [hovered, setHovered]` lives), add:

```typescript
const [hovered, setHovered]     = useState<string | null>(null);
const hoverLeaveTimer           = useRef<ReturnType<typeof setTimeout> | null>(null);
```

- [ ] **Step 2: Create a stable `setHoveredDebounced` callback**

Add this immediately after the new ref, before the JSX return:

```typescript
const setHoveredDebounced = useCallback((id: string | null) => {
  if (id !== null) {
    if (hoverLeaveTimer.current !== null) {
      clearTimeout(hoverLeaveTimer.current);
      hoverLeaveTimer.current = null;
    }
    setHovered(id);
  } else {
    hoverLeaveTimer.current = setTimeout(() => {
      hoverLeaveTimer.current = null;
      setHovered(null);
    }, 60);
  }
}, []);
```

- [ ] **Step 3: Cancel the timer on unmount**

Add a cleanup effect after `setHoveredDebounced`:

```typescript
useEffect(() => () => {
  if (hoverLeaveTimer.current !== null) clearTimeout(hoverLeaveTimer.current);
}, []);
```

- [ ] **Step 4: Pass `setHoveredDebounced` instead of `setHovered` to `GraphCanvas`**

Find the `<GraphCanvas ... setHovered={setHovered}` prop (~line 1012) and change it:

```typescript
setHovered={setHoveredDebounced}
```

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/ThreatGraph.tsx
git commit -m "fix(threat-graph): debounce hover clear to eliminate connected-node flicker"
```

---

### Task 3: Fix trackpad — distinguish two-finger scroll from pinch zoom

**Files:**
- Modify: `frontend/src/components/ThreatGraph.tsx`

The wheel handler in `useViewport` (~line 343) currently applies zoom to all wheel events. On trackpads, two-finger scroll fires `wheel` without `ctrlKey`; pinch fires `wheel` with `ctrlKey`. Split these.

- [ ] **Step 1: Rewrite the wheel handler in `useViewport`**

Replace the entire `handler` function inside the `useEffect` (starting at `const handler = (e: WheelEvent) =>`, ~line 343):

```typescript
const handler = (e: WheelEvent) => {
  e.preventDefault();
  const rect = el.getBoundingClientRect();

  if (e.ctrlKey) {
    // Pinch-to-zoom: ctrlKey is set by the OS for pinch gestures and Ctrl+scroll
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;
    setVp(v => {
      const factor = Math.exp(-e.deltaY * 0.0015);
      const k = Math.max(0.3, Math.min(2.4, v.k * factor));
      const rawX = mx - ((mx - v.x) * k) / v.k;
      const rawY = my - ((my - v.y) * k) / v.k;
      const L = layoutRef.current;
      if (!L) return { k, x: rawX, y: rawY };
      const { x, y } = clampTranslate(rawX, rawY, k, L, rect.width, rect.height);
      return { k, x, y };
    });
  } else {
    // Two-finger scroll → pan
    setVp(v => {
      const rawX = v.x - e.deltaX;
      const rawY = v.y - e.deltaY;
      const L = layoutRef.current;
      if (!L) return { ...v, x: rawX, y: rawY };
      const { x, y } = clampTranslate(rawX, rawY, v.k, L, rect.width, rect.height);
      return { ...v, x, y };
    });
  }
};
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ThreatGraph.tsx
git commit -m "fix(threat-graph): two-finger scroll pans; pinch (ctrlKey) zooms"
```

---

## Part B — Build Detection Link (Full Stack)

---

### Task 4: Backend — MapTactics request/response models + roundtrip test

**Files:**
- Modify: `backend/internal/models/models.go`
- Create: `backend/internal/models/models_map_tactics_test.go`

- [ ] **Step 1: Write the failing roundtrip test**

Create `backend/internal/models/models_map_tactics_test.go`:

```go
package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMapTacticsRequestRoundtrip(t *testing.T) {
	req := MapTacticsRequest{
		Client:    "acme",
		Prose:     "No detection for lateral movement via RDP",
		LogSource: "windows_security",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MapTacticsRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, req) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, req)
	}
}

func TestMapTacticsResponseRoundtrip(t *testing.T) {
	resp := MapTacticsResponse{
		TacticIDs:    []string{"TA0008", "TA0001"},
		TechniqueIDs: []string{"T1021.001", "T1566.001"},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MapTacticsResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, resp) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", got, resp)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/models/... -run TestMapTactics -v`  
Expected: FAIL — `MapTacticsRequest` and `MapTacticsResponse` undefined.

- [ ] **Step 3: Add the structs to `models.go`**

Append to `backend/internal/models/models.go` (after the `BuildDetectionResponse` type, at the end of the file):

```go
// ── Map Tactics ──────────────────────────────────────────────────────────

// MapTacticsRequest is the payload for POST /api/map-tactics.
type MapTacticsRequest struct {
	Client    string `json:"client"`
	Prose     string `json:"prose"`
	LogSource string `json:"log_source"`
}

// MapTacticsResponse is the payload returned by POST /api/map-tactics.
type MapTacticsResponse struct {
	TacticIDs    []string `json:"tactic_ids"`
	TechniqueIDs []string `json:"technique_ids"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/models/... -run TestMapTactics -v`  
Expected: PASS for both roundtrip tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/models.go backend/internal/models/models_map_tactics_test.go
git commit -m "feat(backend): add MapTacticsRequest/Response models with roundtrip tests"
```

---

### Task 5: Backend — `GenerateMapTactics` LLM function + unit test

**Files:**
- Create: `backend/internal/llm/map_tactics.go`
- Create: `backend/internal/llm/map_tactics_test.go`

- [ ] **Step 1: Write the failing unit test**

Create `backend/internal/llm/map_tactics_test.go`:

```go
package llm

import (
	"context"
	"testing"
)

func TestParseMapTacticsResponse(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantTactics []string
		wantTechs   []string
		wantErr     bool
	}{
		{
			name:        "clean JSON",
			raw:         `{"tactic_ids":["TA0008","TA0001"],"technique_ids":["T1021.001","T1566.001"]}`,
			wantTactics: []string{"TA0008", "TA0001"},
			wantTechs:   []string{"T1021.001", "T1566.001"},
		},
		{
			name:        "json wrapped in markdown",
			raw:         "```json\n{\"tactic_ids\":[\"TA0002\"],\"technique_ids\":[\"T1059.001\"]}\n```",
			wantTactics: []string{"TA0002"},
			wantTechs:   []string{"T1059.001"},
		},
		{
			name:        "empty arrays",
			raw:         `{"tactic_ids":[],"technique_ids":[]}`,
			wantTactics: []string{},
			wantTechs:   []string{},
		},
		{
			name:    "invalid JSON",
			raw:     `not json at all`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMapTactics(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.TacticIDs) != len(tc.wantTactics) {
				t.Errorf("tactic_ids: got %v, want %v", got.TacticIDs, tc.wantTactics)
			}
			if len(got.TechniqueIDs) != len(tc.wantTechs) {
				t.Errorf("technique_ids: got %v, want %v", got.TechniqueIDs, tc.wantTechs)
			}
		})
	}
}

// mockProvider is a minimal Provider implementation for testing.
type mockProvider struct{ response string }

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Complete(_ context.Context, _ CompletionRequest) (string, error) {
	return m.response, nil
}

func TestGenerateMapTacticsCallsProvider(t *testing.T) {
	p := &mockProvider{response: `{"tactic_ids":["TA0008"],"technique_ids":["T1021.001"]}`}
	result, err := GenerateMapTactics(context.Background(), p, MapTacticsInput{
		Prose:     "RDP lateral movement with no alert coverage",
		LogSource: "windows_security",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.TacticIDs) != 1 || result.TacticIDs[0] != "TA0008" {
		t.Errorf("unexpected tactic_ids: %v", result.TacticIDs)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/llm/... -run TestParseMapTactics -v`  
Expected: FAIL — `parseMapTactics` and `GenerateMapTactics` undefined.

- [ ] **Step 3: Create `map_tactics.go`**

Create `backend/internal/llm/map_tactics.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const mapTacticsSystemPrompt = `You are a MITRE ATT&CK expert. Given a security detection gap, identify the most relevant MITRE ATT&CK tactics and techniques.

Rules:
- tactic_ids: TA-prefixed IDs only (e.g. TA0001, TA0008). Maximum 3.
- technique_ids: T-prefixed IDs, include subtechnique suffix when applicable (e.g. T1566.001, T1021.001). Maximum 5.
- Only return techniques that belong to the identified tactics.
- Return empty arrays when no clear mapping exists.

Respond with JSON only — no markdown, no explanation:
{"tactic_ids": ["TA0001"], "technique_ids": ["T1566.001"]}`

// MapTacticsInput is the context for MITRE tactic mapping.
type MapTacticsInput struct {
	Prose     string
	LogSource string
}

// mapTacticsResult holds the raw parsed LLM output.
type mapTacticsResult struct {
	TacticIDs    []string `json:"tactic_ids"`
	TechniqueIDs []string `json:"technique_ids"`
}

// parseMapTactics extracts a mapTacticsResult from raw LLM output, stripping
// markdown code fences if present.
func parseMapTactics(raw string) (*mapTacticsResult, error) {
	s := strings.TrimSpace(raw)
	// Strip markdown fences: find first '{' and last '}'
	if start := strings.Index(s, "{"); start > 0 {
		s = s[start:]
	}
	if end := strings.LastIndex(s, "}"); end >= 0 && end < len(s)-1 {
		s = s[:end+1]
	}
	var r mapTacticsResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("parse map tactics JSON: %w", err)
	}
	// Defensive caps
	if len(r.TacticIDs) > 3 {
		r.TacticIDs = r.TacticIDs[:3]
	}
	if len(r.TechniqueIDs) > 5 {
		r.TechniqueIDs = r.TechniqueIDs[:5]
	}
	return &r, nil
}

// GenerateMapTactics uses the LLM to map a gap prose and log source to MITRE
// ATT&CK tactic and technique IDs.
func GenerateMapTactics(ctx context.Context, provider Provider, input MapTacticsInput) (*mapTacticsResult, error) {
	userMsg := fmt.Sprintf("Gap: %s\nLog source: %s", input.Prose, input.LogSource)

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: mapTacticsSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    256,
		FastMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	return parseMapTactics(resp)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/llm/... -run "TestParseMapTactics|TestGenerateMapTactics" -v`  
Expected: PASS for all 5 test cases.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/llm/map_tactics.go backend/internal/llm/map_tactics_test.go
git commit -m "feat(backend): add GenerateMapTactics LLM function with parse tests"
```

---

### Task 6: Backend — `HandleMapTactics` handler + route registration

**Files:**
- Modify: `backend/internal/api/handlers.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add `HandleMapTactics` to `handlers.go`**

Append after `HandleBuildDetection` (at the end of the file, or after the last handler function):

```go
// HandleMapTactics handles POST /api/map-tactics.
// It maps a gap prose description to MITRE ATT&CK tactic and technique IDs via LLM.
// On LLM failure it returns empty arrays rather than an error, so the frontend can
// still navigate to the builder.
func (h *Handler) HandleMapTactics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.MapTacticsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	req.Prose = strings.TrimSpace(req.Prose)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	if req.Prose == "" {
		writeError(w, http.StatusBadRequest, "missing required field: prose")
		return
	}
	if _, ok := h.config.Clients[req.Client]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	nvidiaKey := h.config.LLM.NvidiaAPIKey
	if h.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
	}
	providerName := h.config.LLM.SuggestionProvider
	if providerName == "" {
		providerName = h.config.LLM.DefaultProvider
	}
	provider, err := llm.NewClassifierProvider(providerName, h.config.LLM.SuggestionModel, llm.ProviderConfig{
		AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
		ClaudeModel:     h.config.LLM.ClaudeModel,
		NvidiaAPIKey:    nvidiaKey,
		NvidiaModel:     h.config.LLM.NvidiaModel,
		NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
		GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
		GeminiModel:     h.config.LLM.GeminiModel,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("provider init: %v", err))
		return
	}

	ctx := r.Context()
	result, llmErr := llm.GenerateMapTactics(ctx, provider, llm.MapTacticsInput{
		Prose:     req.Prose,
		LogSource: req.LogSource,
	})
	if llmErr != nil {
		log.Printf("WARN HandleMapTactics client=%s llm error: %v — returning empty", req.Client, llmErr)
		writeJSON(w, http.StatusOK, models.MapTacticsResponse{
			TacticIDs:    []string{},
			TechniqueIDs: []string{},
		})
		return
	}

	log.Printf("INFO HandleMapTactics client=%s tactics=%d techniques=%d",
		req.Client, len(result.TacticIDs), len(result.TechniqueIDs))
	writeJSON(w, http.StatusOK, models.MapTacticsResponse{
		TacticIDs:    result.TacticIDs,
		TechniqueIDs: result.TechniqueIDs,
	})
}
```

- [ ] **Step 2: Register the route in `main.go`**

In `backend/cmd/server/main.go`, add after the `/api/build-detection` line:

```go
mux.HandleFunc("/api/map-tactics", handler.HandleMapTactics)
```

- [ ] **Step 3: Verify the backend builds**

Run: `cd backend && go build ./...`  
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/handlers.go backend/cmd/server/main.go
git commit -m "feat(backend): add HandleMapTactics endpoint at POST /api/map-tactics"
```

---

### Task 7: Frontend — types + API service function

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: Add `MapTacticsResponse` to `types/index.ts`**

Add after the `ActionableGapCategories` interface (~line 129):

```typescript
export interface MapTacticsResponse {
  tactic_ids: string[];
  technique_ids: string[];
}
```

- [ ] **Step 2: Add `fetchMapTactics` to `services/api.ts`**

Append after the `fetchCorrelations` function:

```typescript
export async function fetchMapTactics(
  client: string,
  prose: string,
  logSource: string,
): Promise<MapTacticsResponse> {
  const res = await fetch(`${API_BASE}/api/map-tactics`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, prose, log_source: logSource }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Map tactics failed' }));
    throw new Error(err.error ?? 'Map tactics failed');
  }
  return res.json() as Promise<MapTacticsResponse>;
}
```

Add `MapTacticsResponse` to the import at the top of `api.ts` if it doesn't already import from `../types`. The file currently imports from `../types` — add `MapTacticsResponse` to that import list.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/services/api.ts
git commit -m "feat(frontend): add MapTacticsResponse type and fetchMapTactics API call"
```

---

### Task 8: `DetectionBuilder` — accept and apply `preselectedIds` prop

**Files:**
- Modify: `frontend/src/components/DetectionBuilder.tsx`

- [ ] **Step 1: Extend the `Props` interface**

In `DetectionBuilder.tsx` (~line 51), change the `Props` interface:

```typescript
interface Props {
  clientName: string;
  preselectedIds?: string[];
}
```

- [ ] **Step 2: Use `preselectedIds` as the initial basket state**

Change the `selectedIds` useState call (~line 58) to initialise from props using a lazy initialiser (runs once on mount, not on re-render):

```typescript
export default function DetectionBuilder({ clientName, preselectedIds }: Props) {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(
    () => new Set(preselectedIds ?? [])
  );
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/DetectionBuilder.tsx
git commit -m "feat(builder): accept preselectedIds prop to pre-populate basket on mount"
```

---

### Task 9: `AlertInsights` — "Build Detection →" button on Advanced Use Cases cards

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`

- [ ] **Step 1: Add `onBuildDetection` prop and import**

In `AlertInsights.tsx`, add the prop to the `Props` interface (~line 8):

```typescript
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
  onBuildDetection?: (techIds: string[]) => void;
}
```

Add `fetchMapTactics` to the imports from `'../services/api'` at the top of the file.

- [ ] **Step 2: Add per-card loading state**

In the component body (after the `correlationAbortRef` ref, ~line 141):

```typescript
const [buildLoadingKey, setBuildLoadingKey] = useState<string | null>(null);
```

- [ ] **Step 3: Add `openBuildDetection` handler**

Add after `openCorrelationDrawer`:

```typescript
const openBuildDetection = useCallback(async (rec: ActionableRecommendation, cardKey: string) => {
  if (!onBuildDetection) return;
  setBuildLoadingKey(cardKey);
  try {
    const result = await fetchMapTactics(client, rec.prose, rec.log_source);
    onBuildDetection(result.technique_ids);
  } catch {
    onBuildDetection([]);
  } finally {
    setBuildLoadingKey(null);
  }
}, [client, onBuildDetection]);
```

- [ ] **Step 4: Add the button inside `renderActionableSection`**

The `renderActionableSection` function (~line 308) takes an optional `onCorrelate` 4th param. Add a 5th param for build detection:

```typescript
const renderActionableSection = (
  title: string,
  actionable: ActionableRecommendation[] | undefined,
  fallback: string[] | undefined,
  onCorrelate?: (rec: ActionableRecommendation) => void,
  onBuild?: (rec: ActionableRecommendation, key: string) => void,
) => {
```

Inside the card JSX, after the `onCorrelate && (...)` button block (~line 354), add:

```typescript
{onBuild && (
  <button
    type="button"
    className="corr-suggest-btn"
    disabled={buildLoadingKey === queryKey}
    onClick={() => onBuild(item, queryKey)}
    style={{ marginLeft: 8 }}
  >
    {buildLoadingKey === queryKey
      ? 'Mapping…'
      : <>Build Detection <ArrowRight size={12} style={{ verticalAlign: 'middle' }} /></>}
  </button>
)}
```

- [ ] **Step 5: Pass `onBuild` only for the Advanced Use Cases section (~line 723)**

```typescript
{renderActionableSection(
  'Advanced Use Cases',
  effectiveReport?.actionable_gaps?.advanced_use_cases,
  effectiveReport?.gap_categories.advanced_use_cases,
  openCorrelationDrawer,
  onBuildDetection ? openBuildDetection : undefined,
)}
```

- [ ] **Step 6: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/AlertInsights.tsx
git commit -m "feat(insights): add Build Detection button on Advanced Use Cases gap cards"
```

---

### Task 10: `App.tsx` — wire `onBuildDetection` handler and pass `preselectedIds` to builder

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Add `preselectedTechIds` state**

In `App.tsx`, alongside the other state declarations (~line 29), add:

```typescript
const [preselectedTechIds, setPreselectedTechIds] = useState<string[]>([]);
```

- [ ] **Step 2: Add `handleBuildDetection` callback**

After the `navigate` function definition (~line 68), add:

```typescript
const handleBuildDetection = useCallback((techIds: string[]) => {
  setPreselectedTechIds(techIds);
  navigate('builder');
}, [navigate]);
```

- [ ] **Step 3: Pass `onBuildDetection` to `AlertInsights`**

Find the `<AlertInsights ...` JSX (~line 190 area). Add the new prop:

```typescript
onBuildDetection={handleBuildDetection}
```

- [ ] **Step 4: Pass `preselectedIds` to `DetectionBuilder`**

Find `<DetectionBuilder clientName={clientName} />` (~line 234). Change to:

```typescript
<DetectionBuilder
  clientName={clientName}
  preselectedIds={preselectedTechIds.length > 0 ? preselectedTechIds : undefined}
/>
```

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit`  
Expected: no errors.

- [ ] **Step 6: Verify the dev server starts**

Run: `cd frontend && npm run dev`  
Expected: server starts, no console errors on load.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(app): wire onBuildDetection handler — gaps tab → pre-populated builder"
```

---

## Summary

| Task | Area | Backend? | Can run parallel? |
|---|---|---|---|
| 1 — Noise real signal | `ThreatGraph.tsx` | No | With Task 4–10 |
| 2 — Hover debounce | `ThreatGraph.tsx` | No | After Task 1 |
| 3 — Trackpad pan/zoom | `ThreatGraph.tsx` | No | After Task 2 |
| 4 — MapTactics models | `models.go` | Yes | With Task 1–3 |
| 5 — GenerateMapTactics LLM | `llm/map_tactics.go` | Yes | After Task 4 |
| 6 — HandleMapTactics handler | `handlers.go`, `main.go` | Yes | After Task 5 |
| 7 — Frontend types + API | `types/index.ts`, `api.ts` | No | After Task 4, parallel with 5–6 |
| 8 — DetectionBuilder prop | `DetectionBuilder.tsx` | No | After Task 7 |
| 9 — AlertInsights button | `AlertInsights.tsx` | No | After Task 8 |
| 10 — App.tsx wiring | `App.tsx` | No | After Tasks 8–9 |
