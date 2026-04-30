# Vendor-Managed Logs — Recommendations Design

**Date:** 2026-04-30
**Status:** Approved

## Summary

`EnrichActionable()` passes all client integrations to Claude without indicating which ones are vendor-managed. Claude then generates recommendations like "add scoped detection on CrowdStrike" for integrations whose alerts are entirely pre-built by the vendor — recommendations the customer cannot act on. The fix derives vendor-managed status from existing alert data (100% of matched alerts are `VendorCovered=true`), filters those integrations out of the enrichment prompt, and shows an informational note in the Gaps tab when all integrations are vendor-managed.

---

## Scope

**In scope:**
- `VendorCoveredCount` field on `monday.Integration` and `models.IntegrationInfo`
- Vendor-managed classification and filtering in `handlers.go` before `EnrichActionable`
- `AllIntegrationsVendorManaged bool` flag on `models.InsightsReport`
- Frontend notice in the Gaps tab when the flag is true

**Out of scope:**
- Changing how `isVendorCovered()` detects individual vendor alerts
- Changing the `weak_detection_quality` MITRE coverage logic
- Any change to the `EnrichActionable` system prompt text
- Partial vendor-managed integrations (≥1 custom alert → treated as customer-managed)

---

## Design

### Vendor-managed definition

An integration is vendor-managed when:
```
AlertCount > 0 AND VendorCoveredCount == AlertCount
```

Integrations with `AlertCount == 0` are NOT vendor-managed — they remain in the customer-managed list as "missing source" candidates.

`allIntegrationsVendorManaged` is true only when `len(integrationInfos) > 0` AND every integration with alerts is vendor-managed. An account with no integrations does not trigger the flag.

---

### Part 1 — Data model changes

**File:** `backend/internal/monday/client.go`

Add to `Integration` struct:
```go
type Integration struct {
    Name               string `json:"name"`
    Application        string `json:"application"`
    Subsystem          string `json:"subsystem"`
    Status             string `json:"status"`
    AlertCount         int    `json:"alert_count"`
    VendorCoveredCount int    `json:"vendor_covered_count,omitempty"`  // new
}
```

**File:** `backend/internal/models/models.go`

Add `VendorCoveredCount` to `IntegrationInfo`:
```go
type IntegrationInfo struct {
    Name               string `json:"name"`
    Application        string `json:"application"`
    Subsystem          string `json:"subsystem"`
    AlertCount         int    `json:"alert_count"`
    VendorCoveredCount int    `json:"vendor_covered_count,omitempty"`  // new
}
```

Add `AllIntegrationsVendorManaged` to `InsightsReport` (wherever the struct is defined):
```go
AllIntegrationsVendorManaged bool `json:"all_integrations_vendor_managed,omitempty"`
```

---

### Part 2 — Backend counting and filtering

**File:** `backend/internal/merge/engine.go`

In `CountAlertsByIntegration`, count vendor-covered matches in the same loop:
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

**File:** `backend/internal/api/handlers.go`

When building `integrationInfos`, copy `VendorCoveredCount`. Then split before calling `EnrichActionable`:
```go
var customerManaged []models.IntegrationInfo
allVendorManaged := len(integrationInfos) > 0
for _, info := range integrationInfos {
    isVendorManaged := info.AlertCount > 0 && info.VendorCoveredCount == info.AlertCount
    if !isVendorManaged {
        allVendorManaged = false
        customerManaged = append(customerManaged, info)
    }
}
// Pass customerManaged (not integrationInfos) to EnrichActionable
// Set ir.AllIntegrationsVendorManaged = allVendorManaged
```

---

### Part 3 — Frontend notice

**File:** `frontend/src/types/index.ts`

Add to `InsightsReport` interface:
```typescript
all_integrations_vendor_managed?: boolean;
```

**File:** `frontend/src/components/AlertInsights.tsx`

At the top of the Gaps tab render, before actionable sections:
```tsx
{effectiveReport?.all_integrations_vendor_managed && (
  <div className="vendor-managed-notice">
    All log sources are vendor-managed. Improvement recommendations require
    at least one customer-controlled integration.
  </div>
)}
```

**File:** `frontend/src/App.css`

```css
.vendor-managed-notice {
  border-left: 3px solid var(--accent);
  padding: 8px 12px;
  font-size: 0.85rem;
  opacity: 0.8;
  margin-bottom: 12px;
}
```

---

## Error Handling

No new error paths. If `EnrichActionable` receives an empty `customerManaged` list, Claude returns all-empty arrays per its existing prompt instructions ("use [] if nothing applies"). The notice explains the empty state to the user.

---

## Tests

| Test | What it verifies |
|------|-----------------|
| `TestCountAlertsByIntegration_vendorCoveredCount` | Integration matched by 2 alerts (1 vendor-covered, 1 not) → `VendorCoveredCount == 1`, `AlertCount == 2` |
| `TestCountAlertsByIntegration_allVendorCovered` | Integration matched by 2 alerts both vendor-covered → `VendorCoveredCount == 2 == AlertCount` |
| `TestCountAlertsByIntegration_noAlerts` | Integration with no matching alerts → `VendorCoveredCount == 0`, `AlertCount == 0` |
| Handler-level: `allVendorManaged` flag set correctly | Covered via integration test or handler unit test |

Frontend: visual verification that `.vendor-managed-notice` appears in the Gaps tab when `all_integrations_vendor_managed: true` is present in the report.
