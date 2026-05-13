// frontend/src/data/mitre-catalog.ts

export interface MITRETactic {
  id: string;
  name: string;
  short: string;
  order: number;
}

export interface MITRETechnique {
  id: string;
  name: string;
  tactic: string;      // tactic ID e.g. 'TA0001'
  tacticName: string;  // denormalized e.g. 'Initial Access'
  tacticOrder: number; // 0..12 for kill-chain sort
  source: string;      // telemetry hint e.g. 'IdP / Cloud'
}

export const MITRE_TACTICS: MITRETactic[] = [
  { id: 'TA0043', name: 'Reconnaissance',       short: 'Recon',         order: 0  },
  { id: 'TA0001', name: 'Initial Access',       short: 'Initial Access', order: 1  },
  { id: 'TA0002', name: 'Execution',            short: 'Execution',      order: 2  },
  { id: 'TA0003', name: 'Persistence',          short: 'Persistence',    order: 3  },
  { id: 'TA0004', name: 'Privilege Escalation', short: 'Priv Esc',       order: 4  },
  { id: 'TA0005', name: 'Defense Evasion',      short: 'Evasion',        order: 5  },
  { id: 'TA0006', name: 'Credential Access',    short: 'Cred Access',    order: 6  },
  { id: 'TA0007', name: 'Discovery',            short: 'Discovery',      order: 7  },
  { id: 'TA0008', name: 'Lateral Movement',     short: 'Lateral',        order: 8  },
  { id: 'TA0009', name: 'Collection',           short: 'Collection',     order: 9  },
  { id: 'TA0011', name: 'Command and Control',  short: 'C2',             order: 10 },
  { id: 'TA0010', name: 'Exfiltration',         short: 'Exfiltration',   order: 11 },
  { id: 'TA0040', name: 'Impact',               short: 'Impact',         order: 12 },
];

const TACTIC_MAP = Object.fromEntries(MITRE_TACTICS.map(t => [t.id, t]));

