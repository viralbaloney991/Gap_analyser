# Insights Engine — Quality & Performance Fixes

**Date:** 2026-04-23
**Status:** Approved
**Topic:** Fix 5 known issues in the similarity/insights engine: false-positive groupings, wrong family labels, vague noise explanations, and slow/broken insights LLM

---

## Problem Summary

Five issues reported against the alerts insights engine:

1. **Similarity false positives** — Distinct alerts (e.g. `GuestUserAnomalyEvent` vs `ApiAnomalyEvent`) are scored 100% similar because their keyword-extracted feature vectors are identical. The actual detection logic (Lucene query) is never compared.
2. **Wrong family names** — `deriveFamilyName` picks the most frequent raw action token, producing labels like "Remove Detections" instead of "Tampering Detections".
3. **Merge false positives** — Same root cause as (1): unrelated alerts from the same source/type are incorrectly grouped as merge candidates.
4. **Vague noise explanations** — `NoiseAlert` only records which feature dimensions are empty. The insights LLM prompt sends only alert names for noisy alerts — no context about what is missing or why.
5. **Insights LLM broken/slow** — Current `z-ai/glm5` model times out on every request. Prompt is also unnecessarily verbose, adding latency. No model choice for the user.

---

## Goals

- Fix similarity scoring so structurally similar but logically distinct alerts are not flagged as duplicates or merge candidates
- Family names use semantic security categories (MITRE tactic names or action→category mapping)
- Noise explanations are specific and actionable
- Insights LLM produces results in ~7s (currently ∞/timeout)
- End users can choose between Mistral Small (fast) and Gemma 3 27B (detailed) for insights generation

## Non-Goals

- Changes to the MITRE coverage view
- Changes to the suggestion engine
- Sub-technique nesting in similarity analysis
- Automated test generation (no test suite exists for this subsystem)

---

## Architecture

### Fix 1 & 3: Similarity Scoring — New 9-Dimension Model

**Root cause:** `scorePair` in `engine.go` computes weighted Jaccard on 7 keyword-extracted feature dimensions. Two alerts detecting different Salesforce event types (`GuestUserAnomalyEvent` vs `ApiAnomalyEvent`) extract identical keyword features — same source, entity, action, condition, alertType, groupBy — giving 100% similarity. The discriminating signal is in the Lucene query, which is never scored.

**Additionally:** `jaccardGroupBy` returns `1.0` for two empty groupBy sets — two alerts with no groupBy defined are considered fully similar on the highest-weight dimension.

**Fix:** Add two new dimensions to `featureVector` and `scorePair`, redistribute weights to maintain the invariant `sum = 1.00`, and fix the empty-set behaviour.

**New weight table:**

| Dimension | Old | New | Change |
|---|---|---|---|
| DataSources | 0.15 | 0.15 | — |
| Entities | 0.10 | 0.10 | — |
| Actions | 0.15 | 0.15 | — |
| Conditions | 0.15 | 0.10 | −0.05 (less signal than Lucene) |
| Techniques | 0.10 | 0.10 | — |
| GroupBy | 0.25 | 0.15 | −0.10 (was over-dominant) |
| AlertType | 0.10 | 0.05 | −0.05 (binary, low signal) |
| **LuceneQuery** | — | **0.15** | **new — actual detection logic** |
| **TimeWindow** | — | **0.05** | **new — binary equality bonus** |
| **Total** | **1.00** | **1.00** | ✓ |

**LuceneQuery scoring:** tokenise the Lucene query string (split on whitespace and operators `:()[]{}+-!"`) into a lowercase set; compute standard Jaccard. `eventType:GuestUserAnomalyEvent` and `eventType:ApiAnomalyEvent` share structural tokens but differ on the event-type token, reducing Lucene Jaccard to ~0.5 — pulling the pair below the 0.85 duplicate threshold.

**TimeWindow scoring:** binary match — `if a.timeWindow == b.timeWindow && a.timeWindow != "" { score += 0.05 }`. The `TimeWindow` field is already extracted into `AlertFeatures` but was never added to `featureVector`.

