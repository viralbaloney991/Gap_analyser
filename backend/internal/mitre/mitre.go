package mitre

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/models"
)

// TechniqueInfo describes a single MITRE ATT&CK Enterprise technique.
type TechniqueInfo struct {
	ID            string
	Name          string
	Tactic        string
	SubTechniques []string
}

// masterTechniqueList is a comprehensive mapping of MITRE ATT&CK Enterprise techniques.
// Covers all 14 tactics with 130+ techniques.
var masterTechniqueList = []TechniqueInfo{
	// ── Reconnaissance ──
	{ID: "T1595", Name: "Active Scanning", Tactic: "reconnaissance", SubTechniques: []string{"T1595.001", "T1595.002", "T1595.003"}},
	{ID: "T1592", Name: "Gather Victim Host Information", Tactic: "reconnaissance", SubTechniques: []string{"T1592.001", "T1592.002", "T1592.003", "T1592.004"}},
	{ID: "T1589", Name: "Gather Victim Identity Information", Tactic: "reconnaissance", SubTechniques: []string{"T1589.001", "T1589.002", "T1589.003"}},
	{ID: "T1590", Name: "Gather Victim Network Information", Tactic: "reconnaissance", SubTechniques: []string{"T1590.001", "T1590.002", "T1590.003", "T1590.004", "T1590.005", "T1590.006"}},
	{ID: "T1591", Name: "Gather Victim Org Information", Tactic: "reconnaissance", SubTechniques: []string{"T1591.001", "T1591.002", "T1591.003", "T1591.004"}},
	{ID: "T1598", Name: "Phishing for Information", Tactic: "reconnaissance", SubTechniques: []string{"T1598.001", "T1598.002", "T1598.003"}},
	{ID: "T1597", Name: "Search Closed Sources", Tactic: "reconnaissance", SubTechniques: []string{"T1597.001", "T1597.002"}},
	{ID: "T1596", Name: "Search Open Technical Databases", Tactic: "reconnaissance", SubTechniques: []string{"T1596.001", "T1596.002", "T1596.003", "T1596.004", "T1596.005"}},
	{ID: "T1593", Name: "Search Open Websites/Domains", Tactic: "reconnaissance", SubTechniques: []string{"T1593.001", "T1593.002", "T1593.003"}},
	{ID: "T1594", Name: "Search Victim-Owned Websites", Tactic: "reconnaissance"},

	// ── Resource Development ──
	{ID: "T1583", Name: "Acquire Infrastructure", Tactic: "resource-development", SubTechniques: []string{"T1583.001", "T1583.002", "T1583.003", "T1583.004", "T1583.005", "T1583.006", "T1583.007", "T1583.008"}},
	{ID: "T1584", Name: "Compromise Infrastructure", Tactic: "resource-development", SubTechniques: []string{"T1584.001", "T1584.002", "T1584.003", "T1584.004", "T1584.005", "T1584.006", "T1584.007"}},
	{ID: "T1586", Name: "Compromise Accounts", Tactic: "resource-development", SubTechniques: []string{"T1586.001", "T1586.002", "T1586.003"}},
	{ID: "T1585", Name: "Establish Accounts", Tactic: "resource-development", SubTechniques: []string{"T1585.001", "T1585.002", "T1585.003"}},
	{ID: "T1588", Name: "Obtain Capabilities", Tactic: "resource-development", SubTechniques: []string{"T1588.001", "T1588.002", "T1588.003", "T1588.004", "T1588.005", "T1588.006"}},
	{ID: "T1587", Name: "Develop Capabilities", Tactic: "resource-development", SubTechniques: []string{"T1587.001", "T1587.002", "T1587.003", "T1587.004"}},
	{ID: "T1608", Name: "Stage Capabilities", Tactic: "resource-development", SubTechniques: []string{"T1608.001", "T1608.002", "T1608.003", "T1608.004", "T1608.005", "T1608.006"}},

	// ── Initial Access ──
	{ID: "T1566", Name: "Phishing", Tactic: "initial-access", SubTechniques: []string{"T1566.001", "T1566.002", "T1566.003", "T1566.004"}},
	{ID: "T1190", Name: "Exploit Public-Facing Application", Tactic: "initial-access"},
	{ID: "T1189", Name: "Drive-by Compromise", Tactic: "initial-access"},
	{ID: "T1195", Name: "Supply Chain Compromise", Tactic: "initial-access", SubTechniques: []string{"T1195.001", "T1195.002", "T1195.003"}},
	{ID: "T1199", Name: "Trusted Relationship", Tactic: "initial-access"},
	{ID: "T1078", Name: "Valid Accounts", Tactic: "initial-access", SubTechniques: []string{"T1078.001", "T1078.002", "T1078.003", "T1078.004"}},
	{ID: "T1133", Name: "External Remote Services", Tactic: "initial-access"},
	{ID: "T1200", Name: "Hardware Additions", Tactic: "initial-access"},
	{ID: "T1091", Name: "Replication Through Removable Media", Tactic: "initial-access"},

	// ── Execution ──
	{ID: "T1059", Name: "Command and Scripting Interpreter", Tactic: "execution", SubTechniques: []string{"T1059.001", "T1059.002", "T1059.003", "T1059.004", "T1059.005", "T1059.006", "T1059.007", "T1059.008", "T1059.009"}},
	{ID: "T1204", Name: "User Execution", Tactic: "execution", SubTechniques: []string{"T1204.001", "T1204.002", "T1204.003"}},
	{ID: "T1047", Name: "Windows Management Instrumentation", Tactic: "execution"},
	{ID: "T1053", Name: "Scheduled Task/Job", Tactic: "execution", SubTechniques: []string{"T1053.002", "T1053.003", "T1053.005", "T1053.006", "T1053.007"}},
	{ID: "T1106", Name: "Native API", Tactic: "execution"},
	{ID: "T1569", Name: "System Services", Tactic: "execution", SubTechniques: []string{"T1569.001", "T1569.002"}},
	{ID: "T1609", Name: "Container Administration Command", Tactic: "execution"},
	{ID: "T1610", Name: "Deploy Container", Tactic: "execution"},
	{ID: "T1203", Name: "Exploitation for Client Execution", Tactic: "execution"},
	{ID: "T1559", Name: "Inter-Process Communication", Tactic: "execution", SubTechniques: []string{"T1559.001", "T1559.002", "T1559.003"}},

	// ── Persistence ──
	{ID: "T1547", Name: "Boot or Logon Autostart Execution", Tactic: "persistence", SubTechniques: []string{"T1547.001", "T1547.002", "T1547.003", "T1547.004", "T1547.005", "T1547.006", "T1547.009", "T1547.010", "T1547.012", "T1547.013", "T1547.014", "T1547.015"}},
	{ID: "T1037", Name: "Boot or Logon Initialization Scripts", Tactic: "persistence", SubTechniques: []string{"T1037.001", "T1037.002", "T1037.003", "T1037.004", "T1037.005"}},
	{ID: "T1505", Name: "Server Software Component", Tactic: "persistence", SubTechniques: []string{"T1505.001", "T1505.002", "T1505.003", "T1505.004", "T1505.005"}},
	{ID: "T1556", Name: "Modify Authentication Process", Tactic: "persistence", SubTechniques: []string{"T1556.001", "T1556.002", "T1556.003", "T1556.004", "T1556.005", "T1556.006", "T1556.007", "T1556.008"}},
	{ID: "T1136", Name: "Create Account", Tactic: "persistence", SubTechniques: []string{"T1136.001", "T1136.002", "T1136.003"}},
	{ID: "T1543", Name: "Create or Modify System Process", Tactic: "persistence", SubTechniques: []string{"T1543.001", "T1543.002", "T1543.003", "T1543.004"}},
	{ID: "T1546", Name: "Event Triggered Execution", Tactic: "persistence", SubTechniques: []string{"T1546.001", "T1546.002", "T1546.003", "T1546.004", "T1546.005", "T1546.008", "T1546.009", "T1546.010", "T1546.011", "T1546.012", "T1546.013", "T1546.014", "T1546.015", "T1546.016"}},
	{ID: "T1574", Name: "Hijack Execution Flow", Tactic: "persistence", SubTechniques: []string{"T1574.001", "T1574.002", "T1574.004", "T1574.005", "T1574.006", "T1574.007", "T1574.008", "T1574.009", "T1574.010", "T1574.011", "T1574.012", "T1574.013"}},
	{ID: "T1542", Name: "Pre-OS Boot", Tactic: "persistence", SubTechniques: []string{"T1542.001", "T1542.002", "T1542.003", "T1542.004", "T1542.005"}},
	{ID: "T1098", Name: "Account Manipulation", Tactic: "persistence", SubTechniques: []string{"T1098.001", "T1098.002", "T1098.003", "T1098.004", "T1098.005", "T1098.006"}},
	{ID: "T1137", Name: "Office Application Startup", Tactic: "persistence", SubTechniques: []string{"T1137.001", "T1137.002", "T1137.003", "T1137.004", "T1137.005", "T1137.006"}},
	// Multi-tactic: techniques that also appear under persistence
	{ID: "T1078", Name: "Valid Accounts", Tactic: "persistence", SubTechniques: []string{"T1078.001", "T1078.002", "T1078.003", "T1078.004"}},
	{ID: "T1053", Name: "Scheduled Task/Job", Tactic: "persistence", SubTechniques: []string{"T1053.002", "T1053.003", "T1053.005", "T1053.006", "T1053.007"}},

	// ── Privilege Escalation ──
	{ID: "T1548", Name: "Abuse Elevation Control Mechanism", Tactic: "privilege-escalation", SubTechniques: []string{"T1548.001", "T1548.002", "T1548.003", "T1548.004", "T1548.005"}},
	{ID: "T1134", Name: "Access Token Manipulation", Tactic: "privilege-escalation", SubTechniques: []string{"T1134.001", "T1134.002", "T1134.003", "T1134.004", "T1134.005"}},
	{ID: "T1068", Name: "Exploitation for Privilege Escalation", Tactic: "privilege-escalation"},
	{ID: "T1484", Name: "Domain Policy Modification", Tactic: "privilege-escalation", SubTechniques: []string{"T1484.001", "T1484.002"}},
	{ID: "T1611", Name: "Escape to Host", Tactic: "privilege-escalation"},
	// Multi-tactic: techniques that also appear under privilege-escalation
	{ID: "T1078", Name: "Valid Accounts", Tactic: "privilege-escalation", SubTechniques: []string{"T1078.001", "T1078.002", "T1078.003", "T1078.004"}},
	{ID: "T1547", Name: "Boot or Logon Autostart Execution", Tactic: "privilege-escalation", SubTechniques: []string{"T1547.001", "T1547.002", "T1547.003", "T1547.004", "T1547.005", "T1547.006", "T1547.009", "T1547.010", "T1547.012", "T1547.013", "T1547.014", "T1547.015"}},
	{ID: "T1543", Name: "Create or Modify System Process", Tactic: "privilege-escalation", SubTechniques: []string{"T1543.001", "T1543.002", "T1543.003", "T1543.004"}},
	{ID: "T1546", Name: "Event Triggered Execution", Tactic: "privilege-escalation", SubTechniques: []string{"T1546.001", "T1546.002", "T1546.003", "T1546.004", "T1546.005", "T1546.008", "T1546.009", "T1546.010", "T1546.011", "T1546.012", "T1546.013", "T1546.014", "T1546.015", "T1546.016"}},
	{ID: "T1574", Name: "Hijack Execution Flow", Tactic: "privilege-escalation", SubTechniques: []string{"T1574.001", "T1574.002", "T1574.004", "T1574.005", "T1574.006", "T1574.007", "T1574.008", "T1574.009", "T1574.010", "T1574.011", "T1574.012", "T1574.013"}},
	{ID: "T1055", Name: "Process Injection", Tactic: "privilege-escalation", SubTechniques: []string{"T1055.001", "T1055.002", "T1055.003", "T1055.004", "T1055.005", "T1055.008", "T1055.009", "T1055.011", "T1055.012", "T1055.013", "T1055.014", "T1055.015"}},
	{ID: "T1053", Name: "Scheduled Task/Job", Tactic: "privilege-escalation", SubTechniques: []string{"T1053.002", "T1053.003", "T1053.005", "T1053.006", "T1053.007"}},
	{ID: "T1098", Name: "Account Manipulation", Tactic: "privilege-escalation", SubTechniques: []string{"T1098.001", "T1098.002", "T1098.003", "T1098.004", "T1098.005", "T1098.006"}},

	// ── Defense Evasion ──
	{ID: "T1055", Name: "Process Injection", Tactic: "defense-evasion", SubTechniques: []string{"T1055.001", "T1055.002", "T1055.003", "T1055.004", "T1055.005", "T1055.008", "T1055.009", "T1055.011", "T1055.012", "T1055.013", "T1055.014", "T1055.015"}},
	{ID: "T1027", Name: "Obfuscated Files or Information", Tactic: "defense-evasion", SubTechniques: []string{"T1027.001", "T1027.002", "T1027.003", "T1027.004", "T1027.005", "T1027.006", "T1027.007", "T1027.008", "T1027.009", "T1027.010", "T1027.011", "T1027.012", "T1027.013"}},
	{ID: "T1070", Name: "Indicator Removal", Tactic: "defense-evasion", SubTechniques: []string{"T1070.001", "T1070.002", "T1070.003", "T1070.004", "T1070.005", "T1070.006", "T1070.007", "T1070.008", "T1070.009"}},
	{ID: "T1562", Name: "Impair Defenses", Tactic: "defense-evasion", SubTechniques: []string{"T1562.001", "T1562.002", "T1562.003", "T1562.004", "T1562.006", "T1562.007", "T1562.008", "T1562.009", "T1562.010", "T1562.011", "T1562.012"}},
	{ID: "T1036", Name: "Masquerading", Tactic: "defense-evasion", SubTechniques: []string{"T1036.001", "T1036.003", "T1036.004", "T1036.005", "T1036.006", "T1036.007", "T1036.008"}},
	{ID: "T1112", Name: "Modify Registry", Tactic: "defense-evasion"},
	{ID: "T1014", Name: "Rootkit", Tactic: "defense-evasion"},
	{ID: "T1564", Name: "Hide Artifacts", Tactic: "defense-evasion", SubTechniques: []string{"T1564.001", "T1564.002", "T1564.003", "T1564.004", "T1564.005", "T1564.006", "T1564.007", "T1564.008", "T1564.009", "T1564.010", "T1564.011"}},
	{ID: "T1218", Name: "System Binary Proxy Execution", Tactic: "defense-evasion", SubTechniques: []string{"T1218.001", "T1218.002", "T1218.003", "T1218.004", "T1218.005", "T1218.007", "T1218.008", "T1218.009", "T1218.010", "T1218.011", "T1218.012", "T1218.013", "T1218.014"}},
	{ID: "T1553", Name: "Subvert Trust Controls", Tactic: "defense-evasion", SubTechniques: []string{"T1553.001", "T1553.002", "T1553.003", "T1553.004", "T1553.005", "T1553.006"}},
	{ID: "T1480", Name: "Execution Guardrails", Tactic: "defense-evasion", SubTechniques: []string{"T1480.001"}},
	{ID: "T1221", Name: "Template Injection", Tactic: "defense-evasion"},
	{ID: "T1140", Name: "Deobfuscate/Decode Files or Information", Tactic: "defense-evasion"},
	{ID: "T1202", Name: "Indirect Command Execution", Tactic: "defense-evasion"},
	{ID: "T1535", Name: "Unused/Unsupported Cloud Regions", Tactic: "defense-evasion"},
	{ID: "T1550", Name: "Use Alternate Authentication Material", Tactic: "defense-evasion", SubTechniques: []string{"T1550.001", "T1550.002", "T1550.003", "T1550.004"}},
	{ID: "T1078", Name: "Valid Accounts", Tactic: "defense-evasion", SubTechniques: []string{"T1078.001", "T1078.002", "T1078.003", "T1078.004"}},
	{ID: "T1497", Name: "Virtualization/Sandbox Evasion", Tactic: "defense-evasion", SubTechniques: []string{"T1497.001", "T1497.002", "T1497.003"}},
	// Multi-tactic: techniques that also appear under defense-evasion
	{ID: "T1548", Name: "Abuse Elevation Control Mechanism", Tactic: "defense-evasion", SubTechniques: []string{"T1548.001", "T1548.002", "T1548.003", "T1548.004", "T1548.005"}},
	{ID: "T1134", Name: "Access Token Manipulation", Tactic: "defense-evasion", SubTechniques: []string{"T1134.001", "T1134.002", "T1134.003", "T1134.004", "T1134.005"}},
	{ID: "T1484", Name: "Domain Policy Modification", Tactic: "defense-evasion", SubTechniques: []string{"T1484.001", "T1484.002"}},

	// ── Credential Access ──
	{ID: "T1110", Name: "Brute Force", Tactic: "credential-access", SubTechniques: []string{"T1110.001", "T1110.002", "T1110.003", "T1110.004"}},
	{ID: "T1003", Name: "OS Credential Dumping", Tactic: "credential-access", SubTechniques: []string{"T1003.001", "T1003.002", "T1003.003", "T1003.004", "T1003.005", "T1003.006", "T1003.007", "T1003.008"}},
	{ID: "T1558", Name: "Steal or Forge Kerberos Tickets", Tactic: "credential-access", SubTechniques: []string{"T1558.001", "T1558.002", "T1558.003", "T1558.004"}},
	{ID: "T1528", Name: "Steal Application Access Token", Tactic: "credential-access"},
	{ID: "T1539", Name: "Steal Web Session Cookie", Tactic: "credential-access"},
	{ID: "T1552", Name: "Unsecured Credentials", Tactic: "credential-access", SubTechniques: []string{"T1552.001", "T1552.002", "T1552.003", "T1552.004", "T1552.005", "T1552.006", "T1552.007", "T1552.008"}},
	{ID: "T1621", Name: "Multi-Factor Authentication Request Generation", Tactic: "credential-access"},
	{ID: "T1056", Name: "Input Capture", Tactic: "credential-access", SubTechniques: []string{"T1056.001", "T1056.002", "T1056.003", "T1056.004"}},
	{ID: "T1557", Name: "Adversary-in-the-Middle", Tactic: "credential-access", SubTechniques: []string{"T1557.001", "T1557.002", "T1557.003"}},
	{ID: "T1111", Name: "Multi-Factor Authentication Interception", Tactic: "credential-access"},
	{ID: "T1187", Name: "Forced Authentication", Tactic: "credential-access"},
	{ID: "T1606", Name: "Forge Web Credentials", Tactic: "credential-access", SubTechniques: []string{"T1606.001", "T1606.002"}},

	// ── Discovery ──
	{ID: "T1087", Name: "Account Discovery", Tactic: "discovery", SubTechniques: []string{"T1087.001", "T1087.002", "T1087.003", "T1087.004"}},
	{ID: "T1046", Name: "Network Service Discovery", Tactic: "discovery"},
	{ID: "T1069", Name: "Permission Groups Discovery", Tactic: "discovery", SubTechniques: []string{"T1069.001", "T1069.002", "T1069.003"}},
	{ID: "T1082", Name: "System Information Discovery", Tactic: "discovery"},
	{ID: "T1083", Name: "File and Directory Discovery", Tactic: "discovery"},
	{ID: "T1057", Name: "Process Discovery", Tactic: "discovery"},
	{ID: "T1012", Name: "Query Registry", Tactic: "discovery"},
	{ID: "T1018", Name: "Remote System Discovery", Tactic: "discovery"},
	{ID: "T1016", Name: "System Network Configuration Discovery", Tactic: "discovery", SubTechniques: []string{"T1016.001", "T1016.002"}},
	{ID: "T1049", Name: "System Network Connections Discovery", Tactic: "discovery"},
	{ID: "T1033", Name: "System Owner/User Discovery", Tactic: "discovery"},
	{ID: "T1007", Name: "System Service Discovery", Tactic: "discovery"},
	{ID: "T1124", Name: "System Time Discovery", Tactic: "discovery"},
	{ID: "T1580", Name: "Cloud Infrastructure Discovery", Tactic: "discovery"},
	{ID: "T1526", Name: "Cloud Service Discovery", Tactic: "discovery"},
	{ID: "T1538", Name: "Cloud Service Dashboard", Tactic: "discovery"},
	{ID: "T1010", Name: "Application Window Discovery", Tactic: "discovery"},
	{ID: "T1217", Name: "Browser Information Discovery", Tactic: "discovery"},
	{ID: "T1613", Name: "Container and Resource Discovery", Tactic: "discovery"},
	{ID: "T1622", Name: "Debugger Evasion", Tactic: "discovery"},
	{ID: "T1654", Name: "Log Enumeration", Tactic: "discovery"},

	// ── Lateral Movement ──
	{ID: "T1021", Name: "Remote Services", Tactic: "lateral-movement", SubTechniques: []string{"T1021.001", "T1021.002", "T1021.003", "T1021.004", "T1021.005", "T1021.006", "T1021.007", "T1021.008"}},
	{ID: "T1550", Name: "Use Alternate Authentication Material", Tactic: "lateral-movement", SubTechniques: []string{"T1550.001", "T1550.002", "T1550.003", "T1550.004"}},
	{ID: "T1534", Name: "Internal Spearphishing", Tactic: "lateral-movement"},
	{ID: "T1080", Name: "Taint Shared Content", Tactic: "lateral-movement"},
	{ID: "T1570", Name: "Lateral Tool Transfer", Tactic: "lateral-movement"},
	{ID: "T1563", Name: "Remote Service Session Hijacking", Tactic: "lateral-movement", SubTechniques: []string{"T1563.001", "T1563.002"}},

	// ── Collection ──
	{ID: "T1114", Name: "Email Collection", Tactic: "collection", SubTechniques: []string{"T1114.001", "T1114.002", "T1114.003"}},
	{ID: "T1530", Name: "Data from Cloud Storage", Tactic: "collection"},
	{ID: "T1039", Name: "Data from Network Shared Drive", Tactic: "collection"},
	{ID: "T1025", Name: "Data from Removable Media", Tactic: "collection"},
	{ID: "T1119", Name: "Automated Collection", Tactic: "collection"},
	{ID: "T1115", Name: "Clipboard Data", Tactic: "collection"},
	{ID: "T1113", Name: "Screen Capture", Tactic: "collection"},
	{ID: "T1074", Name: "Data Staged", Tactic: "collection", SubTechniques: []string{"T1074.001", "T1074.002"}},
	{ID: "T1560", Name: "Archive Collected Data", Tactic: "collection", SubTechniques: []string{"T1560.001", "T1560.002", "T1560.003"}},
	{ID: "T1185", Name: "Browser Session Hijacking", Tactic: "collection"},
	{ID: "T1557", Name: "Adversary-in-the-Middle", Tactic: "collection", SubTechniques: []string{"T1557.001", "T1557.002", "T1557.003"}},

	// ── Command and Control ──
	{ID: "T1071", Name: "Application Layer Protocol", Tactic: "command-and-control", SubTechniques: []string{"T1071.001", "T1071.002", "T1071.003", "T1071.004"}},
	{ID: "T1105", Name: "Ingress Tool Transfer", Tactic: "command-and-control"},
	{ID: "T1573", Name: "Encrypted Channel", Tactic: "command-and-control", SubTechniques: []string{"T1573.001", "T1573.002"}},
	{ID: "T1572", Name: "Protocol Tunneling", Tactic: "command-and-control"},
	{ID: "T1090", Name: "Proxy", Tactic: "command-and-control", SubTechniques: []string{"T1090.001", "T1090.002", "T1090.003", "T1090.004"}},
	{ID: "T1219", Name: "Remote Access Software", Tactic: "command-and-control"},
	{ID: "T1095", Name: "Non-Application Layer Protocol", Tactic: "command-and-control"},
	{ID: "T1571", Name: "Non-Standard Port", Tactic: "command-and-control"},
	{ID: "T1132", Name: "Data Encoding", Tactic: "command-and-control", SubTechniques: []string{"T1132.001", "T1132.002"}},
	{ID: "T1568", Name: "Dynamic Resolution", Tactic: "command-and-control", SubTechniques: []string{"T1568.001", "T1568.002", "T1568.003"}},
	{ID: "T1008", Name: "Fallback Channels", Tactic: "command-and-control"},
	{ID: "T1104", Name: "Multi-Stage Channels", Tactic: "command-and-control"},
	{ID: "T1102", Name: "Web Service", Tactic: "command-and-control", SubTechniques: []string{"T1102.001", "T1102.002", "T1102.003"}},

	// ── Exfiltration ──
	{ID: "T1041", Name: "Exfiltration Over C2 Channel", Tactic: "exfiltration"},
	{ID: "T1048", Name: "Exfiltration Over Alternative Protocol", Tactic: "exfiltration", SubTechniques: []string{"T1048.001", "T1048.002", "T1048.003"}},
	{ID: "T1567", Name: "Exfiltration Over Web Service", Tactic: "exfiltration", SubTechniques: []string{"T1567.001", "T1567.002", "T1567.003", "T1567.004"}},
	{ID: "T1029", Name: "Scheduled Transfer", Tactic: "exfiltration"},
	{ID: "T1030", Name: "Data Transfer Size Limits", Tactic: "exfiltration"},
	{ID: "T1537", Name: "Transfer Data to Cloud Account", Tactic: "exfiltration"},
	{ID: "T1020", Name: "Automated Exfiltration", Tactic: "exfiltration", SubTechniques: []string{"T1020.001"}},
	{ID: "T1052", Name: "Exfiltration Over Physical Medium", Tactic: "exfiltration", SubTechniques: []string{"T1052.001"}},

	// ── Impact ──
	{ID: "T1486", Name: "Data Encrypted for Impact", Tactic: "impact"},
	{ID: "T1485", Name: "Data Destruction", Tactic: "impact"},
	{ID: "T1489", Name: "Service Stop", Tactic: "impact"},
	{ID: "T1490", Name: "Inhibit System Recovery", Tactic: "impact"},
	{ID: "T1491", Name: "Defacement", Tactic: "impact", SubTechniques: []string{"T1491.001", "T1491.002"}},
	{ID: "T1498", Name: "Network Denial of Service", Tactic: "impact", SubTechniques: []string{"T1498.001", "T1498.002"}},
	{ID: "T1496", Name: "Resource Hijacking", Tactic: "impact"},
	{ID: "T1495", Name: "Firmware Corruption", Tactic: "impact"},
	{ID: "T1531", Name: "Account Access Removal", Tactic: "impact"},
	{ID: "T1499", Name: "Endpoint Denial of Service", Tactic: "impact", SubTechniques: []string{"T1499.001", "T1499.002", "T1499.003", "T1499.004"}},
	{ID: "T1561", Name: "Disk Wipe", Tactic: "impact", SubTechniques: []string{"T1561.001", "T1561.002"}},
	{ID: "T1657", Name: "Financial Theft", Tactic: "impact"},
}

