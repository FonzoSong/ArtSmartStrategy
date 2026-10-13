package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// performanceProjection is the shared 正式演出 row shape. Every counter is derived
// from persisted content and schedule rows for the same team, so the list and the
// detail page can never disagree and no number is invented.
const performanceProjection = `
SELECT p.*, COALESCE(u.name, '') AS owner_name,
       COUNT(c.id) FILTER (WHERE c.id IS NOT NULL) AS content_count,
       COUNT(c.id) FILTER (WHERE c.id IS NOT NULL AND c.lifecycle = '已发布') AS published_content_count,
       COUNT(c.id) FILTER (WHERE c.id IS NOT NULL AND c.lifecycle = '未发布') AS unpublished_content_count,
       COUNT(c.id) FILTER (WHERE c.id IS NOT NULL AND c.lifecycle = '未发布' AND c.workflow_state = '待排期') AS schedulable_content_count,
       COUNT(c.id) FILTER (WHERE c.id IS NOT NULL AND c.lifecycle = '未发布' AND EXISTS (
           SELECT 1 FROM schedule s
           WHERE s.team_id = c.team_id AND s.content_id = c.id AND s.status = '待发布'
       )) AS scheduled_content_count
FROM performance_booking p
LEFT JOIN user_account u ON u.id = p.owner_user_id
LEFT JOIN content c ON c.performance_id = p.id AND c.team_id = p.team_id`

// ListPerformances returns the current team's 正式演出 with their real linked-content
// counters. Upcoming shows come first, then past shows newest first.
func (r *Postgres) ListPerformances(ctx context.Context, teamID int64) ([]map[string]any, error) {
	rows, err := r.QueryMaps(ctx, performanceProjection+`
 WHERE p.team_id = $1
 GROUP BY p.id, u.name
 ORDER BY (p.starts_at >= LOCALTIMESTAMP) DESC,
          CASE WHEN p.starts_at >= LOCALTIMESTAMP THEN p.starts_at END ASC,
          p.starts_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []map[string]any{}, nil
	}
	return rows, nil
}

// GetPerformance returns one team-scoped 正式演出 with its counters.
func (r *Postgres) GetPerformance(ctx context.Context, teamID int64, id string) (map[string]any, error) {
	rows, err := r.QueryMaps(ctx, performanceProjection+`
 WHERE p.team_id = $1 AND p.id = $2
 GROUP BY p.id, u.name`, teamID, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}

// GetPerformanceContent returns the promotional videos already grouped under one
// 正式演出: the published ones with their real effect data, and the unpublished
// ones with their workflow state so the page can show what still blocks a plan.
func (r *Postgres) GetPerformanceContent(ctx context.Context, teamID int64, performanceID string) (map[string][]map[string]any, error) {
	published, err := r.QueryMaps(ctx, `
		SELECT id, title, category, platform, published_at, views, likes, comments,
		       saves, shares, completion, total_interactions, owner,
		       profile_visits, follows, attendance, tickets
		FROM content
		WHERE team_id = $1 AND performance_id = $2 AND lifecycle = '已发布'
		ORDER BY published_at DESC, id`, teamID, performanceID)
	if err != nil {
		return nil, err
	}
	unpublished, err := r.QueryMaps(ctx, `
		SELECT c.id, c.title, c.category, c.platform, c.workflow_state, c.lifecycle,
		       c.owner, c.created_at, c.updated_at,
		       COALESCE(s.planned_date, NULL) AS planned_date,
		       COALESCE(s.planned_time, '') AS planned_time,
		       COALESCE(s.status, '') AS schedule_status
		FROM content c
		LEFT JOIN schedule s
		       ON s.content_id = c.id AND s.team_id = c.team_id AND s.status = '待发布'
		WHERE c.team_id = $1 AND c.performance_id = $2 AND c.lifecycle = '未发布'
		ORDER BY c.created_at DESC, c.id`, teamID, performanceID)
	if err != nil {
		return nil, err
	}
	timeline, err := r.QueryMaps(ctx, `
		SELECT s.id, s.content_id, s.planned_date, s.planned_time, s.title, s.category,
		       s.platform, s.status, s.source, s.reason, s.owner,
		       CASE WHEN s.source = 'manual' THEN '用户手动' ELSE '系统自动' END AS source_label
		FROM schedule s
		JOIN content c ON c.id = s.content_id AND c.team_id = s.team_id
		WHERE c.team_id = $1 AND c.performance_id = $2
		ORDER BY s.planned_date, s.planned_time, s.id`, teamID, performanceID)
	if err != nil {
		return nil, err
	}
	if published == nil {
		published = []map[string]any{}
	}
	if unpublished == nil {
		unpublished = []map[string]any{}
	}
	return map[string][]map[string]any{
		"published":   published,
		"unpublished": unpublished,
		"timeline":    NormalizeScheduleRows(timeline),
	}, nil
}

// SettlePerformanceStatuses applies the server-side expiry rule: a 筹备中 or 预热中
// show whose start time has passed becomes 已演出. 已取消 is terminal and is never
// rewritten. `now` comes from the application rather than the database session, so
// show times entered in the team's wall clock are compared against the same wall
// clock. It returns the number of rows it changed.
func (r *Postgres) SettlePerformanceStatuses(ctx context.Context, teamID int64) (int64, error) {
	result, err := r.DB.ExecContext(ctx, `
		UPDATE performance_booking
		SET booking_status = '已演出'
		WHERE team_id = $1
		  AND booking_status IN ('筹备中', '预热中')
		  AND starts_at <= $2`, teamID, time.Now())
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return affected, err
}

// validatePerformance checks the 正式演出 contract shared by create and update.
func validatePerformance(input domain.Performance) error {
	if strings.TrimSpace(input.Title) == "" || len([]rune(strings.TrimSpace(input.Title))) > 300 {
		return errors.New("演出名称不能为空且最多 300 个字符")
	}
	if input.StartsAt.IsZero() {
		return errors.New("演出时间为必填项")
	}
	if len([]rune(input.Venue)) > 200 {
		return errors.New("演出地点最多 200 个字符")
	}
	if len([]rune(input.PerformanceType)) > 60 {
		return errors.New("演出类型最多 60 个字符")
	}
	if !domain.IsPerformanceStatus(input.BookingStatus) {
		return fmt.Errorf("演出状态必须是 %s", strings.Join(domain.PerformanceStatuses(), "、"))
	}
	return nil
}

// resolvePerformanceOwner fills Owner from the team-scoped account so the display
// name always belongs to the caller's team.
func resolvePerformanceOwner(ctx context.Context, tx *sql.Tx, teamID int64, input *domain.Performance) error {
	if input.OwnerUserID == nil {
		input.Owner = ""
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT name FROM user_account WHERE id=$1 AND team_id=$2`, *input.OwnerUserID, teamID).Scan(&input.Owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("负责人不存在或不属于当前团队")
		}
		return err
	}
	return nil
}

