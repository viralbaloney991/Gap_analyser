package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestValidateLookbackDays_validValues(t *testing.T) {
	for _, days := range []int{7, 14, 30, 90} {
		got := validateLookbackDays(days)
		if got != days {
			t.Errorf("validateLookbackDays(%d) = %d, want %d", days, got, days)
		}
	}
}

func TestValidateLookbackDays_invalidDefaultsTo30(t *testing.T) {
	for _, days := range []int{0, 1, 15, 45, 100, -1, 365} {
		got := validateLookbackDays(days)
		if got != 30 {
			t.Errorf("validateLookbackDays(%d) = %d, want 30", days, got)
		}
	}
}

func TestHandleAnalyze_rejectsGet(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/analyze", nil)
	w := httptest.NewRecorder()
	h.HandleAnalyze(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleAnalyze_unknownClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"ghost","lookback_days":30}`)
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleAnalyze(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAnalyze_missingClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"lookback_days":30}`)
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleAnalyze(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAnalyze_invalidJSON(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleAnalyze(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
