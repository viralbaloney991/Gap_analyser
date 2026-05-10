# Suggestions & Advanced Use Cases Bug Fixes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two backend bugs: (1) the suggestions endpoint passes vendor-managed integrations (e.g. CrowdStrike) as available log sources to the LLM, causing it to suggest customer-built detections on vendor-managed data; (2) the advanced use cases gap category is never populated because the LLM prompt criterion is too narrow.

**Architecture:** Task 1 extracts a testable `buildSuggestionLogSources` helper from `HandleSuggestions` in `handlers.go` that calls the existing `merge.CountAlertsByIntegration` to get vendor coverage, then filters out fully vendor-managed integrations and vendor-covered alert data sources before passing to the LLM. Task 2 widens the `advanced_use_cases` criterion in the `gapAnalysisSystemPrompt` constant in `enrich.go` and adds a parse-path test to verify populated items round-trip correctly.

**Tech Stack:** Go (backend only — no frontend changes)

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/api/handlers.go` | Extract `buildSuggestionLogSources`; call it in `HandleSuggestions` instead of inline loop |
| `backend/internal/api/suggestions_filter_test.go` | New: TDD tests for `buildSuggestionLogSources` |
| `backend/internal/insights/enrich.go` | Widen `advanced_use_cases` line in `gapAnalysisSystemPrompt` |
| `backend/internal/insights/enrich_test.go` | Add `TestParseGapCategoriesResponse_AdvancedUseCases_Populated` |

---

## Task 1: Filter Vendor-Managed Log Sources from Suggestions

**Files:**
- Create: `backend/internal/api/suggestions_filter_test.go`
- Modify: `backend/internal/api/handlers.go` — extract helper, use in HandleSuggestions

The `HandleSuggestions` handler (around line 800–825 of `handlers.go`) builds a `logSources` slice from Monday integrations and alert data sources. Currently it includes ALL integrations regardless of vendor coverage. After `coralogix.ExtractFeatures(alerts, nil)` is already called, `alert.Features.VendorCovered` is populated. We have `merge.CountAlertsByIntegration` (already imported) to get `VendorCoveredCount` per integration.

### Step 1: Write the failing test

Create `backend/internal/api/suggestions_filter_test.go`:

```go
package api

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
)

func TestBuildSuggestionLogSources_ExcludesVendorManagedIntegrations(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "CrowdStrike", Application: "crowdstrike", Subsystem: ""},
		{Name: "Okta",        Application: "okta",        Subsystem: ""},
	}
	alerts := []*models.AlertDef{
		// CrowdStrike alerts — all vendor-covered
		{Name: "CS - Malware", Features: models.AlertFeatures{DataSources: []string{"crowdstrike"}, VendorCovered: true}},
		{Name: "CS - Lateral", Features: models.AlertFeatures{DataSources: []string{"crowdstrike"}, VendorCovered: true}},
		// Okta alert — customer-managed
		{Name: "Okta - MFA Fatigue", Features: models.AlertFeatures{DataSources: []string{"okta"}, VendorCovered: false}},
	}

	sources := buildSuggestionLogSources(integrations, alerts)

	for _, s := range sources {
		if s == "CrowdStrike" {
			t.Errorf("CrowdStrike is fully vendor-managed and must not appear in log sources")
		}
	}
	found := false
	for _, s := range sources {
		if s == "Okta" {
			found = true
		}
	}
	if !found {
		t.Errorf("Okta is customer-managed and must appear in log sources; got %v", sources)
	}
}

func TestBuildSuggestionLogSources_IncludesPartiallyVendorManaged(t *testing.T) {
	// Integration has BOTH vendor-covered and customer alerts — should still be included.
	integrations := []monday.Integration{
		{Name: "AWS CloudTrail", Application: "cloudtrail", Subsystem: ""},
	}
	alerts := []*models.AlertDef{
		{Name: "CT - Vendor", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: true}},
		{Name: "CT - Custom", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: false}},
	}

	sources := buildSuggestionLogSources(integrations, alerts)

	found := false
	for _, s := range sources {
		if s == "AWS CloudTrail" {
			found = true
		}
	}
	if !found {
		t.Errorf("AWS CloudTrail has customer-managed alerts and must appear in log sources; got %v", sources)
	}
}

