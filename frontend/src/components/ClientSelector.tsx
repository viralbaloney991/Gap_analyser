import { useState, useEffect } from 'react';
import { fetchClients } from '../services/api';

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients] = useState<string[]>([]);
  const [selected, setSelected] = useState('');
  const [fetchError, setFetchError] = useState('');

  useEffect(() => {
    fetchClients()
      .then((list) => {
        setClients(list);
        if (list.length > 0) setSelected(list[0]);
      })
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (selected) onAnalyze(selected);
  };

  return (
    <div className="client-selector">
      <div className="client-selector-grid" />
      <span className="client-selector-corner top-left">CX_ALERTS v2.1</span>
      <span className="client-selector-corner top-right">
        <span className="status-dot" />
        ONLINE
      </span>
      <div className="client-selector-content">
        <h2 className="landing-wordmark"><strong>Alert</strong> Analyzer</h2>
        <p className="landing-subtitle">Coralogix Integration Intelligence</p>
        {fetchError && <div className="error-banner">{fetchError}</div>}
        <form className="landing-form" onSubmit={handleSubmit}>
          <select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
            disabled={loading || clients.length === 0}
          >
            {clients.length === 0 && <option value="">Loading...</option>}
            {clients.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
          <button
            type="submit"
            className="btn btn-primary"
            disabled={loading || !selected}
          >
            {loading ? 'ANALYZING...' : 'ANALYZE →'}
          </button>
        </form>
      </div>
    </div>
  );
}
