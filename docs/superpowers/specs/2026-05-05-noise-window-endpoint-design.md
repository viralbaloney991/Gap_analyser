---
title: Noise Window — Dedicated Endpoint Design
date: 2026-05-05
status: approved
---

# Bug Fix: Time Window Change Reruns Full Pipeline

## Problem

Selecting a different time window in the Noise tab (7d / 14d / 30d / 90d) calls
`handleAnalyze(clientName, false, days)` in `App.tsx`, which re-runs the entire analysis
pipeline: alert fetch, MITRE classification, pairwise similarity (O(n²)), MITRE coverage,
event count fetch, and background LLM insights. This takes 15–30 seconds and resets all
other tabs (duplicates, families, gaps) for no reason — the only thing that should change
is the noise list.

## Solution

New `POST /api/noise` endpoint that runs only noise detection. The frontend calls this
endpoint on window change instead of re-running the full analyze pipeline.

**Response time target:** ~2s (event count fetch + feature extraction only, no pairwise).

## Backend Design

### New exported function: `similarity.AnalyzeNoise`

**File:** `internal/similarity/engine.go`

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

### New request/response model

**File:** `internal/models/models.go`

```go
// NoiseRequest is the request body for POST /api/noise.
type NoiseRequest struct {
    Client      string `json:"client"`
    LookbackDays int   `json:"lookback_days"`
}

// NoiseResponse is the response for POST /api/noise.
type NoiseResponse struct {
    NoiseAlerts  []NoiseAlert `json:"noise_alerts"`
    LookbackDays int          `json:"lookback_days"`
}
```

### New handler: `HandleNoise`

**File:** `internal/api/handlers.go`

```
POST /api/noise
Body: { "client": "X", "lookback_days": 7 }
Response: { "noise_alerts": [...], "lookback_days": 7 }
```

Steps:
1. Validate client and lookback_days (via `validateLookbackDays`)
2. Load alerts — store-first, same pattern as `HandleInsights`
3. `coralogix.ExtractFeatures(alerts, nil)` — populates features without LLM mappings
4. Build alert ID slice; `fetchEventCounts(ctx, region, apiKey, alertIDs, lookback)`
5. `similarity.AnalyzeNoise(alerts, eventCounts, 0)` — integrationCount=0 (Monday not fetched)
6. Return `NoiseResponse{NoiseAlerts: noiseAlerts, LookbackDays: lookback}`

### New route

**File:** `backend/cmd/server/main.go`

```go
mux.HandleFunc("/api/noise", handler.HandleNoise)
```

## Frontend Design

### New type

**File:** `frontend/src/types/index.ts`

```ts
export interface NoiseResponse {
  noise_alerts: NoiseAlert[];
  lookback_days: number;
}
```

### New API function

**File:** `frontend/src/services/api.ts`

```ts
export async function fetchNoise(client: string, lookbackDays: number): Promise<NoiseAlert[]> {
  const res = await fetch('/api/noise', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, lookback_days: lookbackDays }),
  });
  if (!res.ok) throw new Error(`noise fetch failed: ${res.status}`);
  const data: NoiseResponse = await res.json();
  return data.noise_alerts;
}
```

### App.tsx changes

**File:** `frontend/src/App.tsx`

Add `noiseLoading` state:
```ts
const [noiseLoading, setNoiseLoading] = useState(false);
```

Replace `onReanalyze` handler (currently calls full `handleAnalyze`):
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

Pass to `AlertInsights`:
```tsx
<AlertInsights
  ...
  onReanalyze={handleReanalyze}
  noiseLoading={noiseLoading}
/>
```

### AlertInsights.tsx changes

**File:** `frontend/src/components/AlertInsights.tsx`

Add `noiseLoading` to Props interface and pass it to `NoisePills` as `disabled`:
```ts
interface Props {
  ...
  noiseLoading?: boolean;
}
```

```tsx
<NoisePills days={lookbackDays} onChange={onReanalyze} disabled={isRegenerating || (noiseLoading ?? false)} />
```

## What Does Not Change

- Full `handleAnalyze` pipeline — unchanged, still called on initial client load and refresh
- `NoisePills` component — unchanged
- `lookbackDays` state and localStorage persistence — unchanged
- All other tabs (duplicates, families, gaps, MITRE) — unaffected by window change
- The `LookbackDays` field in `ClientAnalyzeRequest` — still used for initial analysis

## Test Cases

1. `AnalyzeNoise` with nil alerts returns nil
2. `AnalyzeNoise` with empty event counts returns structural noise for unscoped alerts
3. `AnalyzeNoise` with high trigger counts returns behavioral noise
4. `HandleNoise` with unknown client returns 400
5. `HandleNoise` with invalid lookback_days defaults to 30
6. `HandleNoise` returns only noise_alerts and lookback_days (not full SimilarityResult)
