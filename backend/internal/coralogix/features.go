package coralogix

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"coralogix-alert-analyzer/internal/models"
)

// ── Score-based fusion for MITRE mapping ─────────────────────────

// Minimum combined score for a technique to be included in the mapping.
// Scoring: label=3, name_pattern=3, name_action=2, desc_regex=2,
// desc_pattern=1.5, desc_action=1, type_fallback=1.
// Per technique, take max score per category (label/name/description/type),
// then sum across categories. Threshold of 2.0 means:
//   - label alone (3) → yes
//   - name pattern alone (3) → yes
//   - desc regex alone (2) → yes
//   - desc pattern alone (1.5) → no (needs corroboration)
//   - desc action alone (1) → no (needs corroboration)
//   - type fallback alone (1) → no (needs corroboration)
const fusionThreshold = 2.0

// scoreCategory groups signal sources for score aggregation.
type scoreCategory string

const (
	catLabel       scoreCategory = "label"
	catLLM         scoreCategory = "llm"
	catName        scoreCategory = "name"
	catDescription scoreCategory = "description"
	catType        scoreCategory = "type"
)

// techniqueCandidate accumulates confidence from multiple sources.
type techniqueCandidate struct {
	scores map[scoreCategory]float64
}

func (c *techniqueCandidate) total() float64 {
	var sum float64
	for _, s := range c.scores {
		sum += s
	}
	return sum
}

// addCandidate registers a technique with a score in a category,
// keeping the max score per category.
func addCandidate(candidates map[string]*techniqueCandidate, tid string, cat scoreCategory, score float64) {
	c, ok := candidates[tid]
	if !ok {
		c = &techniqueCandidate{scores: make(map[scoreCategory]float64)}
		candidates[tid] = c
	}
	if score > c.scores[cat] {
		c.scores[cat] = score
	}
}

// mapperCategory converts a ScoredCandidate category string to a scoreCategory.
func mapperCategory(cat string) scoreCategory {
	switch cat {
	case "name":
		return catName
	case "description":
		return catDescription
	case "type":
		return catType
	default:
		return catName
	}
}

