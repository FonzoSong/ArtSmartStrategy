package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// NormalizeTag applies compatibility normalization and validates tag syntax.
func NormalizeTag(value string) (string, string, error) {
	name := strings.TrimSpace(norm.NFKC.String(value))
	if name == "" || len([]rune(name)) > 50 {
		return "", "", errors.New("Tag 名称不能为空且不超过 50 个字符")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '_' && r != '-' && r != '#' {
			return "", "", errors.New("Tag 仅允许文字、数字、下划线、连字符和 #")
		}
	}
	return name, cases.Fold().String(name), nil
}

// CreateContent saves an asset, its tags, and its initial workflow event.
func (r *Postgres) CreateContent(
	ctx context.Context,
	teamID, actorID int64,
	actorName string,
	content domain.Content,
) (map[string]any, error) {
	if content.ID == "" {
		content.ID = newUUID()
	}
	if !uuidPattern.MatchString(content.ID) {
		return nil, errors.New("内容 ID 格式无效")
	}
	if err := validateContent(content); err != nil {
		return nil, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := validateOwnerAndPerformance(ctx, tx, teamID, content); err != nil {
		return nil, err
	}
	tagsJSON, err := json.Marshal(content.Tags)
	if err != nil {
		return nil, err
	}
	content.TeamID = teamID
	content.TotalInteractions = content.Likes + content.Comments + content.Saves + content.Shares
	if content.CreatedBy == "" {
		content.CreatedBy = actorName
	}
	if content.Source == "" {
		content.Source = "manual"
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO content (
			id, team_id, title, category, platform, published_at, duration,
			views, likes, comments, saves, shares, completion, total_interactions,
			tags_json, source, lifecycle, materials, cover_url, owner, notes,
			profile_visits, follows, attendance, tickets, comment_text, adopted,
			recommendation_id, performance_id, owner_user_id, workflow_state,
			created_by, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
			$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,NOW(),NOW()
		)
	`, content.ID, teamID, content.Title, content.Category, content.Platform,
		content.PublishedAt, content.Duration, content.Views, content.Likes,
		content.Comments, content.Saves, content.Shares, content.Completion,
		content.TotalInteractions, string(tagsJSON), content.Source,
		content.Lifecycle, content.Materials, content.CoverURL, content.Owner,
		content.Notes, content.ProfileVisits, content.Follows, content.Attendance,
		content.Tickets, content.CommentText, content.Adopted,
		content.RecommendationID, content.PerformanceID, content.OwnerUserID,
		content.WorkflowState, content.CreatedBy); err != nil {
		return nil, err
	}
	if err = syncContentTags(ctx, tx, teamID, actorID, content.ID, content.Tags); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO content_workflow_event
		(team_id, content_id, from_state, to_state, source, actor_user_id, created_at)
		VALUES ($1,$2,'',$3,'manual',$4,NOW())
	`, teamID, content.ID, content.WorkflowState, actorID); err != nil {
		return nil, err
	}
	if err = insertAudit(ctx, tx, teamID, actorName, "create", "content", content.ID, content.Title); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetContent(ctx, teamID, content.ID)
}

// UpdateContent applies an allow-listed partial update and logs state changes.
func (r *Postgres) UpdateContent(
	ctx context.Context,
	teamID, actorID int64,
	actorName, contentID string,
	values map[string]any,
) (map[string]any, error) {
	if !uuidPattern.MatchString(contentID) {
		return nil, sql.ErrNoRows
	}
	// Workflow state and publication outcome are not directly editable: every
	// state change must go through the validated workflow transitions.
	allowed := map[string]bool{
		"title": true, "category": true, "platform": true,
		"published_at": true, "duration": true, "views": true, "likes": true,
		"comments": true, "saves": true, "shares": true, "completion": true,
		"source": true, "materials": true, "cover_url": true,
		"owner": true, "notes": true, "profile_visits": true, "follows": true,
		"attendance": true, "tickets": true, "comment_text": true,
		"adopted": true, "recommendation_id": true, "performance_id": true,
		"owner_user_id": true,
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var beforeState, beforeLifecycle string
	err = tx.QueryRowContext(ctx, `
		SELECT workflow_state, lifecycle FROM content WHERE id = $1 AND team_id = $2 FOR UPDATE
	`, contentID, teamID).Scan(&beforeState, &beforeLifecycle)
	if err != nil {
		return nil, err
	}
	var assignments []string
	var args []any
	for key, value := range values {
		if !allowed[key] {
			continue
		}
		if (key == "recommendation_id" || key == "performance_id" || key == "owner_user_id") && value == "" {
			value = nil
		}
		if key == "owner_user_id" && value != nil {
			numeric, ok := value.(float64)
			if !ok {
				return nil, errors.New("负责人 ID 无效")
			}
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_account WHERE id=$1 AND team_id=$2)`, int64(numeric), teamID).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, errors.New("负责人不存在或不属于当前团队")
			}
			value = int64(numeric)
		}
		if key == "performance_id" && value != nil {
			performanceID, ok := value.(string)
			if !ok {
				return nil, errors.New("关联演出 ID 无效")
			}
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM performance_booking WHERE id=$1 AND team_id=$2)`, performanceID, teamID).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, errors.New("关联演出不存在或不属于当前团队")
			}
		}
		if key == "published_at" {
			if raw, ok := value.(string); ok {
				var parsed time.Time
				for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02 15:04:05"} {
					if candidate, parseErr := time.Parse(layout, raw); parseErr == nil {
						parsed = candidate
						break
					}
				}
				if parsed.IsZero() {
					return nil, errors.New("发布时间格式无效")
				}
				value = parsed
			}
		}
		if key == "duration" || key == "views" || key == "likes" || key == "comments" || key == "saves" || key == "shares" || key == "profile_visits" || key == "follows" || key == "attendance" || key == "tickets" {
			if numeric, ok := value.(float64); ok {
				if math.Trunc(numeric) != numeric {
					return nil, fmt.Errorf("%s 必须是整数", key)
				}
				value = int64(numeric)
			}
		}
		args = append(args, value)
		assignments = append(assignments, fmt.Sprintf("%s = $%d", key, len(args)))
	}
	// The workflow state and lifecycle cannot change here, so they stay consistent
	// with the transition rules by construction.
	if len(assignments) > 0 {
		args = append(args, contentID, teamID)
		query := "UPDATE content SET " + strings.Join(assignments, ", ") +
			", updated_at = NOW() WHERE id = $" + fmt.Sprint(len(args)-1) +
			" AND team_id = $" + fmt.Sprint(len(args))
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}
	if tags, ok := values["tags"].([]any); ok {
		names := make([]string, 0, len(tags))
		for _, item := range tags {
			if name, ok := item.(string); ok {
				names = append(names, name)
			}
		}
		if err = syncContentTags(ctx, tx, teamID, actorID, contentID, names); err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(names)
		if _, err = tx.ExecContext(ctx, `
			UPDATE content SET tags_json = $1, updated_at = NOW()
			WHERE id = $2 AND team_id = $3
		`, string(encoded), contentID, teamID); err != nil {
			return nil, err
		}
	}
	var currentState string
	if err = tx.QueryRowContext(ctx, `
		SELECT workflow_state FROM content WHERE id = $1 AND team_id = $2
	`, contentID, teamID).Scan(&currentState); err != nil {
		return nil, err
	}
	if beforeState != currentState {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO content_workflow_event
			(team_id, content_id, from_state, to_state, source, actor_user_id, created_at)
			VALUES ($1,$2,$3,$4,'manual',$5,NOW())
		`, teamID, contentID, beforeState, currentState, actorID); err != nil {
			return nil, err
		}
	}
	if err = insertAudit(ctx, tx, teamID, actorName, "update", "content", contentID, "内容记录更新"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetContent(ctx, teamID, contentID)
}

// DeleteContent removes a team-owned content record and linked workflow rows.
// ContentCategoryCounts returns one count per content records category, using the
// same semantics as the category filter: workflow state for the pipeline stages
// and lifecycle for the derived 未发布/已发布 views. The aggregation happens in the
// database, so the numbers never depend on the current page.
func (r *Postgres) ContentCategoryCounts(ctx context.Context, teamID int64) (map[string]int64, error) {
	rows, err := r.QueryMaps(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE workflow_state = '待制作') AS drafting,
			COUNT(*) FILTER (WHERE workflow_state = '已制作') AS produced,
			COUNT(*) FILTER (WHERE workflow_state = '待排期') AS scheduling,
			COUNT(*) FILTER (WHERE workflow_state = '已排期') AS scheduled,
			COUNT(*) FILTER (WHERE lifecycle = '未发布') AS unpublished,
			COUNT(*) FILTER (WHERE lifecycle = '已发布') AS published,
			COUNT(*) AS total
		FROM content WHERE team_id = $1
	`, teamID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, category := range domain.CategoryOrder() {
		counts[string(category)] = 0
	}
	if len(rows) == 0 {
		return counts, nil
	}
	row := rows[0]
	counts[string(domain.CategoryDrafting)] = intColumn(row, "drafting")
	counts[string(domain.CategoryProduced)] = intColumn(row, "produced")
	counts[string(domain.CategoryScheduling)] = intColumn(row, "scheduling")
	counts[string(domain.CategoryScheduled)] = intColumn(row, "scheduled")
	counts[string(domain.CategoryUnpublished)] = intColumn(row, "unpublished")
	counts[string(domain.CategoryPublished)] = intColumn(row, "published")
	counts[string(domain.CategoryAll)] = intColumn(row, "total")
	return counts, nil
}

