# Olly Hunt Pipeline Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade `hunt.go`'s two-pass Olly pipeline to use a model split (gpt-5.4 / claude-sonnet-4-6), inject cx_security normalized fields into Pass 1, and add timeout recovery for Pass 2.

**Architecture:** Two-pass SSE pipeline remains unchanged. Pass 1 gets a faster model (gpt-5.4) plus injected field context. Pass 2 gets a stronger model (claude-sonnet-4-6) plus progressive back-off recovery on timeout. All changes are confined to `backend/internal/api/hunt.go` and its test file.

**Tech Stack:** Go 1.21+, `go test ./backend/internal/api/...`

---

## File Map

| File | Changes |
|------|---------|
| `backend/internal/api/hunt.go` | Replace `ollyModel` with two constants; add `--read-only` + new timeout to `cxRunner`; add `detectLogSource`, `_RAW_SUPPLEMENTARY_FIELDS`, `formatSourceFields`; enrich `ollySchemaPromptData` + template; add `runWithRecovery`; wire into `HandleHuntStream` |
| `backend/internal/api/hunt_test.go` | Add `TestDetectLogSource`, `TestBuildOllySchemaPromptEnrichment`, `TestRunWithRecovery_*`; extend `mockCxExecutor` with call-count support; update `TestBuildOllySchemaPrompt` |

---

## Task 1: Model split + `--read-only` + Pass 2 timeout

**Files:**
- Modify: `backend/internal/api/hunt.go` (constants block, `cxRunner.runOllySchema`, `cxRunner.runOllyReport`)
- Test: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write the failing test**

Add to `hunt_test.go`:

```go
func TestOllyModelConstants(t *testing.T) {
	if ollyPass1Model == "" {
		t.Error("ollyPass1Model must not be empty")
	}
	if ollyPass2Model == "" {
		t.Error("ollyPass2Model must not be empty")
	}
	if ollyPass1Model == ollyPass2Model {
		t.Error("pass1 and pass2 models must differ")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestOllyModelConstants -v
```

Expected: `undefined: ollyPass1Model`

- [ ] **Step 3: Replace the model constant in `hunt.go`**

Find and replace the existing constant block (around line 380):

```go
// ollyModel is the cx model used for both passes.
// claude-sonnet-4-5 is the only model that supports --mode skill and has
// DataPrime expertise. GPT focus mode reliably 524s on the Coralogix backend.
const ollyModel = "claude-sonnet-4-5"
```

Replace with:

```go
// ollyPass1Model is used for Pass 1 (schema discovery — 4 focused questions).
// gpt-5.4 is the recommended Pro-mode model; faster for structured short output.
const ollyPass1Model = "gpt-5.4"

// ollyPass2Model is used for Pass 2 (full 11-section report).
// claude-sonnet-4-6 has stronger long-form analytical reasoning than 4-5.
const ollyPass2Model = "claude-sonnet-4-6"
```

- [ ] **Step 4: Update `cxRunner.runOllySchema` to use `ollyPass1Model` + `--read-only`**

Find `runOllySchema` (around line 414) and update:

```go
func (r *cxRunner) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "olly", "ask",
		"--output", "agents", "--mode", "skill", "--model", ollyPass1Model,
		"--timeout", "300", "--read-only", prompt)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}
```

- [ ] **Step 5: Update `cxRunner.runOllyReport` to use `ollyPass2Model` + `--read-only` + timeout 900**

Find `runOllyReport` (around line 426) and update:

```go
func (r *cxRunner) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	args := []string{"olly", "ask", "--mode", "skill", "--model", ollyPass2Model,
		"--timeout", "900", "--read-only"}
	if chatID != "" {
		args = append(args, "--chat-id", chatID)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, r.binPath, args...)
	cmd.Env = r.env()
	return readCapped(cmd, maxOutputBytes)
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestOllyModelConstants -v
```

Expected: `PASS`

