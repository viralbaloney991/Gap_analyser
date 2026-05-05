import type { AnalyzeResponse, ClientInfo, ExportNarrativeReport, InsightsReport, NoiseAlert, NoiseResponse, SuggestionsResponse } from '../types';

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
