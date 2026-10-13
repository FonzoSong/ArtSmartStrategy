package service

import (
	"strings"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

func planPolicy() SlotPolicy {
	return SlotPolicy{
		HorizonDays:            14,
		TimeWindows:            []string{"12:30", "19:30"},
		Weekdays:               []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"},
		MaxPerDay:              2,
		MaxPerPlatformPerWeek:  2,
		ExistingDaily:          map[string]int{},
		ExistingPlatformWeekly: map[string]int{},
	}
}

func planCandidates() []domain.Content {
	return []domain.Content{
		{ID: "c-promo", Title: "开票提醒", Category: "演出宣传", Platform: "视频号", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending},
		{ID: "c-back", Title: "后台纪实", Category: "幕后记录", Platform: "短视频平台A", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending},
		{ID: "c-skill", Title: "水袖训练", Category: "技艺展示", Platform: "短视频平台A", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending},
		{ID: "c-sci", Title: "行当科普", Category: "艺术科普", Platform: "内容社区C", Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending},
	}
}

// planSamples builds a small same-team history: 幕后记录 performs best on 视频号
// in the Wednesday 19:30 window.
func planSamples() []PerformanceSample {
	base := time.Date(2026, 9, 2, 19, 30, 0, 0, time.Local) // a Wednesday
	samples := []PerformanceSample{}
	for i := 0; i < 4; i++ {
		samples = append(samples, PerformanceSample{
			Category: "幕后记录", Platform: "视频号",
			PublishedAt: base.AddDate(0, 0, 7*i),
			Views:       9000, Likes: 400, Comments: 90, Saves: 60, Shares: 50, Completion: 62,
		})
	}
	samples = append(samples, PerformanceSample{
		Category: "技艺展示", Platform: "短视频平台A",
		PublishedAt: base, Views: 3000, Likes: 120, Comments: 20, Saves: 10, Shares: 5, Completion: 41,
	})
	return samples
}

func TestBuildPerformancePlanStaysBeforeShowAndSpreadsAcrossDays(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	show := time.Date(2026, 10, 21, 19, 30, 0, 0, time.Local)
	plan, insufficient := BuildPerformancePlan(PerformancePlanInput{
		Now: now, ShowTitle: "春季折子戏专场", StartsAt: show,
		Candidates: planCandidates(), Samples: planSamples(), Policy: planPolicy(),
	})
	if len(plan) != 4 {
		t.Fatalf("expected every candidate to be planned, got %d entries", len(plan))
	}
	if insufficient {
		t.Fatal("a 5-sample history must not be reported as insufficient")
	}
	dates := map[string]int{}
	for _, item := range plan {
		planned, err := time.Parse("2006-01-02", item.PlannedDate)
		if err != nil {
			t.Fatalf("planned date %q is not a pure date", item.PlannedDate)
		}
		if planned.After(time.Date(show.Year(), show.Month(), show.Day(), 0, 0, 0, 0, show.Location())) {
			t.Fatalf("plan %s lands after the show (%s)", item.PlannedDate, item.Title)
		}
		if planned.Equal(time.Date(show.Year(), show.Month(), show.Day(), 0, 0, 0, 0, show.Location())) && item.TimeSlot > "19:30" {
			t.Fatalf("same-day plan %s is scheduled after the curtain", item.TimeSlot)
		}
		if item.Reason == "" || !strings.Contains(item.Reason, "距开演") {
			t.Fatalf("reason must state the distance to the show, got %q", item.Reason)
		}
		if !domain.ContainsString(planPolicy().TimeWindows, item.TimeSlot) {
			t.Fatalf("time slot %q is not an allowed window", item.TimeSlot)
		}
		dates[item.PlannedDate]++
	}
	if len(dates) < 2 {
		t.Fatalf("plan piles every item on one day: %#v", dates)
	}
}

func TestBuildPerformancePlanExplainsHistoryAndKeepsPromoClosestToShow(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	show := time.Date(2026, 10, 21, 19, 30, 0, 0, time.Local)
	plan, _ := BuildPerformancePlan(PerformancePlanInput{
		Now: now, ShowTitle: "春季折子戏专场", StartsAt: show,
		Candidates: planCandidates(), Samples: planSamples(), Policy: planPolicy(),
	})
	var promoDate, earliestOther string
	for _, item := range plan {
		if item.ContentType == "幕后记录" {
			if !strings.Contains(item.Reason, "视频号") {
				t.Fatalf("expected the history-based platform to be explained, got %q", item.Reason)
			}
			if !strings.Contains(item.Platform, "视频号") {
				t.Fatalf("expected 视频号 for 幕后记录, got %s", item.Platform)
			}
		}
		if item.ContentType == "演出宣传" {
			promoDate = item.PlannedDate
			continue
		}
		if earliestOther == "" || item.PlannedDate < earliestOther {
			earliestOther = item.PlannedDate
		}
	}
	if promoDate == "" || promoDate < earliestOther {
		t.Fatalf("promotional content should sit closest to the show (promo=%s, earliest=%s)", promoDate, earliestOther)
	}
}

func TestBuildPerformancePlanStatesInsufficientSamples(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	show := time.Date(2026, 10, 20, 19, 30, 0, 0, time.Local)
	plan, insufficient := BuildPerformancePlan(PerformancePlanInput{
		Now: now, ShowTitle: "无历史演出", StartsAt: show,
		Candidates: planCandidates(), Samples: []PerformanceSample{}, Policy: planPolicy(),
	})
	if !insufficient {
		t.Fatal("an empty history must be reported as insufficient samples")
	}
	if len(plan) == 0 {
		t.Fatal("the plan must still be usable when history is missing")
	}
	for _, item := range plan {
		if !strings.Contains(item.Reason, "样本不足") {
			t.Fatalf("reason must admit the missing samples, got %q", item.Reason)
		}
		if !strings.Contains(item.Limitations, "样本") {
			t.Fatalf("limitations must mention the sample size, got %q", item.Limitations)
		}
	}
}

func TestBuildPerformancePlanIsDeterministic(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	show := time.Date(2026, 10, 24, 19, 30, 0, 0, time.Local)
	input := PerformancePlanInput{Now: now, ShowTitle: "s", StartsAt: show, Candidates: planCandidates(), Samples: planSamples(), Policy: planPolicy()}
	first, _ := BuildPerformancePlan(input)
	second, _ := BuildPerformancePlan(input)
	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].PlannedDate != second[i].PlannedDate || first[i].TimeSlot != second[i].TimeSlot || first[i].Platform != second[i].Platform {
			t.Fatalf("item %d is not deterministic: %#v vs %#v", i, first[i], second[i])
		}
	}
}
