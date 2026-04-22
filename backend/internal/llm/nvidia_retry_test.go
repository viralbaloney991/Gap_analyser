package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNvidia_429BackoffRespectsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p := &nvidiaProvider{apiKey: "test", model: "test-model", endpoint: srv.URL}
	start := time.Now()
	result, err := p.Complete(context.Background(), CompletionRequest{
		UserMessage: "hello",
		FastMode:    true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("want 'ok', got %q", result)
	}
	if calls.Load() != 3 {
		t.Fatalf("want 3 calls, got %d", calls.Load())
	}
	// 2 retries × 1s Retry-After = at least 2s total
	if elapsed < 2*time.Second {
		t.Fatalf("expected at least 2s elapsed (2 retry sleeps), got %s", elapsed)
	}
}

func TestNvidia_429MaxRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &nvidiaProvider{apiKey: "test", model: "test-model", endpoint: srv.URL}
	_, err := p.Complete(context.Background(), CompletionRequest{
		UserMessage: "hello",
		FastMode:    true,
	})
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
}

func TestNvidia_NonRetryableErrorFastFails(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	p := &nvidiaProvider{apiKey: "test", model: "test-model", endpoint: srv.URL}
	_, err := p.Complete(context.Background(), CompletionRequest{
		UserMessage: "hello",
		FastMode:    true,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls.Load() != 1 {
		t.Fatalf("want 1 call (no retry for 500), got %d", calls.Load())
	}
}
