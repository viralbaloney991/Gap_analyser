# Noise Classification Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-dimension feature-token noise check with a hybrid two-signal model that catches behaviorally over-firing alerts AND structurally under-scoped alerts, while correctly excluding vendor-covered alerts, building blocks, non-security alerts, and flow alert structural checks.

**Architecture:** Backend changes work outward from data model → client → engine → handler; frontend changes work inward from types → component → styles. All changes are additive (no removal of existing structural exclusion logic — it is replaced with new logic). Graceful degradation: if the EventsService call fails, `eventCounts` is `nil` and only the structural signal runs.

**Tech Stack:** Go (backend), React + TypeScript (frontend), grpcurl for Coralogix API calls.

---

### Task 1: Update NoiseAlert Data Model

**Files:**
- Modify: `backend/internal/models/models.go`

The current `NoiseAlert` struct has `Name`, `MissingFeatures`, `Reason`. We add `TriggerCount` (0 = not behaviorally noisy or data unavailable) and `NoiseType` ("behavioral" | "structural" | "both" | "").

- [ ] **Step 1: Add fields to NoiseAlert struct**

In `backend/internal/models/models.go`, replace the `NoiseAlert` struct (currently lines 75-79):

```go
// NoiseAlert represents an alert flagged by the hybrid noise model.
// TriggerCount is 0 when behavioral data is unavailable or the alert is not behaviorally noisy.
// NoiseType is "behavioral", "structural", or "both"; empty string means unclassified.
type NoiseAlert struct {
	Name            string   `json:"name"`
	MissingFeatures []string `json:"missing_features"`
	Reason          string   `json:"reason,omitempty"`
	TriggerCount    int      `json:"trigger_count,omitempty"`
	NoiseType       string   `json:"noise_type,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add backend/internal/models/models.go
git commit -m "feat(models): add TriggerCount and NoiseType to NoiseAlert"
```

---

### Task 2: Add FetchAlertEventCounts to Coralogix Client

**Files:**
- Modify: `backend/internal/coralogix/client.go`
- Create: `backend/internal/coralogix/client_events_test.go`

The Coralogix `EventsService/ListEventsCount` endpoint accepts a list of alert IDs and a time range, returning trigger counts per alert. We shell out to grpcurl (same pattern as `FetchAllAlerts`). We extract the JSON parsing into a testable helper so tests don't need to call grpcurl.

**Important:** The exact JSON field names in the response must be verified against the actual API. The struct below uses the most common protobuf-to-JSON transcoding convention (`alertsEventsCounts` with camelCase). If the real response uses different field names, update `listEventsCountResp` only — `FetchAlertEventCounts` and its tests remain unchanged.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/coralogix/client_events_test.go`:

```go
package coralogix

import (
	"testing"
)

func TestParseEventCountResponse_happyPath(t *testing.T) {
	raw := []byte(`{
		"alertsEventsCounts": [
			{"alertId": "abc123", "count": 47},
			{"alertId": "def456", "count": 5}
		]
	}`)
	got, err := parseEventCountResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["abc123"] != 47 {
		t.Errorf("abc123: want 47, got %d", got["abc123"])
	}
	if got["def456"] != 5 {
		t.Errorf("def456: want 5, got %d", got["def456"])
	}
}

func TestParseEventCountResponse_emptyResponse(t *testing.T) {
	raw := []byte(`{"alertsEventsCounts": []}`)
	got, err := parseEventCountResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseEventCountResponse_missingAlertInResponse(t *testing.T) {
	// Alerts not in the response should have count 0 (caller's responsibility to default).
	raw := []byte(`{"alertsEventsCounts": [{"alertId": "abc123", "count": 10}]}`)
	got, err := parseEventCountResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["missing-id"]; ok {
		t.Errorf("missing-id should not be in map")
	}
	// count 10 for abc123 is well below behavioral threshold — just verifying parse
	if got["abc123"] != 10 {
		t.Errorf("abc123: want 10, got %d", got["abc123"])
	}
}

func TestParseEventCountResponse_invalidJSON(t *testing.T) {
	_, err := parseEventCountResponse([]byte(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/coralogix/ -run TestParseEventCountResponse -v
```

Expected: FAIL — `parseEventCountResponse` is undefined.

- [ ] **Step 3: Implement FetchAlertEventCounts and parseEventCountResponse**

