import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import type { SimilarityResult, InsightsReport, MITRECoverageResult, DetectionFamily, ActionableRecommendation, CorrelationSuggestion } from '../types';
import { fetchInsights, fetchExportNarrative, fetchCorrelations, fetchMapTactics } from '../services/api';
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
  noiseLoading?: boolean;
  onBuildDetection: (tacticIds: string[], techniqueIds: string[]) => void;
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

type SeverityLevel = 'critical' | 'high' | 'medium' | 'low';

interface TopGap {
  rank: number;
  severity: SeverityLevel;
  categoryLabel: string;
  prose: string;
  logSource?: string;
}

const SEVERITY_WEIGHT: Record<SeverityLevel, number> = { critical: 4, high: 3, medium: 2, low: 1 };

const CATEGORY_PRIORITY: Record<string, number> = {
  no_detection: 6, missing_source_alerts: 5, poor_tactic_coverage: 4,
  weak_detection_quality: 3, advanced_use_cases: 2, environment_cleanup: 1,
};

function severityBadgeClass(severity: string): string {
  return `badge badge--${severity.toLowerCase()}`;
}

function coveragePctToSeverity(pct: number): SeverityLevel {
  if (pct === 0) return 'critical';
  if (pct < 10)  return 'high';
  return 'medium';
}

function computeTopGaps(
  report: InsightsReport,
  mitreCoverage: MITRECoverageResult,
  n = 5,
): TopGap[] {
  type Candidate = Omit<TopGap, 'rank'> & { categoryKey: string };
  const candidates: Candidate[] = [];

  const actionableDefs: Array<{ key: string; label: string; items: ActionableRecommendation[] | undefined }> = [
    { key: 'no_detection',           label: 'No Detection',          items: report.actionable_gaps?.no_detection },
    { key: 'missing_source_alerts',  label: 'Missing Source Alerts', items: report.actionable_gaps?.missing_source_alerts },
    { key: 'weak_detection_quality', label: 'Weak Detection',        items: report.actionable_gaps?.weak_detection_quality },
    { key: 'advanced_use_cases',     label: 'Advanced Use Cases',    items: report.actionable_gaps?.advanced_use_cases },
  ];
  for (const { key, label, items } of actionableDefs) {
    for (const item of items ?? []) {
      candidates.push({ severity: item.severity, categoryKey: key, categoryLabel: label, prose: item.prose, logSource: item.log_source });
    }
  }

  const breakdown = mitreCoverage.summary.tactic_breakdown ?? {};
  for (const str of report.gap_categories.poor_tactic_coverage ?? []) {
    const lower = str.toLowerCase();
    let sev: SeverityLevel = 'high';
    for (const [key, tc] of Object.entries(breakdown)) {
      const name = tc.tactic_name?.toLowerCase() ?? '';
      if (name && (lower.includes(name) || lower.includes(key.replace(/-/g, ' ')))) {
        sev = coveragePctToSeverity(tc.percent);
        break;
      }
    }
    candidates.push({ severity: sev, categoryKey: 'poor_tactic_coverage', categoryLabel: 'Poor Tactic Coverage', prose: str });
  }

  for (const str of report.gap_categories.environment_cleanup ?? []) {
    candidates.push({ severity: 'medium', categoryKey: 'environment_cleanup', categoryLabel: 'Environment Cleanup', prose: str });
  }

  candidates.sort((a, b) => {
    const sw = SEVERITY_WEIGHT[b.severity] - SEVERITY_WEIGHT[a.severity];
    return sw !== 0 ? sw : (CATEGORY_PRIORITY[b.categoryKey] ?? 0) - (CATEGORY_PRIORITY[a.categoryKey] ?? 0);
  });

  return candidates.slice(0, n).map((c, i) => ({ ...c, rank: i + 1 }));
}

