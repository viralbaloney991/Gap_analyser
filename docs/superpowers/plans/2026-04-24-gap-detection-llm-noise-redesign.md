# Gap Detection & LLM Noise Accuracy Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace keyword-based coverage gap detection with MITRE tactic coverage data, and fix the LLM prompt to reliably surface noise alerts it receives.

**Architecture:** Three tasks across two backend files. `analyzeCoverage()` drops its hardcoded keyword array and takes a `*models.MITRECoverageResult`; `Analyze()` gains a fourth parameter that both handler call sites supply; `buildPrompt()` in enrich.go adds noise signal metadata and a mandatory rule forcing `noise_explanations` to be populated.

**Tech Stack:** Go — `similarity`, `insights`, `api/handlers` packages. No frontend changes.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/similarity/engine.go` | Rewrite `analyzeCoverage()`, update `Analyze()` signature, remove dead keyword helpers |
| `backend/internal/similarity/engine_test.go` | Add 3 tests for new `analyzeCoverage`, update existing `Analyze()` call |
| `backend/internal/api/handlers.go` | Pass `mitreCoverage` to `HandleAnalyze`'s `Analyze()` call; add MITRE computation + pass to `HandleInsights`'s `Analyze()` call |
| `backend/internal/insights/enrich.go` | Add noise type/count tags; add mandatory noise_explanations rule |
| `backend/internal/insights/enrich_test.go` | Add 1 test for new noise format |

---

## Task 1: Rewrite `analyzeCoverage` in engine.go

**Files:**
- Modify: `backend/internal/similarity/engine.go`
- Test: `backend/internal/similarity/engine_test.go`

### Context for the implementer

`analyzeCoverage` currently matches alerts against a hardcoded English category list using fuzzy token matching — it misses alerts whose Lucene query uses different terms than their name. We replace it with MITRE tactic coverage data from `*models.MITRECoverageResult`, which already knows exactly which tactics are covered.

`Analyze()` gains a fourth parameter `mitreResult *models.MITRECoverageResult`. The internal call at Step 6 (line ~233) passes it down. The `commonCategories` var, `categoryMatchesTokens()`, and `collectAllTokens()` are removed — they are only used by `analyzeCoverage`.

- [ ] **Step 1: Write three failing tests in `engine_test.go`**

Add this helper and three test functions at the end of `backend/internal/similarity/engine_test.go`:

```go
// makeMITREResult builds a minimal MITRECoverageResult for test use.
func makeMITREResult(tactics map[string]struct{ covered, total int }) *models.MITRECoverageResult {
	breakdown := make(map[string]models.TacticCoverage, len(tactics))
	for name, ct := range tactics {
		pct := 0.0
		if ct.total > 0 {
			pct = float64(ct.covered) / float64(ct.total) * 100.0
		}
		breakdown[name] = models.TacticCoverage{
			TacticName: name,
			Total:      ct.total,
			Covered:    ct.covered,
			Percent:    pct,
		}
	}
	return &models.MITRECoverageResult{
		Summary: models.MITRECoverageSummary{TacticBreakdown: breakdown},
	}
}

func TestAnalyzeCoverage_mitrePrimaryPath(t *testing.T) {
	mitre := makeMITREResult(map[string]struct{ covered, total int }{
		"Reconnaissance":   {0, 9},  // gap
		"Lateral Movement": {0, 5},  // gap
		"Initial Access":   {2, 12}, // thin (16.6% < 25%)
		"Persistence":      {8, 19}, // covered (42%) — no insight
	})
	insights := analyzeCoverage(mitre)

	gapFound, thinFound, keywordFound := false, false, false
	for _, s := range insights {
		if strings.Contains(s, "No alert coverage") {
			gapFound = true
		}
		if strings.Contains(s, "Thin coverage") && strings.Contains(s, "Initial Access") {
			thinFound = true
		}
		if strings.Contains(s, "login anomalies") || strings.Contains(s, "mfa bypass") {
			keywordFound = true
		}
	}
	if !gapFound {
		t.Error("expected gap insight for zero-covered tactics, got none")
	}
	if !thinFound {
		t.Error("expected thin-coverage insight for Initial Access (16%), got none")
	}
	if keywordFound {
		t.Error("keyword-based strings must not appear when mitreResult is non-nil")
	}
}

func TestAnalyzeCoverage_nilMitre_returnsNil(t *testing.T) {
	insights := analyzeCoverage(nil)
	if len(insights) != 0 {
		t.Errorf("expected no insights with nil MITRE result, got %v", insights)
	}
}

