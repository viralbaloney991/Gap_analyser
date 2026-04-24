# Noise Classification Redesign — Design Spec
**Date:** 2026-04-24
**Status:** Approved for implementation

---

## Problem

The current noise classification in `findNoiseAlerts` has three distinct failures:

1. **Vendor-covered alerts appear as Noise.** Alerts like "GCP Security Command Center - Low Severity Toxic Combination Class Finding" are flagged as noisy because the feature extractor finds sparse tokens (the vendor handles detection internally). These are intentionally sparse — it is the vendor's job to detect, Coralogix just receives a severity signal.

2. **Every noise card shows the same generic reason.** The reason string is built from fixed templates keyed only on which feature dimensions are empty. Two completely different alerts — a GCP finding and an AWS threshold rule — both missing `entities` and `behavioral signal` get byte-for-byte identical reason text. The alert name and what IS present are never used.

3. **Behavioral noise is invisible.** A well-structured flow alert that triggers 20 times in 30 days is genuinely noisy (too many matches given the building block combination), but the current purely-structural check would not flag it.

---

## Solution

Replace the single-dimension feature-token check with a **hybrid two-signal noise model**.

An alert is noisy if **either or both** of the following signals are true:

### Signal 1 — Behavioral Noise (Confirmed)

The alert has actually been firing too often in production.

- **Source:** `com.coralogixapis.events.v3.EventsService/ListEventsCount` — returns an integer trigger count for a list of alert IDs within a time window.
- **Window:** 30 days.
- **Threshold:** `trigger_count > 20` — named constant `behavioralNoiseThreshold = 20` (tunable).
- **Exclusions:** Vendor-covered alerts (high frequency is expected from a vendor detection engine).
- **Reason text (specific):** *"Fired 47 times in the last 30 days — alert is over-triggering. Review threshold or add entity/app scoping."*

### Signal 2 — Structural Noise (Predicted)

The alert configuration is too generic given this org's data ingestion footprint — it **will** fire broadly even if it hasn't yet.

**Criteria — all must be true:**
1. No `applicationName` AND no `subsystemName` filter in the Lucene filter (unscoped)
2. No entity extracted (no user / IP / host / device in name or query)
3. Alert type is `logs_threshold`, `metric_threshold`, or `logs_immediate` (high-volume types prone to broad matching)
4. NOT vendor-covered, NOT a building block, IS a security alert

**Org context amplifier:** If the org has ≥ 10 integrations (available from `AnalyzeResponse.Integrations`), the structural flag is promoted to confirmed — a generic unscoped alert across many integrations will fire everywhere.

**Reason text (specific):** *"No app/subsystem scoping across an org with 43 integrations — this alert will trigger on all matching log sources."* The reason incorporates the actual integration count.

### Exclusions — Never Noise

| Category | Condition | Rationale |
|---|---|---|
| Vendor-covered | `alert.Features.VendorCovered == true` | Vendor handles detection internally; sparse features are intentional |
| Building blocks | `alert.Labels["flow_alert"] == "building block"` | Fragments by design; evaluated as part of a flow alert |
| Non-security alerts | `alert.Features.IsSecurityAlert == false` | Outside the scope of security noise analysis |
| Flow alerts | `alert.AlertType == "flow"` | Structural noise is assessed through their building blocks; behavioral count applies independently |

---

## Architecture

### Files Changed

| File | Change |
|---|---|
| `backend/internal/coralogix/client.go` | Add `FetchAlertEventCounts(ctx, alertIDs []string, days int) (map[string]int, error)` |
| `backend/internal/models/models.go` | Add `TriggerCount int` and `NoiseType string` to `NoiseAlert` |
| `backend/internal/similarity/engine.go` | Rewrite `findNoiseAlerts` to accept `eventCounts map[string]int` and `integrationCount int`; implement two-signal model |
| `backend/internal/api/handlers.go` | Fetch event counts before running analysis; pass into `Analyze()` |
| `frontend/src/types/index.ts` | Add `trigger_count?: number` and `noise_type?: string` to `NoiseAlert` |
| `frontend/src/components/AlertInsights.tsx` | Show `trigger_count` and `noise_type` badge in noise cards |
| `frontend/src/App.css` | Add `.noise-type-badge` variants for behavioral / structural / both |

### No new files required.

---

## Implementation Detail

### New Coralogix client method

```go
// FetchAlertEventCounts returns the 30-day trigger count for each alert ID.
// Uses EventsService/ListEventsCount (integer response, no pagination needed).
// Returns a map of alertID → count; missing IDs have count 0.
func (c *Client) FetchAlertEventCounts(
    ctx context.Context,
    alertIDs []string,
    days int,
) (map[string]int, error)
```

Request body sent to `com.coralogixapis.events.v3.EventsService/ListEventsCount`:
```json
{
  "alert_ids": ["<id1>", "<id2>", ...],
  "timestamp_range": {
    "from": "<now - days>",
    "to": "<now>"
  }
}
```

Batch size: send all alert IDs in a single call (the API accepts `repeated string alert_ids`). If the call fails (permission denied, service unavailable), return `nil, err` — the caller falls back to structural-only noise detection.

### Updated NoiseAlert model

```go
type NoiseAlert struct {
    Name            string   `json:"name"`
    MissingFeatures []string `json:"missing_features"`
    Reason          string   `json:"reason,omitempty"`
    TriggerCount    int      `json:"trigger_count,omitempty"`  // 0 = not available or not behaviorally noisy
    NoiseType       string   `json:"noise_type,omitempty"`     // "behavioral" | "structural" | "both"
}
```

### Updated findNoiseAlerts signature