func TestBuildSuggestionLogSources_CapAt30(t *testing.T) {
	integrations := make([]monday.Integration, 40)
	for i := range integrations {
		integrations[i] = monday.Integration{Name: fmt.Sprintf("Source%d", i), Application: fmt.Sprintf("src%d", i)}
	}
	// No alerts, so no vendor coverage — all 40 should be candidates but capped at 30.
	sources := buildSuggestionLogSources(integrations, nil)
	if len(sources) > 30 {
		t.Errorf("expected at most 30 log sources, got %d", len(sources))
	}
}

func TestBuildSuggestionLogSources_ExcludesVendorCoveredAlertDataSources(t *testing.T) {
	// No Monday integrations; only alert data sources as supplement.
	// Vendor-covered alert data sources must not be included.
	integrations := []monday.Integration{}
	alerts := []*models.AlertDef{
		{Name: "Vendor DS", Features: models.AlertFeatures{DataSources: []string{"sentinelone"}, VendorCovered: true}},
		{Name: "Custom DS", Features: models.AlertFeatures{DataSources: []string{"linux_auditd"}, VendorCovered: false}},
	}

	sources := buildSuggestionLogSources(integrations, alerts)

	for _, s := range sources {
		if s == "sentinelone" {
			t.Errorf("sentinelone is vendor-covered and must not appear in supplemental sources")
		}
	}
}
```

Note: `suggestions_filter_test.go` uses `fmt` — add `"fmt"` to its imports.

### Step 2: Run test to verify it fails

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/ -run TestBuildSuggestionLogSources -v
```

Expected: compile error — `buildSuggestionLogSources` undefined.

### Step 3: Implement `buildSuggestionLogSources` helper in handlers.go

Read `handlers.go` to find the inline log-source-building block (approximately lines 797–825). Replace the entire block with a call to the new helper. Add the helper function after `HandleSuggestions`.

Replace the inline block (from `// Extract features...` through the `if len(logSources) > maxLogSources` cap) with:

```go
// Extract features to get data sources (no LLM mapping needed for suggestions endpoint)
coralogix.ExtractFeatures(alerts, nil)

logSources := buildSuggestionLogSources(integrations, alerts)
```

Then add this helper function anywhere in `handlers.go` (e.g. just before `HandleSuggestions` or at the bottom of the file):

```go
// buildSuggestionLogSources returns the non-vendor-managed log source names available
// for a client, capped at 30 to keep LLM prompts lean.
// Monday integrations that are fully vendor-managed (VendorCoveredCount == AlertCount > 0)
// are excluded. Vendor-covered alert data sources are also excluded from the supplement list.
func buildSuggestionLogSources(integrations []monday.Integration, alerts []*models.AlertDef) []string {
	// Enrich integrations with vendor coverage counts using the matched alerts.
	enriched := merge.CountAlertsByIntegration(integrations, alerts)

	logSourceSet := make(map[string]bool)
	var logSources []string

	// Monday integrations are primary — exclude fully vendor-managed ones.
	for _, integ := range enriched {
		isVendorManaged := integ.AlertCount > 0 && integ.VendorCoveredCount == integ.AlertCount
		if integ.Name != "" && !isVendorManaged && !logSourceSet[integ.Name] {
			logSourceSet[integ.Name] = true
			logSources = append(logSources, integ.Name)
		}
	}

	// Supplement with alert data sources — skip vendor-covered alerts.
	for _, alert := range alerts {
		if alert.Features.VendorCovered {
			continue
		}
		for _, ds := range alert.Features.DataSources {
			if !logSourceSet[ds] {
				logSourceSet[ds] = true
				logSources = append(logSources, ds)
			}
		}
	}

	const maxLogSources = 30
	if len(logSources) > maxLogSources {
		logSources = logSources[:maxLogSources]
	}
	return logSources
}
```

### Step 4: Run test to verify it passes

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/ -run TestBuildSuggestionLogSources -v
```

Expected: all 4 tests PASS.

### Step 5: Run full backend test suite

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./...
```

Expected: all PASS.

### Step 6: Commit

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/api/suggestions_filter_test.go backend/internal/api/handlers.go
git commit -m "$(cat <<'EOF'
fix(suggestions): exclude vendor-managed integrations from LLM log source list

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Widen Advanced Use Cases LLM Prompt Criterion

**Files:**
- Modify: `backend/internal/insights/enrich.go` — widen line 41 in `gapAnalysisSystemPrompt`
- Modify: `backend/internal/insights/enrich_test.go` — add populated parse test