Add to `backend/internal/coralogix/client.go` (after the `grpcCall` method, before the `// ── JSON response types` comment):

```go
// FetchAlertEventCounts returns the trigger count for each alert ID over the
// past [days] days. Uses EventsService/ListEventsCount (no pagination).
// Returns a map of alertID → count; IDs not in the response have count 0.
// If the call fails, returns nil, err — the caller falls back to structural-only.
func (c *Client) FetchAlertEventCounts(
	ctx context.Context,
	alertIDs []string,
	days int,
) (map[string]int, error) {
	if len(alertIDs) == 0 {
		return map[string]int{}, nil
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -days)

	type reqBody struct {
		AlertIDs        []string `json:"alert_ids"`
		TimestampRange  struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"timestamp_range"`
	}
	var body reqBody
	body.AlertIDs = alertIDs
	body.TimestampRange.From = from.Format(time.RFC3339)
	body.TimestampRange.To = now.Format(time.RFC3339)

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal event count request: %w", err)
	}

	raw, err := c.grpcCall(ctx, "com.coralogixapis.events.v3.EventsService/ListEventsCount", string(bodyJSON))
	if err != nil {
		return nil, err
	}
	return parseEventCountResponse(raw)
}

// parseEventCountResponse parses the ListEventsCount JSON response into a
// map of alertID → count. Extracted for testability (avoids grpcurl dependency).
// NOTE: if the real API uses different field names, update listEventsCountResp only.
func parseEventCountResponse(raw []byte) (map[string]int, error) {
	var resp listEventsCountResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse event count response: %w", err)
	}
	counts := make(map[string]int, len(resp.AlertsEventsCounts))
	for _, entry := range resp.AlertsEventsCounts {
		counts[entry.AlertID] = entry.Count
	}
	return counts, nil
}
```

Add the `time` import and the response type. In `client.go`, the imports block currently has `bytes`, `context`, `encoding/json`, `fmt`, `os/exec`, `strings`. Add `"time"` to that block.

Add to the `// ── JSON response types` section at the bottom of `client.go`:

```go
// listEventsCountResp mirrors the EventsService/ListEventsCount JSON response.
// Field names are camelCase per protobuf-to-JSON transcoding convention.
// Verify against real API output if counts are always 0.
type listEventsCountResp struct {
	AlertsEventsCounts []struct {
		AlertID string `json:"alertId"`
		Count   int    `json:"count"`
	} `json:"alertsEventsCounts"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/coralogix/ -run TestParseEventCountResponse -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add backend/internal/coralogix/client.go backend/internal/coralogix/client_events_test.go
git commit -m "feat(coralogix): add FetchAlertEventCounts using EventsService/ListEventsCount"
```

---

### Task 3: Rewrite findNoiseAlerts in engine.go

**Files:**
- Modify: `backend/internal/similarity/engine.go`
- Modify: `backend/internal/similarity/engine_test.go`

Replace the single-dimension feature-token check with the hybrid two-signal model. The new `findNoiseAlerts` signature adds `eventCounts map[string]int` and `integrationCount int` parameters. The `Analyze` function signature is NOT changed yet (Task 4) — for now, `findNoiseAlerts` gets the new signature, and `Analyze` passes `nil, 0` temporarily to keep the build green.

The complete new logic:
- **Exclusions:** skip vendor-covered, building-block, non-security, and flow alerts (for structural; behavioral still applies to flow)
- **Signal 1 (behavioral):** `eventCounts != nil && eventCounts[alert.ID] > behavioralNoiseThreshold`
- **Signal 2 (structural):** unscoped (no app AND no subsystem) AND no entity AND high-volume type — applies to non-flow alerts only
- **Org amplifier:** if integrationCount ≥ 10, structural is "confirmed" — this affects reason text, not detection

- [ ] **Step 1: Write failing tests for new logic**

Add to `backend/internal/similarity/engine_test.go` (after existing tests):

```go
// ── New hybrid noise model tests ──────────────────────────────────────────

