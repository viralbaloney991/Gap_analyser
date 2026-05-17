import { useState } from 'react';
import type { SavedDetection, PushResponse } from '../types';

export type PushState = 'idle' | 'pushing' | 'pushed' | 'error';

interface DetectionCardProps {
  detection: SavedDetection;
  onDelete?: (id: string) => void;
  onPush?: (id: string) => Promise<PushResponse>;
}

const SEV_COLOR: Record<string, string> = {
  critical: '#ef4444',
  high:     '#f97316',
  medium:   '#eab308',
  low:      '#22c55e',
};

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export default function DetectionCard({ detection: d, onDelete, onPush }: DetectionCardProps) {
  const [expanded, setExpanded] = useState(false);
  const [pushState, setPushState] = useState<PushState>('idle');
  const [pushUrl, setPushUrl] = useState('');

  const handlePush = async () => {
    if (!onPush || pushState === 'pushing') return;
    setPushState('pushing');
    try {
      const res = await onPush(d.id);
      setPushUrl(res.url);
      setPushState('pushed');
    } catch {
      setPushState('error');
      setTimeout(() => setPushState('idle'), 3000);
    }
  };

  const sevColor = SEV_COLOR[d.severity] ?? '#6366f1';

  return (
    <div className="det-card">
      <div className="det-card-header">
        <span className="det-sev-chip" style={{ background: `${sevColor}22`, color: sevColor }}>
          {d.severity.toUpperCase()}
        </span>
        <span className="det-source-badge">{d.source}</span>
      </div>

      <div className="det-card-title">{d.title}</div>
      <div className="det-card-meta">
        {d.technique_id} · {d.log_source} · {d.client} · {relativeTime(d.created_at)}
      </div>

      <div className="det-card-actions">
        <button className="btn-small" onClick={() => setExpanded(e => !e)}>
          {expanded ? 'Hide' : 'View'}
        </button>

        {onPush && pushState === 'idle' && (
          <button className="btn-small btn-push" onClick={handlePush}>Push →CX</button>
        )}
        {pushState === 'pushing' && <span className="det-push-status">Pushing…</span>}
        {pushState === 'pushed' && (
          <a className="det-push-status det-push-ok" href={pushUrl} target="_blank" rel="noreferrer">
            ✓ Pushed ↗
          </a>
        )}
        {pushState === 'error' && <span className="det-push-status det-push-err">✗ Error</span>}

        {onDelete && (
          <button className="btn-small btn-danger" onClick={() => onDelete(d.id)}>✕</button>
        )}
      </div>

      {expanded && (
        <pre className="det-card-sigma">{d.sigma_rule || '# No Sigma rule'}</pre>
      )}
    </div>
  );
}
