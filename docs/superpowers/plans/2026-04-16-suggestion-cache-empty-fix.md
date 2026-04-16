# Suggestion Cache Empty-Result Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop empty LLM responses from being cached and soften the system prompt so models prefer suggesting partial alerts over returning nothing.

**Architecture:** Two independent one-line changes — a guard in the handler's cache-write path, and a string replacement in the system prompt constant. No new files, no interface changes, no migration needed.

**Tech Stack:** Go (backend handler + LLM prompt)

---

## File Map

| File | Change |
|---|---|
| `backend/internal/api/handlers.go` | Add `len(result.Suggestions) > 0` guard before `AppendCachedSuggestions` |
| `backend/internal/llm/suggestions.go` | Replace last rule in `systemPrompt` constant |

---

### Task 1: Soften the system prompt

**Files:**
- Modify: `backend/internal/llm/suggestions.go:55`

- [ ] **Step 1: Replace the empty-array rule**

In `backend/internal/llm/suggestions.go`, replace line 55:

```go
- If the technique CANNOT be detected with any available log source, return an empty array []
```

with:

```go
- Only return an empty array [] if there is truly no log source that could detect any aspect of this technique — prefer suggesting an imperfect or partial alert over returning nothing
```

The full `systemPrompt` constant after the change (lines 44–66):

```go
const systemPrompt = `You are a Coralogix SIEM alert engineering expert specializing in MITRE ATT&CK coverage.

Your job: given a client's available log sources and ONE specific uncovered MITRE ATT&CK technique, suggest up to 6 concrete Coralogix alerts that can detect this technique using the available logs.

Rules:
- Only suggest alerts that are REALISTICALLY detectable from the available log sources
- Each suggestion must reference a specific log source the client already has
- Provide a concrete Lucene/DataPrime query hint for each alert
- Be specific about what fields/events to look for in the log source
- Keep alert names concise: "[LogSource] - [Behavior Description]"
- Suggest DIFFERENT detection approaches (different log sources, different indicators) — do not repeat the same idea
- Only return an empty array [] if there is truly no log source that could detect any aspect of this technique — prefer suggesting an imperfect or partial alert over returning nothing
- Return at most 6 suggestions, ordered by detection quality (best first)

Respond ONLY with a JSON array. No markdown, no explanation, just the JSON array.
Each object must have exactly these fields:
{
  "log_source": "Which available log source to use",
  "alert_name": "Suggested alert name",
  "description": "What the alert detects and why it maps to this technique",
  "query_hint": "Lucene or DataPrime query pattern to use",
  "priority": "critical|high|medium|low"
}`
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/llm/suggestions.go
git commit -m "fix(suggestions): soften empty-array rule to prefer partial coverage over nothing"
```

---

### Task 2: Don't cache empty LLM responses

**Files:**
- Modify: `backend/internal/api/handlers.go:592`

- [ ] **Step 1: Add the guard**

In `backend/internal/api/handlers.go`, replace the cache-append block starting at line 591:

```go
	// Append to persistent cache.
	if h.alertStore != nil && cacheKey != "" {
```

with:

```go
	// Append to persistent cache — skip empty results so future requests
	// call the LLM fresh rather than returning a cached empty response.
	if len(result.Suggestions) > 0 && h.alertStore != nil && cacheKey != "" {
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Run existing tests to confirm no regression**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/llm/... ./cmd/server/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/api/handlers.go
git commit -m "fix(suggestions): skip caching empty LLM responses to prevent cache poisoning"
```

---

### Task 3: Clear poisoned cache entry and smoke test

**Files:** None (DB operation + live test)

- [ ] **Step 1: Delete the poisoned T1029 cache rows from NeonDB**

```bash
psql "postgresql://neondb_owner:npg_48NejplWUsMx@ep-royal-scene-a1m3lul8.ap-southeast-1.aws.neon.tech/neondb?sslmode=require" \
  -c "DELETE FROM suggestion_cache WHERE technique_id = 'T1029';"
```

Expected output: `DELETE 1` (or more, one per cached empty row).

- [ ] **Step 2: Restart the server**

```bash
cd /Users/aviral.baloni/Desktop/claude && ./dev.sh restart
```

Expected: server starts cleanly.

- [ ] **Step 3: Hit the suggestions endpoint for JioStar T1029**

```bash
time curl -sf -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"JioStar","technique_id":"T1029","technique_name":"Scheduled Transfer","tactic":"Exfiltration"}' \
  | python3 -c "import sys,json; r=json.load(sys.stdin); print(f'suggestions={len(r[\"suggestions\"])} provider={r[\"provider\"]}')"
```

Expected: `suggestions=N provider=NVIDIA NIM` where N ≥ 1, returned in under 15 seconds.

- [ ] **Step 4: Verify the result is now cached (second call returns cache hit)**

```bash
curl -sf -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"JioStar","technique_id":"T1029","technique_name":"Scheduled Transfer","tactic":"Exfiltration"}' \
  | python3 -c "import sys,json; r=json.load(sys.stdin); print(f'suggestions={len(r[\"suggestions\"])}')"
```

Check backend logs:
```bash
tail -5 /tmp/backend.log
```

Expected log line: `INFO [suggestions] cache hit client=JioStar technique=T1029 rows=1 merged=N` where N ≥ 1.
