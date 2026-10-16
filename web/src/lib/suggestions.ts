/**
 * Schedule-suggestion rules shared by the suggestion page.
 *
 * Candidate eligibility comes from the backend workflow aggregation. The client
 * uses that count for an empty state and leaves schedule-rule validation to the
 * server when a recommendation is applied.
 */

/** Required empty-state copy when no scheduling candidate can be suggested. */
export const NO_SCHEDULE_CANDIDATE_TEXT = "当前无待排期内容";

/**
 * Build the explicit request sent by the AI scheduling action. The application
 * is team-scoped (it has no separate project entity), so projectId carries the
 * active team scope and the server verifies it against the authenticated user.
 */
export function buildScheduleRecommendationRequest({ teamId, performanceId = "", contentIds = [] }: {
  teamId: number | string;
  performanceId?: string;
  contentIds?: Array<string | number>;
}): {
  projectId: number;
  contentIds: string[];
  strategy: string;
  constraints: Record<string, never>;
  performanceId?: string;
} {
  const request: {
    projectId: number;
    contentIds: string[];
    strategy: string;
    constraints: Record<string, never>;
    performanceId?: string;
  } = {
    projectId: Number(teamId),
    contentIds: [...new Set(contentIds.map(String).filter(Boolean))],
    strategy: "balanced",
    constraints: {},
  };
  if (performanceId) request.performanceId = String(performanceId);
  return request;
}

/**
 * Decides whether the suggestion page should show its no-candidates state. The
 * candidate count is supplied by the backend's workflow aggregation, not
 * re-derived from client-side lifecycle rules.
 */
export function showNoScheduleCandidateEmptyState(analysisStatus: unknown, readyCandidateCount: number): boolean {
  return analysisStatus === "no_candidates" || readyCandidateCount <= 0;
}
