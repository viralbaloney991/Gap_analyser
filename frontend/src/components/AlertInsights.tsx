import { useState, useEffect, useMemo } from 'react';
import type { SimilarityResult, InsightsReport, MITRECoverageResult, NoiseAlert, DetectionFamily, ActionableRecommendation } from '../types';
import { fetchInsights, fetchExportNarrative } from '../services/api';
import { exportTabAsXLSX, exportTabAsPDF, exportFullReportPDF } from '../utils/export';
import NoisePills from './NoisePills';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
  insightsError?: boolean;
  client: string;
  mitreCoverage: MITRECoverageResult;
  totalAlerts: number;
  lookbackDays: number;
  onReanalyze: (days: number) => void;
}

type Tab = 'duplicates' | 'families' | 'merge' | 'gaps' | 'noise' | 'unique';

/** Returns a CSS gradient colour for a similarity bar (0–1 scale) */
function simBarGradient(score: number): string {
  if (score >= 0.85) return 'linear-gradient(90deg, #f59e0b, #10b981)';
  if (score >= 0.65) return 'linear-gradient(90deg, #ef4444, #f59e0b)';
  return 'linear-gradient(90deg, #ef4444, #ef4444)';
}

function noiseTypeLabel(noiseType?: string): string {
  switch (noiseType) {
    case 'behavioral': return 'Behavioral';
    case 'structural': return 'Structural';
    case 'both':       return 'Both';
    default:           return 'Structural'; // fallback for legacy data without noise_type
  }
}

