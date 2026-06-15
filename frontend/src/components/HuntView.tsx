import React, { useEffect, useRef, useState } from 'react';
import { ArrowLeft, ExternalLink, Download, ChevronDown, ChevronRight, MessageSquare } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { FlowAlert, HuntQueryResult, HuntReport } from '../types';
import { openHuntStream, exportHuntReport } from '../services/api';
import './HuntView.css';

interface Props {
  detection: FlowAlert;
  clientName: string;
  cxRegion?: string;
  onBack: () => void;
  origin?: 'builder' | 'mitre';
}

type Step = 'query' | 'olly' | 'report';

const OLLY_LABELS: Record<string, string> = {
  '1': 'What We Found',
  '2': 'Hunt Summary',
  '3': 'Original Query',
  '4': 'Schema Mapping',
  '5': 'Translated Query — DataPrime',
  '6': 'Translated Query — Lucene',
  '7': 'Detection Logic',
  '8': 'False Positive Sources',
  '9': 'Visibility Gaps',
  '10': 'Follow-up Hunts',
  '11': 'Alert Definition',
};

// Renders inline markdown: **bold**, `code`, and [text](url) links.
function renderMd(text: string): React.ReactNode {
  const parts: React.ReactNode[] = [];
  // Combined regex: markdown link [text](url), **bold**, or `code`
  const re = /\[([^\]]+)\]\((https?:\/\/[^)]+)\)|\*\*([^*]+)\*\*|`([^`]+)`/g;
  let last = 0;
  let match: RegExpExecArray | null;
  let key = 0;
  while ((match = re.exec(text)) !== null) {
    if (match.index > last) parts.push(text.slice(last, match.index));
    if (match[1] && match[2]) {
      parts.push(<a key={key++} href={match[2]} target="_blank" rel="noopener noreferrer" className="finding-link">{match[1]}</a>);
    } else if (match[3]) {
      parts.push(<strong key={key++}>{match[3]}</strong>);
    } else if (match[4]) {
      parts.push(<code key={key++} className="finding-code">{match[4]}</code>);
    }
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return parts.length ? <>{parts}</> : text;
}

function buildOllyChatUrl(region: string | undefined, chatId: string | undefined): string {
  if (!region || !chatId) return '';
  return `https://${region}.coralogix.com/#/olly/chat/${encodeURIComponent(chatId)}`;
}

