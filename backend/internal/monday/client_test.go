package monday

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGroups(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"data":{"boards":[{"groups":[{"id":"group_abc","title":"JioStar"},{"id":"group_def","title":"Deel"}]}]}}`)
	}))
	defer srv.Close()

	c := &Client{apiToken: "token", boardID: 123, baseURL: srv.URL}
	groups, err := c.FetchGroups(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].ID != "group_abc" || groups[0].Title != "JioStar" {
		t.Errorf("unexpected group[0]: %+v", groups[0])
	}
	if groups[1].ID != "group_def" || groups[1].Title != "Deel" {
		t.Errorf("unexpected group[1]: %+v", groups[1])
	}
}

func TestFetchGroups_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "internal error")
	}))
	defer srv.Close()

	c := &Client{apiToken: "token", boardID: 123, baseURL: srv.URL}
	_, err := c.FetchGroups(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestFetchGroups_EmptyBoard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"data":{"boards":[]}}`)
	}))
	defer srv.Close()

	c := &Client{apiToken: "token", boardID: 999, baseURL: srv.URL}
	_, err := c.FetchGroups(context.Background())
	if err == nil {
		t.Fatal("expected error for missing board, got nil")
	}
}
