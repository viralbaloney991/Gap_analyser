# Suggestion Cache Empty-Result Fix

**Date:** 2026-04-16  
**Status:** Approved

## Problem

Two related issues cause the suggestion panel to show 0 results even when detection IS possible:

1. **Cache poisoning:** When an LLM returns an empty `[]` for a technique, the result is stored in NeonDB. Every subsequent request (including Regenerate) hits the cached empty result and never calls the LLM again.

2. **Over-conservative model:** The system prompt instructs the model to return `[]` if the technique "CANNOT be detected". Mistral Small interprets this too strictly — it returned `[]` for T1029 (Scheduled Transfer) despite 30 log sources including DLP (ForcePoint), Zscaler, and PaloAlto Firewall that are clearly capable of detecting scheduled transfers.

## Root Cause Evidence

From backend logs:
```
INFO [nvidia] non-streaming complete, response=2 chars   ← [] cached
INFO [suggestions] cache hit ... rows=1 merged=0          ← every subsequent request
```

## Solution

### Change 1: Don't cache empty LLM responses

**File:** `backend/internal/api/handlers.go`

Add a guard before `AppendCachedSuggestions` (currently line 592):

```go
// Only cache non-empty results — empty responses may be transient model
// conservatism and should not block future LLM calls for this technique.
if len(result.Suggestions) > 0 && h.alertStore != nil && cacheKey != "" {
    // existing AppendCachedSuggestions call
}
```

Empty responses are discarded. Future requests and Regenerate clicks always call the LLM fresh until at least one suggestion is returned.

### Change 2: Soften the empty-array rule in the system prompt

**File:** `backend/internal/llm/suggestions.go`

Replace the last rule:

**Before:**
```
- If the technique CANNOT be detected with any available log source, return an empty array []
```

**After:**
```
- Only return an empty array [] if there is truly no log source that could detect any aspect of this technique — prefer suggesting an imperfect or partial alert over returning nothing
```

## Testing

### Unit test: empty result not cached
- Mock LLM returns `[]`
- Call handler
- Assert `GetCachedSuggestions` returns nothing (no row inserted)

### Unit test: non-empty result still cached
- Mock LLM returns 2 suggestions
- Call handler
- Assert `GetCachedSuggestions` returns 1 row with 2 suggestions

### Smoke test
- Clear the existing T1029/JioStar cache entry from NeonDB
- Hit `/api/suggestions` for JioStar/T1029
- Assert response contains at least 1 suggestion

## What Does NOT Change

- Cache key algorithm
- Force flag / Regenerate mechanism (already correct)
- Merge logic
- Frontend
- All other caching behaviour for non-empty results
