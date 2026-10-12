package service

import (
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
