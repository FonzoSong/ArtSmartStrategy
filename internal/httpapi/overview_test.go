package httpapi

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestFlexibleTimeAcceptsClientAndAPITimestampLayouts(t *testing.T) {
	cases := map[string]struct {
		input   string
		want    string
		wantErr bool
	}{
		"datetime-local": {"2026-10-12T19:30", "2026-10-12T19:30:00Z", false},
		"rfc3339":        {"2026-10-12T19:30:00Z", "2026-10-12T19:30:00Z", false},
		"space layout":   {"2026-10-12 19:30:00", "2026-10-12T19:30:00Z", false},
		"date only":      {"2026-10-12", "2026-10-12T00:00:00Z", false},
		"invalid":        {"12/10/2026", "", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var parsed flexibleTime
			err := parsed.UnmarshalJSON([]byte(`"` + tc.input + `"`))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected %q to be rejected", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := parsed.UTC().Format(time.RFC3339); got != tc.want {
				t.Fatalf("parsed = %s, want %s", got, tc.want)
			}
		})
	}
}

// requiredOverviewFields is the contract the operations overview page consumes.
var requiredOverviewFields = []string{
	"published_count",
	"unpublished_count",
	"total_posts",
	"total_views",
	"total_interactions",
	"platform_count",
	"avg_completion",
	"sample_count",
	"top_contents",
}

func decodeOverview(t *testing.T, payload any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestOverviewResponseAlwaysCarriesTheFrontendContract(t *testing.T) {
	response := buildOverviewResponse(nil, nil, map[string]float64{"completion": .6, "interaction": .4})
	decoded := decodeOverview(t, response)
	for _, field := range requiredOverviewFields {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("overview response is missing %q", field)
		}
	}
	if _, ok := decoded["top_contents"].([]any); !ok {
		t.Fatalf("top_contents must be a JSON array, got %T", decoded["top_contents"])
	}
	if response.TopContents == nil {
		t.Fatal("top_contents must never be nil")
	}
	if len(response.TopContents) != 0 {
		t.Fatalf("empty team must return an empty ranking, got %d items", len(response.TopContents))
	}
	if response.AvgCompletion != 0 || response.SampleCount != 0 || response.TotalPosts != 0 || response.PlatformCount != 0 {
		t.Fatalf("missing values must normalize to zero: %#v", response)
	}
}

func TestOverviewResponseNormalizesNullsAndUnknownTypes(t *testing.T) {
	row := map[string]any{
		"published_count":    int64(4),
		"unpublished_count":  nil,
		"total_views":        int32(120),
		"total_likes":        "45",
		"platform_count":     float64(2),
		"avg_completion":     nil,
		"sample_count":       int64(4),
		"total_interactions": int64(0),
	}
	response := buildOverviewResponse(row, nil, map[string]float64{"completion": .6, "interaction": .4})
	if response.PublishedCount != 4 || response.TotalPosts != 4 {
		t.Fatalf("published counts = %d/%d", response.PublishedCount, response.TotalPosts)
	}
	if response.UnpublishedCount != 0 || response.AvgCompletion != 0 {
		t.Fatalf("nulls must normalize to zero: %#v", response)
	}
	if response.TotalViews != 120 || response.TotalLikes != 45 || response.PlatformCount != 2 {
		t.Fatalf("numeric conversions are wrong: %#v", response)
	}
}

