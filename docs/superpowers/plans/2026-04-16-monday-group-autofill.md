# Monday Group ID Auto-Fill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-resolve Monday board group IDs at startup by fuzzy-matching client names, so operators only need `api_key` and `region` in `clients.yaml`.

**Architecture:** Add `FetchGroups()` to `monday.Client`, add a pure `resolveGroupIDs()` helper in `cmd/server`, and call both in `main.go` after `config.Load()` using the existing `ctx`. Clients with an explicit `monday_group_id` are never overwritten.

**Tech Stack:** Go stdlib (`strings`, `context`), `net/http/httptest` for tests.

---

### Task 1: Add `baseURL` field to `monday.Client` for testability

**Files:**
- Modify: `backend/internal/monday/client.go`

- [ ] **Step 1: Add `baseURL` to the `Client` struct and use it in `graphql()`**

In `backend/internal/monday/client.go`, replace:

```go
// Client queries the Monday.com GraphQL API.
type Client struct {
	apiToken string
	boardID  int64
}
```

with:

```go
// Client queries the Monday.com GraphQL API.
type Client struct {
	apiToken string
	boardID  int64
	baseURL  string // overrides apiURL constant; used in tests
}
```

And in `graphql()`, replace:

```go
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
```

with:

```go
	url := apiURL
	if c.baseURL != "" {
		url = c.baseURL
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/monday/client.go
git commit -m "refactor(monday): add baseURL field to Client for test overrides"
```

---

### Task 2: Add `Group` type and `FetchGroups` method to `monday.Client`

**Files:**
- Modify: `backend/internal/monday/client.go`
- Create: `backend/internal/monday/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/monday/client_test.go`:

```go
package monday

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGroups(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"data":{"boards":[{"groups":[{"id":"group_abc","title":"JioStar"},{"id":"group_def","title":"Deel"}]}]}}`)
	}))
	defer srv.Close()

	c := &Client{apiToken: "token", boardID: 123, baseURL: srv.URL}
	groups, err := c.FetchGroups(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].ID != "group_abc" || groups[0].Title != "JioStar" {
		t.Errorf("unexpected group[0]: %+v", groups[0])
	}
	if groups[1].ID != "group_def" || groups[1].Title != "Deel" {
		t.Errorf("unexpected group[1]: %+v", groups[1])
	}
}

func TestFetchGroups_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "internal error")
	}))
	defer srv.Close()

	c := &Client{apiToken: "token", boardID: 123, baseURL: srv.URL}
	_, err := c.FetchGroups(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestFetchGroups_EmptyBoard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"data":{"boards":[]}}`)
	}))
	defer srv.Close()

	c := &Client{apiToken: "token", boardID: 999, baseURL: srv.URL}
	_, err := c.FetchGroups(context.Background())
	if err == nil {
		t.Fatal("expected error for missing board, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/monday/... -v -run TestFetchGroups
```

Expected: FAIL with `c.FetchGroups undefined`.

- [ ] **Step 3: Add `Group` type and `FetchGroups` method to `client.go`**

Add after the `Integration` struct in `backend/internal/monday/client.go`:

```go
// Group represents a Monday.com board group.
type Group struct {
	ID    string
	Title string
}
```

Add after the `FetchIntegrations` method:

```go
// FetchGroups returns all groups on the configured board.
func (c *Client) FetchGroups(ctx context.Context) ([]Group, error) {
	query := fmt.Sprintf(`{ boards(ids: [%d]) { groups { id title } } }`, c.boardID)

	resp, err := c.graphql(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Boards []struct {
				Groups []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"groups"`
			} `json:"boards"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse monday groups: %w", err)
	}

	if len(result.Data.Boards) == 0 {
		return nil, fmt.Errorf("board %d not found", c.boardID)
	}

	groups := make([]Group, 0, len(result.Data.Boards[0].Groups))
	for _, g := range result.Data.Boards[0].Groups {
		groups = append(groups, Group{ID: g.ID, Title: g.Title})
	}
	return groups, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/monday/... -v -run TestFetchGroups
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/monday/client.go internal/monday/client_test.go
git commit -m "feat(monday): add Group type and FetchGroups method"
```

---

### Task 3: Add `resolveGroupIDs` helper

**Files:**
- Create: `backend/cmd/server/resolve.go`
- Create: `backend/cmd/server/resolve_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/cmd/server/resolve_test.go`:

