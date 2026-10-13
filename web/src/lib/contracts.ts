/**
 * API response normalization.
 *
 * Every response the pages consume passes through `normalizeAPIResponse`, so the
 * rendering code never sees `undefined` in a field it treats as a list, a number
 * or a date. Normalization only fills in safe defaults; it never invents
 * business values. Calendar dates are reduced to pure `YYYY-MM-DD` keys, which is
 * the same semantic the API exposes for DATE columns.
 */

import { pureDateKey } from "./dates.ts";
import { toFiniteNumber } from "./format.ts";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function asString(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value);
  }
  return "";
}

function asNumber(value: unknown, fallback = 0): number {
  return toFiniteNumber(value) ?? fallback;
}

function asNullableNumber(value: unknown): number | null {
  return toFiniteNumber(value);
}

function asRecords(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
}

export interface OverviewTopContent {
  id: string;
  title: string;
  category: string;
  platform: string;
  completion: number | null;
  interaction_rate: number | null;
  score: number | null;
  published_at: string;
}

export interface OverviewMetrics {
  published_count: number;
  unpublished_count: number;
  total_content_count: number;
  total_posts: number;
  total_views: number;
  total_likes: number;
  total_interactions: number;
  platform_count: number;
  avg_completion: number | null;
  sample_count: number;
  top_contents: OverviewTopContent[];
  weights: { completion: number; interaction: number };
  performances: OverviewPerformances;
}

/**
 * The 正式演出 statistics shown on the operations overview and the home page.
 * They come from persisted shows, content links and plans, so a value is only
 * absent (`null`) when the underlying data does not exist yet.
 */
export interface OverviewPerformances {
  preparing_count: number;
  warming_up_count: number;
  played_count: number;
  canceled_count: number;
  linked_performance_count: number;
  unpublished_promo_count: number;
  planned_count: number;
  plan_total: number;
  schedule_completion_rate: number | null;
  next_starts_at: string;
  next_days_to_start: number | null;
}

function normalizeOverviewPerformances(value: unknown): OverviewPerformances {
  const source = isRecord(value) ? value : {};
  return {
    preparing_count: asNumber(source.preparing_count),
    warming_up_count: asNumber(source.warming_up_count),
    played_count: asNumber(source.played_count),
    canceled_count: asNumber(source.canceled_count),
    linked_performance_count: asNumber(source.linked_performance_count),
    unpublished_promo_count: asNumber(source.unpublished_promo_count),
    planned_count: asNumber(source.planned_count),
    plan_total: asNumber(source.plan_total),
    schedule_completion_rate: asNullableNumber(source.schedule_completion_rate),
    next_starts_at: asString(source.next_starts_at),
    next_days_to_start: asNullableNumber(source.next_days_to_start),
  };
}

/** Normalizes the operations overview payload into always-present fields. */
export function normalizeOverview(payload: unknown): OverviewMetrics {
  const source = isRecord(payload) ? payload : {};
  const weights = isRecord(source.weights) ? source.weights : {};
  const published = asNumber(source.published_count);
  return {
    published_count: published,
    unpublished_count: asNumber(source.unpublished_count),
    total_content_count: asNumber(source.total_content_count, published),
    total_posts: asNumber(source.total_posts, published),
    total_views: asNumber(source.total_views),
    total_likes: asNumber(source.total_likes),
    total_interactions: asNumber(source.total_interactions),
    platform_count: asNumber(source.platform_count),
    avg_completion: asNullableNumber(source.avg_completion),
    sample_count: asNumber(source.sample_count),
    top_contents: asRecords(source.top_contents).map((row) => ({
      id: asString(row.id),
      title: asString(row.title),
      category: asString(row.category),
      platform: asString(row.platform),
      completion: asNullableNumber(row.completion),
      interaction_rate: asNullableNumber(row.interaction_rate),
      score: asNullableNumber(row.score),
      published_at: asString(row.published_at),
    })),
    weights: {
      completion: asNumber(weights.completion, 0.6),
      interaction: asNumber(weights.interaction, 0.4),
    },
    performances: normalizeOverviewPerformances(source.performances),
  };
}

export interface PerformanceRow {
  id: string;
  title: string;
  venue: string;
  starts_at: string;
  booking_status: string;
  performance_type: string;
  ticket_link: string;
  owner: string;
  owner_user_id: number | null;
  notes: string;
  content_count: number;
  published_content_count: number;
  unpublished_content_count: number;
  schedulable_content_count: number;
  scheduled_content_count: number;
}

