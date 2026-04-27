package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestHandleExportNarrative_rejectsGet(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/export/narrative", nil)
	w := httptest.NewRecorder()
	h.HandleExportNarrative(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleExportNarrative_missingClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/export/narrative", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleExportNarrative(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleExportNarrative_unknownClient(t *testing.T) {
	h := &Handler{config: &config.Config{Clients: map[string]config.ClientConfig{}}}
	body := strings.NewReader(`{"client":"ghost"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/export/narrative", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleExportNarrative(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
