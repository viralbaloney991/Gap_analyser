export interface ClientInfo {
  name: string;
  region: string;
}

export interface IntegrationInfo {
  name: string;
  application: string;
  subsystem: string;
  alert_count: number;
}

export interface AnalysisStats {
  total_integrations: number;
  done_integrations: number;
  total_alerts: number;
  security_alerts: number;
  vendor_covered_alerts: number;
  integrations_with_alerts: number;
}

export interface TacticCoverage {
  tactic_name: string;
  total: number;
  covered: number;
  percent: number;
  total_subs: number;
  covered_subs: number;
}

export interface MITRECoverageSummary {
  total_techniques: number;
  covered_techniques: number;
  coverage_percent: number;
  total_sub_techniques: number;
  covered_sub_techniques: number;
  tactic_breakdown: Record<string, TacticCoverage>;
}

export interface NavigatorTechnique {
  techniqueID: string;
  tactic: string;
  score: number;
  color: string;
  comment: string;
  name: string;
}

export interface NavigatorLayer {
  name: string;
  domain: string;
  versions: { attack: string; navigator: string; layer: string };
  techniques: NavigatorTechnique[];
  gradient: { colors: string[]; minValue: number; maxValue: number };
  legendItems: { label: string; color: string }[];
  [key: string]: unknown;
}

export interface MITRECoverageResult {
  navigator_layer: NavigatorLayer;
  summary: MITRECoverageSummary;
}

export interface DetectionFamily {
  name: string;
  alert_ids: string[];
  alert_names: string[];
}

export interface DuplicateGroup {
  alert_ids: string[];
  alert_names: string[];
  similarity: number;
  explanation: string;
}

export interface MergeSuggestion {
  alert_ids: string[];
  alert_names: string[];
  reason: string;
}

export interface SimilarityResult {
  families: DetectionFamily[];
  duplicates: DuplicateGroup[];
  merge_suggestions: MergeSuggestion[];
  coverage_insights: string[];
  unique_detections: string[];
  noise_alerts?: string[];
}

export interface InsightsReport {
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  enriched_gaps: string[];
}

export interface AnalyzeResponse {
  integrations: IntegrationInfo[];
  stats: AnalysisStats;
  mitre_coverage: MITRECoverageResult;
  alert_insights: SimilarityResult;
  insights_report?: InsightsReport | null;
  cached: boolean;
}

export interface AlertSuggestion {
  log_source: string;
  alert_name: string;
  description: string;
  query_hint: string;
  priority: string;
}

export interface SuggestionsResponse {
  provider: string;
  technique_id: string;
  technique_name: string;
  suggestions: AlertSuggestion[];
  log_sources: string[];
}
