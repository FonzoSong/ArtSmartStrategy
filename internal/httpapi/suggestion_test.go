package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/arts-smart/strategy/internal/service"
)

func testWindow(t *testing.T) domain.PlanningWindow {
	t.Helper()
	window, ok := domain.HorizonWindow(time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC), 14)
	if !ok {
		t.Fatal("expected a valid window")
	}
	return window
}

func TestSuggestionBlockReasonAcceptsOnlyPlannableTargets(t *testing.T) {
	window := testWindow(t)
	rules := domain.PlanningRules{HorizonDays: 14, TimeWindows: []string{"19:30"}, Weekdays: []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}, MaxPerDay: 2, MaxPerPlatformPerWeek: 2}
	ready := service.Suggestion{TargetContentID: "ready", PlannedDate: "2026-10-12", TimeSlot: "19:30"}
	targets := map[string]suggestionTarget{
		"ready":     {Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending, Platform: "视频号"},
		"drafting":  {Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionPending, Platform: "视频号"},
		"produced":  {Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone, Platform: "视频号"},
		"scheduled": {Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowScheduled, Platform: "视频号"},
		"published": {Lifecycle: domain.LifecyclePublished, WorkflowState: domain.WorkflowPublished, Platform: "视频号"},
	}

	if reason, _, platform := suggestionBlockReason(ready, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason != "" || platform != "视频号" {
		t.Fatalf("ready target must be adoptable, got %q (%s)", reason, platform)
	}

	published := ready
	published.TargetContentID = "published"
	if reason, _, _ := suggestionBlockReason(published, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason != "目标内容已发布，无需排期" {
		t.Fatalf("published target reason = %q", reason)
	}

	drafting := ready
	drafting.TargetContentID = "drafting"
	if reason, _, _ := suggestionBlockReason(drafting, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason == "" {
		t.Fatal("待制作 target must not be an adoptable suggestion candidate")
	}
	if reason, _, _ := suggestionBlockReason(drafting, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); !strings.Contains(reason, "待排期") {
		t.Fatalf("unfinished target reason = %q", reason)
	}
	for _, state := range []string{"produced", "scheduled"} {
		other := ready
		other.TargetContentID = state
		if reason, _, _ := suggestionBlockReason(other, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason == "" {
			t.Fatalf("%s target must not be an adoptable suggestion candidate", state)
		}
	}

	missing := ready
	missing.TargetContentID = "gone"
	if reason, _, _ := suggestionBlockReason(missing, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason != "目标内容不存在或不属于当前团队" {
		t.Fatalf("missing target reason = %q", reason)
	}

	if reason, _, _ := suggestionBlockReason(ready, rules, window, targets, map[string]bool{"ready": true}, map[string]int{}, map[string]int{}); reason != "该内容已有待发布排期" {
		t.Fatalf("already scheduled reason = %q", reason)
	}

	outside := ready
	outside.PlannedDate = "2026-10-26"
	if reason, _, _ := suggestionBlockReason(outside, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason != domain.ErrSlotWindow.Error() {
		t.Fatalf("out-of-window reason = %q", reason)
	}

	badSlot := ready
	badSlot.TimeSlot = "08:00"
	if reason, _, _ := suggestionBlockReason(badSlot, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason != domain.ErrSlotTimeWindow.Error() {
		t.Fatalf("time window reason = %q", reason)
	}

	invalidDate := ready
	invalidDate.PlannedDate = "not-a-date"
	if reason, _, _ := suggestionBlockReason(invalidDate, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{}); reason != "计划日期格式无效" {
		t.Fatalf("invalid date reason = %q", reason)
	}
}

func TestSuggestionBlockReasonHonoursFrequencyLimits(t *testing.T) {
	window := testWindow(t)
	rules := domain.PlanningRules{HorizonDays: 14, TimeWindows: []string{"19:30"}, Weekdays: []string{"周一"}, MaxPerDay: 1, MaxPerPlatformPerWeek: 1}
	item := service.Suggestion{TargetContentID: "ready", PlannedDate: "2026-10-12", TimeSlot: "19:30"}
	targets := map[string]suggestionTarget{"ready": {Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending, Platform: "视频号"}}

	reason, _, _ := suggestionBlockReason(item, rules, window, targets, map[string]bool{}, map[string]int{"2026-10-12": 1}, map[string]int{})
	if reason != domain.ErrSlotDailyLimit.Error() {
		t.Fatalf("daily limit reason = %q", reason)
	}
	reason, _, _ = suggestionBlockReason(item, rules, window, targets, map[string]bool{}, map[string]int{}, map[string]int{"视频号:2026:42": 1})
	if reason != domain.ErrSlotWeeklyLimit.Error() {
		t.Fatalf("weekly limit reason = %q", reason)
	}
}

func TestConsumeSlotTracksDailyAndWeeklyLoad(t *testing.T) {
	dayCounts := map[string]int{}
	weekCounts := map[string]int{}
	consumeSlot("2026-10-12", "视频号", dayCounts, weekCounts)
	consumeSlot("2026-10-12", "视频号", dayCounts, weekCounts)
	if dayCounts["2026-10-12"] != 2 {
		t.Fatalf("day counts = %v", dayCounts)
	}
	if len(weekCounts) != 1 {
		t.Fatalf("week counts = %v", weekCounts)
	}
	for key, count := range weekCounts {
		if count != 2 {
			t.Fatalf("week count for %s = %d", key, count)
		}
	}
}
