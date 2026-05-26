# Olly Hunt Pipeline Upgrade Design

**Date:** 2026-05-26  
**Status:** Approved  
**Scope:** `backend/internal/api/hunt.go` only — no SSE protocol changes, no frontend changes

---

## Problem

The current two-pass Olly hunt pipeline in `hunt.go` has three stale configurations:

1. **Stale model:** Both passes use `claude-sonnet-4-5`. `claude-sonnet-4-6` is now available with stronger analytical reasoning. `gpt-5.4` is now available and recommended for Pro mode's structured, fast tasks.
2. **No timeout recovery:** A Cloudflare 524 or subprocess timeout kills the entire hunt with no retry. `olly.py` (the baselining system) solved this with a progressive back-off recovery loop.
3. **Weak Pass 1 schema prompt:** The schema discovery prompt has no knowledge of `cx_security.*` normalized fields or log source context. Olly must discover these from scratch each time, wasting Pass 1's token budget on fields it misses.

Additionally: no `--read-only` flag on either pass, leaving Olly free to make write operations in a fully-automated pipeline.

---

## Design

### 1. Model Split (B)

Pass 1 and Pass 2 are different workloads and should use different models:

| Pass | Workload | Model | Mode |
|------|----------|-------|------|
| Pass 1 — Schema Discovery | 4 focused questions, structured output, speed matters | `gpt-5.4` | `--mode skill` |
| Pass 2 — Full Report | 11-section long-form analysis, reasoning depth matters | `claude-sonnet-4-6` | `--mode skill` |

Both remain in `--mode skill` (Pro mode), which auto-loads Olly's built-in `dataprime`, `logs`, and `alerts` skills based on prompt context. No explicit skill invocation is needed.

**Constants in `hunt.go`:**
```go
const ollyPass1Model = "gpt-5.4"
const ollyPass2Model = "claude-sonnet-4-6"
```

The existing single `ollyModel` constant is replaced by two.

### 2. Pass 1 Prompt Enrichment (C)

The Pass 1 schema discovery prompt gains a **Tier 1 normalized fields** block and **log source context** derived from the Lucene query. This is the key pattern from `olly.py`.

**Two additions to `buildOllySchemaPrompt`:**

**a) Log source detection** — a pure-Go function `detectLogSource(luceneQuery string) string` that pattern-matches the query against known vendor keywords and returns one of: `aws`, `gcp`, `azure`, `okta`, `crowdstrike`, `paloalto`, `fortinet`, `cloudflare`, `m365`, `gworkspace`, `github`, `sentinelone`, `generic`. Same logic as `olly.py._detect_log_source` but operating only on the Lucene query string (no alert metadata available in the hunt context).

**b) Tier 1 cx_security field injection** — the schema prompt template gains a new section:

```
Known normalized fields (cx_security.* — prefer these in DataPrime translation if present):
  cx_security.username, cx_security.email, cx_security.target_username,
  cx_security.target_email, cx_security.source_ip, cx_security.destination_ip,
  cx_security.source_hostname, cx_security.userAgent, cx_security.event_name,
  cx_security.event_result, cx_security.event_type, cx_security.resource
  GeoIP: cx_security.source_ip_geoip.asn.number, .asn.organization, .country_name

Log source detected: {{.LogSource}}
Source-specific supplementary fields:
{{.SourceFields}}
```

The `ollySchemaPromptData` struct gains `LogSource string` and `SourceFields string`. `SourceFields` is a formatted string from a `_RAW_SUPPLEMENTARY_FIELDS` map (ported from `olly.py`) keyed by the detected log source.

**c) CONCISE_PREFIX** — Pass 1 gets a brief prefix: `"Be concise and structured. Output only what's asked. Use compact tables. No preamble."` Pass 2 does not — it needs expressive long-form output.

### 3. Timeout Recovery

Both `runOllySchema` and `runOllyReport` can time out (Cloudflare 524 or subprocess expiry). The recovery pattern from `olly.py`:

**New helper: `runWithRecovery(ctx, cx, step, chatID, prompt, timeout, maxRetries) ([]byte, error)`**

Behaviour:
1. Run the primary invocation.
2. If it exits cleanly with output → return immediately.
3. If it times out AND `chatID` is non-empty → enter recovery loop:
   - Sleep: 45s → 60s → 75s → 90s → 105s (up to `maxRetries = 5`)
   - Each recovery sends a short ping (`"Are you done? Return your best answer so far."`) to the same `chat_id`
   - On first non-empty reply → return it
4. If all recoveries exhausted → return error.

For Pass 1, `chatID` is empty on the first call, so no recovery is attempted (no chat to recover). Recovery only applies to Pass 2 (which has a `chatID` from Pass 1).

**Timeout values:**
- Pass 1: `--timeout 300` (unchanged, 4-question prompt is fast)
- Pass 2: `--timeout 900` (raised from 600 to match cx default)

### 4. `--read-only` Flag

Both `runOllySchema` and `runOllyReport` add `--read-only` to their `cx olly ask` invocations. This blocks Olly from creating alerts, modifying configs, or any write operation during the automated hunt.

### 5. Output Format

Both passes keep `--output agents`. The agents format returns structured CSV-style output with `chat_id` embedded, which `parseAgentsOutput` already handles. No change to the parser.

---

## Files Changed

**Modified:**
- `backend/internal/api/hunt.go` — all changes above

**Not changed:**
- `frontend/` — no UI changes
- SSE event names — unchanged
- Section numbers in the 11-section report — unchanged
- `HandleHuntExport`, `serializeReportToMarkdown` — unchanged
- `HandleHuntStream` orchestration logic — unchanged except calling `runWithRecovery` instead of direct cx calls

---

## Test Changes

**`backend/internal/api/hunt_test.go`:**
- Replace `ollyModel` references with `ollyPass1Model` / `ollyPass2Model`
- Add `TestDetectLogSource` — table-driven tests for each source keyword pattern
- Add `TestBuildOllySchemaPromptEnrichment` — verify cx_security fields appear in rendered prompt
- Add `TestRunWithRecovery_TimesOut` — mock executor that always times out; verify error returned after 5 retries
- Add `TestRunWithRecovery_RecoverySucceeds` — mock that times out on primary, succeeds on first ping
- Existing tests: update mock executor to return `--output agents` format (already done in previous plan)

---

## Out of Scope

- Changing the 2-pass architecture to 3+ passes
- Custom Olly skill files (Markdown-managed in Coralogix UI)
- Frontend changes (HuntView.tsx, HuntView.css)
- Adding new SSE events
- Changing the section parser or verdict derivation logic
- Changing the `cx logs` call (Pass 0) — already correct
- Adding Gemini models (no chat continuity guarantee via `--chat-id` confirmed)
