import {
  useState, useEffect, useRef, useMemo,
  useLayoutEffect, useCallback, type RefObject,
} from 'react';
import type { AnalyzeResponse } from '../types';

// ── Prop types ──────────────────────────────────────────────────────────

interface Props {
  data: AnalyzeResponse;
  clientName: string;
  lookbackDays: number;
  onViewMitre: () => void;
}

// ── Internal types ──────────────────────────────────────────────────────

type Severity = 'critical' | 'high' | 'medium' | 'low';
type Coverage = 'strong' | 'partial' | 'none';

interface AlertRule {
  id: string;
  name: string;
  source: string;
  severity: Severity;
  tids: string[];
  count: number;
  noisePct: number;
  fpRate: number;
  lastSeenHrs: number;
  trend: number;
  owner: string;
  mttd: number;
  mttr: number;
  assets: number;
}

interface TechNode {
  id: string;
  name: string;
  tactic: string;
  tacticName: string;
  tacticShort: string;
  linkedCount: number;
  totalAlerts: number;
  coverage: Coverage;
}

interface TacticInfo { id: string; name: string; short: string; }

interface Posture {
  totalAlerts: number;
  critical: number; high: number; medium: number; low: number;
  techniquesCovered: number; techniquesTotal: number;
  strong: number; partial: number; gaps: number;
  avgFpRate: number;
}

interface BipartiteLayout {
  alertPos: Record<string, { x: number; y: number }>;
  techPos:  Record<string, { x: number; y: number }>;
  tacticBands: { id: string; name: string; short: string; y: number; h: number }[];
  leftX: number; rightX: number; topY: number; botY: number;
  W: number; H: number;
}

interface Vp { x: number; y: number; k: number; }

type Pick =
  | { type: 'alert'; data: AlertRule }
  | { type: 'tech';  data: TechNode  };

// ── Constants ───────────────────────────────────────────────────────────

const SEV_COLOR: Record<Severity, string> = {
  critical: '#dc2626', high: '#f97316', medium: '#eab308', low: '#94a3b8',
};
const COV_COLOR: Record<Coverage, string> = {
  strong: '#10b981', partial: '#eab308', none: '#475569',
};
const COV_LABEL: Record<Coverage, string> = {
  strong: 'Strong', partial: 'Partial', none: 'No coverage',
};

const TACTICS_ORDER: TacticInfo[] = [
  { id: 'reconnaissance',      name: 'Reconnaissance',        short: 'Recon'        },
  { id: 'initial-access',      name: 'Initial Access',        short: 'Initial Access'},
  { id: 'execution',           name: 'Execution',             short: 'Execution'    },
  { id: 'persistence',         name: 'Persistence',           short: 'Persistence'  },
  { id: 'privilege-escalation',name: 'Privilege Escalation',  short: 'Priv Esc'     },
  { id: 'defense-evasion',     name: 'Defense Evasion',       short: 'Def Evasion'  },
  { id: 'credential-access',   name: 'Credential Access',     short: 'Cred Access'  },
  { id: 'discovery',           name: 'Discovery',             short: 'Discovery'    },
  { id: 'lateral-movement',    name: 'Lateral Movement',      short: 'Lateral'      },
  { id: 'collection',          name: 'Collection',            short: 'Collection'   },
  { id: 'command-and-control', name: 'Command and Control',   short: 'C2'           },
  { id: 'exfiltration',        name: 'Exfiltration',          short: 'Exfiltration' },
  { id: 'impact',              name: 'Impact',                short: 'Impact'       },
];

const TACTIC_MAP: Record<string, TacticInfo> = Object.fromEntries(
  TACTICS_ORDER.map(t => [t.id, t])
);


// ── Helper utilities ────────────────────────────────────────────────────

function fmtNum(n: number): string {
  return n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n);
}

function deterministicN(seed: string, offset: number, min: number, max: number): number {
  let h = 5381 + offset;
  for (let i = 0; i < seed.length; i++) h = ((h << 5) + h) ^ seed.charCodeAt(i);
  return min + (Math.abs(h) % (max - min + 1));
}

function countToSev(count: number): Severity {
  if (count > 200) return 'critical';
  if (count > 50)  return 'high';
  if (count > 10)  return 'medium';
  return 'low';
}

function deriveSource(app: string, sub: string): string {
  const t = `${app} ${sub}`.toLowerCase();
  if (/aws|gcp|azure|cloud|s3|ec2/.test(t))    return 'Cloud';
  if (/network|firewall|palo|fortinet|cisco|dns|vpn/.test(t)) return 'Network';
  if (/email|gmail|mail|exchange|phish/.test(t)) return 'Email';
  if (/okta|idp|identity|auth|sso/.test(t))     return 'IdP';
  if (/waf|web.app/.test(t))                     return 'WAF';
  return 'EDR';
}


// ── Data builders ───────────────────────────────────────────────────────

