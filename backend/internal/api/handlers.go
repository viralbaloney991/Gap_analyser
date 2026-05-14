package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/cache"
	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/insights"
	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/merge"
	"coralogix-alert-analyzer/internal/mitre"
	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/pipeline"
	"coralogix-alert-analyzer/internal/prewarm"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/similarity"
	"coralogix-alert-analyzer/internal/store"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	config         *config.Config
	mondayClient   *monday.Client
	cache          *cache.Store
	alertStore     *store.Store // NeonDB; nil if unavailable — falls back to live fetch
	prewarmWorker  *prewarm.Worker
	prewarmCancels sync.Map // client string → context.CancelFunc
	sem            *pipeline.Semaphore
	cxBinPath      string     // path to cx binary; empty means cx not configured
	cxExec         cxExecutor // nil → real cxRunner; non-nil → injected (tests)
}

// NewHandler creates a new Handler.
// redisStore and alertStore may each be nil if the respective service is unavailable.
func NewHandler(cfg *config.Config, redisStore *cache.Store, alertStore *store.Store, prewarmWorker *prewarm.Worker, sem *pipeline.Semaphore) *Handler {
	return &Handler{
		config:        cfg,
		mondayClient:  monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:         redisStore,
		alertStore:    alertStore,
		prewarmWorker: prewarmWorker,
		sem:           sem,
	}
}

// SetCxBinPath configures the cx binary path for the hunt feature.
// If path is empty it tries to auto-discover cx via PATH.
func (h *Handler) SetCxBinPath(path string) {
	if path == "" {
		if found, err := exec.LookPath("cx"); err == nil {
			path = found
		}
	}
	h.cxBinPath = path
}

// HandleClients returns the list of configured clients with their region.
func (h *Handler) HandleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, h.config.ClientsWithRegion())
}

// HandleAnalyze runs the full analysis pipeline for a client.
// Supports ?refresh=true to bust the cache.
func (h *Handler) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.ClientAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}

	clientCfg, ok := h.config.Clients[req.Client]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	ctx := r.Context()
	refresh := r.URL.Query().Get("refresh") == "true"

	// Check cache unless refresh requested.
	if !refresh && h.cache != nil {
		if cached := h.cache.Get(ctx, req.Client); cached != nil {
			cached.Cached = true
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	// Cache miss or refresh — run full pipeline.
	if refresh {
		log.Printf("INFO [analyze] refresh requested for client=%s", req.Client)
	}

	// Fetch Monday integrations and Coralogix alerts in parallel.
	var (
		integrations []monday.Integration
		alerts       []*models.AlertDef
		mondayErr    error
		alertsErr    error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		integrations, mondayErr = h.mondayClient.FetchIntegrations(ctx, clientCfg.MondayGroupID)
	}()

	go func() {
		defer wg.Done()
		if h.alertStore != nil {
			stored, err := h.alertStore.LoadAlerts(ctx, req.Client)
			if err != nil {
				log.Printf("WARN [analyze] load from store client=%s: %v — falling back to live", req.Client, err)
			} else if len(stored) > 0 {
				log.Printf("INFO [analyze] loaded %d alerts from store client=%s", len(stored), req.Client)
				alerts = stored
				return
			} else {
				log.Printf("INFO [analyze] store empty for client=%s — fetching live (first boot)", req.Client)
			}
		}
		alerts, alertsErr = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
	}()

	wg.Wait()

	if alertsErr != nil {
		log.Printf("ERROR [analyze] fetch alerts client=%s: %v", req.Client, alertsErr)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch alerts: %v", alertsErr))
		return
	}
	if mondayErr != nil {
		log.Printf("WARN [analyze] monday fetch client=%s: %v", req.Client, mondayErr)
	}

	log.Printf("INFO [analyze] client=%s alerts=%d integrations=%d", req.Client, len(alerts), len(integrations))

	// Build MITRE mappings via Claude classifier.
	// Only runs on security alerts with no existing label/T-code coverage.
	// Results are cached per-alert in Redis for 7 days.
	var llmMappings map[string][]string
	if h.cache != nil {
		baseCfg := llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		}
		classifierProvider, err := llm.NewClassifierProvider(
			h.config.LLM.ClassifierProvider,
			h.config.LLM.ClassifierModel,
			baseCfg,
		)
		if err != nil {
			log.Printf("WARN [analyze] classifier provider unavailable: %v", err)
		} else {
			var inputs []llm.AlertInput
			for _, a := range alerts {
				if !coralogix.IsSecurityAlert(a) || coralogix.HasExistingMITRE(a) {
					continue
				}
				app, subsystem := coralogix.ExtractAppSubsystem(a.TypeDef)
				inputs = append(inputs, llm.AlertInput{
					ID:        a.ID,
					Name:      a.Name,
					Query:     coralogix.ExtractLuceneQuery(a.TypeDef),
					App:       app,
					Subsystem: subsystem,
				})
			}
			log.Printf("INFO [analyze] MITRE pipeline: %d/%d alerts need classification", len(inputs), len(alerts))
			if len(inputs) > 0 {
				llmMappings = llm.BatchClassify(ctx, classifierProvider, h.cache, h.sem, inputs)
			}
		}
	} else {
		log.Printf("WARN [analyze] cache unavailable, skipping MITRE classification for client=%s", req.Client)
	}

	// Extract features.
	coralogix.ExtractFeatures(alerts, llmMappings)

	// Match integrations to alerts.
	matched := merge.CountAlertsByIntegration(integrations, alerts)

	// Run MITRE coverage.
	mitreCoverage := mitre.AnalyzeCoverage(alerts)

	// Fetch trigger counts for behavioral noise detection using the requested lookback window.
	// Returns nil on failure — findNoiseAlerts falls back to structural-only.
	lookback := validateLookbackDays(req.LookbackDays)
	alertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		alertIDs[i] = a.ID
	}
	eventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, alertIDs, lookback)
	if eventCounts == nil {
		log.Printf("WARN [noise] event counts unavailable for client=%s — structural-only noise", req.Client)
	}

	// Run similarity analysis.
	alertInsights := similarity.Analyze(alerts, eventCounts, len(integrations), mitreCoverage)

	// Build integration info for response.
	integrationInfos := make([]models.IntegrationInfo, len(matched))
	withAlerts := 0
	for i, m := range matched {
		integrationInfos[i] = models.IntegrationInfo{
			Name:               m.Name,
			Application:        m.Application,
			Subsystem:          m.Subsystem,
			AlertCount:         m.AlertCount,
			VendorCoveredCount: m.VendorCoveredCount,
			PriorityCounts:     m.PriorityCounts,
		}
		if m.AlertCount > 0 {
			withAlerts++
		}
	}

	// Count security and vendor-covered alerts.
	securityCount := 0
	vendorCoveredCount := 0
	for _, a := range alerts {
		if a.Features.IsSecurityAlert {
			securityCount++
		}
		if a.Features.VendorCovered {
			vendorCoveredCount++
		}
	}

	resp := &models.AnalyzeResponse{
		Integrations: integrationInfos,
		Stats: models.AnalysisStats{
			TotalIntegrations:      len(integrations),
			DoneIntegrations:       len(integrations),
			TotalAlerts:            len(alerts),
			SecurityAlerts:         securityCount,
			VendorCoveredAlerts:    vendorCoveredCount,
			IntegrationsWithAlerts: withAlerts,
		},
		MITRECoverage:  mitreCoverage,
		AlertInsights:  alertInsights,
		InsightsReport: nil, // fetched separately via POST /api/insights
		Cached:         false,
	}

	// Store in cache.
	if h.cache != nil {
		h.cache.Set(ctx, req.Client, resp)
	}

	writeJSON(w, http.StatusOK, resp)

	// Trigger background suggestion pre-warm (non-blocking).
	// Cancels any in-flight pre-warm for this client first.
	if h.prewarmWorker != nil && h.alertStore != nil {
		if prev, ok := h.prewarmCancels.Load(req.Client); ok {
			if cancel, ok := prev.(context.CancelFunc); ok {
				cancel()
			}
		}
		prewarmCtx, cancel := context.WithCancel(context.Background())
		h.prewarmCancels.Store(req.Client, cancel)
		uncovered := mitre.GetUncoveredTechniques(alerts)
		go h.prewarmWorker.Start(prewarmCtx, req.Client, clientCfg, uncovered)
	}

	// Trigger background insights enrichment concurrently with pre-warm.
	// Uses a detached context; semaphore ensures it doesn't crowd out pre-warm workers.
	if h.sem != nil && h.cache != nil {
		h.runInsightsBackground(req.Client, alertInsights, alerts, integrationInfos, mitreCoverage, eventCounts)
	}
}

