/**
 * Schedule-suggestion rules shared by the suggestion page.
 *
 * These mirror the server-side rule in `internal/domain/workflow.go`: only
 * unpublished content that finished production can be suggested. The client uses
 * them for display decisions only; the server always re-validates.
 */

/** Required empty-state copy when no finished video can be suggested. */
export const NO_FINISHED_VIDEO_TEXT = "当前无制作完毕视频";

const LIFECYCLE_UNPUBLISHED = "未发布";
const WORKFLOW_PRODUCTION_DONE = "已制作";

/** Reports whether a content record may enter the suggestion candidate set. */
export function isReadySuggestionCandidate(lifecycle: unknown, workflowState: unknown): boolean {
  return lifecycle === LIFECYCLE_UNPUBLISHED && workflowState === WORKFLOW_PRODUCTION_DONE;
}

/**
 * Decides whether the suggestion page must show the "no finished video" empty
 * state: either the backend reports no candidate, or the current team has no
 * unpublished finished content at all. It is never shown while suggestions exist.
 */
export function showNoFinishedVideoEmptyState(analysisStatus: unknown, readyCandidateCount: number): boolean {
  return analysisStatus === "no_candidates" || readyCandidateCount <= 0;
}
