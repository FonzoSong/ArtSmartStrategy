package domain

import (
	"testing"
	"time"
)

func TestHorizonWindowCountsTodayAsTheFirstDay(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	window, ok := HorizonWindow(now, 14)
	if !ok {
		t.Fatal("expected a valid window")
	}
	if got := window.Start.Format("2006-01-02"); got != "2026-10-12" {
		t.Fatalf("start = %s, want 2026-10-12", got)
	}
	if got := window.End.Format("2006-01-02"); got != "2026-10-25" {
		t.Fatalf("end = %s, want 2026-10-25", got)
	}
	if window.Days() != 14 {
		t.Fatalf("days = %d, want 14", window.Days())
	}
	if dates := window.Dates(); len(dates) != 14 || dates[0].Format("2006-01-02") != "2026-10-12" || dates[13].Format("2006-01-02") != "2026-10-25" {
		t.Fatalf("unexpected dates: %v", dates)
	}
}

func TestHorizonWindowCrossesMonthAndLeapDayBoundaries(t *testing.T) {
	monthEnd, ok := HorizonWindow(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), 5)
	if !ok || monthEnd.End.Format("2006-01-02") != "2026-11-03" {
		t.Fatalf("month boundary window = %v (ok=%v)", monthEnd, ok)
	}
	leap, ok := HorizonWindow(time.Date(2028, 2, 28, 0, 0, 0, 0, time.UTC), 3)
	if !ok || leap.End.Format("2006-01-02") != "2028-03-01" {
		t.Fatalf("leap boundary window = %v (ok=%v)", leap, ok)
	}
	nonLeap, ok := HorizonWindow(time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC), 2)
	if !ok || nonLeap.End.Format("2006-01-02") != "2027-03-01" {
		t.Fatalf("non-leap boundary window = %v (ok=%v)", nonLeap, ok)
	}
}

func TestHorizonWindowRejectsUnsupportedHorizons(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for _, horizon := range []int{0, -1, MaxHorizonDays + 1} {
		if _, ok := HorizonWindow(now, horizon); ok {
			t.Fatalf("horizon %d must be rejected", horizon)
		}
	}
	if _, ok := HorizonWindow(now, MaxHorizonDays); !ok {
		t.Fatalf("horizon %d must be accepted", MaxHorizonDays)
	}
}

func TestPlanningWindowContainsIsInclusiveOnBothEdges(t *testing.T) {
	window, ok := HorizonWindow(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), 14)
	if !ok {
		t.Fatal("expected a valid window")
	}
	cases := map[string]bool{
		"2026-10-11": false, // the day before the first day
		"2026-10-12": true,  // first day
		"2026-10-25": true,  // last day
		"2026-10-26": false, // the day after the window, previously generated but not adoptable
		"2026-1-2":   false, // malformed dates never match
		"":           false,
	}
	for date, want := range cases {
		if got := window.Contains(date); got != want {
			t.Fatalf("Contains(%q) = %v, want %v", date, got, want)
		}
	}
}

func TestValidateSlotRejectsOutOfRangeAndRuleViolations(t *testing.T) {
	window, ok := HorizonWindow(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), 14)
	if !ok {
		t.Fatal("expected a valid window")
	}
	rules := PlanningRules{
		HorizonDays:           14,
		Weekdays:              []string{"周一"},
		TimeWindows:           []string{"19:30"},
		MaxPerDay:             2,
		MaxPerPlatformPerWeek: 2,
	}
	if err := rules.ValidateSlot(window, "2026-10-26", "19:30", SlotCounts{SameDay: 1, SamePlatformSameISOWeek: 1}); err != ErrSlotWindow {
		t.Fatalf("day after the window: %v", err)
	}
	if err := rules.ValidateSlot(window, "2026-10-13", "19:30", SlotCounts{SameDay: 1, SamePlatformSameISOWeek: 1}); err != ErrSlotWeekday {
		t.Fatalf("wrong weekday: %v", err)
	}
	if err := rules.ValidateSlot(window, "2026-10-12", "18:30", SlotCounts{SameDay: 1, SamePlatformSameISOWeek: 1}); err != ErrSlotTimeWindow {
		t.Fatalf("wrong time window: %v", err)
	}
	if err := rules.ValidateSlot(window, "2026-10-12", "19:30", SlotCounts{SameDay: 3, SamePlatformSameISOWeek: 1}); err != ErrSlotDailyLimit {
		t.Fatalf("daily limit: %v", err)
	}
	if err := rules.ValidateSlot(window, "2026-10-12", "19:30", SlotCounts{SameDay: 2, SamePlatformSameISOWeek: 3}); err != ErrSlotWeeklyLimit {
		t.Fatalf("weekly limit: %v", err)
	}
	if err := rules.ValidateSlot(window, "2026-10-12", "19:30", SlotCounts{SameDay: 2, SamePlatformSameISOWeek: 2}); err != nil {
		t.Fatalf("exactly at the limits must pass: %v", err)
	}
	if err := rules.ValidateSlot(window, "not-a-date", "19:30", SlotCounts{}); err != ErrSlotDateInvalid {
		t.Fatalf("invalid date: %v", err)
	}
}

func TestValidateSlotTreatsMissingWeekdaysAsEveryDay(t *testing.T) {
	window, ok := HorizonWindow(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), 14)
	if !ok {
		t.Fatal("expected a valid window")
	}
	rules := PlanningRules{TimeWindows: []string{"19:30"}}
	if err := rules.ValidateSlot(window, "2026-10-15", "19:30", SlotCounts{SameDay: 1}); err != nil {
		t.Fatalf("empty weekday list must allow every day: %v", err)
	}
	noWindows := PlanningRules{Weekdays: []string{"周一"}}
	if err := noWindows.ValidateSlot(window, "2026-10-12", "19:30", SlotCounts{SameDay: 1}); err != ErrSlotTimeWindow {
		t.Fatalf("empty time windows must reject every slot: %v", err)
	}
}

func TestSlotErrorsKeepTheirClientVisibleMessages(t *testing.T) {
	expected := map[error]string{
		ErrSlotDateInvalid: "排期日期格式必须为 YYYY-MM-DD",
		ErrSlotWindow:      "计划日期超出规则集排期周期",
		ErrSlotWeekday:     "计划日期不符合规则集允许星期",
		ErrSlotTimeWindow:  "计划时间不符合规则集允许时间窗",
		ErrSlotDailyLimit:  "计划日期超过规则集每日发布频率",
		ErrSlotWeeklyLimit: "计划日期超过该平台每周发布频率",
	}
	for err, message := range expected {
		if err.Error() != message {
			t.Fatalf("message = %q, want %q", err.Error(), message)
		}
	}
}

func TestWeekdayLabelUsesRuleSetNames(t *testing.T) {
	if got := WeekdayLabel(time.Monday); got != "周一" {
		t.Fatalf("Monday = %q", got)
	}
	if got := WeekdayLabel(time.Sunday); got != "周日" {
		t.Fatalf("Sunday = %q", got)
	}
}
