# Claude Provider Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route all LLM pipeline stages to Anthropic Claude — Haiku for classifier/validator, Opus for insights and suggestions — with NVIDIA NIM and Gemini retained as user-selectable overrides for suggestions only.

**Architecture:** Config fields already exist for all four stages; the work is updating their values in `clients.yaml`, fixing two incorrect model-fallback paths in `handlers.go`, removing dead model-switching code from the insights handler, and updating two frontend components. No new files, no new structs, no new API fields.

**Tech Stack:** Go 1.21 (backend), React + TypeScript (frontend), `internal/llm` provider abstraction.

---

## File Map

| File | Change |
|------|--------|
| `backend/clients.yaml` | Update provider/model values for all four stages |
| `backend/internal/api/handlers.go` | Fix `runInsightsBackground` model fallback; clean up `HandleInsights` request struct, validation, model-branch, and provider construction |
| `frontend/src/components/AlertInsights.tsx` | Remove model selector state and dropdown; replace with static badge; fix regenerate and error-state text |
| `frontend/src/components/MITREHeatmap.tsx` | Update suggestions provider dropdown options |

---

## Task 1: Update clients.yaml provider/model assignments

**Files:**
- Modify: `backend/clients.yaml`

- [ ] **Step 1: Update the YAML**

Replace the `llm:` section in `backend/clients.yaml` with:

```yaml
neon_dsn: ""           # set via NEON_DSN env var
monday_api_token: ""   # set via MONDAY_API_TOKEN env var
monday_board_id: 0     # set via MONDAY_BOARD_ID env var

llm:
  default_provider: "claude"
  nvidia_api_key: ""             # set via NVIDIA_API_KEY env var
  nvidia_model: "nvidia/nemotron-3-super-120b-a12b"
  nvidia_endpoint: "https://integrate.api.nvidia.com/v1/chat/completions"

  classifier_provider: "claude"
  classifier_model: "claude-haiku-4-5-20251001"

  validator_provider: "claude"
  validator_model: "claude-haiku-4-5-20251001"

  suggestion_provider: "claude"
  suggestion_model: "claude-opus-4-7"
  nvidia_suggestion_api_key: "" # set via NVIDIA_SUGGESTION_API_KEY env var

  insights_provider: "claude"
  insights_model: "claude-opus-4-7"

  gemini_api_key: ""   # set via GEMINI_API_KEY env var
  gemini_model: "gemini-2.0-flash"

classifier:
  endpoint: "http://localhost:8001"

clients:
  Deel:
    api_key: ""          # set via CLIENT_DEEL_API_KEY env var
    region: eu1
    monday_group_id: "new_group47786"
  JioStar:
    api_key: ""          # set via CLIENT_JIOSTAR_API_KEY env var
    region: ap1
    monday_group_id: "group_mkrzg3ma"
```

- [ ] **Step 2: Verify build still passes**

```bash
cd backend && go build ./...
```
Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
cd backend
git add clients.yaml
git commit -m "config: migrate all LLM stages to Claude (Haiku for classifier/validator, Opus for insights/suggestions)"
```

---

## Task 2: Fix runInsightsBackground provider construction

**Files:**
- Modify: `backend/internal/api/handlers.go` (lines ~308–327)

The background insights runner has two bugs now that the provider is Claude:
1. Falls back to `NvidiaModel` instead of `ClaudeModel` when `InsightsModel` is empty.
2. Passes `""` as `classifierModel` to `NewClassifierProvider`, so the model override is never applied.

- [ ] **Step 1: Write a failing test**

Create `backend/internal/api/insights_provider_test.go`:

```go
package api

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/llm"
)