// techniqueByID is a lookup index built at init time.
var techniqueByID map[string]*TechniqueInfo

// tacticDisplayNames maps tactic slugs to human-readable names.
var tacticDisplayNames = map[string]string{
	"reconnaissance":      "Reconnaissance",
	"resource-development": "Resource Development",
	"initial-access":      "Initial Access",
	"execution":           "Execution",
	"persistence":         "Persistence",
	"privilege-escalation": "Privilege Escalation",
	"defense-evasion":     "Defense Evasion",
	"credential-access":   "Credential Access",
	"discovery":           "Discovery",
	"lateral-movement":    "Lateral Movement",
	"collection":          "Collection",
	"command-and-control": "Command and Control",
	"exfiltration":        "Exfiltration",
	"impact":              "Impact",
}

// tacticOrder defines the canonical ordering of MITRE tactics.
var tacticOrder = []string{
	"reconnaissance",
	"resource-development",
	"initial-access",
	"execution",
	"persistence",
	"privilege-escalation",
	"defense-evasion",
	"credential-access",
	"discovery",
	"lateral-movement",
	"collection",
	"command-and-control",
	"exfiltration",
	"impact",
}

func init() {
	techniqueByID = make(map[string]*TechniqueInfo, len(masterTechniqueList))
	for i := range masterTechniqueList {
		t := &masterTechniqueList[i]
		techniqueByID[t.ID] = t
	}
}