**jaccardGroupBy fix:** change the empty+empty case from `return 1.0` to `return 0.0`. Two alerts with no groupBy keys have nothing in common on that dimension.

**`featureVector` additions:**
```go
type featureVector struct {
    // ... existing fields ...
    luceneQuery map[string]struct{} // new — tokenised Lucene query
    timeWindow  string              // new — from AlertFeatures.TimeWindow
    tactics     []string            // new — from AlertFeatures.Tactics (for family naming)
}
```

**`buildFeatureVectors` additions:**
```go
vectors[i].luceneQuery = tokenizeLucene(coralogix.ExtractLuceneQuery(a.TypeDef))
vectors[i].timeWindow  = a.Features.TimeWindow
vectors[i].tactics     = a.Features.Tactics
```

**`tokenizeLucene`** — new helper in `engine.go`:
```go
func tokenizeLucene(q string) map[string]struct{} {
    re := regexp.MustCompile(`[:()\[\]{}\s+\-!"]+`)
    tokens := re.Split(strings.ToLower(q), -1)
    s := make(map[string]struct{})
    for _, t := range tokens {
        t = strings.TrimSpace(t)
        if len(t) > 1 { // skip single-char noise
            s[t] = struct{}{}
        }
    }
    return s
}
```

---

### Fix 2: Family Naming — Three-Tier Lookup

**Root cause:** `deriveFamilyName` picks the most frequent raw feature token (e.g. `"remove"`) and title-cases it → `"Remove Detections"`. MITRE tactic names on cluster members are never consulted.

**Fix:** Replace the single-path lookup with a three-tier chain:

**Tier 1 — MITRE tactic frequency (primary):**
Collect all tactic slugs from `vectors[idx]`'s parent `AlertFeatures.Tactics` across cluster members (requires passing `[]*models.AlertDef` alongside vectors). Pick the most frequent. Map to a human label:

```go
var tacticLabels = map[string]string{
    "initial-access":        "Initial Access",
    "execution":             "Execution",
    "persistence":           "Persistence",
    "privilege-escalation":  "Privilege Escalation",
    "defense-evasion":       "Defense Evasion",
    "credential-access":     "Credential Access",
    "discovery":             "Discovery",
    "lateral-movement":      "Lateral Movement",
    "collection":            "Collection",
    "exfiltration":          "Exfiltration",
    "command-and-control":   "Command & Control",
    "impact":                "Impact",
    "reconnaissance":        "Reconnaissance",
    "resource-development":  "Resource Development",
}
```
Result: `"Privilege Escalation Detections"`.

**Tier 2 — Action→category semantic map (fallback when no tactics):**

```go
var actionCategories = []struct{ keywords []string; category string }{
    {[]string{"remove", "delete", "revoke", "wipe"},         "Tampering"},
    {[]string{"login", "authenticate", "signin", "logon"},   "Authentication"},
    {[]string{"escalat", "grant", "privilege", "sudo"},      "Privilege Escalation"},
    {[]string{"exfiltrat", "download", "upload", "transfer"},"Exfiltration"},
    {[]string{"scan", "enumerat", "discover", "recon"},      "Discovery"},
    {[]string{"execute", "run", "inject", "spawn"},          "Execution"},
    {[]string{"persist", "install", "schedule", "startup"},  "Persistence"},
    {[]string{"encrypt", "ransom", "wipe", "destroy"},       "Impact"},
}
```

For each action token in the cluster, check if it contains any keyword in the table. First match wins. Result: `"Tampering Detections"` for `"Github Enterprise - Action Secret Was Removed"`.

**Tier 3 — Current behaviour (final fallback):**
Most frequent raw technique or action token, title-cased — unchanged from today.

**Implementation note:** Rather than changing function signatures, add a `tactics []string` field to `featureVector` and populate it in `buildFeatureVectors` from `a.Features.Tactics`. `deriveFamilyName` then uses `vectors[idx].tactics` directly — no need to pass `[]*models.AlertDef` deeper into the call chain.

---

### Fix 4: Noise Explanations

**Two changes:**

**Change A — Rule-based `Reason` field on `NoiseAlert`:**

Add `Reason string` to `models.NoiseAlert`. `findNoiseAlerts` gains a `alerts []*models.AlertDef` parameter so it can call `coralogix.ExtractAppSubsystem` to detect broad-scope alerts.

Classification logic:

```
app, sub = ExtractAppSubsystem(alert.TypeDef)
isBroadScope = (app == "" && sub == "")
```

| Condition | Reason |
|---|---|
| Broad-scope (no app/subsystem filter) | `"Broad-scope alert — no application or subsystem filter. Fires across all ingested data; ensure threshold and conditions are tight enough to avoid alert fatigue."` |
| No data sources | `"No log source identified — alert may fire across unintended data sources."` |
| No entities | `"No monitored entity (user, IP, host) — cannot scope blast radius or owner."` |
| No actions AND no conditions | `"No behavioral signal — likely a generic threshold with no attack-pattern context."` |
| No techniques | `"No MITRE technique mapped — coverage gap, hard to classify threat type."` |
| Multiple missing | Concatenate the two highest-priority reasons above. |

Broad-scope alerts with low features are **excluded from the noise list** — they are intentionally global monitors, not misconfigured rules.

**Change B — Richer LLM insights prompt:**

In `buildPrompt`, change the noise section to include `missingFeatures` and `reason` per alert:

```
Before:
  Noise alerts: Alert A, Alert B, Alert C