// resolveInsightsProvider replicates the model/provider resolution logic used in
// both runInsightsBackground and HandleInsights so it can be tested in isolation.
func resolveInsightsProvider(cfg *config.Config) (llm.Provider, error) {
	providerName := cfg.LLM.InsightsProvider
	if providerName == "" {
		providerName = cfg.LLM.DefaultProvider
	}
	model := cfg.LLM.InsightsModel
	if model == "" {
		model = cfg.LLM.ClaudeModel
	}
	return llm.NewClassifierProvider(providerName, model, llm.ProviderConfig{
		AnthropicAPIKey: cfg.LLM.AnthropicAPIKey,
		ClaudeModel:     cfg.LLM.ClaudeModel,
		NvidiaAPIKey:    cfg.LLM.NvidiaAPIKey,
		NvidiaModel:     cfg.LLM.NvidiaModel,
		NvidiaEndpoint:  cfg.LLM.NvidiaEndpoint,
		GeminiAPIKey:    cfg.LLM.GeminiAPIKey,
		GeminiModel:     cfg.LLM.GeminiModel,
	})
}

func TestResolveInsightsProvider_UsesClaudeOpus(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			InsightsProvider: "claude",
			InsightsModel:    "claude-opus-4-7",
			AnthropicAPIKey:  "test-key",
		},
	}
	p, err := resolveInsightsProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "Claude" {
		t.Errorf("expected provider name Claude, got %q", p.Name())
	}
}

func TestResolveInsightsProvider_FallsBackToDefaultProvider(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			InsightsProvider: "",
			DefaultProvider:  "claude",
			InsightsModel:    "claude-opus-4-7",
			AnthropicAPIKey:  "test-key",
		},
	}
	p, err := resolveInsightsProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "Claude" {
		t.Errorf("expected provider Claude on empty InsightsProvider, got %q", p.Name())
	}
}
```

- [ ] **Step 2: Run tests to see them fail** (function not yet extracted)

```bash
cd backend && go test ./internal/api/... -run TestResolveInsightsProvider -v
```
Expected: `FAIL` — `resolveInsightsProvider` is not defined yet.

- [ ] **Step 3: Update runInsightsBackground in handlers.go**

Find the block at ~line 308 inside `runInsightsBackground` and replace:

```go
		insightsProviderName := h.config.LLM.InsightsProvider
		if insightsProviderName == "" {
			insightsProviderName = h.config.LLM.SuggestionProvider
		}
		insightsModel := h.config.LLM.InsightsModel
		if insightsModel == "" {
			insightsModel = h.config.LLM.NvidiaModel
		}
		insightsProvider, err := llm.NewClassifierProvider(
			insightsProviderName, "",
			llm.ProviderConfig{
				AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
				ClaudeModel:     h.config.LLM.ClaudeModel,
				NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
				NvidiaModel:     insightsModel,
				NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
				GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
				GeminiModel:     h.config.LLM.GeminiModel,
			},
		)
```

With:

```go
		insightsProvider, err := resolveInsightsProvider(h.config)
