# Claude-Native MITRE Classifier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the two-stage Python sidecar + LLM validator pipeline with a single Claude call that classifies alerts directly against the full MITRE ATT&CK technique list (~530 entries, compact JSON).

**Architecture:** `BuildTechniqueJSON()` in `internal/mitre/mitre.go` generates a tactic-grouped compact JSON of all parent + sub-techniques once via `sync.Once`. A new `classifier_prompt.go` in `internal/llm` embeds this JSON in a system prompt and parses Claude's JSON-array response with ID validation. `BatchClassify` in `mitre_mapper.go` replaces `BatchClassifyAndValidate`, removing the sidecar client entirely. Redis caching and pipeline semaphore are unchanged.

**Tech Stack:** Go 1.21, `encoding/json`, `sync`, existing `pipeline.Run`, existing Redis cache interface.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/internal/mitre/mitre.go` | Add | `BuildTechniqueJSON()`, `ValidTechniqueID()`, `buildTechniqueData()` |
| `backend/internal/llm/classifier_prompt.go` | Create | System prompt builder, message builder, response parser |
| `backend/internal/llm/validator.go` | **Delete** | Old validate-candidates logic (imports classifier package) |
| `backend/internal/llm/mitre_mapper.go` | Rewrite | `BatchClassify(provider, store, sem, inputs)` — single LLM call per alert |
| `backend/internal/api/handlers.go` | Update | Remove `classifier.NewClient`, wire `llm.BatchClassify` |
| `backend/internal/config/config.go` | Update | Remove `ClassifierConfig`, `Classifier` field, `ValidatorProvider/Model`, endpoint/validator validations |
| `backend/clients.yaml` | Update | Remove `classifier:` section and `validator_*` fields |
| `backend/internal/classifier/` | **Delete** | Entire Go package |
| `classifier/` (project root) | **Delete** | Entire Python sidecar directory |

---

## Task 1: Add BuildTechniqueJSON and ValidTechniqueID to mitre.go

**Files:**
- Modify: `backend/internal/mitre/mitre.go`
- Create: `backend/internal/mitre/technique_json_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/mitre/technique_json_test.go`:

```go
package mitre

import (
	"encoding/json"
	"testing"
)

func TestBuildTechniqueJSON_IsValidJSON(t *testing.T) {
	result := BuildTechniqueJSON()
	if result == "" {
		t.Fatal("BuildTechniqueJSON returned empty string")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("BuildTechniqueJSON returned invalid JSON: %v", err)
	}
}

func TestBuildTechniqueJSON_ContainsAllTactics(t *testing.T) {
	result := BuildTechniqueJSON()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	expected := []string{
		"reconnaissance", "resource-development", "initial-access",
		"execution", "persistence", "privilege-escalation",
		"defense-evasion", "credential-access", "discovery",
		"lateral-movement", "collection", "command-and-control",
		"exfiltration", "impact",
	}
	for _, tactic := range expected {
		if _, ok := parsed[tactic]; !ok {
			t.Errorf("missing tactic: %s", tactic)
		}
	}
}

func TestBuildTechniqueJSON_SubTechniqueSuffixFormat(t *testing.T) {
	result := BuildTechniqueJSON()
	type techEntry struct {
		N string            `json:"n"`
		S map[string]string `json:"s"`
	}
	var parsed map[string]map[string]techEntry
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec, ok := parsed["execution"]
	if !ok {
		t.Fatal("missing execution tactic")
	}
	t1059, ok := exec["T1059"]
	if !ok {
		t.Fatal("missing T1059 in execution")
	}
	if t1059.S["001"] != "PowerShell" {
		t.Errorf("expected T1059 suffix 001 = PowerShell, got %q", t1059.S["001"])
	}
}

func TestValidTechniqueID_KnownParent(t *testing.T) {
	if !ValidTechniqueID("T1059") {
		t.Error("T1059 should be valid")
	}
}

