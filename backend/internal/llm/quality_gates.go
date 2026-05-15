package llm

import (
	"log"

	"coralogix-alert-analyzer/internal/models"
)

// validateDetectionAlert checks a BuildDetectionAlert for the three quality gates:
// non-empty sigma_rule, non-empty logic (Lucene), and at least one falsepositives entry.
func validateDetectionAlert(a models.BuildDetectionAlert) []string {
	var errs []string
	if a.SigmaRule == "" {
		errs = append(errs, "sigma_rule is empty")
	}
	if a.Logic == "" {
		errs = append(errs, "lucene_query (logic) is empty")
	}
	if len(a.Falsepositives) == 0 {
		errs = append(errs, "falsepositives is empty")
	}
	return errs
}

// validateSuggestion checks a Suggestion for the three quality gates:
// non-empty title, sigma_rule, lucene_query, and at least one falsepositives entry.
func validateSuggestion(s Suggestion) []string {
	var errs []string
	if s.Title == "" {
		errs = append(errs, "title is empty")
	}
	if s.SigmaRule == "" {
		errs = append(errs, "sigma_rule is empty")
	}
	if s.LuceneQuery == "" {
		errs = append(errs, "lucene_query is empty")
	}
	if len(s.Falsepositives) == 0 {
		errs = append(errs, "falsepositives is empty")
	}
	return errs
}

// filterValidSuggestions removes any suggestions that fail quality gates and logs a warning for each.
func filterValidSuggestions(suggestions []Suggestion) []Suggestion {
	out := make([]Suggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if errs := validateSuggestion(s); len(errs) > 0 {
			log.Printf("WARN [suggestions] quality gate failed for %q: %v", s.Title, errs)
			continue
		}
		out = append(out, s)
	}
	return out
}
