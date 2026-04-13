import { useState } from 'react';
import type { SimilarityResult, InsightsReport, NoiseAlert } from '../types';

interface Props {
  data: SimilarityResult;
  report: InsightsReport | null;
}

type Tab = 'duplicates' | 'families' | 'merge' | 'coverage' | 'noise' | 'unique' | 'recommendations';

export default function AlertInsights({ data, report }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('duplicates');

  const noiseCount = data.noise_alerts?.length ?? 0;
  const gapCount   = data.coverage_insights?.length ?? 0;
  const recsCount  = report?.recommendations?.length ?? 0;

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'duplicates',      label: 'Duplicates',      count: data.duplicates?.length      ?? 0 },
    { key: 'families',        label: 'Families',        count: data.families?.length         ?? 0 },
    { key: 'merge',           label: 'Merge',           count: data.merge_suggestions?.length ?? 0 },
    { key: 'coverage',        label: 'Coverage',        count: gapCount                           },
    { key: 'noise',           label: 'Noise',           count: noiseCount                         },
    { key: 'unique',          label: 'Unique',          count: data.unique_detections?.length ?? 0 },
    { key: 'recommendations', label: 'Recommendations', count: recsCount                          },
  ];

  return (
    <div className="alert-insights">
      <div className="insights-grid">

        {/* ── Left panel ── */}
        <div className="insights-panel">

          <div className="insights-panel-summary">
            {report ? (
              report.summary || 'Enrichment unavailable — check LLM provider configuration.'
            ) : (
              <>
                <div className="insights-skeleton" style={{ width: '100%' }} />
                <div className="insights-skeleton" style={{ width: '85%' }} />
                <div className="insights-skeleton" style={{ width: '70%' }} />
              </>
            )}
          </div>

          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Top Priority</div>
            {report ? (
              report.top_priority?.length ? (
                report.top_priority.map((item, i) => (
                  <div key={i} className="insights-panel-item insights-panel-item--priority">
                    {i + 1}. {item}
                  </div>
                ))
              ) : (
                <div className="insights-panel-item">No priorities flagged.</div>
              )
            ) : (
              <>
                <div className="insights-skeleton" style={{ width: '90%' }} />
                <div className="insights-skeleton" style={{ width: '80%' }} />
              </>
            )}
          </div>

          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Strengths</div>
            {report ? (
              report.strengths?.length ? (
                report.strengths.map((s, i) => (
                  <div key={i} className="insights-panel-item">• {s}</div>
                ))
              ) : (
                <div className="insights-panel-item">No strengths identified.</div>
              )
            ) : (
              <>
                <div className="insights-skeleton" style={{ width: '75%' }} />
                <div className="insights-skeleton" style={{ width: '65%' }} />
              </>
            )}
          </div>

          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Recommendations</div>
            {report ? (
              report.recommendations?.length ? (
                report.recommendations.map((r, i) => (
                  <div key={i} className="insights-panel-item">• {r}</div>
                ))
              ) : (
                <div className="insights-panel-item">No recommendations available.</div>
              )
            ) : (
              <div className="insights-skeleton" style={{ width: '80%' }} />
            )}
          </div>

          <div className="insights-panel-section">
            <div className="insights-panel-section-title">Signals</div>
            <div className="insights-panel-item">
              [{data.duplicates?.length ?? 0}] duplicates
            </div>
            <div className="insights-panel-item">
              [{data.families?.length ?? 0}] families
            </div>
            <div className={`insights-panel-item${noiseCount > 0 ? ' insights-panel-item--danger' : ''}`}>
              [{noiseCount}{noiseCount > 0 ? '!' : ''}] noise
            </div>
            <div className={`insights-panel-item${gapCount > 0 ? ' insights-panel-item--danger' : ''}`}>
              [{gapCount}{gapCount > 0 ? '!' : ''}] gaps
            </div>
            {recsCount > 0 && (
              <div className="insights-panel-item insights-panel-item--priority">
                [{recsCount}] recommendations
              </div>
            )}
          </div>

        </div>

        {/* ── Right tabs panel ── */}
        <div className="insights-tabs-panel">
          <div className="insights-tabs">
            {tabs.map((tab) => (
              <button
                key={tab.key}
                className={`tab-btn${activeTab === tab.key ? ' active' : ''}`}
                onClick={() => setActiveTab(tab.key)}
              >
                {tab.label}
                <span className="tab-count">{tab.count}</span>
              </button>
            ))}
          </div>

          <div className="tab-content">
            {activeTab === 'duplicates'      && <DuplicatesView      data={data} report={report} />}
            {activeTab === 'families'        && <FamiliesView        data={data} />}
            {activeTab === 'merge'           && <MergeView           data={data} />}
            {activeTab === 'coverage'        && <CoverageView        data={data} report={report} />}
            {activeTab === 'noise'           && <NoiseView           data={data} report={report} />}
            {activeTab === 'unique'          && <UniqueView          data={data} />}
            {activeTab === 'recommendations' && <RecommendationsView report={report} />}
          </div>
        </div>

      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab views
// ---------------------------------------------------------------------------

function DuplicatesView({
  data,
  report,
}: {
  data: SimilarityResult;
  report: InsightsReport | null;
}) {
  if (!data.duplicates?.length) {
    return <div className="empty-state">No duplicate detections found.</div>;
  }
  return (
    <div className="card-list">
      {data.duplicates.map((dup, i) => (
        <div key={i} className="insight-card duplicate-card">
          <div className="card-header">
            <span className="similarity-badge" style={badgeStyle(dup.similarity)}>
              {(dup.similarity * 100).toFixed(0)}% Similar
            </span>
          </div>
          <div className="card-alerts">
            {dup.alert_names?.map((name, j) => (
              <div key={j} className="alert-name">{name}</div>
            ))}
          </div>
          <div className="card-explanation">
            {report?.enriched_dups?.[i] ?? dup.explanation}
          </div>
        </div>
      ))}
    </div>
  );
}

function FamiliesView({ data }: { data: SimilarityResult }) {
  if (!data.families?.length) {
    return <div className="empty-state">No detection families identified.</div>;
  }
  return (
    <div className="card-list">
      {data.families.map((fam, i) => (
        <div key={i} className="insight-card family-card">
          <div className="card-header">
            <h3>{fam.name}</h3>
            <span className="member-count">
              {fam.alert_names?.length ?? 0} detections
            </span>
          </div>
          <div className="card-alerts">
            {fam.alert_names?.map((name, j) => (
              <div key={j} className="alert-name">{name}</div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function MergeView({ data }: { data: SimilarityResult }) {
  if (!data.merge_suggestions?.length) {
    return (
      <div className="empty-state">
        No merge suggestions. Your detections are well-organised.
      </div>
    );
  }
  return (
    <div className="card-list">
      {data.merge_suggestions.map((sug, i) => (
        <div key={i} className="insight-card merge-card">
          <div className="card-header">
            <h3>Merge {sug.alert_names?.length ?? 0} rules into 1</h3>
          </div>
          <div className="card-reason">{sug.reason}</div>
          <div className="card-alerts">
            {sug.alert_names?.map((name, j) => (
              <div key={j} className="alert-name">{name}</div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function CoverageView({
  data,
  report,
}: {
  data: SimilarityResult;
  report: InsightsReport | null;
}) {
  if (!data.coverage_insights?.length) {
    return <div className="empty-state">No coverage insights available.</div>;
  }
  return (
    <div className="card-list">
      {data.coverage_insights.map((insight, i) => (
        <div key={i} className="insight-card coverage-card">
          <p>{report?.enriched_gaps?.[i] ?? insight}</p>
        </div>
      ))}
    </div>
  );
}

/**
 * NoiseView — renders structured NoiseAlert objects.
 * Falls back gracefully when `noise_alerts` is absent or empty.
 */
function NoiseView({
  data,
  report,
}: {
  data: SimilarityResult;
  report: InsightsReport | null;
}) {
  const alerts: NoiseAlert[] = data.noise_alerts ?? [];

  if (alerts.length === 0) {
    return <div className="empty-state">No noise alerts detected.</div>;
  }

  return (
    <div className="card-list">
      {alerts.map((noise, i) => {
        const enrichedExplanation = report?.noise_explanations?.[i] ?? null;
        return (
          <div key={i} className="insight-card noise-card">
            <div className="card-header">
              <div className="alert-name noise-alert-name">[!!] {noise.name}</div>
            </div>

            {noise.missing_features?.length > 0 && (
              <div className="noise-missing">
                <span className="noise-missing-label">Missing features:</span>{' '}
                {noise.missing_features.join(', ')}
              </div>
            )}

            <div className="card-explanation">
              {enrichedExplanation ??
                'Sparse feature vector — likely a threshold-only rule. Review for contextual conditions.'}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function UniqueView({ data }: { data: SimilarityResult }) {
  if (!data.unique_detections?.length) {
    return (
      <div className="empty-state">No unique (standalone) detections found.</div>
    );
  }
  return (
    <div className="card-list">
      {data.unique_detections.map((name, i) => (
        <div key={i} className="insight-card unique-card">
          <div className="alert-name">{name}</div>
        </div>
      ))}
    </div>
  );
}

function RecommendationsView({ report }: { report: InsightsReport | null }) {
  if (!report) {
    return (
      <div className="card-list">
        {[1, 2, 3].map((i) => (
          <div key={i} className="insight-card">
            <div className="insights-skeleton" style={{ width: '100%' }} />
            <div className="insights-skeleton" style={{ width: '80%' }} />
          </div>
        ))}
      </div>
    );
  }

  const recs = report.recommendations ?? [];

  if (recs.length === 0) {
    return (
      <div className="empty-state">No recommendations generated yet.</div>
    );
  }

  return (
    <div className="card-list">
      {recs.map((rec, i) => (
        <div key={i} className="insight-card recommendation-card">
          <div className="recommendation-index">{String(i + 1).padStart(2, '0')}</div>
          <div className="recommendation-text">{rec}</div>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

function badgeStyle(similarity: number): React.CSSProperties {
  const r = Math.round(255 * similarity);
  const g = Math.round(255 * (1 - similarity));
  return {
    backgroundColor: `rgba(${r}, ${g}, 60, 0.15)`,
    color: `rgb(${r}, ${g}, 60)`,
    border: `1px solid rgba(${r}, ${g}, 60, 0.3)`,
  };
}
