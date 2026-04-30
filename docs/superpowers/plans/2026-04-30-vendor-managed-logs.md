# Vendor-Managed Logs Recommendations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent `EnrichActionable()` from generating improvement recommendations for integrations whose alerts are entirely vendor-managed (all matched alerts have `VendorCovered=true`), and show an informational note in the Gaps tab when all integrations are vendor-managed.

**Architecture:** Three isolated changes: (1) add `VendorCoveredCount` to the Integration and IntegrationInfo structs and populate it in `CountAlertsByIntegration`; (2) filter vendor-managed integrations before passing to `EnrichActionable` in `runInsightsBackground` and set `AllIntegrationsVendorManaged` on the InsightsReport; (3) read the flag in the frontend and render an informational callout in the Gaps tab.

**Tech Stack:** Go (backend), React + TypeScript (frontend).

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/monday/client.go` | Add `VendorCoveredCount int` to `Integration` struct |
| `backend/internal/models/models.go` | Add `VendorCoveredCount int` to `IntegrationInfo`; add `AllIntegrationsVendorManaged bool` to `InsightsReport` |
| `backend/internal/merge/engine.go` | Count vendor-covered alerts per integration in `CountAlertsByIntegration` |
| `backend/internal/merge/engine_test.go` | New file — three tests for `VendorCoveredCount` |
| `backend/internal/api/handlers.go` | Copy `VendorCoveredCount` into `IntegrationInfo`; filter + flag in `runInsightsBackground` |
| `frontend/src/types/index.ts` | Add `all_integrations_vendor_managed?: boolean` to `InsightsReport` |
| `frontend/src/components/AlertInsights.tsx` | Add vendor-managed notice JSX in Gaps tab |
| `frontend/src/App.css` | Add `.vendor-managed-notice` CSS rule |

---

## Task 1: Struct fields + counting logic

**Files:**
- Modify: `backend/internal/monday/client.go:22-28`
- Modify: `backend/internal/models/models.go:196-201`
- Create: `backend/internal/merge/engine_test.go`
- Modify: `backend/internal/merge/engine.go:21-28`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/merge/engine_test.go`:

```go
package merge

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
)

func TestCountAlertsByIntegration_vendorCoveredCount(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "AWS CloudTrail", Application: "cloudtrail"},
	}
	alerts := []*models.AlertDef{
		{Name: "cloudtrail event", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: false}},
		{Name: "cloudtrail alert", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: true}},
	}
	result := CountAlertsByIntegration(integrations, alerts)
	if result[0].AlertCount != 2 {
		t.Errorf("want AlertCount=2, got %d", result[0].AlertCount)
	}
	if result[0].VendorCoveredCount != 1 {
		t.Errorf("want VendorCoveredCount=1, got %d", result[0].VendorCoveredCount)
	}
}

func TestCountAlertsByIntegration_allVendorCovered(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "Okta", Application: "okta"},
	}
	alerts := []*models.AlertDef{
		{Name: "okta event 1", Features: models.AlertFeatures{DataSources: []string{"okta"}, VendorCovered: true}},
		{Name: "okta event 2", Features: models.AlertFeatures{DataSources: []string{"okta"}, VendorCovered: true}},
	}
	result := CountAlertsByIntegration(integrations, alerts)
	if result[0].AlertCount != 2 {
		t.Errorf("want AlertCount=2, got %d", result[0].AlertCount)
	}
	if result[0].VendorCoveredCount != 2 {
		t.Errorf("want VendorCoveredCount=2, got %d", result[0].VendorCoveredCount)
	}
}

func TestCountAlertsByIntegration_noAlerts(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "Splunk", Application: "splunk"},
	}
	alerts := []*models.AlertDef{
		{Name: "cloudtrail alert", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: true}},
	}
	result := CountAlertsByIntegration(integrations, alerts)
	if result[0].AlertCount != 0 {
		t.Errorf("want AlertCount=0, got %d", result[0].AlertCount)
	}
	if result[0].VendorCoveredCount != 0 {
		t.Errorf("want VendorCoveredCount=0, got %d", result[0].VendorCoveredCount)
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd backend && go test ./internal/merge/... -run "TestCountAlertsByIntegration" -v 2>&1
```

Expected: FAIL — `result[0].VendorCoveredCount` is always 0 (field doesn't exist yet, or isn't populated).

- [ ] **Step 3: Add `VendorCoveredCount` to `monday.Integration`**

In `backend/internal/monday/client.go`, replace:

```go
// Integration represents a single onboarded log source from Monday.
type Integration struct {
	Name        string `json:"name"`
	Application string `json:"application"`
	Subsystem   string `json:"subsystem"`
	Status      string `json:"status"`
	AlertCount  int    `json:"alert_count"`
}
```

With:

```go
// Integration represents a single onboarded log source from Monday.
type Integration struct {
	Name               string `json:"name"`
	Application        string `json:"application"`
	Subsystem          string `json:"subsystem"`
	Status             string `json:"status"`
	AlertCount         int    `json:"alert_count"`
	VendorCoveredCount int    `json:"vendor_covered_count,omitempty"`
}
```

