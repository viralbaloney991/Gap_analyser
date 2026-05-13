# Agentic Hunt — Multi-Model Tool Use Design

## Goal

Replace the current Olly → Claude pipe with a fully agentic hunt loop where the selected LLM (Claude or Gemini) autonomously runs `cx` DataPrime/Lucene queries as tools, discovers the real log schema, investigates the detection, and produces a complete 12-section threat hunt report. The model, query count, and investigation depth are all model-driven — no hard cap except a 30-turn safety net.

## Architecture

```
cx logs (1s) → query_done SSE
      ↓
AgentLoop (provider-agnostic)
  - model selection: ?model=claude | gemini (frontend dropdown)
  - initial message: lucene query + initial hits + sample events
  - loop (max 30 turns safety net):
      a. provider.NextTurn(messages, tools) → AgentTurnResponse
      b. tool_use → execute cx query → tool_call SSE → append tool_result → continue
      c. text → parseOllySections → olly_done SSE → report_ready SSE
```

Olly is removed from the production path. The pipe architecture (`runOllyChat` + `runClaudeHuntAnalysis`) is replaced by `runAgentLoop`.

## Files

**Modified:**
- `backend/internal/llm/provider.go` — add `AgentProvider` interface, `AgentTurnResponse`, `ToolDefinition`, `Message`, `ContentBlock`, `ToolUseBlock`, `ParameterSchema` types; add `NewAgentProvider(name, config)` factory
- `backend/internal/api/hunt.go` — add `runAgentLoop`, `huntTool` definition, `tool_call` SSE event; remove `runOllyChat` call and `runClaudeHuntAnalysis`; add `?model=` query param handling

**Created:**
- `backend/internal/llm/claude_agent.go` — `claudeAgentProvider` implementing `AgentProvider` via Anthropic messages API with `tools`
- `backend/internal/llm/gemini_agent.go` — `geminiAgentProvider` implementing `AgentProvider` via Gemini function calling API
- `backend/internal/llm/claude_agent_test.go` — message format unit tests
- `backend/internal/llm/gemini_agent_test.go` — message format unit tests
- `backend/internal/api/hunt_agent_test.go` — loop behaviour unit tests

## Data Models

```go
// llm package

type ToolDefinition struct {
    Name        string
    Description string
    Parameters  map[string]ParameterSchema
    Required    []string
}

type ParameterSchema struct {
    Type        string // "string" | "integer"
    Description string
}

type Message struct {
    Role    string         // "user" | "assistant"
    Content []ContentBlock
}

type ContentBlock struct {
    Type              string         // "text" | "tool_use" | "tool_result"
    Text              string
    ToolUseID         string
    ToolName          string
    ToolInput         map[string]any
    ToolResultContent string
    IsError           bool
}

type AgentTurnResponse struct {
    Type      string         // "tool_use" | "text"
    Text      string         // final report text when Type=="text"
    ToolCalls []ToolUseBlock // one or more tool calls when Type=="tool_use"
}

type ToolUseBlock struct {
    ID    string
    Name  string
    Input map[string]any
}

type AgentProvider interface {
    NextTurn(ctx context.Context, systemPrompt string, messages []Message, tools []ToolDefinition) (AgentTurnResponse, error)
    Name() string
}

func NewAgentProvider(name string, cfg ProviderConfig) (AgentProvider, error)
```

## Tool Exposed to the Model

One tool: `run_cx_query`

```json
{
  "name": "run_cx_query",
  "description": "Run a DataPrime or Lucene query against Coralogix logs. Use DataPrime (starting with 'source logs') for aggregations and Lucene for raw event retrieval. Returns up to 50 results or aggregated output.",
  "parameters": {
    "query":   {"type": "string", "description": "DataPrime (source logs | ...) or Lucene query string"},
    "window":  {"type": "string", "description": "Time window: 30d, 7d, 24h. Default: 30d"},
    "purpose": {"type": "string", "description": "One sentence: what this query is investigating (shown to user in real-time)"}
  },
  "required": ["query", "purpose"]
}
```

The `purpose` field is the user-facing label in the live feed.

## SSE Events

Existing events unchanged (`stream_opened`, `query_done`, `olly_done`, `report_ready`, `error`).

New event:
```
event: tool_call
data: {
  "query": "source logs | groupby $d.event_type aggregate count() as hits | orderby hits desc | limit 10",
  "purpose": "Checking event type distribution to understand log schema",
  "result_summary": "FlowExecution: 142k, URI: 89k, Login: 12k, ApiTotalUsage: 8k"
}
```

`result_summary` is the first 200 characters of the cx output.

## Agent System Prompt

The system prompt instructs the model to:
- Act as a senior threat hunting analyst with access to `run_cx_query`
- Start by discovering the real log schema (what fields exist, event types)
- Validate the detection query against actual field names
- Investigate anomalies and pivot based on findings
- Produce a 12-section numbered report (`## 1.` through `## 12.`) as the final text turn
- Use DataPrime syntax for all queries (reference included in system prompt)
- Never guess field names — always confirm with a query first

