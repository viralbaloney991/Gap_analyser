import { useState, useEffect, useMemo, useCallback, Fragment, useRef } from 'react';
import { AlertTriangle, Info, XCircle } from 'lucide-react';
import { MITRE_TACTICS, MITRE_TECHNIQUES, mockGenerate } from '../data/mitre-catalog';
import type { MITRETechnique } from '../data/mitre-catalog';
import { buildDetection, fetchMitreCatalog } from '../services/api';
import type { GenerationResult, FlowAlert, MitreCatalog } from '../types';

// ── Helpers ──────────────────────────────────────────────────────────────────

const SEV_COLOR: Record<string, string> = {
  critical: '#dc2626',
  high:     '#f97316',
  medium:   '#eab308',
  low:      '#94a3b8',
};

function cls(...xs: (string | false | undefined | null)[]) {
  return xs.filter(Boolean).join(' ');
}

// ── Tactic visual metadata (ported from design handoff builder-base.jsx) ─────

const TACTIC_COLOR: Record<string, string> = {
  TA0043: '#06b6d4', TA0001: '#f97316', TA0002: '#eab308', TA0003: '#f59e0b',
  TA0004: '#fb923c', TA0005: '#a855f7', TA0006: '#ec4899', TA0007: '#3b82f6',
  TA0008: '#6366f1', TA0009: '#14b8a6', TA0011: '#ef4444', TA0010: '#dc2626',
  TA0040: '#b91c1c', TA0042: '#10b981',
};

const TACTIC_GLYPH: Record<string, string> = {
  TA0043: '⌖', TA0001: '⇲', TA0002: '▶', TA0003: '⚓',
  TA0004: '↑',  TA0005: '◈', TA0006: '⚷', TA0007: '?',
  TA0008: '⇄',  TA0009: '◐', TA0011: '⚡', TA0010: '↗',
  TA0040: '✸',  TA0042: '⊕',
};

// ── SVG layout constants (match handoff pixel-for-pixel) ─────────────────────

const NODE_W        = 208;
const NODE_H        = 52;
const NODE_GAP      = 10;
const BAND_PAD_X    = 28;
const BAND_W        = NODE_W + BAND_PAD_X * 2; // 264
const HEAD_H        = 60;
const TRIGGER_W     = 132;
const TRIGGER_X     = 16;
const BANDS_START_X = TRIGGER_X + TRIGGER_W + 40; // 188

// ── Types ────────────────────────────────────────────────────────────────────

interface Props {
  clientName: string;
  preselectedIds?: string[];
  onHunt?: (alert: FlowAlert) => void;
}

// ── Root component ───────────────────────────────────────────────────────────