func makeAlert(id, alertType string, vendorCovered, isSecurityAlert bool, labels map[string]string, appName, subsystem string) *models.AlertDef {
	typeDef := map[string]any{}
	if appName != "" || subsystem != "" {
		typeDef["logsFilter"] = map[string]any{
			"simpleFilter": map[string]any{
				"labelFilters": map[string]any{
					"applicationName": []any{map[string]any{"value": appName, "operation": "IS"}},
					"subsystemName":   []any{map[string]any{"value": subsystem, "operation": "IS"}},
				},
			},
		}
	}
	if labels == nil {
		labels = map[string]string{}
	}
	return &models.AlertDef{
		ID:        id,
		AlertType: alertType,
		Labels:    labels,
		TypeDef:   typeDef,
		Features: models.AlertFeatures{
			VendorCovered:   vendorCovered,
			IsSecurityAlert: isSecurityAlert,
		},
	}
}

// Vectors for structural noise: logs_threshold, no entity, no other features
func sparseLogsThresholdVector(name string) featureVector {
	return featureVector{
		alertName:   name,
		dataSources: map[string]struct{}{},
		entities:    map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
}

func TestFindNoiseAlerts_vendorCoveredExcluded(t *testing.T) {
	v := sparseLogsThresholdVector("GCP SCC Alert")
	alert := makeAlert("gcp-1", "logs_threshold", true, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("vendor-covered alert should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_buildingBlockExcluded(t *testing.T) {
	v := sparseLogsThresholdVector("BB Alert")
	alert := makeAlert("bb-1", "logs_threshold", false, true,
		map[string]string{"flow_alert": "building block"}, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("building block should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_nonSecurityExcluded(t *testing.T) {
	v := sparseLogsThresholdVector("Ops Alert")
	alert := makeAlert("ops-1", "logs_threshold", false, false, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("non-security alert should be excluded, got %v", noisy)
	}
}

func TestFindNoiseAlerts_structuralNoise_unscopedHighVolume(t *testing.T) {
	v := sparseLogsThresholdVector("Generic Threshold")
	// No app/subsystem (unscoped), security, logs_threshold
	alert := makeAlert("t-1", "logs_threshold", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert, got %d: %v", len(noisy), noisy)
	}
	if noisy[0].NoiseType != "structural" {
		t.Errorf("noise_type: want structural, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_structuralNoise_scopedAlertNotNoisy(t *testing.T) {
	v := sparseLogsThresholdVector("Scoped Alert")
	// Has app scoping → not structural noise
	alert := makeAlert("t-2", "logs_threshold", false, true, nil, "my-app", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("scoped alert should not be structural noise, got %v", noisy)
	}
}

func TestFindNoiseAlerts_structuralNoise_lowVolumeTypeNotNoisy(t *testing.T) {
	v := sparseLogsThresholdVector("Anomaly Alert")
	// logs_anomaly is not a high-volume type
	alert := makeAlert("t-3", "logs_anomaly", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("low-volume type alert should not be structural noise, got %v", noisy)
	}
}

func TestFindNoiseAlerts_behavioralNoise_overThreshold(t *testing.T) {
	v := featureVector{
		alertName:   "Chatty Alert",
		dataSources: map[string]struct{}{"cloudtrail": {}},
		entities:    map[string]struct{}{"user": {}},
		actions:     map[string]struct{}{"login": {}},
		conditions:  map[string]struct{}{"failed": {}},
		techniques:  map[string]struct{}{"t1078": {}},
	}
	// Rich alert — would NOT be structural noise. But fires 25 times.
	alert := makeAlert("b-1", "logs_threshold", false, true, nil, "my-app", "auth")
	counts := map[string]int{"b-1": 25}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 behavioral noise alert, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "behavioral" {
		t.Errorf("noise_type: want behavioral, got %q", noisy[0].NoiseType)
	}
	if noisy[0].TriggerCount != 25 {
		t.Errorf("trigger_count: want 25, got %d", noisy[0].TriggerCount)
	}
}

func TestFindNoiseAlerts_behavioralNoise_atThresholdNotNoisy(t *testing.T) {
	v := sparseLogsThresholdVector("Borderline Alert")
	// Exactly at threshold (20) — not noisy
	alert := makeAlert("b-2", "logs_threshold", false, true, nil, "my-app", "")
	counts := map[string]int{"b-2": 20}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0)
	// 20 is not > 20, so not behavioral. No scoping issue either (has app).
	if len(noisy) != 0 {
		t.Errorf("alert at threshold should not be noisy, got %v", noisy)
	}
}

func TestFindNoiseAlerts_bothSignals(t *testing.T) {
	v := sparseLogsThresholdVector("Double Trouble")
	// Unscoped + high volume type → structural; also fires 30 times → behavioral
	alert := makeAlert("bt-1", "logs_threshold", false, true, nil, "", "")
	counts := map[string]int{"bt-1": 30}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 5)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 noise alert, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "both" {
		t.Errorf("noise_type: want both, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_flowAlert_behavioralApplies(t *testing.T) {
	v := featureVector{
		alertName:   "Flow Alert",
		dataSources: map[string]struct{}{"logs": {}},
		entities:    map[string]struct{}{},
		actions:     map[string]struct{}{},
		conditions:  map[string]struct{}{},
		techniques:  map[string]struct{}{},
	}
	// Flow alert with high trigger count → behavioral noise applies
	alert := makeAlert("f-1", "flow", false, true, nil, "", "")
	counts := map[string]int{"f-1": 25}
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, counts, 0)
	if len(noisy) != 1 {
		t.Fatalf("flow alert with high count should be behavioral noise, got %d", len(noisy))
	}
	if noisy[0].NoiseType != "behavioral" {
		t.Errorf("noise_type: want behavioral, got %q", noisy[0].NoiseType)
	}
}

func TestFindNoiseAlerts_flowAlert_structuralDoesNotApply(t *testing.T) {
	v := sparseLogsThresholdVector("Flow No Triggers")
	// Flow alert, sparse, unscoped — structural should NOT apply (behavioral needs count)
	alert := makeAlert("f-2", "flow", false, true, nil, "", "")
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 0)
	if len(noisy) != 0 {
		t.Errorf("flow alert should skip structural signal, got %v", noisy)
	}
}

func TestFindNoiseAlerts_orgAmplifier_reasonContainsCount(t *testing.T) {
	v := sparseLogsThresholdVector("Generic")
	alert := makeAlert("amp-1", "logs_threshold", false, true, nil, "", "")
	// 15 integrations → org amplifier kicks in, reason should mention count
	noisy := findNoiseAlerts([]featureVector{v}, []*models.AlertDef{alert}, nil, 15)
	if len(noisy) != 1 {
		t.Fatalf("expected 1 structural noise alert, got %d", len(noisy))
	}
	if !strings.Contains(noisy[0].Reason, "15") {
		t.Errorf("reason should mention integration count 15, got: %q", noisy[0].Reason)
	}
}
```

Add `"strings"` to the test imports if not present.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/ -run "TestFindNoiseAlerts_vendor|TestFindNoiseAlerts_building|TestFindNoiseAlerts_non|TestFindNoiseAlerts_structural|TestFindNoiseAlerts_behavioral|TestFindNoiseAlerts_both|TestFindNoiseAlerts_flow|TestFindNoiseAlerts_org" -v 2>&1 | head -40
```

Expected: compile error or test failures because `findNoiseAlerts` still has the old signature.

- [ ] **Step 3: Update existing tests for new signature**

In `engine_test.go`, all existing `findNoiseAlerts(vectors, nil)` calls need two more arguments. Update every call site:

Old:
```go
noisy := findNoiseAlerts(vectors, nil)
```
New:
```go
noisy := findNoiseAlerts(vectors, nil, nil, 0)
```

Also update `findNoiseAlerts(nil, nil)`:
```go
noisy := findNoiseAlerts(nil, nil, nil, 0)
```

Search for all occurrences:
```bash
cd /Users/aviral.baloni/Desktop/claude/backend && grep -n "findNoiseAlerts" internal/similarity/engine_test.go
```

Update every call site. The existing tests test the OLD feature-token logic which is being replaced. After the new implementation, those tests may need to be removed or adapted — see Step 5.

- [ ] **Step 4: Implement new findNoiseAlerts**

In `backend/internal/similarity/engine.go`, replace the `findNoiseAlerts` function (currently lines 1073–1148) with:

```go
// ── Step 8: Noise Detection ──────────────────────────────────────────────

const behavioralNoiseThreshold = 20 // triggers in 30 days before alert is behaviorally noisy

// findNoiseAlerts applies the hybrid two-signal noise model.
//
//   - Signal 1 (behavioral): alert fired > behavioralNoiseThreshold times in the
//     last 30 days per eventCounts. Pass nil to skip behavioral detection.
//   - Signal 2 (structural): alert is unscoped (no app/subsystem), has no entity,
//     and is a high-volume type (logs_threshold, metric_threshold, logs_immediate).
//
// Exclusions (neither signal applies):
//
//   - Vendor-covered alerts: intentionally sparse, vendor does detection internally.
//   - Building blocks (flow_alert=building block): fragments by design.
//   - Non-security alerts: outside the scope of security noise analysis.
//
// Flow alerts skip structural (assessed via building blocks) but ARE checked for
// behavioral noise — a flow firing too often is genuinely noisy.
//
// alerts is parallel to vectors (same index). Pass nil alerts in tests that
// do not need alert-level fields (behavioral and structural signals are skipped).
func findNoiseAlerts(
	vectors []featureVector,
	alerts []*models.AlertDef,
	eventCounts map[string]int,  // alertID → 30-day trigger count; nil = skip behavioral
	integrationCount int,         // total integrations in org (for structural reason text)
) []models.NoiseAlert {
	var noisy []models.NoiseAlert

	for i, v := range vectors {
		// Determine if we have an alert struct for this vector.
		var alert *models.AlertDef
		if alerts != nil && i < len(alerts) {
			alert = alerts[i]
		}

		// ── Exclusions ────────────────────────────────────────────────────
		if alert != nil {
			if alert.Features.VendorCovered {
				continue
			}
			if alert.Labels["flow_alert"] == "building block" {
				continue
			}
			if !alert.Features.IsSecurityAlert {
				continue
			}
		}

		isFlowAlert := alert != nil && alert.AlertType == "flow"

		// ── Signal 1: Behavioral ─────────────────────────────────────────
		var triggerCount int
		if alert != nil && eventCounts != nil {
			triggerCount = eventCounts[alert.ID]
		}
		isBehavioral := eventCounts != nil && triggerCount > behavioralNoiseThreshold

		// ── Signal 2: Structural (skipped for flow alerts) ───────────────
		isStructural := false
		if !isFlowAlert && alert != nil {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
			isUnscoped := app == "" && sub == ""
			noEntity := len(v.entities) == 0
			isHighVolumeType := alert.AlertType == "logs_threshold" ||
				alert.AlertType == "metric_threshold" ||
				alert.AlertType == "logs_immediate"
			isStructural = isUnscoped && noEntity && isHighVolumeType
		}

		// ── Neither signal → skip ─────────────────────────────────────────
		if !isBehavioral && !isStructural {
			continue
		}

		// ── Build NoiseAlert ──────────────────────────────────────────────
		noisy = append(noisy, models.NoiseAlert{
			Name:            v.alertName,
			MissingFeatures: buildMissingFeatures(v),
			Reason:          buildNoiseReason(triggerCount, integrationCount, isBehavioral, isStructural),
			TriggerCount:    triggerCount,
			NoiseType:       noiseTypeString(isBehavioral, isStructural),
		})
	}

	sort.Slice(noisy, func(i, j int) bool {
		return noisy[i].Name < noisy[j].Name
	})
	return noisy
}

// noiseTypeString returns "behavioral", "structural", or "both".
func noiseTypeString(isBehavioral, isStructural bool) string {
	switch {
	case isBehavioral && isStructural:
		return "both"
	case isBehavioral:
		return "behavioral"
	default:
		return "structural"
	}
}

// buildNoiseReason returns a specific human-readable reason for the noise classification.
func buildNoiseReason(triggerCount, integrationCount int, isBehavioral, isStructural bool) string {
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

// buildMissingFeatures returns the names of empty feature dimensions for this vector.
func buildMissingFeatures(v featureVector) []string {
	var missing []string
	if len(v.dataSources) == 0 {
		missing = append(missing, "data sources")
	}
	if len(v.entities) == 0 {
		missing = append(missing, "entities")
	}
	if len(v.actions) == 0 {
		missing = append(missing, "actions")
	}
	if len(v.conditions) == 0 {
		missing = append(missing, "conditions")
	}
	if len(v.techniques) == 0 {
		missing = append(missing, "techniques")
	}
	return missing
}
```

- [ ] **Step 5: Adapt or remove obsolete existing tests**

The three original tests (`TestFindNoiseAlerts_returnsNoisyAlerts`, `TestFindNoiseAlerts_nilInput`, `TestFindNoiseAlerts_atThreshold`) test the old feature-token threshold model which no longer exists. Update them to match the new model behavior:

- `TestFindNoiseAlerts_nilInput` → still valid, just update signature: `findNoiseAlerts(nil, nil, nil, 0)`
- `TestFindNoiseAlerts_returnsNoisyAlerts` → the "Noisy" alert has 0 entities and 1 data source. With nil alerts, the structural signal can't check alert type/scoping. **Remove this test** — it relies on internals that no longer exist. The new behavioral tests cover this case.
- `TestFindNoiseAlerts_atThreshold` → **Remove this test** — the concept of a feature-token threshold is gone.

Also look for any other calls to `findNoiseAlerts` in `engine_test.go` with the `TestFindNoiseAlerts_disjointSets` or `TestWeightedJaccard` patterns — those test different functions and are unaffected.

Check: `grep -n "findNoiseAlerts" /Users/aviral.baloni/Desktop/claude/backend/internal/similarity/engine_test.go`

Remove the two obsolete tests. Keep `TestFindNoiseAlerts_nilInput` with updated signature.

- [ ] **Step 6: Also update Analyze (temporary stub) to pass nil, 0**

In `engine.go`, find the `Analyze` function (line ~239):

Current:
```go
noiseAlerts := findNoiseAlerts(vectors, alerts)
```

Temporary (for this task only — Task 4 will wire real data):
```go
noiseAlerts := findNoiseAlerts(vectors, alerts, nil, 0)
```

- [ ] **Step 7: Run all similarity tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/similarity/ -v 2>&1 | tail -30
```

Expected: all tests PASS including the new hybrid model tests.

- [ ] **Step 8: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(similarity): rewrite findNoiseAlerts with hybrid behavioral+structural noise model"
```

---

### Task 4: Update Analyze Signature and Wire Handler

**Files:**
- Modify: `backend/internal/similarity/engine.go` (Analyze function signature)
- Modify: `backend/internal/api/handlers.go` (HandleAnalyze + HandleInsights)

The `Analyze` function passes `nil, 0` to `findNoiseAlerts` right now. We update its signature to accept `eventCounts` and `integrationCount`, then fetch event counts in both handler call sites.

- [ ] **Step 1: Update Analyze signature in engine.go**

Find the `Analyze` function declaration (line ~197):

Old:
```go
func Analyze(alerts []*models.AlertDef) *models.SimilarityResult {
```

New:
```go
// Analyze performs full similarity analysis on a set of alert definitions.
// eventCounts maps alertID → 30-day trigger count; pass nil to skip behavioral noise detection.
// integrationCount is the total number of integrations in the org (used for structural noise reason text).
func Analyze(alerts []*models.AlertDef, eventCounts map[string]int, integrationCount int) *models.SimilarityResult {
```

Find the `findNoiseAlerts` call inside `Analyze` (around line 239):

Old (the temporary stub from Task 3):
```go
noiseAlerts := findNoiseAlerts(vectors, alerts, nil, 0)
```

New (pass through real data):
```go
noiseAlerts := findNoiseAlerts(vectors, alerts, eventCounts, integrationCount)
```

- [ ] **Step 2: Update HandleAnalyze in handlers.go**

Find the section in `HandleAnalyze` where similarity analysis runs (currently line ~209):

Old:
```go
// Run similarity analysis.
alertInsights := similarity.Analyze(alerts)
```

New — add event count fetch above it, and pass to Analyze:

```go
// Fetch 30-day trigger counts for behavioral noise detection.
// Falls back to structural-only if the EventsService call fails.
var eventCounts map[string]int
{
	alertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		alertIDs[i] = a.ID
	}
	cxClient, cxErr := coralogix.NewClient(clientCfg.Region, clientCfg.APIKey)
	if cxErr != nil {
		log.Printf("WARN [noise] coralogix client init: %v — structural-only noise", cxErr)
	} else {
		counts, countErr := cxClient.FetchAlertEventCounts(ctx, alertIDs, 30)
		cxClient.Close()
		if countErr != nil {
			log.Printf("WARN [noise] event counts unavailable: %v — structural-only noise", countErr)
		} else {
			eventCounts = counts
		}
	}
}

// Run similarity analysis.
alertInsights := similarity.Analyze(alerts, eventCounts, len(integrations))
```

- [ ] **Step 3: Update HandleInsights in handlers.go**

Find the `similarity.Analyze` call in `HandleInsights` (currently line ~404):

Old:
```go
alertInsights := similarity.Analyze(alerts)
```

New — fetch event counts here too (for accurate cache key + noise detection):

```go
// Fetch event counts for behavioral noise (structural-only fallback on error).
var insightsEventCounts map[string]int
{
	alertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		alertIDs[i] = a.ID
	}
	// clientCfg is available from the lookup above (line ~378)
	cxClient, cxErr := coralogix.NewClient(clientCfg.Region, clientCfg.APIKey)
	if cxErr == nil {
		counts, countErr := cxClient.FetchAlertEventCounts(ctx, alertIDs, 30)
		cxClient.Close()
		if countErr == nil {
			insightsEventCounts = counts
		}
	}
}

// Similarity analysis is fast (< 1s) and required for the cache key + LLM prompt.
// Pass 0 for integrationCount — Monday not fetched in this path; structural reason
// text won't include org integration count but all other noise signals are accurate.
alertInsights := similarity.Analyze(alerts, insightsEventCounts, 0)
```

- [ ] **Step 4: Verify build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no errors. If there are any other call sites for `similarity.Analyze` (search: `grep -rn "similarity\.Analyze" backend/`), update them to pass `nil, 0`.

- [ ] **Step 5: Run all backend tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add backend/internal/similarity/engine.go backend/internal/api/handlers.go
git commit -m "feat(api): wire event counts into similarity.Analyze for behavioral noise detection"
```

---

### Task 5: Update Frontend Types

**Files:**
- Modify: `frontend/src/types/index.ts`

Add the two new optional fields to the `NoiseAlert` interface.

- [ ] **Step 1: Update NoiseAlert interface**

In `frontend/src/types/index.ts`, replace the `NoiseAlert` interface (currently lines 88–92):

Old:
```ts
export interface NoiseAlert {
  name: string;
  missing_features: string[];
  reason?: string;
}
```

New:
```ts
export interface NoiseAlert {
  name: string;
  missing_features: string[];
  reason?: string;
  /** 0 when behavioral data unavailable or not behaviorally noisy */
  trigger_count?: number;
  /** "behavioral" | "structural" | "both" */
  noise_type?: string;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run build 2>&1 | tail -15
```

Expected: no type errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add frontend/src/types/index.ts
git commit -m "feat(types): add trigger_count and noise_type to NoiseAlert interface"
```

---

### Task 6: Frontend Noise Card UI — Badge and Trigger Count Chip

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`
- Modify: `frontend/src/App.css`

Add a `noise_type` badge (Behavioral = red, Structural = amber, Both = dark red) and a `trigger_count` chip (`Fired N×`) to each noise card's collapsed header. The existing `reason` string already renders via `insight-card-noise-preview` — no change needed there.

- [ ] **Step 1: Add CSS classes to App.css**

Find the end of the `AlertInsights`-related CSS in `frontend/src/App.css`. Add these new classes (the spec defines the exact values):

```css
/* ── Noise card type badges ───────────────────────────────────────── */
.noise-type-badge {
  font-family: var(--font-mono);
  font-size: 0.6rem;
  padding: 2px 6px;
  border-radius: 3px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

.noise-type-badge--behavioral {
  background: #7f1d1d;
  color: #fca5a5;
}

.noise-type-badge--structural {
  background: #78350f;
  color: #fcd34d;
}

.noise-type-badge--both {
  background: #450a0a;
  color: #fca5a5;
}

.noise-trigger-count {
  font-family: var(--font-mono);
  font-size: 0.6rem;
  color: var(--text-dim);
  flex-shrink: 0;
}
```

- [ ] **Step 2: Add noiseTypeLabel helper in AlertInsights.tsx**

Open `frontend/src/components/AlertInsights.tsx`. Add a helper function near the top (before the component, after imports):

```tsx
function noiseTypeLabel(noiseType?: string): string {
  switch (noiseType) {
    case 'behavioral': return 'Behavioral';
    case 'structural': return 'Structural';
    case 'both':       return 'Both';
    default:           return 'Structural'; // fallback for legacy data
  }
}
```

- [ ] **Step 3: Add badge and chip to noise card header**

Locate the noise card header render section in `AlertInsights.tsx`. The collapsed header currently shows the alert name and optionally the reason preview. Add the badge and chip alongside the alert name.

Find the section rendering noise cards (search for `insight-card-noise` or `noise.name`). The header of each noise card (the clickable toggle row) should be updated to include the badge.

The exact JSX to add (insert after the alert name span, before the chevron/toggle button, inside the card header `div`):

```tsx
<span className={`noise-type-badge noise-type-badge--${noise.noise_type ?? 'structural'}`}>
  {noiseTypeLabel(noise.noise_type)}
</span>
{(noise.trigger_count ?? 0) > 0 && (
  <span className="noise-trigger-count">Fired {noise.trigger_count}×</span>
)}
```

Read the current noise card JSX structure first (`grep -n "noise" frontend/src/components/AlertInsights.tsx | head -30`) to find the exact location, then insert after the name.

- [ ] **Step 4: Run the dev server and verify visually**

```bash
cd /Users/aviral.baloni/Desktop/claude && npm run dev --prefix frontend
```

Load the app in a browser, analyze a client, go to the Noise tab. Verify:
- Each noise card shows a colored badge (Behavioral / Structural / Both)
- Cards with `trigger_count > 0` show a `Fired N×` chip
- Expanding the card still works (accordion not broken)
- Reason text still shows in the preview (not duplicated)
- Badge colors: Behavioral = dark red text on dark red bg, Structural = amber text on brown bg

If no real data has behavioral noise yet (event counts unavailable in dev), the badge will default to "Structural" — this is correct.

- [ ] **Step 5: Verify TypeScript build is clean**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run build 2>&1 | tail -15
```

Expected: no type errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add frontend/src/components/AlertInsights.tsx frontend/src/App.css
git commit -m "feat(ui): add noise type badge and trigger count chip to noise cards"
```

---

## Self-Review Against Spec

### Spec Coverage Check

| Spec requirement | Task |
|---|---|
| Vendor-covered alerts excluded from noise | Task 3 — `alert.Features.VendorCovered` check |
| Building block alerts excluded | Task 3 — `alert.Labels["flow_alert"] == "building block"` |
| Non-security alerts excluded | Task 3 — `!alert.Features.IsSecurityAlert` |
| Flow alerts skip structural, behavioral applies | Task 3 — `isFlowAlert` gates structural only |
| Behavioral signal: `trigger_count > 20` in 30d | Task 3 — `behavioralNoiseThreshold = 20` |
| Structural: unscoped + no entity + high-volume type | Task 3 — `isUnscoped && noEntity && isHighVolumeType` |
| Org amplifier ≥ 10 integrations in reason text | Task 3 — `buildNoiseReason` |
| `FetchAlertEventCounts` in client.go | Task 2 |
| `TriggerCount` and `NoiseType` in model | Task 1 |
| Handler fetches event counts before `Analyze` | Task 4 |
| Graceful degradation on API failure | Task 4 — `eventCounts = nil` on error |
| Frontend `trigger_count?` and `noise_type?` types | Task 5 |
| Frontend badge variants (behavioral/structural/both) | Task 6 |
| Frontend `Fired N×` chip | Task 6 |
| Specific reason text (not generic templates) | Task 3 — `buildNoiseReason` |
| `behavioralNoiseThreshold = 20` named constant | Task 3 |

### No Placeholders
- All code blocks contain complete, compilable code
- All test cases include assertions
- All commands include expected output

### Type Consistency
- `findNoiseAlerts(vectors, alerts, eventCounts, integrationCount)` — signature consistent across Task 3 (definition) and Tasks 3, 4 (call sites)
- `Analyze(alerts, eventCounts, integrationCount)` — signature consistent across Task 4 (definition) and Task 4 (call sites in handlers)
- `models.NoiseAlert{TriggerCount: ..., NoiseType: ...}` — fields added in Task 1, used in Task 3, typed in Task 5
- `noiseTypeLabel`, `noiseTypeString`, `buildNoiseReason`, `buildMissingFeatures` — all defined in Task 3, used only in Task 3/6

### One Gap Fixed
The spec mentions that `FetchAlertEventCounts` response format needs verification against the real API. The plan addresses this in Task 2 Step 3 with an explicit note in the struct comment: "Verify against real API output if counts are always 0."
