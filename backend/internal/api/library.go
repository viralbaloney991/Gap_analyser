package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

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

// HandleLibraryExport handles GET /api/library/export.
// Streams a zip of Sigma .yml files filtered by ?client=.
func (h *Handler) HandleLibraryExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.alertStore == nil {
		writeError(w, http.StatusServiceUnavailable, "library unavailable")
		return
	}

	client := r.URL.Query().Get("client")
	rows, err := h.alertStore.ListDetections(r.Context(), store.DetectionFilter{Client: client, Limit: 500})
	if err != nil {
		log.Printf("ERROR HandleLibraryExport: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load detections")
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

// HandleLibraryPush handles POST /api/library/{id}/push.
// Pushes the detection to Coralogix as a live alert via the REST API.
func (h *Handler) HandleLibraryPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.alertStore == nil {
		writeError(w, http.StatusServiceUnavailable, "library unavailable")
		return
	}

	// Path: /api/library/{id}/push
	path := strings.TrimPrefix(r.URL.Path, "/api/library/")
	path = strings.TrimSuffix(path, "/push")
	id := strings.TrimSpace(path)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "missing detection id")
		return
	}

	det, err := h.alertStore.GetDetection(r.Context(), id)
	if err != nil {
		log.Printf("ERROR HandleLibraryPush GetDetection id=%s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to load detection")
		return
	}
	if det == nil {
		writeError(w, http.StatusNotFound, "detection not found")
		return
	}

	// Look up the client config for API key + region.
	cc, ok := h.config.Clients[det.Client]
	if !ok || cc.APIKey == "" {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("no API key configured for client %q", det.Client))
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

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		log.Printf("ERROR HandleLibraryPush build request: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to build push request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+cc.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("ERROR HandleLibraryPush coralogix request: %v", err)
		writeError(w, http.StatusBadGateway, "failed to reach Coralogix API")
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR HandleLibraryPush read response: %v", err)
		writeError(w, http.StatusBadGateway, "failed to read Coralogix response")
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("ERROR HandleLibraryPush coralogix status=%d body=%s", resp.StatusCode, respBody)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Coralogix API error: %s", string(respBody)))
		return
	}

	// Parse alert ID from Coralogix response.
	var cxResp struct {
		Alert struct {
			ID string `json:"id"`
		} `json:"alert"`
		ID string `json:"id"` // some API versions return top-level id
	}
	if err := json.Unmarshal(respBody, &cxResp); err != nil {
		log.Printf("ERROR HandleLibraryPush parse response: %v body=%s", err, respBody)
		writeError(w, http.StatusBadGateway, "failed to parse Coralogix response")
		return
	}

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
