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

	missing, err := s.GetDetection(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetDetection not-found: %v", err)
	}
	if missing != nil {
		t.Errorf("want nil for unknown id, got %+v", missing)
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
