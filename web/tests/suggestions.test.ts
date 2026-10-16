import assert from "node:assert/strict";
import test from "node:test";

import { WORKFLOW, WORKFLOW_STATES } from "../src/components/workflow.ts";
import { buildScheduleRecommendationRequest, NO_SCHEDULE_CANDIDATE_TEXT, showNoScheduleCandidateEmptyState } from "../src/lib/suggestions.ts";

test("content record tabs keep the required category order", () => {
  assert.deepEqual(WORKFLOW_STATES, ["待制作", "已制作", "待排期", "已排期", "未发布", "已发布"]);
  assert.equal(WORKFLOW_STATES.indexOf("未发布") < WORKFLOW_STATES.indexOf("已发布"), true, "未发布 must come before 已发布");
  assert.equal(WORKFLOW_STATES.length, 6, "the page adds 全部 itself, so six states remain");
  assert.equal(new Set(WORKFLOW_STATES).size, WORKFLOW_STATES.length, "each state appears once");
});

test("every workflow state label still matches the domain values", () => {
  assert.deepEqual(WORKFLOW_STATES, [
    WORKFLOW.productionPending,
    WORKFLOW.productionComplete,
    WORKFLOW.schedulingPending,
    WORKFLOW.scheduled,
    WORKFLOW.unpublished,
    WORKFLOW.published,
  ]);
});

test("the no-schedule-candidate empty state only applies without candidates", () => {
  assert.equal(showNoScheduleCandidateEmptyState("no_candidates", 3), true, "the backend already knows there is no candidate");
  assert.equal(showNoScheduleCandidateEmptyState("rule_fallback", 0), true, "no eligible scheduling content in the team");
  assert.equal(showNoScheduleCandidateEmptyState("rule_fallback", 2), false, "eligible content exists, so keep the normal empty text");
  assert.equal(showNoScheduleCandidateEmptyState("ai_completed", 1), false);
  assert.equal(NO_SCHEDULE_CANDIDATE_TEXT, "当前无待排期内容");
});

test("AI scheduling requests include the authenticated project scope and active filter", () => {
  assert.deepEqual(buildScheduleRecommendationRequest({ teamId: "12", performanceId: "performance-4" }), {
    projectId: 12,
    contentIds: [],
    strategy: "balanced",
    constraints: {},
    performanceId: "performance-4",
  });
});

test("AI scheduling requests normalize explicit content IDs without duplicates", () => {
  assert.deepEqual(buildScheduleRecommendationRequest({ teamId: 12, contentIds: ["a", "a", 7, ""] }), {
    projectId: 12,
    contentIds: ["a", "7"],
    strategy: "balanced",
    constraints: {},
  });
});