/** Normalizes one 正式演出 row, keeping counters numeric and dates as text. */
export function normalizePerformance(payload: unknown): PerformanceRow {
  const row = isRecord(payload) ? payload : {};
  const ownerID = asNullableNumber(row.owner_user_id);
  return {
    id: asString(row.id),
    title: asString(row.title),
    venue: asString(row.venue),
    starts_at: asString(row.starts_at),
    booking_status: asString(row.booking_status),
    performance_type: asString(row.performance_type),
    ticket_link: asString(row.ticket_link),
    owner: asString(row.owner) || asString(row.owner_name),
    owner_user_id: ownerID,
    notes: asString(row.notes),
    content_count: asNumber(row.content_count),
    published_content_count: asNumber(row.published_content_count),
    unpublished_content_count: asNumber(row.unpublished_content_count),
    schedulable_content_count: asNumber(row.schedulable_content_count),
    scheduled_content_count: asNumber(row.scheduled_content_count),
  };
}

/** Normalizes the 正式演出 list. */
export function normalizePerformanceList(payload: unknown): PerformanceRow[] {
  return asRecords(payload).map(normalizePerformance);
}

export interface PerformanceDetail {
  performance: PerformanceRow;
  days_to_start: number | null;
  published: Record<string, unknown>[];
  unpublished: Record<string, unknown>[];
  timeline: ScheduleRow[];
  counts: Record<string, number>;
  status_options: string[];
}

/** Normalizes one 正式演出 detail payload with its aggregated content. */
export function normalizePerformanceDetail(payload: unknown): PerformanceDetail {
  const source = isRecord(payload) ? payload : {};
  const counts = isRecord(source.counts) ? source.counts : {};
  const normalizedCounts: Record<string, number> = {};
  for (const [key, value] of Object.entries(counts)) {
    normalizedCounts[key] = asNumber(value);
  }
  return {
    performance: normalizePerformance(source.performance),
    days_to_start: asNullableNumber(source.days_to_start),
    published: asRecords(source.published).map((row) => ({ ...row, published_at: asString(row.published_at) })),
    unpublished: asRecords(source.unpublished).map((row) => ({ ...row, planned_date: pureDateKey(row.planned_date) })),
    timeline: asRecords(source.timeline).map(normalizeScheduleRow),
    counts: normalizedCounts,
    status_options: Array.isArray(source.status_options) ? source.status_options.map(asString).filter(Boolean) : [],
  };
}

export interface PerformanceComparisonRow {
  id: string;
  title: string;
  venue: string;
  booking_status: string;
  performance_type: string;
  starts_at: string;
  published_count: number;
  unpublished_count: number;
  total_views: number;
  total_interactions: number;
  avg_completion: number | null;
  interaction_rate: number | null;
  sample_sufficient: boolean;
}

export interface PerformanceComparison {
  items: PerformanceComparisonRow[];
  min_sample: number;
  baseline: { sample_count: number; avg_completion: number | null; interaction_rate: number | null };
}

/** Normalizes the per-show comparison used by the analysis page. */
export function normalizePerformanceComparison(payload: unknown): PerformanceComparison {
  const source = isRecord(payload) ? payload : {};
  const baseline = isRecord(source.baseline) ? source.baseline : {};
  return {
    items: asRecords(source.items).map((row) => ({
      id: asString(row.id),
      title: asString(row.title),
      venue: asString(row.venue),
      booking_status: asString(row.booking_status),
      performance_type: asString(row.performance_type),
      starts_at: asString(row.starts_at),
      published_count: asNumber(row.published_count),
      unpublished_count: asNumber(row.unpublished_count),
      total_views: asNumber(row.total_views),
      total_interactions: asNumber(row.total_interactions),
      avg_completion: asNullableNumber(row.avg_completion),
      interaction_rate: asNullableNumber(row.interaction_rate),
      sample_sufficient: row.sample_sufficient === true,
    })),
    min_sample: asNumber(source.min_sample, 3),
    baseline: {
      sample_count: asNumber(baseline.sample_count),
      avg_completion: asNullableNumber(baseline.avg_completion),
      interaction_rate: asNullableNumber(baseline.interaction_rate),
    },
  };
}

export interface ScheduleAdjustment {
  actor: string;
  detail: string;
  created_at: string;
}

