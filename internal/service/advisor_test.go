package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

func TestBuildRuleSuggestionsUsesPublishedMatchingSamplesOnly(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	candidates := []domain.Content{
		{ID: "unpublished-1", Title: "排练幕后", Category: "幕后记录", Platform: "视频号", Tags: []string{"舞台幕后"}, Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowProductionDone},
		{ID: "published-1", Title: "已发内容", Category: "幕后记录", Platform: "视频号", Lifecycle: domain.LifecyclePublished, WorkflowState: domain.WorkflowPublished},
	}
	samples := []PerformanceSample{{Category: "幕后记录", Platform: "视频号", Tags: []string{"舞台幕后"}, PublishedAt: time.Date(2026, 10, 7, 18, 30, 0, 0, time.UTC), Views: 3000}}
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

func TestTagMatchedSamplesOnlyUsesSharedTags(t *testing.T) {
	candidate := domain.Content{ID: "candidate", Tags: []string{"舞台", "演出宣传"}}
	samples := []PerformanceSample{
		{ID: "match", Tags: []string{"演出宣传"}, Views: 1000, Likes: 20},
		{ID: "case-match", Tags: []string{"舞台"}, Views: 2000},
		{ID: "unrelated", Tags: []string{"服装"}, Views: 999999},
		{ID: "untagged", Views: 888888},
	}
	matched := TagMatchedSamples(candidate, samples)
	if len(matched) != 2 || matched[0].ID != "match" || matched[1].ID != "case-match" {
		t.Fatalf("matched samples = %#v", matched)
	}
	if got := TagMatchedSamples(domain.Content{ID: "no-tags"}, samples); len(got) != 0 {
		t.Fatalf("untagged candidate borrowed unrelated history: %#v", got)
	}
}

func TestValidateAIRecommendationsRequiresStructuredFieldsAndRespectsPolicy(t *testing.T) {
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	candidate := domain.Content{ID: "candidate", Title: "预热", Category: "演出宣传", Platform: "视频号", Owner: "林", Tags: []string{"开演"}, Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending}
	samples := []PerformanceSample{{ID: "same-tag", Tags: []string{"开演"}, Views: 1000, Likes: 80, Comments: 10, Saves: 5, Shares: 4, Completion: 68}}
	policy := SlotPolicy{HorizonDays: 3, TimeWindows: []string{"19:30"}, Weekdays: []string{"周一", "周二", "周三"}, MaxPerDay: 1, MaxPerPlatformPerWeek: 3}
	confidence := .8
	valid := []StructuredAIRecommendation{{TargetContentID: "candidate", PlannedDate: "2026-10-12", TimeSlot: "19:30", Priority: "high", PriorityOrder: 1, Reason: "共享 Tag 样本在该发布时间表现稳定", Confidence: &confidence, RiskNotes: "样本只有一条，结果可能波动"}}
	items, err := ValidateAIRecommendations(now, valid, []domain.Content{candidate}, samples, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Priority != "high" || items[0].Confidence != "高" || items[0].ReferenceSampleCount != 1 || items[0].ConfidenceScore != .8 || items[0].Tags[0] != "开演" {
		t.Fatalf("unexpected normalized recommendation: %#v", items)
	}

	bad := valid[0]
	bad.Priority = "urgent"
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{bad}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("invalid priority was accepted")
	}
	bad = valid[0]
	bad.Confidence = nil
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{bad}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("missing confidence was accepted")
	}
	bad = valid[0]
	bad.TargetContentID = "outside-team"
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{bad}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("out-of-scope content ID was accepted")
	}
	bad = valid[0]
	bad.PlannedDate = "2026-10-20"
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{bad}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("out-of-window date was accepted")
	}
	bad = valid[0]
	bad.RiskNotes = ""
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{bad}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("missing risk notes were accepted")
	}
	bad = valid[0]
	bad.PriorityOrder = 0
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{bad}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("missing recommendation order was accepted")
	}
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{valid[0], valid[0]}, []domain.Content{candidate}, samples, policy); err == nil {
		t.Fatal("duplicate content recommendations were accepted")
	}

	secondCandidate := candidate
	secondCandidate.ID = "candidate-2"
	second := valid[0]
	second.TargetContentID = secondCandidate.ID
	capacityPolicy := policy
	capacityPolicy.MaxPerDay = 2
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{valid[0], second}, []domain.Content{candidate, secondCandidate}, samples, capacityPolicy); err == nil {
		t.Fatal("duplicate priority order was accepted")
	}

	capacityPolicy.MaxPerDay = 1
	if _, err = ValidateAIRecommendations(now, []StructuredAIRecommendation{valid[0], second}, []domain.Content{candidate, secondCandidate}, samples, capacityPolicy); err == nil {
		t.Fatal("daily schedule capacity was exceeded")
	}
}

