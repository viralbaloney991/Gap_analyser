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