export default function DetectionBuilder({ clientName, preselectedIds, onHunt }: Props) {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(
    () => new Set(preselectedIds ?? [])
  );
  const [query, setQuery]             = useState('');
  const [generating, setGenerating]   = useState(false);
  const [result, setResult]           = useState<GenerationResult | null>(null);
  const [catalog, setCatalog]         = useState<MitreCatalog | null>(null);
  const [catalogLoading, setCatalogLoading] = useState(true);

  const tactics    = catalog?.tactics    ?? MITRE_TACTICS.map(t => ({ id: t.id, name: t.name, short: t.short, order: t.order }));
  const techniques = catalog?.techniques ?? MITRE_TECHNIQUES.map(t => ({ id: t.id, name: t.name, tactic: t.tactic, tacticName: t.tacticName, tacticOrder: t.tacticOrder, source: t.source }));

  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  // Fetch full MITRE ATT&CK catalog; fall back to static on error.
  useEffect(() => {
    setCatalogLoading(true);
    fetchMitreCatalog()
      .then(data => { if (mountedRef.current) { setCatalog(data); setCatalogLoading(false); } })
      .catch(() => { if (mountedRef.current) setCatalogLoading(false); });
  }, []);

  const selected = useMemo(
    () => techniques.filter(t => selectedIds.has(t.id)),
    [selectedIds, techniques],
  );

  const addTech = useCallback((t: { id: string; name: string; tactic: string; tacticName: string; tacticOrder: number; source: string }) => {
    setSelectedIds(s => { const n = new Set(s); n.add(t.id); return n; });
  }, []);

  const removeTech = useCallback((id: string) => {
    setSelectedIds(s => { const n = new Set(s); n.delete(id); return n; });
  }, []);

  const clearAll = useCallback(() => {
    setSelectedIds(new Set());
    setResult(null);
  }, []);

  // Listen for drop events dispatched by SourceMatrix.
  useEffect(() => {
    const onDrop = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      const t = techniques.find(x => x.id === id);
      if (t) addTech(t);
    };
    window.addEventListener('basket-drop', onDrop);
    return () => window.removeEventListener('basket-drop', onDrop);
  }, [addTech, techniques]);

  const handleGenerate = async () => {
    setGenerating(true);
    setResult(null);

    const payload = selected.map(t => ({
      id: t.id, name: t.name,
      tactic_id: t.tactic, tactic_name: t.tacticName,
      tactic_order: t.tacticOrder, source: t.source,
    }));

    let out: GenerationResult;
    try {
      out = await buildDetection(clientName, payload);
    } catch {
      out = mockGenerate(selected);
    }

    // Ensure loading state is perceptible (≥600ms) matching handoff spec.
    setTimeout(() => {
      if (mountedRef.current) {
        setResult(out);
        setGenerating(false);
      }
    }, 600);
  };

  return (
    <div className="db-root">
      {/* Page intro */}
      <div className="page-intro">
        <div>
          <div className="pi-eyebrow">Detection Builder</div>
          <div className="pi-title">Compose a multi-stage detection chain</div>
          <div className="pi-sub">
            Drag techniques into the chain below. The system orders by kill-chain stage
            and generates correlated flow alerts with realistic time windows.
          </div>
        </div>
        <div className="pi-search">
          <input
            placeholder="Search techniques (T1078, brute force…)"
            value={query}
            onChange={e => setQuery(e.target.value)}
          />
          {query && <button className="clear-x" onClick={() => setQuery('')}>×</button>}
        </div>
      </div>

      <div className="builder-body">
        {/* Left: source matrix + basket (below, matching handoff layout) */}
        <div className="builder-source">
          <div className="bs-head">
            <span className="bs-title">MITRE ATT&amp;CK Techniques</span>
            <span className="bs-hint">
              {catalogLoading
                ? <span className="catalog-loading">⟳ Loading full catalog…</span>
                : catalog
                  ? <span className="catalog-badge">{techniques.length} techniques · Full ATT&amp;CK</span>
                  : 'Drag to chain · double-click to add'}
            </span>
          </div>
          <SourceMatrix selectedIds={selectedIds} onAdd={addTech} query={query} tactics={tactics} techniques={techniques} />
          <Basket
            selected={selected}
            tactics={tactics}
            onRemove={removeTech}
            onClear={clearAll}
            onGenerate={handleGenerate}
            generating={generating}
          />
        </div>

        {/* Right: generated panel only */}
        <div className="builder-right">
          <GeneratedPanel
            result={result}
            generating={generating}
            onClose={() => setResult(null)}
            onRegenerate={handleGenerate}
            onHunt={onHunt}
          />
        </div>
      </div>
    </div>
  );
}

// ── SourceMatrix ─────────────────────────────────────────────────────────────

type CatalogTechnique = { id: string; name: string; tactic: string; tacticName: string; tacticOrder: number; source: string };
type CatalogTactic    = { id: string; name: string; short: string; order: number };

interface SourceMatrixProps {
  selectedIds: Set<string>;
  onAdd: (t: CatalogTechnique) => void;
  query: string;
  tactics: CatalogTactic[];
  techniques: CatalogTechnique[];
}

