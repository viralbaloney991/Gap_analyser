package main

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/monday"
)

func TestResolveGroupIDs(t *testing.T) {
	groups := []monday.Group{
		{ID: "group_abc", Title: "JioStar"},
		{ID: "group_def", Title: "Deel"},
		{ID: "group_ghi", Title: "Sinarmas - ASM"},
		{ID: "group_jkl", Title: "Sinarmas - Mining"},
	}

	tests := []struct {
		name       string
		clientName string
		existingID string
		expectedID string
	}{
		{
			name:       "exact match",
			clientName: "JioStar",
			expectedID: "group_abc",
		},
		{
			name:       "case-insensitive match",
			clientName: "jiostar",
			expectedID: "group_abc",
		},
		{
			name:       "client name contained in group title",
			clientName: "Deel",
			expectedID: "group_def",
		},
		{
			name:       "no match - stays empty",
			clientName: "Unknown",
			expectedID: "",
		},
		{
			name:       "already set - not overwritten",
			clientName: "JioStar",
			existingID: "existing_id",
			expectedID: "existing_id",
		},
		{
			name:       "multiple matches - uses first",
			clientName: "Sinarmas",
			expectedID: "group_ghi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Clients: map[string]config.ClientConfig{
					tt.clientName: {
						APIKey:        "key",
						Region:        "eu1",
						MondayGroupID: tt.existingID,
					},
				},
			}
			resolveGroupIDs(cfg, groups)
			got := cfg.Clients[tt.clientName].MondayGroupID
			if got != tt.expectedID {
				t.Errorf("client %q: expected group ID %q, got %q", tt.clientName, tt.expectedID, got)
			}
		})
	}
}