```

- [ ] **Step 4: Add resolveInsightsProvider as a package-level function in handlers.go**

Add this function just before `runInsightsBackground`:

```go
// resolveInsightsProvider constructs the LLM provider for insights enrichment
// using the insights-specific config fields, falling back to the default provider.
func resolveInsightsProvider(cfg *config.Config) (llm.Provider, error) {
	providerName := cfg.LLM.InsightsProvider
	if providerName == "" {
		providerName = cfg.LLM.DefaultProvider
	}
	model := cfg.LLM.InsightsModel
	if model == "" {
		model = cfg.LLM.ClaudeModel
	}
	return llm.NewClassifierProvider(providerName, model, llm.ProviderConfig{
		AnthropicAPIKey: cfg.LLM.AnthropicAPIKey,
		ClaudeModel:     cfg.LLM.ClaudeModel,
		NvidiaAPIKey:    cfg.LLM.NvidiaAPIKey,
		NvidiaModel:     cfg.LLM.NvidiaModel,
		NvidiaEndpoint:  cfg.LLM.NvidiaEndpoint,
		GeminiAPIKey:    cfg.LLM.GeminiAPIKey,
		GeminiModel:     cfg.LLM.GeminiModel,
	})
}
```

- [ ] **Step 5: Run tests — expect pass**

```bash
cd backend && go test ./internal/api/... -run TestResolveInsightsProvider -v
```
Expected:
```
--- PASS: TestResolveInsightsProvider_UsesClaudeOpus (0.00s)
--- PASS: TestResolveInsightsProvider_FallsBackToDefaultProvider (0.00s)
PASS
```

- [ ] **Step 6: Verify full build**

```bash
cd backend && go build ./...
```
Expected: no output.

- [ ] **Step 7: Commit**

```bash
cd backend
git add internal/api/handlers.go internal/api/insights_provider_test.go
git commit -m "refactor(insights): extract resolveInsightsProvider, fix NvidiaModel fallback bug in background runner"
```

---

## Task 3: Clean up HandleInsights — remove model-switching code

**Files:**
- Modify: `backend/internal/api/handlers.go` (HandleInsights function, ~lines 364–511)

`HandleInsights` still has the old `req.Model` field, model validation, and NVIDIA model-name branching. Since insights is now Claude-only, this is dead code that must be removed.

- [ ] **Step 1: Remove req.Model from the request struct**

Find (in `HandleInsights`):
```go
	var req struct {
		Client string `json:"client"`
		Model  string `json:"model"` // "mistral" | "gemma" | "" (default = mistral)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	req.Model = strings.TrimSpace(strings.ToLower(req.Model))
	if req.Model != "" && req.Model != "mistral" && req.Model != "gemma" {
		writeError(w, http.StatusBadRequest, "unknown insights model: use \"mistral\" or \"gemma\"")
		return
	}
```

Replace with:
```go
	var req struct {
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
```

- [ ] **Step 2: Remove the cache-bypass condition tied to req.Model**

Find:
```go
	if h.cache != nil && req.Model == "" {
```

Replace with:
```go
	if h.cache != nil {
```

- [ ] **Step 3: Remove the model-branching block and replace with resolveInsightsProvider**

Find (after the cache-check block, ~line 458):
```go
	// Resolve model: explicit request overrides config default.
	insightsProviderName := h.config.LLM.InsightsProvider
	if insightsProviderName == "" {
		insightsProviderName = h.config.LLM.SuggestionProvider
	}
	insightsModel := h.config.LLM.InsightsModel
	if insightsModel == "" {
		insightsModel = h.config.LLM.NvidiaModel
	}
	modelLabel := "Mistral Small" // human-readable name shown in UI
	if req.Model == "gemma" {
		insightsModel = "google/gemma-3-27b-it"
		modelLabel = "Gemma 3 27B"
	}
	// Insights uses the primary NVIDIA key (not the suggestion-specific key).
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	insightsProvider, err := llm.NewClassifierProvider(
		insightsProviderName,
		"",
		llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     insightsModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		},
	)
```

Replace with:
```go
	modelLabel := "Claude Opus 4.7"
	insightsProvider, err := resolveInsightsProvider(h.config)
```

- [ ] **Step 4: Build and run tests**

```bash
cd backend && go build ./... && go test ./internal/api/... -v
```
Expected: clean build, all tests pass.

- [ ] **Step 5: Commit**

```bash
cd backend
git add internal/api/handlers.go
git commit -m "refactor(insights): remove dead model-switching code, use resolveInsightsProvider"
```

---

## Task 4: Update AlertInsights.tsx — replace model dropdown with static badge

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx`

Three changes: (a) remove `selectedModel` state, (b) simplify `handleRegenerate`, (c) replace the `<select>` with a static provider badge, (d) fix the error-state retry label.

- [ ] **Step 1: Remove selectedModel state and simplify handleRegenerate**

Find:
```tsx
  const [selectedModel, setSelectedModel] = useState<'mistral' | 'gemma'>('mistral');
```
Delete this line entirely.

Find:
```tsx
  const handleRegenerate = async () => {
    setIsRegenerating(true);
    setRegenError(false);
    try {
      const newReport = await fetchInsights(client, selectedModel);
```

Replace with:
```tsx
  const handleRegenerate = async () => {
    setIsRegenerating(true);
    setRegenError(false);
    try {
      const newReport = await fetchInsights(client);
```

- [ ] **Step 2: Replace the model selector with a static badge**

Find:
```tsx
        {/* Model selector */}
        <div className="insights-model-header">
          <select
            className="insights-model-select"
            value={selectedModel}
            onChange={(e) => setSelectedModel(e.target.value as 'mistral' | 'gemma')}
            disabled={isRegenerating}
          >
            <option value="mistral">Mistral Small 3.1</option>
            <option value="gemma">Gemma 3 27B</option>
          </select>
          <button
            className="insights-regenerate-btn"
            onClick={handleRegenerate}
            disabled={isRegenerating || !client}
            title="Regenerate insights with selected model"
          >
            {isRegenerating ? '…' : '↺'}
          </button>
        </div>
```

Replace with:
```tsx
        {/* Provider badge */}
        <div className="insights-model-header">
          <span className="insights-model-badge">Claude Opus 4.7</span>
          <button
            className="insights-regenerate-btn"
            onClick={handleRegenerate}
            disabled={isRegenerating || !client}
            title="Regenerate insights"
          >
            {isRegenerating ? '…' : '↺'}
          </button>
        </div>
```

- [ ] **Step 3: Fix the error-state retry label**

Find:
```tsx
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry with {selectedModel === 'mistral' ? 'Mistral Small' : 'Gemma 3 27B'}</button>
```

Replace with:
```tsx
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry with Claude Opus</button>
```

- [ ] **Step 4: Add the badge style to App.css**

In `frontend/src/App.css`, find the existing `.insights-model-select` rule (~line 567) and add the badge rule immediately after it:

```css
.insights-model-badge { flex: 1; font-family: var(--font-mono); font-size: 0.68rem; font-weight: 600; color: var(--text); padding: 7px 10px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); white-space: nowrap; }
```

- [ ] **Step 5: Verify TypeScript build**

```bash
cd frontend && npm run build 2>&1 | tail -20
```
Expected: no TypeScript errors, build succeeds.

- [ ] **Step 6: Commit**

```bash
cd frontend
git add src/components/AlertInsights.tsx src/App.css
git commit -m "feat(ui): replace insights model dropdown with static Claude Opus 4.7 badge"
```

---

## Task 5: Update MITREHeatmap.tsx — update suggestions provider dropdown

**Files:**
- Modify: `frontend/src/components/MITREHeatmap.tsx` (~line 399–404)

- [ ] **Step 1: Update the dropdown options**

Find:
```tsx
          <option value="">Mistral Small (default)</option>
          <option value="nvidia">NVIDIA (Nemotron)</option>
          <option value="claude">Claude (Haiku)</option>
          <option value="gemini">Gemini 2.0 Flash</option>
```

Replace with:
```tsx
          <option value="">Claude Opus (default)</option>
          <option value="nvidia">NVIDIA NIM (Nemotron)</option>
          <option value="gemini">Gemini 2.0 Flash</option>
```

- [ ] **Step 2: Verify TypeScript build**

```bash
cd frontend && npm run build 2>&1 | tail -20
```
Expected: clean build, no errors.

- [ ] **Step 3: Commit**

```bash
cd frontend
git add src/components/MITREHeatmap.tsx
git commit -m "feat(ui): update suggestions dropdown — Claude Opus default, keep NVIDIA NIM and Gemini, remove explicit Claude option"
```

---

## Final Verification

- [ ] **Run full backend test suite**

```bash
cd backend && go test ./... -v 2>&1 | tail -30
```
Expected: all tests pass, no failures.

- [ ] **Run frontend type check**

```bash
cd frontend && npx tsc --noEmit
```
Expected: no errors.

- [ ] **Smoke test** (requires `dev.sh` running)

```bash
./dev.sh start
# In browser: open http://localhost:5173
# 1. Select a client and run analysis
# 2. Verify Insights panel shows "Claude Opus 4.7" badge (not a dropdown)
# 3. Open a yellow/red MITRE technique → verify suggestions dropdown shows "Claude Opus (default)" as first option
# 4. Generate suggestions with default → verify response includes provider "Claude"
# 5. Switch to NVIDIA NIM → verify suggestions generate successfully
# 6. Switch to Gemini → verify suggestions generate successfully
```
