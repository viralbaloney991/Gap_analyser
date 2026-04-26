package insights

import (
	"fmt"

	"coralogix-alert-analyzer/internal/models"
)

// structuredSignals is the JSON payload sent to Claude Opus for gap analysis.
type structuredSignals struct {
	AlertCount        int                              `json:"alert_count"`
	IntegrationCount  int                              `json:"integration_count"`
	TacticCoverage    map[string]signalsTacticEntry    `json:"tactic_coverage"`
	TechniqueCoverage map[string]signalsTechniqueEntry `json:"technique_coverage"`
	IntegrationGaps   []signalsIntegrationGap          `json:"integration_gaps"`
	NoiseAlerts       []string                         `json:"noise_alerts"`
	DuplicateGroups   int                              `json:"duplicate_groups"`
}

type signalsTacticEntry struct {
	Pct    float64 `json:"pct"`
	Alerts int     `json:"alerts"`
}

type signalsTechniqueEntry struct {
	Name   string `json:"name"`
	Alerts int    `json:"alerts"`
	Weak   bool   `json:"weak,omitempty"`
}

type signalsIntegrationGap struct {
	Name   string `json:"name"`
	Alerts int    `json:"alerts"`
}

// buildStructuredSignals assembles the ~1–2k token JSON payload for Claude Opus.
// All parameters are optional — nil inputs produce empty but valid signals.
func buildStructuredSignals(
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
) structuredSignals {
	sig := structuredSignals{
		TacticCoverage:    make(map[string]signalsTacticEntry),
		TechniqueCoverage: make(map[string]signalsTechniqueEntry),
	}

	sig.AlertCount = len(alerts)
	sig.IntegrationCount = len(integrations)

	if result != nil {
		sig.DuplicateGroups = len(result.Duplicates)

		for _, na := range result.NoiseAlerts {
			label := na.Name
			switch {
			case na.TriggerCount > 0 && na.NoiseType != "":
				label = fmt.Sprintf("%s [%s, %d×]", na.Name, na.NoiseType, na.TriggerCount)
			case na.NoiseType != "":
				label = fmt.Sprintf("%s [%s]", na.Name, na.NoiseType)
			}
			sig.NoiseAlerts = append(sig.NoiseAlerts, label)
		}
	}

	if mitreCoverage != nil {
		for tactic, tc := range mitreCoverage.Summary.TacticBreakdown {
			sig.TacticCoverage[tactic] = signalsTacticEntry{
				Pct:    tc.Percent,
				Alerts: tc.Covered,
			}
		}
		for id, entry := range mitreCoverage.TechniqueCoverage {
			sig.TechniqueCoverage[id] = signalsTechniqueEntry{
				Name:   entry.Name,
				Alerts: entry.AlertCount,
				Weak:   entry.Weak,
			}
		}
	}

	for _, integ := range integrations {
		if integ.AlertCount == 0 {
			sig.IntegrationGaps = append(sig.IntegrationGaps, signalsIntegrationGap{
				Name:   integ.Name,
				Alerts: 0,
			})
		}
	}

	return sig
}