var (
	// techniqueIDRegex matches MITRE technique IDs: T1234 or T1234.001
	techniqueIDRegex = regexp.MustCompile(`[Tt]\d{4}(?:\.\d{3})?`)
	// bareIDRegex matches bare technique IDs without T prefix: just 4 digits
	bareIDRegex = regexp.MustCompile(`^\d{4}$`)

	// tacticIDToName maps MITRE tactic IDs (numeric part) to tactic names
	tacticIDToName = map[string]string{
		"0001": "initial-access",
		"0002": "execution",
		"0003": "persistence",
		"0004": "privilege-escalation",
		"0005": "defense-evasion",
		"0006": "credential-access",
		"0007": "discovery",
		"0008": "lateral-movement",
		"0009": "collection",
		"0010": "exfiltration",
		"0011": "command-and-control",
		"0031": "command-and-control", // alternate ID seen in data
		"0040": "impact",
		"0042": "resource-development",
		"0043": "reconnaissance",
		"0104": "reconnaissance", // alternate seen in data
	}

	// techniqueToTactics maps parent technique IDs to all their tactics.
	// Multi-tactic techniques (T1078, T1053, T1055, etc.) list all applicable tactics.
	techniqueToTactics = map[string][]string{
		// Reconnaissance
		"T1595": {"reconnaissance"}, "T1592": {"reconnaissance"}, "T1589": {"reconnaissance"},
		"T1590": {"reconnaissance"}, "T1591": {"reconnaissance"}, "T1598": {"reconnaissance"},
		"T1597": {"reconnaissance"}, "T1596": {"reconnaissance"}, "T1593": {"reconnaissance"},
		"T1594": {"reconnaissance"},
		// Resource Development
		"T1583": {"resource-development"}, "T1584": {"resource-development"},
		"T1586": {"resource-development"}, "T1585": {"resource-development"},
		"T1588": {"resource-development"}, "T1587": {"resource-development"},
		"T1608": {"resource-development"}, "T1635": {"resource-development"},
		// Initial Access
		"T1566": {"initial-access"}, "T1190": {"initial-access"}, "T1189": {"initial-access"},
		"T1195": {"initial-access"}, "T1199": {"initial-access"},
		"T1133": {"initial-access"}, "T1200": {"initial-access"}, "T1091": {"initial-access"},
		// Multi-tactic: Valid Accounts
		"T1078": {"initial-access", "persistence", "privilege-escalation", "defense-evasion"},
		// Execution
		"T1059": {"execution"}, "T1204": {"execution"}, "T1047": {"execution"},
		"T1106": {"execution"}, "T1569": {"execution"},
		"T1609": {"execution"}, "T1610": {"execution"}, "T1203": {"execution"},
		// Multi-tactic: Scheduled Task/Job
		"T1053": {"execution", "persistence", "privilege-escalation"},
		// Persistence
		"T1037": {"persistence"}, "T1505": {"persistence"},
		"T1556": {"persistence"}, "T1136": {"persistence"},
		"T1542": {"persistence"}, "T1137": {"persistence"},
		// Multi-tactic: persistence + privilege-escalation
		"T1547": {"persistence", "privilege-escalation"},
		"T1543": {"persistence", "privilege-escalation"},
		"T1546": {"persistence", "privilege-escalation"},
		"T1574": {"persistence", "privilege-escalation"},
		// Multi-tactic: persistence + privilege-escalation
		"T1098": {"persistence", "privilege-escalation"},
		// Privilege Escalation
		"T1068": {"privilege-escalation"},
		"T1611": {"privilege-escalation"},
		// Multi-tactic: privilege-escalation + defense-evasion
		"T1548": {"privilege-escalation", "defense-evasion"},
		"T1134": {"privilege-escalation", "defense-evasion"},
		"T1484": {"privilege-escalation", "defense-evasion"},
		// Defense Evasion
		"T1027": {"defense-evasion"}, "T1070": {"defense-evasion"},
		"T1562": {"defense-evasion"}, "T1036": {"defense-evasion"}, "T1112": {"defense-evasion"},
		"T1014": {"defense-evasion"}, "T1564": {"defense-evasion"}, "T1218": {"defense-evasion"},
		"T1553": {"defense-evasion"}, "T1480": {"defense-evasion"}, "T1221": {"defense-evasion"},
		"T1535": {"defense-evasion"}, "T1578": {"defense-evasion"},
		"T1140": {"defense-evasion"}, "T1202": {"defense-evasion"},
		"T1497": {"defense-evasion"},
		// Multi-tactic: defense-evasion + lateral-movement
		"T1550": {"defense-evasion", "lateral-movement"},
		// Multi-tactic: defense-evasion + privilege-escalation
		"T1055": {"defense-evasion", "privilege-escalation"},
		// Credential Access
		"T1110": {"credential-access"}, "T1003": {"credential-access"}, "T1558": {"credential-access"},
		"T1528": {"credential-access"}, "T1539": {"credential-access"}, "T1552": {"credential-access"},
		"T1621": {"credential-access"}, "T1056": {"credential-access"},
		"T1111": {"credential-access"}, "T1606": {"credential-access"}, "T1649": {"credential-access"},
		// Multi-tactic: credential-access + collection
		"T1557": {"credential-access", "collection"},
		// Discovery
		"T1087": {"discovery"}, "T1046": {"discovery"}, "T1069": {"discovery"},
		"T1082": {"discovery"}, "T1083": {"discovery"}, "T1057": {"discovery"},
		"T1012": {"discovery"}, "T1018": {"discovery"}, "T1016": {"discovery"},
		"T1049": {"discovery"}, "T1033": {"discovery"}, "T1007": {"discovery"},
		"T1124": {"discovery"}, "T1580": {"discovery"}, "T1526": {"discovery"},
		"T1538": {"discovery"}, "T1613": {"discovery"}, "T1614": {"discovery"},
		// Lateral Movement
		"T1021": {"lateral-movement"}, "T1534": {"lateral-movement"},
		"T1080": {"lateral-movement"}, "T1570": {"lateral-movement"},
		"T1563": {"lateral-movement"}, "T1602": {"lateral-movement"},
		// Collection
		"T1213": {"collection"},
		"T1114": {"collection"}, "T1530": {"collection"}, "T1039": {"collection"},
		"T1025": {"collection"}, "T1119": {"collection"}, "T1115": {"collection"},
		"T1113": {"collection"}, "T1074": {"collection"}, "T1560": {"collection"},
		// Command and Control
		"T1071": {"command-and-control"}, "T1105": {"command-and-control"},
		"T1573": {"command-and-control"}, "T1572": {"command-and-control"},
		"T1090": {"command-and-control"}, "T1219": {"command-and-control"},
		"T1095": {"command-and-control"}, "T1571": {"command-and-control"},
		"T1132": {"command-and-control"}, "T1568": {"command-and-control"},
		// Exfiltration
		"T1041": {"exfiltration"}, "T1048": {"exfiltration"}, "T1567": {"exfiltration"},
		"T1029": {"exfiltration"}, "T1030": {"exfiltration"}, "T1537": {"exfiltration"},
		"T1020": {"exfiltration"},
		// Impact
		"T1486": {"impact"}, "T1485": {"impact"}, "T1489": {"impact"},
		"T1490": {"impact"}, "T1491": {"impact"}, "T1498": {"impact"},
		"T1496": {"impact"}, "T1495": {"impact"}, "T1531": {"impact"},
		"T1499": {"impact"}, "T1561": {"impact"},
	}

	// providerNormalize maps raw alert_provider values to clean names
	providerNormalize = map[string]string{
		"aws":              "aws",
		"eks":              "aws_eks",
		"cloudtrail":       "aws_cloudtrail",
		"route53audit":     "aws_route53",
		"gcp":              "gcp",
		"gemini":           "google_gemini",
		"google_workspace": "google_workspace",
		"github":           "github",
		"crowdstrike":      "crowdstrike",
		"okta":             "okta",
		"slack":            "slack",
		"cloudflare":       "cloudflare",
		"paloalto":         "palo_alto",
		"palo alto":        "palo_alto",
		"netskope":         "netskope",
		"sfdc":             "salesforce",
		"kubernetes":       "kubernetes",
		"kandji":           "kandji",
		"island":           "island",
		"coralogix":        "coralogix",
	}

	// Keyword matching for alerts WITHOUT labels (conservative)
	dataSourceKeywords = map[string]string{
		"okta": "okta", "aws": "aws", "cloudtrail": "aws_cloudtrail",
		"m365": "m365", "office 365": "m365", "o365": "m365",
		"azure": "azure", "entra": "azure_ad", "gcp": "gcp",
		"google workspace": "google_workspace", "github": "github",
		"crowdstrike": "crowdstrike", "palo alto": "palo_alto",
		"kubernetes": "kubernetes", "k8s": "kubernetes", "eks": "aws_eks",
		"cloudflare": "cloudflare", "slack": "slack", "salesforce": "salesforce",
		"netskope": "netskope", "duo": "duo",
	}

	entityKeywords = map[string]string{
		"user": "user", "username": "user", "ip": "ip_address",
		"ip_address": "ip_address", "source_ip": "ip_address",
		"device": "device", "endpoint": "device", "host": "host",
		"account": "account", "email": "email", "role": "role",
		"session": "session", "token": "token", "bucket": "bucket",
		"pod": "pod", "container": "container", "process": "process",
	}

	actionKeywords = map[string]string{
		"login": "login", "sign-in": "login", "signin": "login",
		"authenticate": "login", "access": "access", "create": "create",
		"delete": "delete", "remove": "delete", "modify": "modify",
		"update": "modify", "change": "modify", "execute": "execute",
		"download": "download", "upload": "upload", "exfiltrat": "exfiltration",
		"escalat": "escalation", "enumerate": "enumeration", "scan": "scan",
		"disable": "disable", "enable": "enable", "grant": "grant",
		"revoke": "revoke", "reset": "reset", "forward": "forward",
		"assume role": "assume_role", "impersonat": "impersonation",
	}

	conditionKeywords = map[string]string{
		"anomaly": "anomaly", "anomalous": "anomaly", "spike": "spike",
		"impossible travel": "impossible_travel", "brute force": "brute_force",
		"brute-force": "brute_force", "threshold": "threshold",
		"new value": "new_value", "first seen": "first_seen",
		"first time": "first_seen", "rare": "rare", "unusual": "unusual",
		"baseline": "deviation", "frequency": "frequency",
		"failed": "failure", "repeated": "repeated", "off-hours": "off_hours",
		"multiple": "multiple",
	}
)

