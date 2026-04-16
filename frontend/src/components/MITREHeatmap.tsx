/**
 * MITREHeatmap — MITRE ATT&CK coverage view.
 *
 * Layout: two modes toggled by the user.
 *   1. Heatmap grid  — the original tactic-column layout (default).
 *   2. Force graph   — D3 force-directed SVG graph (Barnes-Hut repulsion, spring links).
 */

import { useState, useRef, useEffect, useCallback, useMemo } from 'react';
import * as d3 from 'd3';
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
// Minimal force-directed simulation (no D3 dependency)
// ---------------------------------------------------------------------------

interface ForceNode {
  id: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  radius: number;
  color: string;
  label: string;
  score: number;
  tactic: string;
  isTactic: boolean;
}

interface ForceEdge {
  source: string;
  target: string;
}

function buildForceGraph(techniques: NavigatorTechnique[], width: number, height: number) {
  const cx = width / 2;
  const cy = height / 2;

  // Tactic nodes arranged in a circle
  const tacticSet = [...new Set(techniques.map((t) => t.tactic).filter(Boolean))];
  const tacticNodes: ForceNode[] = tacticSet.map((tactic, i) => {
    const angle = (i / tacticSet.length) * 2 * Math.PI - Math.PI / 2;
    const r = Math.min(width, height) * 0.32;
    return {
      id: `tactic:${tactic}`,
      x: cx + r * Math.cos(angle),
      y: cy + r * Math.sin(angle),
      vx: 0, vy: 0,
      radius: 22,
      color: '#00ff64',
      label: TACTIC_LABELS[tactic] ?? tactic,
      score: 0,
      tactic,
      isTactic: true,
    };
  });

  // Technique nodes — scatter deterministically around their tactic parent
  const techniqueNodes: ForceNode[] = techniques.map((t, i) => {
    const parentTactic = tacticNodes.find((n) => n.id === `tactic:${t.tactic}`);
    // Deterministic jitter using index to avoid layout thrash on re-render
    const angle = (i * 2.399) % (2 * Math.PI); // golden-angle spread
    const r = 30 + (i % 3) * 15;
    return {
      id: `tech:${t.techniqueID}:${t.tactic}`,
      x: (parentTactic?.x ?? cx) + r * Math.cos(angle),
      y: (parentTactic?.y ?? cy) + r * Math.sin(angle),
      vx: 0, vy: 0,
      radius: 10,
      color: t.color ?? '#334',
      label: t.techniqueID,
      score: t.score,
      tactic: t.tactic,
      isTactic: false,
    };
  });

  const edges: ForceEdge[] = techniques
    .filter((t) => tacticSet.includes(t.tactic))
    .map((t) => ({ source: `tactic:${t.tactic}`, target: `tech:${t.techniqueID}:${t.tactic}` }));

  return { nodes: [...tacticNodes, ...techniqueNodes], edges };
}

/** D3 force simulation hook.
 *  Accepts a stable `graphData` reference (from useMemo) so the simulation
 *  restarts when `techniques` change (e.g., navigating between clients). */
