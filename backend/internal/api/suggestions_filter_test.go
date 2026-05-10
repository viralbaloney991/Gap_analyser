package api

import (
	"fmt"
	"testing"

	"coralogix-alert-analyzer/internal/models"
	"coralogix-alert-analyzer/internal/monday"
)

func TestBuildSuggestionLogSources_ExcludesVendorManagedIntegrations(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "CrowdStrike", Application: "crowdstrike", Subsystem: ""},
		{Name: "Okta",        Application: "okta",        Subsystem: ""},
	}
	alerts := []*models.AlertDef{
		{Name: "CS - Malware", Features: models.AlertFeatures{DataSources: []string{"crowdstrike"}, VendorCovered: true}},
		{Name: "CS - Lateral", Features: models.AlertFeatures{DataSources: []string{"crowdstrike"}, VendorCovered: true}},
		{Name: "Okta - MFA Fatigue", Features: models.AlertFeatures{DataSources: []string{"okta"}, VendorCovered: false}},
	}

	sources := buildSuggestionLogSources(integrations, alerts)

	for _, s := range sources {
		if s == "CrowdStrike" {
			t.Errorf("CrowdStrike is fully vendor-managed and must not appear in log sources")
		}
	}
	found := false
	for _, s := range sources {
		if s == "Okta" {
			found = true
		}
	}
	if !found {
		t.Errorf("Okta is customer-managed and must appear in log sources; got %v", sources)
	}
}

func TestBuildSuggestionLogSources_IncludesPartiallyVendorManaged(t *testing.T) {
	integrations := []monday.Integration{
		{Name: "AWS CloudTrail", Application: "cloudtrail", Subsystem: ""},
	}
	alerts := []*models.AlertDef{
		{Name: "CT - Vendor", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: true}},
		{Name: "CT - Custom", Features: models.AlertFeatures{DataSources: []string{"cloudtrail"}, VendorCovered: false}},
	}

	sources := buildSuggestionLogSources(integrations, alerts)

	found := false
	for _, s := range sources {
		if s == "AWS CloudTrail" {
			found = true
		}
	}
	if !found {
		t.Errorf("AWS CloudTrail has customer-managed alerts and must appear in log sources; got %v", sources)
	}
}

func TestBuildSuggestionLogSources_CapAt30(t *testing.T) {
	integrations := make([]monday.Integration, 40)
	for i := range integrations {
		integrations[i] = monday.Integration{Name: fmt.Sprintf("Source%d", i), Application: fmt.Sprintf("src%d", i)}
	}
	sources := buildSuggestionLogSources(integrations, nil)
	if len(sources) > 30 {
		t.Errorf("expected at most 30 log sources, got %d", len(sources))
	}
}

func TestBuildSuggestionLogSources_ExcludesVendorCoveredAlertDataSources(t *testing.T) {
	integrations := []monday.Integration{}
	alerts := []*models.AlertDef{
		{Name: "Vendor DS", Features: models.AlertFeatures{DataSources: []string{"sentinelone"}, VendorCovered: true}},
		{Name: "Custom DS", Features: models.AlertFeatures{DataSources: []string{"linux_auditd"}, VendorCovered: false}},
	}

	sources := buildSuggestionLogSources(integrations, alerts)

	for _, s := range sources {
		if s == "sentinelone" {
			t.Errorf("sentinelone is vendor-covered and must not appear in supplemental sources")
		}
	}
}