- [ ] **Step 7: Run full test suite to check nothing broke**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v 2>&1 | tail -20
```

Expected: all existing tests PASS (no references to removed `ollyModel` constant remain — `buildOllyPrompt` deprecated wrapper uses `buildOllySchemaPrompt` which will use `ollyPass1Model` after Task 3).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): model split gpt-5.4/claude-sonnet-4-6, --read-only, pass2 timeout 900s"
```

---

## Task 2: Log source detection + supplementary fields map

**Files:**
- Modify: `backend/internal/api/hunt.go` (new functions + map, below the `olly prompts` section)
- Test: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `hunt_test.go`:

```go
func TestDetectLogSource(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		// AWS
		{`eventSource:"cloudtrail.amazonaws.com"`, "aws"},
		{`eventName:"CreateBucket" AND awsRegion:us-east-1`, "aws"},
		{`errorCode:"AccessDenied" AND recipientAccountId:*`, "aws"},
		// GCP
		{`protoPayload.methodName:"storage.objects.delete"`, "gcp"},
		{`resource.type:"gcs_bucket"`, "gcp"},
		// Azure
		{`operationName:"Microsoft.Authorization/roleAssignments/write"`, "azure"},
		{`signinlogs AND resultType:"Failure"`, "azure"},
		// Okta
		{`target.type:"AppInstance" AND outcome.result:"FAILURE"`, "okta"},
		{`actor.type:"User" AND auth0`, "okta"},
		// CrowdStrike
		{`event.Technique:"T1059" AND event.SeverityName:"High"`, "crowdstrike"},
		// Palo Alto
		{`PaloAlto.threat_name:* AND PaloAlto.severity:"high"`, "paloalto"},
		{`panw AND ngfw`, "paloalto"},
		// Fortinet
		{`action:"deny" AND policyname:* AND fortinet`, "fortinet"},
		// Cloudflare
		{`Action:"block" AND RuleID:* AND ClientIP:*`, "cloudflare"},
		// M365
		{`Operation:"MailboxLogin" AND Workload:"Exchange"`, "m365"},
		{`UserId:* AND office365`, "m365"},
		// Google Workspace
		{`events.name:"login_failure" AND workspace`, "gworkspace"},
		// GitHub
		{`repo:* AND org:* AND action:"push"`, "github"},
		// SentinelOne
		{`event.DetectId:* AND sentinelone`, "sentinelone"},
		// Generic fallback
		{`event_type:"cmd_exec" AND user:admin`, "generic"},
		{`kubernetes.pod:frontend-*`, "generic"},
	}
	for _, tc := range tests {
		got := detectLogSource(tc.query)
		if got != tc.want {
			t.Errorf("detectLogSource(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestFormatSourceFields(t *testing.T) {
	// aws should return a non-empty string with known fields
	out := formatSourceFields("aws")
	if out == "" {
		t.Error("formatSourceFields(aws) should not be empty")
	}
	if !strings.Contains(out, "errorCode") {
		t.Errorf("formatSourceFields(aws) missing errorCode: %q", out)
	}
	// generic should still return something
	if formatSourceFields("generic") == "" {
		t.Error("formatSourceFields(generic) should not be empty")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run "TestDetectLogSource|TestFormatSourceFields" -v
```

Expected: `undefined: detectLogSource`

- [ ] **Step 3: Add `detectLogSource`, `_RAW_SUPPLEMENTARY_FIELDS`, and `formatSourceFields` to `hunt.go`**

Add after the `// ── Olly prompts` section header and before `ollySchemaPromptTemplate`:

