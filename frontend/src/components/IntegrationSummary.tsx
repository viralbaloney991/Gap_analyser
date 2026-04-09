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
      <div className="summary-header">
        <h2>{clientName} — Integration Summary</h2>
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

      <div className="stats-row">
        <div className="stat-card">
          <div className="stat-number">{stats.done_integrations}</div>
          <div className="stat-desc">Integrations</div>
        </div>
        <div className="stat-card">
          <div className="stat-number">{stats.total_alerts}</div>
          <div className="stat-desc">Active Alerts</div>
        </div>
        <div className="stat-card">
          <div className="stat-number" style={{ color: 'var(--accent)' }}>{stats.security_alerts}</div>
          <div className="stat-desc">Security Alerts</div>
        </div>
        {stats.vendor_covered_alerts > 0 && (
          <div className="stat-card">
            <div className="stat-number" style={{ color: 'var(--text-dim)' }}>
              {stats.vendor_covered_alerts}
            </div>
            <div className="stat-desc">Vendor Covered</div>
          </div>
        )}
        <div className="stat-card">
          <div className="stat-number" style={{ color: 'var(--accent)' }}>
            {stats.integrations_with_alerts}
          </div>
          <div className="stat-desc">With Coverage</div>
        </div>
        <div
          className="stat-card"
          style={blindSpots.length > 0
            ? { borderLeftColor: 'var(--danger)', background: 'var(--danger-dim)' }
            : {}}
        >
          <div className="stat-number" style={{ color: blindSpots.length > 0 ? 'var(--danger)' : 'var(--accent)' }}>
            {blindSpots.length}
          </div>
          <div className="stat-desc">Blind Spots</div>
        </div>
      </div>

      <div className="integration-table-wrap">
        <table className="integration-table">
          <thead>
            <tr>
              <th></th>
              <th>Integration</th>
              <th>Application</th>
              <th>Subsystem</th>
              <th>Alerts</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((integ, i) => (
              <tr key={i} className={integ.alert_count === 0 ? 'row-blind-spot' : ''}>
                <td>{integ.alert_count > 0 ? '\u2705' : '\u26A0\uFE0F'}</td>
                <td>{integ.name}</td>
                <td><code>{integ.application || '\u2014'}</code></td>
                <td><code>{integ.subsystem || '\u2014'}</code></td>
                <td className="alert-count">{integ.alert_count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="action-buttons">
        <button className="btn btn-action" onClick={onViewMITRE}>
          <div className="action-title">MITRE ATT&CK Coverage</div>
          <div className="action-desc">Technique-level detection coverage across the ATT&CK matrix</div>
        </button>
        <button className="btn btn-action" onClick={onViewInsights}>
          <div className="action-title">Alert Insights</div>
          <div className="action-desc">Find duplicates, gaps, and merge opportunities</div>
        </button>
      </div>
    </div>
  );
}
