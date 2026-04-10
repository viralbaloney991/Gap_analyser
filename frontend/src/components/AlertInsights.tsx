import { useState } from 'react';
import type { SimilarityResult } from '../types';

interface Props {
  data: SimilarityResult;
}

type Tab = 'duplicates' | 'families' | 'merge' | 'coverage' | 'unique';

export default function AlertInsights({ data }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('duplicates');

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'duplicates', label: 'Duplicates', count: data.duplicates?.length || 0 },
    { key: 'families', label: 'Detection Families', count: data.families?.length || 0 },
    { key: 'merge', label: 'Merge Suggestions', count: data.merge_suggestions?.length || 0 },
    { key: 'coverage', label: 'Coverage Insights', count: data.coverage_insights?.length || 0 },
    { key: 'unique', label: 'Unique Detections', count: data.unique_detections?.length || 0 },
  ];

  return (
    <div className="alert-insights">
      <h2>Alert Insights</h2>

      <div className="insights-tabs">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            className={`tab-btn ${activeTab === tab.key ? 'active' : ''}`}
            onClick={() => setActiveTab(tab.key)}
          >
            {tab.label}
            <span className="tab-count">{tab.count}</span>
          </button>
        ))}
      </div>

      <div className="tab-content">
        {activeTab === 'duplicates' && <DuplicatesView data={data} />}
        {activeTab === 'families' && <FamiliesView data={data} />}
        {activeTab === 'merge' && <MergeView data={data} />}
        {activeTab === 'coverage' && <CoverageView data={data} />}
        {activeTab === 'unique' && <UniqueView data={data} />}
      </div>
    </div>
  );
}

function DuplicatesView({ data }: { data: SimilarityResult }) {
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
          <div className="card-explanation">{dup.explanation}</div>
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
            <span className="member-count">{fam.alert_names?.length || 0} detections</span>
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
    return <div className="empty-state">No merge suggestions. Your detections are well-organized.</div>;
  }
  return (
    <div className="card-list">
      {data.merge_suggestions.map((sug, i) => (
        <div key={i} className="insight-card merge-card">
          <div className="card-header">
            <h3>Merge {sug.alert_names?.length || 0} rules into 1</h3>
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

function CoverageView({ data }: { data: SimilarityResult }) {
  if (!data.coverage_insights?.length) {
    return <div className="empty-state">No coverage insights available.</div>;
  }
  return (
    <div className="card-list">
      {data.coverage_insights.map((insight, i) => (
        <div key={i} className="insight-card coverage-card">
          <p>{insight}</p>
        </div>
      ))}
    </div>
  );
}

function UniqueView({ data }: { data: SimilarityResult }) {
  if (!data.unique_detections?.length) {
    return <div className="empty-state">No unique (standalone) detections found.</div>;
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

function badgeStyle(similarity: number): React.CSSProperties {
  const r = Math.round(255 * similarity);
  const g = Math.round(255 * (1 - similarity));
  return {
    backgroundColor: `rgba(${r}, ${g}, 60, 0.15)`,
    color: `rgb(${r}, ${g}, 60)`,
    border: `1px solid rgba(${r}, ${g}, 60, 0.3)`,
  };
}