```go
func findNoiseAlerts(
    vectors []featureVector,
    alerts []*models.AlertDef,
    eventCounts map[string]int,     // alertID → 30-day trigger count; nil = skip behavioral check
    integrationCount int,           // total integrations in org
) []models.NoiseAlert
```

### Noise detection logic (pseudocode)

```go
const behavioralNoiseThreshold = 20  // triggers in 30 days

for i, v := range vectors {
    alert := alerts[i]

    // ── Exclusions ─────────────────────────────────────
    if alert.Features.VendorCovered        { continue }
    if isFlowAlert(alert)                  { continue }
    if isBuildingBlock(alert)              { continue }
    if !alert.Features.IsSecurityAlert     { continue }

    // ── Signal 1: Behavioral ───────────────────────────
    isBehavioral := false
    triggerCount := eventCounts[alert.ID]
    if eventCounts != nil && triggerCount > behavioralNoiseThreshold {
        isBehavioral = true
    }

    // ── Signal 2: Structural ───────────────────────────
    app, sub := ExtractAppSubsystem(alert.TypeDef)
    isUnscoped := app == "" && sub == ""
    noEntity   := len(v.entities) == 0
    isHighVolumeType := alert.AlertType == "logs_threshold" ||
                        alert.AlertType == "metric_threshold" ||
                        alert.AlertType == "logs_immediate"

    isStructural := isUnscoped && noEntity && isHighVolumeType

    // ── Neither signal → skip ──────────────────────────
    if !isBehavioral && !isStructural { continue }

    // ── Build NoiseAlert ───────────────────────────────
    noiseType := noiseTypeString(isBehavioral, isStructural)
    reason    := buildReason(alert, v, triggerCount, integrationCount, isBehavioral, isStructural)
    missing   := buildMissingFeatures(v)

    noisy = append(noisy, models.NoiseAlert{
        Name:            v.alertName,
        MissingFeatures: missing,
        Reason:          reason,
        TriggerCount:    triggerCount,
        NoiseType:       noiseType,
    })
}
```

### Reason text generation

```go
func buildReason(alert, v, triggerCount, integrationCount int, isBehavioral, isStructural bool) string {
    var parts []string
    if isBehavioral {
        parts = append(parts, fmt.Sprintf(
            "Fired %d times in the last 30 days — alert is over-triggering.", triggerCount))
    }
    if isStructural {
        if integrationCount >= 10 {
            parts = append(parts, fmt.Sprintf(
                "No app/subsystem scoping across an org with %d integrations — fires on all matching log sources.",
                integrationCount))
        } else {
            parts = append(parts, "No app/subsystem scoping and no entity filter — alert may fire too broadly.")
        }
    }
    return strings.Join(parts, " ")
}
```

### Handler change

In `api/handlers.go`, before calling `Analyze()`:
```go
alertIDs := make([]string, len(alerts))
for i, a := range alerts { alertIDs[i] = a.ID }

eventCounts, err := cxClient.FetchAlertEventCounts(ctx, alertIDs, 30)
if err != nil {
    log.Printf("WARN [noise] event counts unavailable: %v — falling back to structural-only", err)
    eventCounts = nil  // graceful degradation
}

result := engine.Analyze(alerts, eventCounts, len(integrations))
```

### Frontend — noise card additions

In `AlertInsights.tsx`, the noise card collapsed header gains:
- A `noise_type` badge: `Behavioral` (red), `Structural` (amber), `Both` (dark red)
- The existing `reason` string already renders as `insight-card-noise-preview` — no change needed for reason display
- If `trigger_count > 0`: show `Fired {N}×` chip next to the badge

```tsx
<span className={`noise-type-badge noise-type-badge--${noise.noise_type ?? 'structural'}`}>
  {noiseTypeLabel(noise.noise_type)}
</span>
{noise.trigger_count > 0 && (
  <span className="noise-trigger-count">Fired {noise.trigger_count}×</span>
)}
```

New CSS classes:
```css
.noise-type-badge { font-family: var(--font-mono); font-size: 0.6rem; padding: 2px 6px; border-radius: 3px; }
.noise-type-badge--behavioral { background: #7f1d1d; color: #fca5a5; }
.noise-type-badge--structural { background: #78350f; color: #fcd34d; }
.noise-type-badge--both       { background: #450a0a; color: #fca5a5; }
.noise-trigger-count { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-dim); }
```

---

## Graceful Degradation

If `FetchAlertEventCounts` fails (permission error, network issue, service unavailable):
- Pass `eventCounts = nil` to `findNoiseAlerts`
- Structural signal still runs normally
- No behavioral badges shown
- Log a warning — no user-visible error

---

## Behaviour After Fix

| Scenario | Before | After |
|---|---|---|
| GCP SCC vendor alert | Appears in Noise (wrong) | Excluded — vendor-covered |
| Well-scoped flow alert, 25 triggers/30d | Not in Noise | Appears — Behavioral badge, "Fired 25 times in 30 days" |
| Generic threshold, no app/subsystem, org has 43 integrations | Might appear with wrong generic reason | Appears — Structural badge, "No scoping across 43 integrations" |
| Same threshold alert in an org with 3 integrations | Might appear | Does NOT appear — org footprint too small to amplify risk |
| All noise cards | Identical reason text | Distinct reason text per alert incorporating count/integration context |

---

## Out of Scope

- Configurable `behavioralNoiseThreshold` per client (use the constant for now)
- Alert suppression rules or muting
- Noise trending over time (week-over-week change in trigger count)
- Changes to Duplicates, Families, Merge, Coverage, or Recommendations tabs