// HandleHealth returns a simple health check response.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resolveInsightsProvider constructs the LLM provider for insights enrichment
// using the insights-specific config fields, falling back to the default provider.
func resolveInsightsProvider(cfg *config.Config) (llm.Provider, error) {
	providerName := cfg.LLM.InsightsProvider
	if providerName == "" {
		providerName = cfg.LLM.DefaultProvider
	}
	model := cfg.LLM.InsightsModel
	if model == "" {
		model = cfg.LLM.ClaudeModel
	}
	return llm.NewClassifierProvider(providerName, model, llm.ProviderConfig{
		AnthropicAPIKey: cfg.LLM.AnthropicAPIKey,
		ClaudeModel:     cfg.LLM.ClaudeModel,
		NvidiaAPIKey:    cfg.LLM.NvidiaAPIKey,
		NvidiaModel:     cfg.LLM.NvidiaModel,
		NvidiaEndpoint:  cfg.LLM.NvidiaEndpoint,
		GeminiAPIKey:    cfg.LLM.GeminiAPIKey,
		GeminiModel:     cfg.LLM.GeminiModel,
	})
}

// runInsightsBackground runs LLM insights enrichment as a fire-and-forget goroutine.
// Uses a detached context so that client disconnect does not abort the work.
// Results are stored in Redis for /api/insights to serve.
func (h *Handler) runInsightsBackground(
	client string,
	alertInsights *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	eventCounts map[string]int,
) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	go func() {
		defer cancel()

		insightsProvider, err := resolveInsightsProvider(h.config)
		if err != nil {
			log.Printf("WARN [insights-bg] client=%s provider init failed: %v", client, err)
			return
		}

		// Acquire one semaphore slot for the single insights LLM call.
		if err := h.sem.Acquire(bgCtx); err != nil {
			log.Printf("WARN [insights-bg] client=%s semaphore acquire: %v", client, err)
			return
		}
		defer h.sem.Release()
		ir, enrichErr := insights.Enrich(bgCtx, alertInsights, alerts, integrations, mitreCoverage, eventCounts, insightsProvider)

		if enrichErr != nil {
			log.Printf("WARN [insights-bg] client=%s enrich: %v", client, enrichErr)
			return
		}
		if ir == nil {
			return
		}

		var customerManaged []models.IntegrationInfo
		allVendorManaged := len(integrations) > 0
		for _, info := range integrations {
			isVendorManaged := info.AlertCount > 0 && info.VendorCoveredCount == info.AlertCount
			if !isVendorManaged {
				allVendorManaged = false
				customerManaged = append(customerManaged, info)
			}
		}
		ir.AllIntegrationsVendorManaged = allVendorManaged

		actionable, aErr := insights.EnrichActionable(bgCtx, ir.GapCategories, customerManaged, insightsProvider)
		if aErr != nil {
			log.Printf("WARN [insights-bg] client=%s actionable enrich: %v", client, aErr)
		}
		ir.ActionableGaps = actionable

		if h.cache != nil {
			if key, err := computeInsightsCacheKey(client, alertInsights); err == nil {
				if data, marshalErr := json.Marshal(ir); marshalErr == nil {
					h.cache.SetString(bgCtx, key, string(data), 24*time.Hour)
					log.Printf("INFO [insights-bg] client=%s cached insights", client)
				}
			}
		}
	}()
}

