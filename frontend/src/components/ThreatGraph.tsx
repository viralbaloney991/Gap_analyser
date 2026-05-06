import { useMemo, useRef, useState, useEffect, useCallback } from 'react';
import type { SimilarityResult } from '../types';

interface Props {
  data: SimilarityResult;
  clientName: string;
}

// ── Internal graph types ────────────────────────────────────────────────

type NodeType = 'family' | 'noise' | 'unique';

interface GNode {
  id: string;
  type: NodeType;
  label: string;
  fullLabel: string;
  alertCount: number;
  alertNames: string[];
  noiseType?: 'behavioral' | 'structural' | 'both';
  triggerCount?: number;
  reason?: string;
  missingFeatures?: string[];
  x: number;
  y: number;
  r: number;
}

interface GEdge {
  id: string;
  source: string;
  target: string;
  similarity: number;
  explanation: string;
  edgeType: 'duplicate' | 'merge';
}

interface GraphResult {
  nodes: GNode[];
  edges: GEdge[];
  idToNode: Map<string, GNode>;
  nN: number;
  nF: number;
  nU: number;
  nZoneX: number;
  nZoneW: number;
  fZoneX: number;
  fZoneW: number;
  uZoneX: number;
  uZoneW: number;
}

// ── Utilities ───────────────────────────────────────────────────────────

const NOISE_CAP = 35;
const UNIQUE_CAP = 24;
const PAD = 68;

function deterministicJitter(seed: string, range: number): number {
  let h = 5381;
  for (let i = 0; i < seed.length; i++) h = ((h << 5) + h) ^ seed.charCodeAt(i);
  return ((Math.abs(h) % 1000) / 1000 - 0.5) * 2 * range;
}

function nodeColor(n: GNode): string {
  if (n.type === 'noise') {
    switch (n.noiseType) {
      case 'behavioral': return '#ef4444';
      case 'structural': return '#f59e0b';
      case 'both':       return '#f97316';
      default:           return '#f59e0b';
    }
  }
  if (n.type === 'family') return '#10b981';
  return '#818cf8';
}

// ── Graph builder ───────────────────────────────────────────────────────

