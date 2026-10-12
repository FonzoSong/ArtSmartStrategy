-- Unify the content workflow on the five canonical states
-- (待制作 → 已制作 → 待排期 → 已排期 → 已发布).
--
-- Legacy rows stored 未发布 as a workflow state, duplicating the publication
-- outcome that the lifecycle column already records. Each row is mapped from its
-- real business situation instead of a blanket rewrite:
--   * a pending schedule exists  -> 已排期 (the plan is already confirmed)
--   * already published          -> 已发布
--   * anything else              -> 待制作 (the start of the pipeline)
UPDATE content SET workflow_state = '已排期', updated_at = NOW()
WHERE workflow_state = '未发布'
  AND EXISTS (
      SELECT 1 FROM schedule s
      WHERE s.team_id = content.team_id AND s.content_id = content.id AND s.status = '待发布'
  );
UPDATE content SET workflow_state = '已发布', updated_at = NOW()
WHERE workflow_state = '未发布' AND lifecycle = '已发布';
UPDATE content SET workflow_state = '待制作', updated_at = NOW()
WHERE workflow_state = '未发布';
ALTER TABLE content DROP CONSTRAINT IF EXISTS ck_content_workflow_state_values;
ALTER TABLE content ADD CONSTRAINT ck_content_workflow_state_values
    CHECK (workflow_state IN ('待制作', '已制作', '待排期', '已排期', '已发布'));
