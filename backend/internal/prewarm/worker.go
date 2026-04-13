package prewarm

import (
	"context"
	"log"
	"sync"

	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/config"
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
			if err == nil && len(stored) > 0 {
				alerts = stored
				return
			}
		}
		// No live fetch here — prewarm uses cached data only to avoid duplicate API calls.
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
