# Claude Provider Migration Design

**Date:** 2026-04-26
**Status:** Approved

## Summary

Migrate all LLM pipeline stages to use Anthropic Claude as the primary provider, with NVIDIA NIM and Gemini retained as user-selectable alternatives for suggestions only.

## Model Assignments

| Stage | Provider | Model | Rationale |
|-------|----------|-------|-----------|
| Classifier | claude | `claude-haiku-4-5-20251001` | High-volume batch per alert; speed and cost efficiency |
| Validator | claude | `claude-haiku-4-5-20251001` | Same batch profile; confirmation task, not reasoning-heavy |
| Insights | claude | `claude-opus-4-7` | Single long-form report per analysis; quality is paramount |
| Suggestions | claude | `claude-opus-4-7` | Per-technique generation; user-facing quality; NVIDIA/Gemini as explicit user overrides |

## Backend Changes

### `clients.yaml`
Update provider/model fields for all four stages:
- `classifier_provider: "claude"`, `classifier_model: "claude-haiku-4-5-20251001"`
- `validator_provider: "claude"`, `validator_model: "claude-haiku-4-5-20251001"`
- `insights_provider: "claude"`, `insights_model: "claude-opus-4-7"`
- `suggestion_provider: "claude"`, `suggestion_model: "claude-opus-4-7"`
- `default_provider: "claude"`

No new config struct fields required — all fields already exist in `LLMConfig`.

### `internal/api/handlers.go` — Insights handler
- Remove `req.Model` branching (was mapping string values like `"mistral"` / `"gemma"` to NVIDIA model names)
- Simplified flow: always construct provider from `cfg.LLM.InsightsProvider` + `cfg.LLM.InsightsModel`
- `Enrich()` call signature unchanged

### `internal/api/handlers.go` — Suggestions handler
- No logic changes required
- When `req.Provider` is empty, the existing fallback chain resolves to `SuggestionProvider` from config (now Claude Opus)
- NVIDIA and Gemini paths remain intact for explicit user selection

## Frontend Changes

### `AlertInsights.tsx`
- Remove `<select>` model dropdown (was Mistral Small / Gemma 3 27B)
- Replace with static badge showing `Claude Opus 4.7`
- Remove `selectedModel` state; regenerate button passes no model param (backend uses configured default)

### `MITREHeatmap.tsx` — Suggestions provider selector
Update dropdown options:

| Value | Label |
|-------|-------|
| `""` | Claude Opus (default) |
| `"nvidia"` | NVIDIA NIM (Nemotron) |
| `"gemini"` | Gemini 2.0 Flash |

Remove the explicit `"claude"` option (Claude is now the default, selecting `""` routes to it).

## What Does Not Change

- Provider interface (`llm.Provider`) — unchanged
- `claude.go`, `nvidia.go`, `gemini.go` implementations — unchanged
- Pipeline semaphore and concurrency model — unchanged
- Caching logic (insights 24h TTL, suggestions per-technique) — unchanged
- Validator/Classifier sidecar — unchanged (only the LLM confirmation stage switches to Claude)
- All API request/response shapes — unchanged

## Testing

- Existing unit tests for provider factory (`provider.go`) pass unchanged
- Insights handler test: verify provider constructed from config, not from `req.Model`
- Suggestions handler test: verify empty `provider` field resolves to Claude Opus
- Manual smoke test: run analyze → check insights report credits Claude Opus in `model` field
