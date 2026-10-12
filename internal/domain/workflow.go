package domain

// The workflow is a single ordered pipeline. Each record holds exactly one
// state, and only the transitions listed here are allowed:
//
//	待制作 → 已制作 → 待排期 → 已排期 → 已发布
//
// 未发布 is not a workflow state. The content records page still offers a
// 未发布 category, but it is a derived publication view over records whose
// lifecycle is 未发布 (implemented by CategoryFilter).

// workflowNextState is the only allowed successor of each state.
var workflowNextState = map[string]string{
	WorkflowProductionPending: WorkflowProductionDone,
	WorkflowProductionDone:    WorkflowSchedulePending,
	WorkflowSchedulePending:   WorkflowScheduled,
	WorkflowScheduled:         WorkflowPublished,
}

// WorkflowStates lists the canonical states in pipeline order.
var WorkflowStates = []string{
	WorkflowProductionPending,
	WorkflowProductionDone,
	WorkflowSchedulePending,
	WorkflowScheduled,
	WorkflowPublished,
}

// NextWorkflowState returns the state a record moves to when its workflow is
// advanced. It reports false for the final state and unknown values.
func NextWorkflowState(current string) (string, bool) {
	next, ok := workflowNextState[current]
	return next, ok
}

// IsWorkflowState reports whether a value is a canonical workflow state.
func IsWorkflowState(value string) bool {
	_, ok := workflowNextState[value]
	return ok || value == WorkflowPublished
}

// CanTransition reports whether a record may move from its current state to the
// requested target state.
func CanTransition(current, target string) bool {
	next, ok := NextWorkflowState(current)
	return ok && next == target
}

// CanSuggestSchedule reports whether a content record may be used as a schedule
// suggestion candidate. Only 待排期 content is eligible: production is finished
// and the owner/performance information is complete, but nothing is scheduled.
func CanSuggestSchedule(lifecycle, workflowState string) bool {
	return lifecycle == LifecycleUnpublished && workflowState == WorkflowSchedulePending
}

// Category is one entry of the content records category bar.
type Category string

const (
	CategoryAll         Category = "全部"
	CategoryDrafting    Category = "待制作"
	CategoryProduced    Category = "已制作"
	CategoryScheduling  Category = "待排期"
	CategoryScheduled   Category = "已排期"
	CategoryUnpublished Category = "未发布"
	CategoryPublished   Category = "已发布"
)

// CategoryFilter is the database predicate behind a category selection. 待制作,
// 已制作, 待排期 and 已排期 filter the workflow state; 未发布 and 已发布 are
// derived views over the publication outcome (lifecycle), so no duplicate
// workflow state is needed.
type CategoryFilter struct {
	WorkflowState string
	Lifecycle     string
}

// CategoryFilters maps every category in the required display order.
var CategoryFilters = map[Category]CategoryFilter{
	CategoryAll:         {},
	CategoryDrafting:    {WorkflowState: WorkflowProductionPending},
	CategoryProduced:    {WorkflowState: WorkflowProductionDone},
	CategoryScheduling:  {WorkflowState: WorkflowSchedulePending},
	CategoryScheduled:   {WorkflowState: WorkflowScheduled},
	CategoryUnpublished: {Lifecycle: LifecycleUnpublished},
	CategoryPublished:   {Lifecycle: LifecyclePublished},
}

// CategoryOrder is the fixed display order of the content records categories.
func CategoryOrder() []Category {
	return []Category{CategoryAll, CategoryDrafting, CategoryProduced, CategoryScheduling, CategoryScheduled, CategoryUnpublished, CategoryPublished}
}

// ResolveCategory returns the filter for a category name.
func ResolveCategory(value string) (CategoryFilter, bool) {
	filter, ok := CategoryFilters[Category(value)]
	return filter, ok
}
