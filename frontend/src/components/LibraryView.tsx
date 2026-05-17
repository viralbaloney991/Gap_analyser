import { useState, useEffect, useMemo } from 'react';
import type { SavedDetection, PushResponse } from '../types';
import { listDetections, deleteDetection, pushDetection, exportDetections } from '../services/api';
import DetectionCard from './DetectionCard';

interface LibraryViewProps {
  clientName: string;
}

export default function LibraryView({ clientName }: LibraryViewProps) {
  const [detections, setDetections] = useState<SavedDetection[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [clientFilter, setClientFilter] = useState(clientName);
  const [severityFilter, setSeverityFilter] = useState('');
  const [exporting, setExporting] = useState(false);

  useEffect(() => {
    setLoading(true);
    setError('');
    listDetections()
      .then(r => setDetections(r.detections))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  const clients = useMemo(() => {
    const set = new Set(detections.map(d => d.client));
    return Array.from(set).sort();
  }, [detections]);

  const filtered = useMemo(() => {
    return detections.filter(d => {
      if (clientFilter && d.client !== clientFilter) return false;
      if (severityFilter && d.severity !== severityFilter) return false;
      if (search) {
        const q = search.toLowerCase();
        return d.title.toLowerCase().includes(q)
          || d.technique_id.toLowerCase().includes(q)
          || d.tactic.toLowerCase().includes(q);
      }
      return true;
    });
  }, [detections, clientFilter, severityFilter, search]);

  const handleDelete = async (id: string) => {
    try {
      await deleteDetection(id);
      setDetections(prev => prev.filter(d => d.id !== id));
    } catch (e) {
      alert('Failed to delete: ' + (e instanceof Error ? e.message : 'unknown error'));
    }
  };

  const handlePush = (id: string): Promise<PushResponse> => pushDetection(id);

  const handleExport = async () => {
    setExporting(true);
    try {
      await exportDetections(clientFilter || undefined);
    } catch (e) {
      alert('Export failed: ' + (e instanceof Error ? e.message : 'unknown error'));
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="library-view">
      <div className="library-header">
        <div className="library-title-row">
          <div>
            <div className="section-label">DETECTION LIBRARY</div>
            <div className="library-count">{filtered.length} detection{filtered.length !== 1 ? 's' : ''}</div>
          </div>
          <button className="btn-export" onClick={handleExport} disabled={exporting || filtered.length === 0}>
            {exporting ? 'Exporting…' : '↓ Export Sigma (.zip)'}
          </button>
        </div>

        <div className="library-filters">
          <input
            className="library-search"
            placeholder="Search by title, technique, tactic…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          <select className="library-select" value={clientFilter} onChange={e => setClientFilter(e.target.value)}>
            <option value="">All clients</option>
            {clients.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
          <select className="library-select" value={severityFilter} onChange={e => setSeverityFilter(e.target.value)}>
            <option value="">All severities</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>
      </div>

      {loading && <div className="library-empty">Loading…</div>}
      {error && <div className="library-empty library-error">{error}</div>}

      {!loading && !error && filtered.length === 0 && (
        <div className="library-empty">
          No detections saved yet. Use the <strong>Save</strong> button on any detection in the Builder or MITRE panel.
        </div>
      )}

      {!loading && !error && filtered.length > 0 && (
        <div className="library-grid">
          {filtered.map(d => (
            <DetectionCard
              key={d.id}
              detection={d}
              onDelete={handleDelete}
              onPush={handlePush}
            />
          ))}
        </div>
      )}
    </div>
  );
}