// GetTechnique returns information about a technique by its ID.
// Returns nil if the technique is not in the master list.
func GetTechnique(id string) *TechniqueInfo {
	// For sub-techniques, try parent first.
	if t, ok := techniqueByID[id]; ok {
		return t
	}
	if idx := strings.Index(id, "."); idx != -1 {
		return techniqueByID[id[:idx]]
	}
	return nil
}

// AnalyzeCoverage maps each alert's techniques to the master technique list
// and computes coverage metrics along with a Navigator v4.5 layer.
func AnalyzeCoverage(alerts []*models.AlertDef) *models.MITRECoverageResult {
	// Build a mapping of technique ID -> list of alert names covering it.
	techToAlerts := make(map[string][]string)

	for _, alert := range alerts {
		for _, tid := range alert.Features.Techniques {
			// Normalize: for sub-techniques, also credit the parent.
			baseTID := tid
			if idx := strings.Index(tid, "."); idx != -1 {
				baseTID = tid[:idx]
			}

			techToAlerts[tid] = appendUnique(techToAlerts[tid], alert.Name)
			if baseTID != tid {
				techToAlerts[baseTID] = appendUnique(techToAlerts[baseTID], alert.Name)
			}
		}
	}

	// Track which parent techniques have at least one scoped alert.
	// An alert is treated as scoped for all of its techniques — scope is alert-level, not per-technique.
	techHasScoped := make(map[string]bool)
	for _, alert := range alerts {
		if len(alert.Features.DataSources) == 0 && len(alert.Features.Entities) == 0 {
			continue
		}
		for _, tid := range alert.Features.Techniques {
			baseTID := tid
			if idx := strings.Index(tid, "."); idx != -1 {
				baseTID = tid[:idx]
			}
			techHasScoped[baseTID] = true
		}
	}

	// Compute per-technique scores and per-tactic coverage.
	tacticTotal := make(map[string]int)
	tacticCovered := make(map[string]int)
	tacticSubTotal := make(map[string]int)
	tacticSubCovered := make(map[string]int)

	type techniqueEntry struct {
		id      string
		name    string
		tactic  string
		score   int
		color   string
		comment string
	}

	var techniqueEntries []techniqueEntry

	for i := range masterTechniqueList {
		t := &masterTechniqueList[i]
		tacticTotal[t.Tactic]++

		alertNames := techToAlerts[t.ID]
		alertCount := len(alertNames)

		// Score: percentage capped at 100. Each alert adds 25% coverage credit.
		score := alertCount * 25
		if score > 100 {
			score = 100
		}

		color := scoreToColor(score)
		comment := ""
		if alertCount > 0 {
			tacticCovered[t.Tactic]++
			comment = fmt.Sprintf("Covered by %d alert(s): %s", alertCount, strings.Join(alertNames, ", "))
		}

		techniqueEntries = append(techniqueEntries, techniqueEntry{
			id:      t.ID,
			name:    t.Name,
			tactic:  t.Tactic,
			score:   score,
			color:   color,
			comment: comment,
		})

		// Also generate entries for sub-techniques.
		for _, subID := range t.SubTechniques {
			tacticSubTotal[t.Tactic]++

			subAlerts := techToAlerts[subID]
			subCount := len(subAlerts)
			subScore := subCount * 25
			if subScore > 100 {
				subScore = 100
			}
			subColor := scoreToColor(subScore)
			subComment := ""
			if subCount > 0 {
				tacticSubCovered[t.Tactic]++
				subComment = fmt.Sprintf("Covered by %d alert(s): %s", subCount, strings.Join(subAlerts, ", "))
			}
			subName := subTechniqueNames[subID]
			if subName == "" {
				subName = t.Name // fallback to parent name
			}
			techniqueEntries = append(techniqueEntries, techniqueEntry{
				id:      subID,
				name:    subName,
				tactic:  t.Tactic,
				score:   subScore,
				color:   subColor,
				comment: subComment,
			})
		}
	}

	// Build the Navigator layer techniques array.
	navTechniques := make([]map[string]any, 0, len(techniqueEntries))
	for _, te := range techniqueEntries {
		entry := map[string]any{
			"techniqueID":       te.id,
			"tactic":            te.tactic,
			"score":             te.score,
			"color":             te.color,
			"name":              te.name,
			"enabled":           true,
			"showSubtechniques": false,
		}
		if te.comment != "" {
			entry["comment"] = te.comment
		} else {
			entry["comment"] = ""
		}
		navTechniques = append(navTechniques, entry)
	}

	// Build tactic breakdown for the summary.
	tacticBreakdown := make(map[string]models.TacticCoverage)
	totalTechniques := 0
	coveredTechniques := 0
	totalSubTechniques := 0
	coveredSubTechniques := 0

	for _, tactic := range tacticOrder {
		total := tacticTotal[tactic]
		covered := tacticCovered[tactic]
		totalTechniques += total
		coveredTechniques += covered

		subTotal := tacticSubTotal[tactic]
		subCovered := tacticSubCovered[tactic]
		totalSubTechniques += subTotal
		coveredSubTechniques += subCovered

		pct := 0.0
		if total > 0 {
			pct = float64(covered) / float64(total) * 100
		}

		displayName := tacticDisplayNames[tactic]
		if displayName == "" {
			displayName = tactic
		}

		tacticBreakdown[tactic] = models.TacticCoverage{
			TacticName:  displayName,
			Total:       total,
			Covered:     covered,
			Percent:     round2(pct),
			TotalSubs:   subTotal,
			CoveredSubs: subCovered,
		}
	}

	overallPercent := 0.0
	if totalTechniques > 0 {
		overallPercent = float64(coveredTechniques) / float64(totalTechniques) * 100
	}

	// Build the Navigator v4.5 layer.
	navigatorLayer := map[string]any{
		"name":        "Coralogix Alert Coverage",
		"description": "MITRE ATT&CK coverage analysis generated from Coralogix alert definitions",
		"domain":      "enterprise-attack",
		"versions": map[string]any{
			"attack":    "14",
			"navigator": "4.9.0",
			"layer":     "4.5",
		},
		"filters": map[string]any{
			"platforms": []string{
				"Linux", "macOS", "Windows", "Network",
				"PRE", "Containers", "Office 365", "SaaS",
				"Google Workspace", "IaaS", "Azure AD",
			},
		},
		"sorting":    0,
		"layout": map[string]any{
			"layout":           "side",
			"aggregateFunction": "average",
			"showID":           true,
			"showName":         true,
			"showAggregateScores": false,
			"countUnscored":      false,
			"expandedSubtechniques": "none",
		},
		"hideDisabled": false,
		"techniques":   navTechniques,
		"gradient": map[string]any{
			"colors":   []string{"#ff0000", "#ff8c00", "#ffd700", "#00cc00"},
			"minValue": 0,
			"maxValue": 100,
		},
		"legendItems": []map[string]any{
			{
				"label": "No Coverage (0%)",
				"color": "#ff0000",
			},
			{
				"label": "Low Coverage (1-50%)",
				"color": "#ff8c00",
			},
			{
				"label": "Medium Coverage (51-75%)",
				"color": "#ffd700",
			},
			{
				"label": "High Coverage (76-100%)",
				"color": "#00cc00",
			},
		},
		"showTacticRowBackground": true,
		"tacticRowBackground":     "#dddddd",
		"selectTechniquesAcrossTactics": true,
		"selectSubtechniquesWithParent": false,
		"selectVisibleTechniques":       false,
		"metadata": []any{},
		"links":    []any{},
	}

	// Build technique-level coverage map (parent techniques only, deduplicated).
	techniqueCoverage := make(map[string]models.TechniqueCoverageEntry)
	seenTechID := make(map[string]bool)
	for i := range masterTechniqueList {
		t := &masterTechniqueList[i]
		if seenTechID[t.ID] {
			continue
		}
		seenTechID[t.ID] = true
		// alertCount reflects parent-level credit — sub-technique alerts are credited to the parent
		// via the baseTID normalization in the techToAlerts loop above.
		alertCount := len(techToAlerts[t.ID])
		techniqueCoverage[t.ID] = models.TechniqueCoverageEntry{
			Name:       t.Name,
			AlertCount: alertCount,
			Weak:       alertCount > 0 && !techHasScoped[t.ID],
		}
	}

	return &models.MITRECoverageResult{
		NavigatorLayer:    navigatorLayer,
		TechniqueCoverage: techniqueCoverage,
		Summary: models.MITRECoverageSummary{
			TotalTechniques:      totalTechniques,
			CoveredTechniques:    coveredTechniques,
			CoveragePercent:      round2(overallPercent),
			TotalSubTechniques:   totalSubTechniques,
			CoveredSubTechniques: coveredSubTechniques,
			TacticBreakdown:      tacticBreakdown,
		},
	}
}