function SourceMatrix({ selectedIds, onAdd, query, tactics, techniques }: SourceMatrixProps) {
  // Fast id→technique lookup
  const techMap = useMemo(() => new Map(techniques.map(t => [t.id, t])), [techniques]);

  // Filter + group by tactic
  const grouped = useMemo(() =>
    tactics
      .map(tac => ({
        ...tac,
        techs: techniques.filter(t =>
          t.tactic === tac.id &&
          (!query ||
            t.id.toLowerCase().includes(query.toLowerCase()) ||
            t.name.toLowerCase().includes(query.toLowerCase()))
        ),
      }))
      .filter(tac => tac.techs.length > 0),
    [tactics, techniques, query],
  );

  // Compute SVG canvas dimensions and node positions
  const { positions, bands, totalW, totalH } = useMemo(() => {
    const tallest = grouped.reduce((m, g) => Math.max(m, g.techs.length), 0);
    const h = HEAD_H + tallest * (NODE_H + NODE_GAP) + 40;
    const w = BANDS_START_X + grouped.length * BAND_W + 40;

    const pos: Record<string, { x: number; y: number }> = {};
    const bds: Array<typeof grouped[0] & { x: number; w: number }> = [];

    grouped.forEach((g, i) => {
      const x = BANDS_START_X + i * BAND_W;
      bds.push({ ...g, x, w: BAND_W });
      g.techs.forEach((t, j) => {
        pos[t.id] = { x: x + BAND_PAD_X, y: HEAD_H + j * (NODE_H + NODE_GAP) };
      });
    });

    return { positions: pos, bands: bds, totalW: w, totalH: h };
  }, [grouped]);

  // Selected nodes sorted by kill-chain stage
  const selOrdered = useMemo(() =>
    techniques
      .filter(t => selectedIds.has(t.id))
      .sort((a, b) => a.tacticOrder - b.tacticOrder),
    [techniques, selectedIds],
  );

  // Step-curve path between two points (orthogonal with rounded corners)
  const stepPath = useCallback((ax: number, ay: number, bx: number, by: number): string => {
    const mid = ax + Math.max(40, (bx - ax) / 2);
    const r = 12;
    if (Math.abs(by - ay) < 2) return `M${ax} ${ay}L${bx} ${by}`;
    const dir: 1 | -1 = by > ay ? 1 : -1;
    return `M${ax} ${ay}H${mid - r}Q${mid} ${ay} ${mid} ${ay + r * dir}V${by - r * dir}Q${mid} ${by} ${mid + r} ${by}H${bx}`;
  }, []);

  // Connection edges between selected nodes
  const edges = useMemo(() => {
    const result: Array<{ d: string; color: string; tacticId: string }> = [];
    const trigY = HEAD_H + 12 + NODE_H / 2;

    if (selOrdered.length > 0) {
      const first = positions[selOrdered[0].id];
      if (first) {
        const c = TACTIC_COLOR[selOrdered[0].tactic] ?? '#6366f1';
        result.push({ d: stepPath(TRIGGER_X + TRIGGER_W, trigY, first.x, first.y + NODE_H / 2), color: c, tacticId: selOrdered[0].tactic });
      }
    }
    for (let i = 0; i < selOrdered.length - 1; i++) {
      const a = positions[selOrdered[i].id];
      const b = positions[selOrdered[i + 1].id];
      if (!a || !b) continue;
      const c = TACTIC_COLOR[selOrdered[i + 1].tactic] ?? '#6366f1';
      result.push({ d: stepPath(a.x + NODE_W, a.y + NODE_H / 2, b.x, b.y + NODE_H / 2), color: c, tacticId: selOrdered[i + 1].tactic });
    }
    return result;
  }, [selOrdered, positions, stepPath]);

  const handleDragStart = useCallback((e: React.DragEvent, tech: CatalogTechnique) => {
    e.dataTransfer.setData('text/plain', tech.id);
    e.dataTransfer.effectAllowed = 'copy';
  }, []);

  const containerRef = useRef<HTMLDivElement>(null);
  const scrollToTactic = useCallback((tacticId: string) => {
    if (!containerRef.current) return;
    const band = bands.find(b => b.id === tacticId);
    if (band) containerRef.current.scrollLeft = Math.max(0, band.x - 40);
  }, [bands]);

  return (
    <div className="src-matrix-wrap">
      <div className="tactic-nav">
        {bands.map(b => (
          <button
            key={b.id}
            className="tactic-chip"
            style={{ '--chip-c': TACTIC_COLOR[b.id] ?? '#6366f1' } as React.CSSProperties}
            onClick={() => scrollToTactic(b.id)}
          >
            <span className="chip-glyph">{TACTIC_GLYPH[b.id] ?? '◆'}</span>
            {b.short}
          </button>
        ))}
      </div>
    <div ref={containerRef} className="src-graph">
      <div className="src-graph-grid" />
      <svg
        className="src-graph-svg"
        width={totalW}
        height={Math.max(totalH, 400)}
      >
        <defs>
          {tactics.map(tac => {
            const c = TACTIC_COLOR[tac.id] ?? '#6366f1';
            return (
              <marker key={tac.id} id={`arrow-${tac.id}`}
                      viewBox="0 0 10 10" refX="8" refY="5"
                      markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                <path d="M0,0 L10,5 L0,10 z" fill={c} />
              </marker>
            );
          })}
        </defs>

        {/* Tactic column bands */}
        {bands.map(b => {
          const c = TACTIC_COLOR[b.id] ?? '#6366f1';
          return (
            <g key={b.id}>
              <rect x={b.x + 8} y={HEAD_H - 12} width={b.w - 16} height={totalH - HEAD_H - 8}
                    fill="rgba(255,255,255,0.012)" stroke={c} strokeOpacity={0.18}
                    strokeDasharray="3 5" rx={8} />
              <circle cx={b.x + b.w / 2 - 38} cy={28} r={4} fill={c} />
              <text x={b.x + b.w / 2 - 28} y={32} fill="#a3acc0" fontSize={11}
                    fontFamily="var(--cx-sans,'Inter',sans-serif)"
                    fontWeight="700" letterSpacing="1">
                {b.short.toUpperCase()}
              </text>
              <text x={b.x + b.w / 2 + 52} y={32} fill="#6b7388" fontSize={10}
                    fontFamily="var(--font-mono,'IBM Plex Mono',monospace)">
                {b.techs.length}
              </text>
            </g>
          );
        })}

        {/* Trigger anchor on far left */}
        <foreignObject x={TRIGGER_X} y={HEAD_H + 12} width={TRIGGER_W} height={NODE_H}>
          <div className="sg-trigger">
            <div className="sg-trigger-icon">▶</div>
            <div className="sg-trigger-body">
              <div className="sg-trigger-eyebrow">TRIGGER</div>
              <div className="sg-trigger-title">Telemetry stream</div>
            </div>
          </div>
        </foreignObject>

        {/* Kill-chain connection lines */}
        <g>
          {edges.map((edge, i) => (
            <path key={i} d={edge.d} fill="none"
                  stroke={edge.color} strokeWidth={2} opacity={0.7}
                  markerEnd={`url(#arrow-${edge.tacticId})`} />
          ))}
        </g>

        {/* Technique node cards */}
        {Object.entries(positions).map(([id, p]) => {
          const t = techMap.get(id);
          if (!t) return null;
          const used  = selectedIds.has(id);
          const c     = TACTIC_COLOR[t.tactic] ?? '#6366f1';
          const glyph = TACTIC_GLYPH[t.tactic]  ?? '◆';
          return (
            <foreignObject key={id} x={p.x} y={p.y} width={NODE_W} height={NODE_H}>
              <div
                className={cls('sg-node', used && 'used')}
                draggable={!used}
                onDragStart={e => handleDragStart(e, t)}
                onDoubleClick={() => !used && onAdd(t)}
                style={{ '--node-c': c } as React.CSSProperties}
              >
                <div className="sg-node-icon" style={{ background: c }}>{glyph}</div>
                <div className="sg-node-body">
                  <div className="sg-node-id mono">{t.id}</div>
                  <div className="sg-node-name">{t.name}</div>
                </div>
                {used && <div className="sg-node-check">✓</div>}
              </div>
            </foreignObject>
          );
        })}
      </svg>
    </div>
    </div>
  );
}

