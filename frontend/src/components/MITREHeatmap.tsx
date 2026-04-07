import { useState } from 'react';
import type { MITRECoverageResult, NavigatorTechnique, SuggestionsResponse } from '../types';
import { fetchSuggestions } from '../services/api';

interface Props {
  data: MITRECoverageResult;
  clientName: string;
}

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
  'reconnaissance': 'Reconnaissance',
  'resource-development': 'Resource Development',
  'initial-access': 'Initial Access',
  'execution': 'Execution',
  'persistence': 'Persistence',
  'privilege-escalation': 'Privilege Escalation',
  'defense-evasion': 'Defense Evasion',
  'credential-access': 'Credential Access',
  'discovery': 'Discovery',
  'lateral-movement': 'Lateral Movement',
  'collection': 'Collection',
  'command-and-control': 'Command & Control',
  'exfiltration': 'Exfiltration',
  'impact': 'Impact',
};

function coverageColor(percent: number): string {
  if (percent === 0) return '#ff0000';
  if (percent <= 50) return '#ff8c00';
  if (percent <= 75) return '#ffd700';
  return '#00cc00';
}

function coverageLabel(percent: number): string {
  if (percent === 0) return 'Not Covered';
  if (percent <= 50) return 'Low Coverage';
  if (percent <= 75) return 'Moderate Coverage';
  return 'Good Coverage';
}

function priorityColor(priority: string): string {
  switch (priority) {
    case 'critical': return '#ff0000';
    case 'high': return '#ff8c00';
    case 'medium': return '#ffd700';
    case 'low': return '#888888';
    default: return '#aaaaaa';
  }
}

