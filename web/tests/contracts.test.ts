import assert from "node:assert/strict";
import test from "node:test";

import {
  normalizeAPIResponse,
  normalizeAdoption,
  normalizeDiagnostics,
  normalizeOverview,
  normalizeRuleList,
  normalizeRuleLogs,
  normalizeScheduleList,
  normalizeSchedulePolicy,
  normalizeSuggestionPayload,
  normalizeSystemRuleSets,
} from "../src/lib/contracts.ts";
import { formatPercent } from "../src/lib/format.ts";

/** Fields the operations overview page reads directly. */
const overviewFields = [
  "published_count",
  "total_posts",
  "total_views",
  "total_interactions",
  "platform_count",
  "avg_completion",
  "sample_count",
  "top_contents",
] as const;

test("overview payload always fills the fields the page renders", () => {
  const empty = normalizeOverview(undefined);
  for (const field of overviewFields) {
    assert.ok(field in empty, `${field} must always be present`);
  }
  assert.deepEqual(empty.top_contents, []);
  assert.equal(empty.total_posts, 0);
  assert.equal(empty.total_views, 0);
  assert.equal(empty.platform_count, 0);
  assert.equal(empty.avg_completion, null);
  assert.equal(empty.sample_count, 0);
});

test("overview normalization keeps numbers, strings and missing fields safe", () => {
  const payload = normalizeOverview({
    published_count: 7,
    unpublished_count: 2,
    total_views: "1500",
    total_interactions: 320.4,
    platform_count: 3,
    avg_completion: "41.5",
    sample_count: 7,
    top_contents: [
      { id: "a", title: "标题", category: "演出宣传", platform: "视频号", score: 42.126, published_at: "2026-10-10T12:00:00Z" },
      null,
      "not-a-row",
    ],
    weights: { completion: 0.7, interaction: 0.3 },
  });
  assert.equal(payload.total_views, 1500);
  assert.equal(payload.total_interactions, 320.4);
  assert.equal(payload.avg_completion, 41.5);
  assert.equal(payload.total_posts, 7);
  assert.equal(payload.weights.completion, 0.7);
  assert.equal(payload.top_contents.length, 1);
  assert.equal(payload.top_contents[0].title, "标题");
  assert.equal(payload.top_contents[0].score, 42.126);
});

test("overview without a ranking never yields undefined lists", () => {
  const payload = normalizeOverview({ top_contents: "unexpected" });
  assert.ok(Array.isArray(payload.top_contents));
  assert.equal(payload.top_contents.length, 0);
});

test("adoption and diagnostics keep their empty-team shape", () => {
  const adoption = normalizeAdoption(undefined);
  assert.equal(adoption.adopted.avg_completion, null);
  assert.equal(adoption.adopted.sample_count, 0);
  assert.equal(adoption.not_adopted.sample_count, 0);
  assert.equal(adoption.completion_difference, null);
  assert.equal(formatPercent(adoption.adopted.avg_completion), "—");

  const diagnostics = normalizeDiagnostics({ items: [{ level: "good", message: "一切正常" }, { level: "warn" }, null] });
  assert.equal(diagnostics.items.length, 1);
  assert.deepEqual(diagnostics.items[0], { level: "good", message: "一切正常" });
  assert.deepEqual(normalizeDiagnostics(undefined).items, []);
});

test("schedule rows keep planned_date a pure calendar date", () => {
  const rows = normalizeScheduleList([
    { id: "s1", content_id: "c1", planned_date: "2026-10-12T00:00:00Z", planned_time: "19:30", status: "待发布", title: "标题", platform: "视频号", priority: "high", priority_order: 2, risk_notes: "时间冲突" },
    { id: "s2", planned_date: null },
  ]);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].planned_date, "2026-10-12");
  assert.equal(rows[0].priority, "high");
  assert.equal(rows[0].priority_order, 2);
  assert.equal(rows[0].risk_notes, "时间冲突");
  assert.equal(rows[1].planned_date, "");
  assert.equal(rows[1].priority, "medium");
  assert.equal(rows[1].title, "");
  assert.equal(rows[1].status, "");
  assert.deepEqual(normalizeScheduleList({ items: [] }), []);
});

test("schedule policy normalization exposes the planning window", () => {
  const policy = normalizeSchedulePolicy({
    planning_horizon_days: 14,
    horizon_definition: "从今天起共 14 个自然日",
    time_windows: ["12:30", null, "19:30"],
    weekdays: ["周一"],
    max_per_day: 2,
    max_per_platform_per_week: "2",
    window_start: "2026-10-12T00:00:00Z",
    window_end: "2026-10-25",
  });
  assert.equal(policy.planning_horizon_days, 14);
  assert.deepEqual(policy.time_windows, ["12:30", "19:30"]);
  assert.equal(policy.max_per_platform_per_week, 2);
  assert.equal(policy.window_start, "2026-10-12");
  assert.equal(policy.window_end, "2026-10-25");
  assert.equal(normalizeSchedulePolicy(undefined).window_end, "");
});

