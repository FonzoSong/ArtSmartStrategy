package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// AdoptedSchedule is a user-approved recommendation item.
type AdoptedSchedule struct {
	TargetContentID string `json:"target_content_id"`
	PlannedDate     string `json:"planned_date"`
	TimeSlot        string `json:"time_slot"`
	Title           string `json:"title"`
}

// Planning eligibility errors. They are distinguished from a genuine schedule
// conflict so the API can answer with a validation status instead of 409.
var (
	ErrContentNotPlannable = errors.New("内容需先完成制作后才能排期")
	ErrContentPublished    = errors.New("已发布内容不能再次排期")
	ErrAlreadyScheduled    = errors.New("该内容已经排期，请在日历中调整现有排期")
	ErrNotUnpublished      = errors.New("只能采纳未发布内容的排期建议")
	ErrNotSuggestionReady  = errors.New("排期建议仅针对待排期内容")
)

// AdoptOutcome reports how many plans were stored and how the stored
// recommendation payload looks after the adoption.
type AdoptOutcome struct {
	Created         int
	Recommendations []map[string]any
}

// ListSchedules returns calendar rows within an inclusive date range.
func (r *Postgres) ListSchedules(ctx context.Context, teamID int64, start, end string) ([]map[string]any, error) {
	rows, err := r.QueryMaps(ctx, `SELECT s.*,u.name AS operator,CASE WHEN s.source='manual' THEN '用户手动' ELSE '系统自动' END AS source_label FROM schedule s LEFT JOIN user_account u ON u.id=s.created_by_user_id WHERE s.team_id=$1 AND s.planned_date >= $2::date AND s.planned_date <= $3::date ORDER BY s.planned_date,s.planned_time`, teamID, start, end)
	if err != nil {
		return nil, err
	}
	return NormalizeScheduleRows(rows), nil
}

// PureDate renders a DATE value as YYYY-MM-DD. PostgreSQL decodes DATE into
// time.Time, which would otherwise serialize as a timestamp and break the pure
// calendar comparison used by the web client.
func PureDate(value any) string {
	switch typed := value.(type) {
	case time.Time:
		return typed.Format("2006-01-02")
	case string:
		trimmed := strings.TrimSpace(typed)
		if len(trimmed) >= 10 {
			return trimmed[:10]
		}
		return trimmed
	case []byte:
		return PureDate(string(typed))
	default:
		return ""
	}
}

// NormalizeScheduleRows applies the pure-date contract to calendar rows.
func NormalizeScheduleRows(rows []map[string]any) []map[string]any {
	for _, row := range rows {
		if _, ok := row["planned_date"]; ok {
			row["planned_date"] = PureDate(row["planned_date"])
		}
	}
	return rows
}

// ParsePlannedDate accepts a pure date or an RFC3339 timestamp and returns the
// normalized YYYY-MM-DD date. Only the calendar date is kept; no timezone
// conversion is applied.
func ParsePlannedDate(value string) (string, error) {
	candidate := strings.TrimSpace(value)
	if len(candidate) > 10 {
		candidate = candidate[:10]
	}
	return ParseScheduleDate(candidate)
}

// CreateManualSchedule creates a schedule and logs its workflow transition.
// ScheduleRequest is one manual plan. Platform, date and time are required so a
// batch never silently applies one record's setting to the others.
type ScheduleRequest struct {
	ContentID   string `json:"content_id"`
	Platform    string `json:"platform"`
	PlannedDate string `json:"planned_date"`
	PlannedTime string `json:"planned_time"`
	Owner       string `json:"owner"`
	Reason      string `json:"reason"`
}

