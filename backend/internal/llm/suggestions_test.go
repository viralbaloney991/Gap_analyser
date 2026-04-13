package llm

import (
	"testing"
)

func TestSanitizeJSONStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no control chars",
			input: `{"key":"value"}`,
			want:  `{"key":"value"}`,
		},
		{
			name:  "literal newline inside string",
			input: "{\"query\":\"line1\nline2\"}",
			want:  `{"query":"line1\nline2"}`,
		},
		{
			name:  "literal tab inside string",
			input: "{\"q\":\"a\tb\"}",
			want:  `{"q":"a\tb"}`,
		},
		{
			name:  "structural newlines preserved",
			input: "[\n  {\"a\":\"b\"}\n]",
			want:  "[\n  {\"a\":\"b\"}\n]",
		},
		{
			name:  "escaped chars not double-escaped",
			input: `{"q":"already\\nescaped"}`,
			want:  `{"q":"already\\nescaped"}`,
		},
		{
			name:  "minimax query_hint multiline",
			input: "{\"query_hint\":\"eventName:(PutObject) AND\nrequestParameters.key:*\"}",
			want:  `{"query_hint":"eventName:(PutObject) AND\nrequestParameters.key:*"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeJSONStrings(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeJSONStrings(%q)\n got  %q\n want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseSuggestions_LiteralNewlineInString(t *testing.T) {
	// Reproduces the minimax-m2.7 failure: query_hint contains a literal newline.
	raw := "```json\n[\n  {\n    \"log_source\": \"CloudTrail\",\n    \"alert_name\": \"Large S3 Upload\",\n    \"description\": \"Detects exfil\",\n    \"query_hint\": \"eventName:(PutObject OR CompleteMultipartUpload) AND\nrequestParameters.x-amz-copy-source:*\"\n  }\n]\n```"

	suggestions, err := parseSuggestions(raw)
	if err != nil {
		t.Fatalf("parseSuggestions failed: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	if suggestions[0].LogSource != "CloudTrail" {
		t.Errorf("unexpected log_source: %s", suggestions[0].LogSource)
	}
}