/** Normalizes recorded manual schedule changes. */
export function normalizeScheduleAdjustments(payload: unknown): ScheduleAdjustment[] {
  const source = isRecord(payload) ? payload : {};
  return asRecords(source.items).map((row) => ({
    actor: asString(row.actor),
    detail: asString(row.detail),
    created_at: asString(row.created_at),
  }));
}

export interface ScheduleRow {
  id: string;
  content_id: string;
  planned_date: string;
  planned_time: string;
  title: string;
  category: string;
  platform: string;
  owner: string;
  reason: string;
  status: string;
  source: string;
  source_label: string;
  operator: string;
}

/** Normalizes one calendar row, keeping `planned_date` a pure calendar date. */
export function normalizeScheduleRow(payload: unknown): ScheduleRow {
  const source = isRecord(payload) ? payload : {};
  return {
    id: asString(source.id),
    content_id: asString(source.content_id),
    planned_date: pureDateKey(source.planned_date),
    planned_time: asString(source.planned_time),
    title: asString(source.title),
    category: asString(source.category),
    platform: asString(source.platform),
    owner: asString(source.owner),
    reason: asString(source.reason),
    status: asString(source.status),
    source: asString(source.source),
    source_label: asString(source.source_label),
    operator: asString(source.operator),
  };
}

/** Normalizes the calendar list endpoint. */
export function normalizeScheduleList(payload: unknown): ScheduleRow[] {
  return asRecords(payload).map(normalizeScheduleRow);
}

export interface SchedulePolicy {
  planning_horizon_days: number;
  horizon_definition: string;
  time_windows: string[];
  weekdays: string[];
  max_per_day: number;
  max_per_platform_per_week: number;
  window_start: string;
  window_end: string;
}

/** Normalizes the read-only scheduling policy the suggestion page pre-checks against. */
export function normalizeSchedulePolicy(payload: unknown): SchedulePolicy {
  const source = isRecord(payload) ? payload : {};
  return {
    planning_horizon_days: asNumber(source.planning_horizon_days),
    horizon_definition: asString(source.horizon_definition),
    time_windows: Array.isArray(source.time_windows) ? source.time_windows.map(asString).filter(Boolean) : [],
    weekdays: Array.isArray(source.weekdays) ? source.weekdays.map(asString).filter(Boolean) : [],
    max_per_day: asNumber(source.max_per_day),
    max_per_platform_per_week: asNumber(source.max_per_platform_per_week),
    window_start: pureDateKey(source.window_start),
    window_end: pureDateKey(source.window_end),
  };
}

export interface AdoptionGroup {
  avg_completion: number | null;
  sample_count: number;
}

export interface AdoptionStats {
  adopted: AdoptionGroup;
  not_adopted: AdoptionGroup;
  completion_difference: number | null;
}

function normalizeAdoptionGroup(value: unknown): AdoptionGroup {
  const source = isRecord(value) ? value : {};
  return { avg_completion: asNullableNumber(source.avg_completion), sample_count: asNumber(source.sample_count) };
}

/** Normalizes adoption comparison statistics, including the empty-team shape. */
export function normalizeAdoption(payload: unknown): AdoptionStats {
  const source = isRecord(payload) ? payload : {};
  return {
    adopted: normalizeAdoptionGroup(source.adopted),
    not_adopted: normalizeAdoptionGroup(source.not_adopted),
    completion_difference: asNullableNumber(source.completion_difference),
  };
}

export interface FunnelStats {
  views: number;
  profile_visits: number;
  follows: number;
  attendance: number;
  tickets: number;
  sample_count: number;
}

/** Normalizes conversion funnel totals. */
export function normalizeFunnel(payload: unknown): FunnelStats {
  const source = isRecord(payload) ? payload : {};
  return {
    views: asNumber(source.views),
    profile_visits: asNumber(source.profile_visits),
    follows: asNumber(source.follows),
    attendance: asNumber(source.attendance),
    tickets: asNumber(source.tickets),
    sample_count: asNumber(source.sample_count),
  };
}

/** Normalizes platform totals; counts stay counts. */
export function normalizePlatformStats(payload: unknown): { items: { name: string; total_views: number; total_likes: number; sample_count: number }[] } {
  const source = isRecord(payload) ? payload : {};
  return {
    items: asRecords(source.items).map((row) => ({
      name: asString(row.name),
      total_views: asNumber(row.total_views),
      total_likes: asNumber(row.total_likes),
      sample_count: asNumber(row.sample_count),
    })),
  };
}