func TestValidTechniqueID_KnownSubTechnique(t *testing.T) {
	if !ValidTechniqueID("T1059.001") {
		t.Error("T1059.001 should be valid")
	}
}

func TestValidTechniqueID_UnknownParent(t *testing.T) {
	if ValidTechniqueID("T9999") {
		t.Error("T9999 should not be valid")
	}
}

func TestValidTechniqueID_UnknownSubTechnique(t *testing.T) {
	if ValidTechniqueID("T1059.999") {
		t.Error("T1059.999 should not be valid")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/mitre/... -run "TestBuildTechniqueJSON|TestValidTechniqueID" -v
```
Expected: `FAIL` — `BuildTechniqueJSON` and `ValidTechniqueID` undefined.

- [ ] **Step 3: Add imports and new types to mitre.go**

At the top of `backend/internal/mitre/mitre.go`, update the import block to add `"encoding/json"` and `"sync"`:

```go
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/models"
)
```

- [ ] **Step 4: Add BuildTechniqueJSON, ValidTechniqueID, and helpers at the bottom of mitre.go**

Append after the `GetAllTechniques()` function (at the very end of `backend/internal/mitre/mitre.go`):

```go
// techniqueCompact is the compact JSON representation of a technique in the classifier prompt.
type techniqueCompact struct {
	N string            `json:"n"`           // technique name
	S map[string]string `json:"s,omitempty"` // suffix → sub-technique name (e.g. "001" → "PowerShell")
}

var (
	techniqueJSONOnce  sync.Once
	techniqueJSONCache string
	validTechIDsCache  map[string]bool
)

// BuildTechniqueJSON returns compact tactic-grouped JSON of all MITRE techniques (parents + sub-techniques).
// Computed once at first call and cached. Used to build the LLM classifier system prompt.
func BuildTechniqueJSON() string {
	techniqueJSONOnce.Do(func() {
		techniqueJSONCache, validTechIDsCache = buildTechniqueData()
	})
	return techniqueJSONCache
}

// ValidTechniqueID reports whether id is a known parent or sub-technique ID in the master list.
func ValidTechniqueID(id string) bool {
	BuildTechniqueJSON() // ensure initialized
	return validTechIDsCache[id]
}

func buildTechniqueData() (string, map[string]bool) {
	tacticMap := make(map[string]map[string]techniqueCompact)
	validIDs := make(map[string]bool)

	for _, t := range masterTechniqueList {
		validIDs[t.ID] = true
		if _, ok := tacticMap[t.Tactic]; !ok {
			tacticMap[t.Tactic] = make(map[string]techniqueCompact)
		}
		if _, exists := tacticMap[t.Tactic][t.ID]; exists {
			continue // already added for this tactic (multi-tactic duplicate)
		}
		entry := techniqueCompact{N: t.Name}
		if len(t.SubTechniques) > 0 {
			entry.S = make(map[string]string, len(t.SubTechniques))
			for _, subID := range t.SubTechniques {
				validIDs[subID] = true
				// sub-technique IDs are "T1059.001" — suffix is the part after the dot
				parts := strings.SplitN(subID, ".", 2)
				if len(parts) == 2 {
					suffix := parts[1]
					name := subTechniqueNames[subID]
					if name == "" {
						name = subID // fallback to full ID if name lookup fails
					}
					entry.S[suffix] = name
				}
			}
		}
		tacticMap[t.Tactic][t.ID] = entry
	}

	data, err := json.Marshal(tacticMap)
	if err != nil {
		return "{}", validIDs
	}
	return string(data), validIDs
}
```

- [ ] **Step 5: Run tests — expect pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/mitre/... -run "TestBuildTechniqueJSON|TestValidTechniqueID" -v
```
Expected:
```
--- PASS: TestBuildTechniqueJSON_IsValidJSON
--- PASS: TestBuildTechniqueJSON_ContainsAllTactics
--- PASS: TestBuildTechniqueJSON_SubTechniqueSuffixFormat
--- PASS: TestValidTechniqueID_KnownParent
--- PASS: TestValidTechniqueID_KnownSubTechnique
--- PASS: TestValidTechniqueID_UnknownParent
--- PASS: TestValidTechniqueID_UnknownSubTechnique
PASS
```

- [ ] **Step 6: Verify full build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/mitre/mitre.go internal/mitre/technique_json_test.go
git commit -m "feat(mitre): add BuildTechniqueJSON and ValidTechniqueID for Claude classifier prompt"
```

---

## Task 2: Create classifier_prompt.go and delete validator.go

**Files:**
- Create: `backend/internal/llm/classifier_prompt.go`
- Create: `backend/internal/llm/classifier_prompt_test.go`
- Delete: `backend/internal/llm/validator.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/llm/classifier_prompt_test.go`:

```go
package llm

import (
	"testing"
)

func TestParseClassifierResponse_ValidArray(t *testing.T) {
	result := parseClassifierResponse(`["T1059.001", "T1078"]`)
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d: %v", len(result), result)
	}
}

