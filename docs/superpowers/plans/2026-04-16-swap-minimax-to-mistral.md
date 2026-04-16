# Swap MiniMax → Mistral Suggestion Model

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `minimaxai/minimax-m2.7` (240s+ latency, frequent timeouts) with `mistralai/mistral-small-4-119b-2603` (8.4s, same quality) as the suggestion generation model.

**Architecture:** Three independent file changes: config swap, remove incompatible `chat_template_kwargs` flag that Mistral rejects (HTTP 400), and update the frontend model label. No new code paths — Mistral uses the same NVIDIA NIM non-streaming endpoint and API key.

**Tech Stack:** Go (backend config + LLM provider), React/TypeScript (frontend label)

---

## File Map

| File | Change |
|---|---|
| `backend/clients.yaml` | `suggestion_model`: minimax → mistral |
| `backend/internal/llm/nvidia.go` | Remove `chat_template_kwargs` from `completeNonStreaming` — Mistral rejects it with HTTP 400 |
| `frontend/src/components/MITREHeatmap.tsx:389` | Update default option label from "MiniMax M2.7" to "Mistral Small" |

---

### Task 1: Update suggestion model in config

**Files:**
- Modify: `backend/clients.yaml`

- [ ] **Step 1: Change suggestion_model**

In `backend/clients.yaml`, replace:
```yaml
  suggestion_model: "minimaxai/minimax-m2.7"
```
with:
```yaml
  suggestion_model: "mistralai/mistral-small-4-119b-2603"
```

- [ ] **Step 2: Verify config loads**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add clients.yaml
git commit -m "config: swap suggestion model from minimax-m2.7 to mistral-small-4-119b"
```

---

### Task 2: Remove chat_template_kwargs from non-streaming path

**Context:** `chat_template_kwargs: {enable_thinking: false}` was added to prevent MiniMax from leaking `<think>` blocks in its output. Mistral doesn't have a thinking mode and rejects this field with HTTP 400. The flag is only used in `completeNonStreaming` (the FastMode path), which is exclusively used for suggestion generation.

**Files:**
- Modify: `backend/internal/llm/nvidia.go`

- [ ] **Step 1: Remove chat_template_kwargs from the request body**

In `backend/internal/llm/nvidia.go`, in `completeNonStreaming`, replace:

```go
	body := map[string]any{
		"model":                n.model,
		"max_tokens":           maxTokens,
		"messages":             messages,
		"temperature":          0.6,
		"top_p":                0.95,
		"stream":               false,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}
```

with:

```go
	body := map[string]any{
		"model":       n.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": 0.6,
		"top_p":       0.95,
		"stream":      false,
	}
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/llm/nvidia.go
git commit -m "fix(nvidia): remove chat_template_kwargs from non-streaming path (unsupported by Mistral)"
```

---

### Task 3: Update frontend default model label

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx`

- [ ] **Step 1: Update the default option label**

In `frontend/src/components/MITREHeatmap.tsx` at line 389, replace:

```tsx
          <option value="">MiniMax M2.7 (default)</option>
```

with:

```tsx
          <option value="">Mistral Small (default)</option>
```

- [ ] **Step 2: Verify frontend builds**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npm run build 2>&1 | tail -5
```
Expected: output ends with something like `✓ built in Xs` (no errors).

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/MITREHeatmap.tsx
git commit -m "ui: update default suggestion model label to Mistral Small"
```

---

### Task 4: Smoke test

- [ ] **Step 1: Restart the server**

```bash
cd /Users/aviral.baloni/Desktop/claude && ./dev.sh restart
```

- [ ] **Step 2: Check backend logs confirm Mistral is active**

```bash
tail -10 /tmp/backend.log
```

Expected: server starts cleanly, no errors about model or API.

- [ ] **Step 3: Verify suggestion endpoint responds within 15s**

```bash
curl -sf -X POST http://localhost:8080/api/suggestions \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel","technique_id":"T1078","technique_name":"Valid Accounts","tactic":"Persistence"}' | python3 -m json.tool
```

Expected: JSON response with `suggestions` array, returned in under 15 seconds.
