package domain

import "testing"

func TestNextWorkflowStateFollowsTheSingleRecordRule(t *testing.T) {
	pipeline := map[string]string{
		WorkflowProductionPending: WorkflowProductionDone,
		WorkflowProductionDone:    WorkflowSchedulePending,
		WorkflowSchedulePending:   WorkflowScheduled,
		WorkflowScheduled:         WorkflowPublished,
	}
	for current, want := range pipeline {
		next, ok := NextWorkflowState(current)
		if !ok || next != want {
			t.Fatalf("%s must advance to %s, got %q (%v)", current, want, next, ok)
		}
		if !CanTransition(current, want) {
			t.Fatalf("%s must be able to reach %s", current, want)
		}
		if CanTransition(current, WorkflowPublished) && want != WorkflowPublished {
			t.Fatalf("%s must not jump straight to 已发布", current)
		}
	}
	if _, ok := NextWorkflowState(WorkflowPublished); ok {
		t.Fatal("已发布 is the final state and cannot advance")
	}
	for _, state := range []string{WorkflowPublished, LifecycleUnpublished, "", "未排期"} {
		if next, ok := NextWorkflowState(state); ok {
			t.Fatalf("state %q must not be advanceable, got %q", state, next)
		}
	}
}

func TestCanSuggestScheduleAcceptsOnlySchedulingReadyContent(t *testing.T) {
	cases := []struct {
		lifecycle string
		state     string
		want      bool
	}{
		{LifecycleUnpublished, WorkflowSchedulePending, true},
		{LifecycleUnpublished, WorkflowProductionDone, false},
		{LifecycleUnpublished, WorkflowProductionPending, false},
		{LifecycleUnpublished, WorkflowScheduled, false},
		{LifecyclePublished, WorkflowPublished, false},
		{LifecyclePublished, WorkflowSchedulePending, false},
	}
	for _, tc := range cases {
		if got := CanSuggestSchedule(tc.lifecycle, tc.state); got != tc.want {
			t.Fatalf("CanSuggestSchedule(%q, %q) = %v, want %v", tc.lifecycle, tc.state, got, tc.want)
		}
	}
}
