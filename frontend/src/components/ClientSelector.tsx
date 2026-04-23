import { useState, useEffect } from 'react';
import { fetchClients } from '../services/api';
import type { ClientInfo } from '../types';

const REGION_META: Record<string, { label: string; city: string; group: string }> = {
  eu1: { label: 'EU1', city: 'Dublin',    group: 'Europe'       },
  eu2: { label: 'EU2', city: 'Stockholm', group: 'Europe'       },
  us1: { label: 'US1', city: 'Virginia',  group: 'Americas'     },
  us2: { label: 'US2', city: 'Oregon',    group: 'Americas'     },
  ap1: { label: 'AP1', city: 'Mumbai',    group: 'Asia Pacific' },
  ap2: { label: 'AP2', city: 'Singapore', group: 'Asia Pacific' },
  ap3: { label: 'AP3', city: 'Tokyo',     group: 'Asia Pacific' },
};

const GROUP_ORDER = ['Europe', 'Americas', 'Asia Pacific'];

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients]         = useState<ClientInfo[]>([]);
  const [selected, setSelected]       = useState('');
  const [fetchError, setFetchError]   = useState('');
  const [searchQuery, setSearchQuery] = useState('');

  useEffect(() => {
    fetchClients()
      .then(setClients)
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  // Filter clients by search query (case-insensitive, matches name or region)
  const filteredClients = searchQuery.trim()
    ? clients.filter((c) =>
        c.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        c.region.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : clients;

  // Group filtered clients
  const grouped: Record<string, ClientInfo[]> = {};
  for (const client of filteredClients) {
    const meta = REGION_META[client.region];
    const group = meta?.group ?? 'Other';
    if (!grouped[group]) grouped[group] = [];
    grouped[group].push(client);
  }

  const allGroups = [...GROUP_ORDER, 'Other'].filter((g) => grouped[g]?.length);
  const totalVisible = filteredClients.length;
  const hasNoResults = searchQuery.trim() !== '' && totalVisible === 0;

  return (
    <div className="client-selector">
      {/* Hero */}
      <div className="cs-hero">
        <div className="cs-eyebrow">Alert Analysis Engine</div>
        <h1 className="cs-title">Select a <em>client</em></h1>
        <p className="cs-subtitle">Analyze detection coverage, identify gaps, and reduce alert fatigue.</p>
      </div>

      {/* Search bar */}
      <div className="cs-search">
        <span className="cs-search__icon">⌕</span>
        <input
          className="cs-search__input"
          type="text"
          placeholder="Filter clients..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          autoComplete="off"
        />
        <span className="cs-search__count">
          {clients.length === 0 ? 'Loading...' : `${totalVisible} client${totalVisible !== 1 ? 's' : ''}`}
        </span>
      </div>

      {fetchError && (
        <div className="error-banner map-error">{fetchError}</div>
      )}

      {/* Region groups */}
      <div className="cs-content">
        {clients.length === 0 && !fetchError && (
          <div className="cs-skeletons">
            {[1, 2, 3, 4, 5, 6].map((i) => (
              <div key={i} className="region-card region-card--skeleton skeleton" aria-hidden="true" />
            ))}
          </div>
        )}

        {hasNoResults && (
          <div className="cs-empty">
            No clients match &ldquo;{searchQuery}&rdquo;
          </div>
        )}

        {allGroups.map((group) => {
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
                    >
                      {isSelected && <span className="region-card__dot" aria-hidden="true" />}
                      <div className="region-card__name">{client.name}</div>
                      <div className="region-card__meta">
                        <span>{meta?.city ?? client.region}</span>
                        <span>{meta?.label ?? client.region.toUpperCase()}</span>
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>

      {/* Sticky CTA — only visible when a client is selected */}
      <div className={`cs-cta${selected ? ' cs-cta--visible' : ''}`}>
        <span className="cs-cta__hint">{selected} selected</span>
        <button
          className="cs-cta__btn"
          onClick={() => { if (selected && !loading) onAnalyze(selected); }}
          disabled={loading || !selected}
        >
          {loading ? 'Analyzing...' : `Analyze ${selected} →`}
        </button>
      </div>
    </div>
  );
}