// intColumn reads a non-null integer column produced by QueryMaps.
func intColumn(row map[string]any, key string) int64 {
	switch typed := row[key].(type) {
	case int64:
		return typed
	case int32:
		return int64(typed)
	case int:
		return int64(typed)
	default:
		return 0
	}
}

func (r *Postgres) DeleteContent(
	ctx context.Context,
	teamID int64,
	actor string,
	contentID string,
) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A content record owns its publishing plans: delete them first so the
	// schedule foreign key does not block the deletion, and report how many were
	// removed together with the record.
	plans, err := tx.ExecContext(ctx, `DELETE FROM schedule WHERE team_id = $1 AND content_id = $2`, teamID, contentID)
	if err != nil {
		return err
	}
	removedPlans, err := plans.RowsAffected()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM content WHERE id = $1 AND team_id = $2
	`, contentID, teamID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if err = insertAudit(ctx, tx, teamID, actor, "delete", "content", contentID,
		fmt.Sprintf("内容记录已删除；同时移除 %d 条关联排期", removedPlans)); err != nil {
		return err
	}
	return tx.Commit()
}

// ContentWorkflowState returns the persisted workflow state of one record.
func (r *Postgres) ContentWorkflowState(ctx context.Context, teamID int64, contentID string) (string, error) {
	var state string
	err := r.DB.QueryRowContext(ctx, `SELECT workflow_state FROM content WHERE id=$1 AND team_id=$2`, contentID, teamID).Scan(&state)
	return state, err
}

// AdvanceWorkflow moves one record to its next workflow state. It uses the same
// transition rule and field whitelist as the batch endpoint.
func (r *Postgres) AdvanceWorkflow(
	ctx context.Context,
	teamID, actorID int64,
	actor, contentID string,
	fields map[string]any,
) (map[string]any, error) {
	current, err := r.ContentWorkflowState(ctx, teamID, contentID)
	if err != nil {
		return nil, err
	}
	target, ok := domain.NextWorkflowState(current)
	if !ok {
		return nil, fmt.Errorf("当前状态为「%s」，没有可推进的下一状态", current)
	}
	outcome, err := r.TransitionContents(ctx, teamID, actorID, actor, target, []TransitionItem{{ID: contentID, Fields: fields}})
	if err != nil {
		return nil, err
	}
	for _, result := range outcome.Results {
		if result.Status != "advanced" {
			return nil, errors.New(result.Reason)
		}
	}
	return r.GetContent(ctx, teamID, contentID)
}

func validateContent(content domain.Content) error {
	if strings.TrimSpace(content.Title) == "" || len([]rune(content.Title)) > 300 {
		return errors.New("标题不能为空且最多 300 个字符")
	}
	if strings.TrimSpace(content.Platform) == "" || content.PublishedAt.IsZero() {
		return errors.New("平台和发布时间为必填项")
	}
	if content.Lifecycle != domain.LifecyclePublished && content.Lifecycle != domain.LifecycleUnpublished {
		return errors.New("内容状态只能是已发布或未发布")
	}
	switch content.WorkflowState {
	case domain.WorkflowProductionPending, domain.WorkflowProductionDone,
		domain.WorkflowSchedulePending, domain.WorkflowScheduled,
		domain.WorkflowPublished:
	default:
		return errors.New("工作流状态无效")
	}
	if content.Lifecycle == domain.LifecyclePublished && content.WorkflowState != domain.WorkflowPublished {
		return errors.New("已发布状态必须使用已发布工作流")
	}
	if content.Lifecycle == domain.LifecycleUnpublished && content.WorkflowState == domain.WorkflowPublished {
		return errors.New("已发布工作流必须使用已发布状态")
	}
	if content.Duration < 0 || content.Views < 0 || content.Likes < 0 ||
		content.Comments < 0 || content.Saves < 0 || content.Shares < 0 ||
		content.Completion < 0 || content.Completion > 100 {
		return errors.New("内容指标不能为负数，完播率范围为 0–100")
	}
	return nil
}

func validateOwnerAndPerformance(
	ctx context.Context,
	tx *sql.Tx,
	teamID int64,
	content domain.Content,
) error {
	if content.OwnerUserID != nil {
		var ownerName string
		if err := tx.QueryRowContext(ctx, `
			SELECT name FROM user_account WHERE id = $1 AND team_id = $2
		`, *content.OwnerUserID, teamID).Scan(&ownerName); err != nil {
			return errors.New("负责人不存在或不属于当前团队")
		}
		content.Owner = ownerName
	}
	if content.PerformanceID != nil {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM performance_booking WHERE id = $1 AND team_id = $2)
		`, *content.PerformanceID, teamID).Scan(&exists); err != nil || !exists {
			return errors.New("关联演出不存在或不属于当前团队")
		}
	}
	return nil
}

