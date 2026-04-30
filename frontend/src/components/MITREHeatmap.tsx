/**
 * MITREHeatmap — MITRE ATT&CK coverage view.
 *
 * Layout: two modes toggled by the user.
 *   1. Heatmap grid  — the original tactic-column layout (default).
 *   2. Force graph   — progressive disclosure: 14 tactic nodes, click to expand techniques.
 */

import { useState, useRef, useEffect, useMemo } from 'react';
import type { MITRECoverageResult, NavigatorTechnique, SuggestionsResponse } from '../types';
import { fetchSuggestions } from '../services/api';

interface Props {
  data: MITRECoverageResult;
  clientName: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const TACTICS_ORDER = [
  'reconnaissance',
  'resource-development',
  'initial-access',
  'execution',
  'persistence',
  'privilege-escalation',
  'defense-evasion',
  'credential-access',
  'discovery',
  'lateral-movement',
  'collection',
  'command-and-control',
  'exfiltration',
  'impact',
];

const TACTIC_LABELS: Record<string, string> = {
  'reconnaissance':       'Reconnaissance',
  'resource-development': 'Resource Development',
  'initial-access':       'Initial Access',
  'execution':            'Execution',
  'persistence':          'Persistence',
  'privilege-escalation': 'Privilege Escalation',
  'defense-evasion':      'Defense Evasion',
  'credential-access':    'Credential Access',
  'discovery':            'Discovery',
  'lateral-movement':     'Lateral Movement',
  'collection':           'Collection',
  'command-and-control':  'Command & Control',
  'exfiltration':         'Exfiltration',
  'impact':               'Impact',
};

const HEATMAP_BODY_MAX_PX = 900; // px — caps scroll container so tallest columns (~1308px of content) always scroll; activates when available height exceeds 900px

// ---------------------------------------------------------------------------
// Colour helpers
// ---------------------------------------------------------------------------

function coverageColor(percent: number): string {
  if (percent === 0)  return '#7f1d1d';  // --cov-none (uncovered = red)
  if (percent < 25)  return '#7c2d12';  // --cov-low
  if (percent < 50)  return '#92400e';  // --cov-partial
  if (percent < 75)  return '#065f46';  // --cov-good
  return '#10b981';                      // --cov-full
}


function priorityColor(priority: string): string {
  switch (priority) {
    case 'critical': return 'var(--danger)';
    case 'high':     return 'var(--warn)';
    case 'medium':   return 'var(--accent)';
    default:         return 'var(--text-dim)';
  }
}

function shortName(name: string): string {
  const trimmed = name.split('(')[0].split('/')[0].trim();
  return trimmed.length > 18 ? trimmed.slice(0, 17) + '\u2026' : trimmed;
}

// ---------------------------------------------------------------------------
// Position helpers (pure — no simulation)
// ---------------------------------------------------------------------------

/**
 * Places `tactics` in a 7-column × 2-row grid, evenly spaced.
 * Returns a map of tactic slug → { cx, cy } centre coordinates.
 */
function tacticGridPositions(
  tactics: string[],
  width: number,
  height: number,
): Record<string, { cx: number; cy: number }> {
  const colCount = 7;
  const rowCount = Math.ceil(tactics.length / colCount);
  const colSpacing = width / (colCount + 1);
  const rowSpacing = height / (rowCount + 1);
  const result: Record<string, { cx: number; cy: number }> = {};
  tactics.forEach((tactic, i) => {
    result[tactic] = {
      cx: colSpacing * ((i % colCount) + 1),
      cy: rowSpacing * (Math.floor(i / colCount) + 1),
    };
  });
  return result;
}

/**
 * Fans `count` technique nodes evenly around a circle centred on (cx, cy).
 * Radius = max(90, sqrt(count) * 30). Clamps nodes to stay within canvas bounds.
 */
function techniqueRadialPositions(
  cx: number,
  cy: number,
  count: number,
  canvasWidth: number,
  canvasHeight: number,
): Array<{ cx: number; cy: number }> {
  if (count === 0) return [];
  const pad = 20;
  const nodeR = 16;
  const radius = Math.max(90, Math.sqrt(count) * 30);
  return Array.from({ length: count }, (_, i) => {
    const angle = -Math.PI / 2 + (i / count) * 2 * Math.PI;
    return {
      cx: Math.max(pad + nodeR, Math.min(canvasWidth  - pad - nodeR, cx + radius * Math.cos(angle))),
      cy: Math.max(pad + nodeR, Math.min(canvasHeight - pad - nodeR, cy + radius * Math.sin(angle))),
    };
  });
}

// ---------------------------------------------------------------------------
// Force Graph component
// ---------------------------------------------------------------------------

function ForceGraph({
  techniques,
  onSelectTechnique,
  selectedId,
}: {
  techniques: NavigatorTechnique[];
  onSelectTechnique: (t: NavigatorTechnique | null) => void;
  selectedId: string | null;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ width: 800, height: 540 });
  const [expandedTactic, setExpandedTactic] = useState<string | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;
    const obs = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDims({ width: Math.max(400, width), height: Math.max(300, height) });
    });
    obs.observe(containerRef.current);
    return () => obs.disconnect();
  }, []);

  useEffect(() => {
    // Reset expanded tactic when the technique dataset changes (e.g. client switch)
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setExpandedTactic(null);
  }, [techniques]);

  // Group techniques by tactic
  const tacticMap = useMemo(() => {
    const map: Record<string, NavigatorTechnique[]> = {};
    for (const t of techniques) {
      if (!t.tactic) continue;
      if (!map[t.tactic]) map[t.tactic] = [];
      map[t.tactic].push(t);
    }
    return map;
  }, [techniques]);

  const activeTactics = useMemo(
    () => TACTICS_ORDER.filter((tac) => tacticMap[tac] !== undefined),
    [tacticMap],
  );

  const gridPos = useMemo(
    () => tacticGridPositions(activeTactics, dims.width, dims.height),
    [activeTactics, dims.width, dims.height],
  );

  // Positions for the currently expanded tactic's techniques
  const expandedTechs  = expandedTactic ? (tacticMap[expandedTactic] ?? []) : [];
  const coveredTechs   = expandedTechs.filter(t => t.score > 0);
  const uncoveredCount = expandedTechs.length - coveredTechs.length;
  const nodeCount      = coveredTechs.length + (uncoveredCount > 0 ? 1 : 0);
  const expandedCenter = expandedTactic ? gridPos[expandedTactic] : null;
  const techPos = expandedCenter
    ? techniqueRadialPositions(
        expandedCenter.cx,
        expandedCenter.cy,
        nodeCount,
        dims.width,
        dims.height,
      )
    : [];

  const handleTacticClick = (tactic: string) => {
    if ((tacticMap[tactic]?.length ?? 0) === 0) return;
    setExpandedTactic((prev) => (prev === tactic ? null : tactic));
    onSelectTechnique(null);
  };

  if (techniques.length === 0) {
    return (
      <div ref={containerRef} className="force-graph-container">
        <svg width={dims.width} height={dims.height} className="force-graph-svg">
          <text
            x={dims.width / 2}
            y={dims.height / 2}
            textAnchor="middle"
            fill="#00ff6466"
            fontSize={14}
            fontFamily="'IBM Plex Mono', monospace"
          >
            No technique data
          </text>
        </svg>
      </div>
    );
  }

  return (
    <div ref={containerRef} className="force-graph-container">
      <svg
        width={dims.width}
        height={dims.height}
        className="force-graph-svg"
        role="img"
        aria-label="MITRE technique force graph"
        onClick={() => setExpandedTactic(null)}
      >
        {/* Edges: expanded tactic → covered techniques + optional summary node */}
        {expandedCenter && coveredTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={`edge:${t.techniqueID}:${t.tactic}`}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(0,255,100,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}
        {expandedCenter && uncoveredCount > 0 && (() => {
          const pos = techPos[coveredTechs.length];
          if (!pos) return null;
          return (
            <line
              key="edge:uncovered-summary"
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(128,128,128,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })()}

        {/* Technique nodes — covered techniques for the expanded tactic */}
        {coveredTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          const nodeId = `tech:${t.techniqueID}:${t.tactic}`;
          const isSelected = selectedId === nodeId;
          const textFill = t.score <= 50 ? '#fff' : '#000';
          return (
            <g
              key={nodeId}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); onSelectTechnique(isSelected ? null : t); }}
              style={{ cursor: 'pointer' }}
              className={`force-node${isSelected ? ' force-node--selected' : ''}`}
              role="button"
              aria-label={t.techniqueID}
            >
              <circle
                r={16}
                fill={t.color}
                stroke={isSelected ? '#fff' : 'rgba(0,255,100,0.5)'}
                strokeWidth={isSelected ? 2 : 1.5}
              />
              <text
                dy="0.35em"
                textAnchor="middle"
                fontSize={7.5}
                fill={textFill}
                fontFamily="'IBM Plex Mono', monospace"
                fontWeight="600"
                style={{ pointerEvents: 'none', userSelect: 'none' }}
              >
                {t.techniqueID}
              </text>
            </g>
          );
        })}
        {/* Summary node for uncovered techniques */}
        {expandedCenter && uncoveredCount > 0 && (() => {
          const pos = techPos[coveredTechs.length];
          if (!pos) return null;
          return (
            <g
              key="uncovered-summary"
              transform={`translate(${pos.cx},${pos.cy})`}
              style={{ pointerEvents: 'none' }}
              aria-label={`${uncoveredCount} uncovered techniques`}
            >
              <circle r={14} fill="var(--surface-2)" />
              <text
                dy="0.35em"
                textAnchor="middle"
                fontSize={7}
                fill="#fff"
                fontFamily="'IBM Plex Mono', monospace"
                fontWeight="600"
                style={{ userSelect: 'none' }}
              >
                +{uncoveredCount}
              </text>
              <text
                textAnchor="middle"
                y={22}
                fontSize={5.5}
                fill="rgba(255,255,255,0.6)"
                fontFamily="'IBM Plex Mono', monospace"
                style={{ userSelect: 'none' }}
              >
                uncovered
              </text>
            </g>
          );
        })()}

        {/* Tactic nodes — always visible, dimmed when another is expanded */}
        {activeTactics.map((tactic) => {
          const pos = gridPos[tactic];
          if (!pos) return null;
          const techs   = tacticMap[tactic] ?? [];
          const covered = techs.filter((t) => t.score > 0).length;
          const total   = techs.length;
          const pct     = total > 0 ? (covered / total) * 100 : 0;
          const isExpanded = expandedTactic === tactic;
          const isDimmed   = expandedTactic !== null && !isExpanded;
          const label  = TACTIC_LABELS[tactic] ?? tactic;
          const words  = label.split(' ');
          const lineH  = 10;
          // Vertically centre the word stack, then add coverage line below
          const startDy = -(words.length - 1) * lineH / 2;

          return (
            <g
              key={tactic}
              transform={`translate(${pos.cx},${pos.cy})`}
              onClick={(e) => { e.stopPropagation(); handleTacticClick(tactic); }}
              style={{
                cursor: total > 0 ? 'pointer' : 'default',
                opacity: isDimmed ? 0.3 : 1,
                transition: 'opacity 0.2s ease',
              }}
              className="force-node force-node--tactic"
              aria-label={`${label}: ${covered} of ${total} covered`}
            >
              <circle
                r={34}
                fill={isExpanded ? 'rgba(0,255,100,0.15)' : 'rgba(0,255,100,0.08)'}
                stroke="#00ff64"
                strokeWidth={isExpanded ? 2 : 1.5}
              />
              <text
                textAnchor="middle"
                fontFamily="'IBM Plex Mono', monospace"
                fontWeight="600"
                fontSize={8}
                fill="#00ff64"
                style={{ pointerEvents: 'none', userSelect: 'none' }}
              >
                {words.map((w, wi) => (
                  <tspan key={wi} x={0} dy={wi === 0 ? startDy : lineH}>
                    {w}
                  </tspan>
                ))}
                <tspan x={0} dy={lineH} fontSize={6.5} fill={coverageColor(pct)} fontWeight="400">
                  {covered}/{total}
                </tspan>
              </text>
            </g>
          );
        })}
      </svg>

      <div className="force-graph-legend">
        <span className="force-legend-item force-legend-item--tactic">Tactic</span>
        <span className="force-legend-item force-legend-item--covered">Covered</span>
        <span className="force-legend-item force-legend-item--partial">Partial</span>
        <span className="force-legend-item force-legend-item--uncovered">Uncovered</span>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Suggestions panel (reused by both views)
