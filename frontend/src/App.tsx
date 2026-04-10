import { useState } from 'react';
import ClientSelector from './components/ClientSelector';
import IntegrationSummary from './components/IntegrationSummary';
import MITREHeatmap from './components/MITREHeatmap';
import AlertInsights from './components/AlertInsights';
import { analyzeClient } from './services/api';
import type { AnalyzeResponse } from './types';
import './App.css';

type View = 'form' | 'summary' | 'mitre' | 'insights';

function App() {
  const [view, setView] = useState<View>('form');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [data, setData] = useState<AnalyzeResponse | null>(null);
  const [clientName, setClientName] = useState('');

  const handleAnalyze = async (client: string, refresh = false) => {
    setLoading(true);
    setError('');
    try {
      const result = await analyzeClient(client, refresh);
      setData(result);
      setClientName(client);
      setView('summary');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Analysis failed');
    } finally {
      setLoading(false);
    }
  };

  const goBack = () => {
    if (view === 'mitre' || view === 'insights') {
      setView('summary');
    } else {
      setView('form');
      setData(null);
      setClientName('');
    }
    setError('');
  };

  const goHome = () => {
    setView('form');
    setData(null);
    setClientName('');
    setError('');
  };

  return (
    <div className="app">
      <header className="app-header">
        {view !== 'form' ? (
          <button className="btn btn-small" onClick={goBack}>
            ← Back
          </button>
        ) : (
          <div />
        )}
        <h1 onClick={goHome}>
          <sup>CX</sup>Alert <strong>Analyzer</strong>
        </h1>
        <div className="header-status">
          <span className="status-dot" />
          ONLINE
        </div>
      </header>

      <main className={`app-main${view === 'form' ? ' app-main--landing' : ''}`}>
        {error && <div className="error-banner">{error}</div>}

        {view === 'form' && (
          <ClientSelector onAnalyze={handleAnalyze} loading={loading} />
        )}

        {view === 'summary' && data && (
          <IntegrationSummary
            data={data}
            clientName={clientName}
            loading={loading}
            onViewMITRE={() => setView('mitre')}
            onViewInsights={() => setView('insights')}
            onRefresh={() => handleAnalyze(clientName, true)}
          />
        )}

        {view === 'mitre' && data && (
          <MITREHeatmap data={data.mitre_coverage} clientName={clientName} />
        )}

        {view === 'insights' && data && (
          <AlertInsights data={data.alert_insights} report={data.insights_report ?? null} />
        )}
      </main>
    </div>
  );
}

export default App;
