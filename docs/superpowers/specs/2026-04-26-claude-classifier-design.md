# Claude-Native MITRE Classifier Design

**Date:** 2026-04-26
**Status:** Approved

## Summary

Replace the two-stage classification pipeline (Python sidecar → LLM validator) with a single Claude call that classifies alerts directly against the full MITRE ATT&CK technique list. Delete the Python sidecar entirely.

## Before / After

```
Before:
Alert → Python sidecar (semantic embeddings) → top-5 candidates → Claude Haiku (validate) → T-codes

After:
Alert → Claude Haiku (classify from full technique list) → T-codes
```

## Prompt Strategy

**Format:** Compact JSON grouped by tactic. Sub-techniques expressed as suffix-only keys under their parent to eliminate ID prefix repetition.

```json
{
  "reconnaissance": {
    "T1595": {"n":"Active Scanning","s":{"001":"IP Blocks","002":"Vuln Scanning","003":"Wordlist Scanning"}}
  },
  "execution": {
    "T1059": {"n":"Command & Script Interpreter","s":{"001":"PowerShell","002":"AppleScript","003":"WMI"}}
  }
}
```

**Token efficiency:** ~2,500 tokens vs ~7,000 for readable list format (~60% reduction).

**System prompt rules:**
- Return ONLY a JSON array of technique IDs, e.g. `["T1078.004", "T1110"]`
- Only use IDs from the provided list — no others
- Sub-techniques are expressed as `{parent}.{suffix}` — e.g. suffix `001` under `T1059` = `T1059.001`
- Prefer sub-techniques over parents when the alert is specific enough
- Return at most 5 techniques per alert
- If none apply, return `[]`
- No markdown, no explanation

**User message** (per alert):
```
Alert: {name} | App: {app} | Subsystem: {subsystem}
Query: {query}
```

**Technique list generation:** `BuildTechniqueJSON()` in `internal/mitre/mitre.go`, computed once via `sync.Once` from the existing `masterTechniqueList` (130 parents) and `subTechniqueNames` (400+ sub-techniques). No new data files.

**Output validation:** Every returned ID is checked against the master list. IDs not in the list are silently dropped. This is the safety net against hallucination.

## Code Changes

| File | Action | Detail |
|------|--------|--------|
| `internal/llm/mitre_mapper.go` | Rewrite | Replace `BatchClassifyAndValidate(classifierClient, validatorProvider, ...)` with `BatchClassify(provider llm.Provider, store, sem, inputs)` — single LLM call per alert |
| `internal/llm/validator.go` | Replace | Rename to `internal/llm/classifier_prompt.go` — new system prompt with compact JSON technique list, `buildClassifierMessage(AlertInput)`, `parseClassifierResponse(string) []string` |
| `internal/mitre/mitre.go` | Add | `BuildTechniqueJSON() string` — compact tactic-grouped JSON, computed once via `sync.Once` |
| `internal/classifier/client.go` | **Delete** | Entire package removed |
| `internal/api/handlers.go` | Update | Remove `classifier.NewClient(...)` and `classifierClient`; call `llm.BatchClassify(classifierProvider, ...)` instead of `BatchClassifyAndValidate` |
| `internal/config/config.go` | Update | Remove `ClassifierConfig` struct, `Classifier` field, `classifier.endpoint` required validation, `ValidatorProvider`/`ValidatorModel` fields |
| `backend/clients.yaml` | Update | Remove `classifier:` section and `validator_provider`/`validator_model` fields |
| `classifier/` (project root) | **Delete** | Entire Python sidecar directory |

## Config After Change

```yaml
llm:
  default_provider: "claude"
  classifier_provider: "claude"
  classifier_model: "claude-haiku-4-5-20251001"
  # validator_provider and validator_model removed
  # classifier endpoint removed
```

## Error Handling

| Condition | Behaviour |
|-----------|-----------|
| LLM call fails | Log warning, return empty techniques for that alert — analysis continues |
| LLM returns invalid JSON | Log warning, return empty techniques |
| LLM returns ID not in master list | Silently drop, keep valid IDs |
| Alert already has MITRE labels | Skip LLM (same as current) |
| All alerts have existing labels | Skip `BatchClassify` entirely |

## Testing

- `TestBuildTechniqueJSON` — valid JSON, all ~530 IDs present, suffix reconstruction correct
- `TestParseClassifierResponse` — valid array, empty array, hallucinated ID dropped, malformed JSON → empty, markdown-wrapped JSON stripped and parsed
- `TestBatchClassify` — mock provider, cache hit skips LLM, semaphore acquired/released

## What Does Not Change

- `ExtractFeatures` and all downstream MITRE coverage analysis — unchanged
- Redis cache (same key scheme: SHA256 of name+query+app+subsystem, 7-day TTL)
- Pipeline semaphore and concurrency model
- All other LLM providers (insights, suggestions) — unchanged
- Frontend — unchanged
