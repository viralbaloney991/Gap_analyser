import { useState, useEffect, useMemo, useCallback, Fragment, useRef } from 'react';
import { MITRE_TACTICS, MITRE_TECHNIQUES, mockGenerate } from '../data/mitre-catalog';
import type { MITRETechnique } from '../data/mitre-catalog';
import { buildDetection } from '../services/api';
import type { GenerationResult, FlowAlert } from '../types';

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

// ── Types ────────────────────────────────────────────────────────────────────

interface Props {
  clientName: string;
}

// ── Root component ───────────────────────────────────────────────────────────

export default function DetectionBuilder({ clientName }: Props) {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [query, setQuery]             = useState('');
  const [generating, setGenerating]   = useState(false);
  const [result, setResult]           = useState<GenerationResult | null>(null);

  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  const selected = useMemo(
    () => MITRE_TECHNIQUES.filter(t => selectedIds.has(t.id)),
    [selectedIds],
  );

  const addTech = useCallback((t: MITRETechnique) => {
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
      const t = MITRE_TECHNIQUES.find(x => x.id === id);
      if (t) addTech(t);
    };
    window.addEventListener('basket-drop', onDrop);
    return () => window.removeEventListener('basket-drop', onDrop);
  }, [addTech]);

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
        {/* Left: source matrix */}
        <div className="builder-source">
          <div className="bs-head">
            <span className="bs-title">MITRE ATT&amp;CK Techniques</span>
            <span className="bs-hint">Drag to chain · double-click to add</span>
          </div>
          <SourceMatrix selectedIds={selectedIds} onAdd={addTech} query={query} />
        </div>

        {/* Right: basket + generated panel */}
        <div className="builder-right">
          <Basket
            selected={selected}
            onRemove={removeTech}
            onClear={clearAll}
            onGenerate={handleGenerate}
            generating={generating}
          />
          <GeneratedPanel
            result={result}
            generating={generating}
            onClose={() => setResult(null)}
            onRegenerate={handleGenerate}
          />
        </div>
      </div>
    </div>
  );
}

// ── SourceMatrix ─────────────────────────────────────────────────────────────

interface SourceMatrixProps {
  selectedIds: Set<string>;
  onAdd: (t: MITRETechnique) => void;
  query: string;
}

function SourceMatrix({ selectedIds, onAdd, query }: SourceMatrixProps) {
  const grouped = MITRE_TACTICS.map(tac => ({
    ...tac,
    techs: MITRE_TECHNIQUES.filter(t =>
      t.tactic === tac.id &&
      (!query ||
        t.id.toLowerCase().includes(query.toLowerCase()) ||
        t.name.toLowerCase().includes(query.toLowerCase()))
    ),
  })).filter(tac => tac.techs.length > 0);

  const handleDragStart = (e: React.DragEvent, tech: MITRETechnique) => {
    e.dataTransfer.setData('text/plain', tech.id);
    e.dataTransfer.effectAllowed = 'copy';
  };

  return (
    <div className="src-matrix">
      {grouped.map(tac => (
        <div key={tac.id} className="src-col">
          <div className="src-col-head">
            <span className="src-col-name">{tac.short}</span>
            <span className={cls('src-col-count', 'mono')}>{tac.techs.length}</span>
          </div>
          <div className="src-col-body">
            {tac.techs.map(t => {
              const used = selectedIds.has(t.id);
              return (
                <div
                  key={t.id}
                  className={cls('tech-card', used && 'used')}
                  draggable={!used}
                  onDragStart={e => handleDragStart(e, t)}
                  onDoubleClick={() => !used && onAdd(t)}
                >
                  <div className="tc-id mono">{t.id}</div>
                  <div className="tc-name">{t.name}</div>
                  <div className="tc-source">{t.source}</div>
                  {used && <div className="tc-check">✓</div>}
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

// ── Basket ───────────────────────────────────────────────────────────────────

interface BasketProps {
  selected: MITRETechnique[];
  onRemove: (id: string) => void;
  onClear: () => void;
  onGenerate: () => void;
  generating: boolean;
}

function Basket({ selected, onRemove, onClear, onGenerate, generating }: BasketProps) {
  const [dragOver, setDragOver] = useState(false);

  const ordered = [...selected].sort((a, b) => a.tacticOrder - b.tacticOrder);

  // Group by tactic.
  const tacticGroups: Array<{ tactic: typeof MITRE_TACTICS[0]; techs: MITRETechnique[] }> = [];
  const seen = new Map<string, number>();
  for (const t of ordered) {
    if (!seen.has(t.tactic)) {
      const tac = MITRE_TACTICS.find(x => x.id === t.tactic)!;
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

// ── GeneratedPanel ───────────────────────────────────────────────────────────

interface GeneratedPanelProps {
  result: GenerationResult | null;
  generating: boolean;
  onClose: () => void;
  onRegenerate: () => void;
}

function GeneratedPanel({ result, generating, onClose, onRegenerate }: GeneratedPanelProps) {
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
                    {f.level === 'error' ? '✕' : f.level === 'warn' ? '!' : 'i'}
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
                  <div className="ac-logic">{a.logic}</div>
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

          {/* Actions (stubs for v1) */}
          <div className="gen-actions">
            <button className="btn btn-primary" onClick={() => console.log('save', result)}>
              Save to detections →
            </button>
            <button className="btn" onClick={() => console.log('export', result)}>
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
