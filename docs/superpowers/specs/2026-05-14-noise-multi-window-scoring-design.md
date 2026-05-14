# Noise Multi-Window Behavioral Scoring Design

## Goal

Improve noise detection false-negative rate by adding four new behavioral signals that catch alerts slipping under the current single-threshold (`> 10 fires in N days`) check. No LLM cost — all signals are deterministic math over multi-window event counts.

## Problem

`fetchEventCounts` returns one total count per alert over the selected lookback window. `AnalyzeNoise` flags an alert as behaviorally noisy only if that count exceeds 10. Patterns that slip through:

- **Burst**: fires 8 times in one day during a deployment, quiet the rest of the month — total looks low
- **Periodic**: fires 3 times per week like clockwork — total is 12 in 30d (caught), but 9 in 21d (missed)
- **Accelerating**: count is 8 today but was 2 three weeks ago — below threshold now, above it soon
- **Persistent low-volume**: fires once or twice every week without fail — total stays under 10 but never stops

## Solution: Multi-Window Comparison

Fetch event counts at four fixed windows (7d, 14d, 21d, 30d) in parallel. Derive scores from the shape of the count curve. This requires 4 API calls instead of 1, all parallelised.

The user-selected window (7/14/30/90d via NoisePills) continues to control the `high_volume` threshold. Multi-window analysis always uses the fixed 7/14/21/30d set regardless.

## Architecture

### Coralogix Client

Add `FetchAlertEventCountsMultiWindow` to `backend/internal/coralogix/client.go`:

```go
// FetchAlertEventCountsMultiWindow fetches alert event counts at 4 fixed windows
// in parallel. Returns map[alertID][4]int where index 0=7d, 1=14d, 2=21d, 3=30d.
func (c *Client) FetchAlertEventCountsMultiWindow(ctx context.Context, alertIDs []string) (map[string][4]int, error)
```

Implementation: 4 goroutines each calling the existing `FetchAlertEventCounts` with their window, merged into the return map. Falls back to a zero-filled array on partial failure (does not abort the whole request).

### Similarity Engine

`AnalyzeNoise` signature change:

```go
// Before:
func AnalyzeNoise(alerts []*models.AlertDef, eventCounts map[string]int, integrationCount int) []models.NoiseAlert

// After:
func AnalyzeNoise(alerts []*models.AlertDef, multiCounts map[string][4]int, integrationCount int) []models.NoiseAlert
```

The existing `map[string]int` single-window path is removed. `multiCounts[id][3]` (30d) replaces the former single count for backward-compatible `high_volume` detection.

Three new pure functions added to `backend/internal/similarity/`:

```go
func burstScore(counts [4]int) float64        // counts[0] / (counts[3]/4)
func periodicityScore(counts [4]int) float64  // |counts[0]/(counts[1]/2) - 1|
func accelerationScore(counts [4]int) float64 // counts[0] / (counts[3]/4)
```

Pattern classification (in priority order — first match wins):

| Pattern | Condition |
|---------|-----------|
| `high_volume` | counts[3] > 10 (existing) |
| `burst` | burstScore > 2.5 AND counts[3] <= 10 |
| `periodic` | periodicityScore < 0.2 AND counts[1] >= 2 |
| `accelerating` | accelerationScore > 1.5 AND counts[3] <= 10 |
| `persistent` | counts[0]>0 AND counts[1]>counts[0] AND counts[2]>counts[1] AND counts[3]<=10 |

An alert with no pattern match is not behaviorally noisy (structural check still runs independently).

### Models

Add to `NoiseAlert` in `backend/internal/models/models.go`:

```go
NoisePattern  string  `json:"noise_pattern,omitempty"`   // high_volume|burst|periodic|accelerating|persistent
WindowCounts  [4]int  `json:"window_counts,omitempty"`   // [7d, 14d, 21d, 30d]
BurstScore    float64 `json:"burst_score,omitempty"`
```

### Handler

`HandleNoise` calls `FetchAlertEventCountsMultiWindow` instead of `fetchEventCounts`. The existing `fetchEventCounts` helper remains for the full analysis pipeline (`HandleAnalyze`), which still uses the single-window count.

## Frontend

`IntegrationSummary.tsx` or wherever the noise alert list is rendered: add a small `NoisePattern` badge per row ("Burst", "Periodic", "Accelerating", "Persistent", "High Volume"). Tooltip on the badge shows window counts: `7d: N / 14d: N / 21d: N / 30d: N`.

`NoisePills` (7d/14d/30d/90d): no change.

## Scoring Thresholds

| Score | Threshold | Rationale |
|-------|-----------|-----------|
| Burst | > 2.5× expected | 2.5× means recent week had 2.5× its proportional share of fires |
| Periodicity | delta < 0.2 (20%) | Within 20% of perfectly even = periodic |
| Acceleration | > 1.5× expected | Recent week 50% above proportional share = accelerating |
| Persistent | fires in all 4 windows | Present every week regardless of volume |

Thresholds are constants in `similarity/noise.go` and can be tuned without API changes.

## Files Changed

**Modified:**
- `backend/internal/coralogix/client.go` — add `FetchAlertEventCountsMultiWindow`
- `backend/internal/models/models.go` — add `NoisePattern`, `WindowCounts`, `BurstScore` to `NoiseAlert`
- `backend/internal/similarity/engine.go` — update `AnalyzeNoise` signature, add scoring functions
- `backend/internal/api/handlers.go` — update `HandleNoise` to call multi-window fetch
- `frontend/src/components/IntegrationSummary.tsx` (or noise list component) — add pattern badge + tooltip

**Test changes:**
- `backend/internal/similarity/` — unit tests for `burstScore`, `periodicityScore`, `accelerationScore`, pattern classification
- `backend/internal/api/` — update `HandleNoise` test mock to use `[4]int` counts

## Out of Scope

- Per-day (N=30 API calls) time-series data
- LLM-based structural scoring (separate follow-up)
- Changes to the `HandleAnalyze` full-pipeline path
- Changing the NoisePills UI window options
- Persisting noise history to NeonDB