// CreatePerformanceBooking records a team-owned 正式演出 and its audit row.
func (r *Postgres) CreatePerformanceBooking(ctx context.Context, teamID, actorID int64, actor string, input domain.Performance) (map[string]any, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Venue = strings.TrimSpace(input.Venue)
	input.PerformanceType = strings.TrimSpace(input.PerformanceType)
	input.TicketLink = strings.TrimSpace(input.TicketLink)
	if input.BookingStatus == "" {
		input.BookingStatus = domain.PerformancePreparing
	}
	if err := validatePerformance(input); err != nil {
		return nil, err
	}
	if input.ID == "" {
		input.ID = newUUID()
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = resolvePerformanceOwner(ctx, tx, teamID, &input); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO performance_booking
			(id, team_id, title, venue, starts_at, booking_status, performance_type, ticket_link, owner_user_id, notes, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())`,
		input.ID, teamID, input.Title, input.Venue, input.StartsAt, input.BookingStatus,
		input.PerformanceType, input.TicketLink, input.OwnerUserID, input.Notes); err != nil {
		return nil, err
	}
	if err = insertAudit(ctx, tx, teamID, actor, "create", "performance", input.ID, input.Title); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPerformance(ctx, teamID, input.ID)
}

// UpdatePerformanceBooking applies an allow-listed partial update to one show.
// Only the fields the caller actually sends are written, so a partial edit never
// clears the rest of the record.
func (r *Postgres) UpdatePerformanceBooking(ctx context.Context, teamID int64, actor, id string, input domain.Performance, fields map[string]bool) (map[string]any, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current domain.Performance
	var ownerID sql.NullInt64
	if err = tx.QueryRowContext(ctx, `
		SELECT id, title, venue, starts_at, booking_status, performance_type, ticket_link, owner_user_id, notes
		FROM performance_booking WHERE id=$1 AND team_id=$2 FOR UPDATE`, id, teamID,
	).Scan(&current.ID, &current.Title, &current.Venue, &current.StartsAt, &current.BookingStatus,
		&current.PerformanceType, &current.TicketLink, &ownerID, &current.Notes); err != nil {
		return nil, err
	}
	if ownerID.Valid {
		current.OwnerUserID = &ownerID.Int64
	}
	if fields["title"] {
		current.Title = input.Title
	}
	if fields["venue"] {
		current.Venue = input.Venue
	}
	if fields["starts_at"] {
		current.StartsAt = input.StartsAt
	}
	if fields["booking_status"] {
		current.BookingStatus = input.BookingStatus
	}
	if fields["performance_type"] {
		current.PerformanceType = input.PerformanceType
	}
	if fields["ticket_link"] {
		current.TicketLink = input.TicketLink
	}
	if fields["notes"] {
		current.Notes = input.Notes
	}
	if fields["owner_user_id"] {
		current.OwnerUserID = input.OwnerUserID
	}
	current.Title = strings.TrimSpace(current.Title)
	current.Venue = strings.TrimSpace(current.Venue)
	current.PerformanceType = strings.TrimSpace(current.PerformanceType)
	current.TicketLink = strings.TrimSpace(current.TicketLink)
	if err = validatePerformance(current); err != nil {
		return nil, err
	}
	if err = resolvePerformanceOwner(ctx, tx, teamID, &current); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE performance_booking
		SET title=$1, venue=$2, starts_at=$3, booking_status=$4, performance_type=$5,
		    ticket_link=$6, owner_user_id=$7, notes=$8
		WHERE id=$9 AND team_id=$10`,
		current.Title, current.Venue, current.StartsAt, current.BookingStatus, current.PerformanceType,
		current.TicketLink, current.OwnerUserID, current.Notes, id, teamID); err != nil {
		return nil, err
	}
	if err = insertAudit(ctx, tx, teamID, actor, "update", "performance", id, current.Title); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPerformance(ctx, teamID, id)
}

