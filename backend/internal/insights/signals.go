package insights

import (
	"fmt"
	"sort"

	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/models"
)

const maxImmediateCandidates = 30

// structuredSignals is the JSON payload sent to Claude Opus for gap analysis.
type structuredSignals struct {
	AlertCount               int                              `json:"alert_count"`
	IntegrationCount         int                              `json:"integration_count"`
	TacticCoverage           map[string]signalsTacticEntry    `json:"tactic_coverage"`
	TechniqueCoverage        map[string]signalsTechniqueEntry `json:"technique_coverage"`
	IntegrationGaps          []signalsIntegrationGap          `json:"integration_gaps"`
	NoiseAlerts              []string                         `json:"noise_alerts"`
	DuplicateGroups          int                              `json:"duplicate_groups"`
	ImmediateNoiseCandidates []signalsImmediateCandidate      `json:"immediate_noise_candidates"`
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

type signalsImmediateCandidate struct {
	Name         string `json:"name"`
	Query        string `json:"query"`
	TriggerCount int    `json:"trigger_count,omitempty"`
}

// buildStructuredSignals assembles the ~1–2k token JSON payload for Claude Opus.
// All parameters are optional — nil inputs produce empty but valid signals.
// eventCounts may be nil; candidates will have TriggerCount 0 (omitted from JSON).
func buildStructuredSignals(
	result *models.SimilarityResult,
	alerts []*models.AlertDef,
	integrations []models.IntegrationInfo,
	mitreCoverage *models.MITRECoverageResult,
	eventCounts map[string]int,
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

	// Pre-filter: unscoped logs_immediate security alerts with no entity filter.
	var candidates []signalsImmediateCandidate
	for _, alert := range alerts {
		if alert.AlertType != "logs_immediate" {
			continue
		}
		if !alert.Features.IsSecurityAlert || alert.Features.IsBuildingBlock || alert.Features.VendorCovered {
			continue
		}
		app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
		if app != "" || sub != "" {
			continue
		}
		if len(alert.Features.Entities) > 0 {
			continue
		}
		candidates = append(candidates, signalsImmediateCandidate{
			Name:         alert.Name,
			Query:        coralogix.ExtractLuceneQuery(alert.TypeDef),
			TriggerCount: eventCounts[alert.ID],
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	if len(candidates) > maxImmediateCandidates {
		candidates = candidates[:maxImmediateCandidates]
	}
	sig.ImmediateNoiseCandidates = candidates

	return sig
}