- [ ] **Step 4: Add `VendorCoveredCount` to `models.IntegrationInfo`**

In `backend/internal/models/models.go`, replace:

```go
type IntegrationInfo struct {
	Name        string `json:"name"`
	Application string `json:"application"`
	Subsystem   string `json:"subsystem"`
	AlertCount  int    `json:"alert_count"`
}
```

With:

```go
type IntegrationInfo struct {
	Name               string `json:"name"`
	Application        string `json:"application"`
	Subsystem          string `json:"subsystem"`
	AlertCount         int    `json:"alert_count"`
	VendorCoveredCount int    `json:"vendor_covered_count,omitempty"`
}
```

- [ ] **Step 5: Populate `VendorCoveredCount` in `CountAlertsByIntegration`**

In `backend/internal/merge/engine.go`, replace:

```go
		count := 0
		for _, alert := range alerts {
			if alertMatchesIntegration(alert, apps, subs, integName) {
				count++
			}
		}
		result[i].AlertCount = count
```

With:

```go
		count := 0
		vendorCount := 0
		for _, alert := range alerts {
			if alertMatchesIntegration(alert, apps, subs, integName) {
				count++
				if alert.Features.VendorCovered {
					vendorCount++
				}
			}
		}
		result[i].AlertCount = count
		result[i].VendorCoveredCount = vendorCount
```

- [ ] **Step 6: Run tests — confirm they pass**

```bash
cd backend && go test ./internal/merge/... -run "TestCountAlertsByIntegration" -v 2>&1
```

Expected: all three PASS.

- [ ] **Step 7: Run full backend build — no regressions**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/monday/client.go \
        backend/internal/models/models.go \
        backend/internal/merge/engine.go \
        backend/internal/merge/engine_test.go
git commit -m "feat(vendor-managed): count vendor-covered alerts per integration"
```

---

## Task 2: Handler filtering + InsightsReport flag

**Files:**
- Modify: `backend/internal/models/models.go:160-170` (InsightsReport)
- Modify: `backend/internal/api/handlers.go:222-234` (integrationInfos build)
- Modify: `backend/internal/api/handlers.go:354-373` (runInsightsBackground)

- [ ] **Step 1: Add `AllIntegrationsVendorManaged` to `InsightsReport`**

In `backend/internal/models/models.go`, replace:

```go
// InsightsReport is the LLM-generated analyst report for a SimilarityResult.
type InsightsReport struct {
	Model             string                   `json:"model,omitempty"`
	Summary           string                   `json:"summary"`
	TopPriority       []string                 `json:"top_priority"`
	Strengths         []string                 `json:"strengths"`
	Recommendations   []string                 `json:"recommendations"`
	EnrichedDups      []string                 `json:"enriched_dups"`
	GapCategories     GapCategories            `json:"gap_categories"`
	ActionableGaps    *ActionableGapCategories `json:"actionable_gaps,omitempty"`
	NoiseExplanations []string                 `json:"noise_explanations"`
}
```

With:

```go
// InsightsReport is the LLM-generated analyst report for a SimilarityResult.
type InsightsReport struct {
	Model                        string                   `json:"model,omitempty"`
	Summary                      string                   `json:"summary"`
	TopPriority                  []string                 `json:"top_priority"`
	Strengths                    []string                 `json:"strengths"`
	Recommendations              []string                 `json:"recommendations"`
	EnrichedDups                 []string                 `json:"enriched_dups"`
	GapCategories                GapCategories            `json:"gap_categories"`
	ActionableGaps               *ActionableGapCategories `json:"actionable_gaps,omitempty"`
	NoiseExplanations            []string                 `json:"noise_explanations"`
	AllIntegrationsVendorManaged bool                     `json:"all_integrations_vendor_managed,omitempty"`
}
```

- [ ] **Step 2: Copy `VendorCoveredCount` when building `integrationInfos`**

In `backend/internal/api/handlers.go`, replace:

```go
	integrationInfos := make([]models.IntegrationInfo, len(matched))
	withAlerts := 0
	for i, m := range matched {
		integrationInfos[i] = models.IntegrationInfo{
			Name:        m.Name,
			Application: m.Application,
			Subsystem:   m.Subsystem,
			AlertCount:  m.AlertCount,
		}
		if m.AlertCount > 0 {
			withAlerts++
		}
	}
```

With:

```go
	integrationInfos := make([]models.IntegrationInfo, len(matched))
	withAlerts := 0
	for i, m := range matched {
		integrationInfos[i] = models.IntegrationInfo{
			Name:               m.Name,
			Application:        m.Application,
			Subsystem:          m.Subsystem,
			AlertCount:         m.AlertCount,
			VendorCoveredCount: m.VendorCoveredCount,
		}
		if m.AlertCount > 0 {
			withAlerts++
		}
	}
```

- [ ] **Step 3: Filter integrations and set flag in `runInsightsBackground`**

In `backend/internal/api/handlers.go`, replace:

```go
		actionable, aErr := insights.EnrichActionable(bgCtx, ir.GapCategories, integrations, insightsProvider)
		if aErr != nil {
			log.Printf("WARN [insights-bg] client=%s actionable enrich: %v", client, aErr)
		}
		ir.ActionableGaps = actionable
