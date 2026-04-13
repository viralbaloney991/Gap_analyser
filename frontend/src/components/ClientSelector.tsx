import { useState, useEffect, useRef } from 'react';
import { fetchClients } from '../services/api';
import type { ClientInfo } from '../types';

// Human-readable region labels and geographic groupings
const REGION_META: Record<string, { label: string; city: string; group: string }> = {
  eu1: { label: 'EU1', city: 'Dublin',    group: 'Europe'        },
  eu2: { label: 'EU2', city: 'Stockholm', group: 'Europe'        },
  us1: { label: 'US1', city: 'Virginia',  group: 'Americas'      },
  us2: { label: 'US2', city: 'Oregon',    group: 'Americas'      },
  ap1: { label: 'AP1', city: 'Mumbai',    group: 'Asia Pacific'  },
  ap2: { label: 'AP2', city: 'Singapore', group: 'Asia Pacific'  },
  ap3: { label: 'AP3', city: 'Tokyo',     group: 'Asia Pacific'  },
};

const GROUP_ORDER = ['Europe', 'Americas', 'Asia Pacific'];

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients] = useState<ClientInfo[]>([]);
  const [selected, setSelected] = useState('');
  const [fetchError, setFetchError] = useState('');
  const [slowLoad, setSlowLoad] = useState(false);
  const slowTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (loading) {
      slowTimer.current = setTimeout(() => setSlowLoad(true), 4000);
    } else {
      if (slowTimer.current) clearTimeout(slowTimer.current);
      setSlowLoad(false);
    }
    return () => { if (slowTimer.current) clearTimeout(slowTimer.current); };
  }, [loading]);

  useEffect(() => {
    fetchClients()
      .then(setClients)
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  const handleAnalyze = () => {
    if (selected && !loading) onAnalyze(selected);
  };

  // Group clients by geographic region group
  const grouped: Record<string, ClientInfo[]> = {};
  for (const client of clients) {
    const meta = REGION_META[client.region];
    const group = meta?.group ?? 'Other';
    if (!grouped[group]) grouped[group] = [];
    grouped[group].push(client);
  }

  return (
    <div className="client-selector">
      {/* Scanline grid background */}
      <div className="client-selector-grid" />

      <span className="client-selector-corner top-left">CX_ALERTS v2.1</span>

      {fetchError && (
        <div className="error-banner map-error">{fetchError}</div>
      )}

      {/* Region cards grid */}
      <div className="region-cards-wrap">
        {GROUP_ORDER.map((group) => {
          const groupClients = grouped[group];
          if (!groupClients?.length) return null;
          return (
            <div key={group} className="region-group">
              <div className="region-group-label">{group}</div>
              <div className="region-group-cards">
                {groupClients.map((client) => {
                  const meta = REGION_META[client.region];
                  const isSelected = client.name === selected;
                  return (
                    <button
                      key={client.name}
                      className={`region-card${isSelected ? ' region-card--selected' : ''}`}
                      onClick={() => setSelected(isSelected ? '' : client.name)}
                      disabled={loading}
                      title={`${client.name} · ${meta?.city ?? client.region}`}
                    >
                      <span className="region-card-tag">
                        {meta?.label ?? client.region.toUpperCase()}
                      </span>
                      <span className="region-card-name">{client.name}</span>
                      {meta?.city && (
                        <span className="region-card-city">{meta.city}</span>
                      )}
                      {isSelected && (
                        <span className="region-card-indicator" aria-hidden="true" />
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}

        {/* Ungrouped fallback */}
        {grouped['Other']?.length ? (
          <div className="region-group">
            <div className="region-group-label">Other</div>
            <div className="region-group-cards">
              {grouped['Other'].map((client) => {
                const isSelected = client.name === selected;
                return (
                  <button
                    key={client.name}
                    className={`region-card${isSelected ? ' region-card--selected' : ''}`}
                    onClick={() => setSelected(isSelected ? '' : client.name)}
                    disabled={loading}
                  >
                    <span className="region-card-tag">
                      {client.region.toUpperCase()}
                    </span>
                    <span className="region-card-name">{client.name}</span>
                    {isSelected && (
                      <span className="region-card-indicator" aria-hidden="true" />
                    )}
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}

        {clients.length === 0 && !fetchError && (
          <div className="region-cards-loading">
            {[1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="region-card region-card--skeleton" aria-hidden="true" />
            ))}
          </div>
        )}
      </div>

      {/* Bottom content block */}
      <div className="client-selector-content">
        <div className="selected-client-label">
          {selected ? `[ ${selected} ]` : 'Select a client above'}
        </div>
        <h2 className="landing-wordmark"><strong>Alert</strong> Analyzer</h2>
        <p className="landing-subtitle">Coralogix Integration Intelligence</p>
        {selected && (
          <button
            className="btn btn-primary"
            onClick={handleAnalyze}
            disabled={loading || !selected}
          >
            {loading ? 'ANALYZING...' : `[ ANALYZE ${selected} ]`}
          </button>
        )}
        {slowLoad && (
          <p className="slow-load-hint">
            Fetching live data — first run may take ~90s...
          </p>
        )}
      </div>
    </div>
  );
}