// isSecurityAlert determines whether an alert is a security detection
// based on its labels and lucene query content.
func isSecurityAlert(alert *models.AlertDef) bool {
	if alert.Labels != nil {
		// Any of these labels indicates a security alert
		if alert.Labels["alert_type"] == "security" {
			return true
		}
		for _, key := range []string{
			"alert_provider", "alert_extension_pack", "security_severity",
			"mitre_technique", "alert_mitre_technique", "technique",
			"mitre_tactic", "alert_mitre_tactic", "mitre_tatic",
		} {
			if v, ok := alert.Labels[key]; ok && v != "" {
				return true
			}
		}
		if alert.Labels["flow_alert"] == "building block" || alert.Labels["flow_alert"] == "buildingblock" {
			return true
		}
	}

	// Check lucene query for security-relevant field references
	lucene := strings.ToLower(extractLuceneQuery(alert.TypeDef))
	if lucene != "" {
		securityFields := []string{
			"security", "threat", "attack", "malware", "brute",
			"login", "auth", "privilege", "firewall", "waf", "exploit",
			"vulnerability", "incident", "detection", "suspicious",
			"anomaly", "unauthorized", "phishing", "credential",
		}
		for _, f := range securityFields {
			if strings.Contains(lucene, f) {
				return true
			}
		}
	}

	// Check alert name for building block pattern (many have null labels)
	nameLower := strings.ToLower(alert.Name)
	if strings.Contains(nameLower, "building block") || strings.Contains(nameLower, "correlation alert") {
		return true
	}

	return false
}

