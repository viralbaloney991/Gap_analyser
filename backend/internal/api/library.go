package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/store"
)

// HandleLibrarySave handles POST /api/library.
func (h *Handler) HandleLibrarySave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.alertStore == nil {
		writeError(w, http.StatusServiceUnavailable, "library unavailable: no database configured")
		return
	}

	var req models.SaveDetectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Client == "" || req.Title == "" || req.TechniqueID == "" || req.LuceneQuery == "" || req.SigmaRule == "" {
		writeError(w, http.StatusBadRequest, "missing required fields: client, title, technique_id, lucene_query, sigma_rule")
		return
	}
	if req.Source != "builder" && req.Source != "suggestions" {
		writeError(w, http.StatusBadRequest, "source must be builder or suggestions")
		return
	}
	validSeverities := map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
	if !validSeverities[req.Severity] {
		writeError(w, http.StatusBadRequest, "severity must be critical, high, medium, or low")
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
		writeError(w, http.StatusInternalServerError, "failed to save detection")
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
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
		writeError(w, http.StatusInternalServerError, "failed to list detections")
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
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.alertStore == nil {
		writeError(w, http.StatusServiceUnavailable, "library unavailable")
		return
	}

	// Extract id from path: /api/library/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/library/")
	id = strings.TrimSuffix(id, "/push") // guard: delete vs push on same prefix
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "missing or invalid detection id")
		return
	}

	if err := h.alertStore.DeleteDetection(r.Context(), id); err != nil {
		log.Printf("ERROR HandleLibraryDelete id=%s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to delete detection")
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
	return "https://api.coralogix.com"
}