export interface DiagnosticItem {
  level: string;
  message: string;
}

/** Normalizes the operations reminders shown on the overview page. */
export function normalizeDiagnostics(payload: unknown): { items: DiagnosticItem[] } {
  const source = isRecord(payload) ? payload : {};
  return {
    items: asRecords(source.items)
      .map((row) => ({ level: asString(row.level) || "info", message: asString(row.message) }))
      .filter((item) => item.message !== ""),
  };
}

export interface ContentRecordExtra {
  id: string;
  title: string;
  published_at: string;
  views: number;
  total_interactions: number;
  [key: string]: unknown;
}

/**
 * Normalizes content rows. `published_at` always becomes a string so display
 * code can slice it safely, and counters always become numbers.
 */
export function normalizeContentList(payload: unknown): { items: ContentRecordExtra[]; counts: Record<string, number> } {
  const source = isRecord(payload) ? payload : {};
  const items = asRecords(source.items).map((row) => ({
    ...row,
    id: asString(row.id),
    title: asString(row.title),
    published_at: asString(row.published_at),
    views: asNumber(row.views),
    total_interactions: asNumber(row.total_interactions),
    tags: Array.isArray(row.tags) ? row.tags.map(asString) : [],
  }));
  // Category counts are aggregated by the server; the client only mirrors them so
  // the badges never depend on the current page. A missing map stays empty and the
  // page falls back to counting the loaded records.
  const rawCounts = isRecord(source.counts) ? source.counts : {};
  const counts: Record<string, number> = {};
  for (const [key, value] of Object.entries(rawCounts)) {
    counts[key] = asNumber(value);
  }
  return { items, counts };
}

export interface TeamRule {
  id: number;
  name: string;
  priority: number;
  enabled: boolean;
  rule_type: string;
  content_type: string;
  workflow_state: string;
  rule_content: string;
  trigger: string;
  logic: string;
  notes: string;
  updated_at: string;
  updated_by: string;
  last_result: string;
  last_detail: string;
  last_executed_at: string;
}

/** Normalizes team-authored rules, tolerating legacy rows with missing columns. */
export function normalizeRuleList(payload: unknown): TeamRule[] {
  return asRecords(payload).map((row) => ({
    id: asNumber(row.id),
    name: asString(row.name),
    priority: asNumber(row.priority),
    enabled: row.enabled === true,
    rule_type: asString(row.rule_type),
    content_type: asString(row.content_type),
    workflow_state: asString(row.workflow_state),
    rule_content: asString(row.rule_content),
    trigger: asString(row.trigger),
    logic: asString(row.logic),
    notes: asString(row.notes),
    updated_at: asString(row.updated_at),
    updated_by: asString(row.updated_by),
    last_result: asString(row.last_result),
    last_detail: asString(row.last_detail),
    last_executed_at: asString(row.last_executed_at),
  }));
}

export interface RuleLog {
  rule_name: string;
  content_id: string;
  trigger: string;
  result: string;
  detail: string;
  created_at: string;
}

/** Normalizes rule execution logs. */
export function normalizeRuleLogs(payload: unknown): RuleLog[] {
  return asRecords(payload).map((row) => ({
    rule_name: asString(row.rule_name),
    content_id: asString(row.content_id),
    trigger: asString(row.trigger),
    result: asString(row.result),
    detail: asString(row.detail),
    created_at: asString(row.created_at),
  }));
}

export interface SystemRuleSet {
  id: number;
  name: string;
  version: string;
  enabled: boolean;
  effective: boolean;
  updated_at: string;
  description: string;
  rule_count: number;
}

/** Normalizes the read-only system rule sets. */
export function normalizeSystemRuleSets(payload: unknown): SystemRuleSet[] {
  const source = isRecord(payload) ? payload : {};
  return asRecords(source.items).map((row) => ({
    id: asNumber(row.id),
    name: asString(row.name),
    version: asString(row.version),
    enabled: row.enabled === true,
    effective: row.effective === true,
    updated_at: asString(row.updated_at),
    description: asString(row.description),
    rule_count: asNumber(row.rule_count),
  }));
}

export interface Suggestion {
  target_content_id: string;
  title: string;
  content_type: string;
  platform: string;
  owner: string;
  planned_date: string;
  day_of_week: string;
  time_slot: string;
  reason: string;
  suggestion_source: string;
  reference_sample_count: number;
  confidence: string;
  limitations: string;
  invalid_reason: string;
  adopted: boolean;
  schedule_id: string;
}