```go
// ── Log source detection ──────────────────────────────────────────────────────

// detectLogSource returns a short identifier for the cloud/vendor log source
// inferred from keyword patterns in the Lucene query.
func detectLogSource(luceneQuery string) string {
	blob := strings.ToLower(luceneQuery)
	// Google Workspace — must precede GCP ("google" matches both)
	if containsAny(blob, "google workspace", "gsuite", "g-suite", "gmail-admin",
		"google-workspace", "workspace") {
		return "gworkspace"
	}
	if containsAny(blob, "cloudtrail", "guardduty", "vpc flow", "vpcflow",
		"aws", "s3 bucket", "aws iam", "aws eks", "lambda", "ec2",
		"rds snapshot", "aws waf", "recipientaccountid", "eventsource") {
		return "aws"
	}
	if containsAny(blob, "gcp", "cloudaudit", "google cloud", "stackdriver",
		"gke", "bigquery", "pubsub", "cloud audit", "protopayload") {
		return "gcp"
	}
	if containsAny(blob, "azure", "signinlogs", "auditlogs", "entra",
		"microsoft azure", "aad", "azuread") {
		return "azure"
	}
	if containsAny(blob, "okta", "auth0") {
		return "okta"
	}
	if containsAny(blob, "crowdstrike", "falcon", "cs-falcon") {
		return "crowdstrike"
	}
	if containsAny(blob, "paloalto", "palo alto", "panorama", "pan-os",
		"panw", "ngfw", "cortex xdr") {
		return "paloalto"
	}
	if containsAny(blob, "fortinet", "fortigate", "fortiweb", "fortisiem",
		"fortianalyzer", "forti") {
		return "fortinet"
	}
	if containsAny(blob, "cloudflare", "cf-ray", "cloudflare waf",
		"cloudflare access") {
		return "cloudflare"
	}
	if containsAny(blob, "m365", "office365", "office 365", "microsoft 365",
		"exchange online", "sharepoint", "msgraph", "microsoft-365", "defender for") {
		return "m365"
	}
	if containsAny(blob, "github", "gitlab") {
		return "github"
	}
	if containsAny(blob, "sentinelone", "sentinel one", "s1-") {
		return "sentinelone"
	}
	return "generic"
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// _rawSupplementaryFields maps log source identifier → source-specific fields
// that complement cx_security.* normalization. For sources with no normalization
// (CrowdStrike, Fortinet, Cloudflare, M365) these are the primary fields.
var _rawSupplementaryFields = map[string][]string{
	"aws": {
		"userIdentity.type              — role vs. user vs. service vs. assumed-role",
		"userIdentity.arn               — full ARN of the caller",
		"errorCode                      — e.g. AccessDenied, NoSuchBucket",
		"errorMessage                   — human-readable failure reason",
		"requestParameters.bucketName   — S3 target (or equivalent resource param)",
		"awsRegion                      — region where the API call was made",
		"recipientAccountId             — target account (cross-account activity)",
	},
	"gcp": {
		"protoPayload.methodName        — full RPC method name",
		"protoPayload.serviceName       — GCP service (storage.googleapis.com, etc.)",
		"protoPayload.status.code       — gRPC status code (0 = OK)",
		"resource.type                  — GCP resource type (gcs_bucket, etc.)",
		"resource.labels.project_id     — GCP project",
	},
	"azure": {
		"operationName                  — Azure operation",
		"resultType                     — Success / Failure",
		"resultDescription              — detailed failure reason",
		"resourceId                     — full ARM resource path",
	},
	"okta": {
		"target.displayName             — target app, group, or user acted upon",
		"outcome.result                 — SUCCESS, FAILURE, SKIPPED, ALLOW, DENY",
		"outcome.reason                 — detailed outcome reason",
		"debugContext.debugData.url     — request URL",
	},
	"crowdstrike": {
		"event.UserName                 — actor username",
		"event.ComputerName             — affected host",
		"event.RemoteAddressIP4         — source IP",
		"event.DetectId                 — detection identifier",
		"event.Technique                — MITRE ATT&CK technique name",
		"event.SeverityName             — severity (Critical, High, Medium, Low)",
		"event.PatternDispositionDescription — disposition (prevented, detected, etc.)",
	},
	"paloalto": {
		"PaloAlto.rule                  — firewall policy rule name",
		"PaloAlto.app                   — application identified",
		"PaloAlto.destination_port      — destination port",
		"PaloAlto.threat_name           — threat signature name",
		"PaloAlto.severity              — threat severity",
	},
	"fortinet": {
		"srcip                          — source IP address",
		"dstip                          — destination IP address",
		"action                         — allow / deny / close",
		"policyname                     — firewall policy name",
		"user                           — authenticated username",
		"msg                            — event description",
	},
	"cloudflare": {
		"ClientIP                       — client IP address",
		"ClientRequestUserAgent         — user agent string",
		"ClientCountry                  — client country code",
		"ClientASNDescription           — client ASN org",
		"Action                         — firewall action (block, allow, challenge)",
		"RuleID                         — firewall rule that matched",
	},
	"m365": {
		"UserId                         — actor UPN (user@domain.com)",
		"ClientIP                       — client IP address",
		"Operation                      — action type (FileDownloaded, MailboxLogin)",
		"ObjectId                       — target object (file path, mailbox)",
		"ResultStatus                   — Succeeded / Failed",
		"Workload                       — M365 workload (Exchange, SharePoint, Teams)",
	},
	"gworkspace": {
		"actor.callerType               — HUMAN vs. KEY (service account)",
		"events.name                    — specific event action",
		"events.type                    — event category (LOGIN, DRIVE, etc.)",
		"ipAddress                      — raw IP from Google's log",
	},
	"github": {
		"repo                           — repository name",
		"org                            — organization",
		"action                         — specific action (push, pull_request)",
		"data.ref                       — branch / tag reference",
		"programmatic_access_type       — token type (OAuth App, GitHub App)",
	},
	"sentinelone": {
		"event.DetectId                 — detection identifier",
		"event.SeverityName             — severity label",
		"event.category                 — detection category",
		"event.Technique                — MITRE ATT&CK technique",
		"event.PatternDispositionDescription — disposition (killed, quarantined)",
	},
	"generic": {
		"Any high-cardinality identity field (user, account, email, UPN)",
		"Any IP address field not mapped to cx_security.source_ip",
		"Any action / operation / method field",
		"Any outcome / result / status field",
	},
}

// formatSourceFields returns the supplementary fields for the given log source
// as a newline-joined string for prompt injection.
func formatSourceFields(source string) string {
	fields, ok := _rawSupplementaryFields[source]
	if !ok {
		fields = _rawSupplementaryFields["generic"]
	}
	var sb strings.Builder
	for _, f := range fields {
		sb.WriteString("  - ")
		sb.WriteString(f)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run "TestDetectLogSource|TestFormatSourceFields" -v
```