// ---------------------------------------------------------------------------

function SuggestionsPanel({
  technique,
  clientName,
}: {
  technique: NavigatorTechnique;
  clientName: string;
}) {
  const [suggestions, setSuggestions] = useState<SuggestionsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [provider, setProvider] = useState('');

  const generate = async (force = false) => {
    setSuggestions(null);
    setError(null);
    setLoading(true);
    try {
      const result = await fetchSuggestions(
        clientName,
        technique.techniqueID,
        technique.tactic,
        provider || undefined,
        force,
      );
      setSuggestions(result);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to generate suggestions');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="suggestions-section">
      <div className="suggestions-controls">
        <select
          className="provider-select"
          value={provider}
          onChange={(e) => { setProvider(e.target.value); setSuggestions(null); }}
          disabled={loading}
        >
          <option value="">Claude Opus (default)</option>
          <option value="nvidia">NVIDIA NIM (Nemotron)</option>
          <option value="gemini">Gemini 2.0 Flash</option>
        </select>
        <button
          className="btn-generate"
          onClick={() => generate(true)}
          disabled={loading}
        >
          {loading ? 'Generating...' : 'Generate Suggestions'}
        </button>
      </div>

      {error && (
        <div className="state-error" style={{ marginBottom: 10 }}>
          <span className="state-error__icon">⚠</span>
          <div>
            <div className="state-error__title">Query generation failed</div>
            <div className="state-error__body">The provider returned an error. Try a different provider.</div>
          </div>
        </div>
      )}

      {suggestions && (
        <div className="suggestions-list">
          <div className="suggestions-header">
            <span>
              {suggestions.suggestions.length} suggestion
              {suggestions.suggestions.length !== 1 ? 's' : ''}
            </span>
            <span className="suggestions-provider">via {suggestions.provider}</span>
            <button
              className="btn btn-small"
              onClick={() => generate(true)}
              disabled={loading}
            >
              Regenerate
            </button>
          </div>

          {suggestions.suggestions.length === 0 ? (
            <div className="suggestions-empty">
              No alerts can be created for this technique with the available log sources.
            </div>
          ) : (
            suggestions.suggestions.map((s, i) => (
              <div key={i} className="suggestion-card">
                <div className="suggestion-header">
                  <span className="suggestion-name">{s.alert_name}</span>
                  <span
                    className="suggestion-priority"
                    style={{ color: priorityColor(s.priority) }}
                  >
                    {s.priority}
                  </span>
                </div>
                <div className="suggestion-source">Log source: {s.log_source}</div>
                <div className="suggestion-desc">{s.description}</div>
                <div className="suggestion-query">
                  <code>{s.query_hint}</code>
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Detail panel (shared between heatmap + graph views)
// ---------------------------------------------------------------------------

function TechniqueDetailPanel({
  technique,
  clientName,
  onClose,
}: {
  technique: NavigatorTechnique;
  clientName: string;
  onClose: () => void;
}) {
  return (
    <div
      className="detail-panel"
      onClick={(e) => e.stopPropagation()}
    >
      <div className="detail-panel-header">
        <div>
          <div className="detail-tech-id">{technique.techniqueID}</div>
          <div className="detail-tech-name">{technique.name || technique.techniqueID}</div>
          <div className="detail-tactic">{TACTIC_LABELS[technique.tactic] ?? technique.tactic}</div>
        </div>
        <button className="detail-close" onClick={onClose} aria-label="Close detail panel">×</button>
      </div>

      <div className="detail-panel-body">
        {technique.score === 100 ? (
          <div>
            <div className="detail-panel-empty" style={{ paddingBottom: 8 }}>
              Covered by existing alerts:
            </div>
            <div className="covered-by-list">
              {(technique.comment || '')
                .replace(/^Covered by \d+ alert\(s\):\s*/, '')
                .split(', ')
                .filter(Boolean)
                .map((name, i) => (
                  <div key={i} className="covered-by-item">{name}</div>
                ))}
            </div>
          </div>
        ) : (
          <div className="detail-suggestion-bar">
            {technique.score > 0 && (
              <div className="detail-panel-empty" style={{ paddingBottom: 8 }}>
                Partially covered by existing alerts:
                <div className="covered-by-list" style={{ marginTop: 4 }}>
                  {(technique.comment || '')
                    .replace(/^Covered by \d+ alert\(s\):\s*/, '')
                    .split(', ')
                    .filter(Boolean)
                    .map((name, i) => (
                      <div key={i} className="covered-by-item">{name}</div>
                    ))}
                </div>
              </div>
            )}
            <SuggestionsPanel technique={technique} clientName={clientName} />
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

type ViewMode = 'heatmap' | 'graph';

export default function MITREHeatmap({ data, clientName }: Props) {
  const [viewMode, setViewMode] = useState<ViewMode>('heatmap');
  const [selectedTechnique, setSelectedTechnique] = useState<NavigatorTechnique | null>(null);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);

  const containerRef       = useRef<HTMLDivElement>(null);
  const toolbarRef         = useRef<HTMLDivElement>(null);
  const [heatmapBodyHeight, setHeatmapBodyHeight] = useState(0);

  const { summary, navigator_layer: layer } = data;

  // Build tactic-keyed technique map
  const techniquesByTactic: Record<string, NavigatorTechnique[]> = {};
  for (const t of layer.techniques) {
    const tactic = t.tactic || 'unknown';
    if (!techniquesByTactic[tactic]) techniquesByTactic[tactic] = [];
    techniquesByTactic[tactic].push(t);
  }
  for (const tactic of Object.keys(techniquesByTactic)) {
    techniquesByTactic[tactic].sort((a, b) => b.score - a.score);
  }

  const downloadLayer = () => {
    const blob = new Blob([JSON.stringify(layer, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'mitre_coverage_layer.json';
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleSelectTechnique = (technique: NavigatorTechnique | null) => {
    setSelectedTechnique(technique);
  };

  // Derive force-graph selected node id
  const selectedNodeId = selectedTechnique
    ? `tech:${selectedTechnique.techniqueID}:${selectedTechnique.tactic}`
    : null;

  // Measure available height for the heatmap scroll area.
  // Uses window.innerHeight − toolbar.bottom (viewport-relative) instead of
  // container.height − toolbar.height, which can be content-inflated on first
  // render before the ResizeObserver fires.
  // HEATMAP_BODY_MAX_PX caps the height on tall monitors (≥ 1440p) to keep
  // all tactic columns shorter than their tallest content (~1308px) so that
  // per-column overflow-y:auto always triggers scroll. On smaller monitors
  // (MacBook Pro, 1080p) the available height is already < 900px so no cap
  // is applied and the heatmap fills the full available viewport.
  useEffect(() => {
    const update = () => {
      if (!toolbarRef.current) return;
      const toolbarBottom = toolbarRef.current.getBoundingClientRect().bottom;
      const available     = window.innerHeight - toolbarBottom;
      setHeatmapBodyHeight(Math.min(available, HEATMAP_BODY_MAX_PX));
    };
    const obs = new ResizeObserver(update);
    if (containerRef.current) obs.observe(containerRef.current);
    if (toolbarRef.current)   obs.observe(toolbarRef.current);
    update();
    return () => obs.disconnect();
  }, []);

  return (
    <div className="mitre-heatmap" ref={containerRef}>
      {/* Toolbar */}
      <div className="mitre-toolbar" ref={toolbarRef}>
        <div className="mitre-stats">
          <div className="mitre-stat">
            <div className="mitre-stat-val mitre-stat-val--accent">
              {summary?.coverage_percent != null
                ? `${Math.round(summary.coverage_percent)}%`
                : '—'}
            </div>
            <div className="mitre-stat-label">Overall</div>
          </div>
          <div className="mitre-stat-divider" />
          <div className="mitre-stat">
            <div className="mitre-stat-val">{summary?.total_techniques ?? '—'}</div>
            <div className="mitre-stat-label">Techniques</div>
          </div>
          <div className="mitre-stat-divider" />
          <div className="mitre-stat">
            <div className="mitre-stat-val mitre-stat-val--warn">
              {summary?.total_techniques != null && summary?.covered_techniques != null
                ? summary.total_techniques - summary.covered_techniques
                : '—'}
            </div>
            <div className="mitre-stat-label">Uncovered</div>
          </div>
          <div className="mitre-stat-divider" />
          <div className="mitre-stat">
            <div className="mitre-stat-val">{summary?.total_sub_techniques ?? '—'}</div>
            <div className="mitre-stat-label">Sub-techniques</div>
          </div>
        </div>

        <div className="mitre-toolbar-right">
          <div className="mitre-legend">
            {(['None','Low','Partial','Good','Full'] as const).map((lbl, i) => {
              const colors = ['#1e2535','#7c2d12','#92400e','#065f46','#10b981'];
              return (
                <div key={lbl} className="mitre-legend-item">
                  <div className="mitre-legend-dot" style={{ background: colors[i] }} />
                  <span className="mitre-legend-label">{lbl}</span>
                </div>
              );
            })}
          </div>
          <div className="mitre-stat-divider" />
          <div className="view-toggle">
            <button
              className={`view-toggle-btn${viewMode === 'heatmap' ? ' view-toggle-btn--active' : ''}`}
              onClick={() => setViewMode('heatmap')}
            >
              Heatmap
            </button>
            <button
              className={`view-toggle-btn${viewMode === 'graph' ? ' view-toggle-btn--active' : ''}`}
              onClick={() => setViewMode('graph')}
            >
              Graph
            </button>
          </div>
          <button className="mitre-download-btn" onClick={downloadLayer}>
            ↓ ATT&CK Layer
          </button>
        </div>
      </div>

      {/* ── Heatmap view ── */}
      {viewMode === 'heatmap' && (
        <div
          className="heatmap-scroll-wrap"
          style={{ height: heatmapBodyHeight > 0 ? heatmapBodyHeight : undefined }}
        >
          <div className="heatmap-columns">
            {TACTICS_ORDER.map((tactic) => {
              const tacticData = summary.tactic_breakdown[tactic];
              const techniques = techniquesByTactic[tactic] || [];
              const pct = tacticData?.percent ?? 0;

              return (
                <div key={tactic} className="tactic-col">
                  <div
                    className="tactic-header"
                    style={{ '--cov-color': coverageColor(pct) } as React.CSSProperties}
                  >
                    <div className="tactic-name">{TACTIC_LABELS[tactic] ?? tactic}</div>
                    <div className={`tactic-count${tacticData?.covered === 0 ? ' tactic-count--zero' : ''}`}>
                      <em>{tacticData?.covered ?? 0}</em>/{tacticData?.total ?? 0} covered
                    </div>
                  </div>

                  <div className="tactic-techniques">
                    {techniques.map((t) => {
                      const isActive =
                        selectedTechnique?.techniqueID === t.techniqueID &&
                        selectedTechnique?.tactic === t.tactic;
                      return (
                        <div
                          key={`${t.techniqueID}-${t.tactic}`}
                          className={`tech-cell${isActive ? ' tech-cell--selected' : ''}`}
                          style={{ background: coverageColor(t.score) }}
                          onClick={(e) => {
                            e.stopPropagation();
                            handleSelectTechnique(isActive ? null : t);
                          }}
                          onMouseEnter={(e) => setTooltip({
                            x: e.clientX + 12,
                            y: e.clientY - 8,
                            text: `${t.techniqueID} · ${t.name ?? t.techniqueID}`,
                          })}
                          onMouseLeave={() => setTooltip(null)}
                        >
                          <span className="tech-id">{t.techniqueID}</span>
                          <span className="tech-name">{shortName(t.name ?? '')}</span>
                        </div>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {tooltip && (
        <div
          className="tech-tooltip"
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          {tooltip.text}
        </div>
      )}

      {/* ── Force graph view ── */}
      {viewMode === 'graph' && (
        <ForceGraph
          techniques={layer.techniques}
          onSelectTechnique={handleSelectTechnique}
          selectedId={selectedNodeId}
        />
      )}

      {/* Detail panel — shared across views */}
      {selectedTechnique && (
        <TechniqueDetailPanel
          technique={selectedTechnique}
          clientName={clientName}
          onClose={() => handleSelectTechnique(null)}
        />
      )}
    </div>
  );
}