// ScheduleResult is the per-record outcome of a manual scheduling batch.
type ScheduleResult struct {
	ID          string `json:"id"`
	ContentID   string `json:"content_id"`
	Title       string `json:"title,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	PlannedDate string `json:"planned_date,omitempty"`
	PlannedTime string `json:"planned_time,omitempty"`
}

// ScheduleOutcome reports how a scheduling request ended.
type ScheduleOutcome struct {
	Scheduled int              `json:"scheduled"`
	Failed    int              `json:"failed"`
	Results   []ScheduleResult `json:"results"`
}

func (r *Postgres) CreateManualSchedule(ctx context.Context, teamID, actorID int64, actor string, request ScheduleRequest) (map[string]any, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := scheduleRecord(ctx, tx, teamID, actorID, request)
	if err != nil {
		return nil, err
	}
	if err = insertAudit(ctx, tx, teamID, actor, "manual_schedule", "schedule", id, "创建手动排期"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	rows, err := r.QueryMaps(ctx, `SELECT s.*,u.name AS operator,'用户手动' AS source_label FROM schedule s LEFT JOIN user_account u ON u.id=s.created_by_user_id WHERE s.id=$1 AND s.team_id=$2`, id, teamID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return NormalizeScheduleRows(rows)[0], nil
}

// ScheduleContents plans several 待排期 records in one transaction. Every record
// is validated on its own slot and reports its own result, so a rejected record
// neither blocks nor silently receives another record's settings.
func (r *Postgres) ScheduleContents(
	ctx context.Context,
	teamID, actorID int64,
	actor string,
	requests []ScheduleRequest,
	rules domain.PlanningRules,
	window domain.PlanningWindow,
) (ScheduleOutcome, error) {
	outcome := ScheduleOutcome{Results: make([]ScheduleResult, 0, len(requests))}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return outcome, err
	}
	defer tx.Rollback()
	dayCounts := map[string]int{}
	weekCounts := map[string]int{}
	existing, err := tx.QueryContext(ctx, `SELECT planned_date,platform FROM schedule WHERE team_id=$1 AND status='待发布'`, teamID)
	if err != nil {
		return outcome, err
	}
	for existing.Next() {
		var date time.Time
		var platform string
		if err = existing.Scan(&date, &platform); err != nil {
			existing.Close()
			return outcome, err
		}
		dayCounts[date.Format("2006-01-02")]++
		year, week := date.ISOWeek()
		weekCounts[fmt.Sprintf("%s:%d:%d", platform, year, week)]++
	}
	if err = existing.Err(); err != nil {
		existing.Close()
		return outcome, err
	}
	existing.Close()
	seen := map[string]bool{}
	for _, request := range requests {
		date, dateErr := ParsePlannedDate(request.PlannedDate)
		if dateErr != nil {
			outcome.Failed++
			outcome.Results = append(outcome.Results, ScheduleResult{ContentID: request.ContentID, Status: "failed", Reason: "计划日期格式必须为 YYYY-MM-DD"})
			continue
		}
		if seen[request.ContentID] {
			outcome.Failed++
			outcome.Results = append(outcome.Results, ScheduleResult{ContentID: request.ContentID, Status: "failed", Reason: "同一条内容在本次请求中重复出现"})
			continue
		}
		seen[request.ContentID] = true
		planned, parseErr := time.Parse("2006-01-02", date)
		if parseErr != nil {
			outcome.Failed++
			outcome.Results = append(outcome.Results, ScheduleResult{ContentID: request.ContentID, Status: "failed", Reason: "计划日期无效"})
			continue
		}
		year, week := planned.ISOWeek()
		key := fmt.Sprintf("%s:%d:%d", request.Platform, year, week)
		dayCounts[date]++
		weekCounts[key]++
		if err = rules.ValidateSlot(window, date, request.PlannedTime, domain.SlotCounts{SameDay: dayCounts[date], SamePlatformSameISOWeek: weekCounts[key]}); err != nil {
			dayCounts[date]--
			weekCounts[key]--
			outcome.Failed++
			outcome.Results = append(outcome.Results, ScheduleResult{ContentID: request.ContentID, Status: "failed", Reason: err.Error()})
			continue
		}
		id, scheduleErr := scheduleRecord(ctx, tx, teamID, actorID, request)
		if scheduleErr != nil {
			dayCounts[date]--
			weekCounts[key]--
			outcome.Failed++
			reason := scheduleErr.Error()
			if errors.Is(scheduleErr, sql.ErrNoRows) {
				reason = "内容不存在或不属于当前团队"
			}
			outcome.Results = append(outcome.Results, ScheduleResult{ContentID: request.ContentID, Status: "failed", Reason: reason})
			continue
		}
		outcome.Scheduled++
		outcome.Results = append(outcome.Results, ScheduleResult{ID: id, ContentID: request.ContentID, Status: "scheduled", PlannedDate: date, PlannedTime: request.PlannedTime})
	}
	if outcome.Scheduled > 0 {
		if err = insertAudit(ctx, tx, teamID, actor, "manual_schedule", "schedule", "bulk", fmt.Sprintf("批量手动排期：成功 %d 条，失败 %d 条", outcome.Scheduled, outcome.Failed)); err != nil {
			return ScheduleOutcome{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return ScheduleOutcome{}, err
	}
	return outcome, nil
}

// scheduleRecord writes one plan for a 待排期 record and moves it to 已排期. The
// slot itself is validated by the caller, which owns the frequency counters.
func scheduleRecord(ctx context.Context, tx *sql.Tx, teamID, actorID int64, request ScheduleRequest) (string, error) {
	var title, category, contentPlatform, owner, state, lifecycle string
	if strings.TrimSpace(request.ContentID) == "" {
		return "", errors.New("缺少目标内容")
	}
	if strings.TrimSpace(request.Platform) == "" {
		return "", errors.New("发布平台为必填项")
	}
	err := tx.QueryRowContext(ctx, `
		SELECT title, category, platform, owner, workflow_state, lifecycle
		FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE
	`, request.ContentID, teamID).Scan(&title, &category, &contentPlatform, &owner, &state, &lifecycle)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	if lifecycle == domain.LifecyclePublished || state == domain.WorkflowPublished {
		return "", ErrContentPublished
	}
	if state != domain.WorkflowSchedulePending {
		if state == domain.WorkflowScheduled {
			return "", ErrAlreadyScheduled
		}
		return "", fmt.Errorf("仅待排期内容可以排期（当前状态：%s）", state)
	}
	var duplicate bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schedule WHERE team_id=$1 AND content_id=$2 AND status='待发布')`, teamID, request.ContentID).Scan(&duplicate); err != nil {
		return "", err
	}
	if duplicate {
		return "", ErrAlreadyScheduled
	}
	if strings.TrimSpace(request.Owner) == "" {
		request.Owner = owner
	}
	var id string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO schedule(id,team_id,content_id,planned_date,planned_time,title,category,platform,owner,reason,status,source,created_by_user_id,created_at,updated_at)
		VALUES($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,'待发布','manual',$11,NOW(),NOW()) RETURNING id
	`, newUUID(), teamID, request.ContentID, request.PlannedDate, request.PlannedTime, title, category, request.Platform, request.Owner, request.Reason, actorID).Scan(&id)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE content SET workflow_state=$1,platform=$2,owner=$3,updated_at=NOW() WHERE id=$4 AND team_id=$5`, domain.WorkflowScheduled, request.Platform, request.Owner, request.ContentID, teamID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO content_workflow_event(team_id,content_id,from_state,to_state,source,actor_user_id,created_at)
		VALUES($1,$2,$3,$4,'manual',$5,NOW())
	`, teamID, request.ContentID, domain.WorkflowSchedulePending, domain.WorkflowScheduled, actorID); err != nil {
		return "", err
	}
	return id, nil
}

// UpdateSchedule changes only the allow-listed date and display fields.
func (r *Postgres) UpdateSchedule(ctx context.Context, teamID int64, id string, fields map[string]any) (map[string]any, error) {
	// `source` and `reason` are part of the manual-adjustment record: an edited
	// plan is marked as user-adjusted and may carry the human rationale.
	allowed := map[string]bool{"planned_date": true, "planned_time": true, "title": true, "category": true, "platform": true, "owner": true, "source": true, "reason": true}
	args := []any{}
	assignments := []string{}
	for key, value := range fields {
		if allowed[key] {
			args = append(args, value)
			placeholder := "$" + strconv.Itoa(len(args))
			if key == "planned_date" {
				assignments = append(assignments, key+" = "+placeholder+"::date")
			} else {
				assignments = append(assignments, key+" = "+placeholder)
			}
		}
	}
	if len(assignments) > 0 {
		args = append(args, id, teamID)
		query := "UPDATE schedule SET " + strings.Join(assignments, ",") + ",updated_at=NOW() WHERE id=$" + strconv.Itoa(len(args)-1) + " AND team_id=$" + strconv.Itoa(len(args))
		result, err := r.DB.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, sql.ErrNoRows
		}
	}
	rows, err := r.QueryMaps(ctx, `SELECT s.*,u.name AS operator,CASE WHEN s.source='manual' THEN '用户手动' ELSE '系统自动' END AS source_label FROM schedule s LEFT JOIN user_account u ON u.id=s.created_by_user_id WHERE s.id=$1 AND s.team_id=$2`, id, teamID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return NormalizeScheduleRows(rows)[0], nil
}

// PublishSchedule stores actual results on both the plan and linked asset.
func (r *Postgres) PublishSchedule(ctx context.Context, teamID, actorID int64, actor, id string, result domain.Content) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var contentID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT content_id FROM schedule WHERE id=$1 AND team_id=$2 FOR UPDATE`, id, teamID).Scan(&contentID)
	if err != nil {
		return err
	}
	if !contentID.Valid {
		return errors.New("此排期未关联内容资产，无法回填到内容历史")
	}
	var before string
	err = tx.QueryRowContext(ctx, `SELECT workflow_state FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE`, contentID.String, teamID).Scan(&before)
	if err != nil {
		return err
	}
	if result.PublishedAt.IsZero() {
		return errors.New("实际发布时间必填")
	}
	_, err = tx.ExecContext(ctx, `UPDATE content SET title=$1,category=$2,platform=$3,published_at=$4,views=$5,likes=$6,comments=$7,saves=$8,shares=$9,completion=$10,profile_visits=$11,follows=$12,attendance=$13,tickets=$14,comment_text=$15,lifecycle=$16,workflow_state=$17,updated_at=NOW() WHERE id=$18 AND team_id=$19`, result.Title, result.Category, result.Platform, result.PublishedAt, result.Views, result.Likes, result.Comments, result.Saves, result.Shares, result.Completion, result.ProfileVisits, result.Follows, result.Attendance, result.Tickets, result.CommentText, domain.LifecyclePublished, domain.WorkflowPublished, contentID.String, teamID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE schedule SET status='已复盘',actual_views=$1,actual_likes=$2,actual_comments=$3,actual_saves=$4,actual_shares=$5,actual_completion=$6,updated_at=NOW() WHERE id=$7 AND team_id=$8`, result.Views, result.Likes, result.Comments, result.Saves, result.Shares, result.Completion, id, teamID)
	if err != nil {
		return err
	}
	if before != domain.WorkflowPublished {
		if _, err = tx.ExecContext(ctx, `INSERT INTO content_workflow_event(team_id,content_id,from_state,to_state,source,actor_user_id,created_at) VALUES($1,$2,$3,$4,'manual',$5,NOW())`, teamID, contentID.String, before, domain.WorkflowPublished, actorID); err != nil {
			return err
		}
	}
	if err = insertAudit(ctx, tx, teamID, actor, "publish_result", "content", contentID.String, "实际发布时间与表现数据已录入"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

// AdoptRecommendations atomically validates and stores user-approved plans.
// The whole batch shares one transaction, so a rejected item leaves no partial
// writes behind. Every item is validated against the same planning rules that
// manual scheduling and recommendation generation use.
func (r *Postgres) AdoptRecommendations(ctx context.Context, teamID, actorID int64, actor, recommendationID string, items []AdoptedSchedule, rules domain.PlanningRules) (AdoptOutcome, error) {
	if len(items) == 0 {
		return AdoptOutcome{}, errors.New("至少保留一条建议后再采纳")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return AdoptOutcome{}, err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM recommendation WHERE id=$1 AND team_id=$2 FOR UPDATE`, recommendationID, teamID).Scan(&stored)
	if err != nil {
		return AdoptOutcome{}, err
	}
	var saved struct {
		Recommendations []map[string]any `json:"recommendations"`
	}
	if err = json.Unmarshal([]byte(stored), &saved); err != nil {
		return AdoptOutcome{}, errors.New("排期建议记录格式无效")
	}
	allowedIDs := map[string]bool{}
	for _, item := range saved.Recommendations {
		if value, ok := item["target_content_id"].(string); ok {
			allowedIDs[value] = true
		}
	}
	window, windowOK := domain.HorizonWindow(time.Now(), rules.HorizonDays)
	if !windowOK {
		return AdoptOutcome{}, errors.New("排期规则集格式无效：planning_horizon_days 超出支持范围")
	}
	dayCounts := map[string]int{}
	weekCounts := map[string]int{}
	existing, err := tx.QueryContext(ctx, `SELECT planned_date,platform FROM schedule WHERE team_id=$1 AND status='待发布'`, teamID)
	if err != nil {
		return AdoptOutcome{}, err
	}
	for existing.Next() {
		var date time.Time
		var platform string
		if err = existing.Scan(&date, &platform); err != nil {
			existing.Close()
			return AdoptOutcome{}, err
		}
		dayCounts[date.Format("2006-01-02")]++
		year, week := date.ISOWeek()
		weekCounts[fmt.Sprintf("%s:%d:%d", platform, year, week)]++
	}
	if err = existing.Err(); err != nil {
		existing.Close()
		return AdoptOutcome{}, err
	}
	existing.Close()
	adoptedScheduleIDs := map[string]string{}
	for _, item := range items {
		if !allowedIDs[item.TargetContentID] {
			return AdoptOutcome{}, errors.New("采纳内容不属于此排期建议")
		}
		date, parseErr := ParsePlannedDate(item.PlannedDate)
		if parseErr != nil {
			return AdoptOutcome{}, parseErr
		}
		item.PlannedDate = date
		var title, category, platform, owner, state, lifecycle string
		err = tx.QueryRowContext(ctx, `SELECT title,category,platform,owner,workflow_state,lifecycle FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE`, item.TargetContentID, teamID).Scan(&title, &category, &platform, &owner, &state, &lifecycle)
		if err != nil {
			return AdoptOutcome{}, err
		}
		if lifecycle != domain.LifecycleUnpublished || state == domain.WorkflowPublished {
			return AdoptOutcome{}, ErrNotUnpublished
		}
		if !domain.CanSuggestSchedule(lifecycle, state) {
			return AdoptOutcome{}, ErrNotSuggestionReady
		}
		var duplicate bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schedule WHERE team_id=$1 AND content_id=$2 AND status='待发布')`, teamID, item.TargetContentID).Scan(&duplicate); err != nil {
			return AdoptOutcome{}, err
		}
		if duplicate {
			return AdoptOutcome{}, errors.New("该内容已有排期，请先调整或取消原排期")
		}
		dayCounts[item.PlannedDate]++
		planned, parseErr := time.Parse("2006-01-02", date)
		if parseErr != nil {
			return AdoptOutcome{}, domain.ErrSlotDateInvalid
		}
		year, week := planned.ISOWeek()
		key := fmt.Sprintf("%s:%d:%d", platform, year, week)
		weekCounts[key]++
		if err = rules.ValidateSlot(window, item.PlannedDate, item.TimeSlot, domain.SlotCounts{SameDay: dayCounts[item.PlannedDate], SamePlatformSameISOWeek: weekCounts[key]}); err != nil {
			return AdoptOutcome{}, err
		}
		if len(item.Title) > 300 {
			item.Title = title
		}
		if item.Title == "" {
			item.Title = title
		}
		var scheduleID string
		err = tx.QueryRowContext(ctx, `INSERT INTO schedule(id,team_id,recommendation_id,content_id,planned_date,planned_time,title,category,platform,owner,reason,status,source,created_by_user_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,'采纳排期建议','待发布','auto',$11,NOW(),NOW()) RETURNING id`, newUUID(), teamID, recommendationID, item.TargetContentID, item.PlannedDate, item.TimeSlot, item.Title, category, platform, owner, actorID).Scan(&scheduleID)
		if err != nil {
			return AdoptOutcome{}, err
		}
		adoptedScheduleIDs[item.TargetContentID] = scheduleID
		if state != domain.WorkflowScheduled {
			if _, err = tx.ExecContext(ctx, `UPDATE content SET workflow_state=$1,updated_at=NOW() WHERE id=$2 AND team_id=$3`, domain.WorkflowScheduled, item.TargetContentID, teamID); err != nil {
				return AdoptOutcome{}, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO content_workflow_event(team_id,content_id,from_state,to_state,source,actor_user_id,created_at) VALUES($1,$2,$3,$4,'system',$5,NOW())`, teamID, item.TargetContentID, state, domain.WorkflowScheduled, actorID); err != nil {
				return AdoptOutcome{}, err
			}
		}
		if err = insertAudit(ctx, tx, teamID, actor, "adopt_schedule", "schedule", scheduleID, "采纳排期建议"); err != nil {
			return AdoptOutcome{}, err
		}
	}
	// Keep remaining suggestions intact and mark only the adopted ones, so a
	// single adoption never hides or resets the rest of the group.
	for _, item := range saved.Recommendations {
		contentID, _ := item["target_content_id"].(string)
		if scheduleID, ok := adoptedScheduleIDs[contentID]; ok {
			item["adopted"] = true
			item["schedule_id"] = scheduleID
		}
	}
	updatedPayload, err := json.Marshal(map[string]any{"recommendations": saved.Recommendations})
	if err != nil {
		return AdoptOutcome{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE recommendation SET payload_json=$1,adopted=TRUE WHERE id=$2 AND team_id=$3`, string(updatedPayload), recommendationID, teamID); err != nil {
		return AdoptOutcome{}, err
	}
	if err = tx.Commit(); err != nil {
		return AdoptOutcome{}, err
	}
	return AdoptOutcome{Created: len(items), Recommendations: saved.Recommendations}, nil
}

// ParseScheduleDate validates an ISO date string before SQL interpolation.
func ParseScheduleDate(value string) (string, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", errors.New("排期日期格式必须为 YYYY-MM-DD")
	}
	return parsed.Format("2006-01-02"), nil
}
