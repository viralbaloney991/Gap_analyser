# Detection Family Naming — Design

**Date:** 2026-04-29
**Status:** Approved

## Summary

`deriveFamilyName()` falls back to `"Detection Family N"` for clusters whose alerts carry no MITRE tactics, no matching action keywords, and no techniques or actions at all. The fix has two parts: (F) expand the Tier 2 action keyword map to catch more patterns before they fall through, and (G) add two new tiers (Tier 4: nameTokens; Tier 5: dataSources) that use signals already present on every `featureVector`, eliminating "Detection Family N" in all practical cases.

---

## Root Cause

`deriveFamilyName` uses a 3-tier strategy:

| Tier | Signal | Produces |
|------|--------|----------|
| 1 | Most frequent MITRE tactic slug | "Credential Access Detections" |
| 2 | Action token matched against 8 keyword categories | "Authentication Detections" |
| 3 | Most frequent technique or action token | "T1078 Detections" / "Login Detections" |
| fallback | — | "Detection Family N" |

Tier 1 fails when alerts have no MITRE enrichment. Tier 2 fails when action tokens don't match any of the 8 narrow categories. Tier 3 fails when both `techniques` and `actions` are empty. The result: `"Detection Family N"`.

Every alert has a name (→ `nameTokens`) and almost always has a data source (→ `dataSources`), but neither is used for naming.

---

## Scope

**In scope:**
- Expand `actionCategories` with ~8 new entries (Part F)
- Add Tier 4 (nameTokens frequency, stop-word filtered) in `deriveFamilyName` (Part G)
- Add Tier 5 (most frequent dataSource) in `deriveFamilyName` (Part G)
- Tests for each new tier and for the expanded keyword map

**Out of scope:**
- Frontend changes
- Replacing Tier 3's technique-ID output (e.g. "T1078 Detections") — separate issue
- IDF-weighted nameToken ranking — plain frequency is sufficient for naming

---

## Design

### Part F — Expanded Tier 2 action keyword categories

Append to the existing `actionCategories` slice in `engine.go`. Order matters (first match wins); new entries go after the existing 8.

| Category | Keywords (substring match) |
|----------|---------------------------|
| Network | "connect", "network", "traffic", "firewall" |
| Access | "access", "read", "fetch", "view", "list" |
| Configuration Change | "modif", "update", "change", "configur", "patch" |
| API Activity | "api", "call", "request", "invoke" |
| Credential | "credential", "token", "key", "secret", "password", "certif" |
| Deployment | "deploy", "launch", "provision", "spawn" |
| Data Operations | "backup", "restore", "export", "import", "query" |
| Anomaly | "anomal", "spike", "surge", "threshold", "volume" |

No signature changes; no changes outside `actionCategories`.

### Part G — Tier 4 (nameTokens) and Tier 5 (dataSources)

Both tiers are added inside `deriveFamilyName`, after the existing Tier 3 block and before the `"Detection Family N"` return. No signature changes — both `nameTokens` and `dataSources` are already fields on `featureVector`.

#### Tier 4 — nameTokens frequency, stop-word filtered

```
nameStopWords = {
    "alert", "alerts", "detection", "detections",
    "rule", "rules", "monitor", "monitoring",
    "log", "logs", "event", "events",
}
```

Algorithm:
1. Build a frequency map: for each cluster member, for each token in `nameTokens`, increment `freq[token]`.
2. Remove all tokens in `nameStopWords` from `freq`.
3. If `freq` is non-empty: pick the token with the highest count (tie-break alphabetically) → `toTitle(token) + " Detections"`.

Example: cluster of `"AWS CloudTrail Unusual API Call"` alerts → nameTokens include `"cloudtrail"`, `"unusual"`, `"api"`, `"call"` → most frequent is `"cloudtrail"` → `"Cloudtrail Detections"`.

#### Tier 5 — most frequent dataSource

If Tier 4 produces no result (all nameTokens were stop-words or nameTokens is empty):
1. Build a frequency map of `dataSources` tokens across all cluster members.
2. Pick the token with the highest count (tie-break alphabetically) → `toTitle(source) + " Detections"`.

Example: sparse cluster with `dataSources: {"okta"}` on every member → `"Okta Detections"`.

#### Fallback

`"Detection Family N"` is now only reached when a cluster has no tactics, no matching actions, no techniques, no actions, all-stop-word alert names, and no data sources — essentially impossible in a real alert corpus.

---

## Tier Summary (after fix)

| Tier | Signal | Example output |
|------|--------|---------------|
| 1 | MITRE tactic | "Credential Access Detections" |
| 2 | Action keyword (expanded) | "Authentication Detections" |
| 3 | Technique / action token | "T1078 Detections" / "Login Detections" |
| 4 (new) | nameTokens frequency, filtered | "Cloudtrail Detections" |
| 5 (new) | dataSources frequency | "Okta Detections" |
| fallback | — | "Detection Family N" |

---

## Tests

| Test | What it verifies |
|------|-----------------|
| `TestDeriveFamilyName_tier4_nameTokens` | Cluster with no tactics/techniques/actions; alert names contain "cloudtrail" → "Cloudtrail Detections" |
| `TestDeriveFamilyName_tier4_stopWordFiltered` | All nameTokens are stop-words → falls through to Tier 5 |
| `TestDeriveFamilyName_tier5_dataSource` | Cluster with no tactics/techniques/actions/nameTokens but dataSources = {"okta"} → "Okta Detections" |
| `TestDeriveFamilyName_tier2_expanded_network` | Action token "connect" → "Network Detections" |
| `TestDeriveFamilyName_tier2_expanded_credential` | Action token "token" → "Credential Detections" |

---

## Error Handling

No new error paths. All new logic operates on in-memory token maps and cannot fail.