/** Normalizes one schedule suggestion. */
export function normalizeSuggestionItem(payload: unknown): Suggestion {
  const source = isRecord(payload) ? payload : {};
  return {
    target_content_id: asString(source.target_content_id),
    title: asString(source.title),
    content_type: asString(source.content_type),
    platform: asString(source.platform),
    owner: asString(source.owner),
    planned_date: pureDateKey(source.planned_date),
    day_of_week: asString(source.day_of_week),
    time_slot: asString(source.time_slot),
    reason: asString(source.reason),
    suggestion_source: asString(source.suggestion_source),
    reference_sample_count: asNumber(source.reference_sample_count),
    confidence: asString(source.confidence),
    limitations: asString(source.limitations),
    invalid_reason: asString(source.invalid_reason),
    adopted: source.adopted === true,
    schedule_id: asString(source.schedule_id),
  };
}

export interface SuggestionPayload {
  id: string;
  source: string;
  source_label: string;
  analysis_status: string;
  reference_count: number;
  confidence: string;
  confidence_note: string;
  fallback_reason: string;
  recommendations: Suggestion[];
}

/** Normalizes a recommendation payload, including its suggestion list. */
export function normalizeSuggestionPayload(payload: unknown): SuggestionPayload {
  const source = isRecord(payload) ? payload : {};
  return {
    id: asString(source.id),
    source: asString(source.source),
    source_label: asString(source.source_label) || asString(source.source),
    analysis_status: asString(source.analysis_status),
    reference_count: asNumber(source.reference_count),
    confidence: asString(source.confidence),
    confidence_note: asString(source.confidence_note),
    fallback_reason: asString(source.fallback_reason),
    recommendations: asRecords(source.recommendations).map(normalizeSuggestionItem),
  };
}

/** Normalizes the recommendation history list. */
export function normalizeSuggestionHistory(payload: unknown): SuggestionPayload[] {
  return asRecords(payload).map(normalizeSuggestionPayload);
}

/**
 * Applies the matching normalizer for an API path. Unknown paths are returned
 * unchanged so unrelated endpoints keep their exact payload.
 */
export function normalizeAPIResponse(path: string, payload: unknown): unknown {
  const pathname = path.split("?")[0];
  switch (pathname) {
    case "/api/contents":
      return normalizeContentList(payload);
    case "/api/stats/overview":
      return normalizeOverview(payload);
    case "/api/stats/adoption":
      return normalizeAdoption(payload);
    case "/api/stats/funnel":
      return normalizeFunnel(payload);
    case "/api/stats/platform":
      return normalizePlatformStats(payload);
    case "/api/stats/diagnostics":
      return normalizeDiagnostics(payload);
    case "/api/schedule":
      return normalizeScheduleList(payload);
    case "/api/schedule/policy":
      return normalizeSchedulePolicy(payload);
    case "/api/schedule/adopt":
      return normalizeAdoptResult(payload);
    case "/api/rules":
      return normalizeRuleList(payload);
    case "/api/rules/logs":
      return normalizeRuleLogs(payload);
    case "/api/rules/system":
      return normalizeSystemRuleSets(payload);
    case "/api/performances":
      return normalizePerformanceList(payload);
    case "/api/stats/performances":
      return normalizePerformanceComparison(payload);
    case "/api/recommendations":
      return normalizeSuggestionHistory(payload);
    case "/api/ai/recommend":
      return normalizeSuggestionPayload(payload);
    default:
      break;
  }
  if (/^\/api\/schedule\/[^/]+$/.test(pathname)) {
    return normalizeScheduleRow(payload);
  }
  if (/^\/api\/schedule\/[^/]+\/adjustments$/.test(pathname)) {
    return normalizeScheduleAdjustments(payload);
  }
  if (/^\/api\/performances\/[^/]+$/.test(pathname)) {
    return normalizePerformanceDetail(payload);
  }
  if (/^\/api\/recommendations\/[^/]+$/.test(pathname)) {
    return normalizeSuggestionPayload(payload);
  }
  return payload;
}

export interface AdoptResult {
  created: number;
  recommendations: Suggestion[];
}

/** Normalizes the adoption response, which reports what was written. */
export function normalizeAdoptResult(payload: unknown): AdoptResult {
  const source = isRecord(payload) ? payload : {};
  return { created: asNumber(source.created), recommendations: asRecords(source.recommendations).map(normalizeSuggestionItem) };
}