func TestAnalyzeCoverage_allTacticsCovered_noInsights(t *testing.T) {
	mitre := makeMITREResult(map[string]struct{ covered, total int }{
		"Persistence":          {8, 19}, // 42% — above threshold
		"Privilege Escalation": {5, 13}, // 38% — above threshold
	})
	insights := analyzeCoverage(mitre)
	if len(insights) != 0 {
		t.Errorf("expected no insights when all tactics above threshold, got %v", insights)
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/similarity/... -run "TestAnalyzeCoverage" -v
```

Expected: compile error — `analyzeCoverage` currently takes `[]featureVector`, not `*models.MITRECoverageResult`.

- [ ] **Step 3: Replace `analyzeCoverage` in `engine.go`**

**3a.** Remove the `commonCategories` var (lines ~122–150 in engine.go):

```go
// DELETE the entire block:
// Common security categories used for gap analysis.
var commonCategories = []string{
    // Identity
    "login anomalies",
    ...
    "insider threat",
}
```

**3b.** Add the `minTacticCoveragePct` constant to the existing `const` block (after `parallelThreshold = 50`):

```go
	// Minimum tactic coverage percentage to avoid a thin-coverage insight.
	minTacticCoveragePct = 25.0
```

**3c.** Replace the `analyzeCoverage` function body (the function currently starts at ~line 913):

```go
// analyzeCoverage generates coverage gap insights from MITRE tactic data.
// Returns nil when mitreResult is nil (callers that cannot supply MITRE data
// simply get no coverage insights).
func analyzeCoverage(mitreResult *models.MITRECoverageResult) []string {
	if mitreResult == nil {
		return nil
	}
	var insights []string
	var gaps []string
	var thin []string

	for _, tc := range mitreResult.Summary.TacticBreakdown {
		if tc.Total == 0 {
			continue
		}
		switch {
		case tc.Covered == 0:
			gaps = append(gaps, tc.TacticName)
		case tc.Percent < minTacticCoveragePct:
			thin = append(thin, fmt.Sprintf("%s (%.0f%%)", tc.TacticName, tc.Percent))
		}
	}

	sort.Strings(gaps)
	sort.Strings(thin)

	if len(gaps) > 0 {
		insights = append(insights, fmt.Sprintf("No alert coverage for: %s", strings.Join(gaps, ", ")))
	}
	for _, t := range thin {
		insights = append(insights, fmt.Sprintf("Thin coverage: %s — consider adding more detections", t))
	}
	return insights
}
```

**3d.** Delete `collectAllTokens` (function at ~line 982–1004) and `categoryMatchesTokens` (function at ~line 1006–1042) — both are now dead code.

- [ ] **Step 4: Update `Analyze()` signature and internal call**

Change the `Analyze` function signature (line ~197) and the Step 6 call (line ~233):

```go
// Analyze performs full similarity analysis on a set of alert definitions.
// eventCounts maps alertID → 30-day trigger count; pass nil to skip behavioral noise detection.
// integrationCount is the total number of integrations in the org.
// mitreResult provides MITRE tactic coverage for gap detection; pass nil to skip coverage insights.
func Analyze(
	alerts []*models.AlertDef,
	eventCounts map[string]int,
	integrationCount int,
	mitreResult *models.MITRECoverageResult,
) *models.SimilarityResult {
```

Inside `Analyze`, change the Step 6 call from:
```go
coverageInsights := analyzeCoverage(vectors)
```
to:
```go
// Step 6: Coverage insights (MITRE-based; nil = no coverage insights).
coverageInsights := analyzeCoverage(mitreResult)
```

- [ ] **Step 5: Fix the existing `Analyze` call in `engine_test.go`**

Line ~100 currently reads:
```go
result := Analyze(alerts, nil, 0)
```

Change to:
```go
result := Analyze(alerts, nil, 0, nil)
```

- [ ] **Step 6: Run all tests to confirm they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/similarity/... -v
```

Expected: all tests pass including the three new `TestAnalyzeCoverage_*` tests. `go build ./...` must also be clean (it will fail until Task 2 updates the handler call sites — that's expected; do `go vet ./internal/similarity/...` instead to verify this package alone is clean).

```bash
go vet ./internal/similarity/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/similarity/engine.go backend/internal/similarity/engine_test.go
git commit -m "feat(similarity): replace keyword gap detection with MITRE tactic coverage

analyzeCoverage() now consumes *models.MITRECoverageResult instead of
fuzzy-matching alert tokens against a hardcoded English category list.
Three-tier output: gap (0 covered), thin (<25%), covered (no insight).
Removes commonCategories, collectAllTokens, categoryMatchesTokens.
Analyze() gains a fourth mitreResult parameter."
```

---

## Task 2: Wire `mitreCoverage` into both handler call sites

**Files:**
- Modify: `backend/internal/api/handlers.go`

### Context for the implementer

`HandleAnalyze` already computes `mitreCoverage := mitre.AnalyzeCoverage(alerts)` at line ~206, before calling `similarity.Analyze()` at line ~220. We just pass it through. `HandleInsights` does not compute MITRE; add a one-line call before `similarity.Analyze()`. The `mitre` package is already imported in handlers.go.

After this task, `go build ./...` will be clean.

- [ ] **Step 1: Update `HandleAnalyze` call site**

Find line ~220 in `backend/internal/api/handlers.go`:

```go
// Before:
alertInsights := similarity.Analyze(alerts, eventCounts, len(integrations))
```

Change to:

```go
// After:
alertInsights := similarity.Analyze(alerts, eventCounts, len(integrations), mitreCoverage)
```

`mitreCoverage` is already the variable computed three lines above (line ~206).

- [ ] **Step 2: Update `HandleInsights` call site**

Find line ~427 in `backend/internal/api/handlers.go`:

```go
// Before:
alertInsights := similarity.Analyze(alerts, insightsEventCounts, 0)
```

Change to:

```go
// Compute MITRE coverage for accurate gap detection. Pure in-memory — no external calls.
insightsMitre := mitre.AnalyzeCoverage(alerts)
alertInsights := similarity.Analyze(alerts, insightsEventCounts, 0, insightsMitre)
```

The `insightsMitre` variable must be inserted on the line immediately before `alertInsights :=`.

- [ ] **Step 3: Build the entire backend**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: all packages pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers.go
git commit -m "feat(api): pass MITRE coverage into similarity.Analyze for both handlers

HandleAnalyze: forward existing mitreCoverage variable to Analyze().
HandleInsights: add mitre.AnalyzeCoverage(alerts) call before Analyze()
so gap insights use real tactic coverage data instead of nil."
```

---

## Task 3: Strengthen noise prompt in `enrich.go`

**Files:**
- Modify: `backend/internal/insights/enrich.go`
- Test: `backend/internal/insights/enrich_test.go`

### Context for the implementer

`buildPrompt()` currently formats each noise alert as:
```
- AlertName: no entities, no data sources — reason text
```

It must now include the noise type and trigger count:
```
- AlertName [behavioral, 45×]: no entities — reason text
- AlertName [structural]: no entities, no data sources — reason text
```

Additionally the STRICT RULES section must mandate that `noise_explanations` is populated. Without this, the LLM drops the field silently even when noise data is provided.

`models.NoiseAlert` fields used:
- `Name` string
- `NoiseType` string — `"behavioral"`, `"structural"`, or `"both"` (may be empty for legacy data)
- `TriggerCount` int — `> 0` only for behavioral noise
- `MissingFeatures []string`
- `Reason` string

- [ ] **Step 1: Write a failing test in `enrich_test.go`**

Add at the end of `backend/internal/insights/enrich_test.go`:

```go
func TestBuildPrompt_noiseTagIncludesTypeAndCount(t *testing.T) {
	result := &models.SimilarityResult{
		NoiseAlerts: []models.NoiseAlert{
			{
				Name:            "Broad Login Alert",
				NoiseType:       "behavioral",
				TriggerCount:    45,
				MissingFeatures: []string{"entities"},
				Reason:          "Fires too frequently",
			},
			{
				Name:      "Generic IAM Alert",
				NoiseType: "structural",
				// TriggerCount 0 — structural only
				MissingFeatures: []string{"data sources", "entities"},
				Reason:          "Unscoped high-volume type",
			},
		},
	}
	prompt := buildPrompt(result, nil)

	if !strings.Contains(prompt, "[behavioral, 45×]") {
		t.Errorf("expected behavioral tag with count in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[structural]") {
		t.Errorf("expected structural tag (no count) in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "noise_explanations MUST contain exactly 2 entries") {
		t.Errorf("expected mandatory noise rule with count=2 in prompt, got:\n%s", prompt)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/insights/... -run TestBuildPrompt_noiseTagIncludesTypeAndCount -v
```

Expected: FAIL — the prompt does not yet contain `[behavioral, 45×]`.

- [ ] **Step 3: Replace the noise section in `buildPrompt`**

In `backend/internal/insights/enrich.go`, replace the noise alerts block (lines ~104–122):

```go
	// Before:
	if len(noiseAlerts) > 0 {
		sb.WriteString("Noisy alerts:\n")
		for _, na := range noiseAlerts {
			line := fmt.Sprintf("- %s", na.Name)
			if len(na.MissingFeatures) > 0 {
				line += fmt.Sprintf(": no %s", strings.Join(na.MissingFeatures, ", no "))
			}
			if na.Reason != "" {
				line += fmt.Sprintf(" — %s", na.Reason)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}
```

With:

```go
	if len(noiseAlerts) > 0 {
		sb.WriteString("Noisy alerts:\n")
		for _, na := range noiseAlerts {
			line := fmt.Sprintf("- %s", na.Name)
			if na.NoiseType != "" {
				if na.TriggerCount > 0 {
					line += fmt.Sprintf(" [%s, %d×]", na.NoiseType, na.TriggerCount)
				} else {
					line += fmt.Sprintf(" [%s]", na.NoiseType)
				}
			}
			if len(na.MissingFeatures) > 0 {
				line += fmt.Sprintf(": no %s", strings.Join(na.MissingFeatures, ", no "))
			}
			if na.Reason != "" {
				line += fmt.Sprintf(" — %s", na.Reason)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}
```

- [ ] **Step 4: Add the mandatory noise rule to STRICT RULES**

In `buildPrompt`, find the STRICT RULES block (lines ~124–129). The block currently ends with `\n\n` before `"Return JSON only"`. Insert the mandatory noise rule between the last existing rule and the closing `\n\n`:

Replace:

```go
	sb.WriteString("STRICT RULES for output:\n")
	sb.WriteString("- Use ONLY alert names, counts, and patterns from the data above — never invent statistics.\n")
	sb.WriteString("- Do NOT reference any client name, company name, or product name (you do not know them).\n")
	sb.WriteString("- Recommendations must describe structural patterns (e.g. 'alerts lacking data source binding'), never specific alert counts you invent.\n\n")
	sb.WriteString("Return JSON only — no prose, no markdown:\n")
	sb.WriteString(`{"summary":"<2-3 sentences>","top_priority":["<3-5 items>"],"strengths":["<2-3 items>"],"recommendations":["<3-5 items>"],"enriched_dups":["<1 sentence each>"],"enriched_gaps":["<1 sentence each>"],"noise_explanations":["<1 sentence each>"]}`)
```

With:

```go
	sb.WriteString("STRICT RULES for output:\n")
	sb.WriteString("- Use ONLY alert names, counts, and patterns from the data above — never invent statistics.\n")
	sb.WriteString("- Do NOT reference any client name, company name, or product name (you do not know them).\n")
	sb.WriteString("- Recommendations must describe structural patterns (e.g. 'alerts lacking data source binding'), never specific alert counts you invent.\n")
	noiseCount := len(result.NoiseAlerts)
	if noiseCount > maxPromptNoise {
		noiseCount = maxPromptNoise
	}
	if noiseCount > 0 {
		sb.WriteString(fmt.Sprintf("- noise_explanations MUST contain exactly %d entries — one per noisy alert listed above. Never omit or truncate this field.\n", noiseCount))
	}
	sb.WriteString("\n")
	sb.WriteString("Return JSON only — no prose, no markdown:\n")
	sb.WriteString(`{"summary":"<2-3 sentences>","top_priority":["<3-5 items>"],"strengths":["<2-3 items>"],"recommendations":["<3-5 items>"],"enriched_dups":["<1 sentence each>"],"enriched_gaps":["<1 sentence each>"],"noise_explanations":["<mandatory — one entry per noisy alert, explain the behavioral or structural signal>"]}`)
```

- [ ] **Step 5: Run all insight tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/insights/... -v
```

Expected: all 6 tests pass including `TestBuildPrompt_noiseTagIncludesTypeAndCount`.

- [ ] **Step 6: Run the full test suite**

```bash
go test ./...
```

Expected: all packages pass.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/insights/enrich.go backend/internal/insights/enrich_test.go
git commit -m "fix(insights): add noise type/count tags and mandatory noise_explanations rule

buildPrompt() now emits '[behavioral, 45×]' or '[structural]' tags on
each noise alert line. Adds a STRICT RULE mandating that noise_explanations
contains exactly N entries when N noisy alerts are provided, preventing
the LLM from silently omitting the field."
```