// HandleInsights runs LLM enrichment for alert insights, decoupled from the main
// analyze pipeline so that the heavy Claude Opus call does not block the initial
// page load. The frontend fires this asynchronously after analyzeClient returns.
// POST /api/insights { "client": "X" }
func (h *Handler) HandleInsights(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}

	clientCfg, ok := h.config.Clients[req.Client]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	ctx := r.Context()

	// Load alerts — store-first, same strategy as HandleAnalyze.
	var alerts []*models.AlertDef
	if h.alertStore != nil {
		stored, err := h.alertStore.LoadAlerts(ctx, req.Client)
		if err == nil && len(stored) > 0 {
			alerts = stored
		}
	}
	if len(alerts) == 0 {
		var err error
		alerts, err = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch alerts: %v", err))
			return
		}
	}

	// Fetch event counts for behavioral noise (structural-only fallback on error).
	insightsAlertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		insightsAlertIDs[i] = a.ID
	}
	// Always use 30-day window for insights — lookback_days is an analyze-only param.
	insightsEventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, insightsAlertIDs, 30)
	if insightsEventCounts == nil {
		log.Printf("WARN [noise] event counts unavailable for insights client=%s — structural-only", req.Client)
	}

	// Populate alert features (MITRE technique IDs, data sources, etc.) from alert
	// definitions. Pass nil for llmMappings — LLM-classified T-codes are not available
	// in this path, but existing label-based MITRE tags are extracted correctly.
	// Without this call, alert.Features.Techniques is always empty and AnalyzeCoverage
	// reports zero coverage across all 14 tactics.
	coralogix.ExtractFeatures(alerts, nil)

	// Compute MITRE coverage for accurate gap detection in the insights prompt.
	// In-memory computation (~1ms), no external calls.
	insightsMitre := mitre.AnalyzeCoverage(alerts)

	// Similarity analysis is fast (< 1s) and required for the cache key + LLM prompt.
	// Pass 0 for integrationCount — Monday not fetched in this path; structural reason
	// text won't include org integration count but all other noise signals are accurate.
	// insightsMitre is passed so tactic gap detection uses real coverage data.
	alertInsights := similarity.Analyze(alerts, insightsEventCounts, 0, insightsMitre)

	// Check insights cache. Provider is fixed to Claude Opus — cache is always consulted.
	var insightsCacheKey string
	if h.cache != nil {
		if key, err := computeInsightsCacheKey(req.Client, alertInsights); err == nil {
			insightsCacheKey = key
			if cached, ok := h.cache.GetString(ctx, key); ok {
				var ir models.InsightsReport
				if json.Unmarshal([]byte(cached), &ir) == nil {
					log.Printf("INFO [insights] cache HIT client=%s", req.Client)
					writeJSON(w, http.StatusOK, &ir)
					return
				}
			}
		}
	}

	modelLabel := "Claude Opus 4.7"
	insightsProvider, err := resolveInsightsProvider(h.config)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("insights provider unavailable: %v", err))
		return
	}

	// Monday integrations are not fetched in this synchronous path to avoid blocking.
	// missing_source_alerts will be empty; all other gap categories are fully populated.
	ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, nil, insightsMitre, insightsEventCounts, insightsProvider)
	if enrichErr != nil {
		log.Printf("WARN [insights] enrich client=%s: %v", req.Client, enrichErr)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("insights enrichment failed: %v", enrichErr))
		return
	}
	if ir == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ir.Model = modelLabel

	if h.cache != nil && insightsCacheKey != "" {
		if data, marshalErr := json.Marshal(ir); marshalErr == nil {
			h.cache.SetString(ctx, insightsCacheKey, string(data), 24*time.Hour)
		}
	}

	writeJSON(w, http.StatusOK, ir)
}

// HandleNoise re-runs only the noise detection step for a different lookback window.
// POST /api/noise { "client": "X", "lookback_days": 7 }
// Response: { "noise_alerts": [...], "lookback_days": 7 }
func (h *Handler) HandleNoise(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.NoiseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}

	clientCfg, ok := h.config.Clients[req.Client]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	lookback := validateLookbackDays(req.LookbackDays)
	ctx := r.Context()

	// Load alerts — store-first, same strategy as HandleInsights.
	var alerts []*models.AlertDef
	if h.alertStore != nil {
		stored, err := h.alertStore.LoadAlerts(ctx, req.Client)
		if err == nil && len(stored) > 0 {
			alerts = stored
		}
	}
	if len(alerts) == 0 {
		var err error
		alerts, err = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch alerts: %v", err))
			return
		}
	}

	alertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		alertIDs[i] = a.ID
	}
	multiCounts := fetchEventCountsMultiWindow(ctx, clientCfg.Region, clientCfg.APIKey, alertIDs)
	if multiCounts == nil {
		log.Printf("WARN [noise] multi-window event counts unavailable client=%s — structural-only", req.Client)
	} else {
		log.Printf("INFO [noise] multi-window event counts: requested=%d fetched=%d client=%s", len(alertIDs), len(multiCounts), req.Client)
	}

	coralogix.ExtractFeatures(alerts, nil)

	// Pass 0 for integrationCount — Monday not fetched in this path; structural
	// reason text won't include org integration count but all noise signals are accurate.
	noiseAlerts := similarity.AnalyzeNoiseMultiWindow(alerts, multiCounts, 0)
	if noiseAlerts == nil {
		noiseAlerts = []models.NoiseAlert{}
	}

	writeJSON(w, http.StatusOK, models.NoiseResponse{
		NoiseAlerts:  noiseAlerts,
		LookbackDays: lookback,
	})
}