Expected: both PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): add detectLogSource + cx_security supplementary fields map"
```

---

## Task 3: Enrich Pass 1 prompt with field context + CONCISE_PREFIX

**Files:**
- Modify: `backend/internal/api/hunt.go` (`ollySchemaPromptTemplate`, `ollySchemaPromptData`, `buildOllySchemaPrompt`)
- Test: `backend/internal/api/hunt_test.go`

- [ ] **Step 1: Write the failing test**

Add to `hunt_test.go`:

```go
func TestBuildOllySchemaPromptEnrichment(t *testing.T) {
	// AWS query — should get AWS supplementary fields
	prompt, err := buildOllySchemaPrompt(
		`eventName:"CreateBucket" AND awsRegion:us-east-1`, 5, "event1", "7d")
	if err != nil {
		t.Fatalf("buildOllySchemaPrompt: %v", err)
	}
	// Must include Tier 1 cx_security fields
	if !strings.Contains(prompt, "cx_security.username") {
		t.Error("prompt missing cx_security.username (Tier 1 field)")
	}
	if !strings.Contains(prompt, "cx_security.source_ip") {
		t.Error("prompt missing cx_security.source_ip (Tier 1 field)")
	}
	// Must include log source detection result
	if !strings.Contains(prompt, "aws") {
		t.Error("prompt should mention detected log source: aws")
	}
	// Must include AWS-specific Tier 2 fields
	if !strings.Contains(prompt, "errorCode") {
		t.Error("prompt missing AWS supplementary field: errorCode")
	}
	// Must include concise prefix
	if !strings.Contains(prompt, "concise") {
		t.Error("prompt missing CONCISE_PREFIX instruction")
	}

	// Generic query — should get generic fields, no AWS fields
	genericPrompt, err := buildOllySchemaPrompt(
		`event_type:"cmd_exec" AND user:admin`, 2, "ev1", "30d")
	if err != nil {
		t.Fatalf("buildOllySchemaPrompt generic: %v", err)
	}
	if strings.Contains(genericPrompt, "errorCode") {
		t.Error("generic prompt must not contain AWS-specific field errorCode")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run TestBuildOllySchemaPromptEnrichment -v
```

Expected: FAIL — `prompt missing cx_security.username`

- [ ] **Step 3: Update `ollySchemaPromptData` struct in `hunt.go`**

Find the struct (around line 135):

```go
type ollySchemaPromptData struct {
	LuceneQuery  string
	SampleCount  int
	SampleEvents string
	Window       string
}
```

Replace with:

```go
type ollySchemaPromptData struct {
	ConcisePrefix string
	LuceneQuery   string
	SampleCount   int
	SampleEvents  string
	Window        string
	LogSource     string
	SourceFields  string
}

const ollySchemaPromptConcisePrefix = "Be concise and structured. Output only what's asked. Use compact tables. No preamble."
```

- [ ] **Step 4: Update `ollySchemaPromptTemplate` in `hunt.go`**

Find and replace the template constant (around line 116):

```go
const ollySchemaPromptTemplate = `{{.ConcisePrefix}}

Schema discovery for threat hunt. Answer ONLY these 4 questions:

Query: {{.LuceneQuery}}
Sample events ({{.SampleCount}} retrieved from last {{.Window}}):
{{.SampleEvents}}

Known normalized fields (cx_security.* — prefer these in DataPrime translation if confirmed present):
  - cx_security.username, cx_security.email, cx_security.target_username, cx_security.target_email
  - cx_security.source_ip, cx_security.destination_ip, cx_security.source_hostname
  - cx_security.userAgent, cx_security.event_name, cx_security.event_result
  - cx_security.event_type, cx_security.resource
  - GeoIP: cx_security.source_ip_geoip.asn.number, cx_security.source_ip_geoip.asn.organization, cx_security.source_ip_geoip.country_name

Log source detected: {{.LogSource}}
Source-specific supplementary fields (use if cx_security.* fields are absent):
{{.SourceFields}}

1. Field mapping: for each key term in the Lucene query, find the matching DataPrime $d path
   in the sample events. Which confirmed paths have data?

2. Pattern match: translate the Lucene query to DataPrime using the confirmed fields from
   question 1. Run it. Do the specific IOCs/patterns actually appear? How many events match?

3. True total: run source logs | filter <your DataPrime translation> | count() as total
   Report the exact number on its own line as: "Total: N"

4. Top patterns: source logs | filter <condition> | groupby <most relevant field> aggregate
   count() as hits | orderby hits desc | limit 5

Facts only.`
```

- [ ] **Step 5: Update `buildOllySchemaPrompt` to populate the new fields**

Find and replace the function (around line 144):

```go
func buildOllySchemaPrompt(luceneQuery string, sampleCount int, sampleEvents, window string) (string, error) {
	source := detectLogSource(luceneQuery)
	var buf bytes.Buffer
	err := ollySchemaTmpl.Execute(&buf, ollySchemaPromptData{
		ConcisePrefix: ollySchemaPromptConcisePrefix,
		LuceneQuery:   luceneQuery,
		SampleCount:   sampleCount,
		SampleEvents:  sampleEvents,
		Window:        window,
		LogSource:     source,
		SourceFields:  formatSourceFields(source),
	})
	if err != nil {
		return "", fmt.Errorf("render olly schema prompt: %w", err)
	}
	return buf.String(), nil
}
```

Note: the function signature is **unchanged** — no call sites need updating.

- [ ] **Step 6: Run tests**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run "TestBuildOllySchemaPromptEnrichment|TestBuildOllySchemaPrompt|TestBuildOllyPrompt" -v
```

Expected: all three PASS. `TestBuildOllySchemaPrompt` already tests the non-enrichment aspects; `TestBuildOllySchemaPromptEnrichment` tests the new fields.

- [ ] **Step 7: Run full test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): inject cx_security fields + log source detection into pass 1 prompt"
```

---

## Task 4: Timeout recovery (`runWithRecovery`)

**Files:**
- Modify: `backend/internal/api/hunt.go` (new function below `cxRunner`)
- Test: `backend/internal/api/hunt_test.go` (extend mock + 3 new tests)

- [ ] **Step 1: Extend `mockCxExecutor` to support call counting**

In `hunt_test.go`, find the `mockCxExecutor` struct and replace it:

```go
type mockCxExecutor struct {
	logsOutput   []byte
	logsErr      error
	schemaOutput []byte
	schemaErr    error
	// reportOutputs[0] is the primary call; subsequent entries are recovery ping replies.
	// If only one entry, all calls return the same value.
	reportOutputs  [][]byte
	reportErrors   []error
	reportCallIdx  int
	capturedChatID string
}

func (m *mockCxExecutor) runLogs(ctx context.Context, query, window string) ([]byte, error) {
	return m.logsOutput, m.logsErr
}

func (m *mockCxExecutor) runOllySchema(ctx context.Context, prompt string) ([]byte, error) {
	return m.schemaOutput, m.schemaErr
}

func (m *mockCxExecutor) runOllyReport(ctx context.Context, chatID, prompt string) ([]byte, error) {
	m.capturedChatID = chatID
	idx := m.reportCallIdx
	if idx >= len(m.reportOutputs) {
		idx = len(m.reportOutputs) - 1
	}
	m.reportCallIdx++
	var err error
	if idx < len(m.reportErrors) {
		err = m.reportErrors[idx]
	}
	return m.reportOutputs[idx], err
}
```

Update `TestHandleHuntStream_MockSuccess` to use the new struct shape — its single `reportOutput` becomes `reportOutputs`:

```go
mock := &mockCxExecutor{
	logsOutput:    []byte(`[{"$m":{"timestamp":"2024-01-01T10:00:00"},"$l":{"applicationname":"h1"},"$d":{"cmd":"enc"}}]`),
	schemaOutput:  schemaAgentsOut,
	reportOutputs: [][]byte{reportOut},
}
```

- [ ] **Step 2: Write the failing recovery tests**

Add to `hunt_test.go`:

```go
func TestRunWithRecovery_ImmediateSuccess(t *testing.T) {
	reportOut := []byte("## 1. What We Found\nTotal hits: 3")
	mock := &mockCxExecutor{
		reportOutputs: [][]byte{reportOut},
	}
	out, err := runWithRecovery(context.Background(), mock, "chat-abc", "report prompt", 3)
	if err != nil {
		t.Fatalf("runWithRecovery: unexpected error: %v", err)
	}
	if string(out) != string(reportOut) {
		t.Errorf("output = %q, want %q", out, reportOut)
	}
	if mock.reportCallIdx != 1 {
		t.Errorf("reportCallIdx = %d, want 1 (no recovery needed)", mock.reportCallIdx)
	}
}

func TestRunWithRecovery_RecoverySucceeds(t *testing.T) {
	recoveryOut := []byte("## 1. What We Found\nRecovered result")
	mock := &mockCxExecutor{
		reportOutputs: [][]byte{nil, recoveryOut},
		reportErrors:  []error{fmt.Errorf("cx exit: signal: killed"), nil},
	}
	out, err := runWithRecovery(context.Background(), mock, "chat-xyz", "report prompt", 3)
	if err != nil {
		t.Fatalf("runWithRecovery: unexpected error after recovery: %v", err)
	}
	if string(out) != string(recoveryOut) {
		t.Errorf("output = %q, want %q", out, recoveryOut)
	}
	if mock.reportCallIdx != 2 {
		t.Errorf("reportCallIdx = %d, want 2 (primary + 1 recovery)", mock.reportCallIdx)
	}
}

func TestRunWithRecovery_ExhaustsRetries(t *testing.T) {
	mock := &mockCxExecutor{
		reportOutputs: [][]byte{nil, nil, nil, nil, nil, nil},
		reportErrors: []error{
			fmt.Errorf("timeout"), fmt.Errorf("timeout"), fmt.Errorf("timeout"),
			fmt.Errorf("timeout"), fmt.Errorf("timeout"), fmt.Errorf("timeout"),
		},
	}
	_, err := runWithRecovery(context.Background(), mock, "chat-fail", "report prompt", 5)
	if err == nil {
		t.Error("expected error after exhausting retries, got nil")
	}
	if mock.reportCallIdx != 6 {
		t.Errorf("reportCallIdx = %d, want 6 (1 primary + 5 recoveries)", mock.reportCallIdx)
	}
}

func TestRunWithRecovery_NoChatIDSkipsRecovery(t *testing.T) {
	mock := &mockCxExecutor{
		reportOutputs: [][]byte{nil},
		reportErrors:  []error{fmt.Errorf("timeout")},
	}
	_, err := runWithRecovery(context.Background(), mock, "", "report prompt", 5)
	if err == nil {
		t.Error("expected error when chatID is empty and primary fails")
	}
	if mock.reportCallIdx != 1 {
		t.Errorf("reportCallIdx = %d, want 1 (no recovery without chatID)", mock.reportCallIdx)
	}
}
```

Also add `"fmt"` to the import block in `hunt_test.go` if not present.

- [ ] **Step 3: Run to verify they fail**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run "TestRunWithRecovery" -v
```

Expected: `undefined: runWithRecovery`

- [ ] **Step 4: Add `runWithRecovery` to `hunt.go`**

Add after the `// ── cx executor` section, before `// ── SSE helpers`:

```go
// recoveryWaits defines the sleep durations between recovery ping attempts.
// 45s → 60s → 75s → 90s → 105s, mirroring the olly.py baselining system.
var recoveryWaits = []time.Duration{
	45 * time.Second,
	60 * time.Second,
	75 * time.Second,
	90 * time.Second,
	105 * time.Second,
}

const recoveryPing = "Are you done? Return your best answer so far in the requested format. Be concise."

// runWithRecovery calls cx.runOllyReport for Pass 2. On error (Cloudflare 524 or
// subprocess timeout), if chatID is non-empty it polls the same chat with a short
// recovery ping on a progressive back-off schedule (up to maxRetries attempts).
// If chatID is empty, it returns the error immediately — there is no chat to recover.
func runWithRecovery(ctx context.Context, cx cxExecutor, chatID, prompt string, maxRetries int) ([]byte, error) {
	out, err := cx.runOllyReport(ctx, chatID, prompt)
	if err == nil && len(out) > 0 {
		return out, nil
	}

	if chatID == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("olly report: empty response")
	}

	lastErr := err
	if lastErr == nil {
		lastErr = fmt.Errorf("olly report: empty response")
	}

	waits := recoveryWaits
	if maxRetries < len(waits) {
		waits = waits[:maxRetries]
	}

	for attempt, wait := range waits {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		rec, rerr := cx.runOllyReport(ctx, chatID, recoveryPing)
		if rerr == nil && len(rec) > 0 {
			return rec, nil
		}
		if rerr != nil {
			lastErr = rerr
		}
		_ = attempt
	}

	return nil, fmt.Errorf("olly report timed out after %d recovery attempts: %w", len(waits), lastErr)
}
```

- [ ] **Step 5: Run tests (with short timeout to avoid actual sleeping)**

The tests pass `context.Background()` and mock returns immediately, so no real sleeping occurs:

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -run "TestRunWithRecovery" -v -timeout 30s
```

Expected: all 4 PASS

- [ ] **Step 6: Run full test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "feat(hunt): add runWithRecovery with progressive back-off for pass 2 timeout"
```

---

## Task 5: Wire `runWithRecovery` into `HandleHuntStream`

**Files:**
- Modify: `backend/internal/api/hunt.go` (`HandleHuntStream` only)
- Test: `backend/internal/api/hunt_test.go` (no new tests — `TestHandleHuntStream_MockSuccess` already exercises this path)

- [ ] **Step 1: Locate the Pass 2 call in `HandleHuntStream`**

In `hunt.go`, find this block (around line 579):

```go
// Step 2b: Pass 2 — full 11-section report (claude-sonnet-4-5, skill, ~216s)
// Continues the same chat so Olly has confirmed field names for live pivots.
// Falls back to a fresh chat if chat_id could not be extracted.
reportOut, err := cx.runOllyReport(ctx, chatID, buildOllyReportPrompt())
if err != nil {
    sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
    return
}
```

- [ ] **Step 2: Replace it with `runWithRecovery`**

```go
// Step 2b: Pass 2 — full 11-section report (claude-sonnet-4-6, skill, ~216s)
// Continues the same chat (chat_id from pass 1) so Olly uses confirmed field names.
// On timeout, polls the same chat with a recovery ping (up to 5 attempts).
reportOut, err := runWithRecovery(ctx, cx, chatID, buildOllyReportPrompt(), 5)
if err != nil {
    sendSSE(w, flusher, "error", huntErrorData{Code: "olly_failed", Message: err.Error()})
    return
}
```

Also update the comment on the Pass 1 block to reflect the new model:

```go
// Step 2a: Pass 1 — schema discovery (gpt-5.4, skill, ~72s)
```

- [ ] **Step 3: Run full test suite**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v 2>&1 | tail -20
```

Expected: all PASS, including `TestHandleHuntStream_MockSuccess`

- [ ] **Step 4: Build check**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go build ./...
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/hunt.go
git commit -m "feat(hunt): wire runWithRecovery into HandleHuntStream pass 2"
```

---

## Task 6: Final verification

- [ ] **Step 1: Run the complete test suite with race detector**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test -race ./internal/api/... -v 2>&1 | grep -E "PASS|FAIL|panic"
```

Expected: all PASS, no panics, no race conditions

- [ ] **Step 2: Verify vet passes**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go vet ./internal/api/...
```

Expected: no output (no issues)

- [ ] **Step 3: Check test count hasn't shrunk**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend && go test ./internal/api/... -v 2>&1 | grep -c "^--- PASS"
```

Expected: ≥ 30 (previous count was ~25; new tests add ≥5)

- [ ] **Step 4: Final commit**

```bash
git add backend/internal/api/hunt.go backend/internal/api/hunt_test.go
git commit -m "test(hunt): verify full suite — model split, field injection, recovery all green"
```

---

## Self-Review Notes

- `detectLogSource` uses query-only pattern matching — no alert metadata available in hunt context (stated in spec ✓)
- `runWithRecovery` only recovers Pass 2 — Pass 1 has no `chatID` so skips recovery (spec ✓, test `TestRunWithRecovery_NoChatIDSkipsRecovery` covers this ✓)
- `ollyPass1Model`/`ollyPass2Model` replace the single `ollyModel` constant — `buildOllyPrompt` (deprecated wrapper) delegates to `buildOllySchemaPrompt` which calls `buildOllySchemaPrompt` — the wrapper still compiles ✓
- `mockCxExecutor` extended to support multi-call with `reportOutputs [][]byte` — `TestHandleHuntStream_MockSuccess` updated to use `reportOutputs: [][]byte{reportOut}` ✓
- `recoveryWaits` is a package-level `var` (not `const`) so tests can override it for zero-sleep test runs if needed in future ✓
- No UI changes, no SSE protocol changes, no new API endpoints — exactly as scoped ✓