// scoreToColor returns the appropriate color for a coverage score.
//
//	Red (#ff0000) for 0%, Orange (#ff8c00) for 1-50%,
//	Yellow (#ffd700) for 51-75%, Green (#00cc00) for 76-100%.
func scoreToColor(score int) string {
	switch {
	case score == 0:
		return "#ff0000"
	case score <= 50:
		return "#ff8c00"
	case score <= 75:
		return "#ffd700"
	default:
		return "#00cc00"
	}
}

// appendUnique appends s to the slice only if it is not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

// round2 rounds a float to 2 decimal places.
func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// GetAllTactics returns the ordered list of all 14 MITRE ATT&CK tactics.
func GetAllTactics() []string {
	result := make([]string, len(tacticOrder))
	copy(result, tacticOrder)
	return result
}

// GetTechniqueName returns the display name for a technique ID.
func GetTechniqueName(id string) string {
	for _, t := range masterTechniqueList {
		if t.ID == id {
			return t.Name
		}
	}
	return id
}

// GetTechniqueTactic returns the primary tactic for a technique ID.
func GetTechniqueTactic(id string) string {
	for _, t := range masterTechniqueList {
		if t.ID == id {
			return t.Tactic
		}
	}
	return ""
}

// UncoveredTechnique describes a technique with zero alert coverage.
type UncoveredTechnique struct {
	ID     string
	Name   string
	Tactic string
}