func TestGenerateAIRecommendationsBuildsTagScopedPromptAndParsesStructuredJSON(t *testing.T) {
	plannedDate := time.Now().Format("2006-01-02")
	candidate := domain.Content{ID: "candidate", Title: "预热", Category: "演出宣传", Platform: "视频号", Tags: []string{"开演"}, Lifecycle: domain.LifecycleUnpublished, WorkflowState: domain.WorkflowSchedulePending}
	samples := []PerformanceSample{
		{ID: "match", Title: "开演预告", Tags: []string{"开演"}, Views: 5000, Likes: 120, Comments: 10, Saves: 20, Shares: 5, Completion: 70, ProfileVisits: 30, Follows: 8, Attendance: 2, Tickets: 1},
		{ID: "unrelated", Title: "服装记录", Tags: []string{"服装"}, Views: 999999},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			ResponseFormat map[string]string `json:"response_format"`
			Messages       []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request.ResponseFormat["type"] != "json_object" {
			t.Errorf("response format = %#v", request.ResponseFormat)
		}
		if len(request.Messages) != 2 {
			t.Errorf("message count=%d", len(request.Messages))
			return
		}
		var prompt struct {
			TagMatchedHistory []struct {
				TargetContentID  string              `json:"target_content_id"`
				PublishedHistory []PerformanceSample `json:"published_history"`
				MetricAggregates map[string]struct {
					SampleSize int     `json:"sample_size"`
					Mean       float64 `json:"mean"`
					Median     float64 `json:"median"`
				} `json:"metric_aggregates"`
			} `json:"tag_matched_history"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &prompt); err != nil {
			t.Errorf("decode prompt: %v", err)
			return
		}
		if len(prompt.TagMatchedHistory) != 1 || prompt.TagMatchedHistory[0].TargetContentID != "candidate" {
			t.Errorf("tag matched history = %#v", prompt.TagMatchedHistory)
		}
		if len(prompt.TagMatchedHistory[0].PublishedHistory) != 1 || prompt.TagMatchedHistory[0].PublishedHistory[0].ID != "match" || prompt.TagMatchedHistory[0].PublishedHistory[0].Likes != 120 {
			t.Errorf("unrelated or incomplete metrics in prompt: %#v", prompt.TagMatchedHistory[0].PublishedHistory)
		}
		likes := prompt.TagMatchedHistory[0].MetricAggregates["likes"]
		if likes.SampleSize != 1 || likes.Mean != 120 || likes.Median != 120 {
			t.Errorf("same-tag likes aggregates = %#v", likes)
		}
		content := `{"recommendations":[{"target_content_id":"candidate","planned_date":"` + plannedDate + `","time_slot":"19:30","priority":"high","priority_order":1,"reason":"共享 Tag 的历史内容在晚间表现较好","confidence":0.82,"risk_notes":"样本数量有限"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer server.Close()
	items, err := GenerateAIRecommendations(context.Background(), AIModelConfig{BaseURL: server.URL, Model: "test-model"}, "test-key", []domain.Content{candidate}, samples, SlotPolicy{HorizonDays: 3, TimeWindows: []string{"19:30"}, MaxPerDay: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TargetContentID != "candidate" || items[0].Priority != "high" || items[0].ConfidenceScore != .82 || items[0].ReferenceSampleCount != 1 {
		t.Fatalf("normalized AI recommendations = %#v", items)
	}
}