// HandleExportNarrative generates an LLM executive narrative for the full PDF report.
// POST /api/export/narrative { "client": "X" }
// Result is not cached — always generated fresh.
func (h *Handler) HandleExportNarrative(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}

	clientCfg, ok := h.config.Clients[req.Client]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	ctx := r.Context()

	var alerts []*models.AlertDef
	if h.alertStore != nil {
		stored, err := h.alertStore.LoadAlerts(ctx, req.Client)
		if err == nil && len(stored) > 0 {
			alerts = stored
		}
	}
	if len(alerts) == 0 {
		var err error
		alerts, err = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch alerts: %v", err))
			return
		}
	}

	coralogix.ExtractFeatures(alerts, nil)
	mitreCoverage := mitre.AnalyzeCoverage(alerts)

	// Fetch event counts so the similarity result matches the one used when
	// the insights cache was written — without this, the cache key hash diverges
	// and cachedReport is always nil, causing a 204 every time.
	exportAlertIDs := make([]string, len(alerts))
	for i, a := range alerts {
		exportAlertIDs[i] = a.ID
	}
	// Always use 30-day window for export — lookback_days is an analyze-only param.
	exportEventCounts := fetchEventCounts(ctx, clientCfg.Region, clientCfg.APIKey, exportAlertIDs, 30)
	alertInsights := similarity.Analyze(alerts, exportEventCounts, 0, mitreCoverage)

	var cachedReport *models.InsightsReport
	if h.cache != nil {
		if key, err := computeInsightsCacheKey(req.Client, alertInsights); err == nil {
			if cached, ok := h.cache.GetString(ctx, key); ok {
				var ir models.InsightsReport
				if json.Unmarshal([]byte(cached), &ir) == nil {
					cachedReport = &ir
					log.Printf("INFO [export] insights cache HIT client=%s", req.Client)
				}
			}
		}
	}

	insightsProvider, err := resolveInsightsProvider(h.config)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("insights provider unavailable: %v", err))
		return
	}

	// If insights aren't cached, generate them inline so the export is self-sufficient.
	// The result is not cached here — export always generates a fresh narrative.
	if cachedReport == nil {
		log.Printf("INFO [export] insights cache MISS client=%s — generating inline", req.Client)
		ir, enrichErr := insights.Enrich(ctx, alertInsights, alerts, nil, mitreCoverage, exportEventCounts, insightsProvider)
		if enrichErr != nil {
			log.Printf("WARN [export] inline insights enrich client=%s: %v", req.Client, enrichErr)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("insights generation failed: %v", enrichErr))
			return
		}
		cachedReport = ir
	}

	narrative, narErr := insights.EnrichExportNarrative(ctx, alerts, nil, mitreCoverage, alertInsights, cachedReport, insightsProvider)
	if narErr != nil {
		log.Printf("WARN [export] client=%s narrative: %v", req.Client, narErr)
		writeError(w, http.StatusBadGateway, "export narrative generation failed")
		return
	}
	if narrative == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, narrative)
}

