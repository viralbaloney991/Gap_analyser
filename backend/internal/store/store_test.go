package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/store"
)

func testDSN(t *testing.T) string {
	t.Helper()
	d := os.Getenv("NEON_DSN")
	if d == "" {
		t.Skip("NEON_DSN not set — skipping integration test")
	}
	return d
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.New(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestLoadAlerts_Empty(t *testing.T) {
	s := newStore(t)
	alerts, err := s.LoadAlerts(context.Background(), "no-such-client")
	if err != nil {
		t.Fatalf("LoadAlerts: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("want 0 alerts, got %d", len(alerts))
	}
}

func TestUpsertAndLoad(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	client := "test-upsert-" + t.Name()

	input := []*models.AlertDef{
		{ID: "a1", Name: "Alert One", Enabled: true, AlertType: "logs_immediate"},
		{ID: "a2", Name: "Alert Two", Enabled: false, AlertType: "logs_threshold"},
	}

	if err := s.UpsertAlerts(ctx, client, input); err != nil {
		t.Fatalf("UpsertAlerts: %v", err)
	}

	got, err := s.LoadAlerts(ctx, client)
	if err != nil {
		t.Fatalf("LoadAlerts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 alerts, got %d", len(got))
	}

	// Upsert again with updated name — should replace, not duplicate
	input[0].Name = "Alert One Updated"
	if err := s.UpsertAlerts(ctx, client, input); err != nil {
		t.Fatalf("UpsertAlerts (second): %v", err)
	}
	got2, _ := s.LoadAlerts(ctx, client)
	if len(got2) != 2 {
		t.Errorf("want 2 after re-upsert, got %d", len(got2))
	}
	foundUpdated := false
	for _, a := range got2 {
		if a.ID == "a1" && a.Name == "Alert One Updated" {
			foundUpdated = true
		}
	}
	if !foundUpdated {
		t.Error("upsert did not update existing row")
	}
}

func TestSyncState_NeverSynced(t *testing.T) {
	s := newStore(t)
	_, ok, err := s.GetLastSynced(context.Background(), "never-synced-client")
	if err != nil {
		t.Fatalf("GetLastSynced: %v", err)
	}
	if ok {
		t.Error("want ok=false for client never synced")
	}
}

func TestSyncState_SetAndGet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	client := "test-syncstate-" + t.Name()

	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SetLastSynced(ctx, client, now); err != nil {
		t.Fatalf("SetLastSynced: %v", err)
	}

	got, ok, err := s.GetLastSynced(ctx, client)
	if err != nil {
		t.Fatalf("GetLastSynced: %v", err)
	}
	if !ok {
		t.Fatal("want ok=true after SetLastSynced")
	}
	if !got.Equal(now) {
		t.Errorf("want %v, got %v", now, got)
	}

	// Idempotent update
	later := now.Add(time.Hour)
	if err := s.SetLastSynced(ctx, client, later); err != nil {
		t.Fatalf("SetLastSynced (update): %v", err)
	}
	got2, _, _ := s.GetLastSynced(ctx, client)
	if !got2.Equal(later) {
		t.Errorf("want updated time %v, got %v", later, got2)
	}
}

func TestGetCachedSuggestions_Empty(t *testing.T) {
	s := newStore(t)
	rows, err := s.GetCachedSuggestions(context.Background(), "nonexistent-cache-key")
	if err != nil {
		t.Fatalf("GetCachedSuggestions: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want 0 rows, got %d", len(rows))
	}
}

func TestAppendAndGetCachedSuggestions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "test-suggest-" + t.Name()

	now := time.Now().UTC().Truncate(time.Millisecond)
	sugsJSON := json.RawMessage(`[{"log_source":"firewall","alert_name":"FW Brute Force","description":"Brute force","query_hint":"event:login_failed","priority":"high"}]`)

	row := store.SuggestionRow{
		CacheKey:    key,
		TechniqueID: "T1021",
		LogSources:  []string{"firewall", "endpoint"},
		Suggestions: sugsJSON,
		Provider:    "nvidia",
		GeneratedAt: now,
	}
	if err := s.AppendCachedSuggestions(ctx, row); err != nil {
		t.Fatalf("AppendCachedSuggestions: %v", err)
	}

	got, err := s.GetCachedSuggestions(ctx, key)
	if err != nil {
		t.Fatalf("GetCachedSuggestions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].TechniqueID != "T1021" {
		t.Errorf("want TechniqueID T1021, got %s", got[0].TechniqueID)
	}
	if got[0].Provider != "nvidia" {
		t.Errorf("want Provider nvidia, got %s", got[0].Provider)
	}
	if !got[0].GeneratedAt.Equal(now) {
		t.Errorf("want GeneratedAt %v, got %v", now, got[0].GeneratedAt)
	}
	if string(got[0].Suggestions) != string(sugsJSON) {
		t.Errorf("suggestions JSON mismatch:\nwant %s\n got %s", sugsJSON, got[0].Suggestions)
	}
}

func TestAppendCachedSuggestions_AccumulatesRows(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "test-accumulate-" + t.Name()

	first := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	second := time.Now().UTC().Truncate(time.Millisecond)

	row1 := store.SuggestionRow{
		CacheKey: key, TechniqueID: "T1021",
		LogSources: []string{"firewall"}, Suggestions: json.RawMessage(`[]`),
		Provider: "nvidia", GeneratedAt: first,
	}
	row2 := store.SuggestionRow{
		CacheKey: key, TechniqueID: "T1021",
		LogSources: []string{"firewall"}, Suggestions: json.RawMessage(`[]`),
		Provider: "claude", GeneratedAt: second,
	}

	if err := s.AppendCachedSuggestions(ctx, row1); err != nil {
		t.Fatalf("AppendCachedSuggestions (1): %v", err)
	}
	if err := s.AppendCachedSuggestions(ctx, row2); err != nil {
		t.Fatalf("AppendCachedSuggestions (2): %v", err)
	}

	got, err := s.GetCachedSuggestions(ctx, key)
	if err != nil {
		t.Fatalf("GetCachedSuggestions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	// Rows ordered ASC by generated_at
	if !got[0].GeneratedAt.Equal(first) {
		t.Errorf("want first row GeneratedAt %v, got %v", first, got[0].GeneratedAt)
	}
	if !got[1].GeneratedAt.Equal(second) {
		t.Errorf("want second row GeneratedAt %v, got %v", second, got[1].GeneratedAt)
	}
	if got[0].Provider != "nvidia" {
		t.Errorf("want first row Provider nvidia, got %s", got[0].Provider)
	}
	if got[1].Provider != "claude" {
		t.Errorf("want second row Provider claude, got %s", got[1].Provider)
	}
}
