package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/classifier"
)

const (
	mitreCacheTTL    = 7 * 24 * time.Hour
	mitreWorkers     = 5
	mitreCachePrefix = "mitre_llm_v1:"
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

var mitreSystemPrompt = `You are a MITRE ATT&CK detection engineer. Analyze the alert name and detection query (Lucene or DataPrime syntax) to identify which MITRE ATT&CK Enterprise techniques this alert detects.

Rules:
- Base your answer on the DETECTION LOGIC in the query — field names, operators, and values
- Only return techniques that are DIRECTLY evidenced by what the query is looking for
- Return parent technique IDs (T1234) or sub-techniques (T1234.001) when you can be specific
- If the query cannot be mapped to any technique with confidence, return []
- Return at most 5 techniques, ordered by confidence
- Respond ONLY with a JSON array of strings. No markdown, no explanation.

Examples:
Alert Name: CloudTrail - Assume Role from Unknown IP
Detection Query: eventName:AssumeRole AND NOT sourceIPAddress:(known_ips)
Response: ["T1078","T1550.001"]

Alert Name: CloudTrail - Delete CloudTrail Logging
Detection Query: eventName:DeleteTrail OR eventName:StopLogging OR eventName:UpdateTrail
Response: ["T1562.008"]

Alert Name: Okta - Brute Force Login Attempts
Detection Query: outcome.result:FAILURE AND eventType:user.session.start
Response: ["T1110"]`

// BatchMapMITRE runs LLM-based MITRE technique classification across all provided
// alerts. Results are cached per-alert in Redis for 7 days. Returns map[alertID][]techniqueID.
// Per-alert errors are logged and skipped — the caller gets an empty slice for those alerts.
func BatchMapMITRE(ctx context.Context, provider Provider, store MITRECacheStore, inputs []AlertInput) map[string][]string {
	result := make(map[string][]string, len(inputs))
	var mu sync.Mutex

	type work struct {
		input    AlertInput
		cacheKey string
	}

	// Separate cached from uncached.
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

	log.Printf("INFO [mitre_mapper] total=%d cached=%d llm_needed=%d",
		len(inputs), len(inputs)-len(uncached), len(uncached))

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
				// Per-alert timeout so one slow call doesn't block the pool.
				aCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
				techs, err := mapSingleAlert(aCtx, provider, w.input)
				cancel()

				if err != nil {
					log.Printf("WARN [mitre_mapper] alert=%s name=%q: %v", w.input.ID, w.input.Name, err)
					techs = []string{}
				}

				if data, jerr := json.Marshal(techs); jerr == nil {
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

func mapSingleAlert(ctx context.Context, provider Provider, inp AlertInput) ([]string, error) {
	query := inp.Query
	if query == "" {
		query = "(no query)"
	}

	var sb strings.Builder
	sb.WriteString("Alert Name: ")
	sb.WriteString(inp.Name)
	if inp.App != "" {
		sb.WriteString("\nApplication: ")
		sb.WriteString(inp.App)
	}
	if inp.Subsystem != "" {
		sb.WriteString("\nSubsystem: ")
		sb.WriteString(inp.Subsystem)
	}
	sb.WriteString("\nDetection Query: ")
	sb.WriteString(query)

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: mitreSystemPrompt,
		UserMessage:  sb.String(),
		MaxTokens:    512,
		FastMode:     true, // classification — no need for extended reasoning
	})
	if err != nil {
		return nil, err
	}

	return parseTechniqueList(resp)
}

func parseTechniqueList(raw string) ([]string, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx > 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var techs []string
	if err := json.Unmarshal([]byte(cleaned), &techs); err != nil {
		return nil, fmt.Errorf("parse: %w (raw: %.100s)", err, raw)
	}

	// Normalize and validate T-codes.
	result := make([]string, 0, len(techs))
	for _, t := range techs {
		t = strings.TrimSpace(strings.ToUpper(t))
		if len(t) >= 5 && strings.HasPrefix(t, "T") {
			result = append(result, t)
		}
	}
	return result, nil
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
