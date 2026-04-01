package coralogix

import (
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestContainsWord(t *testing.T) {
	tests := []struct {
		s, word string
		want    bool
	}{
		// "low" must not match inside other words
		{"flow alert", "low", false},
		{"allow access", "low", false},
		{"below threshold", "low", false},
		{"slow response", "low", false},
		// "low" should match as a standalone word
		{"vendor - low severity", "low", true},
		{"low severity detection", "low", true},
		{"severity: low", "low", true},
		{"detection - low", "low", true},
		// "high" must not match inside other words
		{"highlight anomaly", "high", false},
		{"highly suspicious", "high", false},
		// "high" should match as a standalone word
		{"vendor - high severity", "high", true},
		{"high severity finding", "high", true},
		{"detection - high", "high", true},
		// "medium" edge cases
		{"medium severity", "medium", true},
		// Multi-word terms
		{"vendor cdr alert critical", "cdr alert", true},
		{"vendor - lead summary", "lead summary", true},
		// Longer terms always fine
		{"severity pass-through", "severity", true},
		{"detection event", "detection", true},
	}

	for _, tt := range tests {
		got := containsWord(tt.s, tt.word)
		if got != tt.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tt.s, tt.word, got, tt.want)
		}
	}
}

func TestIsVendorCovered(t *testing.T) {
	tests := []struct {
		name       string
		alert      *models.AlertDef
		wantVendor string
		wantCover  bool
	}{
		// ── Should be vendor-covered ──────────────────────────────
		{
			name: "generic severity pass-through with alert_provider",
			alert: &models.AlertDef{
				Name:   "CrowdStrike - Detection - Critical Severity",
				Labels: map[string]string{"alert_provider": "CrowdStrike"},
			},
			wantVendor: "CrowdStrike",
			wantCover:  true,
		},
		{
			name: "generic finding with extension pack",
			alert: &models.AlertDef{
				Name:   "WIZ - CDR Alert Critical",
				Labels: map[string]string{"alert_extension_pack": "WIZ Security"},
			},
			wantVendor: "WIZ Security",
			wantCover:  true,
		},
		{
			name: "unknown vendor still detected via label",
			alert: &models.AlertDef{
				Name:   "Trend Micro - Detection - High Severity",
				Labels: map[string]string{"alert_provider": "Trend Micro"},
			},
			wantVendor: "Trend Micro",
			wantCover:  true,
		},
		{
			name: "vendor with severity word only",
			alert: &models.AlertDef{
				Name:   "Cybereason - Incident - Medium",
				Labels: map[string]string{"alert_provider": "Cybereason"},
			},
			wantVendor: "Cybereason",
			wantCover:  true,
		},
		{
			name: "DoControl lead summary",
			alert: &models.AlertDef{
				Name:   "DoControl - Lead Summary",
				Labels: map[string]string{"alert_provider": "DoControl"},
			},
			wantVendor: "DoControl",
			wantCover:  true,
		},
		{
			name: "vendor-covered with MITRE labels still tagged",
			alert: &models.AlertDef{
				Name: "SentinelOne - Detection - Critical Severity",
				Labels: map[string]string{
					"alert_provider":  "SentinelOne",
					"mitre_technique": "T1059",
				},
			},
			wantVendor: "SentinelOne",
			wantCover:  true,
		},

		// ── Should NOT be vendor-covered ──────────────────────────
		{
			name: "behavioral alert - brute force",
			alert: &models.AlertDef{
				Name:   "CrowdStrike - Brute Force Attack - Critical",
				Labels: map[string]string{"alert_provider": "CrowdStrike"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral alert - login failure",
			alert: &models.AlertDef{
				Name:   "Vendor - Failed Login Detection - High",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral alert - malware",
			alert: &models.AlertDef{
				Name:   "Defender - Malware Detected - Critical",
				Labels: map[string]string{"alert_provider": "Microsoft Defender"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral alert - suspicious anomaly",
			alert: &models.AlertDef{
				Name:   "Vendor - Suspicious Activity Detected",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral alert - credential access",
			alert: &models.AlertDef{
				Name:   "CrowdStrike - Credential Theft - High Severity",
				Labels: map[string]string{"alert_provider": "CrowdStrike"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral alert - publicly exposed",
			alert: &models.AlertDef{
				Name:   "WIZ - Publicly Exposed Database - Critical",
				Labels: map[string]string{"alert_provider": "WIZ"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "no provenance label - not vendor covered",
			alert: &models.AlertDef{
				Name:   "Generic Detection - High Severity",
				Labels: map[string]string{"alert_type": "security"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "no labels at all",
			alert: &models.AlertDef{
				Name:   "Some Alert - Detection - Critical",
				Labels: nil,
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "building block excluded",
			alert: &models.AlertDef{
				Name:   "Building Block - WIZ Finding - High",
				Labels: map[string]string{"alert_provider": "WIZ"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "correlation alert excluded",
			alert: &models.AlertDef{
				Name:   "Correlation Alert - CrowdStrike Detection",
				Labels: map[string]string{"alert_provider": "CrowdStrike"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "no generic term in name",
			alert: &models.AlertDef{
				Name:   "CrowdStrike - Endpoint Status",
				Labels: map[string]string{"alert_provider": "CrowdStrike"},
			},
			wantVendor: "",
			wantCover:  false,
		},

		// ── Word boundary edge cases (issue #3) ──────────────────
		{
			name: "flow in name should not match 'low'",
			alert: &models.AlertDef{
				Name:   "Vendor - Data Flow Detection",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			// "flow" contains "low" but word boundary prevents match.
			// "detection" IS a generic term, so this checks behavioral:
			// no behavioral words → vendor-covered
			wantVendor: "SomeVendor",
			wantCover:  true,
		},
		{
			name: "allow in name should not match 'low'",
			alert: &models.AlertDef{
				Name:   "Vendor - Allow List Update",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			// No generic term ("allow" doesn't match "low") → not vendor-covered
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "highlight in name should not match 'high'",
			alert: &models.AlertDef{
				Name:   "Vendor - Highlight Summary",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			// "highlight" doesn't match "high" via word boundary.
			// No other generic terms → not vendor-covered
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral - exploit in name prevents vendor-covered",
			alert: &models.AlertDef{
				Name:   "Vendor - Exploit Detected - High",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			wantVendor: "",
			wantCover:  false,
		},
		{
			name: "behavioral - policy change prevents vendor-covered",
			alert: &models.AlertDef{
				Name:   "Vendor - Policy Change Detected - Medium",
				Labels: map[string]string{"alert_provider": "SomeVendor"},
			},
			wantVendor: "",
			wantCover:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vendor, covered := isVendorCovered(tt.alert)
			if covered != tt.wantCover {
				t.Errorf("isVendorCovered() covered = %v, want %v", covered, tt.wantCover)
			}
			if vendor != tt.wantVendor {
				t.Errorf("isVendorCovered() vendor = %q, want %q", vendor, tt.wantVendor)
			}
		})
	}
}

func TestExtractVendorCoveredFeatures_WithMITRELabels(t *testing.T) {
	alert := &models.AlertDef{
		Name: "SentinelOne - Detection - Critical Severity",
		Labels: map[string]string{
			"alert_provider":  "SentinelOne",
			"mitre_technique": "T1059",
			"mitre_tactic":    "execution",
		},
	}

	features := extractVendorCoveredFeatures(alert, nil)

	if len(features.Techniques) == 0 {
		t.Error("expected vendor MITRE labels to be preserved, got empty techniques")
	}
	found := false
	for _, tech := range features.Techniques {
		if tech == "T1059" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected T1059 in techniques, got %v", features.Techniques)
	}

	if len(features.Tactics) == 0 {
		t.Error("expected tactics to be derived, got empty")
	}
}

func TestExtractVendorCoveredFeatures_WithoutMITRELabels(t *testing.T) {
	alert := &models.AlertDef{
		Name:   "CrowdStrike - Detection - Critical Severity",
		Labels: map[string]string{"alert_provider": "CrowdStrike"},
	}

	features := extractVendorCoveredFeatures(alert, nil)

	if len(features.Techniques) != 0 {
		t.Errorf("expected empty techniques for vendor-covered without MITRE labels, got %v", features.Techniques)
	}
	if len(features.Tactics) != 0 {
		t.Errorf("expected empty tactics, got %v", features.Tactics)
	}
}
