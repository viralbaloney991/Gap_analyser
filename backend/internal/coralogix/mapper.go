package coralogix

import (
	"strings"

	"coralogix-alert-analyzer/internal/models"
)

// ScoredCandidate represents a MITRE technique candidate with a confidence score.
type ScoredCandidate struct {
	TechniqueID string
	Tactic      string
	Score       float64
	Category    string // "name", "description", or "type"
}

// TechniqueMapper maps an alert to scored MITRE ATT&CK technique candidates.
// Implement this interface to swap in an LLM-based mapper.
type TechniqueMapper interface {
	MapCandidates(alert *models.AlertDef) []ScoredCandidate
}

// DefaultMapper is the active mapper used by ExtractFeatures.
// Replace with an LLM-backed implementation for production use.
var DefaultMapper TechniqueMapper = &RuleBasedMapper{}

// RuleBasedMapper maps alerts to MITRE techniques using universal security
// behavior patterns — action verbs, target objects, and detection context.
// No customer-specific or provider-specific alert names are used.
type RuleBasedMapper struct{}

// MapCandidates analyzes an alert's name, description, lucene query, and type
// and returns scored technique candidates. Name and description are evaluated
// separately with different confidence scores.
func (m *RuleBasedMapper) MapCandidates(alert *models.AlertDef) []ScoredCandidate {
	nameLower := strings.ToLower(alert.Name)
	descLower := strings.ToLower(alert.Description)
	lucene := strings.ToLower(extractLuceneQuery(alert.TypeDef))

	// Name corpus: alert name + lucene query (both author-chosen, high signal)
	nameCorpus := nameLower + " " + lucene
	// Description corpus: description text only (verbose, noisier)
	descCorpus := descLower

	var candidates []ScoredCandidate

	collectAll := func(rules []mappingRule, corpus string, score float64, category string, isDesc bool) {
		for _, rule := range rules {
			if isDesc && rule.nameOnly {
				continue
			}
			if rule.matches(corpus) {
				for i, tid := range rule.techniques {
					tactic := ruleTactic(rule, i)
					candidates = append(candidates, ScoredCandidate{tid, tactic, score, category})
				}
			}
		}
	}

	collectFirst := func(rules []mappingRule, corpus string, score float64, category string, isDesc bool) {
		for _, rule := range rules {
			if isDesc && rule.nameOnly {
				continue
			}
			if rule.matches(corpus) {
				for i, tid := range rule.techniques {
					tactic := ruleTactic(rule, i)
					candidates = append(candidates, ScoredCandidate{tid, tactic, score, category})
				}
				return // first match wins
			}
		}
	}

	// Detection pattern rules — all matching rules fire
	collectAll(detectionPatternRules, nameCorpus, 3.0, "name", false)
	collectAll(detectionPatternRules, descCorpus, 1.5, "description", true)

	// Action+target rules — first match wins, per corpus independently
	collectFirst(actionTargetRules, nameCorpus, 2.0, "name", false)
	collectFirst(actionTargetRules, descCorpus, 1.0, "description", true)

	// Alert type fallback — low confidence
	fullCorpus := nameLower + " " + descLower
	t, ta := alertTypeFallback(alert.AlertType, fullCorpus)
	for i, tid := range t {
		tactic := ""
		if i < len(ta) {
			tactic = ta[i]
		}
		candidates = append(candidates, ScoredCandidate{tid, tactic, 1.0, "type"})
	}

	return candidates
}

// ruleTactic extracts the tactic for the i-th technique in a rule.
func ruleTactic(rule mappingRule, i int) string {
	if i < len(rule.tactics) {
		return rule.tactics[i]
	}
	if len(rule.tactics) > 0 {
		return rule.tactics[0]
	}
	return ""
}

// ── Rule types ───────────────────────────────────────────────────

