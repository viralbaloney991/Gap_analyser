// backend/internal/llm/detection_builder_test.go
package llm

import (
	"strings"
	"testing"
)

func TestParseDetectionResult_valid(t *testing.T) {
	raw := `{
  "validation": {"verdict": "ok", "findings": [{"level": "info", "message": "Chain valid"}]},
  "alerts": [
    {"name":"Stage 1","description":"desc","techniqueId":"T1078","logic":"login seen","window":"15m","windowReason":"fast","source":"IdP","severity":"high"},
    {"name":"Stage 2","description":"desc2","techniqueId":"T1098","logic":"key created","window":"1h","windowReason":"slow","source":"CloudTrail","severity":"critical"}
  ],
  "correlation": {"name":"chain","logic":"Alert 1 -> Alert 2 within 24h","window":"24h","severity":"critical"}
}`

	got, err := parseDetectionResult(raw)
	if err != nil {
		t.Fatalf("parseDetectionResult failed: %v", err)
	}
	if got.Validation.Verdict != "ok" {
		t.Errorf("expected verdict=ok, got %q", got.Validation.Verdict)
	}
	if len(got.Alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(got.Alerts))
	}
	if got.Alerts[0].TechniqueID != "T1078" {
		t.Errorf("expected techniqueId=T1078, got %q", got.Alerts[0].TechniqueID)
	}
	if got.Correlation.Window != "24h" {
		t.Errorf("expected correlation.window=24h, got %q", got.Correlation.Window)
	}
}

func TestParseDetectionResult_markdownFenced(t *testing.T) {
	raw := "```json\n{\"validation\":{\"verdict\":\"ok\",\"findings\":[]},\"alerts\":[{\"name\":\"S1\",\"description\":\"d\",\"techniqueId\":\"T1566\",\"logic\":\"l\",\"window\":\"15m\",\"windowReason\":\"r\",\"source\":\"Email\",\"severity\":\"high\"}],\"correlation\":{\"name\":\"c\",\"logic\":\"l\",\"window\":\"1h\",\"severity\":\"high\"}}\n```"

	got, err := parseDetectionResult(raw)
	if err != nil {
		t.Fatalf("parseDetectionResult with fences failed: %v", err)
	}
	if got.Alerts[0].TechniqueID != "T1566" {
		t.Errorf("unexpected techniqueId: %q", got.Alerts[0].TechniqueID)
	}
}

func TestBuildDetectionPrompt_containsTechniqueInfo(t *testing.T) {
	techs := []BuildTechnique{
		{ID: "T1078", Name: "Valid Accounts", TacticName: "Initial Access", Source: "IdP / Cloud", TacticOrder: 1},
		{ID: "T1098", Name: "Account Manipulation", TacticName: "Persistence", Source: "CloudTrail", TacticOrder: 3},
	}
	prompt := buildDetectionPrompt(techs)
	for _, want := range []string{"T1078", "Valid Accounts", "Initial Access", "IdP / Cloud", "T1098", "Persistence"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestMockBuildDetection_twoTechs(t *testing.T) {
	techs := []BuildTechnique{
		{ID: "T1078", Name: "Valid Accounts", TacticName: "Initial Access", TacticID: "TA0001", TacticOrder: 1, Source: "IdP"},
		{ID: "T1098", Name: "Account Manipulation", TacticName: "Persistence", TacticID: "TA0003", TacticOrder: 3, Source: "Cloud"},
	}
	result := mockBuildDetection(techs)
	if result == nil {
		t.Fatal("mockBuildDetection returned nil")
	}
	if len(result.Alerts) != 2 {
		t.Errorf("expected 2 alerts, got %d", len(result.Alerts))
	}
	if result.Correlation.Window == "" {
		t.Error("correlation window must not be empty")
	}
}

func TestMockBuildDetection_warningForMissingInitialAccess(t *testing.T) {
	techs := []BuildTechnique{
		{ID: "T1059", TacticID: "TA0002", TacticName: "Execution", TacticOrder: 2, Name: "Cmd", Source: "EDR"},
		{ID: "T1486", TacticID: "TA0040", TacticName: "Impact", TacticOrder: 12, Name: "Ransomware", Source: "EDR"},
	}
	result := mockBuildDetection(techs)
	hasWarn := false
	for _, f := range result.Validation.Findings {
		if f.Level == "warn" && strings.Contains(f.Message, "Initial Access") {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected warning for missing Initial Access technique")
	}
}
