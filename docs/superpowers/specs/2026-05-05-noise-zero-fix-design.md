---
title: Noise Always Zero — Fix Design
date: 2026-05-05
status: approved
---

# Bug Fix: Noise Detection Always Returns 0

## Problem

The Noise tab consistently shows 0 results for all clients, even when alerts are confirmed
to be firing more than 15 times in the selected time window (verified on the Coralogix
platform). Both behavioral and structural noise detection are silenced simultaneously.

## Root Cause Analysis

Two independent failures combine to produce this outcome.

### Failure 1 — `hasEvidenceOfVolume` gates structural detection (logic error)

In `internal/similarity/engine.go`, the structural noise check is:

```go
hasEvidenceOfVolume := eventCounts == nil || triggerCount > 0
isStructural = noEntity && hasEvidenceOfVolume && (isUnscoped || isBroadQuery)
```

When `fetchEventCounts` returns a non-nil but empty map `{}` (the API call succeeds but
returns no matching events), `triggerCount` is 0 for every alert and `hasEvidenceOfVolume`
is `false` for every alert. This silently blocks structural detection for the entire corpus.

This is a logic error. Structural noise is about **alert design quality** — an unscoped alert
with no entity filter and a broad Lucene query is noisy by construction, regardless of how
many times it has fired. Trigger frequency is only relevant to behavioral noise.

### Failure 2 — Event count fetch returns empty map (API/ID mismatch)

`FetchAlertEventCounts` in `internal/coralogix/client.go` initialises an empty `counts` map,
calls `ListAlertEvents`, and increments `counts[ev.AlertID]` per event. If no events are
returned, or if `ev.AlertID` doesn't match the `alert.ID` values from `ListAlertDefs` (e.g.
different ID format), the map remains empty and is returned as non-nil `{}`.

This is distinct from an API error (which returns `nil`). A non-nil empty map is indistinguishable
from "all alerts have 0 events" — but the platform confirms counts > 15. The exact cause
(field name mismatch, ID format divergence, or pagination issue) requires diagnostic logging
to identify.

## Fix Design

### Fix 1A — Remove `hasEvidenceOfVolume` from structural check

**File:** `internal/similarity/engine.go`

Remove the `hasEvidenceOfVolume` condition from `isStructural`:

```go
// Before
isStructural = noEntity && hasEvidenceOfVolume && (isUnscoped || isBroadQuery)

// After
isStructural = noEntity && (isUnscoped || isBroadQuery)
```

`hasEvidenceOfVolume` is retained as a computed variable for debug output only. The
`noSignalReasons` switch must also be reordered: `scoped_specific_query` must come before
`zero_triggers`, since `hasEvidenceOfVolume` is no longer a gate on structural detection.
Without this reordering, the debug log would record "zero_triggers" for scoped alerts with
triggerCount=0, masking the real reason (scoped/specific query).

**Impact:** Structural noise returns immediately for alerts that are unscoped or have broad
queries with no entity filter. Behavioral noise is unchanged.

### Fix 1B — Add match-rate diagnostic logging to `FetchAlertEventCounts`

**File:** `internal/coralogix/client.go`

After parsing the full paginated response, log:

```
INFO [noise] event counts: requested=N matched=M
```

Where `matched` = number of requested alert IDs that appear in the response with count > 0.
If `matched` is consistently 0 while `requested` > 0, the ID format mismatch is confirmed.
A follow-on fix will correct the mapping once the log output identifies the divergence.

## What Changes

| File | Change |
|---|---|
| `internal/similarity/engine.go` | Remove `hasEvidenceOfVolume` from `isStructural`; reorder `noSignalReasons` switch |
| `internal/coralogix/client.go` | Add match-rate log after response parse |
| `internal/similarity/engine_test.go` | Tests for structural noise with nil, empty, and populated event count maps |

## What Does Not Change

- Behavioral noise detection: still requires `triggerCount > behavioralNoiseThreshold` (10)
- Exclusion logic: vendor-covered, building blocks, non-security alerts still excluded
- `hasEvidenceOfVolume` is still computed — used only in debug output
- The `noiseType` string ("behavioral", "structural", "both") is unaffected

## Test Cases

1. Alert with no entity, unscoped, `eventCounts = nil` → structural noise fires
2. Alert with no entity, unscoped, `eventCounts = {}` (empty map) → structural noise fires (new)
3. Alert with no entity, unscoped, `eventCounts = {id: 0}` → structural noise fires (new)
4. Alert with entity filter → not flagged regardless of event counts
5. Alert with entity filter, `triggerCount = 20` → behavioral noise fires
6. Alert with no entity, scoped + specific query → not flagged
7. `noSignalReasons` debug log records `scoped_specific_query` for a scoped alert with
   triggerCount=0 (not `zero_triggers`, since `hasEvidenceOfVolume` is no longer the gate)
