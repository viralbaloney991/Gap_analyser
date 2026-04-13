package prewarm

import (
	"coralogix-alert-analyzer/internal/config"
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