function buildAlertRules(data: AnalyzeResponse): AlertRule[] {
  const techCov = data.mitre_coverage.technique_coverage ?? {};

  // Invert technique_coverage: alertName → Set<techniqueID>
  const alertToTids = new Map<string, Set<string>>();
  for (const [tid, entry] of Object.entries(techCov)) {
    for (const name of entry.alert_rules ?? []) {
      if (!alertToTids.has(name)) alertToTids.set(name, new Set());
      alertToTids.get(name)!.add(tid);
    }
  }

  const noiseNames = new Set((data.alert_insights.noise_alerts ?? []).map(n => n.name));
  return data.integrations
    .filter(int => int.alert_count > 0)
    .map((int, i) => {
      const isNoisy = noiseNames.has(int.name);
      const noisePct = isNoisy ? deterministicN(int.name, 9, 50, 90)
                               : deterministicN(int.name, 9, 5, 40);
      return {
        id: `int-${i}`,
        name: int.name,
        source: deriveSource(int.application, int.subsystem),
        severity: countToSev(int.alert_count),
        tids: [...(alertToTids.get(int.name) ?? [])],
        count: int.alert_count,
        noisePct,
        fpRate: Math.min(95, noisePct + deterministicN(int.name, 10, 0, 15)),
        lastSeenHrs: deterministicN(int.name, 1, 0, 72),
        trend: (deterministicN(int.name, 2, 0, 200) - 100) / 100,
        owner: ['SOC Tier 1', 'SOC Tier 2', 'IR Team', 'Cloud Sec'][deterministicN(int.name, 3, 0, 3)],
        mttd: deterministicN(int.name, 4, 2, 30),
        mttr: deterministicN(int.name, 5, 15, 240),
        assets: deterministicN(int.name, 6, 1, 18),
      };
    });
}

function buildTechNodes(data: AnalyzeResponse, alerts: AlertRule[]): TechNode[] {
  // Keep only base techniques (no sub-techniques like T1059.001).
  // Deduplicate: the same techniqueID can appear under multiple tactics in the
  // navigator layer. Keep the entry with the highest score so coverage is accurate.
  const seen = new Map<string, typeof data.mitre_coverage.navigator_layer.techniques[0]>();
  data.mitre_coverage.navigator_layer.techniques
    .filter(nt => !nt.techniqueID.includes('.'))
    .forEach(nt => {
      const existing = seen.get(nt.techniqueID);
      if (!existing || nt.score > existing.score) seen.set(nt.techniqueID, nt);
    });
  const navTechs = [...seen.values()];
  const techCov  = data.mitre_coverage.technique_coverage ?? {};

  return navTechs.map(nt => {
    const cov = techCov[nt.techniqueID];
    const linked = alerts.filter(a => a.tids.includes(nt.techniqueID));
    let coverage: Coverage;
    if (nt.score === 0) coverage = 'none';
    else if (nt.score <= 2) coverage = 'partial';
    else coverage = 'strong';
    const tac = TACTIC_MAP[nt.tactic] ?? { id: nt.tactic, name: nt.tactic, short: nt.tactic };
    return {
      id: nt.techniqueID,
      name: nt.name,
      tactic: nt.tactic,
      tacticName: tac.name,
      tacticShort: tac.short,
      linkedCount: cov ? (cov.alert_count > 0 ? linked.length || 1 : 0) : linked.length,
      totalAlerts: cov?.alert_count ?? linked.reduce((s, a) => s + a.count, 0),
      coverage,
    };
  });
}

function buildPosture(data: AnalyzeResponse, alerts: AlertRule[]): Posture {
  const { stats, mitre_coverage } = data;
  const s = mitre_coverage.summary;

  let critical = 0, high = 0, medium = 0, low = 0;
  alerts.forEach(a => {
    if (a.severity === 'critical') critical += a.count;
    else if (a.severity === 'high') high += a.count;
    else if (a.severity === 'medium') medium += a.count;
    else low += a.count;
  });

  const navTechs = mitre_coverage.navigator_layer.techniques;
  const strong  = navTechs.filter(t => t.score > 2).length;
  const partial = navTechs.filter(t => t.score > 0 && t.score <= 2).length;
  const covered = s.covered_techniques;
  const total   = Math.max(s.total_techniques, covered);

  const noiseAlerts = data.alert_insights.noise_alerts ?? [];
  const avgFpRate = alerts.length
    ? Math.round(alerts.reduce((s, a) => s + a.fpRate, 0) / alerts.length)
    : 0;

  return {
    totalAlerts: stats.total_alerts,
    critical, high, medium, low,
    techniquesCovered: covered,
    techniquesTotal: total,
    strong, partial,
    gaps: Math.max(0, total - covered),
    avgFpRate,
  };
}

// ── Layout algorithm ────────────────────────────────────────────────────

