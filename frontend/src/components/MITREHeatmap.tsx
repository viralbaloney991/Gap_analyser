/**
 * MITREHeatmap — MITRE ATT&CK coverage view.
 *
 * Layout: two modes toggled by the user.
 *   1. Heatmap grid  — the original tactic-column layout (default).
 *   2. Force graph   — progressive disclosure: 14 tactic nodes, click to expand techniques.
 */

import { useState, useRef, useEffect } from 'react';
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

// Reference the helpers so they're not marked as unused (Task 2 will use these)
void tacticGridPositions;
void techniqueRadialPositions;

// ---------------------------------------------------------------------------
// Force Graph component
// ---------------------------------------------------------------------------

function ForceGraph({
  techniques: _techniques,
  onSelectTechnique: _onSelectTechnique,
  selectedId: _selectedId,
}: {
  techniques: NavigatorTechnique[];
  onSelectTechnique: (t: NavigatorTechnique | null) => void;
  selectedId: string | null;
}) {
  // Task 2: Placeholder refs/effects for force graph implementation
  const _svgRef = useRef<SVGSVGElement>(null);
  useEffect(() => {
    // Task 2: Implement force graph simulation here
  }, []);

  return (
    <div className="force-graph-container">
      <svg ref={_svgRef} width={800} height={540} className="force-graph-svg">
        <text
          x={400} y={270}
          textAnchor="middle"
          fill="#00ff6466"
          fontSize={14}
          fontFamily="'IBM Plex Mono', monospace"
        >
          Loading graph...
        </text>
      </svg>
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
