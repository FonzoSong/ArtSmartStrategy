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

// ListSchedules returns calendar rows within an inclusive date range.
func (r *Postgres) ListSchedules(ctx context.Context, teamID int64, start, end string) ([]map[string]any, error) {
	return r.QueryMaps(ctx, `SELECT s.*,u.name AS operator,CASE WHEN s.source='manual' THEN '用户手动' ELSE '系统自动' END AS source_label FROM schedule s LEFT JOIN user_account u ON u.id=s.created_by_user_id WHERE s.team_id=$1 AND s.planned_date >= $2::date AND s.planned_date <= $3::date ORDER BY s.planned_date,s.planned_time`, teamID, start, end)
}

// CreateManualSchedule creates a schedule and logs its workflow transition.
func (r *Postgres) CreateManualSchedule(ctx context.Context, teamID, actorID int64, actor, contentID, date, clock string) (map[string]any, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var item domain.Content
	err = tx.QueryRowContext(ctx, `SELECT id,title,category,platform,owner,workflow_state,lifecycle,owner_user_id FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE`, contentID, teamID).Scan(&item.ID, &item.Title, &item.Category, &item.Platform, &item.Owner, &item.WorkflowState, &item.Lifecycle, &item.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if item.Lifecycle == domain.LifecyclePublished || item.WorkflowState == domain.WorkflowPublished {
		return nil, errors.New("已发布内容不能再次排期")
	}
	if item.WorkflowState != domain.WorkflowProductionDone && item.WorkflowState != domain.WorkflowSchedulePending && item.WorkflowState != domain.WorkflowScheduled {
		return nil, errors.New("内容需先完成制作后才能排期")
	}
	var alreadyScheduled bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schedule WHERE team_id=$1 AND content_id=$2 AND status='待发布')`, teamID, contentID).Scan(&alreadyScheduled); err != nil {
		return nil, err
	}
	if alreadyScheduled {
		return nil, errors.New("该内容已经排期，请在日历中调整现有排期")
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO schedule(id,team_id,content_id,planned_date,planned_time,title,category,platform,owner,reason,status,source,created_by_user_id,created_at,updated_at) VALUES($1,$2,$3,$4::date,$5,$6,$7,$8,$9,'用户手动安排','待发布','manual',$10,NOW(),NOW()) RETURNING id`, newUUID(), teamID, contentID, date, clock, item.Title, item.Category, item.Platform, item.Owner, actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	before := item.WorkflowState
	if before != domain.WorkflowScheduled {
		if _, err = tx.ExecContext(ctx, `UPDATE content SET workflow_state=$1,updated_at=NOW() WHERE id=$2 AND team_id=$3`, domain.WorkflowScheduled, contentID, teamID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO content_workflow_event(team_id,content_id,from_state,to_state,source,actor_user_id,created_at) VALUES($1,$2,$3,$4,'manual',$5,NOW())`, teamID, contentID, before, domain.WorkflowScheduled, actorID); err != nil {
			return nil, err
		}
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
	return rows[0], nil
}

// UpdateSchedule changes only the allow-listed date and display fields.
func (r *Postgres) UpdateSchedule(ctx context.Context, teamID int64, id string, fields map[string]any) (map[string]any, error) {
	allowed := map[string]bool{"planned_date": true, "planned_time": true, "title": true, "category": true, "platform": true, "owner": true}
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
	return rows[0], nil
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
func (r *Postgres) AdoptRecommendations(ctx context.Context, teamID, actorID int64, actor, recommendationID string, items []AdoptedSchedule, policy map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, errors.New("至少保留一条建议后再采纳")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM recommendation WHERE id=$1 AND team_id=$2 FOR UPDATE`, recommendationID, teamID).Scan(&stored)
	if err != nil {
		return 0, err
	}
	var saved struct {
		Recommendations []map[string]any `json:"recommendations"`
	}
	if err = json.Unmarshal([]byte(stored), &saved); err != nil {
		return 0, errors.New("排期建议记录格式无效")
	}
	allowedIDs := map[string]bool{}
	for _, item := range saved.Recommendations {
		if value, ok := item["target_content_id"].(string); ok {
			allowedIDs[value] = true
		}
	}
	windows := stringList(policy["time_windows"])
	weekdays := stringList(policy["weekdays"])
	daySet := map[string]bool{}
	for _, day := range weekdays {
		daySet[day] = true
	}
	horizon := intValue(policy["planning_horizon_days"], 14)
	maxDay := intValue(policy["max_per_day"], 2)
	maxWeek := intValue(policy["max_per_platform_per_week"], 2)
	today := time.Now().Format("2006-01-02")
	end := time.Now().AddDate(0, 0, horizon-1).Format("2006-01-02")
	dayCounts := map[string]int{}
	weekCounts := map[string]int{}
	existing, err := tx.QueryContext(ctx, `SELECT planned_date,platform FROM schedule WHERE team_id=$1 AND status='待发布'`, teamID)
	if err != nil {
		return 0, err
	}
	for existing.Next() {
		var date time.Time
		var platform string
		if err = existing.Scan(&date, &platform); err != nil {
			existing.Close()
			return 0, err
		}
		dayCounts[date.Format("2006-01-02")]++
		year, week := date.ISOWeek()
		weekCounts[fmt.Sprintf("%s:%d:%d", platform, year, week)]++
	}
	if err = existing.Err(); err != nil {
		existing.Close()
		return 0, err
	}
	existing.Close()
	for _, item := range items {
		if !allowedIDs[item.TargetContentID] {
			return 0, errors.New("采纳内容不属于此排期建议")
		}
		date, parseErr := time.Parse("2006-01-02", item.PlannedDate)
		if parseErr != nil || item.PlannedDate < today || item.PlannedDate > end {
			return 0, errors.New("计划日期超出规则集排期周期")
		}
		if len(daySet) > 0 && !daySet[scheduleWeekday(date.Weekday())] {
			return 0, errors.New("计划日期不符合规则集允许星期")
		}
		if !containsString(windows, item.TimeSlot) {
			return 0, errors.New("计划时间不符合规则集允许时间窗")
		}
		var title, category, platform, owner, state, lifecycle string
		err = tx.QueryRowContext(ctx, `SELECT title,category,platform,owner,workflow_state,lifecycle FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE`, item.TargetContentID, teamID).Scan(&title, &category, &platform, &owner, &state, &lifecycle)
		if err != nil {
			return 0, err
		}
		if lifecycle != domain.LifecycleUnpublished || state == domain.WorkflowPublished {
			return 0, errors.New("只能采纳未发布内容的排期建议")
		}
		var duplicate bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schedule WHERE team_id=$1 AND content_id=$2 AND status='待发布')`, teamID, item.TargetContentID).Scan(&duplicate); err != nil {
			return 0, err
		}
		if duplicate {
			return 0, errors.New("该内容已有排期，请先调整或取消原排期")
		}
		dayCounts[item.PlannedDate]++
		if maxDay > 0 && dayCounts[item.PlannedDate] > maxDay {
			return 0, errors.New("计划日期超过规则集每日发布频率")
		}
		year, week := date.ISOWeek()
		key := fmt.Sprintf("%s:%d:%d", platform, year, week)
		weekCounts[key]++
		if maxWeek > 0 && weekCounts[key] > maxWeek {
			return 0, errors.New("计划日期超过该平台每周发布频率")
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
			return 0, err
		}
		if state != domain.WorkflowScheduled {
			if _, err = tx.ExecContext(ctx, `UPDATE content SET workflow_state=$1,updated_at=NOW() WHERE id=$2 AND team_id=$3`, domain.WorkflowScheduled, item.TargetContentID, teamID); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO content_workflow_event(team_id,content_id,from_state,to_state,source,actor_user_id,created_at) VALUES($1,$2,$3,$4,'system',$5,NOW())`, teamID, item.TargetContentID, state, domain.WorkflowScheduled, actorID); err != nil {
				return 0, err
			}
		}
		if err = insertAudit(ctx, tx, teamID, actor, "adopt_schedule", "schedule", scheduleID, "采纳排期建议"); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE recommendation SET adopted=TRUE WHERE id=$1 AND team_id=$2`, recommendationID, teamID); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

func stringList(raw any) []string {
	values, _ := raw.([]any)
	if values == nil {
		strings, _ := raw.([]string)
		return strings
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
func intValue(raw any, fallback int) int {
	if value, ok := raw.(float64); ok {
		return int(value)
	}
	if value, ok := raw.(int); ok {
		return value
	}
	return fallback
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func scheduleWeekday(day time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[day]
}

// ParseScheduleDate validates an ISO date string before SQL interpolation.
func ParseScheduleDate(value string) (string, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", errors.New("排期日期格式必须为 YYYY-MM-DD")
	}
	return parsed.Format("2006-01-02"), nil
}