function buildBipartite(
  alerts: AlertRule[],
  techniques: TechNode[],
  W: number,
  H: number,
): BipartiteLayout {
  const leftX  = 240;
  const rightX = W - 260;
  const topY   = 80;
  const botY   = H - 80;

  const sorted = [...alerts].sort((a, b) => b.count - a.count);
  const aSpacing = (botY - topY) / Math.max(1, sorted.length - 1);
  const alertPos: Record<string, { x: number; y: number }> = {};
  sorted.forEach((a, i) => { alertPos[a.id] = { x: leftX, y: topY + i * aSpacing }; });

  const techByTactic = new Map<string, TechNode[]>(
    TACTICS_ORDER.map(t => [t.id, []])
  );
  techniques.forEach(t => {
    if (!techByTactic.has(t.tactic)) techByTactic.set(t.tactic, []);
    techByTactic.get(t.tactic)!.push(t);
  });

  // Use rows (2 techniques per row) to halve canvas height.
  const totalRows = TACTICS_ORDER.reduce((sum, tac) => {
    const list = techByTactic.get(tac.id) ?? [];
    return sum + Math.ceil(list.length / 2);
  }, 0);
  const techPos: Record<string, { x: number; y: number }> = {};
  const tacticBands: BipartiteLayout['tacticBands'] = [];
  let cursorY = topY;

  TACTICS_ORDER.forEach(tac => {
    const list = techByTactic.get(tac.id) ?? [];
    if (!list.length) return;
    const numRows = Math.ceil(list.length / 2);
    const bandH = (botY - topY) * (numRows / Math.max(totalRows, 1));
    const rowSpacing = Math.max(28, bandH / Math.max(1, numRows));
    tacticBands.push({ id: tac.id, name: tac.name, short: tac.short, y: cursorY, h: bandH });
    list.forEach((t, i) => {
      const col = i % 2;
      const row = Math.floor(i / 2);
      techPos[t.id] = { x: rightX + col * 120, y: cursorY + (row + 0.5) * rowSpacing };
    });
    cursorY += bandH;
  });

  return { alertPos, techPos, tacticBands, leftX, rightX, topY, botY, W, H };
}

// ── Viewport hook ───────────────────────────────────────────────────────

// Clamp translation so at least `margin` px of graph content stays on-screen.
// Without this, zooming into empty space between columns pushes both columns off-screen.
function clampTranslate(
  x: number, y: number, k: number,
  layout: BipartiteLayout,
  viewW: number, viewH: number,
  margin = 120,
): { x: number; y: number } {
  return {
    x: Math.max(margin - layout.W * k, Math.min(viewW - margin, x)),
    y: Math.max(margin - layout.H * k, Math.min(viewH - margin, y)),
  };
}

function useViewport(svgRef: RefObject<SVGSVGElement | null>, layout: BipartiteLayout | null) {
  const [vp, setVp] = useState<Vp>({ x: 0, y: 0, k: 1 });
  const vpRef      = useRef<Vp>({ x: 0, y: 0, k: 1 });
  const layoutRef  = useRef<BipartiteLayout | null>(null);
  const drag       = useRef<{ sx: number; sy: number; ox: number; oy: number } | null>(null);

  useEffect(() => { vpRef.current = vp; },       [vp]);
  useEffect(() => { layoutRef.current = layout; }, [layout]);

  const fit = useCallback(() => {
    if (!svgRef.current || !layout) return;
    const rect = svgRef.current.getBoundingClientRect();
    const padX = 60, padY = 40;
    const w = layout.W + padX * 2;
    const h = layout.H + padY * 2;
    const k = Math.min(rect.width / w, rect.height / h, 1.2);
    setVp({
      x: rect.width  / 2 - (layout.W / 2) * k,
      y: rect.height / 2 - (layout.H / 2) * k,
      k,
    });
  }, [layout, svgRef]);

  useEffect(() => { fit(); }, [fit]);

  // Non-passive wheel: prevents page scroll and applies cursor-anchored zoom
  // with bounds clamping so content can't be pushed fully off-screen.
  useEffect(() => {
    const el = svgRef.current;
    if (!el) return;
    const handler = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const mx = e.clientX - rect.left;
      const my = e.clientY - rect.top;
      setVp(v => {
        const factor = Math.exp(-e.deltaY * 0.0015);
        const k = Math.max(0.3, Math.min(2.4, v.k * factor));
        const rawX = mx - ((mx - v.x) * k) / v.k;
        const rawY = my - ((my - v.y) * k) / v.k;
        const L = layoutRef.current;
        if (!L) return { k, x: rawX, y: rawY };
        const { x, y } = clampTranslate(rawX, rawY, k, L, rect.width, rect.height);
        return { k, x, y };
      });
    };
    el.addEventListener('wheel', handler, { passive: false });
    return () => el.removeEventListener('wheel', handler);
  }, [svgRef]);

  // Document-level drag so fast mouse movements outside the SVG don't drop the pan.
  useEffect(() => {
    const el = svgRef.current;
    const onMove = (e: MouseEvent) => {
      if (!drag.current || !el) return;
      const rect = el.getBoundingClientRect();
      const rawX = drag.current.ox + (e.clientX - drag.current.sx);
      const rawY = drag.current.oy + (e.clientY - drag.current.sy);
      setVp(v => {
        const L = layoutRef.current;
        if (!L) return { ...v, x: rawX, y: rawY };
        const { x, y } = clampTranslate(rawX, rawY, v.k, L, rect.width, rect.height);
        return { ...v, x, y };
      });
    };
    const onUp = () => { drag.current = null; };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup',  onUp);
    return () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup',  onUp);
    };
  }, [svgRef]);

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    if (e.button !== 0) return;
    drag.current = { sx: e.clientX, sy: e.clientY, ox: vpRef.current.x, oy: vpRef.current.y };
  }, []);

  return { vp, setVp, fit, onMouseDown };
}

