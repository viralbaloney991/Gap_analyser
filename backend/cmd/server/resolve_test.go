package main

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/monday"
)

func TestResolveGroupIDs(t *testing.T) {
	groups := []monday.Group{
		{ID: "group_abc", Title: "Acme Corp"},
		{ID: "group_def", Title: "Globex"},
		{ID: "group_ghi", Title: "Initech - Division A"},
		{ID: "group_jkl", Title: "Initech - Division B"},
	}

	tests := []struct {
		name       string
		clientName string
		existingID string
		expectedID string
	}{
		{
			name:       "exact match",
			clientName: "Acme Corp",
			expectedID: "group_abc",
		},
		{
			name:       "case-insensitive match",
			clientName: "acme corp",
			expectedID: "group_abc",
		},
		{
			name:       "client name contained in group title",
			clientName: "Globex",
			expectedID: "group_def",
		},
		{
			name:       "no match - stays empty",
			clientName: "Unknown",
			expectedID: "",
		},
		{
			name:       "already set - not overwritten",
			clientName: "Acme Corp",
			existingID: "existing_id",
			expectedID: "existing_id",
		},
		{
			name:       "multiple matches - uses first",
			clientName: "Initech",
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