const RAW_TECHNIQUES: Omit<MITRETechnique, 'tacticName' | 'tacticOrder'>[] = [
  // Reconnaissance
  { id: 'T1595',     name: 'Active Scanning',                          tactic: 'TA0043', source: 'Network'        },
  { id: 'T1595.002', name: 'Vulnerability Scanning',                   tactic: 'TA0043', source: 'Network'        },
  { id: 'T1592',     name: 'Gather Victim Host Information',           tactic: 'TA0043', source: 'Network / OSINT' },
  { id: 'T1598',     name: 'Phishing for Information',                 tactic: 'TA0043', source: 'Email Gateway'  },
  { id: 'T1596',     name: 'Search Open Technical Databases',          tactic: 'TA0043', source: 'Network / OSINT' },
  // Initial Access
  { id: 'T1078',     name: 'Valid Accounts',                        tactic: 'TA0001', source: 'IdP / Cloud'    },
  { id: 'T1078.004', name: 'Cloud Accounts',                        tactic: 'TA0001', source: 'CloudTrail'     },
  { id: 'T1190',     name: 'Exploit Public-Facing Application',     tactic: 'TA0001', source: 'WAF'            },
  { id: 'T1566',     name: 'Phishing',                              tactic: 'TA0001', source: 'Email Gateway'  },
  { id: 'T1133',     name: 'External Remote Services',              tactic: 'TA0001', source: 'VPN / RDP'      },
  // Execution
  { id: 'T1059',     name: 'Command and Scripting Interpreter',     tactic: 'TA0002', source: 'EDR'            },
  { id: 'T1204',     name: 'User Execution',                        tactic: 'TA0002', source: 'EDR'            },
  { id: 'T1610',     name: 'Deploy Container',                      tactic: 'TA0002', source: 'Cloud'          },
  // Persistence
  { id: 'T1098',     name: 'Account Manipulation',                  tactic: 'TA0003', source: 'IdP / Cloud'   },
  { id: 'T1098.001', name: 'Additional Cloud Credentials',          tactic: 'TA0003', source: 'CloudTrail'     },
  { id: 'T1136',     name: 'Create Account',                        tactic: 'TA0003', source: 'EDR / IdP'     },
  { id: 'T1547',     name: 'Boot/Logon Autostart',                  tactic: 'TA0003', source: 'EDR'            },
  { id: 'T1053',     name: 'Scheduled Task/Job',                    tactic: 'TA0003', source: 'EDR'            },
  // Privilege Escalation
  { id: 'T1548',     name: 'Abuse Elevation Control',               tactic: 'TA0004', source: 'EDR'            },
  { id: 'T1068',     name: 'Exploitation for Privilege Escalation', tactic: 'TA0004', source: 'EDR'            },
  { id: 'T1484',     name: 'Domain Policy Modification',            tactic: 'TA0004', source: 'AD'             },
  // Defense Evasion
  { id: 'T1027',     name: 'Obfuscated Files or Information',       tactic: 'TA0005', source: 'EDR'            },
  { id: 'T1070',     name: 'Indicator Removal',                     tactic: 'TA0005', source: 'EDR'            },
  { id: 'T1562',     name: 'Impair Defenses',                       tactic: 'TA0005', source: 'EDR'            },
  // Credential Access
  { id: 'T1110',     name: 'Brute Force',                           tactic: 'TA0006', source: 'IdP'            },
  { id: 'T1003',     name: 'OS Credential Dumping',                 tactic: 'TA0006', source: 'EDR'            },
  { id: 'T1555',     name: 'Credentials from Password Stores',      tactic: 'TA0006', source: 'EDR'            },
  // Discovery
  { id: 'T1087',     name: 'Account Discovery',                     tactic: 'TA0007', source: 'EDR / Cloud'   },
  { id: 'T1018',     name: 'Remote System Discovery',               tactic: 'TA0007', source: 'EDR'            },
  { id: 'T1538',     name: 'Cloud Service Dashboard',               tactic: 'TA0007', source: 'CloudTrail'     },
  // Lateral Movement
  { id: 'T1021',     name: 'Remote Services',                       tactic: 'TA0008', source: 'EDR / Network' },
  { id: 'T1550',     name: 'Use Alternate Authentication Material', tactic: 'TA0008', source: 'IdP'            },
  { id: 'T1570',     name: 'Lateral Tool Transfer',                 tactic: 'TA0008', source: 'Network'        },
  // Collection
  { id: 'T1005',     name: 'Data from Local System',                tactic: 'TA0009', source: 'EDR'            },
  { id: 'T1530',     name: 'Data from Cloud Storage',               tactic: 'TA0009', source: 'CloudTrail'     },
  { id: 'T1560',     name: 'Archive Collected Data',                tactic: 'TA0009', source: 'EDR'            },
  // C2
  { id: 'T1071',     name: 'Application Layer Protocol',            tactic: 'TA0011', source: 'Network'        },
  { id: 'T1573',     name: 'Encrypted Channel',                     tactic: 'TA0011', source: 'Network'        },
  { id: 'T1105',     name: 'Ingress Tool Transfer',                 tactic: 'TA0011', source: 'EDR'            },
  // Exfiltration
  { id: 'T1041',     name: 'Exfil Over C2 Channel',                 tactic: 'TA0010', source: 'Network'        },
  { id: 'T1567',     name: 'Exfil Over Web Service',                tactic: 'TA0010', source: 'Network'        },
  { id: 'T1567.002', name: 'Exfil to Cloud Storage',                tactic: 'TA0010', source: 'CloudTrail'     },
  // Impact
  { id: 'T1486',     name: 'Data Encrypted for Impact',             tactic: 'TA0040', source: 'EDR'            },
  { id: 'T1490',     name: 'Inhibit System Recovery',               tactic: 'TA0040', source: 'EDR'            },
  { id: 'T1485',     name: 'Data Destruction',                      tactic: 'TA0040', source: 'EDR'            },
];

export const MITRE_TECHNIQUES: MITRETechnique[] = RAW_TECHNIQUES.map(t => ({
  ...t,
  tacticName: TACTIC_MAP[t.tactic]?.name ?? t.tactic,
  tacticOrder: TACTIC_MAP[t.tactic]?.order ?? 99,
}));

// ── Mock fallback (deterministic, no LLM required) ──
// Used when the API call fails. Port of mockGenerate from builder-base.jsx.

