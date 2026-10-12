package service

import (
	"sort"
	"strconv"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// SlotPolicy is the validated, database-resident schedule policy.
type SlotPolicy struct {
	HorizonDays            int            `json:"planning_horizon_days"`
	TimeWindows            []string       `json:"time_windows"`
	Weekdays               []string       `json:"weekdays"`
	MaxPerDay              int            `json:"max_per_day"`
	MaxPerPlatformPerWeek  int            `json:"max_per_platform_per_week"`
	ExistingDaily          map[string]int `json:"-"`
	ExistingPlatformWeekly map[string]int `json:"-"`
}

// Rules projects the policy onto the shared planning rules used by every
// scheduling entry point (recommendations, manual plans, adoption, AI output).
func (p SlotPolicy) Rules() domain.PlanningRules {
	return domain.PlanningRules{
		HorizonDays:           p.HorizonDays,
		Weekdays:              p.Weekdays,
		TimeWindows:           p.TimeWindows,
		MaxPerDay:             p.MaxPerDay,
		MaxPerPlatformPerWeek: p.MaxPerPlatformPerWeek,
	}
}

// Window derives the inclusive planning window for this policy at the given time.
func (p SlotPolicy) Window(now time.Time) (domain.PlanningWindow, bool) {
	return domain.HorizonWindow(now, p.HorizonDays)
}

// PerformanceSample contains only a current-team published asset.
type PerformanceSample struct {
	Category    string
	Platform    string
	PublishedAt time.Time
	Views       int64
	Likes       int64
	Comments    int64
	Saves       int64
	Shares      int64
	Completion  float64
}

// Suggestion is shared by AI and local-rule schedule providers.
type Suggestion struct {
	TargetContentID      string `json:"target_content_id"`
	Title                string `json:"title"`
	ContentType          string `json:"content_type"`
	Platform             string `json:"platform"`
	Owner                string `json:"owner"`
	PlannedDate          string `json:"planned_date"`
	DayOfWeek            string `json:"day_of_week"`
	TimeSlot             string `json:"time_slot"`
	Reason               string `json:"reason"`
	SuggestionSource     string `json:"suggestion_source"`
	ReferenceSampleCount int    `json:"reference_sample_count"`
	Confidence           string `json:"confidence"`
	Limitations          string `json:"limitations"`
	// InvalidReason is filled in by the API after generation. A non-empty value
	// means the item must not be adopted until the reason is resolved.
	InvalidReason string `json:"invalid_reason,omitempty"`
	// Adopted marks items already written to the calendar during a partial adoption.
	Adopted    bool   `json:"adopted,omitempty"`
	ScheduleID string `json:"schedule_id,omitempty"`
}

// BuildRuleSuggestions schedules only unpublished assets against matching
// same-team published performance samples. It is deterministic for its inputs.
func BuildRuleSuggestions(now time.Time, candidates []domain.Content, samples []PerformanceSample, policy SlotPolicy) []Suggestion {
	rules := policy.Rules()
	window, valid := domain.HorizonWindow(now, rules.HorizonDays)
	if !valid || len(rules.TimeWindows) == 0 {
		return []Suggestion{}
	}
	type scoredSlot struct {
		date, time, weekday string
		score               float64
		count               int
	}
	usedPerDay := map[string]int{}
	for date, count := range policy.ExistingDaily {
		usedPerDay[date] = count
	}
	usedPerWeek := map[string]int{}
	for key, count := range policy.ExistingPlatformWeekly {
		usedPerWeek[key] = count
	}
	result := make([]Suggestion, 0, len(candidates))
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	for _, candidate := range candidates {
		if candidate.Lifecycle != domain.LifecycleUnpublished || candidate.WorkflowState == domain.WorkflowPublished {
			continue
		}
		var slots []scoredSlot
		for _, day := range window.Dates() {
			date := day.Format("2006-01-02")
			if usedPerDay[date] >= rules.MaxPerDay && rules.MaxPerDay > 0 {
				continue
			}
			weekday := domain.WeekdayLabel(day.Weekday())
			if len(rules.Weekdays) > 0 && !domain.ContainsString(rules.Weekdays, weekday) {
				continue
			}
			year, week := day.ISOWeek()
			weekKey := candidate.Platform + ":" + strconv.Itoa(year) + ":" + strconv.Itoa(week)
			if rules.MaxPerPlatformPerWeek > 0 && usedPerWeek[weekKey] >= rules.MaxPerPlatformPerWeek {
				continue
			}
			for _, clock := range rules.TimeWindows {
				if clockAlreadyPassed(now, day, clock) {
					continue
				}
				var score float64
				count := 0
				for _, sample := range samples {
					if sample.Category == candidate.Category && sample.Platform == candidate.Platform && sample.PublishedAt.Weekday() == day.Weekday() && sample.PublishedAt.Format("15:04") == clock {
						count++
						score += float64(sample.Views) + float64(sample.Likes+sample.Comments+sample.Saves+sample.Shares)*8 + sample.Completion*10
					}
				}
				if count > 0 {
					score /= float64(count)
				}
				slots = append(slots, scoredSlot{date: date, time: clock, weekday: weekday, score: score, count: count})
			}
		}
		if len(slots) == 0 {
			continue
		}
		sort.SliceStable(slots, func(i, j int) bool {
			if slots[i].score == slots[j].score {
				if slots[i].date == slots[j].date {
					return slots[i].time < slots[j].time
				}
				return slots[i].date < slots[j].date
			}
			return slots[i].score > slots[j].score
		})
		chosen := slots[0]
		usedPerDay[chosen.date]++
		chosenDate, _ := time.Parse("2006-01-02", chosen.date)
		chosenYear, chosenWeek := chosenDate.ISOWeek()
		usedPerWeek[candidate.Platform+":"+strconv.Itoa(chosenYear)+":"+strconv.Itoa(chosenWeek)]++
		matchingCount := 0
		for _, sample := range samples {
			if sample.Category == candidate.Category && sample.Platform == candidate.Platform {
				matchingCount++
			}
		}
		confidence := "低"
		limitations := "同团队同类型、同平台的已发布样本不足，建议仅依据规则集时间窗。"
		reason := "按规则集 schedule_policy.time_windows 选择可用发布时间，并遵守 planning_horizon_days 与 max_per_day。"
		if matchingCount >= 10 {
			confidence = "高"
			limitations = "根据同团队同类型、同平台已发布样本排序；历史表现不保证未来结果。"
			reason += " 匹配历史样本 " + itoa(matchingCount) + " 条，按对应星期与时段的表现排序。"
		} else if matchingCount >= 3 {
			confidence = "中"
			limitations = "历史样本较少，建议结合团队判断。"
			reason += " 匹配历史样本 " + itoa(matchingCount) + " 条，置信度有限。"
		}
		result = append(result, Suggestion{TargetContentID: candidate.ID, Title: candidate.Title, ContentType: candidate.Category, Platform: candidate.Platform, Owner: candidate.Owner, PlannedDate: chosen.date, DayOfWeek: chosen.weekday, TimeSlot: chosen.time, Reason: reason, SuggestionSource: "规则引擎", ReferenceSampleCount: matchingCount, Confidence: confidence, Limitations: limitations})
	}
	return result
}

// clockAlreadyPassed reports whether a slot on the current day has already
// started. Recommendations never propose a time that is already in the past;
// manual planning may still record a same-day plan explicitly.
func clockAlreadyPassed(now, day time.Time, clock string) bool {
	nowYear, nowMonth, nowDay := now.Date()
	dayYear, dayMonth, dayDate := day.Date()
	if nowYear != dayYear || nowMonth != dayMonth || nowDay != dayDate {
		return false
	}
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		return false
	}
	return now.Hour()*60+now.Minute() >= parsed.Hour()*60+parsed.Minute()
}

func itoa(value int) string { return strconv.Itoa(value) }
