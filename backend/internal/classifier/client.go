package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Candidate is a MITRE technique candidate returned by the classifier sidecar.
type Candidate struct {
	TechniqueID string  `json:"technique_id"`
	Name        string  `json:"name"`
	Score       float64 `json:"score"`
}

// Client calls the Python classifier sidecar over HTTP.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// NewClient creates a classifier Client for the given sidecar endpoint (e.g. "http://localhost:8001").
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type classifyRequest struct {
	Name      string `json:"name"`
	Query     string `json:"query"`
	App       string `json:"app"`
	Subsystem string `json:"subsystem"`
}

// ClassifyAlert calls POST /classify on the sidecar and returns top-K candidates.
// Returns a non-nil error if the sidecar is unreachable.
func (c *Client) ClassifyAlert(ctx context.Context, name, query, app, subsystem string) ([]Candidate, error) {
	payload, _ := json.Marshal(classifyRequest{
		Name:      name,
		Query:     query,
		App:       app,
		Subsystem: subsystem,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/classify", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("classifier sidecar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("classifier returned %d", resp.StatusCode)
	}

	var candidates []Candidate
	if err := json.NewDecoder(resp.Body).Decode(&candidates); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return candidates, nil
}

// IsHealthy returns true if the sidecar /health endpoint responds OK.
func (c *Client) IsHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	return err == nil && resp.StatusCode == http.StatusOK
}