func TestRankTopContentsScoresAndOrdersDeterministically(t *testing.T) {
	weights := map[string]float64{"completion": .6, "interaction": .4}
	rows := []map[string]any{
		{
			"id": "b", "title": "B", "category": "演出宣传", "platform": "视频号",
			"views": int64(1000), "total_interactions": int64(100), "completion": 50.0,
			"published_at": time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC),
		},
		{
			"id": "a", "title": "A", "category": "幕后记录", "platform": "视频号",
			"views": int64(1000), "total_interactions": int64(200), "completion": 50.0,
			"published_at": time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC),
		},
		{
			"id": "c", "title": "C", "category": "艺术科普", "platform": "视频号",
			"views": int64(0), "total_interactions": int64(7), "completion": 0.0,
			"published_at": time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC),
		},
	}
	items := rankTopContents(rows, weights)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if items[0].ID != "a" || items[1].ID != "b" || items[2].ID != "c" {
		t.Fatalf("unexpected order: %s, %s, %s", items[0].ID, items[1].ID, items[2].ID)
	}
	// 50*0.6 + 20*0.4 = 38, and the interaction rate must be a percentage value.
	if items[0].Score != 38 || items[0].InteractionRate != 20 {
		t.Fatalf("unexpected score for a: %#v", items[0])
	}
	// A record without views must not divide by zero and scores from completion only.
	if items[2].InteractionRate != 0 || items[2].Score != 0 {
		t.Fatalf("unexpected score for c: %#v", items[2])
	}
	if items[0].Title != "A" || items[0].Platform != "视频号" || items[0].Category != "幕后记录" {
		t.Fatalf("top content must expose the fields the page renders: %#v", items[0])
	}
	if items[0].PublishedAt == "" {
		t.Fatal("published_at must be rendered for tie-break ordering")
	}
}

func TestRankTopContentsRoundsToTwoDecimals(t *testing.T) {
	weights := map[string]float64{"completion": .6, "interaction": .4}
	items := rankTopContents([]map[string]any{{
		"id": "x", "title": "X", "views": int64(3), "total_interactions": int64(1), "completion": 33.33333,
	}}, weights)
	if items[0].Completion != 33.33 {
		t.Fatalf("completion = %v, want 33.33", items[0].Completion)
	}
	if items[0].InteractionRate != 33.33 {
		t.Fatalf("interaction rate = %v, want 33.33", items[0].InteractionRate)
	}
}

func TestBuildDiagnosticsProducesReadableLevels(t *testing.T) {
	empty := buildDiagnostics(nil)
	if len(empty) == 0 {
		t.Fatal("an empty team still needs one reminder")
	}
	for _, item := range empty {
		if item.Level == "" || item.Message == "" {
			t.Fatalf("diagnostics must carry a level and a message: %#v", item)
		}
	}
	items := buildDiagnostics([]map[string]any{
		{"lifecycle": "已发布", "count": int64(6)},
		{"lifecycle": "未发布", "count": int64(2)},
	})
	levels := map[string]string{}
	for _, item := range items {
		levels[item.Level] = item.Message
	}
	if levels["good"] == "" || levels["warn"] == "" {
		t.Fatalf("expected good and warn reminders, got %#v", items)
	}
}

func TestFieldReadersTolerateDriverVariants(t *testing.T) {
	row := map[string]any{
		"int": int32(7), "int64": int64(8), "float": 9.9, "numeric_string": "10.5",
		"bad_string": "abc", "bool": true, "bool_string": "true",
		"stamp":  time.Date(2026, 10, 12, 8, 30, 0, 0, time.UTC),
		"picked": nil,
	}
	if intField(row, "int") != 7 || intField(row, "int64") != 8 || intField(row, "float") != 9 || intField(row, "numeric_string") != 10 {
		t.Fatalf("int conversions are wrong: %#v", row)
	}
	if intField(row, "bad_string") != 0 || intField(row, "picked") != 0 || intField(row, "absent") != 0 {
		t.Fatal("invalid integers must normalize to zero")
	}
	if floatField(row, "float") != 9.9 || floatField(row, "numeric_string") != 10.5 || floatField(row, "bad_string") != 0 {
		t.Fatal("float conversions are wrong")
	}
	if stringField(row, "int") != "" || stringField(row, "absent") != "" {
		t.Fatal("non-string values must not be rendered as text")
	}
	if !boolField(row, "bool") || !boolField(row, "bool_string") || boolField(row, "absent") {
		t.Fatal("bool conversions are wrong")
	}
	if timestampField(row, "stamp") != "2026-10-12T08:30:00Z" || timestampField(row, "picked") != "" {
		t.Fatal("timestamp conversions are wrong")
	}
	if roundTo(1.23456, 2) != 1.23 || roundTo(-1.235, 2) != -1.24 || roundTo(10, 2) != 10 {
		t.Fatalf("roundTo is wrong: %v %v", roundTo(1.23456, 2), roundTo(-1.235, 2))
	}
	if roundTo(math.Inf(1), 2) != 0 || roundTo(math.NaN(), 2) != 0 {
		t.Fatal("non-finite values must normalize to zero")
	}
}
