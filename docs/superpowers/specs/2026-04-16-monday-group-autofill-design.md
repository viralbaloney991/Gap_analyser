# Monday Group ID Auto-Fill

**Date:** 2026-04-16  
**Status:** Approved

## Problem

Adding a new client to `clients.yaml` requires manually looking up the Monday board group ID — a friction point that requires API querying or UI digging.

## Goal

Allow operators to add a client with only `api_key` and `region`. The server resolves the Monday group ID automatically at startup by fuzzy-matching the client name against all group titles on the configured board.

## Approach

Startup-time resolution in `main.go` (one-shot, explicit, visible).

## Components

### 1. `monday.Client.FetchGroups(ctx context.Context) ([]Group, error)`

New method on the existing Monday client. Calls the same GraphQL endpoint using the existing `graphql()` helper.

```go
type Group struct {
    ID    string
    Title string
}
```

Query:
```graphql
{ boards(ids: [<boardID>]) { groups { id title } } }
```

Returns all groups on the board. Returns an error if the API call fails.

### 2. `resolveGroupIDs(cfg *config.Config, groups []monday.Group)`

Pure function in `main.go`. Iterates over all clients with an empty `MondayGroupID` and fuzzy-matches against the group list.

**Matching logic** (case-insensitive):
- `strings.Contains(groupTitle, clientName)` OR `strings.Contains(clientName, groupTitle)`
- Already-set group IDs are never overwritten
- No match → log warning, leave empty (Monday data skipped for this client)
- Multiple matches → log warning, use first match

Mutates `cfg.Clients` in place.

### 3. `main.go` startup sequence

After `config.Load()` and before handler creation:

```
1. config.Load()                          // parse YAML
2. monday.NewClient(token, boardID)       // create client
3. client.FetchGroups(ctx)                // fetch all groups
   → on error: log warning, skip resolution
4. resolveGroupIDs(cfg, groups)           // populate missing group IDs
5. api.NewHandler(cfg, ...)               // handlers see resolved config
```

## Error Handling

| Scenario | Behaviour |
|---|---|
| Monday API unreachable at startup | Log warning, skip resolution, continue |
| Client name matches no group | Log warning per client, proceed with empty group ID |
| Client name matches multiple groups | Log warning, use first match |
| Client already has explicit `monday_group_id` | Never overwritten |

The server always starts. Worst case is a client has no Monday data — identical to today's behaviour when `monday_group_id` is missing.

## Testing

### `FetchGroups`
- Mock HTTP server (`httptest.NewServer`) returning a fixed GraphQL response
- Verify parsed `[]Group` matches expected IDs and titles
- Verify error returned on non-200 response

### `resolveGroupIDs` (table-driven unit tests)
| Case | Input | Expected |
|---|---|---|
| Exact match | client `"JioStar"`, group `"JioStar"` | ID resolved |
| Case-insensitive | client `"jiostar"`, group `"JioStar"` | ID resolved |
| Partial (contains) | client `"JioStar"`, group `"JioStar - P1 - SRC"` | ID resolved |
| No match | client `"Unknown"`, no matching group | Empty, warning logged |
| Multiple matches | client `"Sinarmas"`, groups `"Sinarmas - ASM"`, `"Sinarmas - Mining"` | First match used, warning logged |
| Already set | client has explicit group ID | Not overwritten |

## Config After This Change

Minimal client entry (no `monday_group_id` needed):

```yaml
clients:
  JioStar:
    api_key: "cxup_..."
    region: ap1
```

Explicit override still works:

```yaml
clients:
  JioStar:
    api_key: "cxup_..."
    region: ap1
    monday_group_id: "group_mkrzg3ma"  # explicit, never overwritten
```
