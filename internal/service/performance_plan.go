package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// PerformancePlanInput is everything the whole-show planner is allowed to use:
// the show itself, its unpublished promotional videos, the team's own published
// history and the read-only rule set. No other data enters the decision.
type PerformancePlanInput struct {
	Now        time.Time
	ShowTitle  string
	StartsAt   time.Time
	Candidates []domain.Content
	Samples    []PerformanceSample
	Policy     SlotPolicy
}

// categoryPhase orders content categories from "earliest preparation" to
// "closest to the show". Promotional reminders end up nearest the curtain.
func categoryPhase(category string) int {
	switch category {
	case "幕后记录":
		return 0
	case "技艺展示":
		return 1
	case "艺术科普":
		return 2
	case "演出宣传":
		return 3
	default:
		return 2
	}
}

// phaseRole explains what the content is meant to achieve at that distance.
func phaseRole(category string) string {
	switch category {
	case "幕后记录":
		return "提前铺垫，让观众熟悉演员与剧目背景"
	case "技艺展示":
		return "展示技艺亮点，吸引对剧种感兴趣的观众"
	case "艺术科普":
		return "降低理解门槛，扩大潜在观众"
	case "演出宣传":
		return "临近开演，承担提醒、催单与余票信息"
	default:
		return "按内容类型安排在预热周期内"
	}
}

// interactionCount is the engagement a published sample produced.
func interactionCount(sample PerformanceSample) float64 {
	return float64(sample.Likes + sample.Comments + sample.Saves + sample.Shares)
}

// nearestClock maps a real publication timestamp onto the rule set's time
// windows, so historical performance can be attributed to a schedulable slot.
func nearestClock(publishedAt time.Time, windows []string) string {
	best, bestDiff := "", 24*60
	publishedMinutes := publishedAt.Hour()*60 + publishedAt.Minute()
	for _, window := range windows {
		parsed, err := time.Parse("15:04", window)
		if err != nil {
			continue
		}
		diff := parsed.Hour()*60 + parsed.Minute() - publishedMinutes
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = window, diff
		}
	}
	return best
}

type aggregate struct {
	count           int
	totalEngage     float64
	totalCompletion float64
}

