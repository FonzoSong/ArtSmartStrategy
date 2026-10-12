package domain

import (
	"errors"
	"time"
)

// MaxHorizonDays bounds the planning horizon accepted from the read-only rule set.
const MaxHorizonDays = 90

// Slot validation errors. The messages are part of the API contract: clients
// show them verbatim when a plan is rejected.
var (
	ErrSlotDateInvalid = errors.New("排期日期格式必须为 YYYY-MM-DD")
	ErrSlotWindow      = errors.New("计划日期超出规则集排期周期")
	ErrSlotWeekday     = errors.New("计划日期不符合规则集允许星期")
	ErrSlotTimeWindow  = errors.New("计划时间不符合规则集允许时间窗")
	ErrSlotDailyLimit  = errors.New("计划日期超过规则集每日发布频率")
	ErrSlotWeeklyLimit = errors.New("计划日期超过该平台每周发布频率")
)

// PlanningRules is the scheduling subset of the read-only rule set. It is the
// single source of truth shared by recommendation generation, manual planning,
// adoption validation and AI output validation.
type PlanningRules struct {
	HorizonDays           int
	Weekdays              []string
	TimeWindows           []string
	MaxPerDay             int
	MaxPerPlatformPerWeek int
}

// PlanningWindow is an inclusive range of natural days.
//
// planning_horizon_days counts natural days starting with today: a horizon of
// 14 covers [today, today+13]. Recommendation generation, manual scheduling and
// adoption all use this window, so a generated suggestion is always adoptable
// while the rules stay unchanged.
type PlanningWindow struct {
	Start time.Time
	End   time.Time
}

// HorizonWindow derives the inclusive planning window for a horizon length.
// It reports false when the horizon is missing or outside the supported range.
func HorizonWindow(now time.Time, horizonDays int) (PlanningWindow, bool) {
	if horizonDays < 1 || horizonDays > MaxHorizonDays {
		return PlanningWindow{}, false
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return PlanningWindow{Start: start, End: start.AddDate(0, 0, horizonDays-1)}, true
}

// Valid reports whether the window holds a usable date range.
func (w PlanningWindow) Valid() bool { return !w.Start.IsZero() && !w.End.Before(w.Start) }

// Dates lists every natural day in the window, oldest first.
func (w PlanningWindow) Dates() []time.Time {
	if !w.Valid() {
		return nil
	}
	result := make([]time.Time, 0, 16)
	for cursor := w.Start; !cursor.After(w.End); cursor = cursor.AddDate(0, 0, 1) {
		result = append(result, cursor)
	}
	return result
}

// Days counts the natural days inside the window.
func (w PlanningWindow) Days() int {
	if !w.Valid() {
		return 0
	}
	count := 0
	for cursor := w.Start; !cursor.After(w.End); cursor = cursor.AddDate(0, 0, 1) {
		count++
	}
	return count
}

// Contains reports whether a YYYY-MM-DD date falls inside the window. The value
// is compared as a pure calendar date, without any timezone conversion.
func (w PlanningWindow) Contains(date string) bool {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || !w.Valid() {
		return false
	}
	return !parsed.Before(w.Start) && !parsed.After(w.End)
}

// SlotCounts holds the planned load for a candidate slot. Counts include the
// candidate plan itself, so a limit of 2 is exceeded by a count of 3.
type SlotCounts struct {
	SameDay                 int
	SamePlatformSameISOWeek int
}

// ValidateSlot checks one candidate plan against the rule set. Empty weekday
// lists mean every weekday is allowed; empty time windows mean the rule set
// cannot schedule anything.
func (p PlanningRules) ValidateSlot(window PlanningWindow, date, clock string, counts SlotCounts) error {
	planned, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ErrSlotDateInvalid
	}
	if !window.Valid() || !window.Contains(date) {
		return ErrSlotWindow
	}
	if len(p.Weekdays) > 0 && !ContainsString(p.Weekdays, WeekdayLabel(planned.Weekday())) {
		return ErrSlotWeekday
	}
	if !ContainsString(p.TimeWindows, clock) {
		return ErrSlotTimeWindow
	}
	if p.MaxPerDay > 0 && counts.SameDay > p.MaxPerDay {
		return ErrSlotDailyLimit
	}
	if p.MaxPerPlatformPerWeek > 0 && counts.SamePlatformSameISOWeek > p.MaxPerPlatformPerWeek {
		return ErrSlotWeeklyLimit
	}
	return nil
}

// WeekdayLabel renders a weekday the way the rule set stores it.
func WeekdayLabel(day time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[day]
}

// ContainsString reports whether values holds target.
func ContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