After:
  Noise alerts:
    Alert A — no actions, no conditions (generic threshold, no behavioral signal)
    Alert B — no sources, no entities (unscoped, no monitored asset)
```

---

### Fix 5: Performance & Model Selection

**Change 1 — Structured prompt format:**

Replace `buildPrompt` output with a compact structured format (~1126 chars vs 1789 chars original). Benchmark confirmed this reduces Mistral-Small latency from 11s to 6.7s with equivalent output quality. The new format uses `<…>` schema placeholders to prevent models echoing empty template values.

```
Role: Senior detection engineer. Task: Analyze alert library quality.

Library: {N} alerts | {D} duplicates | {F} families | {K} noisy alerts

Duplicates:
- A ≈ B (pct%)
...

Families: Name1 (alert1, alert2) | Name2 (alert3)

Coverage gaps: category1=N detections; category2=0, category3=0

Noisy alerts:
- AlertName: no actions, no conditions — <reason>
...

Return JSON only — no prose, no markdown:
{"summary":"<2-3 sentences>","top_priority":["<3-5 items>"],"strengths":["<2-3 items>"],"recommendations":["<3-5 items>"],"enriched_dups":["<1 sentence each>"],"enriched_gaps":["<1 sentence each>"],"noise_explanations":["<1 sentence each>"]}
```

**Change 2 — Switch default insights model:**

In `clients.yaml`:
```yaml
insights_model: "mistralai/mistral-small-4-119b-2603"
```
(was `z-ai/glm5` which times out on every request)

**Change 3 — Parallel insights goroutine in `HandleAnalyze`:**

Fire the insights LLM call concurrently with the first suggestion batch:

```go
type insightsResult struct {
    report *models.InsightsReport
    err    error
}
insightsCh := make(chan insightsResult, 1)
go func() {
    report, err := insights.Enrich(ctx, simResult, alerts, insightsProvider)
    insightsCh <- insightsResult{report, err}
}()
// ... existing suggestion pipeline.Run(...) ...
ir := <-insightsCh
```

**Change 4 — New `/api/insights` endpoint:**

`HandleInsights` accepts `POST /api/insights` with body `{ "client": "...", "model": "mistral|gemma" }`.
- Reads the cached similarity result for the client from Redis (if cache miss, returns 400 — user must run analysis first)
- Resolves the provider: `"mistral"` → Mistral-Small NIM, `"gemma"` → Gemma-3-27B NIM
- Calls `insights.Enrich` and returns `InsightsReport` (including the new `Model string` field)

**`InsightsReport` addition:**
```go
type InsightsReport struct {
    Model           string   `json:"model"`           // new — e.g. "Mistral Small"
    Summary         string   `json:"summary"`
    TopPriority     []string `json:"top_priority"`
    Strengths       []string `json:"strengths"`
    Recommendations []string `json:"recommendations"`
    EnrichedDups    []string `json:"enriched_dups"`
    EnrichedGaps    []string `json:"enriched_gaps"`
    NoiseExplanations []string `json:"noise_explanations"`
}
```

**Change 5 — Frontend model selector in insights panel:**

In the insights panel component:
- Header badge: `"Generated by: Mistral Small ▾"` — label uses `InsightsReport.Model`
- Clicking the badge opens a dropdown: `"Mistral Small (fast, ~7s)"` / `"Gemma 3 27B (detailed, ~14s)"`
- "Regenerate" button fires `POST /api/insights` with selected model
- Loading state: spinner replaces insights content; badge shows `"Regenerating…"`
- On response: badge updates, insights content replaces

---

## Reduced Prompt Caps

In `enrich.go`:
```go
maxPromptDuplicates = 10  // was 15
maxPromptFamilies   =  8  // was 15
maxPromptNoise      = 12  // was 20
```

---

## Files Changed

| File | Change |
|---|---|
| `backend/internal/similarity/engine.go` | `featureVector` + `buildFeatureVectors` + `scorePair` (Lucene+TimeWindow); `jaccardGroupBy` fix; `deriveFamilyName` 3-tier; `findNoiseAlerts` receives `[]*AlertDef`; new `tokenizeLucene` helper |
| `backend/internal/similarity/pivot_categories.go` | `jaccardGroupBy` empty+empty → `0.0` |
| `backend/internal/models/models.go` | `NoiseAlert.Reason string`; `InsightsReport.Model string` |
| `backend/internal/insights/enrich.go` | Structured prompt; noise section includes missingFeatures+reason; reduced caps |
| `backend/internal/api/handlers.go` | Parallel insights goroutine in `HandleAnalyze`; new `HandleInsights` |
| `backend/cmd/server/main.go` | Register `POST /api/insights` |
| `backend/clients.yaml` | `insights_model` → `mistralai/mistral-small-4-119b-2603` |
| `frontend/src/components/AlertInsights.tsx` | Model badge + dropdown + Regenerate button in insights panel |

---

## Benchmark Results (empirical)

Model × prompt latency (same representative insights prompt, NVIDIA NIM):

| Model | Prompt | Latency | Valid JSON |
|---|---|---|---|
| GLM5 (current) | original | TIMEOUT | ✗ |
| Mistral-Small | original | 11.1s | ✓ |
| **Mistral-Small** | **structured** | **6.7s** | **✓** |
| Gemma-3-27B | original | 14.0s | ✓ |
| Gemma-3-27B | structured | 11.9s | ✓ |
| Devstral-2-123B | original | 15.9s | ✓ |
| Llama-3.3-70B | original | 34.4s | ✓ |

Gemma-4-31B: "Downloadable" only — not available as NIM API endpoint.

---

## Error Handling

- `HandleInsights` cache miss → `400 Bad Request: "run analysis first"`
- Unknown model value → `400 Bad Request: "unknown insights model"`
- LLM timeout/error → `502 Bad Gateway` with error message; frontend shows error toast
- Empty insights result (no duplicates, families, or noise) → `204 No Content`; frontend hides insights panel

---

## Testing

Manual smoke tests:
1. Run analysis for a client with Salesforce SFDC alerts — verify `GuestUserAnomalyEvent` and `ApiAnomalyEvent` are **not** in the duplicates list
2. Check family names contain semantic labels ("Tampering", "Privilege Escalation") not raw tokens
3. Check noisy alerts have non-empty `Reason` field
4. Insights panel loads in ~7s with "Generated by: Mistral Small" badge
5. Switch to Gemma 3 27B via dropdown, click Regenerate — badge updates, insights replace in ~14s
6. Verify insights endpoint returns 400 when called before analysis is run