// BuildPerformancePlan turns one show's unpublished videos into an executable
// pre-heat schedule. It is deterministic for its inputs, never plans past the
// show's start time, spreads items across days and platforms, and explains every
// recommendation with the historical sample it is based on. When the team has no
// matching history it says so instead of inventing a reason.
func BuildPerformancePlan(in PerformancePlanInput) ([]Suggestion, bool) {
	rules := in.Policy.Rules()
	window, ok := domain.HorizonWindow(in.Now, rules.HorizonDays)
	if !ok || len(rules.TimeWindows) == 0 || len(in.Candidates) == 0 {
		return []Suggestion{}, false
	}
	showDay := time.Date(in.StartsAt.Year(), in.StartsAt.Month(), in.StartsAt.Day(), 0, 0, 0, 0, in.StartsAt.Location())
	if showDay.Before(window.Start) {
		// The show is already over or starts before the planning window opens.
		return []Suggestion{}, false
	}
	allowed := make([]time.Time, 0, 32)
	for _, day := range window.Dates() {
		if day.After(showDay) {
			break
		}
		if len(rules.Weekdays) > 0 && !domain.ContainsString(rules.Weekdays, domain.WeekdayLabel(day.Weekday())) {
			continue
		}
		allowed = append(allowed, day)
	}
	if len(allowed) == 0 {
		return []Suggestion{}, false
	}

	// Historical evidence: how each category performed per platform and per slot.
	platformStats := map[string]map[string]*aggregate{}
	slotStats := map[string]map[string]*aggregate{}
	var teamEngage, teamCompletion float64
	for _, sample := range in.Samples {
		teamEngage += interactionCount(sample)
		teamCompletion += sample.Completion
		if platformStats[sample.Category] == nil {
			platformStats[sample.Category] = map[string]*aggregate{}
		}
		stat := platformStats[sample.Category][sample.Platform]
		if stat == nil {
			stat = &aggregate{}
			platformStats[sample.Category][sample.Platform] = stat
		}
		stat.count++
		stat.totalEngage += interactionCount(sample)
		clock := nearestClock(sample.PublishedAt, rules.TimeWindows)
		if clock != "" {
			key := domain.WeekdayLabel(sample.PublishedAt.Weekday()) + " " + clock
			if slotStats[sample.Category] == nil {
				slotStats[sample.Category] = map[string]*aggregate{}
			}
			slot := slotStats[sample.Category][key]
			if slot == nil {
				slot = &aggregate{}
				slotStats[sample.Category][key] = slot
			}
			slot.count++
			slot.totalEngage += interactionCount(sample)
			slot.totalCompletion += sample.Completion
		}
	}
	teamAvgEngage, teamAvgCompletion := 0.0, 0.0
	if len(in.Samples) > 0 {
		teamAvgEngage = teamEngage / float64(len(in.Samples))
		teamAvgCompletion = teamCompletion / float64(len(in.Samples))
	}

	// bestPlatform picks the team's strongest platform for a category, falling
	// back to the platform the video already targets.
	bestPlatform := func(candidate domain.Content) (string, string) {
		stats := platformStats[candidate.Category]
		best, bestAvg, bestCount := "", -1.0, 0
		for platform, stat := range stats {
			avg := stat.totalEngage / float64(stat.count)
			if avg > bestAvg || (avg == bestAvg && platform < best) {
				best, bestAvg, bestCount = platform, avg, stat.count
			}
		}
		if best == "" {
			return candidate.Platform, fmt.Sprintf("样本不足，沿用该内容已有平台「%s」；建议发布后回填实际数据以形成历史依据。", candidate.Platform)
		}
		verdict := "高于"
		if bestAvg <= teamAvgEngage {
			verdict = "接近或低于"
		}
		return best, fmt.Sprintf("该团队历史「%s」类内容在「%s」发布 %d 条，平均互动 %s，%s团队同类平均 %s。", candidate.Category, best, bestCount, formatNumber(bestAvg), verdict, formatNumber(teamAvgEngage))
	}

	// bestClock returns the team's strongest weekday+time for a category, or an
	// empty clock when the team has no matching history. The caller decides how to
	// rotate the rule-set windows in that case, so the reason text always matches
	// the slot that was actually chosen.
	bestClock := func(category string) (string, string) {
		stats := slotStats[category]
		bestKey, bestAvg := "", -1.0
		for key, stat := range stats {
			avg := stat.totalCompletion / float64(stat.count)
			if avg > bestAvg || (avg == bestAvg && key < bestKey) {
				bestKey, bestAvg = key, avg
			}
		}
		if bestKey == "" {
			return "", ""
		}
		parts := strings.SplitN(bestKey, " ", 2)
		verdict := "高于"
		if bestAvg <= teamAvgCompletion {
			verdict = "接近或低于"
		}
		return parts[1], fmt.Sprintf("该团队历史「%s」类内容在%s%s的平均看完比例 %s%%，%s团队同类平均 %s%%，故建议沿用该时段。", category, parts[0], parts[1], formatNumber(bestAvg), verdict, formatNumber(teamAvgCompletion))
	}

	ordered := append([]domain.Content(nil), in.Candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, pj := categoryPhase(ordered[i].Category), categoryPhase(ordered[j].Category)
		if pi != pj {
			return pi < pj
		}
		return ordered[i].ID < ordered[j].ID
	})

	dayCount := map[string]int{}
	weekPlatform := map[string]int{}
	dayPlatform := map[string]bool{}
	slotRotation := 0
	insufficient := len(in.Samples) < 3
	result := make([]Suggestion, 0, len(ordered))

	// Promotional reminders are assigned from the end of the window so they land
	// closest to the curtain; preparation content fills the earlier days.
	lastPhase := categoryPhase(ordered[len(ordered)-1].Category)
	for _, candidate := range ordered {
		platform, platformReason := bestPlatform(candidate)
		historyClock, historyReason := bestClock(candidate.Category)
		fromEnd := categoryPhase(candidate.Category) == lastPhase && lastPhase == 3
		assigned, plannedDate, clock := "", "", ""
		scan := func(day time.Time, candidateClock string) bool {
			key := day.Format("2006-01-02")
			if rules.MaxPerDay > 0 && dayCount[key] >= rules.MaxPerDay {
				return false
			}
			year, week := day.ISOWeek()
			weekKey := platform + ":" + strconv.Itoa(year) + ":" + strconv.Itoa(week)
			if rules.MaxPerPlatformPerWeek > 0 && weekPlatform[weekKey] >= rules.MaxPerPlatformPerWeek {
				return false
			}
			// Never publish the same material on two platforms on the same day.
			if dayPlatform[key+"|"+platform] {
				return false
			}
			// The plan must not land after the show has started.
			if key == showDay.Format("2006-01-02") {
				planned, err := time.Parse("2006-01-02 15:04", key+" "+candidateClock)
				if err != nil || planned.After(in.StartsAt) {
					return false
				}
			}
			// Never propose a slot that has already started today.
			if clockAlreadyPassed(in.Now, day, candidateClock) {
				return false
			}
			dayCount[key]++
			weekPlatform[weekKey]++
			dayPlatform[key+"|"+platform] = true
			assigned, plannedDate, clock = key, key, candidateClock
			return true
		}
		// The historical slot is tried first; without history the rule set's
		// windows rotate per item so a plan never repeats one time by default.
		start := slotRotation
		if historyClock != "" {
			for index, window := range rules.TimeWindows {
				if window == historyClock {
					start = index
					break
				}
			}
		}
		slotRotation++
		for offset := 0; offset < len(rules.TimeWindows) && clock == ""; offset++ {
			candidateClock := rules.TimeWindows[(start+offset)%len(rules.TimeWindows)]
			if fromEnd {
				for i := len(allowed) - 1; i >= 0; i-- {
					if scan(allowed[i], candidateClock) {
						break
					}
				}
			} else {
				for _, day := range allowed {
					if scan(day, candidateClock) {
						break
					}
				}
			}
		}
		if assigned == "" {
			continue
		}
		clockReason := fmt.Sprintf("样本不足，按规则集时间窗轮换选择 %s。", clock)
		if clock == historyClock && historyClock != "" {
			clockReason = historyReason
		}
		plannedDay, _ := time.Parse("2006-01-02", plannedDate)
		distance := int(showDay.Sub(plannedDay).Hours() / 24)
		referenceCount := 0
		if stats := platformStats[candidate.Category]; stats != nil {
			if stat := stats[platform]; stat != nil {
				referenceCount = stat.count
			}
		}
		reason := strings.Join([]string{
			platformReason,
			clockReason,
			fmt.Sprintf("距开演 %d 天，%s。", distance, phaseRole(candidate.Category)),
		}, " ")
		confidence, limitations := "低", "同团队同类型历史样本不足，排期主要依据规则集与内容类型搭配；请结合实际安排复核。"
		if referenceCount >= 10 {
			confidence, limitations = "高", "基于同团队历史发布数据；历史表现不保证未来结果。"
		} else if referenceCount >= 3 {
			confidence, limitations = "中", "历史样本较少，建议结合团队判断。"
		}
		result = append(result, Suggestion{
			TargetContentID:      candidate.ID,
			Title:                candidate.Title,
			ContentType:          candidate.Category,
			Platform:             platform,
			Owner:                candidate.Owner,
			PlannedDate:          plannedDate,
			DayOfWeek:            domain.WeekdayLabel(plannedDay.Weekday()),
			TimeSlot:             clock,
			Reason:               reason,
			SuggestionSource:     "整场排期（规则引擎）",
			ReferenceSampleCount: referenceCount,
			Confidence:           confidence,
			Limitations:          limitations,
		})
	}
	return result, insufficient
}

// formatNumber renders a metric with at most two decimals and no trailing zeros.
func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
