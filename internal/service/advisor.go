package service

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
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
	ID            string    `json:"content_id"`
	Title         string    `json:"title"`
	Category      string    `json:"content_type"`
	Platform      string    `json:"platform"`
	PublishedAt   time.Time `json:"published_at"`
	Tags          []string  `json:"tags"`
	Duration      int       `json:"duration_seconds"`
	Views         int64     `json:"views"`
	Likes         int64     `json:"likes"`
	Comments      int64     `json:"comments"`
	Saves         int64     `json:"favorites"`
	Shares        int64     `json:"shares"`
	Completion    float64   `json:"completion_rate"`
	ProfileVisits int64     `json:"profile_visits"`
	Follows       int64     `json:"follows"`
	Attendance    int64     `json:"attendance"`
	Tickets       int64     `json:"tickets"`
}

// Suggestion is shared by AI and local-rule schedule providers.
type Suggestion struct {
	TargetContentID      string   `json:"target_content_id"`
	Title                string   `json:"title"`
	ContentType          string   `json:"content_type"`
	Platform             string   `json:"platform"`
	Owner                string   `json:"owner"`
	PlannedDate          string   `json:"planned_date"`
	DayOfWeek            string   `json:"day_of_week"`
	TimeSlot             string   `json:"time_slot"`
	Reason               string   `json:"reason"`
	SuggestionSource     string   `json:"suggestion_source"`
	ReferenceSampleCount int      `json:"reference_sample_count"`
	Confidence           string   `json:"confidence"`
	Limitations          string   `json:"limitations"`
	PriorityOrder        int      `json:"priority_order"`
	Priority             string   `json:"priority"`
	ConfidenceScore      float64  `json:"confidence_score"`
	RiskNotes            string   `json:"risk_notes"`
	Tags                 []string `json:"tags"`
	// PreviousSchedule is set only when an existing plan was changed while
	// applying this recommendation. It makes that operation reversible.
	PreviousSchedule *ScheduleSnapshot `json:"previous_schedule,omitempty"`
	// InvalidReason is filled in by the API after generation. A non-empty value
	// means the item must not be adopted until the reason is resolved.
	InvalidReason string `json:"invalid_reason,omitempty"`
	// Adopted marks items already written to the calendar during a partial adoption.
	Adopted    bool   `json:"adopted,omitempty"`
	ScheduleID string `json:"schedule_id,omitempty"`
}

// ScheduleSnapshot captures the prior calendar values when an AI suggestion
// adjusts an existing plan, so applying that recommendation can be reverted.
type ScheduleSnapshot struct {
	ID               string `json:"id"`
	RecommendationID string `json:"recommendation_id"`
	Date             string `json:"planned_date"`
	Time             string `json:"planned_time"`
	Title            string `json:"title"`
	Category         string `json:"category"`
	Platform         string `json:"platform"`
	Owner            string `json:"owner"`
	Reason           string `json:"reason"`
	Source           string `json:"source"`
	Priority         string `json:"priority"`
	PriorityOrder    int    `json:"priority_order"`
	RiskNotes        string `json:"risk_notes"`
}

// StructuredAIRecommendation is the validated JSON contract expected from an
// AI provider. Confidence is a probability in [0,1], not an unbounded score.
type StructuredAIRecommendation struct {
	TargetContentID string   `json:"target_content_id"`
	PlannedDate     string   `json:"planned_date"`
	TimeSlot        string   `json:"time_slot"`
	Priority        string   `json:"priority"`
	PriorityOrder   int      `json:"priority_order"`
	Reason          string   `json:"reason"`
	Confidence      *float64 `json:"confidence"`
	RiskNotes       string   `json:"risk_notes"`
}

