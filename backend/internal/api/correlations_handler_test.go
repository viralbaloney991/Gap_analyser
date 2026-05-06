package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/store"
)

func TestHandleCorrelations_rejectsGet(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/correlations", nil)
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleCorrelations_missingClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"gap_prose":"some gap"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/correlations", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCorrelations_missingGapProse(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"acme"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/correlations", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCorrelations_unknownClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"ghost","gap_prose":"T1059 has no anomaly layer"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/correlations", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCorrelations(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestMergeCorrelations_skipsCorruptRow(t *testing.T) {
	rows := []store.CorrelationRow{
		{
			Provider:    "anthropic",
			Suggestions: json.RawMessage(`[{"type":"correlation","title":"Brute Force","description":"d","involved_techniques":["T1110"],"query_skeleton":"event.action:auth*","priority":"high"}]`),
			GeneratedAt: time.Now().Add(-2 * time.Minute),
		},
		{
			Provider:    "nvidia",
			Suggestions: json.RawMessage(`NOT VALID JSON`),
			GeneratedAt: time.Now().Add(-1 * time.Minute),
		},
	}
	merged, provider := mergeCorrelations(rows)
	if provider != "anthropic" {
		t.Errorf("expected provider=anthropic (last good row), got %q", provider)
	}
	if len(merged) != 1 {
		t.Errorf("expected 1 merged suggestion, got %d", len(merged))
	}
}

func TestBuildCorrelationCacheKey_stable(t *testing.T) {
	key1 := buildCorrelationCacheKey("acme", "T1059 threshold alert, no anomaly layer")
	key2 := buildCorrelationCacheKey("acme", "T1059 threshold alert, no anomaly layer")
	if key1 != key2 {
		t.Error("buildCorrelationCacheKey should be deterministic")
	}
	key3 := buildCorrelationCacheKey("acme", "  T1059 threshold alert, no anomaly layer  ")
	if key1 != key3 {
		t.Error("buildCorrelationCacheKey should trim whitespace before hashing")
	}
	keyCased := buildCorrelationCacheKey("ACME", "T1059 threshold alert, no anomaly layer")
	if key1 != keyCased {
		t.Error("buildCorrelationCacheKey should be case-insensitive for client")
	}
	keyOther := buildCorrelationCacheKey("other-client", "T1059 threshold alert, no anomaly layer")
	if key1 == keyOther {
		t.Error("buildCorrelationCacheKey should differ for different clients")
	}
}