function buildGraph(data: SimilarityResult, W: number, H: number): GraphResult {
  const nodes: GNode[] = [];
  const edges: GEdge[] = [];
  const nameToId = new Map<string, string>();
  const idToNode = new Map<string, GNode>();

  const noiseAlerts = (data.noise_alerts ?? []).slice(0, NOISE_CAP);
  const families    = data.families ?? [];
  const uniques     = (data.unique_detections ?? []).slice(0, UNIQUE_CAP);

  const nN = noiseAlerts.length;
  const nF = families.length;
  const nU = uniques.length;

  const innerH = H - PAD * 2;

  // Zone layout
  const hasN = nN > 0;
  const hasU = nU > 0;
  let nZoneX = PAD, nZoneW = 0;
  let fZoneX = PAD, fZoneW = W - PAD * 2;
  let uZoneX = 0,   uZoneW = 0;

  if (hasN && hasU) {
    nZoneW = Math.min(210, (W - PAD * 2) * 0.21);
    uZoneW = Math.min(195, (W - PAD * 2) * 0.19);
    fZoneW = W - PAD * 2 - nZoneW - uZoneW - 40;
    nZoneX = PAD;
    fZoneX = PAD + nZoneW + 20;
    uZoneX = fZoneX + fZoneW + 20;
  } else if (hasN) {
    nZoneW = Math.min(230, (W - PAD * 2) * 0.25);
    fZoneW = W - PAD * 2 - nZoneW - 20;
    nZoneX = PAD;
    fZoneX = PAD + nZoneW + 20;
  } else if (hasU) {
    uZoneW = Math.min(210, (W - PAD * 2) * 0.21);
    fZoneW = W - PAD * 2 - uZoneW - 20;
    fZoneX = PAD;
    uZoneX = PAD + fZoneW + 20;
  }

  // ── Noise nodes ──
  noiseAlerts.forEach((na, i) => {
    const id = `n-${i}`;
    const jx = deterministicJitter(na.name + 'x', nZoneW * 0.38);
    const jy = deterministicJitter(na.name + 'y', 16);
    const x  = nZoneX + nZoneW * 0.5 + jx;
    const y  = PAD + (innerH / Math.max(nN, 1)) * (i + 0.5) + jy;
    const r  = na.trigger_count && na.trigger_count > 200 ? 24
             : na.trigger_count && na.trigger_count > 50  ? 21
             : na.trigger_count && na.trigger_count > 10  ? 18
             : 15;
    const node: GNode = {
      id, type: 'noise',
      label:    na.name.length > 26 ? na.name.slice(0, 25) + '…' : na.name,
      fullLabel: na.name,
      alertCount: na.trigger_count ?? 0,
      alertNames: [na.name],
      noiseType: na.noise_type,
      triggerCount: na.trigger_count,
      reason: na.reason,
      missingFeatures: na.missing_features,
      x, y, r,
    };
    nodes.push(node);
    idToNode.set(id, node);
    nameToId.set(na.name, id);
  });

  // ── Family nodes ──
  const fCols  = Math.max(2, Math.ceil(Math.sqrt(nF * 1.5)));
  const fRows  = Math.ceil(nF / fCols);
  const cellW  = fZoneW / fCols;
  const cellH  = innerH / Math.max(fRows, 1);

  families.forEach((fam, i) => {
    const id  = `f-${i}`;
    const col = i % fCols;
    const row = Math.floor(i / fCols);
    const jx  = deterministicJitter(fam.name + 'x', cellW * 0.26);
    const jy  = deterministicJitter(fam.name + 'y', cellH * 0.26);
    const x   = fZoneX + cellW * (col + 0.5) + jx;
    const y   = PAD + cellH * (row + 0.5) + jy;
    const r   = Math.max(16, Math.min(34, 14 + Math.log2(fam.alert_ids.length + 1) * 5));

    const node: GNode = {
      id, type: 'family',
      label:    fam.name.length > 28 ? fam.name.slice(0, 27) + '…' : fam.name,
      fullLabel: fam.name,
      alertCount: fam.alert_ids.length,
      alertNames: fam.alert_names,
      x, y, r,
    };
    nodes.push(node);
    idToNode.set(id, node);
    nameToId.set(fam.name, id);
    fam.alert_names.forEach(n => nameToId.set(n, id));
  });

  // ── Unique nodes ──
  const uCols  = Math.max(1, Math.ceil(Math.sqrt(nU)));
  const uRows  = Math.ceil(nU / Math.max(uCols, 1));
  const uCellW = uZoneW / Math.max(uCols, 1);
  const uCellH = innerH / Math.max(uRows, 1);

  uniques.forEach((name, i) => {
    const id  = `u-${i}`;
    const col = i % Math.max(uCols, 1);
    const row = Math.floor(i / Math.max(uCols, 1));
    const jx  = deterministicJitter(name + 'x', uCellW * 0.28);
    const jy  = deterministicJitter(name + 'y', uCellH * 0.28);
    const x   = uZoneX + uCellW * (col + 0.5) + jx;
    const y   = PAD + uCellH * (row + 0.5) + jy;
    const node: GNode = {
      id, type: 'unique',
      label:    name.length > 22 ? name.slice(0, 21) + '…' : name,
      fullLabel: name,
      alertCount: 1,
      alertNames: [name],
      x, y, r: 8,
    };
    nodes.push(node);
    idToNode.set(id, node);
    nameToId.set(name, id);
  });

  // ── Edges from duplicates ──
  const seenEdges = new Set<string>();

  data.duplicates.forEach((dup, i) => {
    for (let a = 0; a < dup.alert_names.length; a++) {
      for (let b = a + 1; b < dup.alert_names.length; b++) {
        const sId = nameToId.get(dup.alert_names[a]);
        const tId = nameToId.get(dup.alert_names[b]);
        if (!sId || !tId || sId === tId) continue;
        const key = [sId, tId].sort().join('|');
        if (seenEdges.has(key)) continue;
        seenEdges.add(key);
        edges.push({
          id: `d-${i}-${a}-${b}`,
          source: sId, target: tId,
          similarity: dup.similarity,
          explanation: dup.explanation,
          edgeType: 'duplicate',
        });
      }
    }
  });

  // ── Edges from merge suggestions ──
  data.merge_suggestions.forEach((ms, i) => {
    for (let a = 0; a < ms.alert_names.length; a++) {
      for (let b = a + 1; b < ms.alert_names.length; b++) {
        const sId = nameToId.get(ms.alert_names[a]);
        const tId = nameToId.get(ms.alert_names[b]);
        if (!sId || !tId || sId === tId) continue;
        const key = [sId, tId].sort().join('|') + ':merge';
        if (seenEdges.has(key)) continue;
        seenEdges.add(key);
        edges.push({
          id: `m-${i}-${a}-${b}`,
          source: sId, target: tId,
          similarity: 0.85,
          explanation: ms.reason,
          edgeType: 'merge',
        });
      }
    }
  });

  return { nodes, edges, idToNode, nN, nF, nU, nZoneX, nZoneW, fZoneX, fZoneW, uZoneX, uZoneW };
}