The current criterion on line 41 of `enrich.go`:
```
- advanced_use_cases: reason over technique type; only flag when threshold/count alerts exist but no anomaly layer
```

This is too narrow — the LLM almost never fires it. The fix: widen to include any upgrade opportunity (sequence detection, anomaly baselining, multi-stage correlation) and cross-reference other gap categories so it's useful even when the library is large.

### Step 1: Write the failing test (actually a parse-path test)

Add to `backend/internal/insights/enrich_test.go` (before the final closing line):

```go
func TestParseGapCategoriesResponse_AdvancedUseCases_Populated(t *testing.T) {
	raw := `{
		"summary": "Baseline detection is solid.",
		"environment_cleanup": [],
		"no_detection": [],
		"poor_tactic_coverage": [],
		"weak_detection_quality": [],
		"advanced_use_cases": [
			"T1078 (Valid Accounts): only threshold alert exists — add anomaly baseline for off-hours access",
			"T1021 (Remote Services): single count alert — sequence detection across auth+lateral events would improve precision"
		],
		"missing_source_alerts": []
	}`
	report := parseGapCategoriesResponse(raw)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if len(report.GapCategories.AdvancedUseCases) != 2 {
		t.Errorf("advanced_use_cases: want 2 items, got %d: %v",
			len(report.GapCategories.AdvancedUseCases), report.GapCategories.AdvancedUseCases)
	}
	if report.GapCategories.AdvancedUseCases[0] == "" {
		t.Error("first advanced_use_cases item must not be empty string")
	}
}
```

### Step 2: Run test to verify it passes already

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/ -run TestParseGapCategoriesResponse_AdvancedUseCases_Populated -v
```

Expected: PASS — the parse path already works. This test documents the expected behaviour and guards against regressions.

### Step 3: Update the prompt in enrich.go

In `backend/internal/insights/enrich.go`, find line 41:

```
- advanced_use_cases: reason over technique type; only flag when threshold/count alerts exist but no anomaly layer
```

Replace it with:

```
- advanced_use_cases: flag up to 3 high-value upgrade opportunities — e.g. techniques covered only by threshold/count alerts where sequence or anomaly detection would improve precision, techniques in no_detection or weak_detection_quality that are high-value (credential access, lateral movement, exfiltration, persistence), or gaps where a multi-stage correlation across two or more covered techniques would catch an attack chain; always include the technique ID and name
```

The full updated line in context (lines 38–47 of enrich.go, for precision):

```
- poor_tactic_coverage: flag any tactic with pct < 25
- weak_detection_quality: only flag techniques where weak=true in the input
- advanced_use_cases: flag up to 3 high-value upgrade opportunities — e.g. techniques covered only by threshold/count alerts where sequence or anomaly detection would improve precision, techniques in no_detection or weak_detection_quality that are high-value (credential access, lateral movement, exfiltration, persistence), or gaps where a multi-stage correlation across two or more covered techniques would catch an attack chain; always include the technique ID and name
- summary: prose only (no bullet points), 2-4 sentences
```

### Step 4: Run full insights test suite

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/insights/ -v
```

Expected: all PASS (prompt is a string constant — no test exercises it at the LLM level, but all parse-path tests still pass).

### Step 5: Run full backend test suite

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./...
```

Expected: all PASS.

### Step 6: Commit

```bash
cd /Users/aviral.baloni/Desktop/claude && git add backend/internal/insights/enrich.go backend/internal/insights/enrich_test.go
git commit -m "$(cat <<'EOF'
fix(insights): widen advanced_use_cases LLM criterion to surface more upgrade opportunities

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Error Handling

| Case | Behaviour |
|------|-----------|
| All integrations are vendor-managed | `buildSuggestionLogSources` returns `[]string{}` (empty); LLM gets an empty log-source list and returns no suggestions — correct behaviour |
| No Monday integrations fetched (mondayErr) | Falls through to alert data-source supplement; vendor-covered alerts still filtered |
| `advanced_use_cases` LLM returns `[]` | `GapCategories.AdvancedUseCases` is empty slice; `EnrichActionable` skips the bucket silently — no crash |
| Cache hit for suggestions | Cache is keyed on technique + log sources; after this fix, log sources passed to `buildSuggestionCacheKey` will be the filtered list — old cache entries may be returned if the key was built pre-fix, but they expire naturally |
