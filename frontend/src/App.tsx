import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import type { Transition } from 'framer-motion';
import ClientSelector from './components/ClientSelector';
import IntegrationSummary from './components/IntegrationSummary';
import MITREHeatmap from './components/MITREHeatmap';
import AlertInsights from './components/AlertInsights';
import ThreatGraph from './components/ThreatGraph';
import DetectionBuilder from './components/DetectionBuilder';
import { analyzeClient, fetchInsights, fetchNoise } from './services/api';
import type { AnalyzeResponse, InsightsReport, SimilarityResult } from './types';
import './App.css';

const VALID_LOOKBACK = [7, 14, 30, 90];

type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder';

const FADE_UP_TRANSITION: Transition = { duration: 0.2, ease: 'easeOut' };

const FADE_UP = {
  initial:    { opacity: 0, y: 8 },
  animate:    { opacity: 1, y: 0 },
  exit:       { opacity: 0, y: -8 },
  transition: FADE_UP_TRANSITION,
};

function App() {
  const [view, setView]                   = useState<View>('form');
  const [loading, setLoading]             = useState(false);
  const [error, setError]                 = useState('');
  const [data, setData]                   = useState<AnalyzeResponse | null>(null);
  const [clientName, setClientName]       = useState('');
  const [insightsReport, setInsightsReport] = useState<InsightsReport | null>(null);
  const [insightsError, setInsightsError] = useState(false);
  const [noiseLoading, setNoiseLoading] = useState(false);
  const [builderPreselectedIds, setBuilderPreselectedIds] = useState<string[]>([]);
  const [lookbackDays, setLookbackDays] = useState<number>(() => {
    const stored = localStorage.getItem('noise_lookback_days');
    const parsed = stored ? Number(stored) : 30;
    return VALID_LOOKBACK.includes(parsed) ? parsed : 30;
  });

  const updateLookback = (days: number) => {
    setLookbackDays(days);
    localStorage.setItem('noise_lookback_days', String(days));
  };

  const handleReanalyze = async (days: number) => {
    if (!data) return;
    const prevDays = lookbackDays;
    updateLookback(days);
    setNoiseLoading(true);
    try {
      const noiseAlerts = await fetchNoise(clientName, days);
      setData(prev => prev
        ? { ...prev, alert_insights: { ...(prev.alert_insights ?? {}), noise_alerts: noiseAlerts } as SimilarityResult }
        : prev
      );
    } catch (e) {
      console.warn('[noise reanalyze]', e);
      updateLookback(prevDays);
    } finally {
      setNoiseLoading(false);
    }
  };

  const handleAnalyze = async (client: string, refresh = false, days = lookbackDays) => {
    setLoading(true);
    setError('');
    setInsightsReport(null);
    setInsightsError(false);
    try {
      const result = await analyzeClient(client, refresh, days);
      setData(result);
      setClientName(client);
      setView('summary');
      fetchInsights(client)
        .then(setInsightsReport)
        .catch((e) => { console.warn('[insights]', e); setInsightsError(true); });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Analysis failed');
    } finally {
      setLoading(false);
    }
  };

  const goBack = () => {
    if (view === 'builder') setBuilderPreselectedIds([]);
    if (view === 'mitre' || view === 'insights' || view === 'graph' || view === 'builder') {
      setView('summary');
    } else {
      setView('form');
      setData(null);
      setClientName('');
      setInsightsReport(null);
      setInsightsError(false);
    }
    setError('');
  };

  const goHome = () => {
    setView('form');
    setData(null);
    setClientName('');
    setInsightsReport(null);
    setInsightsError(false);
    setBuilderPreselectedIds([]);
    setError('');
  };

  const handleBuildDetectionFromInsights = (tacticIds: string[], techniqueIds: string[]) => {
    void tacticIds; // tactic selection is driven by technique membership in DetectionBuilder
    setBuilderPreselectedIds(techniqueIds);
    setView('builder');
  };

  // Build breadcrumb segments for non-landing views
  const breadcrumb: { label: string }[] = view !== 'form' && clientName
    ? [
        { label: clientName },
        ...(view === 'summary' ? [] : [{
            label: view === 'mitre' ? 'MITRE Coverage'
                 : view === 'insights' ? 'Alert Insights'
                 : view === 'graph' ? 'Threat Graph'
                 : 'Build detections'
          }]),
      ]
    : [];

  return (
    <div className="app">
      <header className="app-header">
        <div className="header-left">
          <button className="app-logo" onClick={goHome}>
            CX<em>Alert</em>
          </button>
          {breadcrumb.length > 0 && (
            <div className="app-breadcrumb">
              {breadcrumb.map((seg, i) => (
                <span key={i}>
                  {i > 0 && <span className="app-breadcrumb-sep">/</span>}
                  <span>{seg.label}</span>
                </span>
              ))}
            </div>
          )}
        </div>

        <div className="header-right">
          {view !== 'form' && (
            <button className="btn-small" onClick={goBack}>
              ← Back
            </button>
          )}
          {data?.cached && <span className="cache-badge">Cached</span>}
          <div className="header-status">
            <span className="status-dot" />
            ONLINE
          </div>
        </div>
      </header>

      <main className={`app-main${view === 'form' ? ' app-main--landing' : ''}`}>
        {error && <div className="error-banner">{error}</div>}

        <AnimatePresence mode="wait">
          {view === 'form' && (
            <motion.div key="form" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
              <ClientSelector onAnalyze={handleAnalyze} loading={loading} />
            </motion.div>
          )}

          {view === 'summary' && data && (
            <motion.div key="summary" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <IntegrationSummary
                data={data}
                clientName={clientName}
                loading={loading}
                onViewMITRE={() => setView('mitre')}
                onViewInsights={() => setView('insights')}
                onViewGraph={() => setView('graph')}
                onViewBuilder={() => { setBuilderPreselectedIds([]); setView('builder'); }}
                onRefresh={() => handleAnalyze(clientName, true)}
              />
            </motion.div>
          )}

          {view === 'mitre' && data && (
            <motion.div key="mitre" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <MITREHeatmap data={data.mitre_coverage} clientName={clientName} />
            </motion.div>
          )}

          {view === 'insights' && data && (
            <motion.div key="insights" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <AlertInsights
                data={data.alert_insights}
                report={insightsReport}
                insightsError={insightsError}
                client={clientName}
                mitreCoverage={data.mitre_coverage}
                totalAlerts={data.stats.total_alerts}
                lookbackDays={lookbackDays}
                onReanalyze={handleReanalyze}
                noiseLoading={noiseLoading}
                onBuildDetection={handleBuildDetectionFromInsights}
              />
            </motion.div>
          )}

          {view === 'graph' && data && (
            <motion.div key="graph" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <ThreatGraph
                data={data}
                clientName={clientName}
                lookbackDays={lookbackDays}
                onViewMitre={() => setView('mitre')}
              />
            </motion.div>
          )}

          {view === 'builder' && data && (
            <motion.div key="builder" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <DetectionBuilder clientName={clientName} preselectedIds={builderPreselectedIds.length > 0 ? builderPreselectedIds : undefined} />
            </motion.div>
          )}
        </AnimatePresence>
      </main>
    </div>
  );
}

export default App;