## Agent Loop (`runAgentLoop`)

```
func runAgentLoop(
    ctx context.Context,
    provider AgentProvider,
    cx cxExecutor,
    lucene string,
    window string,
    initialHits int,
    sampleEvents string,
    sseCallback func(event string, data any),
) (string, error)
```

1. Build initial user message: lucene + hits + samples
2. Loop (max 30 iterations):
   a. Call `provider.NextTurn(systemPrompt, messages, [huntTool])`
   b. If `AgentTurnResponse.Type == "text"`: return the text (final report)
   c. If `AgentTurnResponse.Type == "tool_use"`: for each ToolUseBlock:
      - Extract `query`, `window`, `purpose` from Input
      - Run `sanitizeQuery(query)` — if fails, return error as tool_result
      - Call `cx.runQuery(ctx, query, window)`
      - Build tool_result content block
      - Call `sseCallback("tool_call", toolCallData)`
      - Append assistant turn + tool_result to messages
3. If loop exhausts 30 turns: return whatever text was found in last assistant turn; if none, return error

## Provider Implementations

### Claude (`claudeAgentProvider`)

Translates `[]Message` → Anthropic messages format:
- `ContentBlock{Type:"tool_use"}` → `{"type":"tool_use","id":...,"name":...,"input":...}`
- `ContentBlock{Type:"tool_result"}` → `{"type":"tool_result","tool_use_id":...,"content":...}`

API call: `POST https://api.anthropic.com/v1/messages` with `tools` array.

Model: `claude-sonnet-4-6` (default, configurable via `ClaudeModel` in config).

### Gemini (`geminiAgentProvider`)

Translates `[]Message` → Gemini content format:
- `ToolDefinition` → `functionDeclarations` in `tools`
- `ContentBlock{Type:"tool_use"}` → `Part{FunctionCall: ...}`
- `ContentBlock{Type:"tool_result"}` → `Part{FunctionResponse: ...}` in a `user` role turn

API call: `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`

Model: `gemini-2.0-flash` (default, configurable via `GeminiModel` in config).

## `cxExecutor` Interface Change

Add `runQuery` method to handle both Lucene and DataPrime:

```go
type cxExecutor interface {
    runLogs(ctx context.Context, query, window string) ([]byte, error)  // existing
    runOllyChat(ctx context.Context, prompt string) ([]byte, error)      // existing, kept for tests
    runQuery(ctx context.Context, query, window string) ([]byte, error)  // new — used by agent tool
}
```

`runQuery` on `cxRunner` delegates to `runLogs` (cx logs handles both Lucene and DataPrime syntax). The separation exists so tests can mock tool execution independently.

## Frontend Changes

**`HuntView.tsx`:**
- Model selector dropdown: `claude` | `gemini` (default: `claude`)
- Pass `?model=<selected>` in hunt URL
- Handle `tool_call` SSE event: append to `toolCalls` state array
- Render live investigation feed: each item shows `purpose` + `result_summary`
- Show tool call count in the waiting state: "Claude has run 7 queries…"

**`api.ts`:** Pass `model` param in `startHunt()`

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| cx query syntax error | Error text returned as tool_result; model adjusts and retries |
| cx query shell injection | `sanitizeQuery` blocks it; error returned as tool_result |
| Provider API error mid-loop | Loop aborts; use partial text if available; fall back to error SSE |
| Model not configured | Immediate `provider_not_configured` error SSE before loop starts |
| 30-turn hard cap reached | Use last text content from assistant; if none, error SSE |
| 6-minute overall timeout | Context cancelled; error SSE with `hunt_timeout` |

## Testing

**`backend/internal/llm/claude_agent_test.go`:**
- `TestClaudeNextTurn_BuildsCorrectRequestFormat` — verify tools array, message format sent to Anthropic
- `TestClaudeNextTurn_ParsesToolUseResponse` — verify tool_use blocks parsed correctly
- `TestClaudeNextTurn_ParsesTextResponse` — verify final text response parsed correctly

**`backend/internal/llm/gemini_agent_test.go`:**
- `TestGeminiNextTurn_BuildsCorrectRequestFormat` — verify functionDeclarations format
- `TestGeminiNextTurn_ParsesFunctionCallResponse`
- `TestGeminiNextTurn_ParsesTextResponse`

**`backend/internal/api/hunt_agent_test.go`:**
- `TestAgentLoop_StopsOnText` — mock provider: 2× tool_use then text; verify 2 tool_call SSE events then report
- `TestAgentLoop_HardCapAt30` — mock always returns tool_use; verify exits at 30, returns partial
- `TestAgentLoop_ToolErrorContinues` — cx returns error; verify loop continues with error as tool_result
- `TestAgentLoop_SanitizesAgentQueries` — Claude-generated query with `;` blocked, error returned as tool_result
- `TestHandleHuntStream_ModelNotConfigured` — gemini model but no key; immediate error SSE
