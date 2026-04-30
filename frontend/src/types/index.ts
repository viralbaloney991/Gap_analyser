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

export interface TechniqueCoverageEntry {
  name: string;
  tactic: string;
  alert_count: number;
  weak?: boolean;
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
  technique_coverage?: Record<string, TechniqueCoverageEntry>;
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

/**
 * An alert flagged by the hybrid noise model.
 * `missing_features` lists which feature dimensions are empty for this alert.
 * `trigger_count` is 0 when behavioral data is unavailable or alert is not behaviorally noisy.
 * `noise_type` is "behavioral" | "structural" | "both".
 */
export interface NoiseAlert {
  name: string;
  missing_features: string[];
  reason?: string;
  /** 0 when behavioral data unavailable or not behaviorally noisy */
  trigger_count?: number;
  noise_type?: 'behavioral' | 'structural' | 'both';
}

export interface GapCategories {
  environment_cleanup: string[];
  no_detection: string[];
  poor_tactic_coverage: string[];
  weak_detection_quality: string[];
  advanced_use_cases: string[];
  missing_source_alerts: string[];
}

export interface ActionableRecommendation {
  prose: string;
  log_source: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  query_skeleton: string;
}

export interface ActionableGapCategories {
  no_detection: ActionableRecommendation[];
  weak_detection_quality: ActionableRecommendation[];
  missing_source_alerts: ActionableRecommendation[];
  advanced_use_cases: ActionableRecommendation[];
}

export interface SimilarityResult {
  families: DetectionFamily[];
  duplicates: DuplicateGroup[];
  merge_suggestions: MergeSuggestion[];
  unique_detections: string[];
  noise_alerts?: NoiseAlert[];
}

export interface InsightsReport {
  model?: string;
  summary: string;
  top_priority: string[];
  strengths: string[];
  recommendations: string[];
  enriched_dups: string[];
  gap_categories: GapCategories;
  actionable_gaps?: ActionableGapCategories;
  noise_explanations?: string[];
  all_integrations_vendor_managed?: boolean;
}

export interface ExportNarrativeReport {
  executive_summary: string;
  key_findings: string[];
  recommended_actions: string[];
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