export default function AlertInsights({ data, report, insightsError = false, client, mitreCoverage, totalAlerts, lookbackDays, onReanalyze, noiseLoading, onBuildDetection }: Props) {
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
  const [noiseFilter, setNoiseFilter] = useState<'all' | 'behavioral' | 'structural'>('all');

  const [correlationDrawer, setCorrelationDrawer] = useState<{
    gapProse: string;
    suggestions: CorrelationSuggestion[];
    loading: boolean;
    cached: boolean;
    error: boolean;
  } | null>(null);

  const [buildingId, setBuildingId] = useState<string | null>(null);

  const correlationAbortRef = useRef<AbortController | null>(null);

  const coveredTechniques = useMemo(
    () =>
      Object.entries(mitreCoverage.technique_coverage ?? {})
        .filter(([, v]) => v.alert_count > 0)
        .map(([k]) => k),
    [mitreCoverage],
  );

  const openCorrelationDrawer = useCallback(async (rec: ActionableRecommendation) => {
    // Cancel any previous in-flight request
    correlationAbortRef.current?.abort();
    const controller = new AbortController();
    correlationAbortRef.current = controller;

    setCorrelationDrawer({ gapProse: rec.prose, suggestions: [], loading: true, cached: false, error: false });
    try {
      const result = await fetchCorrelations(
        client,
        rec.prose,
        rec.log_source ? [rec.log_source] : [],
        coveredTechniques,
        false,
        controller.signal,
      );
      if (!controller.signal.aborted) {
        setCorrelationDrawer({ gapProse: rec.prose, suggestions: result.suggestions, loading: false, cached: result.cached, error: false });
      }
    } catch (e) {
      if (!controller.signal.aborted) {
        console.warn('[correlations]', e);
        setCorrelationDrawer(prev => prev ? { ...prev, loading: false, error: true } : null);
      }
    }
  }, [client, coveredTechniques]);

  const handleBuildDetection = async (prose: string, logSource: string, itemKey: string) => {
    setBuildingId(itemKey);
    try {
      const result = await fetchMapTactics(client, prose, logSource);
      onBuildDetection(result.tactic_ids, result.technique_ids);
    } catch {
      // On network error: still navigate (open builder empty)
      onBuildDetection([], []);
    } finally {
      setBuildingId(null);
    }
  };

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

  const NOISE_FILTER_LABELS: Record<'all' | 'behavioral' | 'structural', string> = {
    all: 'All', behavioral: 'Behavioral', structural: 'Structural',
  };

  const filteredNoise = useMemo(() => {
    const allNoise = data.noise_alerts ?? [];
    return allNoise
      .map((n, origIdx) => ({ n, origIdx }))
      .filter(({ n }) => {
        if (noiseFilter === 'all') return true;
        if (noiseFilter === 'behavioral') return n.noise_type === 'behavioral' || n.noise_type === 'both';
        return n.noise_type === 'structural' || n.noise_type === 'both';
      });
  }, [data.noise_alerts, noiseFilter]);

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

  useEffect(() => { setNoiseFilter('all'); }, [data.noise_alerts]);

  useEffect(() => {
    if (!showExportMenu) return;
    const close = () => setShowExportMenu(false);
    document.addEventListener('click', close);
    return () => document.removeEventListener('click', close);
  }, [showExportMenu]);

  // Abort any in-flight correlation fetch on unmount
  useEffect(() => {
    return () => { correlationAbortRef.current?.abort(); };
  }, []);

  // Close drawer on Escape key
  useEffect(() => {
    if (!correlationDrawer) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setCorrelationDrawer(null);
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [correlationDrawer]);

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

  const renderActionableSection = (
    title: string,
    actionable: ActionableRecommendation[] | undefined,
    fallback: string[] | undefined,
    onCorrelate?: (rec: ActionableRecommendation) => void,
    showBuildDetection = false,
  ) => {
    if (actionable && actionable.length > 0) {
      return (
        <div key={title} style={{ marginBottom: 16 }}>
          <div className="eyebrow" style={{ marginBottom: 8 }}>{title}</div>
          {actionable.map((item, i) => {
            const queryKey = `${title}-${i}`;
            const expanded = expandedQueries.has(queryKey);
            const itemKey = item.prose ? `${title}-${item.prose.slice(0, 40)}` : String(i);
            return (
              <div key={queryKey} className="insight-card insight-card--coverage" style={{ marginBottom: 8 }}>
                <div className="insight-card-header" style={{ marginBottom: 4 }}>
                  <span
                    className={severityBadgeClass(item.severity)}
                    style={{ marginRight: 8 }}
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
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: onCorrelate || showBuildDetection ? 4 : 0 }}>
                  {onCorrelate && (
                    <button
                      type="button"
                      className="corr-suggest-btn"
                      onClick={() => onCorrelate(item)}
                    >
                      Suggest correlations →
                    </button>
                  )}
                  {showBuildDetection && (
                    <button
                      type="button"
                      className="corr-suggest-btn build-detection-btn"
                      onClick={() => handleBuildDetection(item.prose ?? '', item.log_source ?? '', itemKey)}
                      disabled={buildingId !== null}
                    >
                      {buildingId === itemKey ? '…' : 'Build Detection →'}
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      );
    }
    return renderGapSection(title, fallback);
  };

  const topGaps = useMemo(() =>
    effectiveReport ? computeTopGaps(effectiveReport, mitreCoverage) : [],
  [effectiveReport, mitreCoverage]);

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
        <div className="insights-tabs-wrap">
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
                {topGaps.length > 0 && (
                  <div className="top-gaps-card">
                    <div className="top-gaps-title">Top {topGaps.length} Gaps · Immediate Attention</div>
                    {topGaps.map(gap => (
                      <div
                        key={`${gap.categoryLabel}-${gap.rank}`}
                        className="top-gaps-item"
                        style={{ animationDelay: `${(gap.rank - 1) * 55}ms` }}
                      >
                        <span className="top-gaps-rank">{String(gap.rank).padStart(2, '0')}</span>
                        <div className="top-gaps-body">
                          <div className="top-gaps-meta">
                            <span
                              className={severityBadgeClass(gap.severity)}
                            >
                              {gap.severity.toUpperCase()}
                            </span>
                            <span className="top-gaps-category">{gap.categoryLabel}</span>
                            {gap.logSource && <span className="top-gaps-source">{gap.logSource}</span>}
                          </div>
                          <div className="top-gaps-prose">{gap.prose}</div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
                {effectiveReport?.all_integrations_vendor_managed && (
                  <div className="vendor-managed-notice">
                    All log sources are vendor-managed. Improvement recommendations require
                    at least one customer-controlled integration.
                  </div>
                )}
                {renderGapSection('Environment Cleanup', effectiveReport?.gap_categories.environment_cleanup)}
                {renderActionableSection('No Detection', effectiveReport?.actionable_gaps?.no_detection, effectiveReport?.gap_categories.no_detection)}
                {renderGapSection('Poor Tactic Coverage', effectiveReport?.gap_categories.poor_tactic_coverage)}
                {renderActionableSection('Weak Detection Quality', effectiveReport?.actionable_gaps?.weak_detection_quality, effectiveReport?.gap_categories.weak_detection_quality)}
                {renderActionableSection('Advanced Use Cases', effectiveReport?.actionable_gaps?.advanced_use_cases, effectiveReport?.gap_categories.advanced_use_cases, openCorrelationDrawer, true)}
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
            <>
              <div style={{ marginBottom: 12 }}>
                <NoisePills days={lookbackDays} onChange={onReanalyze} disabled={isRegenerating || (noiseLoading ?? false)} />
              </div>
              <div className="noise-filter-pills">
                {(['all', 'behavioral', 'structural'] as const).map(f => (
                  <button
                    key={f}
                    className={`noise-filter-pill${noiseFilter === f ? ' noise-filter-pill--active' : ''}`}
                    onClick={() => setNoiseFilter(f)}
                  >
                    {NOISE_FILTER_LABELS[f]}
                  </button>
                ))}
              </div>
              {filteredNoise.length ? (
                filteredNoise.map(({ n: noise, origIdx }) => {
                  const key = `noise-${origIdx}`;
                  const isOpen = expandedCards.has(key);
                  const explanation = effectiveReport?.noise_explanations?.[origIdx];
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
                  <div className="state-empty__title">
                    {noiseFilter === 'all' ? 'No rule-confirmed noisy alerts' : `No ${NOISE_FILTER_LABELS[noiseFilter]} noise alerts`}
                  </div>
                  <div className="state-empty__body">
                    {noiseFilter === 'all'
                      ? 'No alerts exceeded the behavioral or structural noise thresholds.'
                      : `No alerts matched the ${NOISE_FILTER_LABELS[noiseFilter].toLowerCase()} noise filter.`}
                  </div>
                </div>
              )}
            </>
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
          Generating report…
        </div>
      )}

      {/* Correlation suggestions drawer */}
      {correlationDrawer && (
        <>
          <div className="corr-backdrop" onClick={() => setCorrelationDrawer(null)} />
          <div className="corr-drawer" role="dialog" aria-modal="true" aria-label="Correlation suggestions">
            <div className="corr-drawer-header">
              <span className="corr-drawer-title" title={correlationDrawer.gapProse}>
                {correlationDrawer.gapProse.length > 80
                  ? correlationDrawer.gapProse.slice(0, 80) + '…'
                  : correlationDrawer.gapProse}
              </span>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexShrink: 0 }}>
                {correlationDrawer.cached && <span className="corr-cached-badge">Cached</span>}
                <button type="button" className="corr-close-btn" aria-label="Close" onClick={() => setCorrelationDrawer(null)}>✕</button>
              </div>
            </div>
            <div className="corr-drawer-body">
              {correlationDrawer.loading ? (
                <>
                  <div className="corr-skeleton skeleton" />
                  <div className="corr-skeleton skeleton" style={{ width: '80%' }} />
                  <div className="corr-skeleton skeleton" style={{ width: '90%' }} />
                </>
              ) : correlationDrawer.error ? (
                <div className="corr-empty">
                  <div className="corr-empty-icon">⚠</div>
                  <div>Failed to load suggestions. Please try again.</div>
                </div>
              ) : correlationDrawer.suggestions.length === 0 ? (
                <div className="state-empty">
                  <div className="state-empty__icon">◎</div>
                  <div className="state-empty__title">No suggestions generated</div>
                  <div className="state-empty__body">The LLM could not produce correlation rules for this gap. Try regenerating.</div>
                </div>
              ) : (
                <>
                  {(['correlation', 'anomaly'] as const).map(type => {
                    const items = correlationDrawer.suggestions.filter(s => s.type === type);
                    if (!items.length) return null;
                    return (
                      <div key={type} className="corr-section">
                        <div className="corr-section-title">
                          {type === 'correlation' ? 'Correlation Rules' : 'Anomaly Rules'}
                        </div>
                        {items.map((sug, i) => {
                          const corrKey = `${type}-${i}`;
                          const isOpen = expandedQueries.has(`corr-${corrKey}`);
                          return (
                            <div key={corrKey} className="corr-card">
                              <div className="corr-card-meta">
                                <span
                                  className={severityBadgeClass(sug.priority)}
                                >
                                  {sug.priority.toUpperCase()}
                                </span>
                                <span className="corr-card-title">{sug.title}</span>
                              </div>
                              <p className="corr-card-desc">{sug.description}</p>
                              {sug.involved_techniques.length > 0 && (
                                <div className="corr-techniques">
                                  {sug.involved_techniques.map(t => (
                                    <span key={t} className="corr-technique-chip">{t}</span>
                                  ))}
                                </div>
                              )}
                              <button
                                type="button"
                                onClick={() => toggleQuery(`corr-${corrKey}`)}
                                style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#60a5fa', fontSize: 12, padding: 0, marginTop: 4 }}
                              >
                                {isOpen ? '▼ Hide query' : '▶ Show query'}
                              </button>
                              {isOpen && (
                                <div style={{ marginTop: 6 }}>
                                  <pre style={{ background: '#1e1e2e', color: '#cdd6f4', padding: '8px 12px', borderRadius: 4, fontSize: 12, overflowX: 'auto', margin: 0 }}>
                                    {sug.query_skeleton}
                                  </pre>
                                  <button
                                    type="button"
                                    onClick={() => navigator.clipboard.writeText(sug.query_skeleton)}
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
                  })}
                </>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
