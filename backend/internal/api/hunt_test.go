package api

import (
	"testing"
)

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
		string(make([]byte, 1001)), // over length limit
	}
	for _, q := range invalid {
		if err := sanitizeQuery(q); err == nil {
			t.Errorf("sanitizeQuery(%q) = nil, want error", q)
		}
	}
}
