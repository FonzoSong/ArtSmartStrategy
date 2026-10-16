package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/arts-smart/strategy/internal/service"
)

const maxRecommendationContentIDs = 100

type scheduleRecommendationRequest struct {
	ProjectID     int64                               `json:"projectId"`
	ContentIDs    []string                            `json:"contentIds"`
	Strategy      string                              `json:"strategy"`
	PerformanceID string                              `json:"performanceId"`
	Constraints   service.AIRecommendationConstraints `json:"constraints"`
}

func decodeScheduleRecommendationRequest(w http.ResponseWriter, r *http.Request, teamID int64) (scheduleRecommendationRequest, int, string, string) {
	var input scheduleRecommendationRequest
	headerTeamID, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-Team-Id")), 10, 64)
	if err != nil || headerTeamID <= 0 {
		return input, http.StatusBadRequest, "team_id_required", "请求缺少有效的 X-Team-Id"
	}
	if headerTeamID != teamID {
		return input, http.StatusForbidden, "team_forbidden", "X-Team-Id 不属于当前登录团队"
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return input, http.StatusUnsupportedMediaType, "content_type_required", "请以 application/json 提交排期建议请求"
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return input, http.StatusBadRequest, "invalid_json", "请求 JSON 无效或为空"
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return input, http.StatusBadRequest, "invalid_json", "请求必须只包含一个 JSON 对象"
	}
	if input.ProjectID <= 0 {
		return input, http.StatusBadRequest, "project_id_required", "请求缺少有效的 projectId"
	}
	if input.ProjectID != teamID {
		return input, http.StatusForbidden, "project_forbidden", "项目不属于当前团队"
	}
	if input.Strategy == "" {
		input.Strategy = "balanced"
	}
	if input.Strategy != "balanced" {
		return input, http.StatusBadRequest, "invalid_strategy", "当前仅支持 balanced 排期策略"
	}
	if len(input.ContentIDs) > maxRecommendationContentIDs {
		return input, http.StatusBadRequest, "too_many_content_ids", "单次最多选择 100 条内容"
	}
	seen := make(map[string]struct{}, len(input.ContentIDs))
	for index, id := range input.ContentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return input, http.StatusBadRequest, "invalid_content_ids", "contentIds 不能包含空 ID"
		}
		if _, exists := seen[id]; exists {
			return input, http.StatusBadRequest, "invalid_content_ids", "contentIds 不能包含重复 ID"
		}
		seen[id] = struct{}{}
		input.ContentIDs[index] = id
	}
	input.PerformanceID = strings.TrimSpace(input.PerformanceID)
	if len(input.PerformanceID) > 100 {
		return input, http.StatusBadRequest, "invalid_performance_id", "performanceId 无效"
	}
	if input.Constraints.StartDate != "" {
		if _, err = time.Parse("2006-01-02", input.Constraints.StartDate); err != nil {
			return input, http.StatusBadRequest, "invalid_constraints", "constraints.startDate 必须使用 YYYY-MM-DD"
		}
	}
	if len(input.Constraints.WorkingDays) > 7 {
		return input, http.StatusBadRequest, "invalid_constraints", "constraints.workingDays 无效"
	}
	seenDays := make(map[int]struct{}, len(input.Constraints.WorkingDays))
	for _, day := range input.Constraints.WorkingDays {
		if day < 1 || day > 7 {
			return input, http.StatusBadRequest, "invalid_constraints", "constraints.workingDays 必须使用 1 到 7 表示周一到周日"
		}
		if _, exists := seenDays[day]; exists {
			return input, http.StatusBadRequest, "invalid_constraints", "constraints.workingDays 不能重复"
		}
		seenDays[day] = struct{}{}
	}
	if input.Constraints.DailyCapacityHours < 0 || input.Constraints.DailyCapacityHours > 24 {
		return input, http.StatusBadRequest, "invalid_constraints", "constraints.dailyCapacityHours 必须在 0 到 24 之间"
	}
	return input, 0, "", ""
}

func applyScheduleRecommendationConstraints(policy service.SlotPolicy, window domain.PlanningWindow, now time.Time, constraints service.AIRecommendationConstraints) (service.SlotPolicy, domain.PlanningWindow, time.Time, error) {
	if constraints.StartDate != "" {
		start, err := time.ParseInLocation("2006-01-02", constraints.StartDate, window.Start.Location())
		if err != nil || start.Before(window.Start) || start.After(window.End) {
			return policy, window, now, errors.New("constraints.startDate 必须在当前排期规则时间窗内")
		}
		window.Start = start
		policy.HorizonDays = window.Days()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if start.After(today) {
			now = start.Add(12 * time.Hour)
		}
	}
	requestedDays := make(map[string]bool, len(constraints.WorkingDays))
	for _, day := range constraints.WorkingDays {
		requestedDays[domain.WeekdayLabel(time.Weekday(day%7))] = true
	}
	if len(requestedDays) == 0 && constraints.IncludeWeekends != nil && !*constraints.IncludeWeekends {
		for _, day := range []string{"周一", "周二", "周三", "周四", "周五"} {
			requestedDays[day] = true
		}
	}
	if len(requestedDays) > 0 {
		allowedByPolicy := make(map[string]bool, len(policy.Weekdays))
		for _, day := range policy.Weekdays {
			allowedByPolicy[day] = true
		}
		weekdays := make([]string, 0, len(requestedDays))
		for _, day := range []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"} {
			if requestedDays[day] && (len(allowedByPolicy) == 0 || allowedByPolicy[day]) {
				if constraints.IncludeWeekends == nil || *constraints.IncludeWeekends || day != "周六" && day != "周日" {
					weekdays = append(weekdays, day)
				}
			}
		}
		if len(weekdays) == 0 {
			return policy, window, now, errors.New("constraints.workingDays 与当前排期规则没有可用日期")
		}
		policy.Weekdays = weekdays
	}
	// dailyCapacityHours is passed to the model as context. Publishing capacity
	// itself remains governed by the existing per-day and per-platform limits.
	return policy, window, now, nil
}