// GetUncoveredTechniques returns all parent techniques with 0 alert coverage.
// This is used to feed the LLM suggestion engine.
func GetUncoveredTechniques(alerts []*models.AlertDef) []UncoveredTechnique {
	// Build set of covered technique IDs (parent level)
	covered := make(map[string]bool)
	for _, alert := range alerts {
		for _, tid := range alert.Features.Techniques {
			// Credit parent technique
			baseTID := tid
			if idx := strings.Index(tid, "."); idx != -1 {
				baseTID = tid[:idx]
			}
			covered[baseTID] = true
		}
	}

	// Find uncovered from master list — deduplicate by technique ID
	// (multi-tactic entries create duplicates)
	seen := make(map[string]bool)
	var uncovered []UncoveredTechnique
	for _, t := range masterTechniqueList {
		if covered[t.ID] || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		uncovered = append(uncovered, UncoveredTechnique{
			ID:     t.ID,
			Name:   t.Name,
			Tactic: t.Tactic,
		})
	}
	return uncovered
}

// GetTechniquesByTactic returns all techniques for a given tactic slug.
func GetTechniquesByTactic(tactic string) []TechniqueInfo {
	var results []TechniqueInfo
	for _, t := range masterTechniqueList {
		if t.Tactic == tactic {
			results = append(results, t)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].ID < results[j].ID
	})
	return results
}

// GetAllTechniques returns a copy of the full master technique list.
func GetAllTechniques() []TechniqueInfo {
	result := make([]TechniqueInfo, len(masterTechniqueList))
	copy(result, masterTechniqueList)
	return result
}

type techniqueCompact struct {
	N string            `json:"n"`
	S map[string]string `json:"s,omitempty"`
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
				parts := strings.SplitN(subID, ".", 2)
				if len(parts) == 2 {
					suffix := parts[1]
					name := subTechniqueNames[subID]
					if name == "" {
						name = subID
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
