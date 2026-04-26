package llm

import (
	"context"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/pipeline"
)

type mockProvider struct {
	response string
	err      error
	calls    int
}

func (m *mockProvider) Complete(_ context.Context, _ CompletionRequest) (string, error) {
	m.calls++
	return m.response, m.err
}

func (m *mockProvider) Name() string { return "mock" }

type mockStore struct {
	data map[string]string
}

func (s *mockStore) GetString(_ context.Context, key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

func (s *mockStore) SetString(_ context.Context, key, value string, _ time.Duration) {
	s.data[key] = value
}

func TestBatchClassify_EmptyInputs(t *testing.T) {
	provider := &mockProvider{response: `[]`}
	store := &mockStore{data: make(map[string]string)}
	sem := pipeline.NewSemaphore(5)
	result := BatchClassify(context.Background(), provider, store, sem, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil inputs, got %v", result)
	}
	if provider.calls != 0 {
		t.Errorf("expected no LLM calls for empty inputs, got %d", provider.calls)
	}
}

func TestBatchClassify_CacheHitSkipsLLM(t *testing.T) {
	inp := AlertInput{ID: "a1", Name: "Test Alert", Query: "foo", App: "myapp", Subsystem: "sub"}
	key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
	store := &mockStore{data: map[string]string{
		key: `["T1059.001"]`,
	}}
	provider := &mockProvider{response: `["T1078"]`}
	sem := pipeline.NewSemaphore(5)
	result := BatchClassify(context.Background(), provider, store, sem, []AlertInput{inp})
	if provider.calls != 0 {
		t.Errorf("expected no LLM calls on cache hit, got %d", provider.calls)
	}
	if len(result["a1"]) != 1 || result["a1"][0] != "T1059.001" {
		t.Errorf("expected cached [T1059.001], got %v", result["a1"])
	}
}

func TestBatchClassify_LLMCallOnCacheMiss(t *testing.T) {
	inp := AlertInput{ID: "a1", Name: "Test Alert", Query: "foo", App: "myapp", Subsystem: "sub"}
	store := &mockStore{data: make(map[string]string)}
	provider := &mockProvider{response: `["T1059.001"]`}
	sem := pipeline.NewSemaphore(5)
	result := BatchClassify(context.Background(), provider, store, sem, []AlertInput{inp})
	if provider.calls != 1 {
		t.Errorf("expected 1 LLM call on cache miss, got %d", provider.calls)
	}
	if len(result["a1"]) != 1 || result["a1"][0] != "T1059.001" {
		t.Errorf("expected [T1059.001], got %v", result["a1"])
	}
}
