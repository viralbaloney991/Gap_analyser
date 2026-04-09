import type { AnalyzeResponse } from '../types';

interface Props {
  data: AnalyzeResponse;
  clientName: string;
  loading: boolean;
  onViewMITRE: () => void;
  onViewInsights: () => void;
  onRefresh: () => void;
}

export default function IntegrationSummary({ data, clientName, loading, onViewMITRE, onViewInsights, onRefresh }: Props) {
  const { integrations, stats } = data;

  const sorted = [...integrations].sort((a, b) => b.alert_count - a.alert_count);
  const blindSpots = integrations.filter((i) => i.alert_count === 0);

  return (
    <div className="integration-summary">
      <div className="summary-grid">

        {/* Left: Command Panel */}
        <div className="summary-panel">
          <div className="summary-panel-header">
            <h2>[ {clientName} ]<br />Integration Summary</h2>
            <div className="cache-info">
              {data.cached && <span className="cache-badge">Cached</span>}
              <button
                className="btn btn-small"
                onClick={onRefresh}
                disabled={loading}
              >
                {loading ? 'Refreshing...' : 'Refresh'}
              </button>
            </div>
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
            <div className={`stat-card${blindSpots.length > 0 ? ' stat-card--danger' : ''}`}>
              <div className="stat-number">{blindSpots.length}</div>
              <div className="stat-desc">Blind Spots</div>
            </div>
          </div>

          <div className="summary-panel-actions">
            <button className="btn-action" onClick={onViewMITRE}>
              <div className="action-title">→ MITRE ATT&CK Coverage</div>
              <div className="action-desc">Technique-level detection coverage across the ATT&CK matrix</div>
            </button>
            <button className="btn-action" onClick={onViewInsights}>
              <div className="action-title">→ Alert Insights</div>
              <div className="action-desc">Find duplicates, gaps, and merge opportunities</div>
            </button>
          </div>
        </div>

        {/* Right: Table Panel */}
        <div className="summary-table-panel">
          <div className="integration-table-wrap">
            <table className="integration-table">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Integration</th>
                  <th>Application</th>
                  <th>Alerts</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((integ) => (
                  <tr key={integ.name} className={integ.alert_count === 0 ? 'row-blind-spot' : ''}>
                    <td>
                      {integ.alert_count > 0
                        ? <span className="status-tag status-tag--ok">[OK]</span>
                        : <span className="status-tag status-tag--blind">[!!]</span>
                      }
                    </td>
                    <td>{integ.name}</td>
                    <td><code>{integ.application || '—'}</code></td>
                    <td className="alert-count">{integ.alert_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

      </div>
    </div>
  );
}