type mappingRule struct {
	// ALL of `allOf` must be present AND at least one of `anyOf` must be present.
	// If anyOf is empty, allOf alone is sufficient.
	allOf      []string
	anyOf      []string
	noneOf     []string // exclusions
	techniques []string
	tactics    []string
	nameOnly   bool // if true, only match against name corpus, skip description
}

func (r *mappingRule) matches(corpus string) bool {
	for _, term := range r.allOf {
		if !strings.Contains(corpus, term) {
			return false
		}
	}
	if len(r.anyOf) > 0 {
		found := false
		for _, term := range r.anyOf {
			if strings.Contains(corpus, term) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, term := range r.noneOf {
		if strings.Contains(corpus, term) {
			return false
		}
	}
	return true
}

// ── Detection pattern rules (high confidence) ───────────────────
// Universal patterns that map to MITRE regardless of data source.

var detectionPatternRules = []mappingRule{
	// ── Credential Access ────────────────────────────────────────
	{
		allOf: []string{}, anyOf: []string{"brute force", "brute-force", "bruteforce", "credential stuffing", "password spray"},
		techniques: []string{"T1110"}, tactics: []string{"credential-access"},
	},
	{
		allOf: []string{}, anyOf: []string{"impossible travel", "impossible_travel", "geo anomaly", "geolocation anomaly"},
		techniques: []string{"T1078"}, tactics: []string{"initial-access"},
	},
	{
		allOf: []string{}, anyOf: []string{"mfa fatigue", "mfa bombing", "push bombing"},
		techniques: []string{"T1621"}, tactics: []string{"credential-access"},
	},
	{
		allOf: []string{}, anyOf: []string{"mfa bypass", "mfa disabled", "disabled mfa", "deactivate mfa", "deactivate okta mfa", "mfa removed", "mfa reset"},
		techniques: []string{"T1556"}, tactics: []string{"persistence"},
	},
	{
		allOf: []string{}, anyOf: []string{"credential dump", "credential harvest", "password dump", "mimikatz"},
		techniques: []string{"T1003"}, tactics: []string{"credential-access"},
	},
	{
		allOf: []string{}, anyOf: []string{"golden ticket", "silver ticket", "kerberoast"},
		techniques: []string{"T1558"}, tactics: []string{"credential-access"},
	},
	{
		anyOf: []string{"token theft", "token replay", "stolen token", "session hijack", "cookie theft"},
		techniques: []string{"T1539"}, tactics: []string{"credential-access"},
	},
	{
		anyOf: []string{"consent grant", "oauth abuse", "illicit consent", "oauth app"},
		techniques: []string{"T1528"}, tactics: []string{"credential-access"},
	},
	{
		anyOf: []string{"unsecured credential", "exposed secret", "secret exposed", "credential in", "hardcoded password", "key exposed"},
		techniques: []string{"T1552"}, tactics: []string{"credential-access"},
	},

	// ── Initial Access / Valid Accounts ──────────────────────────
	{
		anyOf: []string{"failed login", "failed sign-in", "failed authentication", "login failure", "authentication failure"},
		noneOf: []string{"brute force", "brute-force"},
		techniques: []string{"T1078"}, tactics: []string{"initial-access"},
	},
	{
		anyOf: []string{"successful login", "successful sign-in", "successful authentication", "user login", "user logged in"},
		techniques: []string{"T1078"}, tactics: []string{"initial-access"},
	},
	{
		anyOf: []string{"phishing", "spearphishing", "spear-phishing"},
		techniques: []string{"T1566"}, tactics: []string{"initial-access"},
	},
	{
		anyOf: []string{"malicious link", "malicious attachment", "suspicious email"},
		techniques: []string{"T1566"}, tactics: []string{"initial-access"},
	},
	{
		anyOf:      []string{"exploit public", "remote code execution", "rce attack", "rce detected", "cve-"},
		techniques: []string{"T1190"}, tactics: []string{"initial-access"},
		nameOnly:   true,
	},
	{
		allOf:      []string{"exploit"},
		noneOf:     []string{"scan", "assessment", "data protection", "data exfil"},
		anyOf:      []string{"public", "application", "web", "server", "http", "api"},
		techniques: []string{"T1190"}, tactics: []string{"initial-access"},
		nameOnly:   true,
	},

	// ── Persistence ──────────────────────────────────────────────
	{
		anyOf: []string{"account creat", "user creat", "new user", "new account", "user added", "account added"},
		techniques: []string{"T1136"}, tactics: []string{"persistence"},
	},
	{
		anyOf: []string{"role change", "permission change", "policy change", "role assign", "privilege grant",
			"admin added", "added to admin", "admin group", "role elevat", "permission grant",
			"access key creat", "api key creat", "token creat", "service account creat"},
		techniques: []string{"T1098"}, tactics: []string{"persistence"},
	},
	{
		anyOf: []string{"webhook creat", "webhook modif", "integration added", "app install",
			"application install", "connector added"},
		techniques: []string{"T1098"}, tactics: []string{"persistence"},
	},
	{
		anyOf: []string{"scheduled task", "cron job", "automation creat", "lambda creat", "function creat"},
		techniques: []string{"T1053"}, tactics: []string{"persistence"},
	},
	{
		anyOf: []string{"web shell", "backdoor", "implant"},
		techniques: []string{"T1505.003"}, tactics: []string{"persistence"},
	},
	{
		anyOf: []string{"email forward", "mail forward", "inbox rule", "mailbox rule", "mail rule"},
		techniques: []string{"T1114.003"}, tactics: []string{"collection"},
	},

	// ── Privilege Escalation ─────────────────────────────────────
	{
		anyOf: []string{"privilege escalat", "privesc", "elevation of privilege", "assume role",
			"role chain", "impersonat"},
		techniques: []string{"T1548"}, tactics: []string{"privilege-escalation"},
	},
	{
		anyOf: []string{"container escape", "escape to host", "pod escape"},
		techniques: []string{"T1611"}, tactics: []string{"privilege-escalation"},
	},
	{
		anyOf: []string{"exec into pod", "exec into container", "kubectl exec", "container exec"},
		techniques: []string{"T1609"}, tactics: []string{"execution"},
	},

	// ── Defense Evasion ──────────────────────────────────────────
	{
		anyOf: []string{"log delet", "trail delet", "audit delet", "logging dis", "trail dis",
			"audit dis", "log tamper", "monitoring dis", "cloudtrail stop",
			"log clear", "event log clear", "evidence delet"},
		techniques: []string{"T1070"}, tactics: []string{"defense-evasion"},
	},
	{
		anyOf: []string{"security group change", "security group modif", "firewall rule change",
			"firewall rule modif", "firewall rule delet", "network acl", "nacl change",
			"inbound rule", "waf delet", "waf dis", "waf modif"},
		techniques: []string{"T1562"}, tactics: []string{"defense-evasion"},
	},
	{
		anyOf: []string{"guard duty", "guardduty", "security hub", "securityhub", "config rule",
			"alarm delet", "alarm dis", "alert dis", "detection dis"},
		allOf: []string{}, noneOf: []string{},
		techniques: []string{"T1562"}, tactics: []string{"defense-evasion"},
	},
	{
		anyOf: []string{"obfuscat", "encoded command", "base64", "masquerad"},
		techniques: []string{"T1027"}, tactics: []string{"defense-evasion"},
	},
	{
		anyOf: []string{"public access", "public exposure", "publicly accessible", "public block removed",
			"made public", "public acl"},
		techniques: []string{"T1562"}, tactics: []string{"defense-evasion"},
	},

	// ── Discovery ────────────────────────────────────────────────
	{
		anyOf: []string{"enumerat", "account discover", "user discover", "group discover",
			"permission discover", "list user", "list account", "list role",
			"describe instance", "list bucket"},
		noneOf: []string{"creat", "delet", "modif"},
		techniques: []string{"T1087"}, tactics: []string{"discovery"},
	},
	{
		anyOf: []string{"port scan", "network scan", "vulnerability scan", "reconnaissance", "recon scan"},
		techniques: []string{"T1046"}, tactics: []string{"discovery"},
	},

	// ── Lateral Movement ─────────────────────────────────────────
	{
		anyOf: []string{"lateral movement", "lateral_movement"},
		techniques: []string{"T1021"}, tactics: []string{"lateral-movement"},
	},
	{
		anyOf: []string{"rdp", "remote desktop", "ssh brute", "ssh login"},
		noneOf: []string{"firewall", "security group"},
		techniques: []string{"T1021"}, tactics: []string{"lateral-movement"},
	},

	// ── Collection / Exfiltration ────────────────────────────────
	{
		anyOf: []string{"data exfil", "exfiltrat", "data transfer", "large download", "bulk download",
			"mass download", "unusual download"},
		techniques: []string{"T1041"}, tactics: []string{"exfiltration"},
	},
	{
		anyOf: []string{"file delet", "data delet", "bucket delet", "resource delet",
			"table delet", "database delet", "topic delet", "queue delet",
			"instance terminat", "cluster delet"},
		techniques: []string{"T1485"}, tactics: []string{"impact"},
	},
	{
		anyOf: []string{"data access", "sensitive data", "classified data", "pii access",
			"call detail", "recording access"},
		noneOf: []string{"block", "prevent"},
		techniques: []string{"T1530"}, tactics: []string{"collection"},
	},
	{
		anyOf: []string{"dns tunnel", "dns exfil", "dns over http", "doh"},
		techniques: []string{"T1048"}, tactics: []string{"exfiltration"},
	},

	// ── Execution ────────────────────────────────────────────────
	{
		anyOf: []string{"command execut", "script execut", "powershell", "bash command",
			"code execut", "remote execut"},
		techniques: []string{"T1059"}, tactics: []string{"execution"},
	},
	{
		anyOf: []string{"malware", "ransomware", "trojan", "virus detect", "malicious file",
			"malicious process", "threat detect"},
		techniques: []string{"T1204"}, tactics: []string{"execution"},
	},

	// ── Impact ───────────────────────────────────────────────────
	{
		anyOf: []string{"ransomware", "encrypt", "ransom"},
		noneOf: []string{"ssl", "tls", "certificate"},
		techniques: []string{"T1486"}, tactics: []string{"impact"},
	},
	{
		anyOf: []string{"denial of service", "dos attack", "ddos", "resource hijack", "cryptomin"},
		techniques: []string{"T1498"}, tactics: []string{"impact"},
	},
	{
		anyOf: []string{"defac", "deface"},
		techniques: []string{"T1491"}, tactics: []string{"impact"},
	},

	// ── Command and Control ──────────────────────────────────────
	{
		anyOf: []string{"c2 beacon", "command and control", "c&c", "callback", "beaconing"},
		techniques: []string{"T1071"}, tactics: []string{"command-and-control"},
	},

	// ── Kubernetes / Container Security ─────────────────────────
	{
		anyOf: []string{"kubernetes privilege", "k8s privilege", "cluster role", "clusterrole",
			"pod privilege", "privileged container", "privileged pod", "hostpath mount"},
		techniques: []string{"T1611"}, tactics: []string{"privilege-escalation"},
	},
	{
		anyOf: []string{"deploy container", "container deploy", "new container", "pod creat"},
		techniques: []string{"T1610"}, tactics: []string{"execution"},
	},
	{
		anyOf: []string{"kubernetes secret", "k8s secret", "secret access"},
		noneOf: []string{"creat"},
		techniques: []string{"T1552"}, tactics: []string{"credential-access"},
	},

	// ── MFA / Authentication ────────────────────────────────────
	{
		anyOf: []string{"mfa challeng", "mfa denied", "mfa fail", "authenticator", "verification fail"},
		noneOf: []string{"bypass", "fatigue", "bombing"},
		techniques: []string{"T1078"}, tactics: []string{"initial-access"},
	},

	// ── SaaS / Session Security ─────────────────────────────────
	{
		anyOf: []string{"session hijack", "session takeover", "session anomal"},
		techniques: []string{"T1563"}, tactics: []string{"lateral-movement"},
	},

	// ── File / Document Operations ──────────────────────────────
	{
		anyOf: []string{"file share", "file sharing", "external share", "shared externally",
			"sharing permission", "share link"},
		techniques: []string{"T1567"}, tactics: []string{"exfiltration"},
	},

	// ── Cloud Infrastructure ────────────────────────────────────
	{
		anyOf: []string{"snapshot", "ami creat", "image creat", "disk export", "vm export"},
		techniques: []string{"T1578"}, tactics: []string{"defense-evasion"},
	},
	{
		anyOf: []string{"vpc change", "vpc modif", "subnet change", "route table", "transit gateway",
			"vpc peering", "network interface"},
		noneOf: []string{"discover", "list"},
		techniques: []string{"T1578"}, tactics: []string{"defense-evasion"},
	},

	// ── Monitoring / Health (no-logs alerts) ────────────────────
	{
		anyOf: []string{"no logs", "no-logs", "log gap", "missing log", "agent offline",
			"sensor offline", "health check fail", "heartbeat miss"},
		techniques: []string{"T1562"}, tactics: []string{"defense-evasion"},
	},

	// ── DNS / Network ───────────────────────────────────────────
	{
		anyOf: []string{"dns query", "dns request", "domain request", "domain query",
			"suspicious domain", "malicious domain", "dga"},
		noneOf: []string{"tunnel", "exfil"},
		techniques: []string{"T1071"}, tactics: []string{"command-and-control"},
	},

	// ── WAF / Exploit Detection ─────────────────────────────────
	{
		allOf:  []string{"waf"},
		anyOf:  []string{"vulnerability", "attack", "injection", "xss", "sqli", "common vulnerability"},
		noneOf: []string{"delet", "dis", "modif", "ddos", "brute"},
		techniques: []string{"T1190"}, tactics: []string{"initial-access"},
		nameOnly: true,
	},

	// ── Threat Intelligence / IOC ───────────────────────────────
	{
		anyOf: []string{"malicious ip", "malicious url", "malicious domain", "malicious hash",
			"threat intel", "ioc detected", "ioc match", "indicator of compromise",
			"malicious fingerprint"},
		techniques: []string{"T1071"}, tactics: []string{"command-and-control"},
	},

	// ── Backup Code / Login Anomaly ─────────────────────────────
	{
		anyOf: []string{"backup code", "recovery code"},
		allOf: []string{},
		techniques: []string{"T1078"}, tactics: []string{"initial-access"},
	},

	// ── Suspected Threat / Generic Vendor Threat ────────────────
	{
		anyOf: []string{"suspected threat", "threat occurred", "suspicious activity"},
		techniques: []string{"T1078"}, tactics: []string{"initial-access"},
	},

	// ── Exec into Pod/Container (catch "exec into" pattern) ─────
	{
		anyOf: []string{"exec into pod", "exec into container", "exec into a pod",
			"kubectl exec", "container exec", "sub resource has been exec"},
		techniques: []string{"T1609"}, tactics: []string{"execution"},
	},

	// ── Data Unload / Export ────────────────────────────────────
	{
		anyOf: []string{"unload data", "data unload", "export data", "data export",
			"copy to external", "unauthorized location"},
		techniques: []string{"T1041"}, tactics: []string{"exfiltration"},
	},

	// ── File Deletion in Bulk ───────────────────────────────────
	{
		anyOf: []string{"files were moved to the trash", "bulk delet", "mass delet",
			"files moved to trash", "files deleted"},
		techniques: []string{"T1485"}, tactics: []string{"impact"},
	},

	// ── Policy Change / Disable ─────────────────────────────────
	{
		anyOf: []string{"policy creat", "policy modif", "policy dis", "policy delet",
			"policy reorder", "policy update", "policy change"},
		noneOf: []string{"password policy"},
		techniques: []string{"T1562"}, tactics: []string{"defense-evasion"},
	},

	// ── Certificate / Key Exposure ──────────────────────────────
	{
		anyOf: []string{"certificate made", "cert made public", "pem certificate",
			"private to public", "key made public"},
		techniques: []string{"T1552"}, tactics: []string{"credential-access"},
	},

	// ── Public Exposure / Repository ────────────────────────────
	{
		anyOf: []string{"publicly available", "publicly accessible", "publicly exposed",
			"made public", "public access grant", "public permission"},
		noneOf: []string{"block", "removed"},
		techniques: []string{"T1213"}, tactics: []string{"collection"},
	},

	// ── Alert/Config Modification (tampering) ───────────────────
	{
		anyOf: []string{"alert has been modified", "alert modified", "alert delet",
			"detection rule modif", "rule disabled"},
		techniques: []string{"T1562"}, tactics: []string{"defense-evasion"},
	},
}

// ── Action + Target rules (medium confidence) ───────────────────
// Applied only when detection pattern rules don't match.
// Ordered by specificity — first match wins.

var actionTargetRules = []mappingRule{
	// Modify/Change + Auth/Security settings
	{
		anyOf:      []string{"password", "credential", "authentication", "login policy", "sign-in policy"},
		allOf:      []string{},
		techniques: []string{"T1556"}, tactics: []string{"persistence"},
	},
	// Delete + Logging/Monitoring
	{
		anyOf:  []string{"delet", "remov", "purg"},
		allOf:  []string{},
		noneOf: []string{},
		techniques: []string{"T1485"}, tactics: []string{"impact"},
	},
	// Create/Modify + Infrastructure
	{
		anyOf:      []string{"creat", "deploy", "launch", "provision"},
		allOf:      []string{},
		techniques: []string{"T1578"}, tactics: []string{"defense-evasion"},
	},
	// Access/View + Data
	{
		anyOf:      []string{"access", "view", "read", "get", "download", "retrieve", "list"},
		allOf:      []string{},
		techniques: []string{"T1530"}, tactics: []string{"collection"},
	},
	// Modify/Update + Configuration
	{
		anyOf:      []string{"modif", "updat", "chang", "edit", "set", "configur"},
		allOf:      []string{},
		techniques: []string{"T1098"}, tactics: []string{"persistence"},
	},
}

// alertTypeFallback maps alert types to broad technique categories when
// nothing else matched. Very conservative — better to have no mapping
// than a wrong one.
func alertTypeFallback(alertType, corpus string) ([]string, []string) {
	switch alertType {
	case "logs_anomaly", "metric_anomaly":
		// Anomaly detection on auth events → valid accounts
		if containsAny(corpus, "login", "auth", "sign-in", "session") {
			return []string{"T1078"}, []string{"initial-access"}
		}
		// Anomaly detection on data events → data access
		if containsAny(corpus, "data", "download", "transfer", "file") {
			return []string{"T1530"}, []string{"collection"}
		}
	case "logs_new_value":
		// New value = first-seen behavior, often reconnaissance or valid accounts
		if containsAny(corpus, "login", "auth", "user", "account") {
			return []string{"T1078"}, []string{"initial-access"}
		}
	case "flow":
		// Flow alerts are multi-stage → often attack chains
		// Too ambiguous for a single technique, skip
	}
	return nil, nil
}

func containsAny(corpus string, terms ...string) bool {
	for _, t := range terms {
		if strings.Contains(corpus, t) {
			return true
		}
	}
	return false
}
