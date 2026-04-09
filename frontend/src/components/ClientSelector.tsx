import { useState, useEffect, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { ComposableMap, Geographies, Geography, Marker } from 'react-simple-maps';
import { fetchClients } from '../services/api';
import type { ClientInfo } from '../types';

const GEO_URL = '/world-110m.json';

const REGION_COORDS: Record<string, [number, number]> = {
  eu1: [-8.2,   53.3],   // Dublin (EU1)
  eu2: [18.1,   59.3],   // Stockholm (EU2)
  us1: [-77.5,  37.8],   // Virginia (US1)
  us2: [-122.8, 45.5],   // Oregon (US2)
  ap1: [72.9,   19.1],   // Mumbai (AP1)
  ap2: [103.8,   1.3],   // Singapore (AP2)
  ap3: [139.7,  35.7],   // Tokyo (AP3)
};

interface Props {
  onAnalyze: (client: string) => void;
  loading: boolean;
}

interface TooltipState {
  name: string;
  region: string;
  x: number;
  y: number;
}

export default function ClientSelector({ onAnalyze, loading }: Props) {
  const [clients, setClients] = useState<ClientInfo[]>([]);
  const [selected, setSelected] = useState('');
  const [fetchError, setFetchError] = useState('');
  const [tooltip, setTooltip] = useState<TooltipState | null>(null);

  useEffect(() => {
    fetchClients()
      .then((list) => {
        setClients(list);
      })
      .catch(() => setFetchError('Failed to load client list'));
  }, []);

  const handleDotClick = useCallback((name: string) => {
    setSelected(name);
    setTooltip(null);
  }, []);

  const handleAnalyze = () => {
    if (selected && !loading) onAnalyze(selected);
  };

  return (
    <div className="client-selector">
      <div className="client-selector-grid" />

      <span className="client-selector-corner top-left">CX_ALERTS v2.1</span>
      <span className="client-selector-corner top-right">
        <span className="status-dot" />
        ONLINE
      </span>

      {fetchError && <div className="error-banner map-error">{fetchError}</div>}

      <ComposableMap
        className="world-map"
        projection="geoEqualEarth"
        projectionConfig={{ scale: 160 }}
      >
        <Geographies geography={GEO_URL}>
          {({ geographies }) =>
            geographies.map((geo) => (
              <Geography
                key={geo.rsmKey}
                geography={geo}
                className="map-geography"
              />
            ))
          }
        </Geographies>

        {clients.map((client) => {
          const coords = REGION_COORDS[client.region];
          if (!coords) return null;
          const isSelected = client.name === selected;
          return (
            <Marker
              key={client.name}
              coordinates={coords}
              onClick={() => handleDotClick(client.name)}
              onMouseEnter={(e: React.MouseEvent<SVGGElement>) =>
                setTooltip({ name: client.name, region: client.region, x: e.clientX, y: e.clientY })
              }
              onMouseLeave={() => setTooltip(null)}
            >
              <circle
                r={isSelected ? 7 : 5}
                className={`map-dot${isSelected ? ' map-dot--selected' : ''}`}
              />
              <circle
                r={isSelected ? 7 : 5}
                className={`map-dot-ring${isSelected ? ' map-dot-ring--selected' : ''}`}
              />
            </Marker>
          );
        })}
      </ComposableMap>

      {tooltip && createPortal(
        <div
          className="map-tooltip"
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          {tooltip.name} · {tooltip.region.toUpperCase()}
        </div>,
        document.body
      )}

      <div className="client-selector-content">
        <div className="selected-client-label">
          {selected ? `▶ ${selected}` : 'Select a client on the map'}
        </div>
        <h2 className="landing-wordmark"><strong>Alert</strong> Analyzer</h2>
        <p className="landing-subtitle">Coralogix Integration Intelligence</p>
        {selected && (
          <button
            className="btn btn-primary"
            onClick={handleAnalyze}
            disabled={loading || !selected}
          >
            {loading ? 'ANALYZING...' : `ANALYZE ${selected} →`}
          </button>
        )}
      </div>
    </div>
  );
}
