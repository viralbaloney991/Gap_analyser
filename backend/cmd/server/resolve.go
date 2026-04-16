package main

import (
	"log"
	"strings"

	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/monday"
)

// resolveGroupIDs populates MondayGroupID for clients that don't have one,
// by fuzzy-matching (case-insensitive contains) the client name against
// Monday board group titles. Clients with an explicit group ID are skipped.
func resolveGroupIDs(cfg *config.Config, groups []monday.Group) {
	for name, client := range cfg.Clients {
		if client.MondayGroupID != "" {
			continue
		}
		var matches []monday.Group
		lName := strings.ToLower(name)
		for _, g := range groups {
			lTitle := strings.ToLower(g.Title)
			if strings.Contains(lTitle, lName) || (len(lTitle) >= 3 && strings.Contains(lName, lTitle)) {
				matches = append(matches, g)
			}
		}
		switch len(matches) {
		case 0:
			log.Printf("WARN [monday] no group match for client %q — Monday data will be skipped", name)
		case 1:
			client.MondayGroupID = matches[0].ID
			cfg.Clients[name] = client
			log.Printf("INFO [monday] resolved group for client %q → %s (%s)", name, matches[0].ID, matches[0].Title)
		default:
			client.MondayGroupID = matches[0].ID
			cfg.Clients[name] = client
			log.Printf("WARN [monday] multiple group matches for client %q, using first: %s (%s)", name, matches[0].ID, matches[0].Title)
		}
	}
}
