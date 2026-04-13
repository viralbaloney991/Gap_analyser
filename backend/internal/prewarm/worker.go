package prewarm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/mitre"
	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/store"
)

const (
	prewarmWorkers = 3
	maxLogSources  = 30
)

// Worker pre-warms the NeonDB suggestion cache for uncovered MITRE techniques.
// It runs in the background after each /api/analyze call.
type Worker struct {
	config     *config.Config
	alertStore *store.Store
	monday     *monday.Client
}

// New creates a Worker. alertStore must be non-nil before calling Start.
func New(cfg *config.Config, alertStore *store.Store, mondayClient *monday.Client) *Worker {
	return &Worker{
		config:     cfg,
		alertStore: alertStore,
		monday:     mondayClient,
	}
}

// fetchLogSources returns a deduplicated, capped list of log sources for a client.
// Monday integrations take priority; alert data sources supplement them.
func (w *Worker) fetchLogSources(ctx context.Context, client string, clientCfg config.ClientConfig) ([]string, error) {
	var integrations []monday.Integration
	var alerts []*models.AlertDef

	var wg sync.WaitGroup
	var mondayErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		integrations, mondayErr = w.monday.FetchIntegrations(ctx, clientCfg.MondayGroupID)
	}()
	go func() {
		defer wg.Done()
		if w.alertStore != nil {
			stored, err := w.alertStore.LoadAlerts(ctx, client)
			if err != nil {
				log.Printf("WARN [prewarm] store load client=%s: %v", client, err)
			} else if len(stored) > 0 {
				alerts = stored
			}
		}
		// No live fetch — prewarm uses NeonDB store only to avoid duplicate API calls.
	}()
	wg.Wait()

	if mondayErr != nil {
		log.Printf("WARN [prewarm] monday fetch client=%s: %v", client, mondayErr)
	}

	seen := make(map[string]bool)
	var logSources []string

	for _, integ := range integrations {
		if integ.Name != "" && !seen[integ.Name] {
			seen[integ.Name] = true
			logSources = append(logSources, integ.Name)
		}
	}
	coralogix.ExtractFeatures(alerts, nil)
	for _, alert := range alerts {
		for _, ds := range alert.Features.DataSources {
			if !seen[ds] {
				seen[ds] = true
				logSources = append(logSources, ds)
			}
		}
	}
	if len(logSources) > maxLogSources {
		logSources = logSources[:maxLogSources]
	}
	return logSources, nil
}

// buildCacheKey returns the same stable SHA256 key used by HandleSuggestions.
// Log sources are sorted before hashing so insertion order does not affect the key.
func buildCacheKey(techniqueID string, logSources []string) string {
	sorted := make([]string, len(logSources))
	copy(sorted, logSources)
	sort.Strings(sorted)
	raw := techniqueID + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Start pre-warms the suggestion cache for all uncovered techniques for the given client.
// It runs until all techniques are processed or ctx is cancelled.
// Intended to be called as a goroutine after HandleAnalyze responds.
func (w *Worker) Start(ctx context.Context, client string, clientCfg config.ClientConfig, techniques []mitre.UncoveredTechnique) {
	log.Printf("INFO [prewarm] client=%s starting techniques=%d workers=%d", client, len(techniques), prewarmWorkers)
	start := time.Now()

	logSources, err := w.fetchLogSources(ctx, client, clientCfg)
	if err != nil {
		log.Printf("WARN [prewarm] client=%s log source fetch failed: %v — aborting", client, err)
		return
	}
	if len(logSources) == 0 {
		log.Printf("WARN [prewarm] client=%s no log sources found — aborting", client)
		return
	}

	// Build suggestion LLM provider (same config as HandleSuggestions).
	nvidiaKey := w.config.LLM.NvidiaAPIKey
	if w.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = w.config.LLM.NvidiaSuggestionAPIKey
	}
	provider, err := llm.NewClassifierProvider(w.config.LLM.SuggestionProvider, w.config.LLM.SuggestionModel, llm.ProviderConfig{
		AnthropicAPIKey: w.config.LLM.AnthropicAPIKey,
		ClaudeModel:     w.config.LLM.ClaudeModel,
		NvidiaAPIKey:    nvidiaKey,
		NvidiaModel:     w.config.LLM.NvidiaModel,
		NvidiaEndpoint:  w.config.LLM.NvidiaEndpoint,
		GeminiAPIKey:    w.config.LLM.GeminiAPIKey,
		GeminiModel:     w.config.LLM.GeminiModel,
	})
	if err != nil {
		log.Printf("WARN [prewarm] client=%s provider init failed: %v — aborting", client, err)
		return
	}

	sem := make(chan struct{}, prewarmWorkers)
	var wg sync.WaitGroup
	warmed, skipped, errors := 0, 0, 0
	var mu sync.Mutex

	for _, tech := range techniques {
		// Exit early on cancellation.
		select {
		case <-ctx.Done():
			log.Printf("INFO [prewarm] client=%s cancelled after %d processed", client, warmed+skipped)
			wg.Wait()
			return
		default:
		}

		cacheKey := buildCacheKey(tech.ID, logSources)

		// Skip if already cached.
		rows, err := w.alertStore.GetCachedSuggestions(ctx, cacheKey)
		if err == nil && len(rows) > 0 {
			mu.Lock()
			skipped++
			mu.Unlock()
			log.Printf("INFO [prewarm] client=%s technique=%s status=skipped", client, tech.ID)
			continue
		}

		// Acquire semaphore slot (blocks if all 3 workers busy).
		sem <- struct{}{}
		wg.Add(1)

		go func(t mitre.UncoveredTechnique, key string) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := llm.GenerateSuggestions(ctx, provider, llm.GapInput{
				LogSources: logSources,
				Technique: llm.TechniqueInput{
					ID:     t.ID,
					Name:   t.Name,
					Tactic: t.Tactic,
				},
			})
			if err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				log.Printf("WARN [prewarm] client=%s technique=%s error=%v", client, t.ID, err)
				return
			}

			sugJSON, err := json.Marshal(result.Suggestions)
			if err != nil {
				log.Printf("WARN [prewarm] client=%s technique=%s marshal error=%v", client, t.ID, err)
				return
			}

			row := store.SuggestionRow{
				CacheKey:    key,
				TechniqueID: t.ID,
				LogSources:  logSources,
				Suggestions: sugJSON,
				Provider:    result.Provider,
				GeneratedAt: time.Now(),
			}
			if err := w.alertStore.AppendCachedSuggestions(ctx, row); err != nil {
				log.Printf("WARN [prewarm] client=%s technique=%s store error=%v", client, t.ID, err)
				return
			}

			mu.Lock()
			warmed++
			mu.Unlock()
			log.Printf("INFO [prewarm] client=%s technique=%s status=warmed", client, t.ID)
		}(tech, cacheKey)
	}

	wg.Wait()
	log.Printf("INFO [prewarm] client=%s done warmed=%d skipped=%d errors=%d elapsed=%s",
		client, warmed, skipped, errors, time.Since(start).Round(time.Second))
}
