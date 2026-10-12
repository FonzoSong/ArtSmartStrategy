package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

func TestBuildRuleSuggestionsUsesPublishedMatchingSamplesOnly(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	candidates := []domain.Content{
		{ID: "unpublished-1", Title: "排练幕后", Category: "幕后记录", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone},
		{ID: "published-1", Title: "已发内容", Category: "幕后记录", Platform: "视频号", Lifecycle: domain.LifecyclePublished, WorkflowState: domain.WorkflowPublished},
	}
	samples := []PerformanceSample{{Category: "幕后记录", Platform: "视频号", PublishedAt: time.Date(2026, 10, 7, 18, 30, 0, 0, time.UTC), Views: 3000}}
	policy := SlotPolicy{HorizonDays: 3, TimeWindows: []string{"18:30"}, Weekdays: []string{"周三"}, MaxPerDay: 2}
	result := BuildRuleSuggestions(now, candidates, samples, policy)
	if len(result) != 1 {
		t.Fatalf("suggestions = %d, want 1", len(result))
	}
	if result[0].TargetContentID != "unpublished-1" || result[0].PlannedDate != "2026-10-14" || result[0].ReferenceSampleCount != 1 {
		t.Fatalf("unexpected suggestion: %#v", result[0])
	}
}

func TestBuildRuleSuggestionsReturnsEmptyForInvalidPolicy(t *testing.T) {
	result := BuildRuleSuggestions(time.Now(), nil, nil, SlotPolicy{})
	if len(result) != 0 {
		t.Fatalf("suggestions = %d, want 0", len(result))
	}
}

func TestBuildRuleSuggestionsOnlyProducesAdoptableDates(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	policy := SlotPolicy{
		HorizonDays:           14,
		TimeWindows:           []string{"12:30", "18:30", "19:30", "20:00"},
		Weekdays:              []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"},
		MaxPerDay:             2,
		MaxPerPlatformPerWeek: 2,
		ExistingDaily:         map[string]int{"2026-10-12": 2},
	}
	candidates := []domain.Content{
		{ID: "c-1", Title: "预告", Category: "演出宣传", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone},
		{ID: "c-2", Title: "幕后", Category: "幕后记录", Platform: "短视频平台A", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending},
	}
	window, ok := policy.Window(now)
	if !ok {
		t.Fatal("expected a valid window")
	}
	result := BuildRuleSuggestions(now, candidates, nil, policy)
	if len(result) != len(candidates) {
		t.Fatalf("suggestions = %d, want %d", len(result), len(candidates))
	}
	dayCounts := map[string]int{}
	weekCounts := map[string]int{}
	for date, count := range policy.ExistingDaily {
		dayCounts[date] = count
	}
	for _, item := range result {
		if item.PlannedDate < window.Start.Format("2006-01-02") || item.PlannedDate > window.End.Format("2006-01-02") {
			t.Fatalf("suggestion %s is outside the planning window: %s", item.TargetContentID, item.PlannedDate)
		}
		dayCounts[item.PlannedDate]++
		planned, err := time.Parse("2006-01-02", item.PlannedDate)
		if err != nil {
			t.Fatal(err)
		}
		year, week := planned.ISOWeek()
		key := item.Platform + ":" + strconv.Itoa(year) + ":" + strconv.Itoa(week)
		weekCounts[key]++
		// Every generated suggestion must survive the same validation the
		// adoption endpoint applies, otherwise the cycle regresses.
		if err := policy.Rules().ValidateSlot(window, item.PlannedDate, item.TimeSlot, domain.SlotCounts{SameDay: dayCounts[item.PlannedDate], SamePlatformSameISOWeek: weekCounts[key]}); err != nil {
			t.Fatalf("generated suggestion is not adoptable: %v", err)
		}
	}
}

func TestBuildRuleSuggestionsUsesTheFullHorizonIncludingToday(t *testing.T) {
	now := time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC)
	policy := SlotPolicy{
		HorizonDays:   3,
		TimeWindows:   []string{"19:30"},
		Weekdays:      []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"},
		MaxPerDay:     1,
		ExistingDaily: map[string]int{},
	}
	candidates := []domain.Content{
		{ID: "a", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone},
		{ID: "b", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone},
		{ID: "c", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone},
	}
	result := BuildRuleSuggestions(now, candidates, nil, policy)
	if len(result) != 3 {
		t.Fatalf("suggestions = %d, want 3", len(result))
	}
	dates := []string{result[0].PlannedDate, result[1].PlannedDate, result[2].PlannedDate}
	want := []string{"2026-10-12", "2026-10-13", "2026-10-14"}
	for i := range want {
		if dates[i] != want[i] {
			t.Fatalf("suggestion dates = %v, want %v (today through today + horizon - 1)", dates, want)
		}
	}
}

func TestBuildRuleSuggestionsSkipsTimeWindowsAlreadyPassedToday(t *testing.T) {
	now := time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC)
	policy := SlotPolicy{
		HorizonDays:   1,
		TimeWindows:   []string{"12:30", "18:30", "19:30", "20:00"},
		Weekdays:      []string{"周一"},
		MaxPerDay:     5,
		ExistingDaily: map[string]int{},
	}
	candidates := []domain.Content{{ID: "a", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone}}
	result := BuildRuleSuggestions(now, candidates, nil, policy)
	if len(result) != 1 {
		t.Fatalf("suggestions = %d, want 1", len(result))
	}
	if result[0].PlannedDate != "2026-10-12" || result[0].TimeSlot != "19:30" {
		t.Fatalf("unexpected suggestion: %#v", result[0])
	}
}