func syncContentTags(
	ctx context.Context,
	tx *sql.Tx,
	teamID, actorID int64,
	contentID string,
	tags []string,
) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM content_tag WHERE content_id = $1`, contentID); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(tags))
	for _, candidate := range tags {
		name, normalized, err := NormalizeTag(candidate)
		if err != nil {
			return err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		var tagID int64
		err = tx.QueryRowContext(ctx, `
			INSERT INTO tag (team_id, name, normalized_name, color, is_active, created_by_user_id, created_at)
			VALUES ($1,$2,$3,'#647383',TRUE,$4,NOW())
			ON CONFLICT (team_id, normalized_name) DO UPDATE
			SET normalized_name = EXCLUDED.normalized_name
			RETURNING id
		`, teamID, name, normalized, actorID).Scan(&tagID)
		if err != nil {
			return err
		}
		var active bool
		if err = tx.QueryRowContext(ctx, `SELECT is_active FROM tag WHERE id = $1`, tagID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("Tag %q 已停用，不能用于新内容", name)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO content_tag (content_id, tag_id) VALUES ($1,$2) ON CONFLICT DO NOTHING
		`, contentID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func insertAudit(
	ctx context.Context,
	tx *sql.Tx,
	teamID int64,
	actor, action, entity, entityID, detail string,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_log (team_id, actor, action, entity_type, entity_id, detail, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
	`, teamID, actor, action, entity, entityID, detail)
	return err
}

func newUUID() string { return uuid.NewString() }
