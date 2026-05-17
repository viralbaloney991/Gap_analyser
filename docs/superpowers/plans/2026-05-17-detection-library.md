# Detection Library & Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist generated detections to a global NeonDB library, add a card-grid Library view with client/severity filters, Sigma zip export, and a per-card Coralogix push button.

**Architecture:** New `saved_detections` table in NeonDB with store CRUD methods and five REST handlers in `backend/internal/api/library.go`. Frontend adds `LibraryView` and `DetectionCard` components, a `Library` nav button in the app header, and Save buttons in the Builder and MITRE Suggestions panels.

**Tech Stack:** Go `pgx/v5`, `archive/zip`, standard `net/http`; React + TypeScript; Coralogix REST API (`/api/v1/external/alerts`).

---

## File Map

| File | Role |
|------|------|
| `backend/internal/store/store.go` | Add `SavedDetection`, `DetectionFilter` structs + `migrate()` DDL + 4 CRUD methods |
| `backend/internal/store/library_store_test.go` | Integration tests for all store methods |
| `backend/internal/models/library.go` | `SaveDetectionRequest`, `LibraryResponse`, `PushResponse` |
| `backend/internal/api/library.go` | 5 handler functions (`Save`, `List`, `Delete`, `Export`, `Push`) |
| `backend/cmd/server/main.go` | Register 5 new routes |
| `frontend/src/types/index.ts` | Add `SavedDetection`, `SaveDetectionRequest`, `LibraryResponse` |
| `frontend/src/services/api.ts` | Add 5 library API functions |
| `frontend/src/components/DetectionCard.tsx` | Reusable saved-detection card |
| `frontend/src/components/LibraryView.tsx` | Full library page with filter bar + card grid |
| `frontend/src/components/DetectionBuilder.tsx` | Add Save button to each generated alert card |
| `frontend/src/components/MITREHeatmap.tsx` | Add Save button to each suggestion card |
| `frontend/src/App.tsx` | Add `'library'` view, Library nav button, breadcrumb label |
| `frontend/src/App.css` | Library + card styles |

---

## Task 1: Store — SavedDetection model + migration

**Files:**
- Modify: `backend/internal/store/store.go`

- [ ] **Step 1: Add `SavedDetection` and `DetectionFilter` structs** at the bottom of `store.go` (before closing brace, after `CorrelationRow`):

```go
// SavedDetection is a persisted detection from the Builder or Suggestions panel.
type SavedDetection struct {
	ID             string    `json:"id"`
	Client         string    `json:"client"`
	Source         string    `json:"source"` // "builder" | "suggestions"
	Title          string    `json:"title"`
	TechniqueID    string    `json:"technique_id"`
	Tactic         string    `json:"tactic"`
	LuceneQuery    string    `json:"lucene_query"`
	SigmaRule      string    `json:"sigma_rule"`
	Severity       string    `json:"severity"`
	LogSource      string    `json:"log_source"`
	Falsepositives []string  `json:"falsepositives"`
	CreatedAt      time.Time `json:"created_at"`
}

// DetectionFilter controls which saved detections ListDetections returns.
type DetectionFilter struct {
	Client      string // empty = all clients
	TechniqueID string // empty = all techniques
	Severity    string // empty = all severities
	Limit       int    // 0 = use default (100)
}
```

- [ ] **Step 2: Add `saved_detections` DDL to `migrate()`**. Find the existing `migrate()` function in `store.go`. Append the new table DDL inside the same `pool.Exec` call, after the `correlation_cache` index line and before the closing backtick:

```sql
CREATE TABLE IF NOT EXISTS saved_detections (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client         TEXT        NOT NULL,
    source         TEXT        NOT NULL CHECK (source IN ('builder', 'suggestions')),
    title          TEXT        NOT NULL,
    technique_id   TEXT        NOT NULL,
    tactic         TEXT        NOT NULL,
    lucene_query   TEXT        NOT NULL,
    sigma_rule     TEXT        NOT NULL,
    severity       TEXT        NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    log_source     TEXT        NOT NULL,
    falsepositives TEXT[]      NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS saved_detections_client_idx    ON saved_detections (client);
CREATE INDEX IF NOT EXISTS saved_detections_technique_idx ON saved_detections (technique_id);
CREATE INDEX IF NOT EXISTS saved_detections_created_idx   ON saved_detections (created_at DESC);
```

- [ ] **Step 3: Verify the project still compiles**

```bash
cd backend && go build ./...
```

Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/store/store.go
git commit -m "feat(store): add SavedDetection model and saved_detections migration"
```

---

## Task 2: Store — CRUD methods + tests

**Files:**
- Modify: `backend/internal/store/store.go`
- Create: `backend/internal/store/library_store_test.go`

- [ ] **Step 1: Write the failing test** — create `backend/internal/store/library_store_test.go`:

```go
package store_test

import (
	"context"
	"testing"

	"coralogix-alert-analyzer/internal/store"
)