// SharesAnyTag reports whether two tag sets have at least one common tag.
func SharesAnyTag(left, right []string) bool {
	seen := make(map[string]struct{}, len(left))
	for _, tag := range left {
		if key := strings.ToLower(strings.TrimSpace(tag)); key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, tag := range right {
		if key := strings.ToLower(strings.TrimSpace(tag)); key != "" {
			if _, ok := seen[key]; ok {
				return true
			}
		}
	}
	return false
}

// TagMatchedSamples returns only same-tag published records for a candidate.
// Candidates with no tags have no tag-matched history and must not silently
// borrow unrelated team samples.
func TagMatchedSamples(candidate domain.Content, samples []PerformanceSample) []PerformanceSample {
	matched := make([]PerformanceSample, 0)
	if len(candidate.Tags) == 0 {
		return matched
	}
	for _, sample := range samples {
		if SharesAnyTag(candidate.Tags, sample.Tags) {
			matched = append(matched, sample)
		}
	}
	return matched
}

// ValidateAIRecommendations rejects malformed, duplicate, out-of-scope, and
// policy-violating model output before it can be stored or shown to users.
func ValidateAIRecommendations(now time.Time, generated []StructuredAIRecommendation, candidates []domain.Content, samples []PerformanceSample, policy SlotPolicy) ([]Suggestion, error) {
	byID := make(map[string]domain.Content, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	window, ok := policy.Window(now)
	if !ok {
		return nil, errors.New("排期规则集缺少有效周期")
	}
	allowedTimes, allowedWeekdays := map[string]bool{}, map[string]bool{}
	for _, clock := range policy.TimeWindows {
		allowedTimes[clock] = true
	}
	for _, day := range policy.Weekdays {
		allowedWeekdays[day] = true
	}
	usedDaily, usedWeekly, usedContent := map[string]int{}, map[string]int{}, map[string]bool{}
	usedOrder := map[int]bool{}
	for key, count := range policy.ExistingDaily {
		usedDaily[key] = count
	}
	for key, count := range policy.ExistingPlatformWeekly {
		usedWeekly[key] = count
	}
	result := make([]Suggestion, 0, len(generated))
	for _, item := range generated {
		candidate, exists := byID[item.TargetContentID]
		priority := strings.ToLower(strings.TrimSpace(item.Priority))
		if !exists || usedContent[item.TargetContentID] || item.PriorityOrder <= 0 || !allowedTimes[item.TimeSlot] || strings.TrimSpace(item.Reason) == "" || strings.TrimSpace(item.RiskNotes) == "" || len(item.Reason) > 1000 || len(item.RiskNotes) > 1000 {
			return nil, errors.New("模型建议包含无效内容或时间")
		}
		if priority != "high" && priority != "medium" && priority != "low" {
			return nil, errors.New("模型建议优先级无效")
		}
		if item.Confidence == nil || *item.Confidence < 0 || *item.Confidence > 1 || math.IsNaN(*item.Confidence) || math.IsInf(*item.Confidence, 0) {
			return nil, errors.New("模型建议置信度必须在 0 到 1 之间")
		}
		planned, err := time.Parse("2006-01-02", item.PlannedDate)
		if err != nil || !window.Contains(item.PlannedDate) || len(allowedWeekdays) > 0 && !allowedWeekdays[domain.WeekdayLabel(planned.Weekday())] {
			return nil, errors.New("模型建议日期超出规则时间范围")
		}
		usedDaily[item.PlannedDate]++
		if policy.MaxPerDay > 0 && usedDaily[item.PlannedDate] > policy.MaxPerDay {
			return nil, errors.New("模型建议超过每日发布频率")
		}
		year, week := planned.ISOWeek()
		weekKey := candidate.Platform + ":" + strconv.Itoa(year) + ":" + strconv.Itoa(week)
		usedWeekly[weekKey]++
		if policy.MaxPerPlatformPerWeek > 0 && usedWeekly[weekKey] > policy.MaxPerPlatformPerWeek {
			return nil, errors.New("模型建议超过平台每周发布频率")
		}
		usedContent[item.TargetContentID] = true
		matching := TagMatchedSamples(candidate, samples)
		confidenceLabel := "低"
		if *item.Confidence >= .75 {
			confidenceLabel = "高"
		} else if *item.Confidence >= .4 {
			confidenceLabel = "中"
		}
		risk := strings.TrimSpace(item.RiskNotes)
		if risk == "" {
			risk = "需结合现有排期与实际制作进度确认。"
		}
		order := item.PriorityOrder
		if usedOrder[order] {
			return nil, errors.New("模型建议优先顺序重复")
		}
		usedOrder[order] = true
		result = append(result, Suggestion{TargetContentID: candidate.ID, Title: candidate.Title, ContentType: candidate.Category, Platform: candidate.Platform, Owner: candidate.Owner, PlannedDate: item.PlannedDate, DayOfWeek: domain.WeekdayLabel(planned.Weekday()), TimeSlot: item.TimeSlot, Reason: strings.TrimSpace(item.Reason), SuggestionSource: "AI", ReferenceSampleCount: len(matching), Confidence: confidenceLabel, ConfidenceScore: *item.Confidence, Limitations: tagLimitations(len(matching)), PriorityOrder: order, Priority: priority, RiskNotes: risk, Tags: append([]string{}, candidate.Tags...)})
	}
	if len(result) == 0 && len(candidates) > 0 {
		return nil, errors.New("模型未返回有效建议")
	}
	return result, nil
}

func tagLimitations(sampleCount int) string {
	if sampleCount >= 10 {
		return "依据共享 Tag 的团队历史样本；历史表现不保证未来结果。"
	}
	if sampleCount >= 3 {
		return "共享 Tag 的历史样本较少，建议结合团队判断。"
	}
	return "共享 Tag 的已发布样本不足，置信度有限。"
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
					if SharesAnyTag(candidate.Tags, sample.Tags) && sample.Category == candidate.Category && sample.Platform == candidate.Platform && sample.PublishedAt.Weekday() == day.Weekday() && sample.PublishedAt.Format("15:04") == clock {
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
		matchingCount := len(TagMatchedSamples(candidate, samples))
		confidence := "低"
		limitations := "共享 Tag 的已发布样本不足，建议仅依据规则集时间窗。"
		reason := "按规则集 schedule_policy.time_windows 选择可用发布时间，并遵守 planning_horizon_days 与 max_per_day。"
		if matchingCount >= 10 {
			confidence = "高"
			limitations = "根据共享 Tag 的团队历史样本排序；历史表现不保证未来结果。"
			reason += " 匹配历史样本 " + itoa(matchingCount) + " 条，按对应星期与时段的表现排序。"
		} else if matchingCount >= 3 {
			confidence = "中"
			limitations = "共享 Tag 的历史样本较少，建议结合团队判断。"
			reason += " 匹配历史样本 " + itoa(matchingCount) + " 条，置信度有限。"
		}
		priority := "medium"
		if len(result) == 0 {
			priority = "high"
		}
		result = append(result, Suggestion{TargetContentID: candidate.ID, Title: candidate.Title, ContentType: candidate.Category, Platform: candidate.Platform, Owner: candidate.Owner, PlannedDate: chosen.date, DayOfWeek: chosen.weekday, TimeSlot: chosen.time, Reason: reason, SuggestionSource: "规则引擎", ReferenceSampleCount: matchingCount, Confidence: confidence, Limitations: limitations, PriorityOrder: len(result) + 1, Priority: priority, ConfidenceScore: 0, RiskNotes: "历史表现不代表未来结果；采纳前请检查现有排期与制作进度。", Tags: append([]string{}, candidate.Tags...)})
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