// HandleSuggestions generates LLM-powered alert suggestions for a single uncovered technique.
// POST /api/suggestions { "client": "X", "technique_id": "T1059", "tactic": "execution", "provider": "nvidia" }
func (h *Handler) HandleSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.SuggestionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	if req.TechniqueID == "" {
		writeError(w, http.StatusBadRequest, "missing required field: technique_id")
		return
	}

	clientCfg, ok := h.config.Clients[req.Client]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	// Determine LLM provider.
	// When the caller specifies req.Provider, use that provider's default model to avoid
	// cross-provider model contamination (e.g. a Claude model being sent to the Nvidia endpoint).
	// When no provider is specified, use the configured suggestion provider + model.

	// For NVIDIA suggestions, prefer the dedicated suggestion key (may be a different account/model).
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	if h.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
	}

	var provider llm.Provider
	var err error
	if req.Provider != "" {
		// Caller-specified provider: use default model for that provider
		provider, err = llm.NewProvider(req.Provider, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	} else {
		// Use configured suggestion provider + model
		providerName := h.config.LLM.SuggestionProvider
		if providerName == "" {
			providerName = h.config.LLM.DefaultProvider
		}
		provider, err = llm.NewClassifierProvider(providerName, h.config.LLM.SuggestionModel, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	// Fetch Monday integrations and Coralogix alerts in parallel for log sources.
	var (
		integrations []monday.Integration
		alerts       []*models.AlertDef
		mondayErr    error
		alertsErr    error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		integrations, mondayErr = h.mondayClient.FetchIntegrations(ctx, clientCfg.MondayGroupID)
	}()

	go func() {
		defer wg.Done()
		alerts, alertsErr = fetchAlerts(ctx, clientCfg.Region, clientCfg.APIKey)
	}()

	wg.Wait()

	if alertsErr != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch alerts: %v", alertsErr))
		return
	}
	if mondayErr != nil {
		log.Printf("WARN [suggestions] monday fetch client=%s: %v", req.Client, mondayErr)
	}

	// Extract features to populate VendorCovered on each alert.
	coralogix.ExtractFeatures(alerts, nil)

	logSources := buildSuggestionLogSources(integrations, alerts)

	// Resolve technique name from master list
	techniqueName := mitre.GetTechniqueName(req.TechniqueID)
	tactic := req.Tactic
	if tactic == "" {
		tactic = mitre.GetTechniqueTactic(req.TechniqueID)
	}

	effectiveProvider := req.Provider
	if effectiveProvider == "" {
		effectiveProvider = h.config.LLM.SuggestionProvider
		if effectiveProvider == "" {
			effectiveProvider = h.config.LLM.DefaultProvider
		}
	}
	log.Printf("INFO [suggestions] client=%s technique=%s (%s) log_sources=%d provider=%s",
		req.Client, req.TechniqueID, techniqueName, len(logSources), effectiveProvider)

	gapInput := llm.GapInput{
		LogSources: logSources,
		Technique: llm.TechniqueInput{
			ID:     req.TechniqueID,
			Name:   techniqueName,
			Tactic: tactic,
		},
	}

	// Build cache key (requires logSources to be finalised above).
	var cacheKey string
	if h.alertStore != nil {
		cacheKey = buildSuggestionCacheKey(req.TechniqueID, logSources)
	}

	// Cache hit path — skip when force=true or store unavailable.
	if !req.Force && h.alertStore != nil {
		cached, err := h.alertStore.GetCachedSuggestions(ctx, cacheKey)
		if err != nil {
			log.Printf("WARN [suggestions] cache lookup client=%s technique=%s: %v", req.Client, req.TechniqueID, err)
		} else if len(cached) > 0 {
			merged, latestProvider := mergeCachedSuggestions(cached)
			log.Printf("INFO [suggestions] cache hit client=%s technique=%s rows=%d merged=%d",
				req.Client, req.TechniqueID, len(cached), len(merged))
			writeJSON(w, http.StatusOK, models.SuggestionsResponse{
				Provider:      latestProvider,
				TechniqueID:   req.TechniqueID,
				TechniqueName: techniqueName,
				Suggestions:   merged,
				LogSources:    logSources,
			})
			return
		}
	}

	// Cache miss or force — call the LLM.
	result, err := llm.GenerateSuggestions(ctx, provider, gapInput)
	if err != nil {
		log.Printf("ERROR [suggestions] LLM call failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM suggestion failed: %v", err))
		return
	}

	// Append to persistent cache — skip empty results so future requests
	// call the LLM fresh rather than returning a cached empty response.
	if len(result.Suggestions) > 0 && h.alertStore != nil && cacheKey != "" {
		sugsJSON, _ := json.Marshal(result.Suggestions)
		appendErr := h.alertStore.AppendCachedSuggestions(ctx, store.SuggestionRow{
			CacheKey:    cacheKey,
			TechniqueID: req.TechniqueID,
			LogSources:  logSources,
			Suggestions: json.RawMessage(sugsJSON),
			Provider:    result.Provider,
			GeneratedAt: time.Now().UTC(),
		})
		if appendErr != nil {
			log.Printf("WARN [suggestions] cache append client=%s technique=%s: %v", req.Client, req.TechniqueID, appendErr)
		}
	}

	// For force requests, re-fetch the full pool and return the merged result.
	// For cache-miss requests, return the LLM result directly (single row, no merge needed).
	var (
		finalSuggestions []models.AlertSuggestion
		finalProvider    = result.Provider
	)

	if req.Force && h.alertStore != nil && cacheKey != "" {
		allRows, fetchErr := h.alertStore.GetCachedSuggestions(ctx, cacheKey)
		if fetchErr != nil {
			log.Printf("WARN [suggestions] force pool fetch client=%s technique=%s: %v", req.Client, req.TechniqueID, fetchErr)
		} else {
			finalSuggestions, finalProvider = mergeCachedSuggestions(allRows)
		}
	}

	if finalSuggestions == nil {
		finalSuggestions = make([]models.AlertSuggestion, len(result.Suggestions))
		for i, s := range result.Suggestions {
			finalSuggestions[i] = models.AlertSuggestion{
				LogSource:   s.LogSource,
				AlertName:   s.AlertName,
				Description: s.Description,
				QueryHint:   s.QueryHint,
				Priority:    s.Priority,
			}
		}
	}

	writeJSON(w, http.StatusOK, models.SuggestionsResponse{
		Provider:      finalProvider,
		TechniqueID:   req.TechniqueID,
		TechniqueName: techniqueName,
		Suggestions:   finalSuggestions,
		LogSources:    logSources,
	})
}

// buildSuggestionCacheKey returns a stable SHA256 hex key for a (technique, log_sources) pair.
// Log sources are sorted before hashing so insertion order does not affect the key.
func buildSuggestionCacheKey(techniqueID string, logSources []string) string {
	sorted := make([]string, len(logSources))
	copy(sorted, logSources)
	sort.Strings(sorted)
	raw := techniqueID + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// mergeCachedSuggestions flattens all suggestion rows into a single deduplicated list.
// rows must be ordered ASC by generated_at so that later rows win dedup conflicts.
// Returns the merged suggestions sorted by priority then alert_name, and the provider
// of the most recent row.
func mergeCachedSuggestions(rows []store.SuggestionRow) ([]models.AlertSuggestion, string) {
	type entry struct {
		sug   models.AlertSuggestion
		genAt time.Time
	}
	seen := make(map[string]entry)
	var latestProvider string

	priorityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}

	for _, row := range rows {
		var llmSugs []llm.Suggestion
		if err := json.Unmarshal(row.Suggestions, &llmSugs); err != nil {
			log.Printf("WARN [suggestions] unmarshal cached suggestions: %v", err)
			continue
		}
		latestProvider = row.Provider
		for _, s := range llmSugs {
			key := strings.ToLower(s.AlertName)
			existing, exists := seen[key]
			if !exists || row.GeneratedAt.After(existing.genAt) {
				seen[key] = entry{
					sug: models.AlertSuggestion{
						LogSource:   s.LogSource,
						AlertName:   s.AlertName,
						Description: s.Description,
						QueryHint:   s.QueryHint,
						Priority:    s.Priority,
					},
					genAt: row.GeneratedAt,
				}
			}
		}
	}

	merged := make([]models.AlertSuggestion, 0, len(seen))
	for _, e := range seen {
		merged = append(merged, e.sug)
	}
	sort.Slice(merged, func(i, j int) bool {
		pi := priorityOrder[strings.ToLower(merged[i].Priority)]
		pj := priorityOrder[strings.ToLower(merged[j].Priority)]
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(merged[i].AlertName) < strings.ToLower(merged[j].AlertName)
	})
	return merged, latestProvider
}

