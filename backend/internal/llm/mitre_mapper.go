package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/classifier"
)

const (
	mitreCacheTTL    = 7 * 24 * time.Hour
	mitreWorkers     = 5
	mitreCachePrefix = "mitre_v3:"
)

// MITRECacheStore is the subset of the cache.Store needed for per-alert caching.
type MITRECacheStore interface {
	GetString(ctx context.Context, key string) (string, bool)
	SetString(ctx context.Context, key, value string, ttl time.Duration)
}

// AlertInput is the minimal per-alert data needed for LLM MITRE mapping.
type AlertInput struct {
	ID        string
	Name      string
	Query     string // Lucene or DataPrime query extracted from TypeDef
	App       string // applicationName filter from logsFilter
	Subsystem string // subsystemName filter from logsFilter
}

func alertHash(name, query, app, subsystem string) string {
	h := sha256.Sum256([]byte(name + "\x00" + query + "\x00" + app + "\x00" + subsystem))
	return fmt.Sprintf("%x", h[:8])
}

// ClassifierClientIface allows injecting the classifier client (or a mock in tests).
type ClassifierClientIface interface {
	ClassifyAlert(ctx context.Context, name, query, app, subsystem string) ([]classifier.Candidate, error)
}

// BatchClassifyAndValidate runs the two-stage MITRE mapping pipeline:
// 1. Classifier sidecar → top-3 semantic candidates per alert
// 2. Llama validator → confirmed technique IDs
// Results are cached per-alert in Redis for 7 days.
// Falls back gracefully: if sidecar is down, candidates are empty and validator is skipped.
func BatchClassifyAndValidate(
	ctx context.Context,
	classifierClient ClassifierClientIface,
	validatorProvider Provider,
	store MITRECacheStore,
	inputs []AlertInput,
) map[string][]string {
	result := make(map[string][]string, len(inputs))
	var mu sync.Mutex

	type work struct {
		input    AlertInput
		cacheKey string
	}

	// Check cache first.
	var uncached []work
	for _, inp := range inputs {
		key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
		if val, ok := store.GetString(ctx, key); ok {
			var techs []string
			if err := json.Unmarshal([]byte(val), &techs); err == nil {
				result[inp.ID] = techs
				continue
			}
		}
		uncached = append(uncached, work{input: inp, cacheKey: key})
	}

	log.Printf("INFO [classifier] total=%d cached=%d to_map=%d", len(inputs), len(inputs)-len(uncached), len(uncached))

	if len(uncached) == 0 {
		return result
	}

	jobs := make(chan work, len(uncached))
	for _, w := range uncached {
		jobs <- w
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < mitreWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				techs := classifyAndValidateSingle(ctx, classifierClient, validatorProvider, w.input)
				if data, err := json.Marshal(techs); err == nil {
					store.SetString(ctx, w.cacheKey, string(data), mitreCacheTTL)
				}
				mu.Lock()
				result[w.input.ID] = techs
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return result
}

func classifyAndValidateSingle(
	ctx context.Context,
	classifierClient ClassifierClientIface,
	validatorProvider Provider,
	inp AlertInput,
) []string {
	aCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Stage 1: classifier sidecar
	candidates, err := classifierClient.ClassifyAlert(aCtx, inp.Name, inp.Query, inp.App, inp.Subsystem)
	if err != nil {
		log.Printf("WARN [classifier] alert=%s: %v", inp.ID, err)
		return []string{}
	}
	if len(candidates) == 0 {
		return []string{}
	}

	// Stage 2: Llama validation
	confirmed, err := ValidateCandidates(aCtx, validatorProvider, inp.Name, inp.Query, inp.App, inp.Subsystem, candidates)
	if err != nil {
		log.Printf("WARN [validator] alert=%s: %v — using raw candidates", inp.ID, err)
		// Fall back to raw classifier output
		techs := make([]string, 0, len(candidates))
		for _, c := range candidates {
			techs = append(techs, c.TechniqueID)
		}
		return techs
	}
	return confirmed
}