// isVendorCovered detects severity pass-through alerts from security vendors
// using pattern-based detection — no hardcoded vendor list.
//
// A severity pass-through is an alert where the vendor detects techniques
// internally but only surfaces a severity level / generic finding to Coralogix.
// Detection criteria (all must be true):
//  1. Has a security provenance label (alert_provider or alert_extension_pack)
//  2. Name contains a generic detection/severity term
//  3. Name lacks behavioral specificity (no action/attack words)
//  4. Not a building block or correlation alert
//
// The vendor name is extracted from labels, not matched against a list.
func isVendorCovered(alert *models.AlertDef) (string, bool) {
	nameLower := strings.ToLower(alert.Name)

	// Building blocks and correlation alerts are Coralogix-authored, not vendor pass-throughs
	if strings.Contains(nameLower, "building block") || strings.Contains(nameLower, "correlation alert") {
		return "", false
	}

	// 1. Must have security provenance label — proves it came from a vendor tool
	vendor := ""
	if alert.Labels != nil {
		if v := alert.Labels["alert_provider"]; v != "" {
			vendor = v
		} else if v := alert.Labels["alert_extension_pack"]; v != "" {
			vendor = v
		}
	}
	if vendor == "" {
		return "", false
	}

	// 2. Name must contain a generic detection/severity term.
	// Uses word-boundary matching to prevent "low" matching "flow"/"allow",
	// "high" matching "highlight", etc.
	genericTerms := []string{
		"severity", "detection", "detected", "incident",
		"finding", "cdr alert", "lead summary",
		"critical", "high", "medium", "low", "informational",
	}
	hasGenericTerm := false
	for _, term := range genericTerms {
		if containsWord(nameLower, term) {
			hasGenericTerm = true
			break
		}
	}
	if !hasGenericTerm {
		return "", false
	}

	// 3. Name must lack behavioral specificity — if the name describes
	// a specific attack or action, it's a real detection, not a pass-through.
	// Uses substring matching (not word-boundary) because many terms are
	// intentional prefixes: "exfiltrat" → exfiltration, "escalat" → escalation.
	behavioralIndicators := []string{
		// Attack actions
		"brute", "login", "exec", "execute", "inject", "encrypt",
		"exfiltrat", "escalat", "phishing", "scan", "spray",
		"lateral", "reconnaissance", "recon", "enumerat",
		// Specific behaviors
		"unauthorized", "denied", "failed", "suspicious",
		"anomal", "malware", "ransomware", "exploit",
		// Specific targets indicating a real detection
		"credential", "password", "token", "certificate",
		"firewall", "policy", "container", "publicly exposed",
		"bucket", "database", "secret", "key rotation",
		"data unload", "file delet", "permission",
	}
	for _, indicator := range behavioralIndicators {
		if strings.Contains(nameLower, indicator) {
			return "", false // behavioral — map normally, not a pass-through
		}
	}

	return vendor, true
}

