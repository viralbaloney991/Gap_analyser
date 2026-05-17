import type { AnalyzeResponse, ClientInfo, CorrelationsResponse, ExportNarrativeReport, GenerationResult, HuntPayload, InsightsReport, LibraryResponse, MapTacticsResponse, MitreCatalog, NoiseAlert, NoiseResponse, PushResponse, SaveDetectionRequest, SuggestionsResponse } from '../types';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export async function fetchClients(): Promise<ClientInfo[]> {
  const res = await fetch(`${API_BASE}/api/clients`);
  if (!res.ok) throw new Error('Failed to fetch clients');
  return res.json();
}

export async function analyzeClient(client: string, refresh = false, lookbackDays = 30): Promise<AnalyzeResponse> {
  const url = refresh ? `${API_BASE}/api/analyze?refresh=true` : `${API_BASE}/api/analyze`;
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, lookback_days: lookbackDays }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Analysis failed' }));
    throw new Error(err.error || 'Analysis failed');
  }
  return res.json();
}

export async function fetchInsights(client: string): Promise<InsightsReport> {
  const res = await fetch(`${API_BASE}/api/insights`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Insights failed' }));
    throw new Error(err.error || 'Failed to fetch insights');
  }
  return res.json();
}

export async function fetchSuggestions(
  client: string,
  techniqueId: string,
  tactic: string,
  provider?: string,
  force = false,
): Promise<SuggestionsResponse> {
  const res = await fetch(`${API_BASE}/api/suggestions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client,
      technique_id: techniqueId,
      tactic,
      provider: provider || '',
      force,
    }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Suggestions failed' }));
    throw new Error(err.error || 'Failed to generate suggestions');
  }
  return res.json();
}

export async function fetchExportNarrative(client: string): Promise<ExportNarrativeReport> {
  const res = await fetch(`${API_BASE}/api/export/narrative`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Export failed' }));
    throw new Error(err.error || 'Export narrative failed');
  }
  if (res.status === 204) {
    throw new Error('No insights available yet. Please wait for analysis to complete.');
  }
  return res.json();
}

export async function fetchNoise(client: string, lookbackDays: number): Promise<NoiseAlert[]> {
  const res = await fetch(`${API_BASE}/api/noise`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, lookback_days: lookbackDays }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Noise fetch failed' }));
    throw new Error(err.error || 'Noise fetch failed');
  }
  const data: NoiseResponse = await res.json();
  return data.noise_alerts;
}

export async function fetchCorrelations(
  client: string,
  gapProse: string,
  logSources: string[],
  coveredTechniques: string[],
  force = false,
  signal?: AbortSignal,
): Promise<CorrelationsResponse> {
  const res = await fetch(`${API_BASE}/api/correlations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client,
      gap_prose: gapProse,
      log_sources: logSources,
      covered_techniques: coveredTechniques,
      force,
    }),
    signal,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Correlations failed' }));
    throw new Error(err.error || 'Failed to generate correlation suggestions');
  }
  return res.json();
}

export async function fetchMapTactics(
  client: string,
  prose: string,
  logSource: string,
): Promise<MapTacticsResponse> {
  const res = await fetch(`${API_BASE}/api/map-tactics`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, prose, log_source: logSource }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Map tactics failed' }));
    throw new Error(err.error ?? 'Map tactics failed');
  }
  return res.json();
}

export async function buildDetection(
  client: string,
  techniques: Array<{
    id: string; name: string;
    tactic_id: string; tactic_name: string;
    tactic_order: number; source: string;
  }>,
  provider = '',
  force = false,
): Promise<GenerationResult> {
  const res = await fetch(`${API_BASE}/api/build-detection`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client, techniques, provider, force }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Build detection failed' }));
    throw new Error(err.error || 'Failed to generate detection');
  }
  return res.json();
}


export async function fetchMitreCatalog(): Promise<MitreCatalog> {
  const res = await fetch(`${API_BASE}/api/mitre-catalog`);
  if (!res.ok) throw new Error('Failed to fetch MITRE catalog');
  return res.json();
}

export function openHuntStream(detection: HuntPayload): EventSource {
  const params = new URLSearchParams({
    lucene:      detection.logic,
    window:      detection.window,
    name:        detection.name,
    severity:    detection.severity,
    techniqueId: detection.techniqueId,
    tacticId:    detection.tacticId,
    source:      detection.source,
    client:      detection.client,
  });
  return new EventSource(`${API_BASE}/api/hunt/stream?${params.toString()}`);
}

export async function exportHuntReport(huntId: string): Promise<void> {
  const url = `${API_BASE}/api/hunt/export?id=${encodeURIComponent(huntId)}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Export failed: ${res.statusText}`);
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = res.headers.get('Content-Disposition')?.match(/filename="(.+)"/)?.[1] ?? 'hunt-report.md';
  a.click();
  URL.revokeObjectURL(a.href);
}

export async function saveDetection(payload: SaveDetectionRequest): Promise<{ id: string }> {
  const res = await fetch(`${API_BASE}/api/library`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Save failed' }));
    throw new Error(err.error || 'Failed to save detection');
  }
  return res.json();
}

export async function listDetections(filter?: { client?: string; severity?: string }): Promise<LibraryResponse> {
  const params = new URLSearchParams();
  if (filter?.client) params.set('client', filter.client);
  if (filter?.severity) params.set('severity', filter.severity);
  const res = await fetch(`${API_BASE}/api/library?${params}`);
  if (!res.ok) throw new Error('Failed to fetch library');
  return res.json();
}

export async function deleteDetection(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/library/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete detection');
}

export async function pushDetection(id: string): Promise<PushResponse> {
  const res = await fetch(`${API_BASE}/api/library/${id}/push`, { method: 'POST' });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Push failed' }));
    throw new Error(err.error || 'Failed to push detection');
  }
  return res.json();
}

export async function exportDetections(client?: string): Promise<void> {
  const params = new URLSearchParams();
  if (client) params.set('client', client);
  const res = await fetch(`${API_BASE}/api/library/export?${params}`);
  if (!res.ok) throw new Error('Export failed');
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  const cd = res.headers.get('Content-Disposition') ?? '';
  const match = cd.match(/filename="([^"]+)"/);
  a.download = match ? match[1] : 'detections.zip';
  a.click();
  URL.revokeObjectURL(url);
}
