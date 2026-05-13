package api

import (
	"context"
	"strings"
	"testing"
)

func TestBuildOllyPrompt(t *testing.T) {
	prompt, err := buildOllyPrompt(`event_type:"cmd_exec"`, 47, "host1 svc-deploy 2024-01-01T10:00:00 cmd -enc abc\nhost2 admin 2024-01-01T10:05:00 cmd -enc xyz")
	if err != nil {
		t.Fatalf("buildOllyPrompt: %v", err)
	}
	if !strings.Contains(prompt, `event_type:"cmd_exec"`) {
		t.Error("prompt missing lucene query")
	}
	if !strings.Contains(prompt, "47") {
		t.Error("prompt missing hit count")
	}
	if !strings.Contains(prompt, "svc-deploy") {
		t.Error("prompt missing sample events")
	}
}

func TestParseOllySections(t *testing.T) {
	raw := `## §1 Hunt Summary
Severity: High
Confidence: High

## §2 Original Query
event_type:"cmd_exec"

## §3 Schema Mapping
| field | cx_path | app | gaps |
|-------|---------|-----|------|
| event_type | log.type | auth | none |
`
	sections := parseOllySections(raw)
	if sections["1"] == "" {
		t.Error("section 1 missing")
	}
	if !strings.Contains(sections["1"], "Severity: High") {
		t.Errorf("section 1 content wrong: %q", sections["1"])
	}
	if sections["2"] == "" {
		t.Error("section 2 missing")
	}
	if sections["3"] == "" {
		t.Error("section 3 missing")
	}
}

func TestSanitizeQuery(t *testing.T) {
	valid := []string{
		`event_type:"cmd_exec" AND cmd:"-EncodedCommand"`,
		`source.ip:10.0.0.1 AND user:admin`,
		`kubernetes.pod:frontend-*`,
	}
	for _, q := range valid {
		if err := sanitizeQuery(q); err != nil {
			t.Errorf("sanitizeQuery(%q) = %v, want nil", q, err)
		}
	}

	invalid := []string{
		`event_type:cmd $(whoami)`,
		"event_type:cmd `id`",
		`event_type:cmd; rm -rf /`,
		`event_type:cmd | cat /etc/passwd`,
		`event_type:cmd` + "\ninjected",
		string(make([]byte, 1001)),          // over length limit (null bytes — caught by allowlist)
		strings.Repeat(" ", 1001),           // 1001 printable ASCII chars — tests length guard exclusively
		`event_type:cmd\injected`,           // backslash
		`event_type:cmd & id`,               // background exec
		`event_type:cmd > /tmp/out`,         // output redirect
	}
	for _, q := range invalid {
		if err := sanitizeQuery(q); err == nil {
			t.Errorf("sanitizeQuery(%q) = nil, want error", q)
		}
	}
}

func TestDeriveVerdict(t *testing.T) {
	tests := []struct {
		hits        int
		section1    string
		wantVerdict string
		wantConf    string
	}{
		{0, "Severity: High\nConfidence: High", "clean", "high"},
		{5, "Severity: Low\nConfidence: Low", "suspicious", "low"},
		{5, "Severity: Medium\nConfidence: Medium", "suspicious", "medium"},
		{10, "Severity: High\nConfidence: High", "threat", "high"},
		{10, "Severity: Critical\nConfidence: Medium", "threat", "medium"},
		{3, "Severity: High\nConfidence: Low", "suspicious", "low"},
	}
	for _, tc := range tests {
		v, c := deriveVerdict(tc.hits, tc.section1)
		if v != tc.wantVerdict {
			t.Errorf("hits=%d sect=%q: verdict=%q want %q", tc.hits, tc.section1, v, tc.wantVerdict)
		}
		if c != tc.wantConf {
			t.Errorf("hits=%d sect=%q: conf=%q want %q", tc.hits, tc.section1, c, tc.wantConf)
		}
	}
}

type mockCxExecutor struct {
	logsOutput  []byte
	logsErr     error
	ollyOutput  []byte
	ollyErr     error
}

func (m *mockCxExecutor) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	return m.logsOutput, m.logsErr
}

func (m *mockCxExecutor) runOllyChat(ctx context.Context, prompt string) ([]byte, error) {
	return m.ollyOutput, m.ollyErr
}

func TestMockCxExecutorInterface(t *testing.T) {
	// Verifies mockCxExecutor satisfies the cxExecutor interface at compile time.
	var _ cxExecutor = &mockCxExecutor{}
}