// computeInsightsCacheKey returns a stable Redis key for the insights report,
// derived from the SimilarityResult JSON content.
// similarity.Analyze() does not guarantee a fully stable sort order for all
// slices (e.g. UniqueDetections is unsorted; Families/Duplicates/MergeSuggestions
// have tie-breaking gaps), so we normalise the result before hashing.
func computeInsightsCacheKey(clientName string, result *models.SimilarityResult) (string, error) {
	// Normalise slices so that tie-breaking variations don't produce different hashes.
	type stableResult struct {
		Families         []models.DetectionFamily
		Duplicates       []models.DuplicateGroup
		MergeSuggestions []models.MergeSuggestion
		UniqueDetections []string
		NoiseAlerts      []models.NoiseAlert
	}

	sr := stableResult{
		Families:         append([]models.DetectionFamily(nil), result.Families...),
		Duplicates:       append([]models.DuplicateGroup(nil), result.Duplicates...),
		MergeSuggestions: append([]models.MergeSuggestion(nil), result.MergeSuggestions...),
		UniqueDetections: append([]string(nil), result.UniqueDetections...),
		NoiseAlerts:      append([]models.NoiseAlert(nil), result.NoiseAlerts...),
	}

	sort.Slice(sr.Families, func(i, j int) bool {
		if len(sr.Families[i].AlertIDs) != len(sr.Families[j].AlertIDs) {
			return len(sr.Families[i].AlertIDs) > len(sr.Families[j].AlertIDs)
		}
		return sr.Families[i].Name < sr.Families[j].Name
	})
	sort.Slice(sr.Duplicates, func(i, j int) bool {
		if sr.Duplicates[i].Similarity != sr.Duplicates[j].Similarity {
			return sr.Duplicates[i].Similarity > sr.Duplicates[j].Similarity
		}
		li := len(sr.Duplicates[i].AlertIDs)
		lj := len(sr.Duplicates[j].AlertIDs)
		if li == 0 && lj == 0 {
			return false
		}
		if li == 0 {
			return true
		}
		if lj == 0 {
			return false
		}
		return sr.Duplicates[i].AlertIDs[0] < sr.Duplicates[j].AlertIDs[0]
	})
	sort.Slice(sr.MergeSuggestions, func(i, j int) bool {
		if len(sr.MergeSuggestions[i].AlertIDs) != len(sr.MergeSuggestions[j].AlertIDs) {
			return len(sr.MergeSuggestions[i].AlertIDs) > len(sr.MergeSuggestions[j].AlertIDs)
		}
		return sr.MergeSuggestions[i].Reason < sr.MergeSuggestions[j].Reason
	})
	sort.Strings(sr.UniqueDetections)
	sort.Slice(sr.NoiseAlerts, func(i, j int) bool {
		return sr.NoiseAlerts[i].Name < sr.NoiseAlerts[j].Name
	})

	data, err := json.Marshal(sr)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	// Replace colons to avoid ambiguous key segments (consistent with SimilarityResult
	// slices being deterministically sorted by similarity.Analyze()).
	safeName := strings.ReplaceAll(clientName, ":", "_")
	return fmt.Sprintf("insights_v3:%s:%s", safeName, hex.EncodeToString(h[:])[:12]), nil
}

// fetchAlerts creates a Coralogix client, fetches active alerts, and closes the client.
func fetchAlerts(ctx context.Context, region, apiKey string) ([]*models.AlertDef, error) {
	client, err := coralogix.NewClient(region, apiKey)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return client.FetchActiveAlerts(ctx)
}

// fetchEventCounts fetches trigger counts for the given alert IDs over the specified number of days.
// Returns nil on any error so callers fall back to structural-only noise detection.
func fetchEventCounts(ctx context.Context, region, apiKey string, alertIDs []string, days int) map[string]int {
	client, err := coralogix.NewClient(region, apiKey)
	if err != nil {
		return nil
	}
	defer client.Close()
	counts, err := client.FetchAlertEventCounts(ctx, alertIDs, days)
	if err != nil {
		log.Printf("DEBUG [noise] event count fetch failed: %v", err)
		return nil
	}
	return counts
}

// fetchEventCountsMultiWindow fetches alert event counts at 4 fixed windows
// (7d/14d/21d/30d) in parallel. Returns nil on complete failure.
func fetchEventCountsMultiWindow(ctx context.Context, region, apiKey string, alertIDs []string) map[string][4]int {
	client, err := coralogix.NewClient(region, apiKey)
	if err != nil {
		return nil
	}
	defer client.Close()
	counts, err := client.FetchAlertEventCountsMultiWindow(ctx, alertIDs)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("DEBUG [noise] multi-window event count fetch cancelled: %v", ctx.Err())
		} else {
			log.Printf("DEBUG [noise] multi-window event count fetch failed: %v", err)
		}
		return nil
	}
	return counts
}

func validateLookbackDays(days int) int {
	switch days {
	case 7, 14, 30, 90:
		return days
	default:
		if days != 0 {
			log.Printf("WARN [validate] invalid lookback_days=%d, defaulting to 30", days)
		}
		return 30
	}
}

