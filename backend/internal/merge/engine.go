package merge

import (
	"strings"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
)

// CountAlertsByIntegration matches alerts to integrations based on
// application, subsystem, and integration name, and populates AlertCount.
func CountAlertsByIntegration(integrations []monday.Integration, alerts []*models.AlertDef) []monday.Integration {
	result := make([]monday.Integration, len(integrations))
	copy(result, integrations)

	for i := range result {
		apps := splitAndNormalize(result[i].Application)
		subs := splitAndNormalize(result[i].Subsystem)
		integName := normalize(result[i].Name)

		count := 0
		vendorCount := 0
		priorityCounts := make(map[string]int)
		for _, alert := range alerts {
			if alertMatchesIntegration(alert, apps, subs, integName) {
				count++
				if alert.Features.VendorCovered {
					vendorCount++
				}
				key := alert.Priority
				if key == "" {
					key = "ALERT_DEF_PRIORITY_P5_OR_UNSPECIFIED"
				}
				priorityCounts[key]++
			}
		}
		result[i].AlertCount = count
		result[i].VendorCoveredCount = vendorCount
		result[i].PriorityCounts = priorityCounts
	}

	return result
}

func alertMatchesIntegration(alert *models.AlertDef, apps, subs []string, integName string) bool {
	// Check data sources against app names
	for _, ds := range alert.Features.DataSources {
		dsNorm := normalize(ds)
		for _, app := range apps {
			appNorm := normalize(app)
			if appNorm != "" && fuzzyMatch(dsNorm, appNorm) {
				return true
			}
		}
	}

	// Check alert name and description against app/sub names
	corpus := normalize(alert.Name + " " + alert.Description)
	for _, app := range apps {
		appNorm := normalize(app)
		if appNorm != "" && strings.Contains(corpus, appNorm) {
			return true
		}
	}
	for _, sub := range subs {
		subNorm := normalize(sub)
		if subNorm != "" && strings.Contains(corpus, subNorm) {
			return true
		}
	}

	// Fallback: match integration name against data sources and alert name.
	// Handles cases where Monday app/sub fields are empty (e.g., Slack)
	// or where the integration name appears in the alert (e.g., CyberINT).
	if integName != "" {
		// Check integration name against data sources
		for _, ds := range alert.Features.DataSources {
			dsNorm := normalize(ds)
			if fuzzyMatch(dsNorm, integName) {
				return true
			}
		}

		// Check integration name (and its first word as vendor shorthand)
		// against alert name. E.g., "Slack - ..." matches "slack",
		// "Deel - CyberInt alert" matches "cyberint cti" via first word "cyberint".
		alertNameNorm := normalize(alert.Name)
		nameTokens := integNameTokens(integName)
		for _, token := range nameTokens {
			if strings.HasPrefix(alertNameNorm, token+" ") ||
				strings.HasPrefix(alertNameNorm, token+":") ||
				strings.Contains(alertNameNorm, " "+token+" ") ||
				alertNameNorm == token {
				return true
			}
		}
	}

	return false
}

// integNameTokens returns the full integration name plus its first word
// (the vendor name) for matching. E.g., "cyberint cti" → ["cyberint cti", "cyberint"].
// Single-word names like "slack" return just that word.
func integNameTokens(name string) []string {
	tokens := []string{name}
	if first, _, ok := strings.Cut(name, " "); ok && len(first) >= 3 {
		tokens = append(tokens, first)
	}
	return tokens
}

// fuzzyMatch checks if two strings match or one contains the other.
func fuzzyMatch(a, b string) bool {
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

// normalize lowercases and replaces hyphens/underscores with spaces for fuzzy matching.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return s
}

// splitAndNormalize splits comma-separated values and normalizes them.
func splitAndNormalize(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
