import * as XLSX from 'xlsx';
import jsPDF from 'jspdf';
import autoTable from 'jspdf-autotable';
import type {
  SimilarityResult,
  InsightsReport,
  GapCategories,
  MITRECoverageResult,
  ExportNarrativeReport,
  ActionableRecommendation,
} from '../types';

type ExportableTab = 'noise' | 'families' | 'gaps';

// ─── Row builders ──────────────────────────────────────────────────────────

function noiseRows(data: SimilarityResult): string[][] {
  const header = ['Name', 'Noise Type', 'Trigger Count', 'Reason', 'Missing Features'];
  const rows = (data.noise_alerts ?? []).map(a => [
    a.name,
    a.noise_type ?? 'structural',
    String(a.trigger_count ?? 0),
    a.reason ?? '',
    (a.missing_features ?? []).join(', '),
  ]);
  return [header, ...rows];
}

function familiesRows(data: SimilarityResult): string[][] {
  const header = ['Family Name', 'Alert Count', 'Alert Names'];
  const rows = data.families.map(f => [
    f.name,
    String(f.alert_names.length),
    f.alert_names.join(', '),
  ]);
  return [header, ...rows];
}

function gapsRows(report: InsightsReport): string[][] {
  const header = ['Category', 'Item', 'Severity', 'Log Source', 'Query'];
  const rows: string[][] = [];

  const actionableCategories: Array<{
    label: string;
    gapKey: keyof GapCategories;
    actionableKey: keyof NonNullable<InsightsReport['actionable_gaps']>;
  }> = [
    { label: 'No Detection', gapKey: 'no_detection', actionableKey: 'no_detection' },
    { label: 'Weak Detection Quality', gapKey: 'weak_detection_quality', actionableKey: 'weak_detection_quality' },
    { label: 'Missing Source Alerts', gapKey: 'missing_source_alerts', actionableKey: 'missing_source_alerts' },
    { label: 'Advanced Use Cases', gapKey: 'advanced_use_cases', actionableKey: 'advanced_use_cases' },
  ];

  for (const cat of actionableCategories) {
    const actionableItems = report.actionable_gaps?.[cat.actionableKey] as ActionableRecommendation[] | undefined;
    if (actionableItems && actionableItems.length > 0) {
      for (const item of actionableItems) {
        rows.push([cat.label, item.prose, item.severity, item.log_source, item.query_skeleton]);
      }
    } else {
      for (const item of report.gap_categories[cat.gapKey] as string[]) {
        rows.push([cat.label, item, '', '', '']);
      }
    }
  }

  const plainCategories: Array<{ label: string; key: 'environment_cleanup' | 'poor_tactic_coverage' }> = [
    { label: 'Environment Cleanup', key: 'environment_cleanup' },
    { label: 'Poor Tactic Coverage', key: 'poor_tactic_coverage' },
  ];
  for (const cat of plainCategories) {
    for (const item of report.gap_categories[cat.key]) {
      rows.push([cat.label, item, '', '', '']);
    }
  }

  return [header, ...rows];
}

// ─── Public exports ────────────────────────────────────────────────────────

export function exportTabAsXLSX(
  tab: ExportableTab,
  data: SimilarityResult,
  report: InsightsReport | null,
  client: string,
): void {
  let rows: string[][];
  if (tab === 'noise') {
    rows = noiseRows(data);
  } else if (tab === 'families') {
    rows = familiesRows(data);
  } else if (tab === 'gaps' && report) {
    rows = gapsRows(report);
  } else {
    return;
  }
  const ws = XLSX.utils.aoa_to_sheet(rows);
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, tab);
  XLSX.writeFile(wb, `${client}-${tab}.xlsx`);
}

