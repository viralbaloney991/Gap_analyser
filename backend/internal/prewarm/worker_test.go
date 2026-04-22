package prewarm

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

func TestBuildCacheKey_Deterministic(t *testing.T) {
	// Same inputs in different order must produce same key.
	sources1 := []string{"Okta", "CloudTrail", "GitHub"}
	sources2 := []string{"GitHub", "Okta", "CloudTrail"}

	key1 := buildCacheKey("T1078", sources1)
	key2 := buildCacheKey("T1078", sources2)

	if key1 != key2 {
		t.Errorf("expected same key for same inputs regardless of order, got %s vs %s", key1, key2)
	}
}

func TestBuildCacheKey_DifferentTechnique(t *testing.T) {
	sources := []string{"Okta", "CloudTrail"}

	key1 := buildCacheKey("T1078", sources)
	key2 := buildCacheKey("T1059", sources)

	if key1 == key2 {
		t.Errorf("expected different keys for different technique IDs")
	}
}

func TestBuildCacheKey_MatchesHandlerLogic(t *testing.T) {
	// Verify our key matches the algorithm used in handlers.go buildSuggestionCacheKey.
	techniqueID := "T1078"
	logSources := []string{"GitHub", "Okta", "CloudTrail"}

	sorted := make([]string, len(logSources))
	copy(sorted, logSources)
	sort.Strings(sorted)
	raw := techniqueID + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	expected := hex.EncodeToString(sum[:])

	got := buildCacheKey(techniqueID, logSources)
	if got != expected {
		t.Errorf("buildCacheKey = %s, want %s", got, expected)
	}
}
