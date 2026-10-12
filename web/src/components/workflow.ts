export const WORKFLOW = Object.freeze({
  productionPending: "待制作",
  productionComplete: "已制作",
  schedulingPending: "待排期",
  scheduled: "已排期",
  published: "已发布",
  unpublished: "未发布",
});

export const WORKFLOW_STATES = Object.values(WORKFLOW);

export const WORKFLOW_COLORS: Record<string, string> = {
  [WORKFLOW.productionPending]: "workflow-pending",
  [WORKFLOW.productionComplete]: "workflow-ready",
  [WORKFLOW.schedulingPending]: "workflow-pending",
  [WORKFLOW.scheduled]: "workflow-scheduled",
  [WORKFLOW.published]: "workflow-published",
  [WORKFLOW.unpublished]: "workflow-unpublished",
};