// ── Basket ───────────────────────────────────────────────────────────────────

interface BasketProps {
  selected: MITRETechnique[];
  tactics: CatalogTactic[];
  onRemove: (id: string) => void;
  onClear: () => void;
  onGenerate: () => void;
  generating: boolean;
}

function Basket({ selected, tactics, onRemove, onClear, onGenerate, generating }: BasketProps) {
  const [dragOver, setDragOver] = useState(false);

  const ordered = [...selected].sort((a, b) => a.tacticOrder - b.tacticOrder);

  // Group by tactic.
  const tacticGroups: Array<{ tactic: CatalogTactic; techs: MITRETechnique[] }> = [];
  const seen = new Map<string, number>();
  for (const t of ordered) {
    if (!seen.has(t.tactic)) {
      const tac = tactics.find(x => x.id === t.tactic) ?? { id: t.tactic, name: t.tactic, short: t.tactic, order: t.tacticOrder };
      seen.set(t.tactic, tacticGroups.length);
      tacticGroups.push({ tactic: tac, techs: [] });
    }
    tacticGroups[seen.get(t.tactic)!].techs.push(t);
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    setDragOver(true);
  };
  const handleDragLeave = (e: React.DragEvent) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) {
      setDragOver(false);
    }
  };
  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    const id = e.dataTransfer.getData('text/plain');
    window.dispatchEvent(new CustomEvent('basket-drop', { detail: id }));
  };

  return (
    <div
      className={cls('basket', dragOver && 'drag-over')}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <div className="basket-head">
        <div>
          <div className="basket-eyebrow">Chain · Kill-chain order</div>
          <div className="basket-title">
            <span className="mono basket-count">{selected.length}</span> techniques selected
          </div>
        </div>
        <div className="basket-actions">
          {selected.length > 0 && (
            <button className="btn btn-ghost" onClick={onClear}>Clear</button>
          )}
          <button
            className="btn btn-primary"
            disabled={selected.length < 2 || generating}
            onClick={onGenerate}
          >
            {generating
              ? <><span className="spin" /> Generating…</>
              : <>Generate alerts <span className="arrow">→</span></>
            }
          </button>
        </div>
      </div>

      {selected.length === 0 ? (
        <div className="basket-empty">
          <div className="be-icon">⊕</div>
          <div className="be-title">Drag techniques here</div>
          <div className="be-sub">
            Build a multi-stage chain. The system will order steps by kill-chain stage
            and generate flow alerts.
          </div>
        </div>
      ) : (
        <div className="basket-flow">
          {tacticGroups.map((g, gi) => (
            <Fragment key={g.tactic.id}>
              <div className="bf-stage">
                <div className="bf-stage-head">
                  <span className="bf-stage-num mono">{String(gi + 1).padStart(2, '0')}</span>
                  <span className="bf-stage-name">{g.tactic.short}</span>
                </div>
                <div className="bf-techs">
                  {g.techs.map(t => (
                    <div key={t.id} className="bf-chip">
                      <span className="bf-chip-id mono">{t.id}</span>
                      <span className="bf-chip-name">{t.name}</span>
                      <button
                        className="bf-chip-x"
                        onClick={() => onRemove(t.id)}
                        aria-label={`Remove ${t.name}`}
                      >×</button>
                    </div>
                  ))}
                </div>
              </div>
              {gi < tacticGroups.length - 1 && (
                <div key={`arrow-${gi}`} className="bf-arrow">→</div>
              )}
            </Fragment>
          ))}
        </div>
      )}
    </div>
  );
}

