// backend/internal/llm/detection_builder.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"coralogix-alert-analyzer/internal/models"
)

// BuildTechnique is the internal representation used by the LLM package.
type BuildTechnique struct {
	ID          string
	Name        string
	TacticID    string
	TacticName  string
	TacticOrder int
	Source      string
}

const buildDetectionSystemPrompt = `You are a senior detection engineer at a SOC. A user has selected MITRE ATT&CK techniques to build a multi-stage detection chain.

Your job:
1. VALIDATE the chain represents a coherent attacker kill-chain (Recon < Initial Access < Execution < Persistence < Priv Esc < Evasion < Cred Access < Discovery < Lateral < Collection < C2 < Exfiltration < Impact). Flag missing steps or implausible sequences.
2. PROPOSE 3–4 flow alerts that together would catch this attacker behaviour.
3. For each alert provide:
   - name: detection title following the pattern "<Verb> <Subject> via <Method>" — e.g. "Detect Credential Dump via LSASS Memory Access"
   - description: one-line summary
   - techniqueId: the MITRE T-id this alert primarily detects (from the user's list)
   - logic: Lucene/OpenSearch DSL query using ECS field paths ONLY. NEVER use vendor-specific field names (see FIELD NAMES rule below).
   - sigma_rule: full Sigma YAML block as a single escaped string. Must include title, status: experimental, logsource (with product e.g. "windows"), detection (with ECS field paths ONLY), falsepositives, level, and tags with attack.<tactic> and attack.<technique-id>.
   - window: realistic per-stage correlation window. Use "1m" for execution; "5m" for persistence/priv-esc; "15m" for initial access/cred-access; "30m" for evasion; "6h" for lateral/discovery; "12h" for collection; "24h" for C2/exfil.
   - windowReason: one sentence explaining the time window choice (surfaces in the UI)
   - source: telemetry source — one of "EDR", "CloudTrail", "IdP", "Email", "Network", "WAF"
   - severity: "critical" | "high" | "medium" | "low" (critical = code execution/exfil; high = priv-esc/cred theft/lateral; medium = discovery/persistence; low = informational)
   - falsepositives: JSON array with at least one realistic false positive scenario — e.g. ["Legitimate admin tools that perform the same operation"]
4. One CORRELATION RULE tying all flow alerts together with the longest plausible attacker dwell window ("1h" | "24h" | "72h").
5. VALIDATION findings: list issues, warnings, or confirmations.

FIELD NAMES — STRICT RULE:
All field names in logic (Lucene) and sigma detection blocks MUST use ECS paths. NEVER use vendor-specific field names. The logsource.product field handles vendor translation.

FORBIDDEN → ECS replacement:
  CrowdStrike  event_type:ProcessRollup2 / process_name / CommandLine → event.action / process.name / process.command_line
  CrowdStrike  ParentImageFileName                                     → process.parent.executable
  GuardDuty    detail.type / detail.service.action.*                   → event.action / destination.ip / source.ip
  GuardDuty    detail.resource.instanceDetails.*                       → cloud.instance.id / host.id
  Sysmon       EventID:1/3/11 / TargetFilename / SourceImage           → use logsource.service:sysmon + process.name / file.path / process.executable
  Windows      EventID:4624 / SubjectUserName / TargetUserName         → use logsource.service:security + event.action / user.name
  Okta         eventType / actor.id / outcome.result                   → event.action / user.id / event.outcome
  CloudTrail   eventName / sourceIPAddress / userIdentity.arn          → event.action / source.ip / aws.cloudtrail.user_identity.arn

Correct ECS examples:
  process.name:"powershell.exe" AND process.command_line:*-enc*
  event.action:"user.session.start" AND source.ip:* AND user.name:*
  file.path:*\\AppData\\* AND process.name:"wscript.exe"
  destination.port:4444 AND network.direction:"egress"

OUTPUT STRICT JSON ONLY — no prose, no markdown fences, just the object:
{
  "validation": {
    "verdict": "ok" | "warnings" | "invalid",
    "findings": [{"level": "info"|"warn"|"error", "message": "..."}]
  },
  "alerts": [
    {
      "name": "...", "description": "...", "techniqueId": "T...",
      "logic": "...", "sigma_rule": "...", "window": "...", "windowReason": "...",
      "source": "...", "severity": "...", "falsepositives": ["..."]
    }
  ],
  "correlation": {
    "name": "...", "logic": "Alert 1 (T...) → Alert 2 (T...) within {window}",
    "window": "...", "severity": "critical" | "high"
  }
}`