export default function HuntView({ detection, clientName, cxRegion, onBack, origin = 'builder' }: Props) {
  const [activeStep, setActiveStep] = useState<Step>('query');
  const [doneSteps, setDoneSteps] = useState<Set<Step>>(new Set());
  const [queryResult, setQueryResult] = useState<HuntQueryResult | null>(null);
  const [ollySections, setOllySections] = useState<Record<string, string> | null>(null);
  const [report, setReport] = useState<HuntReport | null>(null);
  const [huntId, setHuntId] = useState<string>('');
  const [error, setError] = useState<string>('');
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set(['1']));
  const [ollyElapsed, setOllyElapsed] = useState(0);
  const esRef = useRef<EventSource | null>(null);
  const ollyTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    const payload = {
      detectionId: `${detection.techniqueId}-${Date.now()}`,
      name: detection.name,
      logic: detection.logic,
      techniqueId: detection.techniqueId,
      tacticId: '',
      window: '24h',
      source: detection.source,
      severity: detection.severity,
      client: clientName,
    };

    const es = openHuntStream(payload);
    esRef.current = es;

    es.addEventListener('stream_opened', (e) => {
      const data = JSON.parse((e as MessageEvent).data);
      setHuntId(data.hunt_id);
    });

    es.addEventListener('query_done', (e) => {
      const data: HuntQueryResult = JSON.parse((e as MessageEvent).data);
      setQueryResult(data);
      setDoneSteps(prev => new Set([...prev, 'query' as Step]));
      setActiveStep('olly');
      // Start elapsed timer for Olly phase; request notification permission
      setOllyElapsed(0);
      ollyTimerRef.current = setInterval(() => setOllyElapsed(s => s + 1), 1000);
      if (Notification.permission === 'default') Notification.requestPermission();
    });

    es.addEventListener('olly_done', (e) => {
      const data = JSON.parse((e as MessageEvent).data);
      setOllySections(data.sections);
      setDoneSteps(prev => new Set([...prev, 'olly' as Step]));
      setActiveStep('report');
      if (ollyTimerRef.current) { clearInterval(ollyTimerRef.current); ollyTimerRef.current = null; }
    });

    es.addEventListener('report_ready', (e) => {
      const data: HuntReport = JSON.parse((e as MessageEvent).data);
      setReport(data);
      setDoneSteps(prev => new Set([...prev, 'report' as Step]));
      es.close();
      // Browser notification if tab is in background
      if (document.hidden && Notification.permission === 'granted') {
        new Notification('Hunt complete', { body: `${detection.name} — ${data.verdict}`, icon: '/favicon.ico' });
      }
    });

    es.addEventListener('error', (e) => {
      try {
        const data = JSON.parse((e as MessageEvent).data);
        setError(data.message || 'Hunt failed');
      } catch {
        setError('Connection lost');
      }
      es.close();
    });

    return () => {
      es.close();
      if (ollyTimerRef.current) clearInterval(ollyTimerRef.current);
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const toggleSection = (key: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    });
  };

  const stepStatus = (step: Step): 'done' | 'active' | 'pending' => {
    if (doneSteps.has(step)) return 'done';
    if (!error && activeStep === step) return 'active';
    return 'pending';
  };

  const openInCoralogix = () => {
    if (!ollySections || !cxRegion) return;
    const lucene = ollySections['5'] || detection.logic;
    window.open(
      `https://${cxRegion}.coralogix.com/#/query-new/logs?query=${encodeURIComponent(lucene)}`,
      '_blank',
      'noopener'
    );
  };

  const verdictColor = (v: string) => {
    if (v === 'threat') return '#EF4444';
    if (v === 'suspicious') return '#F59E0B';
    return '#22C55E';
  };

  const severityBadgeClass = (sev: string) => {
    const s = sev.toLowerCase();
    if (s === 'critical') return 'hunt-badge-sev-critical';
    if (s === 'high') return 'hunt-badge-sev-high';
    if (s === 'medium') return 'hunt-badge-sev-medium';
    if (s === 'low') return 'hunt-badge-sev-low';
    return 'hunt-badge-default';
  };

  const continuationHref: string =
    ollySections?.['chat_url'] ||
    buildOllyChatUrl(cxRegion, ollySections?.['chat_id']) ||
    '';

  const STEP_LABELS: Record<Step, string> = { query: 'Log Query', olly: 'Olly Analysis', report: 'Hunt Report' };
  const STEPS: Step[] = ['query', 'olly', 'report'];

  return (
    <div className="hunt-page">
      <button className="hunt-back" onClick={onBack}>
        <ArrowLeft size={14} /> Back to {origin === 'mitre' ? 'MITRE Coverage' : 'Detection Builder'}
      </button>

      <div className="hunt-title">Hunt: {detection.name}</div>
      <div className="hunt-meta">
        <span className={`hunt-badge ${severityBadgeClass(detection.severity)}`}>{detection.severity}</span>
        <span className="hunt-badge hunt-badge-default">{detection.techniqueId}</span>
        <span className="hunt-badge hunt-badge-default">{detection.source}</span>
        <span className="hunt-badge hunt-badge-default">24h</span>
      </div>

      <div className="hunt-stepper">
        {STEPS.map((step, i) => {
          const status = stepStatus(step);
          return (
            <React.Fragment key={step}>
              <div className="hunt-step">
                <div className={`hunt-step-icon hunt-step-${status}`}>
                  {status === 'done' ? '✓' : i + 1}
                </div>
                <span className={`hunt-step-label${status === 'active' ? ' active' : ''}`}>{STEP_LABELS[step]}</span>
              </div>
              {i < 2 && <div className="hunt-step-connector" />}
            </React.Fragment>
          );
        })}
      </div>

      {error && (
        <div style={{ background: 'rgba(239,68,68,.1)', border: '1px solid rgba(239,68,68,.3)', borderRadius: 8, padding: '12px 16px', color: '#EF4444', fontSize: 13, marginBottom: 16 }}>
          Hunt failed: {error}
        </div>
      )}

      {/* Section 1: Log Query */}
      <div className="hunt-section">
        <div className="hunt-section-header">
          <div className="hunt-section-num">1</div>
          <div className="hunt-section-title">Log Query</div>
        </div>
        <div className="hunt-section-body">
          {queryResult ? (
            <>
              <div className="hunt-cmd-block">$ {queryResult.cx_command}</div>
              <div className="hunt-stat-row">
                {[
                  { val: queryResult.hits, lbl: 'total hits', color: queryResult.hits > 0 ? '#EF4444' : '#22C55E' },
                  { val: queryResult.hosts, lbl: 'hosts affected', color: '#E6EDF3' },
                  { val: queryResult.last_seen, lbl: 'last seen', color: '#E6EDF3' },
                  { val: queryResult.unique_users, lbl: 'unique users', color: '#E6EDF3' },
                ].map(({ val, lbl, color }) => (
                  <div key={lbl} className="hunt-stat">
                    <div className="hunt-stat-val" style={{ color }}>{val}</div>
                    <div className="hunt-stat-lbl">{lbl}</div>
                  </div>
                ))}
              </div>
              {queryResult.sample_events?.length > 0 && (
                <table className="hunt-log-table">
                  <thead><tr><th>Time</th><th>Host</th><th>User</th><th>Command</th></tr></thead>
                  <tbody>
                    {queryResult.sample_events?.slice(0, 5).map((ev, i) => (
                      <tr key={i}>
                        <td>{ev.timestamp}</td>
                        <td>{ev.host}</td>
                        <td>{ev.user}</td>
                        <td style={{ maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ev.command}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          ) : (
            <div>
              <div className="shimmer-stat-row">
                {[0,1,2,3].map(i => <div key={i} className="shimmer shimmer-stat" />)}
              </div>
              {[0,1,2].map(i => <div key={i} className="shimmer shimmer-row" />)}
            </div>
          )}
        </div>
      </div>

      {/* Section 2: Olly Analysis */}
      <div className="hunt-section">
        <div className="hunt-section-header">
          <div className="hunt-section-num">2</div>
          <div className="hunt-section-title">Olly Analysis</div>
        </div>
        <div className="hunt-section-body">
          {ollySections ? (
            Object.entries(OLLY_LABELS).map(([key, label]) => {
              const isExpanded = expandedSections.has(key);
              const content = ollySections[key] || '';
              return (
                <div key={key} className="olly-accordion">
                  <div className={`olly-acc-header${isExpanded ? ' expanded' : ''}`} onClick={() => toggleSection(key)}>
                    <span><span className="olly-acc-num">§{key}</span>{label}</span>
                    {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  </div>
                  {isExpanded && (
                    <div className="olly-acc-body olly-md">
                      {key === '9' && content && (
                        <div className="olly-gap-alert">Visibility gaps detected — review before deploying this detection.</div>
                      )}
                      {content
                        ? <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
                        : <span style={{ color: '#6E7B8B', fontStyle: 'italic' }}>No content returned.</span>
                      }
                    </div>
                  )}
                </div>
              );
            })
          ) : (
            <div>
              {activeStep === 'olly' && (
                <div className="olly-waiting">
                  <div className="olly-waiting-title">
                    Olly is analyzing your environment
                    <span className="olly-elapsed">{ollyElapsed}s</span>
                  </div>
                  <div className="olly-waiting-sub">
                    Running live queries against your Coralogix data — typically takes 3–4 minutes.
                    {ollyElapsed >= 60 && ' You can switch tabs; we\'ll notify you when done.'}
                  </div>
                </div>
              )}
              {[0,1,2,3].map(i => <div key={i} className="shimmer shimmer-accordion" />)}
            </div>
          )}
        </div>
      </div>

      {/* Hunt Report — animates in when ready */}
      {report && (() => {
        const v = report.verdict;
        const color = verdictColor(v);
        const bannerClass = `report-banner-${v === 'threat' ? 'threat' : v === 'suspicious' ? 'suspicious' : 'clean'}`;
        const iconClass = `report-icon-${v === 'threat' ? 'threat' : v === 'suspicious' ? 'suspicious' : 'clean'}`;
        const labelClass = `label-${v === 'threat' ? 'threat' : v === 'suspicious' ? 'suspicious' : 'clean'}`;
        const verdictLabel = v === 'threat' ? 'THREAT DETECTED' : v === 'suspicious' ? 'SUSPICIOUS ACTIVITY' : 'NO THREATS FOUND';

        return (
          <div className="hunt-report">
            <div className={`report-banner ${bannerClass}`}>
              <div className={`report-icon ${iconClass}`}>
                {v === 'threat' && (
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
                  </svg>
                )}
                {v === 'suspicious' && (
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                    <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
                  </svg>
                )}
                {v === 'clean' && (
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><polyline points="9 12 11 14 15 10"/>
                  </svg>
                )}
              </div>
              <div className="report-banner-body">
                <div className={`report-verdict-label ${labelClass}`}>
                  HUNT REPORT · {verdictLabel} · {report.confidence.toUpperCase()} CONFIDENCE
                </div>
                <div className="report-title">{report.title}</div>
                <div className="report-subtitle">{report.subtitle}</div>
                <div className="report-stats">
                  <div className="rstat"><div className="rstat-val" style={{ color }}>{report.stats.hits}</div><div className="rstat-lbl">hits</div></div>
                  <div className="rstat"><div className="rstat-val" style={{ color: report.stats.hosts !== '0' ? color : '#6E7B8B' }}>{report.stats.hosts}</div><div className="rstat-lbl">hosts</div></div>
                  <div className="rstat"><div className="rstat-val" style={{ color: '#6E7B8B' }}>{report.stats.attack_window}</div><div className="rstat-lbl">window</div></div>
                  {report.stats.c2_flagged && (
                    <div className="rstat"><div className="rstat-val" style={{ color: '#EF4444' }}>C2</div><div className="rstat-lbl">IP flagged</div></div>
                  )}
                </div>
              </div>
            </div>

            <div className="report-body">
              {report.findings?.length > 0 && (
                <>
                  <div className="report-section-title">Key Findings</div>
                  <ul className="report-findings">
                    {report.findings.map((f, i) => (
                      <li key={i}><div className={`finding-dot dot-${f.severity}`} /><span>{renderMd(f.text)}</span></li>
                    ))}
                  </ul>
                </>
              )}
              {report.actions?.length > 0 && (
                <>
                  <div className="report-section-title">Immediate Actions</div>
                  <div className="report-actions">
                    {report.actions.map((a, i) => (
                      <div key={i} className={`action-item action-${a.level}`}>
                        <span className={`action-num action-num-${a.level}`}>{a.priority}.</span>
                        <span className="action-text">{a.description}</span>
                      </div>
                    ))}
                  </div>
                </>
              )}
              {report.alert_def && Object.keys(report.alert_def).length > 0 && (
                <>
                  <div className="report-section-title">Alert Definition Skeleton</div>
                  <div className="report-alert-def">
                    <div className="alert-def-grid">
                      {(Object.entries(report.alert_def) as [string, string][]).map(([k, val]) => (
                        <React.Fragment key={k}>
                          <span className="alert-def-key">{k.replace('_', ' ')}</span>
                          <span className="alert-def-val">{val}</span>
                        </React.Fragment>
                      ))}
                    </div>
                  </div>
                </>
              )}
            </div>

            <div className="report-footer">
              <span className="report-footer-meta">
                {new Date(report.timestamp).toLocaleString()} · {report.run_duration_ms}ms
              </span>
              <div className="report-actions-row">
                {continuationHref && (
                  <a
                    className="hunt-btn hunt-btn-primary"
                    href={continuationHref}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <MessageSquare size={13} /> Continue in CX
                  </a>
                )}
                {ollySections?.['5'] && cxRegion && (
                  <button className="hunt-btn hunt-btn-secondary" onClick={openInCoralogix}>
                    <ExternalLink size={13} /> Open in Coralogix
                  </button>
                )}
                {huntId && (
                  <button className="hunt-btn hunt-btn-secondary" onClick={() => exportHuntReport(huntId)}>
                    <Download size={13} /> Export Report
                  </button>
                )}
              </div>
            </div>
          </div>
        );
      })()}

      {/* Report skeleton while waiting */}
      {activeStep === 'report' && !report && (
        <div className="hunt-section">
          <div className="hunt-section-header">
            <div className="hunt-section-num" style={{ background: 'rgba(59,130,246,.2)', color: '#3B82F6' }}>3</div>
            <div className="hunt-section-title">Hunt Report</div>
          </div>
          <div className="hunt-section-body">
            <div className="shimmer shimmer-box" style={{ marginBottom: 12 }} />
            {['w-full','w-3q','w-half'].map(w => <div key={w} className={`shimmer shimmer-line ${w}`} />)}
          </div>
        </div>
      )}
    </div>
  );
}
