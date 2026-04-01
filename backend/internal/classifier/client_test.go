package classifier_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"coralogix-alert-analyzer/internal/classifier"
)

func TestClassifyAlert_ReturnsCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/classify" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]classifier.Candidate{
			{TechniqueID: "T1110", Name: "Brute Force", Score: 0.91},
			{TechniqueID: "T1078", Name: "Valid Accounts", Score: 0.74},
		})
	}))
	defer srv.Close()

	c := classifier.NewClient(srv.URL)
	candidates, err := c.ClassifyAlert(context.Background(), "Okta Brute Force", "failed_logins:>5", "okta", "okta-audit")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].TechniqueID != "T1110" {
		t.Errorf("expected T1110 first, got %s", candidates[0].TechniqueID)
	}
}

func TestClassifyAlert_SidecarDown_ReturnsError(t *testing.T) {
	c := classifier.NewClient("http://localhost:19999") // nothing listening here
	candidates, err := c.ClassifyAlert(context.Background(), "Alert", "query", "", "")

	if err == nil {
		t.Fatal("expected error when sidecar is down")
	}
	if candidates != nil {
		t.Errorf("expected nil candidates, got %v", candidates)
	}
}

func TestIsHealthy_SidecarUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "techniques": 650})
	}))
	defer srv.Close()

	c := classifier.NewClient(srv.URL)
	if !c.IsHealthy(context.Background()) {
		t.Error("expected healthy")
	}
}