// ── YAML export helpers ───────────────────────────────────────────────────────

function yamlStr(v: string) {
  return `"${v.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
}

function resultToYaml(result: GenerationResult): string {
  const lines: string[] = [
    '# Detection chain — Coralogix Detection Builder',
    '',
    `validation:`,
    `  verdict: ${result.validation.verdict}`,
  ];
  if (result.validation.findings.length) {
    lines.push('  findings:');
    for (const f of result.validation.findings) {
      lines.push(`    - level: ${f.level}`);
      lines.push(`      message: ${yamlStr(f.message)}`);
    }
  }
  lines.push('', 'alerts:');
  for (const a of result.alerts) {
    lines.push(`  - name: ${yamlStr(a.name)}`);
    lines.push(`    technique: ${a.techniqueId}`);
    lines.push(`    severity: ${a.severity}`);
    lines.push(`    window: ${a.window}`);
    lines.push(`    source: ${yamlStr(a.source)}`);
    lines.push(`    logic: |`);
    for (const ln of a.logic.split('\n')) lines.push(`      ${ln}`);
    lines.push('');
  }
  lines.push(
    'correlation:',
    `  name: ${yamlStr(result.correlation.name)}`,
    `  severity: ${result.correlation.severity}`,
    `  window: ${result.correlation.window}`,
    '  logic: |',
    ...result.correlation.logic.split('\n').map(ln => `    ${ln}`),
  );
  return lines.join('\n');
}

function downloadYaml(result: GenerationResult) {
  const blob = new Blob([resultToYaml(result)], { type: 'text/yaml' });
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  a.href     = url;
  a.download = `detection-chain-${Date.now()}.yaml`;
  a.click();
  URL.revokeObjectURL(url);
}

// ── GeneratedPanel ───────────────────────────────────────────────────────────

interface GeneratedPanelProps {
  result: GenerationResult | null;
  generating: boolean;
  onClose: () => void;
  onRegenerate: () => void;
  onHunt?: (alert: FlowAlert) => void;
}

function GeneratedPanel({ result, generating, onClose, onRegenerate, onHunt }: GeneratedPanelProps) {
  const [saved, setSaved] = useState(false);
  const [showSigma, setShowSigma] = useState(false);

  const handleSave = () => {
    if (!result) return;
    navigator.clipboard.writeText(resultToYaml(result)).catch(() => {});
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  if (!result && !generating) return null;

  return (
    <aside className="gen-panel">
      <div className="gen-head">
        <div>
          <div className="gen-eyebrow">Generated detection chain</div>
          <div className="gen-title">
            {generating ? 'Generating…' : (result?.correlation?.name ?? 'Detection flow')}
          </div>
        </div>
        <div className="gen-head-actions">
          <button className="btn btn-ghost btn-sm" onClick={onRegenerate} disabled={generating}>
            Regenerate
          </button>
          <button className="panel-x" onClick={onClose} aria-label="Close panel">×</button>
        </div>
      </div>

      {generating && <GenSkeleton />}

      {result && !generating && (
        <>
          {/* Validation card */}
          <div className={cls('val-card', `val-${result.validation.verdict}`)}>
            <div className="val-head">
              <span className="val-dot" />
              <span className="val-verdict">
                {result.validation.verdict === 'ok'       ? 'Chain validated'
                : result.validation.verdict === 'warnings' ? 'Validated with warnings'
                :                                            'Chain has issues'}
              </span>
            </div>
            <ul className="val-list">
              {result.validation.findings.map((f, i) => (
                <li key={i} className={cls('val-finding', `val-${f.level}`)}>
                  <span className="val-bullet">
                    {f.level === 'error' ? <XCircle size={12} /> : f.level === 'warn' ? <AlertTriangle size={12} /> : <Info size={12} />}
                  </span>
                  {f.message}
                </li>
              ))}
            </ul>
          </div>

          {/* Flow alerts */}
          <div className="gen-section-title">
            Flow alerts <span className="ps-count">{result.alerts.length}</span>
          </div>
          <div className="alert-cards">
            {result.alerts.map((a: FlowAlert, i: number) => (
              <div key={i} className="alert-card">
                <div className="ac-stripe" style={{ background: SEV_COLOR[a.severity] }} />
                <div className="ac-body">
                  <div className="ac-head">
                    <div className="ac-step mono">{String(i + 1).padStart(2, '0')}</div>
                    <div className="ac-title-block">
                      <div className="ac-name">{a.name}</div>
                      <div className="ac-desc">{a.description}</div>
                    </div>
                    <div className={`ac-sev ac-sev-${a.severity}`}>{a.severity}</div>
                  </div>
                  <div className="query-format-toggle">
                    <button
                      className={`toggle-btn${!showSigma ? ' active' : ''}`}
                      onClick={() => setShowSigma(false)}
                    >
                      Lucene
                    </button>
                    <button
                      className={`toggle-btn${showSigma ? ' active' : ''}`}
                      onClick={() => setShowSigma(true)}
                      disabled={!a.sigma_rule}
                    >
                      Sigma
                    </button>
                  </div>
                  <pre className="query-block">
                    {showSigma ? (a.sigma_rule || '# No Sigma rule available') : a.logic}
                  </pre>
                  <div className="ac-meta">
                    <span className="ac-meta-item">
                      <span className="ac-meta-k">Technique</span>
                      <span className="ac-meta-v mono">{a.techniqueId}</span>
                    </span>
                    <span className="ac-meta-item">
                      <span className="ac-meta-k">Source</span>
                      <span className="ac-meta-v">{a.source}</span>
                    </span>
                    <span className="ac-meta-item ac-window">
                      <span className="ac-meta-k">Window</span>
                      <span className="ac-meta-v mono">{a.window}</span>
                    </span>
                  </div>
                  <div className="ac-window-reason">
                    <span className="awr-label">Why this window:</span> {a.windowReason}
                  </div>
                  {onHunt && (
                    <div style={{ marginTop: 12, display: 'flex', justifyContent: 'flex-end' }}>
                      <button
                        className="hunt-trigger-btn"
                        onClick={() => onHunt(a)}
                      >
                        Hunt
                      </button>
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>

          {/* Correlation rule */}
          <div className="gen-section-title">Correlation rule</div>
          <div className="db-corr-card">
            <div className="db-corr-head">
              <div className="db-corr-name">{result.correlation.name}</div>
              <div className={`ac-sev ac-sev-${result.correlation.severity}`}>
                {result.correlation.severity}
              </div>
            </div>
            <div className="db-corr-logic mono">{result.correlation.logic}</div>
            <div className="db-corr-window">
              <span className="db-cw-label">Global window</span>
              <span className="db-cw-val mono">{result.correlation.window}</span>
            </div>
          </div>

          {/* Actions */}
          <div className="gen-actions">
            <button className="btn btn-primary" onClick={handleSave} disabled={saved}>
              {saved ? '✓ Copied to clipboard' : 'Save to detections →'}
            </button>
            <button className="btn" onClick={() => result && downloadYaml(result)}>
              Export YAML
            </button>
          </div>
        </>
      )}
    </aside>
  );
}

// ── GenSkeleton ──────────────────────────────────────────────────────────────

function GenSkeleton() {
  return (
    <div className="gen-skel">
      <div className="skel-block sk-1" />
      <div className="skel-block sk-2" />
      <div className="skel-block sk-3" />
      <div className="skel-block sk-3" />
    </div>
  );
}
