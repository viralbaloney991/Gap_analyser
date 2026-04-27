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
): void {
  const doc = new jsPDF();
  const pageW = doc.internal.pageSize.getWidth();
  const margin = 14;
  const contentW = pageW - margin * 2;

  // ── Cover page ──────────────────────────────────────────────────────────
  doc.setFontSize(28);
  doc.setFont('helvetica', 'bold');
  doc.text('Security Alert Analysis', margin, 50);

  doc.setFontSize(16);
  doc.setFont('helvetica', 'normal');
  doc.text(client, margin, 65);

  doc.setFontSize(11);
  doc.text(date, margin, 78);

  doc.setFontSize(12);
  doc.text(`MITRE Coverage: ${mitreCoverage.summary.coverage_percent.toFixed(1)}%`, margin, 100);
  doc.text(`Detection Families: ${data.families.length}`, margin, 112);
  doc.text(`Noise Alerts: ${data.noise_alerts?.length ?? 0}`, margin, 124);

  // ── Executive Summary ───────────────────────────────────────────────────
  doc.addPage();
  doc.setFontSize(16);
  doc.setFont('helvetica', 'bold');
  doc.text('Executive Summary', margin, 20);
  doc.setFont('helvetica', 'normal');
  doc.setFontSize(10);
  const summaryLines = doc.splitTextToSize(narrative.executive_summary, contentW) as string[];
  doc.text(summaryLines, margin, 32);

  // ── Key Findings ────────────────────────────────────────────────────────
  const summaryHeight = summaryLines.length * 5;
  let findingsY = 32 + summaryHeight + 12;
  if (findingsY > 250) {
    doc.addPage();
    findingsY = 20;
  }
  doc.setFontSize(14);
  doc.setFont('helvetica', 'bold');
  doc.text('Key Findings', margin, findingsY);
  doc.setFont('helvetica', 'normal');
  doc.setFontSize(10);
  narrative.key_findings.forEach((finding, i) => {
    doc.text(`• ${finding}`, margin + 2, findingsY + 10 + i * 7);
  });

  // ── MITRE Coverage ──────────────────────────────────────────────────────
  doc.addPage();
  doc.setFontSize(14);
  doc.setFont('helvetica', 'bold');
  doc.text('MITRE Coverage', margin, 20);
  doc.setFont('helvetica', 'normal');

  const tc = mitreCoverage.technique_coverage ?? {};
  const techniqueRows: string[][] = Object.entries(tc)
    .filter(([, entry]) => entry.alert_count > 0)
    .sort((a, b) => a[1].tactic.localeCompare(b[1].tactic) || a[0].localeCompare(b[0]))
    .map(([tcode, entry]) => [
      tcode,
      entry.name,
      entry.tactic,
      String(entry.alert_count),
      entry.weak ? 'Weak' : 'Strong',
    ]);

  autoTable(doc, {
    head: [['T-Code', 'Technique Name', 'Tactic', 'Alert Count', 'Quality']],
    body: techniqueRows,
    startY: 28,
    styles: { fontSize: 8, cellPadding: 2 },
    headStyles: { fillColor: [30, 30, 30] as [number, number, number] },
    columnStyles: { 0: { cellWidth: 22 }, 3: { cellWidth: 24 }, 4: { cellWidth: 22 } },
  });

  // ── Noise Alerts ────────────────────────────────────────────────────────
  doc.addPage();
  doc.setFontSize(14);
  doc.setFont('helvetica', 'bold');
  doc.text('Noise Alerts', margin, 20);
  doc.setFont('helvetica', 'normal');
  const [noiseHeader, ...noiseBody] = noiseRows(data);
  autoTable(doc, {
    head: [noiseHeader],
    body: noiseBody,
    startY: 28,
    styles: { fontSize: 8, cellPadding: 2 },
    headStyles: { fillColor: [30, 30, 30] as [number, number, number] },
  });

  // ── Detection Families ──────────────────────────────────────────────────
  doc.addPage();
  doc.setFontSize(14);
  doc.setFont('helvetica', 'bold');
  doc.text('Detection Families', margin, 20);
  doc.setFont('helvetica', 'normal');
  const [famHeader, ...famBody] = familiesRows(data);
  autoTable(doc, {
    head: [famHeader],
    body: famBody,
    startY: 28,
    styles: { fontSize: 8, cellPadding: 2 },
    headStyles: { fillColor: [30, 30, 30] as [number, number, number] },
  });

  // ── Gap Analysis ────────────────────────────────────────────────────────
  doc.addPage();
  doc.setFontSize(14);
  doc.setFont('helvetica', 'bold');
  doc.text('Gap Analysis', margin, 20);
  doc.setFont('helvetica', 'normal');
  const [gapHeader, ...gapBody] = gapsRows(report);
  autoTable(doc, {
    head: [gapHeader],
    body: gapBody,
    startY: 28,
    styles: { fontSize: 8, cellPadding: 2 },
    headStyles: { fillColor: [30, 30, 30] as [number, number, number] },
  });

  // ── Recommended Actions ─────────────────────────────────────────────────
  doc.addPage();
  doc.setFontSize(14);
  doc.setFont('helvetica', 'bold');
  doc.text('Recommended Actions', margin, 20);
  doc.setFont('helvetica', 'normal');
  doc.setFontSize(10);
  narrative.recommended_actions.forEach((action, i) => {
    const lines = doc.splitTextToSize(`• ${action}`, contentW - 4) as string[];
    doc.text(lines, margin + 2, 32 + i * 12);
  });

  doc.save(`${client}-full-report-${date}.pdf`);
}
