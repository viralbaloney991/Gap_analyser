package similarity

import (
	"testing"
)

func TestNormalizeGroupByKeys_knownPaths(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"okta.actor.alternateId", "user"},
		{"userIdentity.arn", "user"},
		{"actor.email", "user"},
		{"USER_NAME", "user"},
		{"ClientIP", "ip"},
		{"cx_security.source_ip", "ip"},
		{"okta.client.ipAddress", "ip"},
		{"event.Hostname", "hostname"},
		{"event.ComputerName", "hostname"},
		{"requestParameters.bucketName", "resource"},
		{"requestParameters.instanceId", "resource"},
		{"instance-id", "resource"},
		{"userIdentity.accountId", "account"},
		{"awsRegion", "account"},
		{"event.DetectId", "detection"},
		{"sourceRule.name", "detection"},
	}
	for _, tc := range cases {
		got := normalizeGroupByKeys([]string{tc.input})
		if _, ok := got[tc.expected]; !ok {
			t.Errorf("normalizeGroupByKeys(%q): expected category %q, got %v", tc.input, tc.expected, got)
		}
		if len(got) != 1 {
			t.Errorf("normalizeGroupByKeys(%q): expected 1 category, got %d: %v", tc.input, len(got), got)
		}
	}
}

func TestNormalizeGroupByKeys_unknownPath(t *testing.T) {
	got := normalizeGroupByKeys([]string{"some.unknown.field"})
	if _, ok := got["some.unknown.field"]; !ok {
		t.Errorf("expected raw path as category for unknown key, got %v", got)
	}
}

func TestNormalizeGroupByKeys_empty(t *testing.T) {
	if got := normalizeGroupByKeys(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := normalizeGroupByKeys([]string{}); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestNormalizeGroupByKeys_mixed(t *testing.T) {
	got := normalizeGroupByKeys([]string{"okta.actor.alternateId", "some.custom.field"})
	if _, ok := got["user"]; !ok {
		t.Errorf("expected 'user' category, got %v", got)
	}
	if _, ok := got["some.custom.field"]; !ok {
		t.Errorf("expected raw path 'some.custom.field', got %v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 categories, got %d: %v", len(got), got)
	}
}

func TestNormalizeGroupByKeys_deduplicatesCategories(t *testing.T) {
	// two different "user" paths → one "user" category
	got := normalizeGroupByKeys([]string{"okta.actor.alternateId", "actor.email"})
	if len(got) != 1 {
		t.Errorf("expected 1 deduplicated category, got %d: %v", len(got), got)
	}
	if _, ok := got["user"]; !ok {
		t.Errorf("expected 'user' category, got %v", got)
	}
}

func TestJaccardGroupBy_bothEmpty(t *testing.T) {
	if score := jaccardGroupBy(nil, nil); score != 1.0 {
		t.Errorf("expected 1.0 for both empty (compatible), got %f", score)
	}
}

func TestJaccardGroupBy_oneEmpty(t *testing.T) {
	a := map[string]struct{}{"user": {}}
	if score := jaccardGroupBy(a, nil); score != 0.0 {
		t.Errorf("expected 0.0 when one side empty, got %f", score)
	}
	if score := jaccardGroupBy(nil, a); score != 0.0 {
		t.Errorf("expected 0.0 when one side empty, got %f", score)
	}
}

func TestJaccardGroupBy_sameCategory(t *testing.T) {
	a := map[string]struct{}{"user": {}}
	b := map[string]struct{}{"user": {}}
	if score := jaccardGroupBy(a, b); score != 1.0 {
		t.Errorf("expected 1.0 for same category, got %f", score)
	}
}

func TestJaccardGroupBy_differentCategory(t *testing.T) {
	a := map[string]struct{}{"user": {}}
	b := map[string]struct{}{"ip": {}}
	if score := jaccardGroupBy(a, b); score != 0.0 {
		t.Errorf("expected 0.0 for different categories, got %f", score)
	}
}
