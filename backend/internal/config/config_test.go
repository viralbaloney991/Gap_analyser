package config_test

import (
	"testing"

	"coralogix-alert-analyzer/internal/config"
)

func TestClientsWithRegion_normalisesRegionToLowercase(t *testing.T) {
	cfg := &config.Config{
		Clients: map[string]config.ClientConfig{
			"Alpha": {APIKey: "k1", Region: "EU1"},
			"Beta":  {APIKey: "k2", Region: "US1"},
		},
	}
	got := cfg.ClientsWithRegion()
	for _, c := range got {
		for _, ch := range c.Region {
			if ch >= 'A' && ch <= 'Z' {
				t.Errorf("client %s: region %q contains uppercase character", c.Name, c.Region)
			}
		}
	}
	if got[0].Region != "eu1" {
		t.Errorf("want eu1, got %s", got[0].Region)
	}
	if got[1].Region != "us1" {
		t.Errorf("want us1, got %s", got[1].Region)
	}
}

func TestClientsWithRegion_sortedByName(t *testing.T) {
	cfg := &config.Config{
		Clients: map[string]config.ClientConfig{
			"Zebra": {APIKey: "k1", Region: "eu1"},
			"Apple": {APIKey: "k2", Region: "us1"},
			"Mango": {APIKey: "k3", Region: "ap2"},
		},
	}
	got := cfg.ClientsWithRegion()
	if len(got) != 3 {
		t.Fatalf("want 3 clients, got %d", len(got))
	}
	if got[0].Name != "Apple" || got[0].Region != "us1" {
		t.Errorf("index 0: want Apple/us1, got %s/%s", got[0].Name, got[0].Region)
	}
	if got[1].Name != "Mango" || got[1].Region != "ap2" {
		t.Errorf("index 1: want Mango/ap2, got %s/%s", got[1].Name, got[1].Region)
	}
	if got[2].Name != "Zebra" || got[2].Region != "eu1" {
		t.Errorf("index 2: want Zebra/eu1, got %s/%s", got[2].Name, got[2].Region)
	}
}