export function exportTabAsPDF(
  tab: ExportableTab,
  data: SimilarityResult,
  report: InsightsReport | null,
  client: string,
): void {
  let rows: string[][];
  if (tab === 'noise') {
    rows = noiseRows(data);
  } else if (tab === 'families') {
    rows = familiesRows(data);
  } else if (tab === 'gaps' && report) {
    rows = gapsRows(report);
  } else {
    return;
  }

  const [header, ...body] = rows;
  const doc = new jsPDF();
  doc.setFontSize(14);
  doc.text(`${client} — ${tab}`, 14, 18);
  autoTable(doc, {
    head: [header],
    body,
    startY: 26,
    styles: { fontSize: 8, cellPadding: 2 },
    headStyles: { fillColor: [30, 30, 30] as [number, number, number] },
  });
  doc.save(`${client}-${tab}.pdf`);
}

export function exportFullReportPDF(
  client: string,
  data: SimilarityResult,
  report: InsightsReport,
  mitreCoverage: MITRECoverageResult,
  narrative: ExportNarrativeReport,
  date: string,
  totalAlerts: number,
): void {
  const doc = new jsPDF({ orientation: 'portrait', unit: 'mm', format: 'a4' });
  const pageW = doc.internal.pageSize.getWidth();
  const pageH = doc.internal.pageSize.getHeight();
  const margin = 16;
  const contentW = pageW - margin * 2;

  // ── Design tokens ────────────────────────────────────────────────────────
  type RGB = [number, number, number];
  const DARK_BG:   RGB = [13, 17, 23];
  const DARK_NAVY: RGB = [20, 30, 50];
  const ACCENT:    RGB = [0, 185, 155];
  const WHITE:     RGB = [255, 255, 255];
  const TEXT:      RGB = [22, 28, 40];
  const TEXT_MUT:  RGB = [105, 115, 135];
  const ROW_ALT:   RGB = [244, 247, 252];
  const BORDER:    RGB = [218, 224, 234];
  const GREEN:     RGB = [22, 163, 74];
  const AMBER:     RGB = [180, 95, 6];
  const RED:       RGB = [185, 28, 28];
  const RED_DIM:   RGB = [254, 226, 226];
  const AMBER_DIM: RGB = [254, 243, 199];
  const BLUE:      RGB = [37, 99, 235];

  function fill(c: RGB)  { doc.setFillColor(c[0], c[1], c[2]); }
  function stroke(c: RGB){ doc.setDrawColor(c[0], c[1], c[2]); }
  function color(c: RGB) { doc.setTextColor(c[0], c[1], c[2]); }

  function sectionTitle(title: string, y: number): number {
    doc.setFont('helvetica', 'bold');
    doc.setFontSize(12);
    color(DARK_NAVY);
    doc.text(title.toUpperCase(), margin, y);
    stroke(ACCENT);
    doc.setLineWidth(0.6);
    doc.line(margin, y + 2.8, pageW - margin, y + 2.8);
    return y + 11;
  }

  // ── COVER PAGE ───────────────────────────────────────────────────────────
  fill(DARK_BG);
  doc.rect(0, 0, pageW, pageH, 'F');

  // Top accent stripe
  fill(ACCENT);
  doc.rect(0, 0, pageW, 2.5, 'F');

  // Decorative step lines (top-right)
  stroke(ACCENT);
  doc.setLineWidth(0.35);
  for (let i = 0; i < 6; i++) {
    const len = 30 - i * 4;
    if (len > 0) doc.line(pageW - margin - len, 14 + i * 4.5, pageW - margin, 14 + i * 4.5);
  }

  // Title
  doc.setFont('helvetica', 'bold');
  doc.setFontSize(30);
  color(WHITE);
  doc.text('SECURITY ALERT', margin, 64);
  doc.text('ANALYSIS', margin, 78);

  // Accent underline
  fill(ACCENT);
  doc.rect(margin, 83, 56, 1.8, 'F');

  // Client name
  doc.setFontSize(20);
  color(ACCENT);
  doc.text(client, margin, 98);

  // Date + by-line
  doc.setFontSize(9);
  doc.setFont('helvetica', 'normal');
  doc.setTextColor(150, 165, 185);
  doc.text(date, margin, 108);
  doc.setFontSize(7);
  doc.setTextColor(62, 78, 100);
  doc.text('PREPARED BY CORALOGIX ALERT ANALYZER', margin, 117);

  // Stats cards 2 × 2
  const cardW = (contentW - 6) / 2;
  const STATS: Array<{ label: string; value: string }> = [
    { label: 'TOTAL ALERTS',       value: String(totalAlerts) },
    { label: 'MITRE COVERAGE',     value: `${mitreCoverage.summary.coverage_percent.toFixed(1)}%` },
    { label: 'DETECTION FAMILIES', value: String(data.families.length) },
    { label: 'NOISE ALERTS',       value: String(data.noise_alerts?.length ?? 0) },
  ];
  STATS.forEach((s, i) => {
    const cx = margin + (i % 2) * (cardW + 6);
    const cy = 132 + Math.floor(i / 2) * 34;
    doc.setFillColor(22, 30, 46);
    doc.roundedRect(cx, cy, cardW, 28, 2, 2, 'F');
    fill(ACCENT);
    doc.rect(cx, cy, 3.5, 28, 'F');
    doc.setFont('helvetica', 'bold');
    doc.setFontSize(22);
    color(WHITE);
    doc.text(s.value, cx + 10, cy + 16);
    doc.setFont('helvetica', 'normal');
    doc.setFontSize(7);
    doc.setTextColor(108, 128, 158);
    doc.text(s.label, cx + 10, cy + 23);
  });

  // Confidential footer on cover
  doc.setFont('helvetica', 'normal');
  doc.setFontSize(7);
  doc.setTextColor(45, 58, 74);
  const conf = 'CONFIDENTIAL — FOR AUTHORIZED RECIPIENTS ONLY';
  doc.text(conf, pageW / 2 - doc.getTextWidth(conf) / 2, pageH - 8);

  // ── EXECUTIVE SUMMARY ────────────────────────────────────────────────────
  doc.addPage();
  let y = 22;
  y = sectionTitle('Executive Summary', y);

  doc.setFont('helvetica', 'normal');
  doc.setFontSize(9.5);
  color(TEXT);
  const sumLines = doc.splitTextToSize(narrative.executive_summary, contentW) as string[];
  doc.text(sumLines, margin, y);
  y += sumLines.length * 4.8 + 14;

  // ── KEY FINDINGS ─────────────────────────────────────────────────────────
  if (y > pageH - 65) { doc.addPage(); y = 22; }
  y = sectionTitle('Key Findings', y);

  narrative.key_findings.forEach((finding, i) => {
    const fl = doc.splitTextToSize(finding, contentW - 10) as string[];
    const rowH = fl.length * 4.5 + 3;
    if (y + rowH > pageH - 18) { doc.addPage(); y = 22; }

    fill(ACCENT);
    doc.roundedRect(margin, y - 3.5, 5.5, 5.5, 1, 1, 'F');
    doc.setFont('helvetica', 'bold');
    doc.setFontSize(7);
    color(WHITE);
    doc.text(String(i + 1), margin + 1.5, y + 0.4);

    doc.setFont('helvetica', 'normal');
    doc.setFontSize(9);
    color(TEXT);
    doc.text(fl, margin + 8, y);
    y += rowH;
  });

  // ── MITRE ATT&CK COVERAGE ─────────────────────────────────────────────────
  doc.addPage();
  y = 22;
  y = sectionTitle('MITRE ATT&CK Coverage', y);

  const tc = mitreCoverage.technique_coverage ?? {};
  const tcRows = Object.entries(tc)
    .filter(([, e]) => e.alert_count > 0)
    .sort((a, b) => a[1].tactic.localeCompare(b[1].tactic) || a[0].localeCompare(b[0]))
    .map(([code, e]) => [code, e.name, e.tactic, String(e.alert_count), e.weak ? 'Weak' : 'Strong']);

  autoTable(doc, {
    head: [['T-Code', 'Technique Name', 'Tactic', 'Alerts', 'Quality']],
    body: tcRows,
    startY: y,
    margin: { left: margin, right: margin, top: 18, bottom: 16 },
    styles: {
      fontSize: 8,
      cellPadding: { top: 2.5, bottom: 2.5, left: 3, right: 3 },
      textColor: TEXT as [number, number, number],
      lineColor: BORDER as [number, number, number],
      lineWidth: 0.2,
      overflow: 'linebreak',
    },
    headStyles: {
      fillColor: DARK_NAVY as [number, number, number],
      textColor: WHITE as [number, number, number],
      fontStyle: 'bold',
      fontSize: 7.5,
    },
    alternateRowStyles: { fillColor: ROW_ALT as [number, number, number] },
    columnStyles: {
      0: { cellWidth: 24 },
      3: { cellWidth: 18, halign: 'center' },
      4: { cellWidth: 20, halign: 'center' },
    },
    didParseCell(hookData) {
      if (hookData.column.index === 4 && hookData.section === 'body') {
        hookData.cell.styles.textColor = (hookData.cell.raw === 'Strong' ? GREEN : AMBER) as [number, number, number];
        hookData.cell.styles.fontStyle = 'bold';
      }
    },
  });

  // ── NOISE ALERTS ─────────────────────────────────────────────────────────
  doc.addPage();
  y = 22;
  y = sectionTitle('Noise Alerts', y);

  const [nh, ...nb] = noiseRows(data);
  autoTable(doc, {
    head: [nh],
    body: nb,
    startY: y,
    margin: { left: margin, right: margin, top: 18, bottom: 16 },
    styles: {
      fontSize: 7.5,
      cellPadding: { top: 2, bottom: 2, left: 3, right: 3 },
      textColor: TEXT as [number, number, number],
      lineColor: BORDER as [number, number, number],
      lineWidth: 0.2,
      overflow: 'linebreak',
    },
    headStyles: {
      fillColor: DARK_NAVY as [number, number, number],
      textColor: WHITE as [number, number, number],
      fontStyle: 'bold',
    },
    alternateRowStyles: { fillColor: ROW_ALT as [number, number, number] },
    columnStyles: {
      1: { cellWidth: 26 },
      2: { cellWidth: 20, halign: 'center' },
    },
    didParseCell(hookData) {
      if (hookData.column.index === 1 && hookData.section === 'body') {
        const t = String(hookData.cell.raw).toLowerCase();
        hookData.cell.styles.textColor = (t.includes('behavioral') ? AMBER : BLUE) as [number, number, number];
        hookData.cell.styles.fontStyle = 'bold';
      }
    },
  });

  // ── DETECTION FAMILIES ────────────────────────────────────────────────────
  doc.addPage();
  y = 22;
  y = sectionTitle('Detection Families', y);

  const [fh, ...fb] = familiesRows(data);
  autoTable(doc, {
    head: [fh],
    body: fb,
    startY: y,
    margin: { left: margin, right: margin, top: 18, bottom: 16 },
    styles: {
      fontSize: 8,
      cellPadding: { top: 2.5, bottom: 2.5, left: 3, right: 3 },
      textColor: TEXT as [number, number, number],
      lineColor: BORDER as [number, number, number],
      lineWidth: 0.2,
      overflow: 'linebreak',
    },
    headStyles: {
      fillColor: DARK_NAVY as [number, number, number],
      textColor: WHITE as [number, number, number],
      fontStyle: 'bold',
    },
    alternateRowStyles: { fillColor: ROW_ALT as [number, number, number] },
    columnStyles: { 1: { cellWidth: 22, halign: 'center' } },
  });

  // ── GAP ANALYSIS ──────────────────────────────────────────────────────────
  doc.addPage();
  y = 22;
  y = sectionTitle('Gap Analysis', y);

  const [gh, ...gb] = gapsRows(report);
  autoTable(doc, {
    head: [gh],
    body: gb,
    startY: y,
    margin: { left: margin, right: margin, top: 18, bottom: 16 },
    styles: {
      fontSize: 7.5,
      cellPadding: { top: 2.5, bottom: 2.5, left: 3, right: 3 },
      textColor: TEXT as [number, number, number],
      lineColor: BORDER as [number, number, number],
      lineWidth: 0.2,
      overflow: 'linebreak',
    },
    headStyles: {
      fillColor: DARK_NAVY as [number, number, number],
      textColor: WHITE as [number, number, number],
      fontStyle: 'bold',
      fontSize: 7.5,
    },
    alternateRowStyles: { fillColor: ROW_ALT as [number, number, number] },
    columnStyles: {
      0: { cellWidth: 30 },
      2: { cellWidth: 18, halign: 'center' },
      3: { cellWidth: 28 },
    },
    didParseCell(hookData) {
      if (hookData.column.index === 2 && hookData.section === 'body') {
        const sev = String(hookData.cell.raw).toLowerCase();
        if (sev === 'high')   { hookData.cell.styles.textColor = RED as [number, number, number];   hookData.cell.styles.fillColor = RED_DIM as [number, number, number];   hookData.cell.styles.fontStyle = 'bold'; }
        if (sev === 'medium') { hookData.cell.styles.textColor = AMBER as [number, number, number]; hookData.cell.styles.fillColor = AMBER_DIM as [number, number, number]; hookData.cell.styles.fontStyle = 'bold'; }
        if (sev === 'low')    { hookData.cell.styles.textColor = BLUE as [number, number, number];  hookData.cell.styles.fontStyle = 'bold'; }
      }
    },
  });

  // ── RECOMMENDED ACTIONS ───────────────────────────────────────────────────
  doc.addPage();
  y = 22;
  y = sectionTitle('Recommended Actions', y);

  narrative.recommended_actions.forEach((action, i) => {
    const lines = doc.splitTextToSize(action, contentW - 18) as string[];
    const cardH = lines.length * 4.5 + 9;
    if (y + cardH > pageH - 18) { doc.addPage(); y = 22; }

    doc.setFillColor(244, 247, 252);
    stroke(BORDER);
    doc.setLineWidth(0.25);
    doc.roundedRect(margin, y - 3, contentW, cardH, 1.5, 1.5, 'FD');
    fill(ACCENT);
    doc.rect(margin, y - 3, 3.5, cardH, 'F');

    doc.setFont('helvetica', 'bold');
    doc.setFontSize(8);
    color(ACCENT);
    doc.text((i + 1).toString().padStart(2, '0'), margin + 6.5, y + cardH / 2 - 5);

    doc.setFont('helvetica', 'normal');
    doc.setFontSize(8.5);
    color(TEXT);
    doc.text(lines, margin + 17, y);
    y += cardH + 4;
  });

  // ── HEADERS + FOOTERS (applied post-hoc to all interior pages) ─────────────
  const totalPgs = doc.getNumberOfPages();
  for (let pg = 2; pg <= totalPgs; pg++) {
    doc.setPage(pg);

    // Header bar
    fill(DARK_NAVY);
    doc.rect(0, 0, pageW, 12, 'F');
    fill(ACCENT);
    doc.rect(0, 0, pageW, 1.5, 'F');
    doc.setFont('helvetica', 'bold');
    doc.setFontSize(6.5);
    color(WHITE);
    doc.text('SECURITY ALERT ANALYSIS', margin, 8);
    doc.setFont('helvetica', 'normal');
    const cl = client.toUpperCase();
    doc.text(cl, pageW - margin - doc.getTextWidth(cl), 8);

    // Footer
    doc.setFont('helvetica', 'normal');
    doc.setFontSize(7);
    color(TEXT_MUT);
    doc.text(`Confidential — ${client}`, margin, pageH - 6);
    const pgLabel = `${pg - 1} / ${totalPgs - 1}`;
    doc.text(pgLabel, pageW - margin - doc.getTextWidth(pgLabel), pageH - 6);
    stroke(BORDER);
    doc.setLineWidth(0.3);
    doc.line(margin, pageH - 10.5, pageW - margin, pageH - 10.5);
  }

  doc.save(`${client}-full-report-${date}.pdf`);
}
