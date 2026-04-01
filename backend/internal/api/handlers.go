package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/cache"
	"coralogix-alert-analyzer/internal/classifier"
	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/llm"
	"coralogix-alert-analyzer/internal/merge"
	"coralogix-alert-analyzer/internal/mitre"
	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/similarity"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	config       *config.Config
	mondayClient *monday.Client
	cache        *cache.Store
}

// NewHandler creates a new Handler with the given config and cache.
func NewHandler(cfg *config.Config, store *cache.Store) *Handler {
	return &Handler{
		config:       cfg,
		mondayClient: monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID),
		cache:        store,
	}
}

// HandleClients returns the list of configured client names.
func (h *Handler) HandleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, h.config.ClientNames())
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

	// Build MITRE mappings via classifier sidecar + Llama validator.
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
		}
		validatorProvider, err := llm.NewClassifierProvider(
			h.config.LLM.ValidatorProvider,
			h.config.LLM.ValidatorModel,
			baseCfg,
		)
		if err != nil {
			log.Printf("WARN [analyze] validator provider unavailable: %v", err)
		} else {
			classifierClient := classifier.NewClient(h.config.Classifier.Endpoint)
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
				llmMappings = llm.BatchClassifyAndValidate(ctx, classifierClient, validatorProvider, h.cache, inputs)
			}
		}
	}

	// Extract features.
	coralogix.ExtractFeatures(alerts, llmMappings)

	// Match integrations to alerts.
	matched := merge.CountAlertsByIntegration(integrations, alerts)

	// Run MITRE coverage.
	mitreCoverage := mitre.AnalyzeCoverage(alerts)

	// Run similarity analysis.
	alertInsights := similarity.Analyze(alerts)

	// Build integration info for response.
	integrationInfos := make([]models.IntegrationInfo, len(matched))
	withAlerts := 0
	for i, m := range matched {
		integrationInfos[i] = models.IntegrationInfo{
			Name:        m.Name,
			Application: m.Application,
			Subsystem:   m.Subsystem,
			AlertCount:  m.AlertCount,
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
		MITRECoverage: mitreCoverage,
		AlertInsights: alertInsights,
		Cached:        false,
	}

	// Store in cache.
	if h.cache != nil {
		h.cache.Set(ctx, req.Client, resp)
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleHealth returns a simple health check response.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

	// Determine LLM provider
	providerName := req.Provider
	if providerName == "" {
		if h.config.LLM.SuggestionProvider != "" {
			providerName = h.config.LLM.SuggestionProvider
		} else {
			providerName = h.config.LLM.DefaultProvider
		}
	}
	suggestionModel := h.config.LLM.SuggestionModel
	baseCfg := llm.ProviderConfig{
		AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
		ClaudeModel:     h.config.LLM.ClaudeModel,
		NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
		NvidiaModel:     h.config.LLM.NvidiaModel,
		NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
	}
	provider, err := llm.NewClassifierProvider(providerName, suggestionModel, baseCfg)
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

	// Extract features to get data sources (no LLM mapping needed for suggestions endpoint)
	coralogix.ExtractFeatures(alerts, nil)

	// Collect available log sources: Monday integrations first (primary),
	// then unique alert data sources. Cap at 30 to keep LLM prompt lean.
	logSourceSet := make(map[string]bool)
	var logSources []string

	// Monday integrations are the primary source of truth for onboarded log sources
	for _, integ := range integrations {
		if integ.Name != "" && !logSourceSet[integ.Name] {
			logSourceSet[integ.Name] = true
			logSources = append(logSources, integ.Name)
		}
	}
	// Supplement with alert data sources (may add sources Monday doesn't track)
	for _, alert := range alerts {
		for _, ds := range alert.Features.DataSources {
			if !logSourceSet[ds] {
				logSourceSet[ds] = true
				logSources = append(logSources, ds)
			}
		}
	}
	// Cap to keep prompt small for LLM
	const maxLogSources = 30
	if len(logSources) > maxLogSources {
		logSources = logSources[:maxLogSources]
	}

	// Resolve technique name from master list
	techniqueName := mitre.GetTechniqueName(req.TechniqueID)
	tactic := req.Tactic
	if tactic == "" {
		tactic = mitre.GetTechniqueTactic(req.TechniqueID)
	}

	log.Printf("INFO [suggestions] client=%s technique=%s (%s) log_sources=%d provider=%s",
		req.Client, req.TechniqueID, techniqueName, len(logSources), providerName)

	gapInput := llm.GapInput{
		LogSources: logSources,
		Technique: llm.TechniqueInput{
			ID:     req.TechniqueID,
			Name:   techniqueName,
			Tactic: tactic,
		},
	}

	result, err := llm.GenerateSuggestions(ctx, provider, gapInput)
	if err != nil {
		log.Printf("ERROR [suggestions] LLM call failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM suggestion failed: %v", err))
		return
	}

	// Map to response model
	suggestions := make([]models.AlertSuggestion, len(result.Suggestions))
	for i, s := range result.Suggestions {
		suggestions[i] = models.AlertSuggestion{
			LogSource:   s.LogSource,
			AlertName:   s.AlertName,
			Description: s.Description,
			QueryHint:   s.QueryHint,
			Priority:    s.Priority,
		}
	}

	resp := models.SuggestionsResponse{
		Provider:      result.Provider,
		TechniqueID:   req.TechniqueID,
		TechniqueName: techniqueName,
		Suggestions:   suggestions,
		LogSources:    logSources,
	}

	writeJSON(w, http.StatusOK, resp)
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