test("rule payloads tolerate missing columns and null timestamps", () => {
  const rules = normalizeRuleList([
    { id: 3, name: "必填", priority: 10, enabled: true, rule_type: "required", rule_content: "{}", updated_at: null, last_result: "no_match", last_executed_at: "2026-10-11T08:00:00Z" },
    { id: "4", name: null },
  ]);
  assert.equal(rules.length, 2);
  assert.equal(rules[0].updated_at, "");
  assert.equal(rules[0].enabled, true);
  assert.equal(rules[0].last_executed_at, "2026-10-11T08:00:00Z");
  assert.equal(rules[1].id, 4);
  assert.equal(rules[1].enabled, false);
  assert.equal(rules[1].name, "");
  assert.deepEqual(normalizeRuleList(null), []);

  const logs = normalizeRuleLogs([{ rule_name: "必填", result: "matched", detail: "命中", created_at: null }]);
  assert.equal(logs[0].created_at, "");
  assert.equal(logs[0].result, "matched");

  const sets = normalizeSystemRuleSets({ items: [{ id: 1, name: "规则集", version: "1.1", enabled: true, effective: true, rule_count: 5, updated_at: "2026-10-12T00:00:00Z" }, {}] });
  assert.equal(sets.length, 2);
  assert.equal(sets[0].effective, true);
  assert.equal(sets[0].rule_count, 5);
  assert.equal(sets[1].effective, false);
  assert.equal(sets[1].version, "");
  assert.deepEqual(normalizeSystemRuleSets(undefined), []);
});

test("suggestion payloads carry validity, adoption state and pure dates", () => {
  const payload = normalizeSuggestionPayload({
    id: "rec-1",
    source_label: "本地规则引擎",
    reference_count: 4,
    strategy: "balanced",
    model: "rules",
    summary: "为 1 条内容生成建议。",
    conflicts: [{ type: "insufficient_samples", severity: "warning", content_id: "c1", message: "样本有限" }],
    recommendations: [
      { target_content_id: "c1", title: "标题", planned_date: "2026-10-12T00:00:00Z", time_slot: "19:30", adopted: true, schedule_id: "s1", reference_sample_count: 2 },
      { target_content_id: "c2", planned_date: "bad", invalid_reason: "计划日期超出规则集排期周期", priority: "high", priority_order: 1, confidence_score: 0.83, tags: ["演出"], risk_notes: "窗口冲突", previous_schedule: { id: "s0", recommendation_id: "rec-old", planned_date: "2026-10-11", priority: "low", priority_order: 4 } },
    ],
  });
  assert.equal(payload.recommendations.length, 2);
  assert.equal(payload.recommendations[0].planned_date, "2026-10-12");
  assert.equal(payload.recommendations[0].adopted, true);
  assert.equal(payload.recommendations[0].schedule_id, "s1");
  assert.equal(payload.recommendations[1].planned_date, "");
  assert.equal(payload.recommendations[1].invalid_reason, "计划日期超出规则集排期周期");
  assert.equal(payload.recommendations[1].adopted, false);
  assert.equal(payload.recommendations[1].priority, "high");
  assert.equal(payload.recommendations[1].priority_order, 1);
  assert.equal(payload.recommendations[1].confidence_score, 0.83);
  assert.deepEqual(payload.recommendations[1].tags, ["演出"]);
  assert.equal(payload.recommendations[1].previous_schedule?.priority, "low");
  assert.equal(payload.recommendations[1].previous_schedule?.planned_date, "2026-10-11");
  assert.equal(payload.strategy, "balanced");
  assert.equal(payload.model, "rules");
  assert.equal(payload.summary, "为 1 条内容生成建议。");
  assert.equal(payload.conflicts[0].type, "insufficient_samples");
  assert.equal(payload.conflicts[0].content_id, "c1");
  assert.deepEqual(normalizeSuggestionPayload(undefined).recommendations, []);
});

test("response normalization is dispatched per endpoint", () => {
  const overview = normalizeAPIResponse("/api/stats/overview?team=1", { total_views: 5 });
  assert.equal((overview as { total_views: number }).total_views, 5);
  assert.deepEqual((overview as { top_contents: unknown[] }).top_contents, []);
  const detail = normalizeAPIResponse("/api/ai/recommend/rec-1", { id: "rec-1", strategy: "balanced", conflicts: [] });
  assert.equal((detail as { strategy: string }).strategy, "balanced");
  const applied = normalizeAPIResponse("/api/ai/recommend/rec-1/apply", { created: 2, adjusted: 1, recommendations: [] });
  assert.equal((applied as { created: number }).created, 2);
  assert.equal((applied as { adjusted: number }).adjusted, 1);

  const schedule = normalizeAPIResponse("/api/schedule", [{ planned_date: "2026-10-12T00:00:00Z" }]);
  assert.equal((schedule as { planned_date: string }[])[0].planned_date, "2026-10-12");

  const single = normalizeAPIResponse("/api/schedule/abc", { planned_date: "2026-11-01T00:00:00Z", status: "待发布" });
  assert.equal((single as { planned_date: string }).planned_date, "2026-11-01");

  const adopt = normalizeAPIResponse("/api/schedule/adopt", { created: "2", adjusted: "1", recommendations: [{ target_content_id: "c1", adopted: true }] });
  assert.equal((adopt as { created: number }).created, 2);
  assert.equal((adopt as { adjusted: number }).adjusted, 1);
  assert.equal((adopt as { recommendations: { adopted: boolean }[] }).recommendations[0].adopted, true);

  const untouched = { anything: true };
  assert.equal(normalizeAPIResponse("/api/settings", untouched), untouched);
});