```go
package main

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/monday"
)

func TestResolveGroupIDs(t *testing.T) {
	groups := []monday.Group{
		{ID: "group_abc", Title: "JioStar"},
		{ID: "group_def", Title: "Deel"},
		{ID: "group_ghi", Title: "Sinarmas - ASM"},
		{ID: "group_jkl", Title: "Sinarmas - Mining"},
	}

	tests := []struct {
		name       string
		clientName string
		existingID string
		expectedID string
	}{
		{
			name:       "exact match",
			clientName: "JioStar",
			expectedID: "group_abc",
		},
		{
			name:       "case-insensitive match",
			clientName: "jiostar",
			expectedID: "group_abc",
		},
		{
			name:       "client name contained in group title",
			clientName: "Deel",
			expectedID: "group_def",
		},
		{
			name:       "no match - stays empty",
			clientName: "Unknown",
			expectedID: "",
		},
		{
			name:       "already set - not overwritten",
			clientName: "JioStar",
			existingID: "existing_id",
			expectedID: "existing_id",
		},
		{
			name:       "multiple matches - uses first",
			clientName: "Sinarmas",
			expectedID: "group_ghi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Clients: map[string]config.ClientConfig{
					tt.clientName: {
						APIKey:        "key",
						Region:        "eu1",
						MondayGroupID: tt.existingID,
					},
				},
			}
			resolveGroupIDs(cfg, groups)
			got := cfg.Clients[tt.clientName].MondayGroupID
			if got != tt.expectedID {
				t.Errorf("client %q: expected group ID %q, got %q", tt.clientName, tt.expectedID, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./cmd/server/... -v -run TestResolveGroupIDs
```

Expected: FAIL with `resolveGroupIDs undefined`.

- [ ] **Step 3: Create `resolve.go`**

Create `backend/cmd/server/resolve.go`:

```go
package main

import (
	"log"
	"strings"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/monday"
)

// resolveGroupIDs populates MondayGroupID for clients that don't have one,
// by fuzzy-matching (case-insensitive contains) the client name against
// Monday board group titles. Clients with an explicit group ID are skipped.
func resolveGroupIDs(cfg *config.Config, groups []monday.Group) {
	for name, client := range cfg.Clients {
		if client.MondayGroupID != "" {
			continue
		}
		var matches []monday.Group
		lName := strings.ToLower(name)
		for _, g := range groups {
			lTitle := strings.ToLower(g.Title)
			if strings.Contains(lTitle, lName) || strings.Contains(lName, lTitle) {
				matches = append(matches, g)
			}
		}
		switch len(matches) {
		case 0:
			log.Printf("WARN [monday] no group match for client %q — Monday data will be skipped", name)
		case 1:
			client.MondayGroupID = matches[0].ID
			cfg.Clients[name] = client
			log.Printf("INFO [monday] resolved group for client %q → %s (%s)", name, matches[0].ID, matches[0].Title)
		default:
			client.MondayGroupID = matches[0].ID
			cfg.Clients[name] = client
			log.Printf("WARN [monday] multiple group matches for client %q, using first: %s (%s)", name, matches[0].ID, matches[0].Title)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./cmd/server/... -v -run TestResolveGroupIDs
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add cmd/server/resolve.go cmd/server/resolve_test.go
git commit -m "feat(server): add resolveGroupIDs helper for Monday group auto-fill"
```

---

### Task 4: Wire up in `main.go`

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add Monday group resolution after `config.Load()`**

In `backend/cmd/server/main.go`, after line 43 (`log.Printf("loaded config: %d clients", ...)`), insert:

```go
	// Auto-resolve Monday group IDs for clients that don't have one configured.
	{
		resolveCtx, resolveCancel := context.WithTimeout(context.Background(), 10*time.Second)
		mondayResolver := monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID)
		if groups, err := mondayResolver.FetchGroups(resolveCtx); err != nil {
			log.Printf("WARN [monday] could not fetch groups for auto-resolution: %v", err)
		} else {
			resolveGroupIDs(cfg, groups)
		}
		resolveCancel()
	}
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no output (clean build).

- [ ] **Step 3: Smoke test — restart server and check logs**

```bash
cd /Users/aviral.baloni/Desktop/claude && ./dev.sh restart
```

Check logs:
```bash
tail -20 /tmp/backend.log
```

Expected: lines like:
```
INFO [monday] resolved group for client "JioStar" → group_mkrzg3ma (JioStar)
```

- [ ] **Step 4: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add cmd/server/main.go
git commit -m "feat(server): auto-resolve Monday group IDs at startup"
```