// ── Sparkline ───────────────────────────────────────────────────────────

function Sparkline({ alert }: { alert: AlertRule }) {
  const W = 312, H = 56, days = 30;
  let s = (alert.id.charCodeAt(alert.id.length - 1) || 13) * 7;
  const rnd = () => { s = (s * 9301 + 49297) % 233280; return s / 233280; };
  const pts = Array.from({ length: days }, (_, i) => {
    const trend = 1 + alert.trend * (i / days);
    return Math.max(0, (alert.count / days) * trend * (0.6 + rnd() * 0.9));
  });
  const max = Math.max(...pts, 1);
  const stepX = W / (days - 1);
  const path = pts.map((v, i) => {
    const x = i * stepX;
    const y = H - 4 - (v / max) * (H - 8);
    return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`;
  }).join(' ');
  const area = `${path} L${W} ${H} L0 ${H} Z`;
  const color = SEV_COLOR[alert.severity];
  return (
    <svg width={W} height={H} className="cx-sparkline">
      <defs>
        <linearGradient id={`spark-${alert.id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%"   stopColor={color} stopOpacity="0.32" />
          <stop offset="100%" stopColor={color} stopOpacity="0"    />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#spark-${alert.id})`} />
      <path d={path} fill="none" stroke={color} strokeWidth="1.5" />
      <text x={4}   y={12} fill="var(--cx-fg-3)" fontSize="10" fontFamily="var(--cx-mono)">30d</text>
      <text x={W-4} y={12} textAnchor="end" fill="var(--cx-fg-3)" fontSize="10" fontFamily="var(--cx-mono)">
        peak {fmtNum(Math.round(max))}
      </text>
    </svg>
  );
}

// ── Drill panel ─────────────────────────────────────────────────────────

function PanelHeader({
  eyebrow, color, title, sub, onClose,
}: { eyebrow: string; color: string; title: string; sub: string; onClose: () => void }) {
  return (
    <div className="cx-panel-head">
      <div className="cx-panel-head-row">
        <div className="cx-panel-eyebrow" style={{ color }}>
          <span className="cx-panel-dot" style={{ background: color }} />
          {eyebrow}
        </div>
        <button className="cx-panel-x" type="button" onClick={onClose} aria-label="Close">×</button>
      </div>
      <h2 className="cx-panel-title">{title}</h2>
      <div className="cx-panel-sub cx-mono">{sub}</div>
    </div>
  );
}

function StatGrid({ items }: { items: { label: string; value: string; sub?: string; accent?: string }[] }) {
  return (
    <div className="cx-stat-grid">
      {items.map((it, i) => (
        <div key={i} className={`cx-sg-item${it.accent ? ` cx-sg-${it.accent}` : ''}`}>
          <div className="cx-sg-label">{it.label}</div>
          <div className="cx-sg-value cx-mono">{it.value}</div>
          {it.sub && <div className="cx-sg-sub">{it.sub}</div>}
        </div>
      ))}
    </div>
  );
}

function Section({
  title, count, children,
}: { title: string; count?: number; children: React.ReactNode }) {
  return (
    <div className="cx-panel-section">
      <div className="cx-panel-section-head">
        <span>{title}</span>
        {count != null && <span className="cx-ps-count">{count}</span>}
      </div>
      <div className="cx-panel-section-body">{children}</div>
    </div>
  );
}

function AlertDrillPanel({
  a, techniques, onClose, onJumpToTech,
}: { a: AlertRule; techniques: TechNode[]; onClose: () => void; onJumpToTech: (t: TechNode) => void }) {
  const linked = a.tids.map(tid => techniques.find(t => t.id === tid)).filter(Boolean) as TechNode[];
  const trendUp   = a.trend > 0.1;
  const trendDown = a.trend < -0.1;
  const trendStr  = `${trendUp ? '↑' : trendDown ? '↓' : '→'} ${Math.abs(Math.round(a.trend * 100))}%`;
  const lastSeen  = a.lastSeenHrs < 1 ? '< 1h ago'
                  : a.lastSeenHrs < 24 ? `${a.lastSeenHrs}h ago`
                  : `${Math.floor(a.lastSeenHrs / 24)}d ago`;

  return (
    <aside className="cx-drill">
      <PanelHeader
        eyebrow={`${a.severity.toUpperCase()} · ${a.source}`}
        color={SEV_COLOR[a.severity]}
        title={a.name}
        sub={a.id}
        onClose={onClose}
      />

      <StatGrid items={[
        { label: 'Volume (30d)', value: fmtNum(a.count), sub: trendStr,
          accent: trendUp ? 'crit' : trendDown ? 'ok' : undefined },
        { label: 'Assets',        value: String(a.assets),      sub: 'affected' },
        { label: 'False positive', value: `${a.fpRate}%`,        accent: a.fpRate > 50 ? 'warn' : undefined },
        { label: 'Noise share',   value: `${a.noisePct}%` },
        { label: 'MTTD',          value: `${a.mttd}m`,          sub: 'detect' },
        { label: 'MTTR',          value: `${a.mttr}m`,          sub: 'respond' },
      ]} />

      <div className="cx-kv-list">
        {[
          ['Owner',    a.owner],
          ['Last seen', lastSeen],
          ['Source',   a.source],
          ['Rule ID',  a.id],
        ].map(([k, v]) => (
          <div key={k} className="cx-kv">
            <span className="cx-kv-k">{k}</span>
            <span className="cx-kv-v cx-mono">{v}</span>
          </div>
        ))}
      </div>

      <Section title="MITRE techniques" count={linked.length}>
        {linked.length === 0 && (
          <div className="cx-empty-cov">
            <div className="cx-ec-title">No technique mapping</div>
            <div className="cx-ec-body">No MITRE techniques are explicitly linked to this rule. Keyword matching is used as a best-effort approximation.</div>
          </div>
        )}
        {linked.map(t => (
          <button key={t.id} type="button" className="cx-link-row" onClick={() => onJumpToTech(t)}>
            <span className="cx-lr-id cx-mono">{t.id}</span>
            <span className="cx-lr-name">{t.name}</span>
            <span className="cx-lr-tag" style={{ '--clr': COV_COLOR[t.coverage] } as React.CSSProperties}>
              {t.tacticShort}
            </span>
          </button>
        ))}
      </Section>

      <Section title="Activity (last 30 days)">
        <Sparkline alert={a} />
      </Section>

      <div className="cx-panel-actions">
        <button type="button" className="cx-btn cx-btn-primary">Open in console →</button>
        <button type="button" className="cx-btn">Tune rule</button>
        <button type="button" className="cx-btn">Suppress…</button>
      </div>
    </aside>
  );
}

function TechDrillPanel({
  t, alerts, onClose, onJumpToAlert, onViewMitre,
}: {
  t: TechNode; alerts: AlertRule[]; onClose: () => void;
  onJumpToAlert: (a: AlertRule) => void; onViewMitre: () => void;
}) {
  const linked = alerts.filter(a => a.tids.includes(t.id))
    .sort((a, b) => b.count - a.count);

  return (
    <aside className="cx-drill">
      <PanelHeader
        eyebrow={`${t.tacticShort.toUpperCase()} · ${COV_LABEL[t.coverage].toUpperCase()}`}
        color={COV_COLOR[t.coverage]}
        title={t.name}
        sub={`${t.id} · ${t.tacticName}`}
        onClose={onClose}
      />

      <StatGrid items={[
        { label: 'Coverage',        value: COV_LABEL[t.coverage],
          accent: t.coverage === 'strong' ? 'ok' : t.coverage === 'partial' ? 'warn' : 'crit' },
        { label: 'Detecting rules', value: String(t.linkedCount) },
        { label: 'Alerts (30d)',    value: fmtNum(t.totalAlerts) },
        { label: 'Tactic',         value: t.tacticShort },
      ]} />

      <Section title="Detecting alert rules" count={linked.length}>
        {linked.length === 0 && (
          <div className="cx-empty-cov">
            <div className="cx-ec-title">No detection coverage</div>
            <div className="cx-ec-body">No alert rules are mapped to this technique. Consider deploying detections from the recommended rule set.</div>
          </div>
        )}
        {linked.map(a => (
          <button key={a.id} type="button" className="cx-link-row" onClick={() => onJumpToAlert(a)}>
            <span className="cx-lr-stripe" style={{ background: SEV_COLOR[a.severity] }} />
            <span className="cx-lr-name">{a.name}</span>
            <span className="cx-lr-num cx-mono">{fmtNum(a.count)}</span>
          </button>
        ))}
      </Section>

      <Section title="Recommendation">
        <div className="cx-reco">
          {t.coverage === 'strong' && <p>Coverage is strong. Continue tuning to reduce noise on the highest-volume detections below.</p>}
          {t.coverage === 'partial' && <p>Coverage is partial — only low-confidence detections fire on this technique. Consider adding behavioral or telemetry-driven rules to catch evasion.</p>}
          {t.coverage === 'none' && <p>This technique is uncovered. Recommended next step: enable telemetry from the matching data source and deploy a baseline rule set.</p>}
        </div>
      </Section>

      <div className="cx-panel-actions">
        <button type="button" className="cx-btn cx-btn-primary" onClick={onViewMitre}>
          Open in MITRE coverage →
        </button>
        <button type="button" className="cx-btn">View ATT&amp;CK page</button>
      </div>
    </aside>
  );
}

// ── Canvas ──────────────────────────────────────────────────────────────

function GraphCanvas({
  alerts, techniques, layout, vp, focusedId, hoveredId, setHovered,
  onPickAlert, onPickTech, severityFilter,
}: {
  alerts: AlertRule[];
  techniques: TechNode[];
  layout: BipartiteLayout;
  vp: Vp;
  focusedId: string | null;
  hoveredId: string | null;
  setHovered: (id: string | null) => void;
  onPickAlert: (a: AlertRule) => void;
  onPickTech: (t: TechNode) => void;
  severityFilter: Set<Severity>;
}) {
  const { alertPos, techPos, tacticBands, rightX } = layout;

  const aVisible = (a: AlertRule) => !severityFilter.size || severityFilter.has(a.severity);

  const edges = useMemo(() => {
    const list: { aid: string; tid: string; sev: Severity; count: number }[] = [];
    alerts.forEach(a => {
      a.tids.forEach(tid => {
        if (!alertPos[a.id] || !techPos[tid]) return;
        list.push({ aid: a.id, tid, sev: a.severity, count: a.count });
      });
    });
    return list;
  }, [alerts, alertPos, techPos]);

  const focusTarget = focusedId ?? hoveredId;
  const focusEdges  = new Set<number>();
  const focusAlerts = new Set<string>();
  const focusTechs  = new Set<string>();

  if (focusTarget) {
    edges.forEach((e, i) => {
      if (e.aid === focusTarget || e.tid === focusTarget) {
        focusEdges.add(i);
        focusAlerts.add(e.aid);
        focusTechs.add(e.tid);
      }
    });
  }

  return (
    <g transform={`translate(${vp.x} ${vp.y}) scale(${vp.k})`}>
      {/* Tactic band separators + labels */}
      {tacticBands.map(b => (
        <g key={b.id}>
          <line
            x1={rightX - 30} x2={rightX - 30} y1={b.y + 6} y2={b.y + b.h - 6}
            stroke="var(--cx-border)" strokeWidth={1}
          />
          <text
            x={rightX - 40} y={b.y + b.h / 2}
            textAnchor="end" fill="var(--cx-fg-3)" fontSize="11"
            fontFamily="var(--cx-sans)" style={{ textTransform: 'uppercase', letterSpacing: '0.06em' }}
          >
            {b.short}
          </text>
        </g>
      ))}

      {/* Column headers */}
      <text x={layout.leftX - 16} y={48} textAnchor="end"
        fill="var(--cx-fg-2)" fontSize="11" fontFamily="var(--cx-sans)"
        fontWeight="600" style={{ textTransform: 'uppercase', letterSpacing: '0.08em' }}>
        Alert rules · {alerts.length}
      </text>
      <text x={rightX - 30} y={48} textAnchor="end"
        fill="var(--cx-fg-2)" fontSize="11" fontFamily="var(--cx-sans)"
        fontWeight="600" style={{ textTransform: 'uppercase', letterSpacing: '0.08em' }}>
        MITRE techniques · {techniques.length}
      </text>

      {/* Edges */}
      {edges.map((e, i) => {
        const aPos = alertPos[e.aid];
        const tPos = techPos[e.tid];
        if (!aPos || !tPos) return null;
        const visible = aVisible(alerts.find(x => x.id === e.aid)!);
        const isFocus = focusEdges.has(i);
        const dim = focusTarget && !isFocus;
        const opacity = !visible ? 0.04 : isFocus ? 0.85 : dim ? 0.06 : 0.18;
        const stroke  = isFocus ? SEV_COLOR[e.sev] : 'var(--cx-edge)';
        const width   = isFocus ? 1.6 : 0.8;
        const ax = aPos.x + 90;
        const tx = tPos.x - 4;
        const cx1 = ax + (tx - ax) * 0.45;
        const cx2 = tx - (tx - ax) * 0.45;
        return (
          <path key={i}
            d={`M${ax} ${aPos.y}C${cx1} ${aPos.y} ${cx2} ${tPos.y} ${tx} ${tPos.y}`}
            fill="none" stroke={stroke} strokeWidth={width} opacity={opacity}
          />
        );
      })}

      {/* Alert nodes (left column) */}
      {alerts.map(a => {
        const p = alertPos[a.id];
        if (!p) return null;
        const visible  = aVisible(a);
        const isFocused = focusedId === a.id;
        const isHovered = hoveredId === a.id;
        const isFocus  = isFocused || focusAlerts.has(a.id);
        const dim = (focusTarget && !isFocus) || !visible;
        const sevColor = SEV_COLOR[a.severity];
        const W = 200, H = 30;

        return (
          <g key={a.id}
            transform={`translate(${p.x - W} ${p.y - H / 2})`}
            opacity={dim ? 0.18 : 1}
            style={{ cursor: 'pointer' }}
            onMouseEnter={() => setHovered(a.id)}
            onMouseLeave={() => setHovered(null)}
            onClick={e => { e.stopPropagation(); onPickAlert(a); }}
          >
            <rect width={W} height={H} rx={6}
              fill={isFocused || isHovered ? 'var(--cx-bg-3)' : 'var(--cx-bg-2)'}
              stroke={isFocused ? sevColor : 'var(--cx-border)'}
              strokeWidth={isFocused ? 1.5 : 1}
            />
            <rect width={3} height={H} rx={1.5} fill={sevColor} />
            <text x={11} y={13} fill="var(--cx-fg)" fontSize="11" fontFamily="var(--cx-sans)" fontWeight="500">
              {a.name.length > 28 ? a.name.slice(0, 26) + '…' : a.name}
            </text>
            <text x={11} y={24} fill="var(--cx-fg-3)" fontSize="9.5" fontFamily="var(--cx-mono)" style={{ letterSpacing: '0.03em' }}>
              {a.id} · {a.source}
            </text>
            <text x={W - 8} y={13} textAnchor="end" fill={sevColor} fontSize="11" fontFamily="var(--cx-mono)" fontWeight="600">
              {fmtNum(a.count)}
            </text>
            <text x={W - 8} y={24} textAnchor="end" fill="var(--cx-fg-3)" fontSize="9" fontFamily="var(--cx-sans)" style={{ textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              {a.severity}
            </text>
          </g>
        );
      })}

      {/* Technique nodes (right column) */}
      {techniques.map(t => {
        const p = techPos[t.id];
        if (!p) return null;
        const isFocused = focusedId === t.id;
        const isHovered = hoveredId === t.id;
        const isFocus  = isFocused || focusTechs.has(t.id);
        const dim = focusTarget && !isFocus;
        const cov = COV_COLOR[t.coverage];
        const W = 100, H = 26;

        return (
          <g key={t.id}
            transform={`translate(${p.x} ${p.y - H / 2})`}
            opacity={dim ? 0.18 : 1}
            style={{ cursor: 'pointer' }}
            onMouseEnter={() => setHovered(t.id)}
            onMouseLeave={() => setHovered(null)}
            onClick={e => { e.stopPropagation(); onPickTech(t); }}
          >
            <rect width={W} height={H} rx={5}
              fill={isFocused || isHovered ? 'var(--cx-bg-3)' : 'var(--cx-bg-2)'}
              stroke={isFocused ? cov : 'var(--cx-border)'}
              strokeWidth={isFocused ? 1.5 : 1}
            />
            <rect width={3} height={H} rx={1.5} fill={cov} />
            <text x={9} y={11} fill="var(--cx-fg)" fontSize="10" fontFamily="var(--cx-mono)" fontWeight="600">
              {t.id}
            </text>
            <text x={9} y={20} fill="var(--cx-fg-3)" fontSize="8.5" fontFamily="var(--cx-sans)">
              {t.name.length > 16 ? t.name.slice(0, 15) + '…' : t.name}
            </text>
          </g>
        );
      })}
    </g>
  );
}

// ── Posture bar ─────────────────────────────────────────────────────────

function PostureBar({
  posture, lookback, severityFilter, toggleSev,
}: {
  posture: Posture;
  lookback: number;
  severityFilter: Set<Severity>;
  toggleSev: (s: Severity) => void;
}) {
  const pct = Math.round((posture.techniquesCovered / Math.max(1, posture.techniquesTotal)) * 100);
  const sevs: Severity[] = ['critical', 'high', 'medium', 'low'];

  return (
    <div className="cx-posturebar">
      <div className="cx-pb-summary">
        <div className="cx-pb-eyebrow">Posture · {lookback}d</div>
        <div className="cx-pb-headline">
          <span className="cx-pb-num cx-mono">{fmtNum(posture.totalAlerts)}</span>
          <span className="cx-pb-label">alerts ·</span>
          <span className="cx-pb-num cx-mono">{posture.techniquesCovered}/{posture.techniquesTotal}</span>
          <span className="cx-pb-label">techniques</span>
          <span className="cx-pb-pct cx-mono">{pct}%</span>
        </div>
      </div>

      <div className="cx-pb-stats">
        <span className="cx-pb-stat cx-pb-stat-crit">
          <span className="cx-mono">{fmtNum(posture.critical)}</span> critical
        </span>
        <span className="cx-pb-stat cx-pb-stat-high">
          <span className="cx-mono">{fmtNum(posture.high)}</span> high
        </span>
        <span className="cx-pb-stat cx-pb-stat-warn">
          <span className="cx-mono">{posture.gaps}</span> gaps
        </span>
        <span className="cx-pb-stat">
          <span className="cx-mono">{posture.avgFpRate}%</span> avg FP
        </span>
      </div>

      <div className="cx-pb-filters">
        <div className="cx-chips">
          {sevs.map(s => {
            const active = !severityFilter.size || severityFilter.has(s);
            return (
              <button key={s} type="button"
                className={`cx-chip cx-chip-${s}${active ? '' : ' cx-chip-inactive'}`}
                onClick={() => toggleSev(s)}
              >
                <span className="cx-chip-dot" />
                {s[0].toUpperCase() + s.slice(1)}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}

// ── Main component ──────────────────────────────────────────────────────

export default function ThreatGraph({ data, clientName, lookbackDays, onViewMitre }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 1200, height: 720 });

  useLayoutEffect(() => {
    const update = () => {
      if (wrapRef.current) {
        const r = wrapRef.current.getBoundingClientRect();
        if (r.width > 0 && r.height > 0) setSize({ width: r.width, height: r.height });
      }
    };
    update();
    window.addEventListener('resize', update);
    return () => window.removeEventListener('resize', update);
  }, []);

  const alerts     = useMemo(() => buildAlertRules(data), [data]);
  const techniques = useMemo(() => buildTechNodes(data, alerts), [data, alerts]);
  const posture    = useMemo(() => buildPosture(data, alerts), [data, alerts]);

  const layout = useMemo(() => {
    // 2-column technique layout: ceil(n/2) rows × 28px each. Cap at 3000px.
    const h = Math.min(Math.max(700, Math.ceil(techniques.length / 2) * 28 + 160, alerts.length * 36), 3000);
    const w = Math.max(1100, size.width * 1.15);
    return buildBipartite(alerts, techniques, w, h);
  }, [alerts, techniques, size]);

  const { vp, setVp, fit, onMouseDown } = useViewport(svgRef, layout);

  const [pick, setPick] = useState<Pick | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [severityFilter, setSeverityFilter] = useState<Set<Severity>>(new Set());
  const [lookback, setLookback] = useState(lookbackDays);

  const focusedId = pick?.data.id ?? null;

  const toggleSev = (s: Severity) => {
    setSeverityFilter(prev => {
      const next = new Set(prev);
      if (!prev.size) return new Set([s]);
      if (next.has(s)) next.delete(s); else next.add(s);
      if (next.size === 4) return new Set();
      return next;
    });
  };

  return (
    <div className="cx-tg-root">

      {/* Posture bar */}
      <PostureBar
        posture={posture}
        lookback={lookback}
        severityFilter={severityFilter}
        toggleSev={toggleSev}
      />

      {/* Body: canvas + drill panel */}
      <div className="cx-tg-body">
        <div className="cx-tg-canvas-wrap" ref={wrapRef}>
          <svg
            ref={svgRef}
            className="cx-tg-canvas"
            onMouseDown={onMouseDown}
            onClick={() => setPick(null)}
            style={{ touchAction: 'none' }}
          >
            <defs>
              <pattern id="cx-tg-grid" width="40" height="40" patternUnits="userSpaceOnUse"
                patternTransform={`translate(${vp.x % (40 * vp.k)} ${vp.y % (40 * vp.k)}) scale(${vp.k})`}>
                <circle cx="20" cy="20" r="0.6" fill="var(--cx-grid-dot)" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill="url(#cx-tg-grid)" />

            <GraphCanvas
              alerts={alerts}
              techniques={techniques}
              layout={layout}
              vp={vp}
              focusedId={focusedId}
              hoveredId={hovered}
              setHovered={setHovered}
              onPickAlert={a => setPick({ type: 'alert', data: a })}
              onPickTech={t => setPick({ type: 'tech', data: t })}
              severityFilter={severityFilter}
            />
          </svg>

          {/* Zoom controls */}
          <div className="cx-tg-zoom">
            <button type="button" onClick={() => setVp(v => ({ ...v, k: Math.min(2.4, v.k * 1.2) }))}>+</button>
            <div className="cx-zoom-readout cx-mono">{Math.round(vp.k * 100)}%</div>
            <button type="button" onClick={() => setVp(v => ({ ...v, k: Math.max(0.3, v.k / 1.2) }))}>−</button>
            <div className="cx-zoom-divider" />
            <button type="button" onClick={fit}>⤢</button>
          </div>

          {/* Lookback control */}
          <div className="cx-tg-lookback">
            <span className="cx-lb-label">Lookback</span>
            <div className="cx-seg">
              {[7, 14, 30, 90].map(d => (
                <button key={d} type="button"
                  className={lookback === d ? 'active' : ''}
                  onClick={() => setLookback(d)}>
                  {d}d
                </button>
              ))}
            </div>
          </div>

          {/* Legend */}
          <div className="cx-tg-legend">
            {(['critical', 'high', 'medium', 'low'] as Severity[]).map(s => (
              <div key={s} className="cx-legend-row">
                <span className="cx-leg-dot" style={{ background: SEV_COLOR[s] }} />
                {s[0].toUpperCase() + s.slice(1)}
              </div>
            ))}
            <div className="cx-leg-divider" />
            {(['strong', 'partial', 'none'] as Coverage[]).map(c => (
              <div key={c} className="cx-legend-row">
                <span className="cx-leg-dot" style={{ background: COV_COLOR[c] }} />
                {COV_LABEL[c]}
              </div>
            ))}
          </div>
        </div>

        {/* Drill panel */}
        {pick?.type === 'alert' && (
          <AlertDrillPanel
            key={pick.data.id}
            a={pick.data}
            techniques={techniques}
            onClose={() => setPick(null)}
            onJumpToTech={t => setPick({ type: 'tech', data: t })}
          />
        )}
        {pick?.type === 'tech' && (
          <TechDrillPanel
            key={pick.data.id}
            t={pick.data}
            alerts={alerts}
            onClose={() => setPick(null)}
            onJumpToAlert={a => setPick({ type: 'alert', data: a })}
            onViewMitre={onViewMitre}
          />
        )}
      </div>
    </div>
  );
}