func TestParseClassifierResponse_EmptyArray(t *testing.T) {
	result := parseClassifierResponse(`[]`)
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestParseClassifierResponse_HallucinatedIDDropped(t *testing.T) {
	result := parseClassifierResponse(`["T1059.001", "T9999.999"]`)
	if len(result) != 1 || result[0] != "T1059.001" {
		t.Errorf("expected only T1059.001, got %v", result)
	}
}

func TestParseClassifierResponse_MalformedJSON(t *testing.T) {
	result := parseClassifierResponse(`not json`)
	if result != nil {
		t.Errorf("expected nil for malformed JSON, got %v", result)
	}
}

func TestParseClassifierResponse_MarkdownWrapped(t *testing.T) {
	result := parseClassifierResponse("```json\n[\"T1059.001\"]\n```")
	if len(result) != 1 || result[0] != "T1059.001" {
		t.Errorf("expected [T1059.001], got %v", result)
	}
}

func TestBuildClassifierMessage_FullAlert(t *testing.T) {
	inp := AlertInput{
		ID:        "a1",
		Name:      "Azure - Audit - Suspicious Login",
		Query:     "action:login AND result:failure",
		App:       "azure",
		Subsystem: "audit",
	}
	msg := buildClassifierMessage(inp)
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	// Must contain alert name and query
	for _, want := range []string{"Azure - Audit - Suspicious Login", "azure", "audit", "action:login"} {
		if !contains(msg, want) {
			t.Errorf("message missing %q\nGot: %s", want, msg)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/llm/... -run "TestParseClassifierResponse|TestBuildClassifierMessage" -v
```
Expected: `FAIL` — `parseClassifierResponse` and `buildClassifierMessage` undefined.

- [ ] **Step 3: Create backend/internal/llm/classifier_prompt.go**

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/mitre"
)

var (
	classifierSysPromptOnce  sync.Once
	classifierSysPromptCache string
)

// buildClassifierSystemPrompt returns the system prompt for Claude MITRE classification.
// Embeds the full compact technique JSON (generated once via sync.Once in mitre package).
func buildClassifierSystemPrompt() string {
	classifierSysPromptOnce.Do(func() {
		classifierSysPromptCache = `You are a MITRE ATT&CK expert. Given a security alert, identify which techniques from the JSON below the alert actively detects.

Rules:
- Return ONLY a JSON array of technique IDs, e.g. ["T1078.004", "T1110"]
- Only use IDs that appear in the JSON — no others
- Sub-techniques use suffix keys: suffix "001" under parent "T1059" means technique ID "T1059.001"
- Prefer sub-techniques over parent techniques when the alert is specific enough
- Return at most 5 technique IDs
- If the alert does not clearly detect any listed technique, return []
- No markdown, no explanation, no other text

MITRE ATT&CK techniques grouped by tactic (compact JSON):
` + mitre.BuildTechniqueJSON()
	})
	return classifierSysPromptCache
}

// buildClassifierMessage constructs the user message for a single alert classification request.
func buildClassifierMessage(inp AlertInput) string {
	var sb strings.Builder
	sb.WriteString("Alert: ")
	sb.WriteString(inp.Name)
	if inp.App != "" {
		sb.WriteString(" | App: ")
		sb.WriteString(inp.App)
	}
	if inp.Subsystem != "" {
		sb.WriteString(" | Subsystem: ")
		sb.WriteString(inp.Subsystem)
	}
	if inp.Query != "" {
		sb.WriteString("\nQuery: ")
		sb.WriteString(inp.Query)
	}
	return sb.String()
}

// parseClassifierResponse parses Claude's JSON-array response into validated technique IDs.
// Strips markdown fences, unmarshals JSON array, drops any ID not in the master technique list.
func parseClassifierResponse(raw string) []string {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var ids []string
	if err := json.Unmarshal([]byte(cleaned), &ids); err != nil {
		log.Printf("WARN [classifier] parse response: %v (raw: %.100s)", err, raw)
		return nil
	}

	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if mitre.ValidTechniqueID(id) {
			valid = append(valid, id)
		} else {
			log.Printf("DEBUG [classifier] dropped unknown technique ID: %q", id)
		}
	}
	return valid
}

// classifySingle calls Claude to classify a single alert and returns validated technique IDs.
func classifySingle(ctx context.Context, provider Provider, inp AlertInput) []string {
	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: buildClassifierSystemPrompt(),
		UserMessage:  buildClassifierMessage(inp),
		MaxTokens:    256,
		FastMode:     true,
	})
	if err != nil {
		log.Printf("WARN [classifier] alert=%s: %v", inp.ID, err)
		return nil
	}
	result := parseClassifierResponse(resp)
	if result == nil {
		return nil
	}
	return result
}

// ensure classifySingle is referenced (avoids unused import if mitre_mapper uses it differently)
var _ = fmt.Sprintf
```

**Note:** Remove the `var _ = fmt.Sprintf` line if `fmt` is not used — only add `"fmt"` to imports if needed. The import list may need trimming after integration. The `classifySingle` function is called from `mitre_mapper.go` (Task 3).

Actually, remove `"fmt"` and `var _ = fmt.Sprintf` entirely — they are not needed:

The final imports for `classifier_prompt.go` should be:
```go
import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/mitre"
)
```

And remove `var _ = fmt.Sprintf`.

- [ ] **Step 4: Delete validator.go**

```bash
rm /Users/aviral.baloni/Desktop/claude/backend/internal/llm/validator.go
```

- [ ] **Step 5: Run tests — expect pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/llm/... -run "TestParseClassifierResponse|TestBuildClassifierMessage" -v
```
Expected: all 6 tests pass. The build may fail at this point because `mitre_mapper.go` still references `ValidateCandidates` and imports `classifier` — that is fixed in Task 3.

- [ ] **Step 6: Verify the llm package tests pass in isolation**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/llm/... -run "TestParseClassifierResponse|TestBuildClassifierMessage" -v 2>&1 | head -30
```

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/llm/classifier_prompt.go internal/llm/classifier_prompt_test.go
git rm internal/llm/validator.go
git commit -m "feat(llm): add classifier_prompt with Claude-native MITRE classification, remove validator"
```

---

## Task 3: Rewrite mitre_mapper.go — BatchClassify

**Files:**
- Modify: `backend/internal/llm/mitre_mapper.go`
- Create: `backend/internal/llm/mitre_mapper_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/llm/mitre_mapper_test.go`:

```go
package llm

import (
	"context"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/pipeline"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	response string
	err      error
	calls    int
}

func (m *mockProvider) Complete(_ context.Context, _ CompletionRequest) (string, error) {
	m.calls++
	return m.response, m.err
}

func (m *mockProvider) Name() string { return "mock" }

// mockStore implements MITRECacheStore for testing.
type mockStore struct {
	data map[string]string
}

func (s *mockStore) GetString(_ context.Context, key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

func (s *mockStore) SetString(_ context.Context, key, value string, _ time.Duration) {
	s.data[key] = value
}

func TestBatchClassify_EmptyInputs(t *testing.T) {
	provider := &mockProvider{response: `[]`}
	store := &mockStore{data: make(map[string]string)}
	sem := pipeline.NewSemaphore(5)
	result := BatchClassify(context.Background(), provider, store, sem, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil inputs, got %v", result)
	}
	if provider.calls != 0 {
		t.Errorf("expected no LLM calls for empty inputs, got %d", provider.calls)
	}
}

func TestBatchClassify_CacheHitSkipsLLM(t *testing.T) {
	inp := AlertInput{ID: "a1", Name: "Test Alert", Query: "foo", App: "myapp", Subsystem: "sub"}
	key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
	store := &mockStore{data: map[string]string{
		key: `["T1059.001"]`,
	}}
	provider := &mockProvider{response: `["T1078"]`}
	sem := pipeline.NewSemaphore(5)
	result := BatchClassify(context.Background(), provider, store, sem, []AlertInput{inp})
	if provider.calls != 0 {
		t.Errorf("expected no LLM calls on cache hit, got %d", provider.calls)
	}
	if len(result["a1"]) != 1 || result["a1"][0] != "T1059.001" {
		t.Errorf("expected cached [T1059.001], got %v", result["a1"])
	}
}

func TestBatchClassify_LLMCallOnCacheMiss(t *testing.T) {
	inp := AlertInput{ID: "a1", Name: "Test Alert", Query: "foo", App: "myapp", Subsystem: "sub"}
	store := &mockStore{data: make(map[string]string)}
	provider := &mockProvider{response: `["T1059.001"]`}
	sem := pipeline.NewSemaphore(5)
	result := BatchClassify(context.Background(), provider, store, sem, []AlertInput{inp})
	if provider.calls != 1 {
		t.Errorf("expected 1 LLM call on cache miss, got %d", provider.calls)
	}
	if len(result["a1"]) != 1 || result["a1"][0] != "T1059.001" {
		t.Errorf("expected [T1059.001], got %v", result["a1"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/llm/... -run "TestBatchClassify" -v 2>&1 | head -20
```
Expected: compile errors because `BatchClassify` is not defined yet.

- [ ] **Step 3: Replace mitre_mapper.go entirely**

Overwrite `backend/internal/llm/mitre_mapper.go` with:

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/pipeline"
)

const (
	mitreCacheTTL    = 7 * 24 * time.Hour
	mitreCachePrefix = "mitre_v3:"
)

// MITRECacheStore is the subset of the cache.Store needed for per-alert caching.
type MITRECacheStore interface {
	GetString(ctx context.Context, key string) (string, bool)
	SetString(ctx context.Context, key, value string, ttl time.Duration)
}

// AlertInput is the minimal per-alert data needed for LLM MITRE classification.
type AlertInput struct {
	ID        string
	Name      string
	Query     string // Lucene or DataPrime query extracted from TypeDef
	App       string // applicationName filter from logsFilter
	Subsystem string // subsystemName filter from logsFilter
}

func alertHash(name, query, app, subsystem string) string {
	h := sha256Sum(name + "\x00" + query + "\x00" + app + "\x00" + subsystem)
	return fmt.Sprintf("%x", h[:8])
}

// BatchClassify classifies each alert against the full MITRE technique list using a single
// Claude call per alert. Results are cached per-alert in Redis for 7 days.
// Alerts that miss the cache get one LLM call; cache hits skip LLM entirely.
func BatchClassify(
	ctx context.Context,
	provider Provider,
	store MITRECacheStore,
	sem *pipeline.Semaphore,
	inputs []AlertInput,
) map[string][]string {
	result := make(map[string][]string, len(inputs))
	var mu sync.Mutex

	var uncached []AlertInput
	for _, inp := range inputs {
		key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
		if val, ok := store.GetString(ctx, key); ok {
			var techs []string
			if err := json.Unmarshal([]byte(val), &techs); err == nil {
				result[inp.ID] = techs
				continue
			}
		}
		uncached = append(uncached, inp)
	}

	log.Printf("INFO [classifier] total=%d cached=%d to_classify=%d", len(inputs), len(inputs)-len(uncached), len(uncached))

	if len(uncached) == 0 {
		return result
	}

	pipeline.Run(ctx, sem, uncached, 1, func(ctx context.Context, inp AlertInput) error {
		aCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		techs := classifySingle(aCtx, provider, inp)
		if techs == nil {
			techs = []string{}
		}
		key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
		if data, err := json.Marshal(techs); err == nil {
			store.SetString(ctx, key, string(data), mitreCacheTTL)
		}
		mu.Lock()
		result[inp.ID] = techs
		mu.Unlock()
		return nil
	})

	return result
}
```

**Important:** `alertHash` uses `sha256Sum` — you need to import `crypto/sha256` and define the helper. Replace the `alertHash` function and add the import:

```go
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"coralogix-alert-analyzer/internal/pipeline"
)
```

And update `alertHash` to:

```go
func alertHash(name, query, app, subsystem string) string {
	h := sha256.Sum256([]byte(name + "\x00" + query + "\x00" + app + "\x00" + subsystem))
	return fmt.Sprintf("%x", h[:8])
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/llm/... -run "TestBatchClassify|TestParseClassifierResponse|TestBuildClassifierMessage" -v
```
Expected: all tests pass.

- [ ] **Step 5: Verify full build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./... 2>&1
```
Expected: errors only for files that still import the `classifier` package (`handlers.go`) — those are fixed in Task 4. If mitre_mapper.go itself compiles cleanly, that is correct.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/llm/mitre_mapper.go internal/llm/mitre_mapper_test.go
git commit -m "feat(llm): replace BatchClassifyAndValidate with BatchClassify — single Claude call per alert"
```

---

## Task 4: Update handlers.go — remove classifier client, wire BatchClassify

**Files:**
- Modify: `backend/internal/api/handlers.go`

- [ ] **Step 1: Read the current MITRE pipeline block in handlers.go**

Read lines 155–205 of `backend/internal/api/handlers.go` to confirm the exact current code.

- [ ] **Step 2: Replace the classifier+validator block**

Find this block in `HandleAnalyze` (the MITRE pipeline section):

```go
	// Build MITRE mappings via classifier sidecar + Llama validator.
	// Only runs on security alerts with no existing label/T-code coverage.
	// Results are cached per-alert in Redis for 7 days.
	var llmMappings map[string][]string
	if h.cache != nil {
		baseCfg := llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		}
		validatorProvider, err := llm.NewClassifierProvider(
			h.config.LLM.ValidatorProvider,
			h.config.LLM.ValidatorModel,
			baseCfg,
		)
		if err != nil {
			log.Printf("WARN [analyze] validator provider unavailable: %v", err)
		} else {
			classifierClient := classifier.NewClient(h.config.Classifier.Endpoint)
			var inputs []llm.AlertInput
			for _, a := range alerts {
				if !coralogix.IsSecurityAlert(a) || coralogix.HasExistingMITRE(a) {
					continue
				}
				app, subsystem := coralogix.ExtractAppSubsystem(a.TypeDef)
				inputs = append(inputs, llm.AlertInput{
					ID:        a.ID,
					Name:      a.Name,
					Query:     coralogix.ExtractLuceneQuery(a.TypeDef),
					App:       app,
					Subsystem: subsystem,
				})
			}
			log.Printf("INFO [analyze] MITRE pipeline: %d/%d alerts need classification", len(inputs), len(alerts))
			if len(inputs) > 0 {
				llmMappings = llm.BatchClassifyAndValidate(ctx, classifierClient, validatorProvider, h.cache, h.sem, inputs)
			}
		}
	} else {
		log.Printf("WARN [analyze] cache unavailable, skipping MITRE classification pipeline for client=%s", req.Client)
	}
```

Replace with:

```go
	// Build MITRE mappings via Claude classifier.
	// Only runs on security alerts with no existing label/T-code coverage.
	// Results are cached per-alert in Redis for 7 days.
	var llmMappings map[string][]string
	if h.cache != nil {
		baseCfg := llm.ProviderConfig{
			AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
			ClaudeModel:     h.config.LLM.ClaudeModel,
			NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
			NvidiaModel:     h.config.LLM.NvidiaModel,
			NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
			GeminiAPIKey:    h.config.LLM.GeminiAPIKey,
			GeminiModel:     h.config.LLM.GeminiModel,
		}
		classifierProvider, err := llm.NewClassifierProvider(
			h.config.LLM.ClassifierProvider,
			h.config.LLM.ClassifierModel,
			baseCfg,
		)
		if err != nil {
			log.Printf("WARN [analyze] classifier provider unavailable: %v", err)
		} else {
			var inputs []llm.AlertInput
			for _, a := range alerts {
				if !coralogix.IsSecurityAlert(a) || coralogix.HasExistingMITRE(a) {
					continue
				}
				app, subsystem := coralogix.ExtractAppSubsystem(a.TypeDef)
				inputs = append(inputs, llm.AlertInput{
					ID:        a.ID,
					Name:      a.Name,
					Query:     coralogix.ExtractLuceneQuery(a.TypeDef),
					App:       app,
					Subsystem: subsystem,
				})
			}
			log.Printf("INFO [analyze] MITRE pipeline: %d/%d alerts need classification", len(inputs), len(alerts))
			if len(inputs) > 0 {
				llmMappings = llm.BatchClassify(ctx, classifierProvider, h.cache, h.sem, inputs)
			}
		}
	} else {
		log.Printf("WARN [analyze] cache unavailable, skipping MITRE classification for client=%s", req.Client)
	}
```

- [ ] **Step 3: Remove the classifier import from handlers.go**

Find the import block in `handlers.go`. Remove the line:
```go
	"coralogix-alert-analyzer/internal/classifier"
```

- [ ] **Step 4: Build — must be clean**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output (clean build). If there are remaining `classifier` import errors, check that the import was fully removed.

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... 2>&1 | tail -20
```
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/api/handlers.go
git commit -m "refactor(api): remove classifier sidecar client, wire llm.BatchClassify"
```

---

## Task 5: Update config.go — remove ClassifierConfig and ValidatorProvider/Model

**Files:**
- Modify: `backend/internal/config/config.go`

- [ ] **Step 1: Remove ClassifierConfig struct and Classifier field from Config**

Find and delete the struct definition:
```go
// ClassifierConfig holds settings for the local MITRE classifier sidecar.
type ClassifierConfig struct {
	Endpoint string `yaml:"endpoint"` // e.g. "http://localhost:8001"
}
```

Find and remove the `Classifier` field from the `Config` struct:
```go
	Classifier     ClassifierConfig        `yaml:"classifier"`
```

- [ ] **Step 2: Remove ValidatorProvider and ValidatorModel from LLMConfig**

Find and remove these two lines from `LLMConfig`:
```go
	// ValidatorProvider/Model: Llama used to confirm/reject classifier candidates.
	ValidatorProvider string `yaml:"validator_provider"`
	ValidatorModel    string `yaml:"validator_model"`
```

- [ ] **Step 3: Remove classifier.endpoint and validator_model validations**

Find and remove:
```go
	if cfg.Classifier.Endpoint == "" {
		return nil, fmt.Errorf("config: classifier.endpoint is required")
	}
	if cfg.LLM.ValidatorModel == "" {
		return nil, fmt.Errorf("config: llm.validator_model is required")
	}
```

- [ ] **Step 4: Update the LLMConfig comment**

Find:
```go
// LLMConfig holds settings for LLM-powered suggestions.
// API keys can also be set via ANTHROPIC_API_KEY / NVIDIA_API_KEY env vars.
```

Replace with:
```go
// LLMConfig holds settings for all LLM-powered features.
// API keys must be set via environment variables (ANTHROPIC_API_KEY, NVIDIA_API_KEY, etc.).
```

- [ ] **Step 5: Build**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output.

- [ ] **Step 6: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add internal/config/config.go
git commit -m "refactor(config): remove ClassifierConfig sidecar endpoint and ValidatorProvider/Model fields"
```

---

## Task 6: Update clients.yaml — remove classifier section and validator fields

**Files:**
- Modify: `backend/clients.yaml`

- [ ] **Step 1: Remove the classifier section and validator fields**

The current `clients.yaml` has a `classifier:` section and `validator_provider`/`validator_model` under `llm:`. Remove them.

Replace the entire file with:

```yaml
neon_dsn: ""           # set via NEON_DSN env var
monday_api_token: ""   # set via MONDAY_API_TOKEN env var
monday_board_id: 0     # set via MONDAY_BOARD_ID env var

llm:
  default_provider: "claude"
  nvidia_api_key: ""             # set via NVIDIA_API_KEY env var
  nvidia_model: "nvidia/nemotron-3-super-120b-a12b"
  nvidia_endpoint: "https://integrate.api.nvidia.com/v1/chat/completions"

  classifier_provider: "claude"
  classifier_model: "claude-haiku-4-5-20251001"

  suggestion_provider: "claude"
  suggestion_model: "claude-opus-4-7"
  nvidia_suggestion_api_key: "" # set via NVIDIA_SUGGESTION_API_KEY env var

  insights_provider: "claude"
  insights_model: "claude-opus-4-7"

  gemini_api_key: ""   # set via GEMINI_API_KEY env var
  gemini_model: "gemini-2.0-flash"

clients:
  Deel:
    api_key: ""          # set via CLIENT_DEEL_API_KEY env var
    region: eu1
    monday_group_id: "new_group47786"
  JioStar:
    api_key: ""          # set via CLIENT_JIOSTAR_API_KEY env var
    region: ap1
    monday_group_id: "group_mkrzg3ma"
```

- [ ] **Step 2: Build to verify config loads correctly**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
git add clients.yaml
git commit -m "config: remove classifier endpoint and validator fields from clients.yaml"
```

---

## Task 7: Delete internal/classifier/ package and Python sidecar

**Files:**
- Delete: `backend/internal/classifier/` (entire directory)
- Delete: `classifier/` (project root — entire Python sidecar)

- [ ] **Step 1: Verify no Go files import the classifier package**

```bash
grep -r "internal/classifier" /Users/aviral.baloni/Desktop/claude/backend --include="*.go"
```
Expected: **no output** (zero matches). If any matches appear, fix those imports before proceeding.

- [ ] **Step 2: Delete the internal/classifier package**

```bash
rm -rf /Users/aviral.baloni/Desktop/claude/backend/internal/classifier
```

- [ ] **Step 3: Delete the Python sidecar directory**

```bash
rm -rf /Users/aviral.baloni/Desktop/claude/classifier
```

- [ ] **Step 4: Build — must be clean**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```
Expected: no output.

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... -v 2>&1 | tail -30
```
Expected: all tests pass, no failures.

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add -A
git commit -m "feat: remove Python classifier sidecar and internal/classifier package — Claude is now the sole MITRE classifier"
```

---

## Final Verification

- [ ] **Run full backend test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./... -count=1 2>&1 | tail -20
```
Expected: all packages pass.

- [ ] **Confirm sidecar directories are gone**

```bash
ls /Users/aviral.baloni/Desktop/claude/classifier 2>&1
ls /Users/aviral.baloni/Desktop/claude/backend/internal/classifier 2>&1
```
Expected: both return `No such file or directory`.

- [ ] **Confirm no stale imports**

```bash
grep -r "internal/classifier\|classifier.NewClient\|ClassifierClientIface\|ValidateCandidates\|BatchClassifyAndValidate\|ValidatorProvider\|ValidatorModel\|Classifier.Endpoint" /Users/aviral.baloni/Desktop/claude/backend --include="*.go"
```
Expected: **no output**.
