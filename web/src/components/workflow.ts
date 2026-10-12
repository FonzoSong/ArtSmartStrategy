export const WORKFLOW = Object.freeze({
  productionPending: "待制作",
  productionComplete: "已制作",
  schedulingPending: "待排期",
  scheduled: "已排期",
  unpublished: "未发布",
  published: "已发布",
});

// Display order used by the content records filter tabs: 全部 is added by the
// page itself, then these states in this exact order.
export const WORKFLOW_STATES = [
  WORKFLOW.productionPending,
  WORKFLOW.productionComplete,
  WORKFLOW.schedulingPending,
  WORKFLOW.scheduled,
  WORKFLOW.unpublished,
  WORKFLOW.published,
];

export const WORKFLOW_COLORS: Record<string, string> = {
  [WORKFLOW.productionPending]: "workflow-pending",
  [WORKFLOW.productionComplete]: "workflow-ready",
  [WORKFLOW.schedulingPending]: "workflow-pending",
  [WORKFLOW.scheduled]: "workflow-scheduled",
  [WORKFLOW.published]: "workflow-published",
  [WORKFLOW.unpublished]: "workflow-unpublished",
};
