# Two-Pass Olly Hunt Design

## Goal

Replace the current single-shot Olly call with a two-pass chat-continuation approach that delivers full 12-section threat hunt reports with live pivot findings, without Cloudflare 524 timeouts. All analysis stays inside Coralogix infrastructure.

## Background

Single-shot Olly with a full 12-section prompt causes Cloudflare 524 timeouts (>5 min) because Olly runs too many sub-queries at once. A focused 3-question schema-discovery pass completes in ~72s and returns a `chat_id`. A second pass continuing that chat with `--mode skill` uses confirmed field names to run targeted pivots and produce the full report in ~216s. Total: ~5 min, no timeout.

**Tested and validated** on the Deel webhook exfiltration query — pass 2 ran live pivots and returned 7 real usernames, source IPs, time-clustering analysis, and verdict from actual data.

## Architecture

```
cx logs (1s)
      ↓ query_done SSE
Pass 1: cx olly ask --mode focus --output agents
  3-question schema discovery (~72s)
  → Returns chat_id + confirmed field names
      ↓
Pass 2: cx olly ask --chat-id <id> --mode skill --model claude-sonnet-4-5
  Full 12-section report with live pivots (~216s)
  → Uses only confirmed fields, runs §8 pivot queries for real findings
      ↓ olly_done SSE (sections parsed)
      ↓ report_ready SSE
```

## Files Changed

**Modified:**
- `backend/internal/api/hunt.go` — two-pass Olly flow, new prompts, chat_id extraction

## Prompts

### Pass 1 — Schema Discovery (gpt-5.2, focus mode, ~72s)

```
Schema discovery for threat hunt. Answer ONLY these 3 questions:

Query: <lucene_query>
Hits: <hit_count>
<sample_events>

1. What URL/URI/path/link fields exist in these logs? Check: $d.url, $d.uri,
   $d.cx_security.uri, $d.Island.details.file_processing_details.urls, $d.Url,
   $d.MessageURLs, $d.page — which ones have data?
2. Do webhook.site, discord.com/api/webhooks, hooks.slack.com, or zapier.com/hooks
   appear in any URL field? How many hits each?
3. Top 5 non-null event types:
   source logs | filter $d.event_type != null | groupby $d.event_type aggregate count() as hits | orderby hits desc | limit 5

Facts only. No preamble.
```

### Pass 2 — Full Hunt Report (claude-sonnet-4-5, skill mode, ~216s)

Sent to same chat via `--chat-id`. Instructs Olly to use confirmed fields and actively run pivot queries for §8.

```
Using what you found above, write the complete threat hunt report.
For section 8 Pivot Investigation, RUN actual queries and report real findings.

## 1. Hunt Summary
Severity (Critical/High/Medium/Low), Confidence (High/Medium/Low), MITRE ATT&CK Tactic/Technique

## 2. Original Query
Echo the Lucene query

## 3. Schema Mapping
Table: Original Field | Confirmed CX Field | Exists (Y/N)

## 4. Translated Query — DataPrime
Using ONLY confirmed fields from section 3

## 5. Translated Query — Lucene
Optimised Lucene with confirmed fields

## 6. Detection Logic Explained
Plain English explanation of what this detects and why

## 7. Hunt Workflow
Step-by-step for a tier-2 analyst investigating a hit

## 8. Pivot Investigation
RUN these now and report actual findings:
- Top domains/URLs in the confirmed URL fields
- Top users or source IPs generating these events
- Are hits time-clustered (automated) or spread (manual)?

## 9. False Positive Considerations
Likely FP sources with suppression suggestions

## 10. Visibility Gaps
Missing log sources or fields that limit this hunt

## 11. Follow-up Hunts
3 related hunts with DataPrime query sketches

## 12. Alert Definition
Name: <name>
Type: standard
Condition: count > 0
Severity: <level>
Group-By: <field>
```

## chat_id Extraction

Pass 1 is run with `--output agents` which returns structured output:

```
[1]{chat_id,interaction_id,status,response,interaction_mode,model_choice}:
  "3fe8d46b-ea69-4701-808d-43ac5c21ba55","fa5cc9f0-...",completed,"<response>",focus,"gpt-5.2"
```

Extract `chat_id` by matching the first UUID pattern: `"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"`.

Extract the response text: everything between the 4th `"` pair (after `completed,`). Since response content contains escaped markdown, parse with a Go regex that matches the agents format.

Pass 2 uses `--output text` (default) for simpler response parsing — only pass 1 needs `--output agents` for the `chat_id`.

## cx Interface Changes