// containsWord checks if s contains word as a whole word (bounded by
// non-alphanumeric characters or string edges). Prevents "low" matching
// inside "flow", "high" inside "highlight", etc.
func containsWord(s, word string) bool {
	for idx := 0; idx < len(s); {
		i := strings.Index(s[idx:], word)
		if i < 0 {
			return false
		}
		pos := idx + i
		end := pos + len(word)
		leftOK := pos == 0 || !isWordChar(s[pos-1])
		rightOK := end == len(s) || !isWordChar(s[end])
		if leftOK && rightOK {
			return true
		}
		idx = pos + 1
	}
	return false
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// ExtractFeatures populates the Features field on each alert.
// llmMappings maps alert ID → MITRE technique IDs from the LLM mapper (highest trust source).
// Pass nil to skip LLM-based mapping (falls back to rule-based only).
// Flow alerts inherit techniques/tactics from their building block alerts.
// Non-security alerts are classified but not MITRE-mapped.
func ExtractFeatures(alerts []*models.AlertDef, llmMappings map[string][]string) {
	// Build ID → alert index for building block lookups.
	byID := make(map[string]*models.AlertDef, len(alerts))
	for _, a := range alerts {
		byID[a.ID] = a
	}

	// Pass 1: extract features for all non-flow alerts first.
	for _, alert := range alerts {
		if alert.AlertType != "flow" {
			isSec := isSecurityAlert(alert)
			if isSec {
				// Check vendor-covered before full mapping
				if vendor, vc := isVendorCovered(alert); vc {
					alert.Features = extractVendorCoveredFeatures(alert, llmMappings)
					alert.Features.IsSecurityAlert = true
					alert.Features.VendorCovered = true
					alert.Features.VendorName = vendor
				} else {
					alert.Features = extractAlertFeatures(alert, llmMappings)
					alert.Features.IsSecurityAlert = true
				}
			} else {
				alert.Features = extractMinimalFeatures(alert)
				alert.Features.IsSecurityAlert = false
			}
		}
	}

	// Pass 2: extract features for flow alerts, then enrich from building blocks.
	for _, alert := range alerts {
		if alert.AlertType != "flow" {
			continue
		}
		isSec := isSecurityAlert(alert)
		if isSec {
			alert.Features = extractAlertFeatures(alert, llmMappings)
			alert.Features.IsSecurityAlert = true
		} else {
			alert.Features = extractMinimalFeatures(alert)
			alert.Features.IsSecurityAlert = false
		}
		enrichFlowFromBuildingBlocks(alert, byID)
	}
}

// extractVendorCoveredFeatures populates minimal features for vendor pass-through
// alerts. Sources applied in priority order: LLM query analysis (highest),
// description T-codes, then labels (vendor-provided, may be generic/wrong).
func extractVendorCoveredFeatures(alert *models.AlertDef, llmMappings map[string][]string) models.AlertFeatures {
	f := extractMinimalFeatures(alert)

	// Source 1 (highest): LLM analysis of alert name + query
	// Validate T-IDs against known taxonomy to reject hallucinated technique IDs.
	if llmMappings != nil {
		for _, tid := range llmMappings[alert.ID] {
			if _, ok := techniqueToTactics[parentTechnique(tid)]; !ok {
				continue
			}
			if !contains(f.Techniques, tid) {
				f.Techniques = append(f.Techniques, tid)
			}
		}
	}

	// Source 2: explicit T-codes in description (author-written, high confidence)
	for _, tid := range extractTechniqueIDs(alert.Description) {
		if !contains(f.Techniques, tid) {
			f.Techniques = append(f.Techniques, tid)
		}
	}

	// Source 3: vendor labels (additive — may be generic but still useful)
	labelTechs, labelTactics := extractMITREFromLabels(alert.Labels)
	for _, tid := range labelTechs {
		if !contains(f.Techniques, tid) {
			f.Techniques = append(f.Techniques, tid)
		}
	}
	for _, tactic := range labelTactics {
		if !contains(f.Tactics, tactic) {
			f.Tactics = append(f.Tactics, tactic)
		}
	}

	sort.Strings(f.Techniques)

	// Derive tactics from all accepted techniques
	for _, tid := range f.Techniques {
		baseTID := parentTechnique(tid)
		if tactics, ok := techniqueToTactics[baseTID]; ok {
			for _, tactic := range tactics {
				if !contains(f.Tactics, tactic) {
					f.Tactics = append(f.Tactics, tactic)
				}
			}
		}
	}

	return f
}

// extractMinimalFeatures populates only data sources and basic metadata
// without running MITRE mapping. Used for non-security alerts.
func extractMinimalFeatures(alert *models.AlertDef) models.AlertFeatures {
	f := models.AlertFeatures{}
	f.DataSources = extractDataSources(alert)
	if f.DataSources == nil {
		f.DataSources = []string{}
	}
	f.Entities = []string{}
	f.Actions = []string{}
	f.Conditions = []string{}
	f.Techniques = []string{}
	f.Tactics = []string{}
	return f
}

// enrichFlowFromBuildingBlocks resolves building block alert IDs from the flow
// stages and inherits their MITRE techniques and tactics.
func enrichFlowFromBuildingBlocks(flow *models.AlertDef, byID map[string]*models.AlertDef) {
	bbIDs := extractBuildingBlockIDs(flow.TypeDef)
	if len(bbIDs) == 0 {
		return
	}

	var bbNames []string
	for _, id := range bbIDs {
		bb, ok := byID[id]
		if !ok {
			continue
		}
		bbNames = append(bbNames, bb.Name)

		// Inherit techniques
		for _, t := range bb.Features.Techniques {
			if !contains(flow.Features.Techniques, t) {
				flow.Features.Techniques = append(flow.Features.Techniques, t)
			}
		}
		// Inherit tactics
		for _, t := range bb.Features.Tactics {
			if !contains(flow.Features.Tactics, t) {
				flow.Features.Tactics = append(flow.Features.Tactics, t)
			}
		}
	}

	// Store building block names for richer context
	flow.Features.BuildingBlocks = bbNames
}

// extractBuildingBlockIDs parses flow typeDef stages to get referenced alert IDs.
func extractBuildingBlockIDs(typeDef map[string]any) []string {
	if typeDef == nil {
		return nil
	}
	stages, _ := typeDef["stages"].([]any)
	if len(stages) == 0 {
		return nil
	}

	var ids []string
	seen := make(map[string]bool)
	for _, stageRaw := range stages {
		stage, _ := stageRaw.(map[string]any)
		if stage == nil {
			continue
		}
		groups, _ := stage["flowStagesGroups"].(map[string]any)
		if groups == nil {
			continue
		}
		groupList, _ := groups["groups"].([]any)
		for _, gRaw := range groupList {
			g, _ := gRaw.(map[string]any)
			if g == nil {
				continue
			}
			alertDefs, _ := g["alertDefs"].([]any)
			for _, adRaw := range alertDefs {
				ad, _ := adRaw.(map[string]any)
				if ad == nil {
					continue
				}
				id, _ := ad["id"].(string)
				if id != "" && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func extractAlertFeatures(alert *models.AlertDef, llmMappings map[string][]string) models.AlertFeatures {
	f := models.AlertFeatures{}

	// ── Score-based fusion: collect candidates from all sources ──
	candidates := make(map[string]*techniqueCandidate)

	// Source 1 (highest): LLM analysis of alert name + query (score 3.0)
	// LLM reads the actual detection logic, making it the most accurate signal.
	// Validate each T-ID against the known technique taxonomy to reject hallucinations.
	if llmMappings != nil {
		for _, tid := range llmMappings[alert.ID] {
			if _, ok := techniqueToTactics[parentTechnique(tid)]; ok {
				addCandidate(candidates, tid, catLLM, 3.0)
			}
		}
	}

	// Source 2: Description T-ID regex (score 2.0)
	descTechniques := extractTechniqueIDs(alert.Description)
	for _, tid := range descTechniques {
		addCandidate(candidates, tid, catDescription, 2.0)
	}

	// Source 3: Rule-based mapper (various scores per category)
	for _, sc := range DefaultMapper.MapCandidates(alert) {
		addCandidate(candidates, sc.TechniqueID, mapperCategory(sc.Category), sc.Score)
	}

	// Source 4: Labels (score 3.0, additive — vendor-provided but may be generic)
	labelTechs, labelTactics := extractMITREFromLabels(alert.Labels)
	for _, tid := range labelTechs {
		addCandidate(candidates, tid, catLabel, 3.0)
	}

	// ── Apply threshold ─────────────────────────────────────────
	for tid, c := range candidates {
		if c.total() >= fusionThreshold {
			f.Techniques = append(f.Techniques, tid)
		}
	}
	sort.Strings(f.Techniques)

	// ── Derive tactics from accepted techniques + label tactics ──
	for _, tactic := range labelTactics {
		if !contains(f.Tactics, tactic) {
			f.Tactics = append(f.Tactics, tactic)
		}
	}
	for _, tid := range f.Techniques {
		baseTID := parentTechnique(tid)
		if tactics, ok := techniqueToTactics[baseTID]; ok {
			for _, tactic := range tactics {
				if !contains(f.Tactics, tactic) {
					f.Tactics = append(f.Tactics, tactic)
				}
			}
		}
	}

	// ── Data sources from labels (primary) ──────────────────────
	f.DataSources = extractDataSources(alert)

	// ── 5. Build text corpus for keyword extraction ─────────────
	corpus := strings.ToLower(alert.Name + " " + alert.Description + " " + extractLuceneQuery(alert.TypeDef))

	// ── 6. Entities, actions, conditions from keywords ──────────
	f.Entities = matchKeywords(corpus, entityKeywords)
	f.Actions = matchKeywords(corpus, actionKeywords)
	f.Conditions = matchKeywords(corpus, conditionKeywords)
	f.Conditions = appendTypeConditions(f.Conditions, alert.AlertType)

	// ── 7. Time window from type definition ─────────────────────
	f.TimeWindow = extractTimeWindow(alert.TypeDef)

	// Ensure no nil slices
	if f.DataSources == nil {
		f.DataSources = []string{}
	}
	if f.Entities == nil {
		f.Entities = []string{}
	}
	if f.Actions == nil {
		f.Actions = []string{}
	}
	if f.Conditions == nil {
		f.Conditions = []string{}
	}
	if f.Techniques == nil {
		f.Techniques = []string{}
	}
	if f.Tactics == nil {
		f.Tactics = []string{}
	}

	return f
}

// ── MITRE Extraction ─────────────────────────────────────────────

// extractMITREFromLabels reads mitre_technique and mitre_tactic from entity_labels.
func extractMITREFromLabels(labels map[string]string) (techniques []string, tactics []string) {
	if labels == nil {
		return nil, nil
	}

	// Technique: check multiple label keys
	for _, key := range []string{"mitre_technique", "alert_mitre_technique", "technique"} {
		if raw, ok := labels[key]; ok && raw != "" {
			for _, t := range normalizeTechniqueID(raw) {
				if !contains(techniques, t) {
					techniques = append(techniques, t)
				}
			}
		}
	}

	// Tactic: check multiple label keys (including typos found in data)
	for _, key := range []string{"mitre_tactic", "alert_mitre_tactic", "mitre_tatic"} {
		if raw, ok := labels[key]; ok && raw != "" {
			if tactic := normalizeTacticID(raw); tactic != "" {
				if !contains(tactics, tactic) {
					tactics = append(tactics, tactic)
				}
			}
		}
	}

	return
}

// normalizeTechniqueID handles formats: "t1485", "T1485", "1485", "T1485.001"
func normalizeTechniqueID(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// Handle comma-separated values
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		t := normalizeSingleTechnique(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

func normalizeSingleTechnique(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))

	// Already has T prefix: T1485, T1485.001
	if techniqueIDRegex.MatchString(s) {
		return s
	}

	// Bare digits: 1485 → T1485
	if bareIDRegex.MatchString(s) {
		return "T" + s
	}

	// Handle malformed like "t11016" (typo) — skip
	return ""
}

// normalizeTacticID handles formats: "ta0003", "TA0003", "0040", "ta005", "ta00040"
func normalizeTacticID(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}

	// Strip "ta" prefix if present
	numPart := raw
	if strings.HasPrefix(numPart, "ta") {
		numPart = strings.TrimPrefix(numPart, "ta")
	}

	// It might be a technique ID placed in the tactic field (e.g., "t1486")
	if strings.HasPrefix(raw, "t") && !strings.HasPrefix(raw, "ta") {
		tid := normalizeSingleTechnique(raw)
		if tid != "" {
			baseTID := parentTechnique(tid)
			if tactics, ok := techniqueToTactics[baseTID]; ok && len(tactics) > 0 {
				return tactics[0] // return primary tactic
			}
		}
		return ""
	}

	// Normalize: "005" → "0005", "00040" → "0040"
	numPart = strings.TrimLeft(numPart, "0")
	if numPart == "" {
		return ""
	}
	// Pad to 4 digits
	for len(numPart) < 4 {
		numPart = "0" + numPart
	}

	if tactic, ok := tacticIDToName[numPart]; ok {
		return tactic
	}

	return ""
}

// extractTechniqueIDs finds T####(.###) patterns in text.
func extractTechniqueIDs(text string) []string {
	matches := techniqueIDRegex.FindAllString(text, -1)
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		t := strings.ToUpper(m)
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

func parentTechnique(tid string) string {
	if idx := strings.Index(tid, "."); idx != -1 {
		return tid[:idx]
	}
	return tid
}

// ── Data Sources ─────────────────────────────────────────────────

func extractDataSources(alert *models.AlertDef) []string {
	seen := make(map[string]bool)
	var sources []string

	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || s == "unknown" {
			return
		}
		if normalized, ok := providerNormalize[s]; ok {
			s = normalized
		}
		if !seen[s] {
			seen[s] = true
			sources = append(sources, s)
		}
	}

	// Primary: from labels
	if alert.Labels != nil {
		for _, key := range []string{"alert_provider", "alert_source", "alert_service"} {
			if v, ok := alert.Labels[key]; ok {
				add(v)
			}
		}
	}

	// Fallback: keyword matching on name only (not description — too noisy)
	if len(sources) == 0 {
		nameLower := strings.ToLower(alert.Name)
		for keyword, ds := range dataSourceKeywords {
			if strings.Contains(nameLower, keyword) && !seen[ds] {
				seen[ds] = true
				sources = append(sources, ds)
			}
		}
	}

	return sources
}

// ── Lucene Query Extraction ──────────────────────────────────────

// ExtractLuceneQuery extracts the Lucene query string from an alert TypeDef.
// Exported so callers outside this package can build LLM inputs.
func ExtractLuceneQuery(typeDef map[string]any) string {
	return extractLuceneQuery(typeDef)
}

// ExtractAppSubsystem extracts the applicationName and subsystemName filters
// from an alert's logsFilter TypeDef. Returns empty strings if not present.
func ExtractAppSubsystem(typeDef map[string]any) (app, subsystem string) {
	if typeDef == nil {
		return
	}
	logsFilter, _ := typeDef["logsFilter"].(map[string]any)
	if logsFilter == nil {
		return
	}
	simpleFilter, _ := logsFilter["simpleFilter"].(map[string]any)
	if simpleFilter == nil {
		return
	}
	labelFilters, _ := simpleFilter["labelFilters"].(map[string]any)
	if labelFilters == nil {
		return
	}
	app = firstLabelFilterValue(labelFilters, "applicationName")
	subsystem = firstLabelFilterValue(labelFilters, "subsystemName")
	return
}

func firstLabelFilterValue(labelFilters map[string]any, key string) string {
	vals, _ := labelFilters[key].([]any)
	if len(vals) == 0 {
		return ""
	}
	first, _ := vals[0].(map[string]any)
	if first == nil {
		return ""
	}
	val, _ := first["value"].(string)
	return val
}

// HasExistingMITRE returns true if the alert already has MITRE techniques from
// labels or description T-codes — these alerts are skipped by the LLM classifier.
func HasExistingMITRE(alert *models.AlertDef) bool {
	techs, _ := extractMITREFromLabels(alert.Labels)
	if len(techs) > 0 {
		return true
	}
	return len(extractTechniqueIDs(alert.Description)) > 0
}

// IsSecurityAlert is the exported version of isSecurityAlert for use outside this package.
func IsSecurityAlert(alert *models.AlertDef) bool {
	return isSecurityAlert(alert)
}

func extractLuceneQuery(typeDef map[string]any) string {
	if typeDef == nil {
		return ""
	}
	// Navigate: logsFilter.simpleFilter.luceneQuery
	logsFilter, _ := typeDef["logsFilter"].(map[string]any)
	if logsFilter == nil {
		return ""
	}
	simpleFilter, _ := logsFilter["simpleFilter"].(map[string]any)
	if simpleFilter == nil {
		return ""
	}
	q, _ := simpleFilter["luceneQuery"].(string)
	return q
}

// ── Keyword Matching ─────────────────────────────────────────────

func matchKeywords(corpus string, keywords map[string]string) []string {
	seen := make(map[string]bool)
	var results []string
	for keyword, category := range keywords {
		if !seen[category] && strings.Contains(corpus, keyword) {
			seen[category] = true
			results = append(results, category)
		}
	}
	return results
}

func appendTypeConditions(conditions []string, alertType string) []string {
	seen := make(map[string]bool)
	for _, c := range conditions {
		seen[c] = true
	}
	add := func(c string) {
		if !seen[c] {
			seen[c] = true
			conditions = append(conditions, c)
		}
	}
	switch alertType {
	case "logs_anomaly", "metric_anomaly":
		add("anomaly")
	case "logs_new_value":
		add("new_value")
	case "logs_threshold", "metric_threshold", "tracing_threshold":
		add("threshold")
	case "logs_ratio_threshold":
		add("ratio")
		add("threshold")
	case "logs_time_relative_threshold":
		add("time_relative")
		add("threshold")
	case "logs_unique_count":
		add("unique_count")
	case "flow":
		add("sequence")
	}
	return conditions
}

// ── Time Window ──────────────────────────────────────────────────

func extractTimeWindow(typeDef map[string]any) string {
	if typeDef == nil {
		return ""
	}
	// Search recursively for timeWindow / logsTimeWindowSpecificValue
	return findTimeWindow(typeDef)
}

func findTimeWindow(m map[string]any) string {
	for key, val := range m {
		switch key {
		case "logsTimeWindowSpecificValue":
			if s, ok := val.(string); ok {
				return normalizeTimeWindowEnum(s)
			}
		case "timeWindow":
			if sub, ok := val.(map[string]any); ok {
				if tw := findTimeWindow(sub); tw != "" {
					return tw
				}
			}
		}
		// Recurse into nested maps and arrays
		switch v := val.(type) {
		case map[string]any:
			if tw := findTimeWindow(v); tw != "" {
				return tw
			}
		case []any:
			for _, item := range v {
				if sub, ok := item.(map[string]any); ok {
					if tw := findTimeWindow(sub); tw != "" {
						return tw
					}
				}
			}
		}
	}
	return ""
}

func normalizeTimeWindowEnum(s string) string {
	s = strings.ToUpper(s)
	// Handle: LOGS_TIME_WINDOW_VALUE_MINUTES_30, LOGS_TIME_WINDOW_VALUE_HOURS_12
	replacements := map[string]string{
		"LOGS_TIME_WINDOW_VALUE_MINUTES_1":  "1m",
		"LOGS_TIME_WINDOW_VALUE_MINUTES_5":  "5m",
		"LOGS_TIME_WINDOW_VALUE_MINUTES_10": "10m",
		"LOGS_TIME_WINDOW_VALUE_MINUTES_15": "15m",
		"LOGS_TIME_WINDOW_VALUE_MINUTES_20": "20m",
		"LOGS_TIME_WINDOW_VALUE_MINUTES_30": "30m",
		"LOGS_TIME_WINDOW_VALUE_HOURS_1":    "1h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_2":    "2h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_4":    "4h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_6":    "6h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_12":   "12h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_24":   "24h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_36":   "36h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_48":   "48h",
		"LOGS_TIME_WINDOW_VALUE_HOURS_72":   "72h",
	}
	if mapped, ok := replacements[s]; ok {
		return mapped
	}
	return fmt.Sprintf("%v", s)
}

// ── Helpers ──────────────────────────────────────────────────────

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