export default function AlertInsights({ data, report, insightsError = false, client, mitreCoverage, totalAlerts, lookbackDays, onReanalyze }: Props) {
  const [activeTab, setActiveTab]         = useState<Tab>('duplicates');
  const [localReport, setLocalReport]     = useState<InsightsReport | null>(report);
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

  const [isExporting, setIsExporting]         = useState(false);
  const [exportError, setExportError]         = useState<string | null>(null);
  const [showExportMenu, setShowExportMenu]   = useState(false);

  const [expandedQueries, setExpandedQueries] = useState<Set<string>>(new Set());

  const toggleQuery = (key: string) => {
    setExpandedQueries(prev => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

  const handleExportCurrentXLSX = () => {
    if (activeTab === 'noise' || activeTab === 'families' || activeTab === 'gaps') {
      exportTabAsXLSX(activeTab as 'noise' | 'families' | 'gaps', data, localReport, client);
    }
    setShowExportMenu(false);
  };

  const handleExportCurrentPDF = () => {
    if (activeTab === 'noise' || activeTab === 'families' || activeTab === 'gaps') {
      exportTabAsPDF(activeTab as 'noise' | 'families' | 'gaps', data, localReport, client);
    }
    setShowExportMenu(false);
  };

  const handleExportFullReport = async () => {
    if (!localReport) return;
    setShowExportMenu(false);
    setIsExporting(true);
    setExportError(null);
    try {
      const narrative = await fetchExportNarrative(client);
      const date = new Date().toISOString().slice(0, 10);
      exportFullReportPDF(client, data, localReport, mitreCoverage, narrative, date, totalAlerts);
    } catch (e) {
      console.error('[export]', e);
      setExportError(e instanceof Error ? e.message : 'Export failed. Try again.');
    } finally {
      setIsExporting(false);
    }
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

  useEffect(() => {
    if (!showExportMenu) return;
    const close = () => setShowExportMenu(false);
    document.addEventListener('click', close);
    return () => document.removeEventListener('click', close);
  }, [showExportMenu]);

  const effectiveReport = localReport;
  const isLoading = !effectiveReport && !insightsError && !regenError;

  const handleRegenerate = async () => {
    setIsRegenerating(true);
    setRegenError(false);
    try {
      const newReport = await fetchInsights(client);
      setLocalReport(newReport);
    } catch (e) {
      console.warn('[insights regen]', e);
      setRegenError(true);
    } finally {
      setIsRegenerating(false);
    }
  };

  const renderGapSection = (title: string, items: string[] | undefined) => {
    if (!items?.length) return null;
    return (
      <div key={title} style={{ marginBottom: 16 }}>
        <div className="eyebrow" style={{ marginBottom: 8 }}>{title}</div>
        {items.map((item, i) => (
          <div key={`${i}-${item}`} className="insight-card insight-card--coverage">
            <div className="insight-card-header">
              <div className="insight-card-title">{item}</div>
              <span className="badge badge--sky">Gap</span>
            </div>
          </div>
        ))}
      </div>
    );
  };

  const severityColors: Record<string, string> = {
    critical: '#ef4444',
    high:     '#f97316',
    medium:   '#eab308',
    low:      '#3b82f6',
  };

  const renderActionableSection = (
    title: string,
    actionable: ActionableRecommendation[] | undefined,
    fallback: string[] | undefined
  ) => {
    if (actionable && actionable.length > 0) {
      return (
        <div key={title} style={{ marginBottom: 16 }}>
          <div className="eyebrow" style={{ marginBottom: 8 }}>{title}</div>
          {actionable.map((item, i) => {
            const queryKey = `${title}-${i}`;
            const expanded = expandedQueries.has(queryKey);
            return (
              <div key={queryKey} className="insight-card insight-card--coverage" style={{ marginBottom: 8 }}>
                <div className="insight-card-header" style={{ marginBottom: 4 }}>
                  <span
                    className="badge"
                    style={{ backgroundColor: severityColors[item.severity] ?? '#6b7280', color: '#fff', marginRight: 8 }}
                  >
                    {item.severity.toUpperCase()}
                  </span>
                  <span style={{ fontWeight: 600, fontSize: 12 }}>{item.log_source}</span>
                </div>
                <div className="insight-card-title" style={{ marginBottom: 6 }}>{item.prose}</div>
                <button
                  onClick={() => toggleQuery(queryKey)}
                  style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#60a5fa', fontSize: 12, padding: 0 }}
                >
                  {expanded ? '▼ Hide query' : '▶ Show query'}
                </button>
                {expanded && (
                  <div style={{ marginTop: 6 }}>
                    <pre style={{ background: '#1e1e2e', color: '#cdd6f4', padding: '8px 12px', borderRadius: 4, fontSize: 12, overflowX: 'auto', margin: 0 }}>
                      {item.query_skeleton}
                    </pre>
                    <button
                      onClick={() => navigator.clipboard.writeText(item.query_skeleton)}
                      style={{ marginTop: 4, background: 'none', border: '1px solid #4b5563', cursor: 'pointer', color: '#9ca3af', fontSize: 11, padding: '2px 8px', borderRadius: 4 }}
                    >
                      Copy
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      );
    }
    return renderGapSection(title, fallback);
  };

  const noiseCount = data.noise_alerts?.length ?? 0;
  const gapCount = effectiveReport
    ? (effectiveReport.gap_categories.environment_cleanup.length +
       effectiveReport.gap_categories.no_detection.length +
       effectiveReport.gap_categories.poor_tactic_coverage.length +
       effectiveReport.gap_categories.weak_detection_quality.length +
       effectiveReport.gap_categories.advanced_use_cases.length +
       effectiveReport.gap_categories.missing_source_alerts.length)
    : 0;
  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'duplicates', label: 'Duplicates', count: data.duplicates?.length       ?? 0 },
    { key: 'families',   label: 'Families',   count: familyGroups.length                  },
    { key: 'merge',      label: 'Merge',      count: data.merge_suggestions?.length ?? 0 },
    { key: 'gaps',       label: 'Gaps',       count: gapCount                           },
    { key: 'noise',      label: 'Noise',      count: noiseCount                         },
    { key: 'unique',     label: 'Unique',     count: data.unique_detections?.length ?? 0 },
  ];

  const hasError = insightsError || regenError;


  return (
    <div className="alert-insights">

      {/* ══ LEFT PANEL ══ */}
      <div className="insights-panel">

        {/* Provider badge */}
        <div className="insights-model-header">
          <span className="insights-model-badge">Claude Opus 4.7</span>
          <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            <NoisePills days={lookbackDays} onChange={onReanalyze} disabled={isRegenerating || isExporting} />
            <button
              className="insights-regenerate-btn"
              onClick={handleRegenerate}
              disabled={isRegenerating || !client}
              title="Regenerate insights"
            >
              {isRegenerating ? '…' : '↺'}
            </button>
            <div style={{ position: 'relative' }}>
              <button
                onClick={(e) => { e.stopPropagation(); setShowExportMenu(v => !v); }}
                disabled={isExporting}
                className="btn-small"
              >
                {isExporting ? 'Generating…' : 'Export ▾'}
              </button>
              {showExportMenu && (
                <div className="export-menu">
                  <button onClick={handleExportCurrentXLSX} className="export-menu-item">Current tab: XLSX</button>
                  <button onClick={handleExportCurrentPDF} className="export-menu-item">Current tab: PDF</button>
                  {localReport && (
                    <>
                      <div className="export-menu-divider" />
                      <button onClick={handleExportFullReport} className="export-menu-item export-menu-item--accent">
                        Full report: PDF
                      </button>
                    </>
                  )}
                </div>
              )}
            </div>
          </div>
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
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry with Claude Opus</button>
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
                            <span key={`${j}-${name}`}>
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
                      <div className="family-alert-list">
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
          {activeTab === 'gaps' && (
            isLoading || isRegenerating ? (
              <>
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
                <div className="insights-skeleton skeleton" style={{ width: '100%', height: 60 }} />
              </>
            ) : hasError ? (
              <div className="state-error">
                <span className="state-error__icon">⚠</span>
                <div>
                  <div className="state-error__title">Coverage analysis unavailable</div>
                  <div className="state-error__body">Gap analysis requires LLM enrichment. Try regenerating.</div>
                  <button className="state-error__retry" onClick={handleRegenerate}>↺ Retry</button>
                </div>
              </div>
            ) : gapCount > 0 ? (
              <>
                {renderGapSection('Environment Cleanup', effectiveReport?.gap_categories.environment_cleanup)}
                {renderActionableSection('No Detection', effectiveReport?.actionable_gaps?.no_detection, effectiveReport?.gap_categories.no_detection)}
                {renderGapSection('Poor Tactic Coverage', effectiveReport?.gap_categories.poor_tactic_coverage)}
                {renderActionableSection('Weak Detection Quality', effectiveReport?.actionable_gaps?.weak_detection_quality, effectiveReport?.gap_categories.weak_detection_quality)}
                {renderActionableSection('Advanced Use Cases', effectiveReport?.actionable_gaps?.advanced_use_cases, effectiveReport?.gap_categories.advanced_use_cases)}
                {renderActionableSection('Missing Source Alerts', effectiveReport?.actionable_gaps?.missing_source_alerts, effectiveReport?.gap_categories.missing_source_alerts)}
              </>
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No gaps detected</div>
                <div className="state-empty__body">No significant MITRE coverage gaps or alert quality issues found.</div>
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
                        <span className={`noise-type-badge noise-type-badge--${noise.noise_type ?? 'structural'}`}>
                          {noiseTypeLabel(noise.noise_type)}
                        </span>
                        {(noise.trigger_count ?? 0) > 0 && (
                          <span className="noise-trigger-count">Fired {noise.trigger_count}×</span>
                        )}
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
                <div className="state-empty__title">No rule-confirmed noisy alerts</div>
                <div className="state-empty__body">No alerts exceeded the behavioral or structural noise thresholds. LLM-identified noise candidates (e.g. unscoped immediate alerts) appear in Gaps → Environment Cleanup.</div>
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

        </div>
      </div>

      {exportError && (
        <div
          role="alert"
          onClick={() => setExportError(null)}
          style={{
            position: 'fixed', bottom: 24, right: 24,
            background: 'var(--danger-dim)', color: 'var(--danger)',
            border: '1px solid rgba(239,68,68,0.25)',
            padding: '10px 16px', borderRadius: 'var(--radius-lg)',
            fontFamily: 'var(--font-mono)', fontSize: '0.7rem',
            zIndex: 100, cursor: 'pointer',
          }}
        >
          {exportError}
        </div>
      )}
      {isExporting && (
        <div style={{
          position: 'fixed', bottom: 24, right: 24,
          background: 'var(--sky-dim)', color: 'var(--sky)',
          border: '1px solid rgba(56,189,248,0.2)',
          padding: '10px 16px', borderRadius: 'var(--radius-lg)',
          fontFamily: 'var(--font-mono)', fontSize: '0.7rem',
          zIndex: 100,
        }}>
          Generating your report, this takes ~20 seconds…
        </div>
      )}
    </div>
  );
}
