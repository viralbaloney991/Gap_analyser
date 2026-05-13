import { useState, useEffect, useCallback } from 'react';
import { ArrowLeft } from 'lucide-react';
import { AnimatePresence, motion } from 'framer-motion';
import type { Transition } from 'framer-motion';
import ClientSelector from './components/ClientSelector';
import IntegrationSummary from './components/IntegrationSummary';
import MITREHeatmap from './components/MITREHeatmap';
import AlertInsights from './components/AlertInsights';
import ThreatGraph from './components/ThreatGraph';
import DetectionBuilder from './components/DetectionBuilder';
import HuntView from './components/HuntView';
import { analyzeClient, fetchInsights, fetchNoise } from './services/api';
import type { AnalyzeResponse, InsightsReport, SimilarityResult, FlowAlert, HuntPayload } from './types';
import './App.css';

const VALID_LOOKBACK = [7, 14, 30, 90];

type View = 'form' | 'summary' | 'mitre' | 'insights' | 'graph' | 'builder' | 'hunt';

const FADE_UP_TRANSITION: Transition = { duration: 0.2, ease: 'easeOut' };

const FADE_UP = {
  initial:    { opacity: 0, y: 8 },
  animate:    { opacity: 1, y: 0 },
  exit:       { opacity: 0, y: -8 },
  transition: FADE_UP_TRANSITION,
};

function App() {
  const [view, setView]                   = useState<View>(() => {
    // Restore view from history state on hard reload (e.g. cmd+shift+r)
    return (history.state?.view as View) ?? 'form';
  });
  const [loading, setLoading]             = useState(false);
  const [error, setError]                 = useState('');
  const [data, setData]                   = useState<AnalyzeResponse | null>(null);
  const [clientName, setClientName]       = useState('');
  const [insightsReport, setInsightsReport] = useState<InsightsReport | null>(null);
  const [insightsError, setInsightsError] = useState(false);
  const [noiseLoading, setNoiseLoading] = useState(false);
  const [builderPreselectedIds, setBuilderPreselectedIds] = useState<string[]>([]);
  const [huntDetection, setHuntDetection] = useState<FlowAlert | null>(null);
  const [huntOrigin, setHuntOrigin] = useState<'builder' | 'mitre'>('builder');
  const [lookbackDays, setLookbackDays] = useState<number>(() => {
    const stored = localStorage.getItem('noise_lookback_days');
    const parsed = stored ? Number(stored) : 30;
    return VALID_LOOKBACK.includes(parsed) ? parsed : 30;
  });

  // ── History API integration ──────────────────────────────────────────────
  // Stamp 'form' on the very first load so there is always a state to pop back to.
  useEffect(() => {
    if (!history.state?.view) history.replaceState({ view: 'form' }, '');
  }, []);

  // Drive view from the browser back/forward buttons.
  // Data is NOT cleared here — it lives in React state so forward navigation
  // can restore summary/detail views without re-fetching.
  useEffect(() => {
    const handlePop = (e: PopStateEvent) => {
      const target = (e.state?.view ?? 'form') as View;
      setView(target);
      if (target !== 'hunt') setHuntDetection(null);
      setError('');
    };
    window.addEventListener('popstate', handlePop);
    return () => window.removeEventListener('popstate', handlePop);
  }, []);

  // Use navigate() instead of setView() for every intentional forward navigation.
  const navigate = useCallback((newView: View) => {
    history.pushState({ view: newView }, '');
    setView(newView);
  }, []);

  // Clear basket preselection when leaving the builder (back button or direct nav).
  useEffect(() => {
    if (view !== 'builder' && builderPreselectedIds.length > 0) {
      setBuilderPreselectedIds([]);
    }
  }, [view]); // eslint-disable-line react-hooks/exhaustive-deps

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
      navigate('summary');
      fetchInsights(client)
        .then(setInsightsReport)
        .catch((e) => { console.warn('[insights]', e); setInsightsError(true); });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Analysis failed');
    } finally {
      setLoading(false);
    }
  };

  // Browser back button already handles this via popstate — keep it DRY.
  const goBack = () => history.back();

  const goHome = () => {
    history.pushState({ view: 'form' }, '');
    setView('form');
    setData(null);
    setClientName('');
    setInsightsReport(null);
    setInsightsError(false);
    setBuilderPreselectedIds([]);
    setHuntDetection(null);
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
                 : view === 'hunt' ? 'Hunt'
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
              <ArrowLeft size={13} style={{ verticalAlign: 'middle', marginRight: 3 }} /> Back
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
                onViewMITRE={() => navigate('mitre')}
                onViewInsights={() => navigate('insights')}
                onViewGraph={() => navigate('graph')}
                onViewBuilder={() => navigate('builder')}
                onRefresh={() => handleAnalyze(clientName, true)}
              />
            </motion.div>
          )}

          {view === 'mitre' && data && (
            <motion.div key="mitre" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <MITREHeatmap
                data={data.mitre_coverage}
                clientName={clientName}
                onHunt={(payload: HuntPayload) => {
                  const validSeverities = ['critical', 'high', 'medium', 'low'] as const;
                  const sev = payload.severity.toLowerCase() as FlowAlert['severity'];
                  setHuntDetection({
                    name: payload.name,
                    description: '',
                    techniqueId: payload.techniqueId,
                    logic: payload.logic,
                    window: payload.window,
                    windowReason: '',
                    source: payload.source,
                    severity: validSeverities.includes(sev) ? sev : 'medium',
                  });
                  setHuntOrigin('mitre');
                  navigate('hunt');
                }}
              />
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
                onViewMitre={() => navigate('mitre')}
              />
            </motion.div>
          )}

          {view === 'builder' && data && (
            <motion.div key="builder" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <DetectionBuilder
                clientName={clientName}
                preselectedIds={builderPreselectedIds.length > 0 ? builderPreselectedIds : undefined}
                onHunt={(alert) => { setHuntDetection(alert); setHuntOrigin('builder'); navigate('hunt'); }}
              />
            </motion.div>
          )}
          {view === 'hunt' && huntDetection && (
            <motion.div key="hunt" {...FADE_UP} style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, overflowY: 'auto' }}>
              <HuntView
                detection={huntDetection}
                cxRegion={import.meta.env.VITE_CX_REGION}
                origin={huntOrigin}
                onBack={() => navigate(huntOrigin)}
              />
            </motion.div>
          )}
        </AnimatePresence>
      </main>
    </div>
  );
}

export default App;