// buildCorrelationCacheKey returns a stable SHA256 hex key for a (client, gap_prose) pair.
func buildCorrelationCacheKey(client, gapProse string) string {
	normalisedClient := strings.ToLower(client)
	normalisedGap := strings.ToLower(strings.TrimSpace(gapProse))
	raw := normalisedClient + "|" + normalisedGap
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// mergeCorrelations flattens correlation rows into a deduplicated list.
// rows must be ordered ASC by generated_at so later rows win dedup conflicts.
// Returns merged suggestions sorted by priority then title, and the provider of the most recent row.
func mergeCorrelations(rows []store.CorrelationRow) ([]models.CorrelationSuggestion, string) {
	type entry struct {
		sug   models.CorrelationSuggestion
		genAt time.Time
	}
	seen := make(map[string]entry)
	priorityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	var latestProvider string

	for _, row := range rows {
		var sugs []models.CorrelationSuggestion
		if err := json.Unmarshal(row.Suggestions, &sugs); err != nil {
			log.Printf("WARN [correlations] unmarshal cached correlations: %v", err)
			continue
		}
		latestProvider = row.Provider
		for _, s := range sugs {
			key := strings.ToLower(s.Title)
			existing, exists := seen[key]
			if !exists || row.GeneratedAt.After(existing.genAt) {
				seen[key] = entry{sug: s, genAt: row.GeneratedAt}
			}
		}
	}

	merged := make([]models.CorrelationSuggestion, 0, len(seen))
	for _, e := range seen {
		merged = append(merged, e.sug)
	}
	sort.Slice(merged, func(i, j int) bool {
		pi := priorityOrder[strings.ToLower(merged[i].Priority)]
		pj := priorityOrder[strings.ToLower(merged[j].Priority)]
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(merged[i].Title) < strings.ToLower(merged[j].Title)
	})
	return merged, latestProvider
}

// HandleCorrelations handles POST /api/correlations.
// It returns LLM-generated correlation and anomaly suggestions for a single Advanced Use Cases gap item.
func (h *Handler) HandleCorrelations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.CorrelationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	req.GapProse = strings.TrimSpace(req.GapProse)
	if req.GapProse == "" {
		writeError(w, http.StatusBadRequest, "missing required field: gap_prose")
		return
	}

	if _, ok := h.config.Clients[req.Client]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	// Resolve LLM provider — same logic as HandleSuggestions.
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	if h.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
	}

	var provider llm.Provider
	var err error
	if req.Provider != "" {
		provider, err = llm.NewProvider(req.Provider, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	} else {
		providerName := h.config.LLM.SuggestionProvider
		if providerName == "" {
			providerName = h.config.LLM.DefaultProvider
		}
		provider, err = llm.NewClassifierProvider(providerName, h.config.LLM.SuggestionModel, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	cacheKey := buildCorrelationCacheKey(req.Client, req.GapProse)

	// Cache hit path.
	if !req.Force && h.alertStore != nil {
		cached, cErr := h.alertStore.GetCachedCorrelations(ctx, cacheKey)
		if cErr != nil {
			log.Printf("WARN HandleCorrelations client=%s cache lookup: %v", req.Client, cErr)
		} else if len(cached) > 0 {
			merged, latestProvider := mergeCorrelations(cached)
			log.Printf("INFO HandleCorrelations client=%s cache=hit suggestions=%d", req.Client, len(merged))
			writeJSON(w, http.StatusOK, models.CorrelationsResponse{
				Suggestions: merged,
				Provider:    latestProvider,
				Cached:      true,
			})
			return
		}
	}

	// Cache miss — call LLM.
	input := llm.CorrelationInput{
		GapProse:          req.GapProse,
		LogSources:        req.LogSources,
		CoveredTechniques: req.CoveredTechniques,
	}
	sugs, llmErr := llm.GenerateCorrelations(ctx, provider, input)
	if llmErr != nil {
		log.Printf("ERROR HandleCorrelations client=%s llm error: %v", req.Client, llmErr)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM generation failed: %v", llmErr))
		return
	}

	effectiveProvider := req.Provider
	if effectiveProvider == "" {
		effectiveProvider = h.config.LLM.SuggestionProvider
		if effectiveProvider == "" {
			effectiveProvider = h.config.LLM.DefaultProvider
		}
	}
	log.Printf("INFO HandleCorrelations client=%s cache=miss provider=%s suggestions=%d", req.Client, effectiveProvider, len(sugs))

	if len(sugs) > 0 && h.alertStore != nil {
		sugsJSON, _ := json.Marshal(sugs)
		if appendErr := h.alertStore.AppendCachedCorrelations(ctx, store.CorrelationRow{
			CacheKey:    cacheKey,
			Client:      req.Client,
			Suggestions: json.RawMessage(sugsJSON),
			Provider:    effectiveProvider,
			GeneratedAt: time.Now().UTC(),
		}); appendErr != nil {
			log.Printf("WARN HandleCorrelations client=%s cache append: %v", req.Client, appendErr)
		}
	}

	// For force requests, re-fetch merged pool; otherwise return LLM result directly.
	if req.Force && h.alertStore != nil {
		allRows, fetchErr := h.alertStore.GetCachedCorrelations(ctx, cacheKey)
		if fetchErr != nil {
			log.Printf("WARN HandleCorrelations client=%s force pool fetch: %v", req.Client, fetchErr)
		} else if len(allRows) > 0 {
			merged, latestProvider := mergeCorrelations(allRows)
			writeJSON(w, http.StatusOK, models.CorrelationsResponse{
				Suggestions: merged,
				Provider:    latestProvider,
				Cached:      false,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, models.CorrelationsResponse{
		Suggestions: sugs,
		Provider:    effectiveProvider,
		Cached:      false,
	})
}

// HandleBuildDetection generates flow alerts for a user-selected MITRE technique chain.
func (h *Handler) HandleBuildDetection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.BuildDetectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Client = strings.TrimSpace(req.Client)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	if len(req.Techniques) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 techniques required")
		return
	}
	if len(req.Techniques) > 20 {
		writeError(w, http.StatusBadRequest, "at most 20 techniques allowed per chain")
		return
	}
	if _, ok := h.config.Clients[req.Client]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	// Resolve provider — same pattern as HandleCorrelations.
	nvidiaKey := h.config.LLM.NvidiaAPIKey
	if h.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
	}
	var provider llm.Provider
	var err error
	if req.Provider != "" {
		provider, err = llm.NewProvider(req.Provider, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	} else {
		providerName := h.config.LLM.SuggestionProvider
		if providerName == "" {
			providerName = h.config.LLM.DefaultProvider
		}
		provider, err = llm.NewClassifierProvider(providerName, h.config.LLM.SuggestionModel, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Convert request techniques to llm.BuildTechnique.
	techs := make([]llm.BuildTechnique, len(req.Techniques))
	for i, t := range req.Techniques {
		techs[i] = llm.BuildTechnique{
			ID:          t.ID,
			Name:        t.Name,
			TacticID:    t.TacticID,
			TacticName:  t.TacticName,
			TacticOrder: t.TacticOrder,
			Source:      t.Source,
		}
	}

	effectiveProvider := req.Provider
	if effectiveProvider == "" {
		effectiveProvider = h.config.LLM.SuggestionProvider
		if effectiveProvider == "" {
			effectiveProvider = h.config.LLM.DefaultProvider
		}
	}

	ctx := r.Context()
	result, err := llm.GenerateDetection(ctx, provider, techs)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("generation failed: %v", err))
		return
	}

	log.Printf("INFO HandleBuildDetection client=%s provider=%s techniques=%d alerts=%d",
		req.Client, effectiveProvider, len(req.Techniques), len(result.Alerts))

	result.Provider = effectiveProvider
	writeJSON(w, http.StatusOK, result)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.ErrorResponse{Error: message})
}

// buildSuggestionLogSources returns non-vendor-managed log source names for a client,
// capped at 30 to keep LLM prompts lean. Fully vendor-managed integrations
// (VendorCoveredCount == AlertCount > 0) and vendor-covered alert data sources are excluded.
func buildSuggestionLogSources(integrations []monday.Integration, alerts []*models.AlertDef) []string {
	enriched := merge.CountAlertsByIntegration(integrations, alerts)

	logSourceSet := make(map[string]bool)
	var logSources []string

	for _, integ := range enriched {
		isVendorManaged := integ.AlertCount > 0 && integ.VendorCoveredCount == integ.AlertCount
		if integ.Name != "" && !isVendorManaged && !logSourceSet[integ.Name] {
			logSourceSet[integ.Name] = true
			logSources = append(logSources, integ.Name)
		}
	}

	for _, alert := range alerts {
		if alert.Features.VendorCovered {
			continue
		}
		for _, ds := range alert.Features.DataSources {
			if !logSourceSet[ds] {
				logSourceSet[ds] = true
				logSources = append(logSources, ds)
			}
		}
	}

	const maxLogSources = 30
	if len(logSources) > maxLogSources {
		logSources = logSources[:maxLogSources]
	}
	return logSources
}

// HandleMapTactics handles POST /api/map-tactics.
// It maps a gap prose description to MITRE ATT&CK tactic and technique IDs via LLM.
// On LLM failure it returns empty arrays rather than an error, so the frontend can
// still navigate to the builder.
func (h *Handler) HandleMapTactics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.MapTacticsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Client = strings.TrimSpace(req.Client)
	req.Prose = strings.TrimSpace(req.Prose)
	if req.Client == "" {
		writeError(w, http.StatusBadRequest, "missing required field: client")
		return
	}
	if req.Prose == "" {
		writeError(w, http.StatusBadRequest, "missing required field: prose")
		return
	}
	if _, ok := h.config.Clients[req.Client]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown client: %s", req.Client))
		return
	}

	nvidiaKey := h.config.LLM.NvidiaAPIKey
	if h.config.LLM.NvidiaSuggestionAPIKey != "" {
		nvidiaKey = h.config.LLM.NvidiaSuggestionAPIKey
	}

	// Resolve provider — same pattern as HandleCorrelations.
	var provider llm.Provider
	var err error
	if req.Provider != "" {
		provider, err = llm.NewProvider(req.Provider, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	} else {
		providerName := h.config.LLM.SuggestionProvider
		if providerName == "" {
			providerName = h.config.LLM.DefaultProvider
		}
		provider, err = llm.NewClassifierProvider(providerName, h.config.LLM.SuggestionModel, llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    nvidiaKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		})
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	effectiveProvider := req.Provider
	if effectiveProvider == "" {
		effectiveProvider = h.config.LLM.SuggestionProvider
	}

	ctx := r.Context()
	result, llmErr := llm.GenerateMapTactics(ctx, provider, llm.MapTacticsInput{
		Prose:     req.Prose,
		LogSource: req.LogSource,
	})
	if llmErr != nil {
		log.Printf("WARN HandleMapTactics client=%s llm error: %v — returning empty", req.Client, llmErr)
		writeJSON(w, http.StatusOK, models.MapTacticsResponse{
			TacticIDs:    []string{},
			TechniqueIDs: []string{},
		})
		return
	}

	log.Printf("INFO HandleMapTactics client=%s provider=%s tactics=%d techniques=%d",
		req.Client, effectiveProvider, len(result.TacticIDs), len(result.TechniqueIDs))
	writeJSON(w, http.StatusOK, models.MapTacticsResponse{
		TacticIDs:    result.TacticIDs,
		TechniqueIDs: result.TechniqueIDs,
	})
}