Current `cxExecutor`:
```go
type cxExecutor interface {
    runLogs(ctx context.Context, query, window string) ([]byte, error)
    runOllyChat(ctx context.Context, prompt string) ([]byte, error)
}
```

Updated:
```go
type cxExecutor interface {
    runLogs(ctx context.Context, query, window string) ([]byte, error)
    runOllySchema(ctx context.Context, prompt string) (agentsOutput []byte, err error) // pass 1, --output agents
    runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error)           // pass 2, --chat-id, --mode skill
}
```

`runOllyChat` removed from interface (kept on `cxRunner` for backward compat with existing tests, deprecated).

### `cxRunner` implementations

```go
func (r *cxRunner) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
    // cx olly ask --output agents --mode focus --timeout 300 "<prompt>"
    cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask",
        "--output", "agents", "--mode", "focus", "--timeout", "300", prompt)
    cmd.Env = r.env()
    return readCapped(cmd, maxOutputBytes)
}

func (r *cxRunner) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
    // cx olly ask --chat-id <id> --mode skill --model claude-sonnet-4-5 --timeout 600 "<prompt>"
    cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask",
        "--chat-id", chatID, "--mode", "skill", "--model", "claude-sonnet-4-5",
        "--timeout", "600", prompt)
    cmd.Env = r.env()
    return readCapped(cmd, maxOutputBytes)
}
```

## `HandleHuntStream` Flow

```go
// Step 2a: Pass 1 — schema discovery
schemaPrompt, _ := buildOllySchemaPrompt(lucene, qd.Hits, sampleText)
agentsOut, err := cx.runOllySchema(ctx, schemaPrompt)
// → error → send error SSE

chatID, schemaText := parseAgentsOutput(agentsOut)
// → empty chatID → fall back to single-pass with runOllyReport(ctx, "", reportPrompt)

// Step 2b: Pass 2 — full report
reportPrompt := buildOllyReportPrompt()
ollyOut, err := cx.runOllyReport(ctx, chatID, reportPrompt)
// → error → send error SSE

sections := parseOllySections(string(ollyOut))
sendSSE(w, flusher, "olly_done", ollyDoneData{Sections: sections})
```

## `parseAgentsOutput`

```go
// parseAgentsOutput extracts chat_id and response text from --output agents format.
// Format: [1]{fields}: "uuid","uuid",completed,"<response>",mode,"model"
func parseAgentsOutput(data []byte) (chatID, response string) {
    // Extract first UUID → chat_id
    uuidRe := regexp.MustCompile(`"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"`)
    if m := uuidRe.FindSubmatch(data); len(m) > 1 {
        chatID = string(m[1])
    }
    // Extract response: between completed," and ",<mode>,"
    respRe := regexp.MustCompile(`completed,"([\s\S]+?)",(?:focus|skill|fast),`)
    if m := respRe.FindSubmatch(data); len(m) > 1 {
        response = string(m[1])
    }
    return
}
```

## Section Format

Pass 2 produces `## 1. Title` through `## 12. Title`. The existing `numberedHeaderRe` parser handles this without changes.

No `§` symbol — simpler and consistent with how Olly naturally formats numbered sections.

## Timeout Configuration

| Timeout | Value | Rationale |
|---------|-------|-----------|
| Pass 1 cx `--timeout` | 300s | Schema discovery completes in ~72s; buffer for slow queries |
| Pass 2 cx `--timeout` | 600s | Full investigation with pivots; tested at ~216s |
| Overall hunt context timeout | 10 min | Increased from 6 min to cover both passes |
| HTTP `WriteTimeout` | 12 min | Server write timeout covers full hunt duration |

## Fallback

If `parseAgentsOutput` returns an empty `chatID` (network error, unexpected format), fall back to a single `runOllyReport(ctx, "", reportPrompt)` call (no `--chat-id`, no schema context). Less accurate but never silently fails.

## Testing

**Unit tests (`hunt_test.go`):**
- `TestParseAgentsOutput_ExtractsChatID` — valid agents output → correct UUID
- `TestParseAgentsOutput_ExtractsResponse` — valid agents output → correct response text
- `TestParseAgentsOutput_EmptyOnBadInput` — malformed output → empty strings, no panic
- `TestBuildOllySchemaPrompt` — template renders with all fields
- `TestBuildOllyReportPrompt` — report prompt contains all 12 section headers

**Integration test (skipped in CI):**
- `TestHandleHuntStream_TwoPassE2E` — real cx, real Olly; verify `chat_id` extracted, two cx calls made, `olly_done` contains sections 1-12