// DeletePerformanceBooking removes one show. Linked promotional videos are kept
// and only lose the association, so deleting a show never deletes content data.
func (r *Postgres) DeletePerformanceBooking(ctx context.Context, teamID int64, actor, id string) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var title string
	if err = tx.QueryRowContext(ctx, `SELECT title FROM performance_booking WHERE id=$1 AND team_id=$2 FOR UPDATE`, id, teamID).Scan(&title); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE content SET performance_id=NULL, updated_at=NOW() WHERE team_id=$1 AND performance_id=$2`, teamID, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM performance_booking WHERE id=$1 AND team_id=$2`, id, teamID); err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, teamID, actor, "delete", "performance", id, title); err != nil {
		return err
	}
	return tx.Commit()
}

// LinkContentPerformance attaches or detaches one promotional video. The content
// and the target show must both belong to the caller's team, which keeps the
// "one video belongs to at most one show" rule inside a single team.
func (r *Postgres) LinkContentPerformance(ctx context.Context, teamID int64, actor, contentID string, performanceID *string) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM content WHERE id=$1 AND team_id=$2)`, contentID, teamID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	if performanceID != nil {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM performance_booking WHERE id=$1 AND team_id=$2)`, *performanceID, teamID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("关联演出不存在或不属于当前团队")
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE content SET performance_id=$1, updated_at=NOW() WHERE id=$2 AND team_id=$3`, performanceID, contentID, teamID); err != nil {
		return err
	}
	detail := "解除演出关联"
	if performanceID != nil {
		detail = "关联到演出 " + *performanceID
	}
	if err = insertAudit(ctx, tx, teamID, actor, "link_performance", "content", contentID, detail); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSchedulesByPerformance returns the calendar rows of one 正式演出 only. It
// filters through content.performance_id, so the calendar can be scoped to a show
// without creating a second scheduling model.
func (r *Postgres) ListSchedulesByPerformance(ctx context.Context, teamID int64, performanceID, start, end string) ([]map[string]any, error) {
	rows, err := r.QueryMaps(ctx, `
		SELECT s.*, u.name AS operator,
		       CASE WHEN s.source='manual' THEN '用户手动' ELSE '系统自动' END AS source_label
		FROM schedule s
		JOIN content c ON c.id = s.content_id AND c.team_id = s.team_id
		LEFT JOIN user_account u ON u.id = s.created_by_user_id
		WHERE s.team_id = $1 AND c.performance_id = $2
		  AND s.planned_date >= $3::date AND s.planned_date <= $4::date
		ORDER BY s.planned_date, s.planned_time`, teamID, performanceID, start, end)
	if err != nil {
		return nil, err
	}
	return NormalizeScheduleRows(rows), nil
}

// LogScheduleAdjustment records a manual plan change with its before/after values
// so later analysis can separate human decisions from generated plans.
func (r *Postgres) LogScheduleAdjustment(ctx context.Context, teamID int64, actor, scheduleID, before, after string) error {
	_, err := r.DB.ExecContext(ctx, `
		INSERT INTO audit_log (team_id, actor, action, entity_type, entity_id, detail, created_at)
		VALUES ($1,$2,'adjust_schedule','schedule',$3,$4,NOW())`,
		teamID, actor, scheduleID, strings.Join([]string{"调整前 " + before, "调整后 " + after}, "；"))
	return err
}

// ScheduleAdjustments lists the manual changes recorded for one plan.
func (r *Postgres) ScheduleAdjustments(ctx context.Context, teamID int64, scheduleID string) ([]map[string]any, error) {
	rows, err := r.QueryMaps(ctx, `
		SELECT actor, detail, created_at FROM audit_log
		WHERE team_id=$1 AND entity_type='schedule' AND entity_id=$2 AND action='adjust_schedule'
		ORDER BY created_at DESC, id DESC`, teamID, scheduleID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []map[string]any{}, nil
	}
	return rows, nil
}
