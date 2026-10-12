import assert from "node:assert/strict";
import test from "node:test";

import { WORKFLOW, WORKFLOW_STATES } from "../src/components/workflow.ts";
import { NO_FINISHED_VIDEO_TEXT, isReadySuggestionCandidate, showNoFinishedVideoEmptyState } from "../src/lib/suggestions.ts";

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

test("only unpublished finished content is a suggestion candidate", () => {
  assert.equal(isReadySuggestionCandidate("未发布", "已制作"), true);
  assert.equal(isReadySuggestionCandidate("未发布", "待制作"), false);
  assert.equal(isReadySuggestionCandidate("未发布", "待排期"), false);
  assert.equal(isReadySuggestionCandidate("未发布", "已排期"), false);
  assert.equal(isReadySuggestionCandidate("未发布", "未发布"), false);
  assert.equal(isReadySuggestionCandidate("已发布", "已发布"), false);
  assert.equal(isReadySuggestionCandidate(null, null), false);
  assert.equal(isReadySuggestionCandidate("已发布", "已制作"), false);
});

test("the no-finished-video empty state only applies without candidates", () => {
  assert.equal(showNoFinishedVideoEmptyState("no_candidates", 3), true, "the backend already knows there is no candidate");
  assert.equal(showNoFinishedVideoEmptyState("rule_fallback", 0), true, "no finished video in the team");
  assert.equal(showNoFinishedVideoEmptyState("rule_fallback", 2), false, "a finished video exists, so keep the normal empty text");
  assert.equal(showNoFinishedVideoEmptyState("ai_completed", 1), false);
  assert.equal(NO_FINISHED_VIDEO_TEXT, "当前无制作完毕视频");
});