func buildDetectionPrompt(techs []BuildTechnique) string {
	ordered := make([]BuildTechnique, len(techs))
	copy(ordered, techs)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].TacticOrder < ordered[j].TacticOrder
	})

	var sb strings.Builder
	sb.WriteString("USER'S SELECTED TECHNIQUES (kill-chain order):\n")
	for _, t := range ordered {
		sb.WriteString(fmt.Sprintf("  - %s %q — tactic: %s — telemetry: %s\n",
			t.ID, t.Name, t.TacticName, t.Source))
	}
	return sb.String()
}

// GenerateDetection calls the LLM and returns a BuildDetectionResponse.
// Falls back to mockBuildDetection if the LLM call or JSON parse fails.
func GenerateDetection(ctx context.Context, provider Provider, techs []BuildTechnique) (*models.BuildDetectionResponse, error) {
	userMsg := buildDetectionPrompt(techs)
	log.Printf("INFO [detection_builder] requesting %s for %d techniques", provider.Name(), len(techs))

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: buildDetectionSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    4096,
		FastMode:     true,
	})
	if err != nil {
		log.Printf("WARN [detection_builder] LLM error: %v — using mock fallback", err)
		mock := mockBuildDetection(techs)
		return mock, nil
	}

	result, err := parseDetectionResult(resp)
	if err != nil {
		log.Printf("WARN [detection_builder] parse error: %v — using mock fallback", err)
		mock := mockBuildDetection(techs)
		return mock, nil
	}
	for _, a := range result.Alerts {
		if errs := validateDetectionAlert(a); len(errs) > 0 {
			log.Printf("WARN [detection_builder] quality gate for alert %q: %v", a.Name, errs)
		}
	}
	return result, nil
}

func parseDetectionResult(raw string) (*models.BuildDetectionResponse, error) {
	cleaned := strings.TrimSpace(raw)
	// Strip markdown fences if present.
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx > 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}
	// Find the outermost JSON object.
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	cleaned = cleaned[start : end+1]
	cleaned = sanitizeJSONStrings(cleaned) // reuse existing helper from suggestions.go

	var result models.BuildDetectionResponse
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("JSON parse: %w\nraw: %s", err, raw[:min(len(raw), 400)])
	}
	return &result, nil
}

