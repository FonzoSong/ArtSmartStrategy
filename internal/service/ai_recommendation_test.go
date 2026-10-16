package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

func TestAggregateTagMetricsIncludesMedianAndSevenAndThirtyDayTrends(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	samples := []PerformanceSample{
		{PublishedAt: now.AddDate(0, 0, -1), Views: 300, Likes: 30},
		{PublishedAt: now.AddDate(0, 0, -4), Views: 100, Likes: 10},
		{PublishedAt: now.AddDate(0, 0, -8), Views: 80, Likes: 8},
		{PublishedAt: now.AddDate(0, 0, -20), Views: 20, Likes: 2},
	}
	views := aggregateTagMetrics(samples, now)["plays"]
	if views.SampleSize != 4 || views.Mean != 125 || views.Median != 90 {
		t.Fatalf("overall views metrics = %#v", views)
	}
	if views.Trend7d.RecentMean == nil || *views.Trend7d.RecentMean != 200 || views.Trend7d.PreviousMean == nil || *views.Trend7d.PreviousMean != 80 {
		t.Fatalf("7-day views trend = %#v", views.Trend7d)
	}
	if views.Trend30d.RecentMean == nil || *views.Trend30d.RecentMean != 125 || views.Trend30d.PreviousMean != nil {
		t.Fatalf("30-day views trend should mark missing comparison as unavailable: %#v", views.Trend30d)
	}
}

func TestGenerateAIRecommendationsRetriesInvalidStructuredOutputOnce(t *testing.T) {
	nowDate := time.Now().Format("2006-01-02")
	candidate := domain.Content{ID: "candidate", Title: "预热", Category: "演出宣传", Platform: "视频号", Tags: []string{"开演"}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeAICompletion(t, w, `{"recommendations":[{"target_content_id":"candidate"}]}`)
			return
		}
		writeAICompletion(t, w, fmt.Sprintf(`{"recommendations":[{"target_content_id":"candidate","planned_date":%q,"time_slot":"19:30","priority":"high","priority_order":1,"reason":"共享 Tag 历史表现较好","confidence":0.8,"risk_notes":"样本有限"}]}`, nowDate))
	}))
	defer server.Close()
	items, err := GenerateAIRecommendations(context.Background(), AIModelConfig{BaseURL: server.URL, Model: "test"}, "test-key", []domain.Content{candidate}, nil, SlotPolicy{HorizonDays: 3, TimeWindows: []string{"19:30"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(items) != 1 || items[0].TargetContentID != candidate.ID {
		t.Fatalf("calls/items = %d/%#v", calls, items)
	}
}

func TestGenerateAIRecommendationsStopsAtConfiguredTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"recommendations\":[]}"}}]}`))
	}))
	defer server.Close()

	candidate := domain.Content{ID: "candidate", Tags: []string{"tag"}}
	started := time.Now()
	_, err := GenerateAIRecommendations(
		context.Background(),
		AIModelConfig{BaseURL: server.URL, Model: "test"},
		"test-key",
		[]domain.Content{candidate},
		nil,
		SlotPolicy{HorizonDays: 3, TimeWindows: []string{"19:30"}},
		AIRecommendationOptions{Timeout: 40 * time.Millisecond},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want configured AI timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("configured timeout took %s, expected under 1s", elapsed)
	}
}

func TestGenerateAIRecommendationsRetainsBoundedRawResponseAfterRetry(t *testing.T) {
	calls := 0
	raw := `{"recommendations":[{"unexpected":"invalid"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeAICompletion(t, w, raw)
	}))
	defer server.Close()
	candidate := domain.Content{ID: "candidate", Tags: []string{"tag"}}
	_, err := GenerateAIRecommendations(context.Background(), AIModelConfig{BaseURL: server.URL, Model: "test"}, "test-key", []domain.Content{candidate}, nil, SlotPolicy{HorizonDays: 3, TimeWindows: []string{"19:30"}})
	var responseErr *AIRecommendationResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error = %v, want AIRecommendationResponseError", err)
	}
	if calls != 2 || responseErr.RawResponse != raw || len(responseErr.RawResponse) > 8000 {
		t.Fatalf("calls/raw response = %d/%q", calls, responseErr.RawResponse)
	}
}

func writeAICompletion(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}}); err != nil {
		t.Errorf("encode completion: %v", err)
	}
}