import type { GenerationResult } from '../types';

const WINDOW_BY_TACTIC: Record<string, { w: string; why: string }> = {
  TA0043: { w: '1h',  why: 'Recon usually precedes access by an hour or less.' },
  TA0001: { w: '15m', why: 'Initial access events are immediate; correlate within 15 minutes.' },
  TA0002: { w: '5m',  why: 'Execution should follow access within minutes.' },
  TA0003: { w: '1h',  why: 'Persistence is typically established within an hour of access.' },
  TA0004: { w: '6h',  why: 'Privilege escalation often happens within a working session.' },
  TA0005: { w: '30m', why: 'Defense evasion clusters around execution and persistence.' },
  TA0006: { w: '15m', why: 'Credential access is usually rapid once execution succeeds.' },
  TA0007: { w: '6h',  why: 'Discovery commands run during the attacker reconnaissance window.' },
  TA0008: { w: '6h',  why: 'Lateral movement happens hours after initial foothold.' },
  TA0009: { w: '12h', why: 'Collection precedes exfiltration by hours.' },
  TA0011: { w: '24h', why: 'C2 channels persist as long as the attacker has a foothold.' },
  TA0010: { w: '24h', why: 'Exfiltration is the final stage; window covers staged extraction.' },
  TA0040: { w: '15m', why: 'Impact actions are tightly clustered once triggered.' },
};

const SEV_BY_TACTIC: Record<string, 'critical' | 'high' | 'medium' | 'low'> = {
  TA0001: 'high', TA0002: 'medium', TA0003: 'high', TA0004: 'high',
  TA0005: 'medium', TA0006: 'critical', TA0007: 'low', TA0008: 'high',
  TA0009: 'medium', TA0011: 'medium', TA0010: 'critical', TA0040: 'critical',
};

export function mockGenerate(selected: MITRETechnique[]): GenerationResult {
  const ordered = [...selected].sort((a, b) => a.tacticOrder - b.tacticOrder);

  const alerts = ordered.slice(0, 4).map((t, i) => ({
    name:         `Stage ${i + 1}: ${t.name}`,
    description:  `Detect ${t.name.toLowerCase()} activity (${t.tacticName}).`,
    techniqueId:  t.id,
    logic:        `Event matching ${t.id} pattern observed via ${t.source}.`,
    window:       WINDOW_BY_TACTIC[t.tactic]?.w ?? '1h',
    windowReason: WINDOW_BY_TACTIC[t.tactic]?.why ?? 'Standard correlation window for this stage.',
    source:       (t.source.split('/')[0] ?? 'EDR').trim(),
    severity:     SEV_BY_TACTIC[t.tactic] ?? 'medium' as const,
  }));

  const tacticIds = new Set(ordered.map(t => t.tactic));
  const findings: GenerationResult['validation']['findings'] = [];
  if (!tacticIds.has('TA0001') && tacticIds.size > 1) {
    findings.push({ level: 'warn', message: 'No Initial Access technique — chain starts mid-attack.' });
  }
  if (tacticIds.has('TA0010') && !tacticIds.has('TA0009')) {
    findings.push({ level: 'info', message: 'Exfiltration without Collection — consider adding T1530.' });
  }
  if (findings.length === 0) {
    findings.push({ level: 'info', message: 'Chain is sequenced correctly across the kill-chain.' });
  }
  const verdict = findings.some(f => f.level === 'error') ? 'invalid'
    : findings.some(f => f.level === 'warn') ? 'warnings' : 'ok';

  const dwellHours = ordered.length >= 4 ? 72 : ordered.length >= 3 ? 24 : 1;
  const corrWindow = dwellHours >= 72 ? '72h' : dwellHours >= 24 ? '24h' : '1h';

  return {
    validation: { verdict, findings },
    alerts,
    correlation: {
      name:     `Multi-stage chain: ${ordered.map(t => t.id).join(' → ')}`,
      logic:    alerts.map((a, i) => `Alert ${i + 1} (${a.techniqueId})`).join(' → ') + ` within ${corrWindow}`,
      window:   corrWindow,
      severity: alerts.some(a => a.severity === 'critical') ? 'critical' : 'high',
    },
  };
}