// mockBuildDetection returns a deterministic response without calling the LLM.
func mockBuildDetection(techs []BuildTechnique) *models.BuildDetectionResponse {
	ordered := make([]BuildTechnique, len(techs))
	copy(ordered, techs)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].TacticOrder < ordered[j].TacticOrder
	})

	windowByTactic := map[string]struct{ w, why string }{
		"TA0043": {"1h", "Recon usually precedes access by an hour or less."},
		"TA0001": {"15m", "Initial access events are immediate; correlate within 15 minutes."},
		"TA0002": {"5m", "Execution should follow access within minutes."},
		"TA0003": {"1h", "Persistence is typically established within an hour of access."},
		"TA0004": {"6h", "Privilege escalation often happens within a working session."},
		"TA0005": {"30m", "Defense evasion clusters around execution and persistence."},
		"TA0006": {"15m", "Credential access is usually rapid once execution succeeds."},
		"TA0007": {"6h", "Discovery commands run during the attacker reconnaissance window."},
		"TA0008": {"6h", "Lateral movement happens hours after initial foothold."},
		"TA0009": {"12h", "Collection precedes exfiltration by hours."},
		"TA0011": {"24h", "C2 channels persist as long as the attacker has a foothold."},
		"TA0010": {"24h", "Exfiltration is the final stage; window covers staged extraction."},
		"TA0040": {"15m", "Impact actions are tightly clustered once triggered."},
	}
	sevByTactic := map[string]string{
		"TA0001": "high", "TA0002": "medium", "TA0003": "high", "TA0004": "high",
		"TA0005": "medium", "TA0006": "critical", "TA0007": "low", "TA0008": "high",
		"TA0009": "medium", "TA0011": "medium", "TA0010": "critical", "TA0040": "critical",
	}

	// Per-technique Lucene queries grounded in standard telemetry field names.
	luceneByTech := map[string]string{
		"T1078":     `event.action:"login_success" AND NOT source.ip:("10.0.0.0/8" OR "192.168.0.0/16") AND (mfa_result:("timeout" OR "auto_push") OR source.country:NOT_IN_BASELINE)`,
		"T1078.004": `event.provider:"sts.amazonaws.com" AND event.action:("AssumeRole" OR "GetFederationToken") AND NOT user_agent:*boto* AND source.ip:NOT_IN_BASELINE`,
		"T1190":     `http.response.status_code:(400 OR 401 OR 403 OR 500) AND (http.request.uri:(*union+select* OR *<script* OR *..\/..\/etc*) OR http.request.body.bytes:>100000)`,
		"T1566":     `email.attachments.file.extension:("exe" OR "doc" OR "xlsm" OR "js" OR "vbs" OR "lnk") AND email.sender.domain:NOT_IN_ALLOWLIST`,
		"T1133":     `event.action:("vpn_auth_success" OR "rdp_session_start") AND (source.geo.country_iso_code:NOT_IN_BASELINE OR source.ip:NOT_IN_BASELINE)`,
		"T1059":     `process.name:("powershell.exe" OR "cmd.exe" OR "wscript.exe" OR "cscript.exe") AND process.args:(*-enc* OR *-EncodedCommand* OR *IEX* OR *-nop* OR *bypass*)`,
		"T1204":     `process.parent.name:("WINWORD.EXE" OR "EXCEL.EXE" OR "POWERPNT.EXE" OR "outlook.exe") AND process.name:("powershell.exe" OR "cmd.exe" OR "mshta.exe")`,
		"T1610":     `event.provider:("ecs.amazonaws.com" OR "eks.amazonaws.com") AND event.action:("RunTask" OR "CreateDeployment") AND NOT user.name:*ci-*`,
		"T1098":     `event.action:("Add member to role" OR "Update user" OR "Reset password") AND target.user.is_privileged:true`,
		"T1098.001": `event.provider:"iam.amazonaws.com" AND event.action:("CreateAccessKey" OR "CreateLoginProfile" OR "AttachUserPolicy") AND event.outcome:"success"`,
		"T1136":     `event.action:("CreateUser" OR "net user /add") AND NOT process.name:("sAMAccountName" OR "adws.exe")`,
		"T1547":     `registry.path:("HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run" OR "HKCU\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run") AND registry.data.type:"REG_SZ"`,
		"T1053":     `(process.name:"schtasks.exe" AND process.args:(*\/create* OR *\/change*)) OR event.action:"SchRpcRegisterTask"`,
		"T1548":     `event.action:("PromptForCredentials" OR "AdjustTokenPrivileges" OR "sudo") AND user.name:NOT_IN_ADMIN_BASELINE`,
		"T1068":     `process.parent.name:("iexplore.exe" OR "chrome.exe" OR "firefox.exe") AND process.name:("cmd.exe" OR "powershell.exe") AND process.privilege_level:"high"`,
		"T1484":     `(event.action:"Modify Group Policy Object" OR process.name:"gpupdate.exe") AND user.name:NOT_IN_DOMAIN_ADMIN_BASELINE`,
		"T1027":     `(process.args:(*-enc* OR *-EncodedCommand*) AND process.args_length:>500) OR file.pe.imphash:IN_KNOWN_PACKERS`,
		"T1070":     `(process.name:"wevtutil.exe" AND process.args:(*cl* OR *clear*)) OR (event.action:"DeleteFile" AND file.path:*\\System32\\winevt\\*)`,
		"T1562":     `(process.name:("sc.exe" OR "net.exe") AND process.args:(*stop* OR *delete*) AND process.args:("WinDefend" OR "MpsSvc" OR "wscsvc" OR "Sense"))`,
		"T1110":     `event.action:("login_fail" OR "authentication_failed") AND event.count:>10 AND timeframe:5m AND source.ip:SAME_IP`,
		"T1003":     `(process.name:"lsass.exe" AND event.action:"OpenProcess" AND process.access.rights:("0x1010" OR "0x1038" OR "0x143a")) OR process.name:("procdump.exe" OR "mimikatz.exe")`,
		"T1555":     `(process.name:("mimikatz.exe" OR "lazagne.exe") OR registry.path:*VaultFiles*) OR (process.name:"cmd.exe" AND process.args:*cmdkey*)`,
		"T1087":     `process.name:("net.exe" OR "net1.exe") AND process.args:(*user* OR *group* OR *localgroup*) AND NOT process.parent.name:"services.exe"`,
		"T1018":     `(process.name:("nmap" OR "netscan.exe" OR "arp.exe") OR (network.destination.port:445 AND event.count:>20 AND timeframe:1m AND source.ip:SAME_IP))`,
		"T1538":     `event.provider:"signin.amazonaws.com" AND event.action:"ConsoleLogin" AND event.outcome:"success" AND source.ip:NOT_IN_BASELINE`,
		"T1021":     `(event.action:("smb_connect" OR "rdp_session_start") OR network.destination.port:(22 OR 445 OR 3389)) AND source.ip:NOT_IN_INTERNAL_RANGE`,
		"T1550":     `(event.action:("kerberoasting" OR "AS-REP Roasting")) OR (network.protocol:"kerberos" AND kerberos.ticket.encryption_type:"0x17")`,
		"T1570":     `network.protocol:"smb" AND file.action:"create" AND file.extension:("exe" OR "dll" OR "bat" OR "ps1") AND NOT source.user:IN_ASSET_BASELINE`,
		"T1005":     `file.action:"read" AND file.path:(*\\Documents\\* OR *\\Desktop\\* OR *\\Downloads\\*) AND process.name:NOT_IN_ALLOWLIST AND file.size:>10MB`,
		"T1530":     `event.provider:"s3.amazonaws.com" AND event.action:"GetObject" AND source.ip:NOT_IN_TRUSTED_RANGES AND event.count:>50 AND timeframe:15m`,
		"T1560":     `process.name:("7z.exe" OR "rar.exe" OR "zip.exe" OR "tar") AND file.action:"create" AND file.size:>50MB AND file.path:NOT_IN_BACKUP_PATHS`,
		"T1071":     `network.transport:"tcp" AND NOT network.destination.port:(80 OR 443 OR 53 OR 8080 OR 8443) AND network.bytes_out:>1MB AND timeframe:1h`,
		"T1573":     `ssl.certificate.issuer:NOT_IN_KNOWN_CAS AND network.bytes_out:>500KB AND ssl.established:true AND destination.ip:NOT_IN_BASELINE`,
		"T1105":     `network.protocol:"http" AND http.request.method:"GET" AND file.action:"create" AND file.extension:("exe" OR "dll" OR "ps1") AND process.name:NOT_IN_BROWSERS`,
		"T1041":     `network.bytes_out:>100MB AND network.destination.ip:IN_THREAT_INTEL AND event.duration:>300s`,
		"T1567":     `http.request.method:"POST" AND http.request.body.bytes:>10MB AND http.destination.domain:("transfer.sh" OR "mega.nz" OR "wetransfer.com" OR "file.io")`,
		"T1567.002": `event.provider:"s3.amazonaws.com" AND event.action:"PutObject" AND bucket.acl:"public-read" AND source.ip:NOT_IN_TRUSTED_RANGES`,
		"T1486":     `file.action:"rename" AND file.path_new:(*\.encrypted OR *\.locked OR *\.crypt) AND event.count:>50 AND timeframe:1m`,
		"T1490":     `(process.name:"vssadmin.exe" AND process.args:(*delete* OR *resize shadows*)) OR (process.name:"wbadmin.exe" AND process.args:*delete*)`,
		"T1485":     `file.action:"delete" AND event.count:>200 AND timeframe:2m AND process.name:NOT_IN_ALLOWLIST`,
	}

	alerts := make([]models.BuildDetectionAlert, len(ordered))
	for i, t := range ordered {
		wt := windowByTactic[t.TacticID]
		if wt.w == "" {
			wt = struct{ w, why string }{"1h", "Standard correlation window for this stage."}
		}
		src := strings.SplitN(t.Source, "/", 2)[0]
		lucene, ok := luceneByTech[t.ID]
		if !ok {
			lucene = fmt.Sprintf(`event.category:"%s" AND technique.id:"%s"`, strings.ToLower(t.TacticName), t.ID)
		}
		sev := sevByTactic[t.TacticID]
		if sev == "" {
			sev = "medium"
		}
		tacticSlug := strings.ToLower(strings.ReplaceAll(t.TacticName, " ", "_"))
		sigmaRule := fmt.Sprintf(
			"title: Detect %s via %s\nstatus: experimental\nlogsource:\n  product: windows\n  service: security\ndetection:\n  selection:\n    technique.id: '%s'\n  condition: selection\nfalsepositives:\n  - Authorized administrative activity\nlevel: %s\ntags:\n  - attack.%s\n  - attack.%s",
			t.Name, t.TacticName, t.ID, sev, tacticSlug, strings.ToLower(t.ID),
		)
		alerts[i] = models.BuildDetectionAlert{
			Name:           fmt.Sprintf("Detect %s via %s", t.Name, t.TacticName),
			Description:    fmt.Sprintf("Detect %s activity (%s).", strings.ToLower(t.Name), t.TacticName),
			TechniqueID:    t.ID,
			Logic:          lucene,
			Window:         wt.w,
			WindowReason:   wt.why,
			Source:         strings.TrimSpace(src),
			Severity:       sev,
			SigmaRule:      sigmaRule,
			Falsepositives: []string{"Authorized administrative activity using the same tools or commands."},
		}
	}

	// Validation findings.
	tacticIDs := make(map[string]bool)
	for _, t := range ordered {
		tacticIDs[t.TacticID] = true
	}
	var findings []models.BuildDetectionFinding
	if !tacticIDs["TA0001"] && len(tacticIDs) > 1 {
		findings = append(findings, models.BuildDetectionFinding{
			Level:   "warn",
			Message: "No Initial Access technique — chain starts mid-attack.",
		})
	}
	if tacticIDs["TA0010"] && !tacticIDs["TA0009"] {
		findings = append(findings, models.BuildDetectionFinding{
			Level:   "info",
			Message: "Exfiltration without Collection — consider adding T1530.",
		})
	}
	if len(findings) == 0 {
		findings = append(findings, models.BuildDetectionFinding{
			Level:   "info",
			Message: "Chain is sequenced correctly across the kill-chain.",
		})
	}
	verdict := "ok"
	for _, f := range findings {
		if f.Level == "error" {
			verdict = "invalid"
			break
		} else if f.Level == "warn" {
			verdict = "warnings"
		}
	}

	dwell := len(ordered)
	corrWindow := "1h"
	if dwell >= 4 {
		corrWindow = "72h"
	} else if dwell >= 3 {
		corrWindow = "24h"
	}

	ids := make([]string, len(ordered))
	for i, t := range ordered {
		ids[i] = t.ID
	}
	logicParts := make([]string, len(alerts))
	for i, a := range alerts {
		logicParts[i] = fmt.Sprintf("Alert %d (%s)", i+1, a.TechniqueID)
	}
	corrSev := "high"
	for _, a := range alerts {
		if a.Severity == "critical" {
			corrSev = "critical"
			break
		}
	}

	var result models.BuildDetectionResponse
	result.Validation.Verdict = verdict
	result.Validation.Findings = findings
	result.Alerts = alerts
	result.Correlation = models.BuildDetectionCorrelation{
		Name:     "Multi-stage chain: " + strings.Join(ids, " → "),
		Logic:    strings.Join(logicParts, " → ") + " within " + corrWindow,
		Window:   corrWindow,
		Severity: corrSev,
	}
	return &result
}