// ── Component ───────────────────────────────────────────────────────────

export default function ThreatGraph({ data, clientName }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ w: 960, h: 580 });
  const [selected, setSelected] = useState<string | null>(null);
  const [filter, setFilter] = useState<'all' | 'noise' | 'families'>('all');

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const obs = new ResizeObserver(entries => {
      const { width, height } = entries[0].contentRect;
      if (width > 10 && height > 10) setDims({ w: width, h: height });
    });
    obs.observe(el);
    return () => obs.disconnect();
  }, []);

  const graph = useMemo(
    () => buildGraph(data, dims.w, dims.h),
    [data, dims.w, dims.h]
  );

  const { nodes, edges, idToNode, nN, nF, nU, nZoneX, nZoneW, fZoneX, fZoneW, uZoneX, uZoneW } = graph;

  const visibleNodes = useMemo(() => {
    if (filter === 'noise')    return nodes.filter(n => n.type === 'noise');
    if (filter === 'families') return nodes.filter(n => n.type === 'family');
    return nodes;
  }, [nodes, filter]);

  const visibleEdges = useMemo(() => {
    if (filter !== 'all') return [];
    return edges;
  }, [edges, filter]);

  const visibleNodeIds = useMemo(() => new Set(visibleNodes.map(n => n.id)), [visibleNodes]);
  const selectedNode = selected ? idToNode.get(selected) : null;

  const handleNodeClick = useCallback((id: string, e: React.MouseEvent | React.KeyboardEvent) => {
    e.stopPropagation();
    setSelected(prev => prev === id ? null : id);
  }, []);

  const edgePath = useCallback((edge: GEdge): string => {
    const src = idToNode.get(edge.source);
    const tgt = idToNode.get(edge.target);
    if (!src || !tgt) return '';
    const mx = (src.x + tgt.x) / 2;
    const my = (src.y + tgt.y) / 2 - Math.abs(tgt.x - src.x) * 0.18;
    return `M ${src.x} ${src.y} Q ${mx} ${my} ${tgt.x} ${tgt.y}`;
  }, [idToNode]);

  const noiseCount  = nodes.filter(n => n.type === 'noise').length;
  const familyCount = nodes.filter(n => n.type === 'family').length;
  const uniqueCount = nodes.filter(n => n.type === 'unique').length;

  return (
    <div className="threat-graph">

      {/* ── Control bar ── */}
      <div className="tg-controls">
        <div className="tg-controls-left">
          <span className="tg-client-label">{clientName}</span>
          <span className="tg-sep">›</span>
          <span className="tg-view-label">Threat Graph</span>
        </div>

        <div className="tg-filter-group" role="group" aria-label="Filter nodes">
          {(['all', 'noise', 'families'] as const).map(f => (
            <button
              key={f}
              type="button"
              className={`tg-filter-btn${filter === f ? ' tg-filter-btn--active' : ''}`}
              onClick={() => setFilter(f)}
            >
              {f === 'all'      ? 'All nodes'
               : f === 'noise'  ? `Noise (${noiseCount})`
               : `Families (${familyCount})`}
            </button>
          ))}
        </div>

        <div className="tg-stats" aria-label="Graph statistics">
          {familyCount > 0 && <span className="tg-stat tg-stat--family">{familyCount} families</span>}
          {noiseCount  > 0 && <span className="tg-stat tg-stat--noise">{noiseCount} noisy</span>}
          {uniqueCount > 0 && <span className="tg-stat tg-stat--unique">{uniqueCount} unique</span>}
          {edges.length > 0 && <span className="tg-stat tg-stat--edge">{edges.length} links</span>}
        </div>
      </div>

      {/* ── Canvas + detail panel ── */}
      <div className="tg-canvas-wrap">
        <div
          className="tg-canvas"
          ref={containerRef}
          onClick={() => setSelected(null)}
          role="presentation"
        >
          <svg width={dims.w} height={dims.h} className="tg-svg" aria-label="Threat correlation graph">
            <defs>
              <filter id="tg-glow" x="-55%" y="-55%" width="210%" height="210%">
                <feGaussianBlur stdDeviation="5" result="blur" />
                <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
              </filter>
              <filter id="tg-glow-sm" x="-65%" y="-65%" width="230%" height="230%">
                <feGaussianBlur stdDeviation="3" result="blur" />
                <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
              </filter>
              <linearGradient id="tg-dup-grad" gradientUnits="userSpaceOnUse">
                <stop offset="0%"   stopColor="#38bdf8" stopOpacity="0.7" />
                <stop offset="100%" stopColor="#818cf8" stopOpacity="0.7" />
              </linearGradient>
              <linearGradient id="tg-merge-grad" gradientUnits="userSpaceOnUse">
                <stop offset="0%"   stopColor="#f59e0b" stopOpacity="0.6" />
                <stop offset="100%" stopColor="#f97316" stopOpacity="0.6" />
              </linearGradient>
            </defs>

            {/* Zone separators */}
            {nN > 0 && (
              <line
                x1={nZoneX + nZoneW + 10} y1={PAD * 0.55}
                x2={nZoneX + nZoneW + 10} y2={dims.h - PAD * 0.55}
                stroke="rgba(16,185,129,0.11)" strokeWidth={1} strokeDasharray="4 9"
              />
            )}
            {nU > 0 && (
              <line
                x1={uZoneX - 10} y1={PAD * 0.55}
                x2={uZoneX - 10} y2={dims.h - PAD * 0.55}
                stroke="rgba(16,185,129,0.11)" strokeWidth={1} strokeDasharray="4 9"
              />
            )}

            {/* Zone labels */}
            {nN > 0 && (
              <text x={nZoneX + nZoneW * 0.5} y={PAD * 0.36}
                textAnchor="middle" fontSize={9.5}
                fill="rgba(239,68,68,0.42)" fontFamily="'IBM Plex Mono',monospace" letterSpacing="0.1em">
                NOISE ZONE
              </text>
            )}
            <text x={fZoneX + fZoneW * 0.5} y={PAD * 0.36}
              textAnchor="middle" fontSize={9.5}
              fill="rgba(16,185,129,0.38)" fontFamily="'IBM Plex Mono',monospace" letterSpacing="0.1em">
              DETECTION FAMILIES
            </text>
            {nU > 0 && (
              <text x={uZoneX + uZoneW * 0.5} y={PAD * 0.36}
                textAnchor="middle" fontSize={9.5}
                fill="rgba(129,140,248,0.42)" fontFamily="'IBM Plex Mono',monospace" letterSpacing="0.1em">
                UNIQUE
              </text>
            )}

            {/* ── Edges ── */}
            <g>
              {visibleEdges.map(edge => {
                if (!visibleNodeIds.has(edge.source) || !visibleNodeIds.has(edge.target)) return null;
                const d = edgePath(edge);
                if (!d) return null;
                const hilit  = selected === edge.source || selected === edge.target;
                const isDup  = edge.edgeType === 'duplicate';
                const stroke = isDup ? '#38bdf8' : '#f59e0b';
                return (
                  <g key={edge.id}>
                    <path
                      d={d} fill="none"
                      stroke={stroke}
                      strokeWidth={hilit ? edge.similarity * 2.5 + 0.8 : edge.similarity * 1.2 + 0.4}
                      strokeOpacity={hilit ? 0.8 : 0.22}
                      strokeDasharray={!isDup ? '7 5' : undefined}
                    />
                    {hilit && (
                      <path
                        d={d} fill="none"
                        stroke={stroke}
                        strokeWidth={1.5}
                        strokeOpacity={0.55}
                        strokeDasharray="10 16"
                        className="tg-edge-flow"
                      />
                    )}
                  </g>
                );
              })}
            </g>

            {/* ── Nodes ── */}
            <g>
              {visibleNodes.map(node => {
                const isSel    = node.id === selected;
                const isDimmed = selected !== null && !isSel;
                const color    = nodeColor(node);
                const isNoise  = node.type === 'noise';
                const isFamily = node.type === 'family';
                const isUnique = node.type === 'unique';

                return (
                  <g
                    key={node.id}
                    transform={`translate(${node.x},${node.y})`}
                    className={[
                      'tg-node',
                      `tg-node--${node.type}`,
                      isSel    ? 'tg-node--selected' : '',
                      isDimmed ? 'tg-node--dimmed'   : '',
                    ].filter(Boolean).join(' ')}
                    onClick={e => handleNodeClick(node.id, e)}
                    onKeyDown={e => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleNodeClick(node.id, e);
                      }
                    }}
                    style={{ cursor: 'pointer' }}
                    role="button"
                    tabIndex={0}
                    aria-label={node.fullLabel}
                    aria-pressed={isSel}
                  >
                    {/* Outer pulse ring for noise */}
                    {isNoise && (
                      <g className="tg-pulse-wrap">
                        <circle
                          r={node.r + 9}
                          fill="none"
                          stroke={color}
                          strokeWidth={1.5}
                          strokeOpacity={0.5}
                          className={`tg-pulse tg-pulse--${node.noiseType ?? 'structural'}`}
                        />
                      </g>
                    )}

                    {/* Spinning selection ring */}
                    {isSel && (
                      <circle
                        r={node.r + (isNoise ? 19 : 13)}
                        fill="none"
                        stroke={color}
                        strokeWidth={1.5}
                        strokeOpacity={0.65}
                        strokeDasharray="5 4"
                        className="tg-select-ring"
                      />
                    )}

                    {/* Alert density ring for busy families */}
                    {isFamily && node.alertCount > 4 && (
                      <circle
                        r={node.r + 5}
                        fill="none"
                        stroke={color}
                        strokeWidth={0.7}
                        strokeOpacity={isSel ? 0.5 : 0.2}
                      />
                    )}

                    {/* Main circle */}
                    <circle
                      r={node.r}
                      fill={isSel ? `${color}33` : `${color}18`}
                      stroke={color}
                      strokeWidth={isSel ? 2.5 : isNoise ? 1.8 : 1.5}
                      filter={isSel ? 'url(#tg-glow)' : isNoise ? 'url(#tg-glow-sm)' : undefined}
                    />

                    {/* Inner dot for unique */}
                    {isUnique && (
                      <circle r={3.5} fill={color} opacity={isSel ? 1 : 0.7} />
                    )}

                    {/* Alert count inside family nodes */}
                    {isFamily && (
                      <text
                        textAnchor="middle" dy="0.36em"
                        fontSize={node.alertCount > 99 ? 9 : 10}
                        fill={isSel ? '#fff' : color}
                        fontFamily="'IBM Plex Mono',monospace" fontWeight="600"
                        style={{ pointerEvents: 'none', userSelect: 'none' }}
                      >
                        {node.alertCount}
                      </text>
                    )}

                    {/* Trigger count inside noise nodes */}
                    {isNoise && (node.triggerCount ?? 0) > 0 && (
                      <text
                        textAnchor="middle" dy="0.36em"
                        fontSize={9}
                        fill={isSel ? '#fff' : color}
                        fontFamily="'IBM Plex Mono',monospace" fontWeight="600"
                        style={{ pointerEvents: 'none', userSelect: 'none' }}
                      >
                        {(node.triggerCount ?? 0) > 999 ? '999+' : node.triggerCount}
                      </text>
                    )}

                    {/* Label below node */}
                    {(isFamily || isSel) && (
                      <text
                        textAnchor="middle" y={node.r + 13}
                        fontSize={isSel ? 9.5 : 8}
                        fill={isSel ? '#f1f5f9' : 'rgba(148,163,184,0.72)'}
                        fontFamily="'IBM Plex Mono',monospace"
                        fontWeight={isSel ? '500' : '400'}
                        style={{ pointerEvents: 'none', userSelect: 'none' }}
                      >
                        {node.label}
                      </text>
                    )}
                  </g>
                );
              })}
            </g>

            {/* Unique overflow badge */}
            {(data.unique_detections ?? []).length > UNIQUE_CAP && nU > 0 && (
              <text
                x={uZoneX + uZoneW * 0.5} y={dims.h - PAD * 0.42}
                textAnchor="middle" fontSize={9}
                fill="rgba(129,140,248,0.4)"
                fontFamily="'IBM Plex Mono',monospace"
              >
                +{(data.unique_detections ?? []).length - UNIQUE_CAP} more
              </text>
            )}
          </svg>
        </div>

        {/* ── Detail panel ── */}
        {selectedNode && (
          <aside className="tg-panel" onClick={e => e.stopPropagation()}>
            <button
              className="tg-panel-close"
              type="button"
              onClick={() => setSelected(null)}
              aria-label="Close panel"
            >
              ✕
            </button>

            <div className="tg-panel-type-badge" data-type={selectedNode.type}>
              {selectedNode.type === 'noise'   ? 'NOISE ALERT'
               : selectedNode.type === 'family' ? 'DETECTION FAMILY'
               : 'UNIQUE DETECTION'}
            </div>

            <h2 className="tg-panel-title">{selectedNode.fullLabel}</h2>

            <div className="tg-panel-metrics">
              {selectedNode.type === 'noise' && (
                <>
                  <div className="tg-metric">
                    <span className="tg-metric-label">Type</span>
                    <span className={`tg-metric-value tg-metric-value--${selectedNode.noiseType ?? 'structural'}`}>
                      {selectedNode.noiseType ?? 'structural'}
                    </span>
                  </div>
                  {(selectedNode.triggerCount ?? 0) > 0 && (
                    <div className="tg-metric">
                      <span className="tg-metric-label">Triggers</span>
                      <span className="tg-metric-value">
                        {(selectedNode.triggerCount ?? 0).toLocaleString()}
                      </span>
                    </div>
                  )}
                  {selectedNode.reason && (
                    <div className="tg-metric tg-metric--full">
                      <span className="tg-metric-label">Reason</span>
                      <span className="tg-metric-value tg-metric-value--prose">{selectedNode.reason}</span>
                    </div>
                  )}
                  {(selectedNode.missingFeatures ?? []).length > 0 && (
                    <div className="tg-metric tg-metric--full">
                      <span className="tg-metric-label">Missing</span>
                      <div className="tg-tag-list">
                        {(selectedNode.missingFeatures ?? []).map(f => (
                          <span key={f} className="tg-tag">{f}</span>
                        ))}
                      </div>
                    </div>
                  )}
                </>
              )}

              {selectedNode.type === 'family' && (
                <>
                  <div className="tg-metric">
                    <span className="tg-metric-label">Alerts</span>
                    <span className="tg-metric-value">{selectedNode.alertCount}</span>
                  </div>
                  <div className="tg-metric tg-metric--full">
                    <span className="tg-metric-label">Members</span>
                    <ul className="tg-alert-list">
                      {(selectedNode.alertNames ?? []).map(name => (
                        <li key={name}>{name}</li>
                      ))}
                    </ul>
                  </div>
                </>
              )}

              {selectedNode.type === 'unique' && (
                <div className="tg-metric tg-metric--full">
                  <span className="tg-metric-label">Status</span>
                  <span className="tg-metric-value tg-metric-value--prose">
                    Standalone detection — no similar alerts found in this client's coverage.
                  </span>
                </div>
              )}
            </div>

            {/* Connected nodes */}
            {(() => {
              const conns = edges.filter(e => e.source === selected || e.target === selected);
              if (conns.length === 0) return null;
              return (
                <div className="tg-panel-connections">
                  <div className="tg-panel-section-label">Connections ({conns.length})</div>
                  {conns.map(e => {
                    const otherId = e.source === selected ? e.target : e.source;
                    const other   = idToNode.get(otherId);
                    if (!other) return null;
                    return (
                      <button
                        key={e.id}
                        type="button"
                        className="tg-conn-item"
                        onClick={() => setSelected(otherId)}
                      >
                        <span className={`tg-conn-dot tg-conn-dot--${e.edgeType}`} />
                        <span className="tg-conn-label" title={other.fullLabel}>{other.fullLabel}</span>
                        <span className="tg-conn-sim">{Math.round(e.similarity * 100)}%</span>
                      </button>
                    );
                  })}
                </div>
              );
            })()}
          </aside>
        )}
      </div>

      {/* ── Legend ── */}
      <div className="tg-legend">
        <span className="tg-legend-item tg-legend-item--family">Detection Family</span>
        <span className="tg-legend-item tg-legend-item--behavioral">Behavioral Noise</span>
        <span className="tg-legend-item tg-legend-item--structural">Structural Noise</span>
        <span className="tg-legend-item tg-legend-item--unique">Unique Detection</span>
        {edges.some(e => e.edgeType === 'duplicate') && (
          <span className="tg-legend-item tg-legend-item--dup-edge">Duplicate Link</span>
        )}
        {edges.some(e => e.edgeType === 'merge') && (
          <span className="tg-legend-item tg-legend-item--merge-edge">Merge Suggestion</span>
        )}
        <span className="tg-legend-hint">Click a node to inspect · Click background to deselect</span>
      </div>
    </div>
  );
}