```

With:

```go
		var customerManaged []models.IntegrationInfo
		allVendorManaged := len(integrations) > 0
		for _, info := range integrations {
			isVendorManaged := info.AlertCount > 0 && info.VendorCoveredCount == info.AlertCount
			if !isVendorManaged {
				allVendorManaged = false
				customerManaged = append(customerManaged, info)
			}
		}
		ir.AllIntegrationsVendorManaged = allVendorManaged

		actionable, aErr := insights.EnrichActionable(bgCtx, ir.GapCategories, customerManaged, insightsProvider)
		if aErr != nil {
			log.Printf("WARN [insights-bg] client=%s actionable enrich: %v", client, aErr)
		}
		ir.ActionableGaps = actionable
```

- [ ] **Step 4: Build check**

```bash
cd backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 5: Run all backend tests**

```bash
cd backend && go test ./... 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/models/models.go \
        backend/internal/api/handlers.go
git commit -m "feat(vendor-managed): filter vendor-managed integrations from actionable enrichment"
```

---

## Task 3: Frontend informational notice

**Files:**
- Modify: `frontend/src/types/index.ts:137-147`
- Modify: `frontend/src/App.css` (after `.state-empty` block, around line 278)
- Modify: `frontend/src/components/AlertInsights.tsx:539-547`

- [ ] **Step 1: Add flag to `InsightsReport` TypeScript interface**

In `frontend/src/types/index.ts`, replace:

```typescript
export interface InsightsReport {
  model?: string;
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  gap_categories: GapCategories;
  actionable_gaps?: ActionableGapCategories;
  noise_explanations?: string[];
}
```

With:

```typescript
export interface InsightsReport {
  model?: string;
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  gap_categories: GapCategories;
  actionable_gaps?: ActionableGapCategories;
  noise_explanations?: string[];
  all_integrations_vendor_managed?: boolean;
}
```

- [ ] **Step 2: Add `.vendor-managed-notice` CSS rule**

In `frontend/src/App.css`, after the `.state-empty__body` line (around line 278), add:

```css
.vendor-managed-notice {
  border-left: 3px solid var(--accent);
  padding: 8px 12px;
  font-size: 0.85rem;
  opacity: 0.8;
  margin-bottom: 12px;
}
```

- [ ] **Step 3: Add notice JSX in the Gaps tab**

In `frontend/src/components/AlertInsights.tsx`, replace:

```tsx
            ) : gapCount > 0 ? (
              <>
                {renderGapSection('Environment Cleanup', effectiveReport?.gap_categories.environment_cleanup)}
```

With:

```tsx
            ) : gapCount > 0 ? (
              <>
                {effectiveReport?.all_integrations_vendor_managed && (
                  <div className="vendor-managed-notice">
                    All log sources are vendor-managed. Improvement recommendations require
                    at least one customer-controlled integration.
                  </div>
                )}
                {renderGapSection('Environment Cleanup', effectiveReport?.gap_categories.environment_cleanup)}
```

- [ ] **Step 4: TypeScript build check**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: no TypeScript errors, build succeeds.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/index.ts \
        frontend/src/components/AlertInsights.tsx \
        frontend/src/App.css
git commit -m "feat(vendor-managed): show notice in Gaps tab when all sources are vendor-managed"
```

---

## Self-Review

**Spec coverage:**
- ✅ `VendorCoveredCount` on `monday.Integration` → Task 1 Step 3
- ✅ `VendorCoveredCount` on `models.IntegrationInfo` → Task 1 Step 4
- ✅ Count vendor-covered in `CountAlertsByIntegration` → Task 1 Step 5
- ✅ `AllIntegrationsVendorManaged bool` on `InsightsReport` → Task 2 Step 1
- ✅ Copy `VendorCoveredCount` into `integrationInfos` → Task 2 Step 2
- ✅ Filter vendor-managed integrations before `EnrichActionable` → Task 2 Step 3
- ✅ Set `ir.AllIntegrationsVendorManaged` → Task 2 Step 3
- ✅ 100% threshold: `AlertCount > 0 && VendorCoveredCount == AlertCount` → Task 2 Step 3
- ✅ Zero-alert integrations stay in customer-managed list → Task 2 Step 3 (only `AlertCount > 0` integrations are filtered)
- ✅ `all_integrations_vendor_managed?: boolean` on TS `InsightsReport` → Task 3 Step 1
- ✅ `.vendor-managed-notice` CSS rule → Task 3 Step 2
- ✅ Notice JSX in Gaps tab → Task 3 Step 3
- ✅ Tests: `vendorCoveredCount`, `allVendorCovered`, `noAlerts` → Task 1 Step 1

**Placeholder scan:** None found.

**Type consistency:** `VendorCoveredCount int` defined in Task 1 and referenced in Task 2 Step 2. `AllIntegrationsVendorManaged bool` defined in Task 2 Step 1, set in Task 2 Step 3, read in Task 3 Steps 1 and 3. `all_integrations_vendor_managed` JSON tag matches TypeScript field name throughout.
