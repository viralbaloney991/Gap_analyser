# Hunt Pipeline Improvements Design

## Goal

Fix three problems with the current two-pass Olly hunt pipeline:
1. Hit count shown in the UI is wrong (cx logs JSON is a bare array; our parser counts lines of text instead of events)
2. Pass 1 prompt is hardcoded for webhook/URL detection — useless for any other query type
3. Pass 2 report buries the actual findings in section 8 of 12; section 7 produces analyst-workflow guidance text that nobody reads

## Changes

### 1. Fix cx logs hit count

**Problem:** `cx logs --output json` returns a bare JSON array `[{...}, {...}]`. Our `parseLogsOutput` tries to unmarshal it into `struct{ Hits int; Events []struct{...} }`, fails, and falls back to `len(strings.Split(raw, "\n"))` — counting lines of pretty-printed JSON, not events. For a 2-event response spanning ~80 lines, `qd.Hits` = 80.

**Fix:** Change `cxRunner.runLogs` from `--output json` to `--output agents`. The agents format uses DataPrime paths (`$m`, `$l`, `$d`) matching what Olly understands. Unmarshal the response as `[]json.RawMessage` to get the real array length. `qd.Hits` = `len(events)` from the sample (honest: "N events retrieved, up to 50").

**True total count:** Pass 1 is the authoritative source (see §2). After pass 1 completes, extract the total from Olly's text with `regexp: Total:\s*(\d+)`. If found, overwrite `qd.Hits` before building the final `huntReport`. If not found, use the sample count unchanged.

The `query_done` SSE fires immediately with the sample count. The corrected count appears in `report_ready`.

### 2. Rewrite pass 1 prompt — generic schema discovery + count

**Problem:** Current questions 1–2 ask specifically about `$d.url`, `webhook.site`, `discord.com/api/webhooks`. For a hunt on IFEO registry persistence or PowerShell encoding, these questions return nothing useful.

**New 4-question prompt (generic):**

```
Schema discovery for threat hunt. Answer ONLY these 4 questions:

Query: {{.LuceneQuery}}
Sample events ({{.SampleCount}} retrieved from last {{.Window}}):
{{.SampleEvents}}

1. Field mapping: for each key term in the Lucene query, find the matching DataPrime $d path
   in the sample events. Which confirmed paths have data?

2. Pattern match: translate the Lucene query to DataPrime using the confirmed fields from
   question 1. Run it. Do the specific IOCs/patterns actually appear? How many events match?

3. True total: run source logs | filter <your DataPrime translation> | count() as total
   Report the exact number on its own line as: "Total: N"

4. Top patterns: source logs | filter <condition> | groupby <most relevant field> aggregate
   count() as hits | orderby hits desc | limit 5

Facts only. No preamble.
```

The `"Total: N"` format on its own line makes extraction reliable. The `{{.Window}}` template variable is added to both prompt and `ollySchemaPromptData` struct.

### 3. Rewrite pass 2 prompt — 11 sections, findings first

**Dropped:** Section 7 "Hunt Workflow" (step-by-step for a tier-2 analyst) — produces guidance text, not findings.

**New structure — "What We Found" leads:**

```
## 1. What We Found
RUN the confirmed DataPrime query. Report actual results:
- Total hits (use the count from schema discovery)
- Top 5 users / actors involved
- Top 5 source IPs
- Time distribution: clustered within minutes (automated/tool) or spread over hours/days (human)?
- Any standout anomalies in the data

## 2. Hunt Summary
Severity (Critical/High/Medium/Low), Confidence (High/Medium/Low), MITRE ATT&CK tactic/technique.
2-3 sentences on what this query detects and why it matters.

## 3. Original Query
Echo the Lucene query verbatim.

## 4. Schema Mapping
Table: Lucene field | Confirmed $d path | Has data (Y/N)

## 5. Translated Query — DataPrime
Using ONLY confirmed fields from §4.

## 6. Translated Query — Lucene
Optimised Lucene with confirmed field paths.

## 7. Detection Logic
Plain English: what does this detect, what attacker behaviour does it expose, why it matters.

## 8. False Positive Sources
Likely benign causes with suppression suggestions.

## 9. Visibility Gaps
Missing log sources or fields that limit confidence in this hunt.

## 10. Follow-up Hunts
3 related hunts with DataPrime query sketches.

## 11. Alert Definition
Name: <descriptive>
Type: standard
Condition: count > 0
Severity: <level>
Group-By: <most relevant confirmed field>
```

**Backend section number updates required:**

| Call site | Old section | New section |
|-----------|-------------|-------------|
| `deriveVerdict` | `sections["1"]` (Hunt Summary) | `sections["2"]` |
| `report.Subtitle` | `sections["1"]` | `sections["2"]` |
| `extractFindings` | `"1"`, `"6"`, `"10"` | `"1"`, `"7"`, `"9"` |
| `extractActions` | `sections["11"]` | `sections["10"]` |
| `parseAlertDef` | `sections["12"]` | `sections["11"]` |

The `numberedHeaderRe` already handles `## **N.` bold format — no change needed there.

### 4. Surface chat_url as "Continue in Olly" button

**How it works:** Olly embeds a `chat_url` link in its response text. The section parser already extracts it into `sections["chat_url"]`. It is never sent to the frontend today.

**Backend:** Add `ContinueURL string json:"continue_url"` to `huntReport`. After parsing sections, set `report.ContinueURL = sections["chat_url"]`.

**Frontend:** When `report.continue_url` is non-empty, render a "Continue in Olly →" button/link that opens the URL in a new tab. The button lives near the hunt report header. If the field is empty, the button is hidden.

**Fallback:** Olly does not always include a chat_url. Empty string = no button shown. No error.

## Files Changed

**Modified:**
- `backend/internal/api/hunt.go` — all four changes above
- `frontend/src/components/HuntStream.tsx` (or equivalent) — "Continue in Olly →" button

**Test changes:**
- `backend/internal/api/hunt_test.go` — update mock `logsOutput` to bare JSON array format; update section number references in existing tests; add `TestExtractTotalFromPass1` for the regex extraction; add `TestParseLogsOutputBareArray` for the new array parser

## Out of Scope

- Changing the SSE event names or adding new SSE events
- Changing the time window for cx logs
- Changing the model or mode used for either pass
- Any UI redesign beyond the single "Continue in Olly →" button