export default function MITREHeatmap({ data, clientName }: Props) {
  const [selectedTactic, setSelectedTactic] = useState<string | null>(null);
  const [selectedTechnique, setSelectedTechnique] = useState<NavigatorTechnique | null>(null);
  const [suggestions, setSuggestions] = useState<SuggestionsResponse | null>(null);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [suggestionsError, setSuggestionsError] = useState<string | null>(null);
  const [selectedProvider, setSelectedProvider] = useState<string>('');

  const { summary, navigator_layer: layer } = data;

  const techniquesByTactic: Record<string, NavigatorTechnique[]> = {};
  for (const t of layer.techniques) {
    const tactic = t.tactic || 'unknown';
    if (!techniquesByTactic[tactic]) techniquesByTactic[tactic] = [];
    techniquesByTactic[tactic].push(t);
  }

  // Sort techniques within each tactic by score (highest first)
  for (const tactic of Object.keys(techniquesByTactic)) {
    techniquesByTactic[tactic].sort((a, b) => b.score - a.score);
  }

  const downloadLayer = () => {
    const blob = new Blob([JSON.stringify(layer, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'mitre_coverage_layer.json';
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleGenerateAlerts = async (technique: NavigatorTechnique, force = false) => {
    setSuggestions(null);
    setSuggestionsError(null);
    setSuggestionsLoading(true);
    try {
      const result = await fetchSuggestions(clientName, technique.techniqueID, technique.tactic, selectedProvider || undefined, force);
      setSuggestions(result);
    } catch (err: unknown) {
      setSuggestionsError(err instanceof Error ? err.message : 'Failed to generate suggestions');
    } finally {
      setSuggestionsLoading(false);
    }
  };

  const handleSelectTechnique = (technique: NavigatorTechnique | null) => {
    setSelectedTechnique(technique);
    // Clear suggestions when switching techniques
    setSuggestions(null);
    setSuggestionsError(null);
    setSuggestionsLoading(false);
  };

  return (
    <div className="mitre-heatmap">
      <div className="mitre-header">
        <h2>MITRE ATT&CK Coverage</h2>
        <button className="btn btn-small" onClick={downloadLayer}>
          Download Navigator Layer
        </button>
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
          <span className="stat-value" style={{ color: coverageColor(summary.coverage_percent) }}>
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

      {/* Tactic Columns */}
      <div className="tactic-grid">
        {TACTICS_ORDER.map((tactic) => {
          const tacticData = summary.tactic_breakdown[tactic];
          const techniques = techniquesByTactic[tactic] || [];
          const isSelected = selectedTactic === tactic;
          const pct = tacticData?.percent || 0;

          return (
            <div
              key={tactic}
              className={`tactic-column ${isSelected ? 'selected' : ''}`}
              onClick={() => setSelectedTactic(isSelected ? null : tactic)}
            >
              <div className="tactic-header" style={{ borderTopColor: coverageColor(pct) }}>
                <div className="tactic-name">{TACTIC_LABELS[tactic] || tactic}</div>
                <div className="tactic-coverage" style={{ color: coverageColor(pct) }}>
                  {tacticData ? `${tacticData.covered}/${tacticData.total}` : '0/0'}
                </div>
                {tacticData && tacticData.total_subs > 0 && (
                  <div className="tactic-subs">
                    {tacticData.covered_subs}/{tacticData.total_subs} subs
                  </div>
                )}
              </div>

              <div className="technique-cells">
                {techniques.map((t) => {
                  const isActive = selectedTechnique?.techniqueID === t.techniqueID
                    && selectedTechnique?.tactic === t.tactic;
                  return (
                    <div
                      key={`${t.techniqueID}-${t.tactic}`}
                      className={`technique-cell ${isActive ? 'active' : ''}`}
                      style={{ backgroundColor: t.color }}
                      onClick={(e) => {
                        e.stopPropagation();
                        handleSelectTechnique(isActive ? null : t);
                      }}
                      title={`${t.techniqueID} — ${t.name || t.techniqueID}`}
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

      {/* Detail Panel */}
      {selectedTechnique && (
        <div className="technique-detail-panel" onClick={(e) => e.stopPropagation()}>
          <div className="detail-panel-header">
            <strong>{selectedTechnique.techniqueID} — {selectedTechnique.name || selectedTechnique.techniqueID}</strong>
            <button className="detail-close" onClick={() => handleSelectTechnique(null)}>x</button>
          </div>
          <div className="detail-panel-body">
            <div className="detail-row">
              <span className="detail-label">Tactic</span>
              <span className="detail-value">{TACTIC_LABELS[selectedTechnique.tactic] || selectedTechnique.tactic}</span>
            </div>
            <div className="detail-row">
              <span className="detail-label">Score</span>
              <span className="detail-value" style={{ color: coverageColor(selectedTechnique.score) }}>
                {selectedTechnique.score}% — {coverageLabel(selectedTechnique.score)}
              </span>
            </div>
            {selectedTechnique.comment && (
              <div className="detail-row detail-row-comment">
                <span className="detail-label">Alerts</span>
                <span className="detail-value">{selectedTechnique.comment}</span>
              </div>
            )}

            {/* Generate Alerts button — only for uncovered techniques (score=0) */}
            {selectedTechnique.score === 0 && (
              <div className="suggestions-section">
                <div className="suggestions-controls">
                  <select
                    className="provider-select"
                    value={selectedProvider}
                    onChange={(e) => setSelectedProvider(e.target.value)}
                    disabled={suggestionsLoading}
                  >
                    <option value="">Default model</option>
                    <option value="nvidia">NVIDIA (Qwen)</option>
                    <option value="claude">Claude (Haiku)</option>
                    <option value="gemini">Gemini 2.0 Flash</option>
                  </select>
                  <button
                    className="btn btn-generate"
                    onClick={() => handleGenerateAlerts(selectedTechnique)}
                    disabled={suggestionsLoading}
                  >
                    {suggestionsLoading ? 'Generating...' : 'Generate Suggestions'}
                  </button>
                </div>

                {suggestionsError && (
                  <div className="suggestions-error">{suggestionsError}</div>
                )}

                {suggestions && (
                  <div className="suggestions-list">
                    <div className="suggestions-header">
                      <span>{suggestions.suggestions.length} suggestion{suggestions.suggestions.length !== 1 ? 's' : ''}</span>
                      <span className="suggestions-provider">via {suggestions.provider}</span>
                      <button
                        className="btn btn-small"
                        onClick={() => handleGenerateAlerts(selectedTechnique, true)}
                        disabled={suggestionsLoading}
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
                            <span className="suggestion-priority" style={{ color: priorityColor(s.priority) }}>
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
            )}
          </div>
        </div>
      )}
    </div>
  );
}
