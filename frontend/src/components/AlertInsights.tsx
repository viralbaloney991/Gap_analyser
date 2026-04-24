import { useState, useEffect, useMemo } from 'react';
import type { SimilarityResult, InsightsReport, NoiseAlert, DetectionFamily } from '../types';
import { fetchInsights } from '../services/api';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
  client: string;
}

type Tab = 'duplicates' | 'families' | 'merge' | 'coverage' | 'noise' | 'unique' | 'recommendations';

/** Returns a CSS gradient colour for a similarity bar (0–1 scale) */
function simBarGradient(score: number): string {
  if (score >= 0.85) return 'linear-gradient(90deg, #f59e0b, #10b981)';
  if (score >= 0.65) return 'linear-gradient(90deg, #ef4444, #f59e0b)';
  return 'linear-gradient(90deg, #ef4444, #ef4444)';
}

export default function AlertInsights({ data, report, insightsError = false, client }: Props) {
  const [activeTab, setActiveTab]         = useState<Tab>('duplicates');
  const [localReport, setLocalReport]     = useState<InsightsReport | null>(report);
  const [selectedModel, setSelectedModel] = useState<'mistral' | 'gemma'>('mistral');
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [regenError, setRegenError]       = useState(false);
  const [expandedCards, setExpandedCards] = useState<Set<string>>(new Set());

  const toggleCard = (key: string) => {
    setExpandedCards(prev => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

  const familyGroups = useMemo(() => {
    const map = new Map<string, { families: DetectionFamily[]; totalAlerts: number }>();
    (data.families ?? []).forEach(fam => {
      const existing = map.get(fam.name);
      if (existing) {
        existing.families.push(fam);
        existing.totalAlerts += fam.alert_names.length;
      } else {
        map.set(fam.name, { families: [fam], totalAlerts: fam.alert_names.length });
      }
    });
    return Array.from(map.entries());
  }, [data.families]);

  useEffect(() => {
    if (report !== null && !isRegenerating) setLocalReport(report);
  }, [report]); // eslint-disable-line react-hooks/exhaustive-deps

  const effectiveReport = localReport;
  const isLoading = !effectiveReport && !insightsError && !regenError;

  const handleRegenerate = async () => {
    setIsRegenerating(true);
    setRegenError(false);
    try {
      const newReport = await fetchInsights(client, selectedModel);
      setLocalReport(newReport);
    } catch (e) {
      console.warn('[insights regen]', e);
      setRegenError(true);
    } finally {
      setIsRegenerating(false);
    }
  };

  const noiseCount = data.noise_alerts?.length ?? 0;
  const gapCount   = data.coverage_insights?.length ?? 0;
  const recsCount  = effectiveReport?.recommendations?.length ?? 0;

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'duplicates',      label: 'Duplicates',      count: data.duplicates?.length       ?? 0 },
    { key: 'families',        label: 'Families',        count: data.families?.length          ?? 0 },
    { key: 'merge',           label: 'Merge',           count: data.merge_suggestions?.length ?? 0 },
    { key: 'coverage',        label: 'Coverage',        count: gapCount                           },
    { key: 'noise',           label: 'Noise',           count: noiseCount                         },
    { key: 'unique',          label: 'Unique',          count: data.unique_detections?.length ?? 0 },
    { key: 'recommendations', label: 'Recommendations', count: recsCount                          },
  ];

  const hasError = insightsError || regenError;

  return (
    <div className="alert-insights">

      {/* ══ LEFT PANEL ══ */}
      <div className="insights-panel">

        {/* Model selector */}
        <div className="insights-model-header">
          <select
            className="insights-model-select"
            value={selectedModel}
            onChange={(e) => setSelectedModel(e.target.value as 'mistral' | 'gemma')}
            disabled={isRegenerating}
          >
            <option value="mistral">Mistral Small 3.1</option>
            <option value="gemma">Gemma 3 27B</option>
          </select>
          <button
            className="insights-regenerate-btn"
            onClick={handleRegenerate}
            disabled={isRegenerating || !client}
            title="Regenerate insights with selected model"
          >
            {isRegenerating ? '…' : '↺'}
          </button>
        </div>

        <div className="insights-panel-scroll">

          {/* Summary */}
          <div>
            <div className="eyebrow">Summary</div>
            {isRegenerating || isLoading ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '100%' }} />
                <div className="insights-skeleton skeleton" style={{ width: '85%' }} />
                <div className="insights-skeleton skeleton" style={{ width: '70%' }} />
              </>
            ) : hasError ? (
              <div className="state-error">
                <span className="state-error__icon">⚠</span>
                <div>
                  <div className="state-error__title">Insights unavailable</div>
                  <div className="state-error__body">LLM enrichment failed. Check your provider configuration.</div>
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry with {selectedModel === 'mistral' ? 'Mistral Small' : 'Gemma 3 27B'}</button>
                </div>
              </div>
            ) : (
              <p className="insights-summary-text">
                {effectiveReport?.summary || 'Enrichment unavailable — check LLM provider configuration.'}
              </p>
            )}
          </div>

          {/* Top Priority */}
          <div>
            <div className="eyebrow">Top Priority</div>
            {isRegenerating || isLoading ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '90%', marginBottom: 8 }} />
                <div className="insights-skeleton skeleton" style={{ width: '80%' }} />
              </>
            ) : hasError ? null : (
              <ul className="insights-priority-list">
                {effectiveReport?.top_priority?.length ? (
                  effectiveReport.top_priority.map((item, i) => (
                    <li key={i} className="insights-priority-item">
                      <span className="insights-priority-num">{String(i + 1).padStart(2, '0')}</span>
                      <span className="insights-priority-text">{item}</span>
                    </li>
                  ))
                ) : (
                  <li className="insights-priority-item">
                    <span className="insights-priority-text" style={{ color: 'var(--text-dim)' }}>No priorities flagged.</span>
                  </li>
                )}
              </ul>
            )}
          </div>

          {/* Signals */}
          <div>
            <div className="eyebrow">Signals</div>
            <div className="insights-signals-grid">
              <div className="insights-signal-pill">
                <span className="signal-count">{data.duplicates?.length ?? 0}</span>
                <span className="signal-label">Duplicates</span>
              </div>
              <div className="insights-signal-pill">
                <span className="signal-count">{data.families?.length ?? 0}</span>
                <span className="signal-label">Families</span>
              </div>
              <div className="insights-signal-pill">
                <span className="signal-count">{noiseCount}</span>
                <span className="signal-label">Noise</span>
              </div>
              <div className="insights-signal-pill">
                <span className="signal-count">{gapCount}</span>
                <span className="signal-label">Gaps</span>
              </div>
            </div>
          </div>

        </div>
      </div>

      {/* ══ RIGHT PANEL ══ */}
      <div style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>

        {/* Tabs */}
        <div className="insights-tabs">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              className={`tab-btn${activeTab === tab.key ? ' tab-btn--active' : ''}`}
              onClick={() => setActiveTab(tab.key)}
            >
              {tab.label}
              <span className="tab-count">{tab.count}</span>
            </button>
          ))}
        </div>

        {/* Tab content */}
        <div className="insights-tab-content">

          {/* ── DUPLICATES ── */}
          {activeTab === 'duplicates' && (
            data.duplicates?.length ? (
              data.duplicates.map((dup, i) => {
                const key = `dup-${i}`;
                const isOpen = expandedCards.has(key);
                const enrichedExplanation = effectiveReport?.enriched_dups?.[i];
                const simPct = Math.round((dup.similarity ?? 0) * 100);
                return (
                  <div
                    key={key}
                    className={`insight-card insight-card--duplicate${isOpen ? ' insight-card--open' : ''}`}
                    onClick={() => toggleCard(key)}
                    style={{ cursor: 'pointer' }}
                  >
                    <div className="insight-card-header">
                      <div className="insight-card-title">{dup.alert_names[0]}</div>
                      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                        <span className="badge badge--indigo">Duplicate</span>
                        <span className="insight-card-chevron">{isOpen ? '▼' : '▶'}</span>
                      </div>
                    </div>
                    {isOpen && (
                      <>
                        <div className="alert-pair">
                          {dup.alert_names.map((name, j) => (
                            <span key={j}>
                              {j > 0 && <span className="alert-pair-sep">↔</span>}
                              <span className="alert-tag">{name}</span>
                            </span>
                          ))}
                        </div>
                        <div className="sim-bar-wrap">
                          <span className="sim-bar-label">{simPct}% similar</span>
                          <div className="sim-bar-track">
                            <div
                              className="sim-bar-fill"
                              style={{ width: `${simPct}%`, background: simBarGradient(dup.similarity ?? 0) }}
                            />
                          </div>
                        </div>
                        {(enrichedExplanation || dup.explanation) && (
                          <p className="insight-card-body">{enrichedExplanation ?? dup.explanation}</p>
                        )}
                      </>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No duplicates found</div>
                <div className="state-empty__body">All alerts have sufficiently distinct detection logic.</div>
              </div>
            )
          )}

          {/* ── FAMILIES ── */}
          {activeTab === 'families' && (
            familyGroups.length ? (
              familyGroups.map(([name, { families, totalAlerts }]) => {
                const key = `fam-${name}`;
                const isOpen = expandedCards.has(key);
                const groupCount = families.length;
                const allAlertNames = families.flatMap(f => f.alert_names);
                return (
                  <div
                    key={key}
                    className={`insight-card insight-card--family${isOpen ? ' insight-card--open' : ''}`}
                    onClick={() => toggleCard(key)}
                    style={{ cursor: 'pointer' }}
                  >
                    <div className="insight-card-header">
                      <div className="insight-card-title">{name}</div>
                      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                        <span className="badge badge--green">
                          {groupCount > 1 ? `${groupCount} groups · ` : ''}{totalAlerts} alerts
                        </span>
                        <span className="insight-card-chevron">{isOpen ? '▼' : '▶'}</span>
                      </div>
                    </div>
                    {isOpen && (
                      <div className="alert-pair" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
                        {allAlertNames.map((alertName, j) => (
                          <span key={`${j}-${alertName}`} className="alert-tag">{alertName}</span>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No families found</div>
                <div className="state-empty__body">No alert groupings detected in this client's stack.</div>
              </div>
            )
          )}

          {/* ── MERGE ── */}
          {activeTab === 'merge' && (
            data.merge_suggestions?.length ? (
              data.merge_suggestions.map((sug, i) => (
                <div key={i} className="insight-card insight-card--merge">
                  <div className="insight-card-header">
                    <div className="insight-card-title">Merge Suggestion</div>
                    <span className="badge badge--amber">{sug.alert_ids.length} alerts</span>
                  </div>
                  <div className="alert-pair" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
                    {sug.alert_names.map((name, j) => (
                      <span key={j} className="alert-tag">{name}</span>
                    ))}
                  </div>
                  {sug.reason && <p className="insight-card-body">{sug.reason}</p>}
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No merge suggestions</div>
                <div className="state-empty__body">No consolidation opportunities identified.</div>
              </div>
            )
          )}

          {/* ── COVERAGE ── */}
          {activeTab === 'coverage' && (
            data.coverage_insights?.length ? (
              data.coverage_insights.map((gap, i) => {
                // enriched_gaps is string[] — use as plain enriched string
                const enrichedGap = effectiveReport?.enriched_gaps?.[i];
                return (
                  <div key={i} className="insight-card insight-card--coverage">
                    <div className="insight-card-header">
                      <div className="insight-card-title">{enrichedGap ?? gap}</div>
                      <span className="badge badge--sky">Gap</span>
                    </div>
                    {enrichedGap && enrichedGap !== gap && (
                      <p className="insight-card-body">{gap}</p>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No coverage gaps</div>
                <div className="state-empty__body">Full MITRE ATT&amp;CK coverage detected across all tactics.</div>
              </div>
            )
          )}

          {/* ── NOISE ── */}
          {activeTab === 'noise' && (
            data.noise_alerts?.length ? (
              data.noise_alerts.map((noise: NoiseAlert, i) => {
                const key = `noise-${i}`;
                const isOpen = expandedCards.has(key);
                const explanation = effectiveReport?.noise_explanations?.[i];
                const reasonPreview = noise.reason ?? '';
                return (
                  <div
                    key={key}
                    className={`insight-card insight-card--noise${isOpen ? ' insight-card--open' : ''}`}
                    onClick={() => toggleCard(key)}
                    style={{ cursor: 'pointer' }}
                  >
                    <div className="insight-card-header">
                      <div className="insight-card-title">{noise.name}</div>
                      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                        <span className="badge badge--red">Noisy</span>
                        <span className="insight-card-chevron">{isOpen ? '▼' : '▶'}</span>
                      </div>
                    </div>
                    {reasonPreview && (
                      <p className="insight-card-noise-preview">{reasonPreview}</p>
                    )}
                    {isOpen && (
                      <>
                        {explanation && (
                          <p className="insight-card-body">{explanation}</p>
                        )}
                        {noise.missing_features?.length > 0 && (
                          <div className="missing-features">
                            {noise.missing_features.map((feat, j) => (
                              <span key={`${j}-${feat}`} className="missing-tag">{feat}</span>
                            ))}
                          </div>
                        )}
                      </>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No noisy alerts</div>
                <div className="state-empty__body">All alerts have sufficient field coverage for reliable detection.</div>
              </div>
            )
          )}

          {/* ── UNIQUE ── */}
          {activeTab === 'unique' && (
            data.unique_detections?.length ? (
              data.unique_detections.map((name, i) => (
                <div key={i} className="insight-card">
                  <div className="insight-card-title">{name}</div>
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No unique detections</div>
                <div className="state-empty__body">All alerts overlap with at least one other detection rule.</div>
              </div>
            )
          )}

          {/* ── RECOMMENDATIONS ── */}
          {activeTab === 'recommendations' && (
            isLoading || isRegenerating ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
              </>
            ) : hasError ? (
              <div className="state-error">
                <span className="state-error__icon">⚠</span>
                <div>
                  <div className="state-error__title">Recommendations unavailable</div>
                  <div className="state-error__body">LLM enrichment failed. Try regenerating with a different model.</div>
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry</button>
                </div>
              </div>
            ) : effectiveReport?.recommendations?.length ? (
              effectiveReport.recommendations.map((rec, i) => (
                <div key={i} className="rec-item">
                  <div className="rec-num">{String(i + 1).padStart(2, '0')}</div>
                  <div className="rec-text">{rec}</div>
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No recommendations</div>
                <div className="state-empty__body">Run insights generation to get AI-powered recommendations.</div>
              </div>
            )
          )}

        </div>
      </div>
    </div>
  );
}