func TestSaveAndListDetection(t *testing.T) {
	s := newStore(t) // uses helper from store_test.go
	ctx := context.Background()

	d := store.SavedDetection{
		Client:         "test-client-" + t.Name(),
		Source:         "builder",
		Title:          "Detect Valid Account Abuse via Anomalous Logon",
		TechniqueID:    "T1078",
		Tactic:         "initial-access",
		LuceneQuery:    `event.category:authentication AND event.outcome:"success"`,
		SigmaRule:      "title: test\nlogsource:\n  product: windows\n",
		Severity:       "high",
		LogSource:      "EDR",
		Falsepositives: []string{"Break-glass admin"},
	}

	id, err := s.SaveDetection(ctx, d)
	if err != nil {
		t.Fatalf("SaveDetection: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	results, err := s.ListDetections(ctx, store.DetectionFilter{Client: d.Client})
	if err != nil {
		t.Fatalf("ListDetections: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 detection, got %d", len(results))
	}
	if results[0].Title != d.Title {
		t.Errorf("want title %q, got %q", d.Title, results[0].Title)
	}
	if results[0].ID != id {
		t.Errorf("want id %q, got %q", id, results[0].ID)
	}
}

func TestDeleteDetection(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	d := store.SavedDetection{
		Client: "test-delete-" + t.Name(), Source: "suggestions",
		Title: "Test", TechniqueID: "T1059", Tactic: "execution",
		LuceneQuery: "process.name:powershell.exe", SigmaRule: "title: test",
		Severity: "medium", LogSource: "Windows",
	}
	id, err := s.SaveDetection(ctx, d)
	if err != nil {
		t.Fatalf("SaveDetection: %v", err)
	}

	if err := s.DeleteDetection(ctx, id); err != nil {
		t.Fatalf("DeleteDetection: %v", err)
	}

	results, err := s.ListDetections(ctx, store.DetectionFilter{Client: d.Client})
	if err != nil {
		t.Fatalf("ListDetections after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 detections after delete, got %d", len(results))
	}
}

func TestGetDetection(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	d := store.SavedDetection{
		Client: "test-get-" + t.Name(), Source: "builder",
		Title: "Detect Cred Dump via LSASS", TechniqueID: "T1003.001",
		Tactic: "credential-access", LuceneQuery: "process.name:lsass.exe",
		SigmaRule: "title: lsass", Severity: "critical", LogSource: "EDR",
		Falsepositives: []string{"AV scanning"},
	}
	id, err := s.SaveDetection(ctx, d)
	if err != nil {
		t.Fatalf("SaveDetection: %v", err)
	}

	got, err := s.GetDetection(ctx, id)
	if err != nil {
		t.Fatalf("GetDetection: %v", err)
	}
	if got.TechniqueID != "T1003.001" {
		t.Errorf("want technique T1003.001, got %s", got.TechniqueID)
	}
	if len(got.Falsepositives) != 1 || got.Falsepositives[0] != "AV scanning" {
		t.Errorf("falsepositives mismatch: %v", got.Falsepositives)
	}
}

func TestListDetections_FilterBySeverity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	client := "test-filter-" + t.Name()

	for _, sev := range []string{"high", "medium", "low"} {
		_, err := s.SaveDetection(ctx, store.SavedDetection{
			Client: client, Source: "builder", Title: "T " + sev,
			TechniqueID: "T1078", Tactic: "initial-access",
			LuceneQuery: "x:y", SigmaRule: "title: t", Severity: sev, LogSource: "EDR",
		})
		if err != nil {
			t.Fatalf("SaveDetection %s: %v", sev, err)
		}
	}

	results, err := s.ListDetections(ctx, store.DetectionFilter{Client: client, Severity: "high"})
	if err != nil {
		t.Fatalf("ListDetections: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("want 1 high-severity detection, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail (NEON_DSN required)**

```bash
cd backend && NEON_DSN="$NEON_DSN" go test ./internal/store/... -run TestSave -v 2>&1 | head -20
```

Expected: `FAIL` with `undefined: store.SaveDetection` (or skip if `NEON_DSN` not set).

- [ ] **Step 3: Implement the four store methods** — add to `store.go` after `AppendCachedCorrelations`:

```go
// SaveDetection inserts a new detection into saved_detections and returns its UUID.
func (s *Store) SaveDetection(ctx context.Context, d SavedDetection) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO saved_detections
			(client, source, title, technique_id, tactic, lucene_query, sigma_rule, severity, log_source, falsepositives)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, d.Client, d.Source, d.Title, d.TechniqueID, d.Tactic,
		d.LuceneQuery, d.SigmaRule, d.Severity, d.LogSource, d.Falsepositives,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert saved_detections: %w", err)
	}
	return id, nil
}

// ListDetections returns saved detections matching the filter, newest first.
func (s *Store) ListDetections(ctx context.Context, f DetectionFilter) ([]SavedDetection, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, client, source, title, technique_id, tactic,
		       lucene_query, sigma_rule, severity, log_source, falsepositives, created_at
		FROM saved_detections
		WHERE ($1 = '' OR client = $1)
		  AND ($2 = '' OR technique_id = $2)
		  AND ($3 = '' OR severity = $3)
		ORDER BY created_at DESC
		LIMIT $4
	`, f.Client, f.TechniqueID, f.Severity, limit)
	if err != nil {
		return nil, fmt.Errorf("query saved_detections: %w", err)
	}
	defer rows.Close()

	var result []SavedDetection
	for rows.Next() {
		var d SavedDetection
		if err := rows.Scan(
			&d.ID, &d.Client, &d.Source, &d.Title, &d.TechniqueID, &d.Tactic,
			&d.LuceneQuery, &d.SigmaRule, &d.Severity, &d.LogSource,
			&d.Falsepositives, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan saved_detection: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("saved_detections rows error: %w", err)
	}
	if result == nil {
		result = []SavedDetection{}
	}
	return result, nil
}

// DeleteDetection removes a saved detection by UUID string.
// Returns nil if the row does not exist (idempotent).
func (s *Store) DeleteDetection(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM saved_detections WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete saved_detection %s: %w", id, err)
	}
	return nil
}

// GetDetection fetches a single detection by UUID string.
// Returns nil, nil when not found.
func (s *Store) GetDetection(ctx context.Context, id string) (*SavedDetection, error) {
	var d SavedDetection
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, client, source, title, technique_id, tactic,
		       lucene_query, sigma_rule, severity, log_source, falsepositives, created_at
		FROM saved_detections WHERE id = $1::uuid
	`, id).Scan(
		&d.ID, &d.Client, &d.Source, &d.Title, &d.TechniqueID, &d.Tactic,
		&d.LuceneQuery, &d.SigmaRule, &d.Severity, &d.LogSource,
		&d.Falsepositives, &d.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved_detection %s: %w", id, err)
	}
	return &d, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd backend && NEON_DSN="$NEON_DSN" go test ./internal/store/... -v 2>&1 | tail -20
```

Expected: all `TestSave*`, `TestDelete*`, `TestGet*`, `TestList*` PASS (or SKIP if no `NEON_DSN`). Existing tests must still pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/store.go backend/internal/store/library_store_test.go
git commit -m "feat(store): SaveDetection, ListDetections, DeleteDetection, GetDetection"
```

---

## Task 3: Backend models

**Files:**
- Create: `backend/internal/models/library.go`

- [ ] **Step 1: Create the models file**

```go
package models

// SaveDetectionRequest is the body for POST /api/library.
type SaveDetectionRequest struct {
	Client         string   `json:"client"`
	Source         string   `json:"source"` // "builder" | "suggestions"
	Title          string   `json:"title"`
	TechniqueID    string   `json:"technique_id"`
	Tactic         string   `json:"tactic"`
	LuceneQuery    string   `json:"lucene_query"`
	SigmaRule      string   `json:"sigma_rule"`
	Severity       string   `json:"severity"`
	LogSource      string   `json:"log_source"`
	Falsepositives []string `json:"falsepositives"`
}

// LibraryResponse is the body for GET /api/library.
type LibraryResponse struct {
	Detections []LibraryDetection `json:"detections"`
	Total      int                `json:"total"`
}

// LibraryDetection is one row in the GET /api/library response.
// Matches store.SavedDetection but adds json tags expected by the frontend.
type LibraryDetection struct {
	ID             string   `json:"id"`
	Client         string   `json:"client"`
	Source         string   `json:"source"`
	Title          string   `json:"title"`
	TechniqueID    string   `json:"technique_id"`
	Tactic         string   `json:"tactic"`
	LuceneQuery    string   `json:"lucene_query"`
	SigmaRule      string   `json:"sigma_rule"`
	Severity       string   `json:"severity"`
	LogSource      string   `json:"log_source"`
	Falsepositives []string `json:"falsepositives"`
	CreatedAt      string   `json:"created_at"` // ISO 8601
}

// PushResponse is returned by POST /api/library/{id}/push.
type PushResponse struct {
	CoralogixAlertID string `json:"coralogix_alert_id"`
	URL              string `json:"url"`
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/library.go
git commit -m "feat(models): SaveDetectionRequest, LibraryResponse, PushResponse"
```

---

## Task 4: Library API handlers (Save, List, Delete)

**Files:**
- Create: `backend/internal/api/library.go`

- [ ] **Step 1: Create `backend/internal/api/library.go`** with the first three handlers:

```go
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/store"
)

// HandleLibrarySave handles POST /api/library.
func (h *Handler) HandleLibrarySave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.alertStore == nil {
		http.Error(w, `{"error":"library unavailable: no database configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req models.SaveDetectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Client == "" || req.Title == "" || req.TechniqueID == "" || req.LuceneQuery == "" || req.SigmaRule == "" {
		http.Error(w, `{"error":"missing required fields: client, title, technique_id, lucene_query, sigma_rule"}`, http.StatusBadRequest)
		return
	}
	if req.Source != "builder" && req.Source != "suggestions" {
		http.Error(w, `{"error":"source must be builder or suggestions"}`, http.StatusBadRequest)
		return
	}
	validSeverities := map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
	if !validSeverities[req.Severity] {
		http.Error(w, `{"error":"severity must be critical, high, medium, or low"}`, http.StatusBadRequest)
		return
	}
	if req.Falsepositives == nil {
		req.Falsepositives = []string{}
	}

	d := store.SavedDetection{
		Client:         req.Client,
		Source:         req.Source,
		Title:          req.Title,
		TechniqueID:    req.TechniqueID,
		Tactic:         req.Tactic,
		LuceneQuery:    req.LuceneQuery,
		SigmaRule:      req.SigmaRule,
		Severity:       req.Severity,
		LogSource:      req.LogSource,
		Falsepositives: req.Falsepositives,
	}

	id, err := h.alertStore.SaveDetection(r.Context(), d)
	if err != nil {
		log.Printf("ERROR HandleLibrarySave: %v", err)
		http.Error(w, `{"error":"failed to save detection"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// HandleLibraryList handles GET /api/library.
// Query params: client, technique, severity.
func (h *Handler) HandleLibraryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.alertStore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.LibraryResponse{Detections: []models.LibraryDetection{}, Total: 0})
		return
	}

	f := store.DetectionFilter{
		Client:      r.URL.Query().Get("client"),
		TechniqueID: r.URL.Query().Get("technique"),
		Severity:    r.URL.Query().Get("severity"),
	}

	rows, err := h.alertStore.ListDetections(r.Context(), f)
	if err != nil {
		log.Printf("ERROR HandleLibraryList: %v", err)
		http.Error(w, `{"error":"failed to list detections"}`, http.StatusInternalServerError)
		return
	}

	detections := make([]models.LibraryDetection, 0, len(rows))
	for _, row := range rows {
		fps := row.Falsepositives
		if fps == nil {
			fps = []string{}
		}
		detections = append(detections, models.LibraryDetection{
			ID:             row.ID,
			Client:         row.Client,
			Source:         row.Source,
			Title:          row.Title,
			TechniqueID:    row.TechniqueID,
			Tactic:         row.Tactic,
			LuceneQuery:    row.LuceneQuery,
			SigmaRule:      row.SigmaRule,
			Severity:       row.Severity,
			LogSource:      row.LogSource,
			Falsepositives: fps,
			CreatedAt:      row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.LibraryResponse{
		Detections: detections,
		Total:      len(detections),
	})
}

// HandleLibraryDelete handles DELETE /api/library/{id}.
func (h *Handler) HandleLibraryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.alertStore == nil {
		http.Error(w, `{"error":"library unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Extract id from path: /api/library/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/library/")
	id = strings.TrimSuffix(id, "/push") // guard: delete vs push on same prefix
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, `{"error":"missing or invalid detection id"}`, http.StatusBadRequest)
		return
	}

	if err := h.alertStore.DeleteDetection(r.Context(), id); err != nil {
		log.Printf("ERROR HandleLibraryDelete id=%s: %v", id, err)
		http.Error(w, `{"error":"failed to delete detection"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// regionToRESTBase maps a Coralogix region ID to its REST API base URL.
func regionToRESTBase(region string) string {
	bases := map[string]string{
		"eu1": "https://api.coralogix.com",
		"eu2": "https://api.eu2.coralogix.com",
		"us1": "https://api.coralogix.us",
		"us2": "https://api.cx498.coralogix.com",
		"ap1": "https://api.app.coralogix.in",
		"ap2": "https://api.coralogixsg.com",
		"ap3": "https://api.ap3.coralogix.com",
	}
	if b, ok := bases[strings.ToLower(region)]; ok {
		return b
	}
	return fmt.Sprintf("https://api.coralogix.com") // safe default
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/library.go
git commit -m "feat(api): HandleLibrarySave, HandleLibraryList, HandleLibraryDelete"
```

---

## Task 5: Export handler (Sigma zip)

**Files:**
- Modify: `backend/internal/api/library.go`

- [ ] **Step 1: Add `HandleLibraryExport` to `library.go`**. Add this import to the import block: `"archive/zip"`, `"regexp"`, `"time"`. Then add the function after `HandleLibraryDelete`:

```go
// HandleLibraryExport handles GET /api/library/export.
// Streams a zip of Sigma .yml files filtered by ?client=.
func (h *Handler) HandleLibraryExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.alertStore == nil {
		http.Error(w, `{"error":"library unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	client := r.URL.Query().Get("client")
	rows, err := h.alertStore.ListDetections(r.Context(), store.DetectionFilter{Client: client, Limit: 500})
	if err != nil {
		log.Printf("ERROR HandleLibraryExport: %v", err)
		http.Error(w, `{"error":"failed to load detections"}`, http.StatusInternalServerError)
		return
	}

	date := time.Now().UTC().Format("2006-01-02")
	clientSlug := "all"
	if client != "" {
		clientSlug = slugify(client)
	}
	filename := fmt.Sprintf("detections-%s-%s.zip", clientSlug, date)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	zw := zip.NewWriter(w)
	defer zw.Close()

	for i, row := range rows {
		name := fmt.Sprintf("%s-%s-%03d.yml", row.TechniqueID, slugify(row.Title), i+1)
		f, err := zw.Create(name)
		if err != nil {
			log.Printf("WARN HandleLibraryExport zip entry %s: %v", name, err)
			continue
		}
		fmt.Fprint(f, row.SigmaRule)
	}
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/library.go
git commit -m "feat(api): HandleLibraryExport — Sigma zip download"
```

---

## Task 6: Push handler (Coralogix REST)

**Files:**
- Modify: `backend/internal/api/library.go`

- [ ] **Step 1: Add `HandleLibraryPush` to `library.go`**. Add `"io"` to imports. Add the function after `slugify`:

```go
// HandleLibraryPush handles POST /api/library/{id}/push.
// Pushes the detection to Coralogix as a live alert via the REST API.
func (h *Handler) HandleLibraryPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.alertStore == nil {
		http.Error(w, `{"error":"library unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Path: /api/library/{id}/push
	path := strings.TrimPrefix(r.URL.Path, "/api/library/")
	path = strings.TrimSuffix(path, "/push")
	id := strings.TrimSpace(path)
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, `{"error":"missing detection id"}`, http.StatusBadRequest)
		return
	}

	det, err := h.alertStore.GetDetection(r.Context(), id)
	if err != nil {
		log.Printf("ERROR HandleLibraryPush GetDetection id=%s: %v", id, err)
		http.Error(w, `{"error":"failed to load detection"}`, http.StatusInternalServerError)
		return
	}
	if det == nil {
		http.Error(w, `{"error":"detection not found"}`, http.StatusNotFound)
		return
	}

	// Look up the client config for API key + region.
	cc, ok := h.cfg.Clients[det.Client]
	if !ok || cc.APIKey == "" {
		msg := fmt.Sprintf(`{"error":"no API key configured for client %q"}`, det.Client)
		http.Error(w, msg, http.StatusUnprocessableEntity)
		return
	}

	// Map severity to Coralogix enum.
	sevMap := map[string]string{
		"critical": "ALERT_SEVERITY_CRITICAL",
		"high":     "ALERT_SEVERITY_HIGH",
		"medium":   "ALERT_SEVERITY_MEDIUM",
		"low":      "ALERT_SEVERITY_LOW",
	}
	cxSeverity := sevMap[det.Severity]
	if cxSeverity == "" {
		cxSeverity = "ALERT_SEVERITY_MEDIUM"
	}

	// Build Coralogix alert creation payload.
	payload := map[string]any{
		"name":        det.Title,
		"description": fmt.Sprintf("Auto-generated from CXAlert Detection Library — technique %s", det.TechniqueID),
		"is_active":   true,
		"severity":    cxSeverity,
		"type": map[string]any{
			"logs_immediate": map[string]any{
				"lucene_query": det.LuceneQuery,
			},
		},
	}

	body, _ := json.Marshal(payload)
	baseURL := regionToRESTBase(cc.Region)
	endpoint := baseURL + "/api/v1/external/alerts"

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		log.Printf("ERROR HandleLibraryPush build request: %v", err)
		http.Error(w, `{"error":"failed to build push request"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cc.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("ERROR HandleLibraryPush coralogix request: %v", err)
		http.Error(w, `{"error":"failed to reach Coralogix API"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("ERROR HandleLibraryPush coralogix status=%d body=%s", resp.StatusCode, respBody)
		http.Error(w, fmt.Sprintf(`{"error":"Coralogix API error: %s"}`, string(respBody)), http.StatusBadGateway)
		return
	}

	// Parse alert ID from Coralogix response.
	var cxResp struct {
		Alert struct {
			ID string `json:"id"`
		} `json:"alert"`
		ID string `json:"id"` // some API versions return top-level id
	}
	json.Unmarshal(respBody, &cxResp)

	alertID := cxResp.Alert.ID
	if alertID == "" {
		alertID = cxResp.ID
	}

	alertURL := fmt.Sprintf("%s/ui/#/alerts/query?alertId=%s", baseURL, alertID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.PushResponse{
		CoralogixAlertID: alertID,
		URL:              alertURL,
	})
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/library.go
git commit -m "feat(api): HandleLibraryPush — create Coralogix alert via REST"
```

---

## Task 7: Register routes in main.go

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add `HandleBuildDetection` line reference** — find the existing route registration block (around line 115) and add these 5 lines directly after `mux.HandleFunc("/api/build-detection", handler.HandleBuildDetection)`:

```go
mux.HandleFunc("/api/library", func(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        handler.HandleLibraryList(w, r)
    case http.MethodPost:
        handler.HandleLibrarySave(w, r)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
})
mux.HandleFunc("/api/library/export", handler.HandleLibraryExport)
mux.HandleFunc("/api/library/", func(w http.ResponseWriter, r *http.Request) {
    if strings.HasSuffix(r.URL.Path, "/push") {
        handler.HandleLibraryPush(w, r)
    } else if r.Method == http.MethodDelete {
        handler.HandleLibraryDelete(w, r)
    } else {
        http.NotFound(w, r)
    }
})
```

Note: `strings` is already imported in `main.go`.

- [ ] **Step 2: Build and start the server to verify routes load**

```bash
cd backend && go build ./... && echo "build OK"
```

Expected: `build OK`.

- [ ] **Step 3: Smoke-test the list endpoint**

```bash
curl -s http://localhost:8080/api/library | python3 -m json.tool
```

Expected: `{"detections": [], "total": 0}` (or library unavailable if no NeonDB).

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): register /api/library routes"
```

---

## Task 8: Frontend types + API methods

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: Add library types** to the end of `frontend/src/types/index.ts`:

```ts
export interface SavedDetection {
  id: string;
  client: string;
  source: 'builder' | 'suggestions';
  title: string;
  technique_id: string;
  tactic: string;
  lucene_query: string;
  sigma_rule: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  log_source: string;
  falsepositives: string[];
  created_at: string; // ISO 8601
}

export interface SaveDetectionRequest {
  client: string;
  source: 'builder' | 'suggestions';
  title: string;
  technique_id: string;
  tactic: string;
  lucene_query: string;
  sigma_rule: string;
  severity: string;
  log_source: string;
  falsepositives: string[];
}

export interface LibraryResponse {
  detections: SavedDetection[];
  total: number;
}

export interface PushResponse {
  coralogix_alert_id: string;
  url: string;
}
```

- [ ] **Step 2: Add library API functions** to the end of `frontend/src/services/api.ts`:

```ts
export async function saveDetection(payload: SaveDetectionRequest): Promise<{ id: string }> {
  const res = await fetch(`${API_BASE}/api/library`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Save failed' }));
    throw new Error(err.error || 'Failed to save detection');
  }
  return res.json();
}

export async function listDetections(filter?: { client?: string; severity?: string }): Promise<LibraryResponse> {
  const params = new URLSearchParams();
  if (filter?.client) params.set('client', filter.client);
  if (filter?.severity) params.set('severity', filter.severity);
  const res = await fetch(`${API_BASE}/api/library?${params}`);
  if (!res.ok) throw new Error('Failed to fetch library');
  return res.json();
}

export async function deleteDetection(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/library/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete detection');
}

export async function pushDetection(id: string): Promise<PushResponse> {
  const res = await fetch(`${API_BASE}/api/library/${id}/push`, { method: 'POST' });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Push failed' }));
    throw new Error(err.error || 'Failed to push detection');
  }
  return res.json();
}

export async function exportDetections(client?: string): Promise<void> {
  const params = new URLSearchParams();
  if (client) params.set('client', client);
  const res = await fetch(`${API_BASE}/api/library/export?${params}`);
  if (!res.ok) throw new Error('Export failed');
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  const cd = res.headers.get('Content-Disposition') ?? '';
  const match = cd.match(/filename="([^"]+)"/);
  a.download = match ? match[1] : 'detections.zip';
  a.click();
  URL.revokeObjectURL(url);
}
```

Also add the new types to the import in `api.ts` — the first line should become:

```ts
import type { AnalyzeResponse, ClientInfo, CorrelationsResponse, ExportNarrativeReport, GenerationResult, HuntPayload, InsightsReport, LibraryResponse, MapTacticsResponse, MitreCatalog, NoiseAlert, NoiseResponse, PushResponse, SaveDetectionRequest, SuggestionsResponse } from '../types';
```

- [ ] **Step 3: Verify TypeScript compilation**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: build succeeds with no type errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/services/api.ts
git commit -m "feat(frontend): library types and API functions"
```

---

## Task 9: DetectionCard component

**Files:**
- Create: `frontend/src/components/DetectionCard.tsx`

- [ ] **Step 1: Create `frontend/src/components/DetectionCard.tsx`**:

```tsx
import { useState } from 'react';
import type { SavedDetection, PushResponse } from '../types';

export type PushState = 'idle' | 'pushing' | 'pushed' | 'error';

interface DetectionCardProps {
  detection: SavedDetection;
  onDelete?: (id: string) => void;
  onPush?: (id: string) => Promise<PushResponse>;
}

const SEV_COLOR: Record<string, string> = {
  critical: '#ef4444',
  high:     '#f97316',
  medium:   '#eab308',
  low:      '#22c55e',
};

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export default function DetectionCard({ detection: d, onDelete, onPush }: DetectionCardProps) {
  const [expanded, setExpanded] = useState(false);
  const [pushState, setPushState] = useState<PushState>('idle');
  const [pushUrl, setPushUrl] = useState('');

  const handlePush = async () => {
    if (!onPush || pushState === 'pushing') return;
    setPushState('pushing');
    try {
      const res = await onPush(d.id);
      setPushUrl(res.url);
      setPushState('pushed');
    } catch {
      setPushState('error');
      setTimeout(() => setPushState('idle'), 3000);
    }
  };

  const sevColor = SEV_COLOR[d.severity] ?? '#6366f1';

  return (
    <div className="det-card">
      <div className="det-card-header">
        <span className="det-sev-chip" style={{ background: `${sevColor}22`, color: sevColor }}>
          {d.severity.toUpperCase()}
        </span>
        <span className="det-source-badge">{d.source}</span>
      </div>

      <div className="det-card-title">{d.title}</div>
      <div className="det-card-meta">
        {d.technique_id} · {d.log_source} · {d.client} · {relativeTime(d.created_at)}
      </div>

      <div className="det-card-actions">
        <button className="btn-small" onClick={() => setExpanded(e => !e)}>
          {expanded ? 'Hide' : 'View'}
        </button>

        {onPush && pushState === 'idle' && (
          <button className="btn-small btn-push" onClick={handlePush}>Push →CX</button>
        )}
        {pushState === 'pushing' && <span className="det-push-status">Pushing…</span>}
        {pushState === 'pushed' && (
          <a className="det-push-status det-push-ok" href={pushUrl} target="_blank" rel="noreferrer">
            ✓ Pushed ↗
          </a>
        )}
        {pushState === 'error' && <span className="det-push-status det-push-err">✗ Error</span>}

        {onDelete && (
          <button className="btn-small btn-danger" onClick={() => onDelete(d.id)}>✕</button>
        )}
      </div>

      {expanded && (
        <pre className="det-card-sigma">{d.sigma_rule || '# No Sigma rule'}</pre>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd frontend && npm run build 2>&1 | grep -E "error|Error|DetectionCard" | head -10
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/DetectionCard.tsx
git commit -m "feat(frontend): DetectionCard component"
```

---

## Task 10: LibraryView component

**Files:**
- Create: `frontend/src/components/LibraryView.tsx`

- [ ] **Step 1: Create `frontend/src/components/LibraryView.tsx`**:

```tsx
import { useState, useEffect, useMemo } from 'react';
import type { SavedDetection, PushResponse } from '../types';
import { listDetections, deleteDetection, pushDetection, exportDetections } from '../services/api';
import DetectionCard from './DetectionCard';

interface LibraryViewProps {
  clientName: string; // pre-fill client filter with current client
}

export default function LibraryView({ clientName }: LibraryViewProps) {
  const [detections, setDetections] = useState<SavedDetection[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [clientFilter, setClientFilter] = useState(clientName);
  const [severityFilter, setSeverityFilter] = useState('');
  const [exporting, setExporting] = useState(false);

  useEffect(() => {
    setLoading(true);
    setError('');
    listDetections()
      .then(r => setDetections(r.detections))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  const clients = useMemo(() => {
    const set = new Set(detections.map(d => d.client));
    return Array.from(set).sort();
  }, [detections]);

  const filtered = useMemo(() => {
    return detections.filter(d => {
      if (clientFilter && d.client !== clientFilter) return false;
      if (severityFilter && d.severity !== severityFilter) return false;
      if (search) {
        const q = search.toLowerCase();
        return d.title.toLowerCase().includes(q)
          || d.technique_id.toLowerCase().includes(q)
          || d.tactic.toLowerCase().includes(q);
      }
      return true;
    });
  }, [detections, clientFilter, severityFilter, search]);

  const handleDelete = async (id: string) => {
    try {
      await deleteDetection(id);
      setDetections(prev => prev.filter(d => d.id !== id));
    } catch (e) {
      alert('Failed to delete: ' + (e instanceof Error ? e.message : 'unknown error'));
    }
  };

  const handlePush = (id: string): Promise<PushResponse> => pushDetection(id);

  const handleExport = async () => {
    setExporting(true);
    try {
      await exportDetections(clientFilter || undefined);
    } catch (e) {
      alert('Export failed: ' + (e instanceof Error ? e.message : 'unknown error'));
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="library-view">
      <div className="library-header">
        <div className="library-title-row">
          <div>
            <div className="section-label">DETECTION LIBRARY</div>
            <div className="library-count">{filtered.length} detection{filtered.length !== 1 ? 's' : ''}</div>
          </div>
          <button className="btn-export" onClick={handleExport} disabled={exporting || filtered.length === 0}>
            {exporting ? 'Exporting…' : '↓ Export Sigma (.zip)'}
          </button>
        </div>

        <div className="library-filters">
          <input
            className="library-search"
            placeholder="Search by title, technique, tactic…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          <select className="library-select" value={clientFilter} onChange={e => setClientFilter(e.target.value)}>
            <option value="">All clients</option>
            {clients.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
          <select className="library-select" value={severityFilter} onChange={e => setSeverityFilter(e.target.value)}>
            <option value="">All severities</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>
      </div>

      {loading && <div className="library-empty">Loading…</div>}
      {error && <div className="library-empty library-error">{error}</div>}

      {!loading && !error && filtered.length === 0 && (
        <div className="library-empty">
          No detections saved yet. Use the <strong>Save</strong> button on any detection in the Builder or MITRE panel.
        </div>
      )}

      {!loading && !error && filtered.length > 0 && (
        <div className="library-grid">
          {filtered.map(d => (
            <DetectionCard
              key={d.id}
              detection={d}
              onDelete={handleDelete}
              onPush={handlePush}
            />
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd frontend && npm run build 2>&1 | grep -E "error|Error|LibraryView" | head -10
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/LibraryView.tsx
git commit -m "feat(frontend): LibraryView component with filter bar and card grid"
```

---

## Task 11: Save buttons in Builder and MITREHeatmap

**Files:**
- Modify: `frontend/src/components/DetectionBuilder.tsx`
- Modify: `frontend/src/components/MITREHeatmap.tsx`

- [ ] **Step 1: Add Save button to DetectionBuilder** — open `DetectionBuilder.tsx`. Find the `GeneratedPanel` component (around line 590). At the top of the function, add the save state:

```tsx
// inside GeneratedPanel function, after const [showSigma, setShowSigma] = useState(false);
const [savedIds, setSavedIds] = useState<Set<string>>(new Set());
const [savingIds, setSavingIds] = useState<Set<string>>(new Set());
```

Add the save handler inside `GeneratedPanel` (after the state declarations):

```tsx
const handleSave = async (a: FlowAlert) => {
  const key = a.techniqueId;
  if (savedIds.has(key) || savingIds.has(key)) return;
  setSavingIds(prev => new Set(prev).add(key));
  try {
    await saveDetection({
      client: clientName,
      source: 'builder',
      title: a.title,
      technique_id: a.techniqueId,
      tactic: a.tactic ?? '',
      lucene_query: a.logic,
      sigma_rule: a.sigma_rule ?? '',
      severity: a.severity ?? 'medium',
      log_source: a.source ?? '',
      falsepositives: a.falsepositives ?? [],
    });
    setSavedIds(prev => new Set(prev).add(key));
    setTimeout(() => setSavedIds(prev => { const n = new Set(prev); n.delete(key); return n; }), 2000);
  } catch {
    // silent fail — save is best-effort
  } finally {
    setSavingIds(prev => { const n = new Set(prev); n.delete(key); return n; });
  }
};
```

Find the import of `saveDetection` — add it to the existing import from `../services/api`. The import line currently imports from `../services/api`; extend it:

```ts
import { buildDetection, saveDetection } from '../services/api';
```

Find the location in the JSX where the `[View][Push →CX]` action buttons are rendered for each alert card (inside the `.ac-body` loop). Add the Save button **after** the existing hunt button and before the closing of the action row:

```tsx
<button
  className={`btn-small${savedIds.has(a.techniqueId) ? ' btn-saved' : ''}`}
  disabled={savedIds.has(a.techniqueId) || savingIds.has(a.techniqueId)}
  onClick={() => handleSave(a)}
>
  {savingIds.has(a.techniqueId) ? 'Saving…' : savedIds.has(a.techniqueId) ? '✓ Saved' : 'Save'}
</button>
```

Also pass `clientName` as a prop to `GeneratedPanel`. Find where `GeneratedPanel` is rendered inside the parent `DetectionBuilder` component and add `clientName={clientName}` to its props. Update `GeneratedPanelProps` interface to include `clientName: string`.

- [ ] **Step 2: Add Save button to MITREHeatmap** — open `MITREHeatmap.tsx`. Find the `SuggestionsPanel` component. Add save state similarly:

```tsx
// inside SuggestionsPanel, after existing state
const [savedIds, setSavedIds] = useState<Set<string>>(new Set());
const [savingIds, setSavingIds] = useState<Set<string>>(new Set());

const handleSave = async (s: AlertSuggestion) => {
  const key = s.title;
  if (savedIds.has(key) || savingIds.has(key)) return;
  setSavingIds(prev => new Set(prev).add(key));
  try {
    await saveDetection({
      client: clientName,
      source: 'suggestions',
      title: s.title,
      technique_id: techniqueId,
      tactic: tactic,
      lucene_query: s.lucene_query,
      sigma_rule: s.sigma_rule ?? '',
      severity: s.severity,
      log_source: s.log_source,
      falsepositives: s.falsepositives ?? [],
    });
    setSavedIds(prev => new Set(prev).add(key));
    setTimeout(() => setSavedIds(prev => { const n = new Set(prev); n.delete(key); return n; }), 2000);
  } catch { /* silent */ } finally {
    setSavingIds(prev => { const n = new Set(prev); n.delete(key); return n; });
  }
};
```

Find where `saveDetection` needs to be imported in MITREHeatmap.tsx and add it to the `../services/api` import.

In `SuggestionsPanel`, find the JSX for each suggestion card where the HUNT button is rendered. Add a Save button immediately after the HUNT button:

```tsx
<button
  className={`btn-small${savedIds.has(s.title) ? ' btn-saved' : ''}`}
  disabled={savedIds.has(s.title) || savingIds.has(s.title)}
  onClick={() => handleSave(s)}
>
  {savingIds.has(s.title) ? 'Saving…' : savedIds.has(s.title) ? '✓ Saved' : 'Save'}
</button>
```

Check what props `SuggestionsPanel` receives — it needs `clientName`, `techniqueId`, and `tactic`. If these are not already props, add them. Check the existing component signature and the call site inside `MITREHeatmap`.

- [ ] **Step 3: Verify TypeScript compilation**

```bash
cd frontend && npm run build 2>&1 | grep -E "error TS|Error" | head -20
```

Expected: no type errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/DetectionBuilder.tsx frontend/src/components/MITREHeatmap.tsx
git commit -m "feat(frontend): Save button on Builder and Suggestions cards"
```

---

## Task 12: App.tsx — Library view + nav button + CSS

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.css`

- [ ] **Step 1: Add `library` to App.tsx**

Find `type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder' | 'hunt';` (line 18) and replace with:

```ts
type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder' | 'hunt' | 'library';
```

Add `LibraryView` import at the top of the file with the other component imports:

```ts
import LibraryView from './components/LibraryView';
```

Find the `breadcrumb` label list (around line 152) and add the library label:

```ts
label: view === 'mitre' ? 'MITRE Coverage'
     : view === 'insights' ? 'Alert Insights'
     : view === 'graph' ? 'Threat Graph'
     : view === 'hunt' ? 'Hunt'
     : view === 'library' ? 'Detection Library'
     : 'Build detections'
```

Add the Library nav button in `header-right`, immediately before the Back button (around line 183):

```tsx
{view !== 'form' && (
  <button className="btn-small btn-library" onClick={() => navigate('library')}>
    Library
  </button>
)}
```

Add the Library view render in the `AnimatePresence` block, after the `hunt` view block:

```tsx
{view === 'library' && (
  <motion.div key="library" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
    <LibraryView clientName={clientName} />
  </motion.div>
)}
```

- [ ] **Step 2: Add CSS** to `frontend/src/App.css` — append at the end of the file:

```css
/* ── Detection Library ───────────────────────────────────── */
.library-view { padding: 24px 32px; max-width: 1200px; margin: 0 auto; }
.library-header { margin-bottom: 20px; }
.library-title-row { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 14px; }
.library-count { font-size: 22px; font-weight: 700; color: var(--text); margin-top: 2px; }
.library-filters { display: flex; gap: 10px; flex-wrap: wrap; }
.library-search { flex: 1; min-width: 220px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 7px 12px; color: var(--text); font-size: 13px; }
.library-search:focus { outline: none; border-color: var(--accent); }
.library-select { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 7px 10px; color: var(--text); font-size: 13px; cursor: pointer; }
.library-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; }
.library-empty { padding: 48px 0; text-align: center; color: var(--text-sec); font-size: 14px; }
.library-error { color: #ef4444; }
.btn-export { background: rgba(74,222,128,0.12); color: #4ade80; border: 1px solid #4ade80; border-radius: var(--radius-sm); padding: 7px 14px; font-size: 12px; cursor: pointer; white-space: nowrap; }
.btn-export:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-library { background: rgba(99,102,241,0.15); color: #818cf8; border: 1px solid rgba(99,102,241,0.4); }

/* ── DetectionCard ───────────────────────────────────────── */
.det-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 14px; display: flex; flex-direction: column; gap: 6px; }
.det-card-header { display: flex; align-items: center; gap: 8px; }
.det-sev-chip { font-size: 10px; font-weight: 700; padding: 2px 7px; border-radius: 4px; letter-spacing: 0.04em; }
.det-source-badge { font-size: 10px; color: var(--text-sec); margin-left: auto; }
.det-card-title { font-size: 13px; font-weight: 600; color: var(--text); line-height: 1.4; }
.det-card-meta { font-size: 11px; color: var(--text-sec); }
.det-card-actions { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; margin-top: 4px; }
.btn-push { background: rgba(99,102,241,0.15); color: #818cf8; border-color: rgba(99,102,241,0.5); }
.btn-saved { background: rgba(74,222,128,0.12); color: #4ade80; border-color: rgba(74,222,128,0.4); }
.btn-danger { color: var(--text-sec); opacity: 0.6; }
.btn-danger:hover { color: #ef4444; opacity: 1; }
.det-push-status { font-size: 11px; color: var(--text-sec); }
.det-push-ok { color: #4ade80; text-decoration: none; }
.det-push-err { color: #ef4444; }
.det-card-sigma { background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 10px 12px; font-family: var(--font-mono, 'IBM Plex Mono', monospace); font-size: 10px; line-height: 1.5; white-space: pre-wrap; word-break: break-all; color: var(--text-sec); margin-top: 6px; max-height: 200px; overflow-y: auto; }
```

- [ ] **Step 3: Verify full build**

```bash
cd frontend && npm run build 2>&1 | tail -10
```

Expected: build succeeds, no errors.

- [ ] **Step 4: Start dev server and manually verify**

```bash
cd frontend && npm run dev &
```

Then open `http://localhost:5173`, select a client, analyze, and confirm:
1. A "Library" button appears in the top-right header
2. Clicking it navigates to the Library view
3. "No detections saved yet" empty state is shown
4. Go to Builder → generate alerts → "Save" button appears on each card
5. Click Save → button briefly shows "Saving…" then "✓ Saved"
6. Return to Library → the saved detection appears as a card
7. "View" button expands the Sigma rule
8. "Export Sigma (.zip)" downloads a zip file containing `.yml` files

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.tsx frontend/src/App.css
git commit -m "feat(frontend): Library nav button, view routing, and card/library CSS"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Covered by |
|---|---|
| Global library with client filter | Task 2 (`ListDetections` with `DetectionFilter`), Task 10 (`LibraryView` filter bar) |
| NeonDB `saved_detections` table | Task 1 migration DDL |
| `POST /api/library` | Task 4 `HandleLibrarySave` |
| `GET /api/library` | Task 4 `HandleLibraryList` |
| `DELETE /api/library/{id}` | Task 4 `HandleLibraryDelete` |
| `GET /api/library/export` Sigma zip | Task 5 `HandleLibraryExport` |
| `POST /api/library/{id}/push` | Task 6 `HandleLibraryPush` |
| Card grid UI | Task 9 `DetectionCard`, Task 10 `LibraryView` grid |
| Search + client + severity filters | Task 10 filter bar |
| Save button in Builder | Task 11 DetectionBuilder |
| Save button in Suggestions panel | Task 11 MITREHeatmap |
| Library nav button in header | Task 12 App.tsx |
| `Library` breadcrumb label | Task 12 App.tsx breadcrumb |
| Severity chip + source badge on cards | Task 9 DetectionCard |
| Expand Sigma YAML inline | Task 9 DetectionCard `expanded` state |
| Push →CX button per card | Task 9 `handlePush`, Task 6 backend |
| Export Sigma zip with client filter | Task 5, Task 10 export button |
| `btn-library`, `det-card` CSS | Task 12 App.css |

**Placeholder scan:** None found.

**Type consistency:** `SavedDetection.id` is `string` throughout (store returns `id::text`, frontend expects `string`). `DetectionFilter` fields match query param names. `SaveDetectionRequest` fields match `store.SavedDetection` fields. `GeneratedPanelProps.clientName` added in Task 11 and used in `handleSave`.
