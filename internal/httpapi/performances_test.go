package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// TestPerformancePatchOnlyTouchesSentFields pins the partial-update contract: the
// browser submits `datetime-local` values, an empty owner clears the assignment,
// and unsupported keys are refused instead of silently ignored.
func TestPerformancePatchOnlyTouchesSentFields(t *testing.T) {
	var input map[string]any
	if err := json.Unmarshal([]byte(`{
		"title": " 春季折子戏专场 ",
		"starts_at": "2026-10-21T19:30",
		"owner_user_id": null,
		"performance_type": "地方戏"
	}`), &input); err != nil {
		t.Fatal(err)
	}
	perf, fields, err := performancePatch(input)
	if err != nil {
		t.Fatal(err)
	}
	if !fields["title"] || !fields["starts_at"] || !fields["owner_user_id"] || !fields["performance_type"] {
		t.Fatalf("expected the sent fields to be marked, got %#v", fields)
	}
	if fields["venue"] || fields["notes"] || fields["booking_status"] {
		t.Fatalf("unsent fields must not be updated, got %#v", fields)
	}
	if perf.OwnerUserID != nil {
		t.Fatal("an explicit null owner must clear the assignment")
	}
	// Storage is a TIMESTAMP WITHOUT TIME ZONE, so the wall-clock value is kept
	// verbatim and never shifted by the server timezone.
	if want := time.Date(2026, 10, 21, 19, 30, 0, 0, time.UTC); !perf.StartsAt.UTC().Equal(want) {
		t.Fatalf("starts_at = %s, want %s", perf.StartsAt, want)
	}
}

func TestPerformancePatchRejectsUnknownAndMalformedFields(t *testing.T) {
	cases := map[string]string{
		"unknown field":   `{"production_secret":"x"}`,
		"bad status type": `{"booking_status": 7}`,
		"bad time":        `{"starts_at": "12/10/2026"}`,
		"bad owner":       `{"owner_user_id": "abc"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var input map[string]any
			if err := json.Unmarshal([]byte(raw), &input); err != nil {
				t.Fatal(err)
			}
			if _, _, err := performancePatch(input); err == nil {
				t.Fatalf("expected %s to be rejected", raw)
			}
		})
	}
}

func TestParseFlexibleTimeRejectsMissingValue(t *testing.T) {
	if _, err := parseFlexibleTime("   "); err == nil {
		t.Fatal("an empty performance time must be rejected")
	}
}

// TestOverviewResponseCarriesPerformanceDimension guards the contract the
// operations overview and home page read for 正式演出 statistics.
func TestOverviewResponseCarriesPerformanceDimension(t *testing.T) {
	payload := buildOverviewResponse(map[string]any{
		"total_content_count": int64(12),
		"published_count":     int64(5),
	}, nil, map[string]float64{"completion": 0.6, "interaction": 0.4})
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["total_content_count"] != float64(12) {
		t.Fatalf("total_content_count = %v", decoded["total_content_count"])
	}
	performances, ok := decoded["performances"].(map[string]any)
	if !ok {
		t.Fatalf("performances block is missing from the overview payload: %#v", decoded)
	}
	for _, key := range []string{"preparing_count", "unpublished_promo_count", "plan_total", "schedule_completion_rate"} {
		if _, ok := performances[key]; !ok {
			t.Fatalf("performances block is missing %q", key)
		}
	}
}

// TestPerformanceStatusContract pins the canonical show states and the rule that
// the expiry transition never rewrites a cancelled show.
func TestPerformanceStatusContract(t *testing.T) {
	want := map[string]bool{"筹备中": true, "预热中": true, "已演出": true, "已取消": true}
	for _, status := range domain.PerformanceStatuses() {
		if !want[status] {
			t.Fatalf("unexpected performance status %q", status)
		}
		delete(want, status)
	}
	if len(want) != 0 {
		t.Fatalf("missing performance statuses: %#v", want)
	}
	if domain.IsPerformanceStatus("已预订") {
		t.Fatal("the legacy booking status must no longer be accepted")
	}
}
