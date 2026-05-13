package llm

import (
	"context"
	"testing"
)

func TestParseMapTacticsResponse(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantTactics []string
		wantTechs   []string
		wantErr     bool
	}{
		{
			name:        "clean JSON",
			raw:         `{"tactic_ids":["TA0008","TA0001"],"technique_ids":["T1021.001","T1566.001"]}`,
			wantTactics: []string{"TA0008", "TA0001"},
			wantTechs:   []string{"T1021.001", "T1566.001"},
		},
		{
			name:        "json wrapped in markdown",
			raw:         "```json\n{\"tactic_ids\":[\"TA0002\"],\"technique_ids\":[\"T1059.001\"]}\n```",
			wantTactics: []string{"TA0002"},
			wantTechs:   []string{"T1059.001"},
		},
		{
			name:        "empty arrays",
			raw:         `{"tactic_ids":[],"technique_ids":[]}`,
			wantTactics: []string{},
			wantTechs:   []string{},
		},
		{
			name:    "invalid JSON",
			raw:     `not json at all`,
			wantErr: true,
		},
		{
			name:        "hallucinated tactic ID dropped",
			raw:         `{"tactic_ids":["TA9999","TA0008"],"technique_ids":["T1021.001"]}`,
			wantTactics: []string{"TA0008"},
			wantTechs:   []string{"T1021.001"},
		},
		{
			name:        "hallucinated technique ID dropped",
			raw:         `{"tactic_ids":["TA0008"],"technique_ids":["T9999.001","T1021.001"]}`,
			wantTactics: []string{"TA0008"},
			wantTechs:   []string{"T1021.001"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMapTactics(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.TacticIDs) != len(tc.wantTactics) {
				t.Errorf("tactic_ids length: got %d (%v), want %d (%v)", len(got.TacticIDs), got.TacticIDs, len(tc.wantTactics), tc.wantTactics)
			} else {
				for i, id := range got.TacticIDs {
					if id != tc.wantTactics[i] {
						t.Errorf("tactic_ids[%d]: got %q, want %q", i, id, tc.wantTactics[i])
					}
				}
			}
			if len(got.TechniqueIDs) != len(tc.wantTechs) {
				t.Errorf("technique_ids length: got %d (%v), want %d (%v)", len(got.TechniqueIDs), got.TechniqueIDs, len(tc.wantTechs), tc.wantTechs)
			} else {
				for i, id := range got.TechniqueIDs {
					if id != tc.wantTechs[i] {
						t.Errorf("technique_ids[%d]: got %q, want %q", i, id, tc.wantTechs[i])
					}
				}
			}
		})
	}
}

func TestGenerateMapTacticsCallsProvider(t *testing.T) {
	// mockProvider is defined in mitre_mapper_test.go (same package)
	p := &mockProvider{response: `{"tactic_ids":["TA0008"],"technique_ids":["T1021.001"]}`}
	result, err := GenerateMapTactics(context.Background(), p, MapTacticsInput{
		Prose:     "RDP lateral movement with no alert coverage",
		LogSource: "windows_security",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.TacticIDs) != 1 || result.TacticIDs[0] != "TA0008" {
		t.Errorf("unexpected tactic_ids: %v", result.TacticIDs)
	}
}
