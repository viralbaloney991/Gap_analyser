package llm

import (
	"encoding/json"
	"testing"

	"coralogix-alert-analyzer/internal/models"
)

func TestSuggestionBackwardCompat(t *testing.T) {
	// Old cached JSON format uses alert_name, query_hint, priority.
	// After struct update, these must still unmarshal into the legacy fields.
	oldJSON := `[{"log_source":"Windows","alert_name":"Old Name","description":"desc","query_hint":"x:y","priority":"high"}]`
	var sugs []Suggestion
	if err := json.Unmarshal([]byte(oldJSON), &sugs); err != nil {
		t.Fatalf("unmarshal old format: %v", err)
	}
	if len(sugs) != 1 {
		t.Fatalf("expected 1, got %d", len(sugs))
	}
	if sugs[0].AlertName != "Old Name" {
		t.Errorf("AlertName compat: got %q", sugs[0].AlertName)
	}
	if sugs[0].QueryHint != "x:y" {
		t.Errorf("QueryHint compat: got %q", sugs[0].QueryHint)
	}
}

func TestValidateDetectionAlert(t *testing.T) {
	cases := []struct {
		name    string
		alert   models.BuildDetectionAlert
		wantErr bool
	}{
		{
			name: "valid alert",
			alert: models.BuildDetectionAlert{
				Name:           "Detect Credential Dump via LSASS",
				SigmaRule:      "title: test\nlogsource:\n  product: windows",
				Logic:          "process.name:lsass.exe",
				Falsepositives: []string{"Security scanners"},
			},
			wantErr: false,
		},
		{
			name: "missing sigma_rule",
			alert: models.BuildDetectionAlert{
				Name:           "Detect Credential Dump via LSASS",
				Logic:          "process.name:lsass.exe",
				Falsepositives: []string{"Security scanners"},
			},
			wantErr: true,
		},
		{
			name: "missing logic",
			alert: models.BuildDetectionAlert{
				Name:           "Detect Credential Dump via LSASS",
				SigmaRule:      "title: test",
				Falsepositives: []string{"Security scanners"},
			},
			wantErr: true,
		},
		{
			name: "empty falsepositives",
			alert: models.BuildDetectionAlert{
				Name:      "Detect Credential Dump via LSASS",
				SigmaRule: "title: test",
				Logic:     "process.name:lsass.exe",
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateDetectionAlert(tc.alert)
			if tc.wantErr && len(errs) == 0 {
				t.Error("expected validation errors, got none")
			}
			if !tc.wantErr && len(errs) > 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
		})
	}
}

func TestMockBuildDetectionHasSigmaAndFP(t *testing.T) {
	techs := []BuildTechnique{
		{ID: "T1003", Name: "OS Credential Dumping", TacticID: "TA0006", TacticName: "Credential Access", TacticOrder: 7, Source: "EDR"},
	}
	result := mockBuildDetection(techs)
	if len(result.Alerts) == 0 {
		t.Fatal("expected at least one alert")
	}
	a := result.Alerts[0]
	if a.SigmaRule == "" {
		t.Error("mock alert SigmaRule should not be empty")
	}
	if len(a.Falsepositives) == 0 {
		t.Error("mock alert Falsepositives should not be empty")
	}
}

func TestValidateSuggestion(t *testing.T) {
	cases := []struct {
		name    string
		sug     Suggestion
		wantErr bool
	}{
		{
			name: "valid",
			sug: Suggestion{
				Title:          "Detect Lateral Movement via Pass-the-Hash",
				SigmaRule:      "title: test",
				LuceneQuery:    "event.action:pth",
				Falsepositives: []string{"Admin tools"},
			},
			wantErr: false,
		},
		{
			name:    "empty title",
			sug:     Suggestion{SigmaRule: "title: test", LuceneQuery: "x:y", Falsepositives: []string{"None"}},
			wantErr: true,
		},
		{
			name:    "empty sigma_rule",
			sug:     Suggestion{Title: "Detect X via Y", LuceneQuery: "x:y", Falsepositives: []string{"None"}},
			wantErr: true,
		},
		{
			name:    "empty lucene_query",
			sug:     Suggestion{Title: "Detect X via Y", SigmaRule: "title: x", Falsepositives: []string{"None"}},
			wantErr: true,
		},
		{
			name:    "empty falsepositives",
			sug:     Suggestion{Title: "Detect X via Y", SigmaRule: "title: x", LuceneQuery: "x:y"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateSuggestion(tc.sug)
			if tc.wantErr && len(errs) == 0 {
				t.Error("expected errors, got none")
			}
			if !tc.wantErr && len(errs) > 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
		})
	}
}
