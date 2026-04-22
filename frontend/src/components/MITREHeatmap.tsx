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

// ---------------------------------------------------------------------------
// Colour helpers
// ---------------------------------------------------------------------------

function coverageColor(percent: number): string {
  if (percent === 0)   return '#ff0000';
  if (percent <= 50)  return '#ff8c00';
  if (percent <= 75)  return '#ffd700';
  return '#00cc00';
}

function coverageLabel(percent: number): string {
  if (percent === 0)   return 'Not Covered';
  if (percent <= 50)  return 'Low Coverage';
  if (percent <= 75)  return 'Moderate Coverage';
  return 'Good Coverage';
}

function priorityColor(priority: string): string {
  switch (priority) {
    case 'critical': return '#ff0000';
    case 'high':     return '#ff8c00';
    case 'medium':   return '#ffd700';
    case 'low':      return '#888888';
    default:         return '#aaaaaa';
  }
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
  const expandedTechs = expandedTactic ? (tacticMap[expandedTactic] ?? []) : [];
  const expandedCenter = expandedTactic ? gridPos[expandedTactic] : null;
  const techPos = expandedCenter
    ? techniqueRadialPositions(
        expandedCenter.cx,
        expandedCenter.cy,
        expandedTechs.length,
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
        {/* Edges: expanded tactic → its techniques */}
        {expandedCenter && expandedTechs.map((_, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          return (
            <line
              key={`edge:${expandedTechs[i].techniqueID}:${expandedTechs[i].tactic}`}
              x1={expandedCenter.cx} y1={expandedCenter.cy}
              x2={pos.cx}           y2={pos.cy}
              stroke="rgba(0,255,100,0.3)"
              strokeWidth={1}
              style={{ pointerEvents: 'none' }}
            />
          );
        })}

        {/* Technique nodes — only for the expanded tactic */}
        {expandedTechs.map((t, i) => {
          const pos = techPos[i];
          if (!pos) return null;
          const nodeId = `tech:${t.techniqueID}:${t.tactic}`;
          const isSelected = selectedId === nodeId;
          // White text on red/orange (score ≤ 50), black on yellow/green
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
          onChange={(e) => setProvider(e.target.value)}
          disabled={loading}
        >
          <option value="">Mistral Small (default)</option>
          <option value="nvidia">NVIDIA (Qwen)</option>
          <option value="claude">Claude (Haiku)</option>
          <option value="gemini">Gemini 2.0 Flash</option>
        </select>
        <button
          className="btn btn-generate"
          onClick={() => generate()}
          disabled={loading}
        >
          {loading ? 'Generating...' : 'Generate Suggestions'}
        </button>
      </div>

      {error && <div className="suggestions-error">{error}</div>}

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
      className="technique-detail-panel"
      onClick={(e) => e.stopPropagation()}
    >
      <div className="detail-panel-header">
        <strong>
          {technique.techniqueID} &mdash; {technique.name || technique.techniqueID}
        </strong>
        <button className="detail-close" onClick={onClose} aria-label="Close detail panel">
          x
        </button>
      </div>

      <div className="detail-panel-body">
        <div className="detail-row">
          <span className="detail-label">Tactic</span>
          <span className="detail-value">
            {TACTIC_LABELS[technique.tactic] ?? technique.tactic}
          </span>
        </div>
        <div className="detail-row">
          <span className="detail-label">Score</span>
          <span className="detail-value" style={{ color: coverageColor(technique.score) }}>
            {technique.score}% &mdash; {coverageLabel(technique.score)}
          </span>
        </div>
        {technique.comment && (
          <div className="detail-row detail-row-comment">
            <span className="detail-label">Alerts</span>
            <span className="detail-value">{technique.comment}</span>
          </div>
        )}

        {technique.score === 0 && (
          <SuggestionsPanel technique={technique} clientName={clientName} />
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
  const [selectedTactic, setSelectedTactic] = useState<string | null>(null);
  const [selectedTechnique, setSelectedTechnique] = useState<NavigatorTechnique | null>(null);

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

  return (
    <div className="mitre-heatmap">
      {/* Header */}
      <div className="mitre-header">
        <h2>MITRE ATT&amp;CK Coverage</h2>
        <div className="mitre-header-actions">
          <div className="view-toggle">
            <button
              className={`view-toggle-btn${viewMode === 'heatmap' ? ' active' : ''}`}
              onClick={() => setViewMode('heatmap')}
            >
              Heatmap
            </button>
            <button
              className={`view-toggle-btn${viewMode === 'graph' ? ' active' : ''}`}
              onClick={() => setViewMode('graph')}
            >
              Force Graph
            </button>
          </div>
          <button className="btn btn-small" onClick={downloadLayer}>
            Download Layer
          </button>
        </div>
      </div>

      {/* Summary Bar */}
      <div className="coverage-summary">
        <div className="summary-stat">
          <span className="stat-value">{summary.covered_techniques}</span>
          <span className="stat-label">/ {summary.total_techniques} Techniques</span>
        </div>
        <div className="summary-stat">
          <span className="stat-value">{summary.covered_sub_techniques}</span>
          <span className="stat-label">/ {summary.total_sub_techniques} Sub-techniques</span>
        </div>
        <div className="summary-stat">
          <span
            className="stat-value"
            style={{ color: coverageColor(summary.coverage_percent) }}
          >
            {summary.coverage_percent.toFixed(1)}%
          </span>
          <span className="stat-label">Overall Coverage</span>
        </div>
      </div>

      {/* Legend */}
      <div className="legend">
        {layer.legendItems?.map((item) => (
          <div key={item.label} className="legend-item">
            <span className="legend-color" style={{ backgroundColor: item.color }} />
            <span>{item.label}</span>
          </div>
        ))}
      </div>

      {/* ── Heatmap view ── */}
      {viewMode === 'heatmap' && (
        <div className="tactic-grid">
          {TACTICS_ORDER.map((tactic) => {
            const tacticData = summary.tactic_breakdown[tactic];
            const techniques = techniquesByTactic[tactic] || [];
            const isSelected = selectedTactic === tactic;
            const pct = tacticData?.percent ?? 0;

            return (
              <div
                key={tactic}
                className={`tactic-column${isSelected ? ' selected' : ''}`}
                onClick={() => setSelectedTactic(isSelected ? null : tactic)}
              >
                <div
                  className="tactic-header"
                  style={{ borderTopColor: coverageColor(pct) }}
                >
                  <div className="tactic-name">
                    {TACTIC_LABELS[tactic] ?? tactic}
                  </div>
                  <div
                    className="tactic-coverage"
                    style={{ color: coverageColor(pct) }}
                  >
                    {tacticData
                      ? `${tacticData.covered}/${tacticData.total}`
                      : '0/0'}
                  </div>
                  {tacticData && tacticData.total_subs > 0 && (
                    <div className="tactic-subs">
                      {tacticData.covered_subs}/{tacticData.total_subs} subs
                    </div>
                  )}
                </div>

                <div className="technique-cells">
                  {techniques.map((t) => {
                    const isActive =
                      selectedTechnique?.techniqueID === t.techniqueID &&
                      selectedTechnique?.tactic === t.tactic;
                    return (
                      <div
                        key={`${t.techniqueID}-${t.tactic}`}
                        className={`technique-cell${isActive ? ' active' : ''}`}
                        style={{ backgroundColor: t.color }}
                        onClick={(e) => {
                          e.stopPropagation();
                          handleSelectTechnique(isActive ? null : t);
                        }}
                        title={`${t.techniqueID} — ${t.name || t.techniqueID}`}
                        role="button"
                        tabIndex={0}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            handleSelectTechnique(isActive ? null : t);
                          }
                        }}
                      >
                        <span className="technique-id">{t.techniqueID}</span>
                      </div>
                    );
                  })}
                </div>
              </div>
            );
          })}
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
