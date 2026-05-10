import type { AnalyzeResponse } from '../types';

interface Props {
  data: AnalyzeResponse;
  clientName: string;
  loading: boolean;
  onViewMITRE: () => void;
  onViewInsights: () => void;
  onViewGraph: () => void;
  onViewBuilder: () => void;
  onRefresh: () => void;
}

export default function IntegrationSummary({ data, clientName, loading, onViewMITRE, onViewInsights, onViewGraph, onViewBuilder, onRefresh }: Props) {
  const { integrations, stats } = data;
  const sorted     = [...integrations].sort((a, b) => b.alert_count - a.alert_count);
  const blindSpots = integrations.filter((i) => i.alert_count === 0);

  // Coverage % for the header
  const coveragePct = stats.done_integrations > 0
    ? Math.round((stats.integrations_with_alerts / stats.done_integrations) * 100)
    : 0;

  return (
    <div className="integration-summary">

      {/* ── Left panel ── */}
      <div className="summary-panel">
        <div className="summary-panel-header">
          <div className="summary-client-label">Analyzing</div>
          <div className="summary-client-name">{clientName}</div>
        </div>

        <div className="summary-panel-stats">
          <div className="stat-card">
            <div className="stat-number">{stats.done_integrations}</div>
            <div className="stat-desc">Integrations</div>
          </div>
          <div className="stat-card">
            <div className="stat-number">{stats.total_alerts}</div>
            <div className="stat-desc">Active Alerts</div>
          </div>
          <div className="stat-card">
            <div className="stat-number">{stats.security_alerts}</div>
            <div className="stat-desc">Security Alerts</div>
          </div>
          {stats.vendor_covered_alerts > 0 && (
            <div className="stat-card">
              <div className="stat-number">{stats.vendor_covered_alerts}</div>
              <div className="stat-desc">Vendor Covered</div>
            </div>
          )}
          <div className="stat-card">
            <div className="stat-number">{stats.integrations_with_alerts}</div>
            <div className="stat-desc">With Coverage</div>
          </div>
          <div className="stat-card stat-card--danger">
            <div className="stat-number">{blindSpots.length}</div>
            <div className="stat-desc">Blind Spots</div>
          </div>
        </div>

        <div className="summary-panel-actions-label">Explore</div>
        <div className="summary-panel-actions">
          <button className="btn-action" onClick={onViewMITRE}>
            <div className="action-icon">▦</div>
            <div>
              <span className="action-title">MITRE Coverage</span>
              <span className="action-desc">ATT&CK heatmap &amp; gaps</span>
            </div>
          </button>
          <button className="btn-action" onClick={onViewInsights}>
            <div className="action-icon">◈</div>
            <div>
              <span className="action-title">Alert Insights</span>
              <span className="action-desc">AI-powered analysis</span>
            </div>
          </button>
          <button className="btn-action" onClick={onViewGraph}>
            <div className="action-icon">⬡</div>
            <div>
              <span className="action-title">Threat Graph</span>
              <span className="action-desc">Alert correlation network</span>
            </div>
          </button>
          <button className="btn-action" onClick={onViewBuilder}>
            <div className="action-icon">⚡</div>
            <div>
              <span className="action-title">Build detections</span>
              <span className="action-desc">Compose multi-stage flow alerts</span>
            </div>
          </button>
          <button className="btn-action" onClick={onRefresh} disabled={loading}>
            <div className="action-icon">↻</div>
            <div>
              <span className="action-title">Refresh</span>
              <span className="action-desc">{loading ? 'Refreshing...' : 'Re-fetch live data'}</span>
            </div>
          </button>
        </div>
      </div>

      {/* ── Right panel ── */}
      <div className="summary-table-panel">
        <div className="summary-table-header">
          <div>
            <div className="summary-table-title">Integrations</div>
            <div className="summary-table-sub">
              {sorted.length} integration{sorted.length !== 1 ? 's' : ''} · sorted by alert volume
            </div>
          </div>
          <div style={{ textAlign: 'right' }}>
            <div className="coverage-pct">{coveragePct}%</div>
            <div className="coverage-label">Coverage</div>
          </div>
        </div>

        {sorted.length === 0 ? (
          <div className="summary-state-wrap">
            <div className="state-empty">
              <div className="state-empty__icon">◎</div>
              <div className="state-empty__title">No integrations found</div>
              <div className="state-empty__body">This client has no integrations configured in the analyzer.</div>
            </div>
          </div>
        ) : (
          <div className="integration-table-wrap">
            <table className="integration-table">
              <thead>
                <tr>
                  <th>Integration</th>
                  <th>Status</th>
                  <th>Coverage</th>
                  <th>Alerts</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((integration) => {
                  const isBlind = integration.alert_count === 0;
                  return (
                    <tr key={integration.name} className={isBlind ? 'row-blind-spot' : ''}>
                      <td>
                        <div className="integration-name">{integration.name}</div>
                        <div className="integration-type">{integration.application} · {integration.subsystem}</div>
                      </td>
                      <td>
                        <span className="status-tag status-tag--ok">Active</span>
                      </td>
                      <td>
                        <span className={`status-tag ${isBlind ? 'status-tag--blind' : 'status-tag--ok'}`}>
                          {isBlind ? 'Blind Spot' : 'Covered'}
                        </span>
                      </td>
                      <td>
                        <div className="alert-count" style={isBlind ? { color: 'var(--danger)' } : {}}>
                          {integration.alert_count}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
