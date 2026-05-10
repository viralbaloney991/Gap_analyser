package merge

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
)

func TestCountAlertsByIntegration_vendorCoveredCount(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "AWS CloudTrail", Application: "cloudtrail"},
	}
	alerts := []*models.AlertDef{
		{Name: "cloudtrail event", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: false}},
		{Name: "cloudtrail alert", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: true}},
	}
	result := CountAlertsByIntegration(integrations, alerts)
	if result[0].AlertCount != 2 {
		t.Errorf("want AlertCount=2, got %d", result[0].AlertCount)
	}
	if result[0].VendorCoveredCount != 1 {
		t.Errorf("want VendorCoveredCount=1, got %d", result[0].VendorCoveredCount)
	}
}

func TestCountAlertsByIntegration_allVendorCovered(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "Okta", Application: "okta"},
	}
	alerts := []*models.AlertDef{
		{Name: "okta event 1", Features: models.AlertFeatures{DataSources: []string{"okta"}, VendorCovered: true}},
		{Name: "okta event 2", Features: models.AlertFeatures{DataSources: []string{"okta"}, VendorCovered: true}},
	}
	result := CountAlertsByIntegration(integrations, alerts)
	if result[0].AlertCount != 2 {
		t.Errorf("want AlertCount=2, got %d", result[0].AlertCount)
	}
	if result[0].VendorCoveredCount != 2 {
		t.Errorf("want VendorCoveredCount=2, got %d", result[0].VendorCoveredCount)
	}
}

func TestCountAlertsByIntegration_noAlerts(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "Splunk", Application: "splunk"},
	}
	alerts := []*models.AlertDef{
		{Name: "cloudtrail alert", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: true}},
	}
	result := CountAlertsByIntegration(integrations, alerts)
	if result[0].AlertCount != 0 {
		t.Errorf("want AlertCount=0, got %d", result[0].AlertCount)
	}
	if result[0].VendorCoveredCount != 0 {
		t.Errorf("want VendorCoveredCount=0, got %d", result[0].VendorCoveredCount)
	}
}

func TestCountAlertsByIntegration_PriorityCounts(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "okta", Application: "okta", Subsystem: ""},
	}
	alerts := []*models.AlertDef{
		{Name: "Okta - P1 Alert", Priority: "ALERT_DEF_PRIORITY_P1", Features: models.AlertFeatures{DataSources: []string{"okta"}}},
		{Name: "Okta - P2 Alert", Priority: "ALERT_DEF_PRIORITY_P2", Features: models.AlertFeatures{DataSources: []string{"okta"}}},
		{Name: "Okta - P2 Alert 2", Priority: "ALERT_DEF_PRIORITY_P2", Features: models.AlertFeatures{DataSources: []string{"okta"}}},
		{Name: "Unrelated Alert", Priority: "ALERT_DEF_PRIORITY_P1", Features: models.AlertFeatures{DataSources: []string{"aws"}}},
	}

	result := CountAlertsByIntegration(integrations, alerts)

	if len(result) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result))
	}
	pc := result[0].PriorityCounts
	if pc == nil {
		t.Fatal("PriorityCounts is nil")
	}
	if pc["ALERT_DEF_PRIORITY_P1"] != 1 {
		t.Errorf("expected P1=1, got %d", pc["ALERT_DEF_PRIORITY_P1"])
	}
	if pc["ALERT_DEF_PRIORITY_P2"] != 2 {
		t.Errorf("expected P2=2, got %d", pc["ALERT_DEF_PRIORITY_P2"])
	}
}

func TestCountAlertsByIntegration_EmptyPriorityFallback(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "okta", Application: "okta", Subsystem: ""},
	}
	alerts := []*models.AlertDef{
		{Name: "Okta - Unspecified", Priority: "", Features: models.AlertFeatures{DataSources: []string{"okta"}}},
	}

	result := CountAlertsByIntegration(integrations, alerts)
	pc := result[0].PriorityCounts
	if pc["ALERT_DEF_PRIORITY_P5_OR_UNSPECIFIED"] != 1 {
		t.Errorf("expected P5_OR_UNSPECIFIED=1, got %d", pc["ALERT_DEF_PRIORITY_P5_OR_UNSPECIFIED"])
	}
}