function useForceSimulation(
  graphData: { nodes: ForceNode[]; edges: ForceEdge[] },
  width: number,
  height: number,
  active: boolean,
): ForceNode[] {
  const [positions, setPositions] = useState<ForceNode[]>(graphData.nodes);
  const simRef = useRef<d3.Simulation<ForceNode, d3.SimulationLinkDatum<ForceNode>> | null>(null);

  useEffect(() => {
    const { nodes, edges } = graphData;
    if (!active || nodes.length === 0) {
      setPositions(nodes);
      return;
    }

    const ns: ForceNode[] = nodes.map((n) => ({ ...n }));
    // d3 forceLink resolves string IDs to node references — cast to satisfy types
    const links = edges.map((e) => ({ ...e })) as unknown as d3.SimulationLinkDatum<ForceNode>[];

    const pad = 24;

    const sim = d3.forceSimulation<ForceNode>(ns)
      .force(
        'link',
        d3.forceLink<ForceNode, d3.SimulationLinkDatum<ForceNode>>(links)
          .id((d) => d.id)
          .distance((link) => {
            const src = link.source as ForceNode;
            return src.isTactic ? 110 : 55;
          })
          .strength(0.6),
      )
      .force('charge', d3.forceManyBody<ForceNode>().strength((d) => d.isTactic ? -300 : -80))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide<ForceNode>().radius((d) => d.radius + 5))
      .alphaDecay(0.02)
      .on('tick', () => {
        for (const n of ns) {
          n.x = Math.max(pad + n.radius, Math.min(width  - pad - n.radius, n.x ?? width  / 2));
          n.y = Math.max(pad + n.radius, Math.min(height - pad - n.radius, n.y ?? height / 2));
        }
        setPositions(ns.map((n) => ({ ...n })));
      });

    simRef.current = sim;
    return () => { sim.stop(); };
  }, [active, graphData, width, height]);

  return positions;
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

  useEffect(() => {
    if (!containerRef.current) return;
    const obs = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDims({ width: Math.max(400, width), height: Math.max(300, height) });
    });
    obs.observe(containerRef.current);
    return () => obs.disconnect();
  }, []);

  const graphData = useMemo(
    () => buildForceGraph(techniques, dims.width, dims.height),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [techniques, dims.width, dims.height],
  );
  const nodes = useForceSimulation(graphData, dims.width, dims.height, true);
  const { edges } = graphData;

  const nodeIndex: Record<string, ForceNode> = {};
  for (const n of nodes) nodeIndex[n.id] = n;

  const handleNodeClick = useCallback((node: ForceNode) => {
    if (node.isTactic) return;
    const techId = node.id.replace(/^tech:/, '').split(':')[0];
    const tactic = node.tactic;
    const technique = techniques.find(
      (t) => t.techniqueID === techId && t.tactic === tactic,
    );
    if (technique) {
      onSelectTechnique(selectedId === node.id ? null : technique);
    }
  }, [techniques, selectedId, onSelectTechnique]);

  return (
    <div ref={containerRef} className="force-graph-container">
      <svg
        width={dims.width}
        height={dims.height}
        className="force-graph-svg"
        role="img"
        aria-label="MITRE technique force graph"
      >
        {/* Edges */}
        <g className="force-edges">
          {edges.map((edge, i) => {
            const src = nodeIndex[edge.source];
            const tgt = nodeIndex[edge.target];
            if (!src || !tgt) return null;
            return (
              <line
                key={i}
                x1={src.x} y1={src.y}
                x2={tgt.x} y2={tgt.y}
                stroke="rgba(0,255,100,0.12)"
                strokeWidth={1}
              />
            );
          })}
        </g>

        {/* Nodes */}
        <g className="force-nodes">
          {nodes.map((node) => {
            const isSelected = selectedId === node.id;
            return (
              <g
                key={node.id}
                transform={`translate(${node.x},${node.y})`}
                className={`force-node${node.isTactic ? ' force-node--tactic' : ''}${isSelected ? ' force-node--selected' : ''}`}
                onClick={() => handleNodeClick(node)}
                style={{ cursor: node.isTactic ? 'default' : 'pointer' }}
                role={node.isTactic ? undefined : 'button'}
                aria-label={node.label}
              >
                <circle
                  r={node.radius}
                  fill={node.isTactic ? 'rgba(0,255,100,0.15)' : node.color}
                  stroke={isSelected ? '#fff' : (node.isTactic ? '#00ff64' : 'rgba(0,255,100,0.3)')}
                  strokeWidth={isSelected ? 2 : 1}
                />
                <text
                  dy="0.35em"
                  textAnchor="middle"
                  fontSize={node.isTactic ? 6 : 5.5}
                  fill={node.isTactic ? '#00ff64' : 'rgba(0,0,0,0.75)'}
                  fontFamily="'IBM Plex Mono', monospace"
                  fontWeight={node.isTactic ? '600' : '400'}
                  style={{ pointerEvents: 'none', userSelect: 'none' }}
                >
                  {node.label.length > 12
                    ? node.isTactic
                      ? node.label.split(' ').map((w, i) => (
                          <tspan key={i} x={0} dy={i === 0 ? `-${(node.label.split(' ').length - 1) * 3}` : '6'}>
                            {w}
                          </tspan>
                        ))
                      : node.label
                    : node.label}
                </text>
              </g>
            );
          })}
        </g>
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
