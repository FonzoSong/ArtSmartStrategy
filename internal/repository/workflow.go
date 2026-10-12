package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arts-smart/strategy/internal/domain"
)

// TransitionItem is one selected record plus the optional fields the operator
// filled in for it. Unset fields are never written, so existing data is kept.
type TransitionItem struct {
	ID     string
	Fields map[string]any
}

// TransitionResult is the per-record outcome of a batch transition.
type TransitionResult struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// TransitionOutcome reports how a batch transition ended.
type TransitionOutcome struct {
	From     string             `json:"from"`
	Target   string             `json:"target"`
	Advanced int                `json:"advanced"`
	Failed   int                `json:"failed"`
	Results  []TransitionResult `json:"results"`
}

// assignment is one column of an UPDATE, either a bound value or a SQL literal.
type assignment struct {
	column  string
	value   any
	literal bool
}

func buildUpdate(assignments []assignment) (string, []any) {
	args := make([]any, 0, len(assignments))
	parts := make([]string, 0, len(assignments))
	for _, item := range assignments {
		if item.literal {
			parts = append(parts, item.column+" = "+fmt.Sprint(item.value))
			continue
		}
		args = append(args, item.value)
		parts = append(parts, fmt.Sprintf("%s = $%d", item.column, len(args)))
	}
	return strings.Join(parts, ", "), args
}

// TransitionContents moves each selected record one step along the workflow.
// The target state is validated against the record's persisted state, only the
// fields that belong to that step are written, and every record reports its own
// result so partial success stays visible. Successful records are committed
// together; a failing record never blocks the others.
func (r *Postgres) TransitionContents(
	ctx context.Context,
	teamID, actorID int64,
	actor, target string,
	items []TransitionItem,
) (TransitionOutcome, error) {
	outcome := TransitionOutcome{Target: target, Results: make([]TransitionResult, 0, len(items))}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return outcome, err
	}
	defer tx.Rollback()
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, duplicate := seen[item.ID]; duplicate {
			continue
		}
		seen[item.ID] = struct{}{}
		title, current, applyErr := r.transitionOne(ctx, tx, teamID, actorID, item, target)
		if applyErr != nil {
			outcome.Failed++
			outcome.Results = append(outcome.Results, TransitionResult{ID: item.ID, Title: title, Status: "failed", Reason: applyErr.Error()})
			continue
		}
		if outcome.From == "" {
			outcome.From = current
		}
		outcome.Advanced++
		outcome.Results = append(outcome.Results, TransitionResult{ID: item.ID, Title: title, Status: "advanced"})
	}
	if outcome.Advanced > 0 {
		detail := fmt.Sprintf("推进工作流到「%s」：成功 %d 条，失败 %d 条", target, outcome.Advanced, outcome.Failed)
		if err = insertAudit(ctx, tx, teamID, actor, "advance_workflow", "content", "bulk", detail); err != nil {
			return TransitionOutcome{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return TransitionOutcome{}, err
	}
	return outcome, nil
}

// transitionOne applies the guarded state change plus the step-specific fields.
func (r *Postgres) transitionOne(
	ctx context.Context,
	tx *sql.Tx,
	teamID, actorID int64,
	item TransitionItem,
	target string,
) (string, string, error) {
	var title, current, lifecycle string
	var currentLikes, currentComments, currentSaves, currentShares int64
	err := tx.QueryRowContext(ctx, `
		SELECT title, workflow_state, lifecycle, likes, comments, saves, shares FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE
	`, item.ID, teamID).Scan(&title, &current, &lifecycle, &currentLikes, &currentComments, &currentSaves, &currentShares)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errors.New("内容不存在或不属于当前团队")
	}
	if err != nil {
		return "", "", err
	}
	if !domain.CanTransition(current, target) {
		return title, current, fmt.Errorf("当前状态为「%s」，不能直接推进到「%s」", current, target)
	}
	assignments := []assignment{
		{column: "workflow_state", value: target},
		{column: "updated_at", value: "NOW()", literal: true},
	}
	nextLifecycle := domain.LifecycleUnpublished
	if target == domain.WorkflowPublished {
		nextLifecycle = domain.LifecyclePublished
	}
	if lifecycle != nextLifecycle {
		assignments = append(assignments, assignment{column: "lifecycle", value: nextLifecycle})
	}
	fields, interactions, err := applyTransitionFields(ctx, tx, teamID, actorID, item, target)
	if err != nil {
		return title, current, err
	}
	assignments = append(assignments, fields...)
	if interactions != nil {
		// PostgreSQL evaluates SET expressions against the old row, so the derived
		// interaction total is computed here instead of in SQL.
		total := currentLikes + currentComments + currentSaves + currentShares
		for key, value := range interactions {
			switch key {
			case "likes":
				total += value - currentLikes
			case "comments":
				total += value - currentComments
			case "saves":
				total += value - currentSaves
			case "shares":
				total += value - currentShares
			}
		}
		assignments = append(assignments, assignment{column: "total_interactions", value: total})
	}
	setSQL, setArgs := buildUpdate(assignments)
	args := append(append([]any{}, setArgs...), item.ID, teamID, current)
	query := fmt.Sprintf(
		"UPDATE content SET %s WHERE id=$%d AND team_id=$%d AND workflow_state=$%d",
		setSQL, len(setArgs)+1, len(setArgs)+2, len(setArgs)+3,
	)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return title, current, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return title, current, err
	} else if affected == 0 {
		return title, current, errors.New("记录状态已被其他操作修改，请刷新后重试")
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO content_workflow_event
		(team_id, content_id, from_state, to_state, source, actor_user_id, created_at)
		VALUES ($1,$2,$3,$4,'manual',$5,NOW())
	`, teamID, item.ID, current, target, actorID); err != nil {
		return title, current, err
	}
	if target == domain.WorkflowPublished {
		if err = settlePendingSchedule(ctx, tx, teamID, item.ID, item.Fields); err != nil {
			return title, current, err
		}
	}
	return title, current, nil
}

// applyTransitionFields validates and converts the fields a workflow step
// allows. Unlisted, unknown or malformed values are rejected for that record.
func applyTransitionFields(
	ctx context.Context,
	tx *sql.Tx,
	teamID, actorID int64,
	item TransitionItem,
	target string,
) ([]assignment, map[string]int64, error) {
	fields := item.Fields
	allowed := map[string]bool{}
	switch target {
	case domain.WorkflowProductionDone:
		allowed = map[string]bool{"materials": true, "notes": true, "tags": true}
	case domain.WorkflowSchedulePending:
		allowed = map[string]bool{"owner_user_id": true, "performance_id": true}
	case domain.WorkflowPublished:
		allowed = map[string]bool{
			"views": true, "likes": true, "comments": true, "saves": true, "shares": true,
			"completion": true, "profile_visits": true, "follows": true, "attendance": true,
			"tickets": true, "comment_text": true,
		}
	default:
		return nil, nil, fmt.Errorf("不支持推进到「%s」", target)
	}
	result := make([]assignment, 0, len(fields))
	if fields == nil {
		return result, nil, nil
	}
	for key := range fields {
		if !allowed[key] {
			return nil, nil, fmt.Errorf("推进到「%s」时不支持字段 %s", target, key)
		}
	}
	if raw, ok := fields["materials"]; ok && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return nil, nil, errors.New("素材说明必须是文本")
		}
		result = append(result, assignment{column: "materials", value: strings.TrimSpace(text)})
	}
	if raw, ok := fields["notes"]; ok && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return nil, nil, errors.New("备注必须是文本")
		}
		result = append(result, assignment{column: "notes", value: strings.TrimSpace(text)})
	}
	if raw, ok := fields["comment_text"]; ok && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return nil, nil, errors.New("评论摘录必须是文本")
		}
		result = append(result, assignment{column: "comment_text", value: text})
	}
	if raw, ok := fields["tags"]; ok && raw != nil {
		tags, err := toStringSlice(raw)
		if err != nil {
			return nil, nil, err
		}
		if err = syncContentTags(ctx, tx, teamID, actorID, item.ID, tags); err != nil {
			return nil, nil, err
		}
	}
	if raw, ok := fields["owner_user_id"]; ok && raw != nil {
		ownerID, ok := toInt64(raw)
		if !ok {
			return nil, nil, errors.New("负责人 ID 无效")
		}
		var ownerName string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM user_account WHERE id=$1 AND team_id=$2`, ownerID, teamID).Scan(&ownerName); err != nil {
			return nil, nil, errors.New("负责人不存在或不属于当前团队")
		}
		result = append(result, assignment{column: "owner_user_id", value: ownerID}, assignment{column: "owner", value: ownerName})
	}
	if raw, ok := fields["performance_id"]; ok && raw != nil {
		performanceID, ok := raw.(string)
		if !ok || strings.TrimSpace(performanceID) == "" {
			return nil, nil, errors.New("关联演出 ID 无效")
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM performance_booking WHERE id=$1 AND team_id=$2)`, performanceID, teamID).Scan(&exists); err != nil || !exists {
			return nil, nil, errors.New("关联演出不存在或不属于当前团队")
		}
		result = append(result, assignment{column: "performance_id", value: performanceID})
	}
	interactions := map[string]int64{}
	for _, key := range []string{"views", "likes", "comments", "saves", "shares", "profile_visits", "follows", "attendance", "tickets"} {
		raw, ok := fields[key]
		if !ok || raw == nil {
			continue
		}
		value, ok := toInt64(raw)
		if !ok {
			return nil, nil, fmt.Errorf("%s 必须是整数", key)
		}
		if value < 0 {
			return nil, nil, fmt.Errorf("%s 不能为负数", key)
		}
		if key != "profile_visits" && key != "follows" && key != "attendance" && key != "tickets" {
			interactions[key] = value
		}
		result = append(result, assignment{column: key, value: value})
	}
	if raw, ok := fields["completion"]; ok && raw != nil {
		value, ok := toFloat64(raw)
		if !ok {
			return nil, nil, errors.New("看完比例必须是数值")
		}
		if value < 0 || value > 100 {
			return nil, nil, errors.New("看完比例必须为 0 到 100 之间")
		}
		result = append(result, assignment{column: "completion", value: value})
	}
	return result, interactions, nil
}

// settlePendingSchedule mirrors the publish result onto the content's pending
// schedule so the calendar and the asset agree after publication.
func settlePendingSchedule(ctx context.Context, tx *sql.Tx, teamID int64, contentID string, fields map[string]any) error {
	if fields == nil {
		return nil
	}
	metric := func(key string) any {
		if raw, ok := fields[key]; ok && raw != nil {
			return raw
		}
		return nil
	}
	if metric("views") == nil && metric("likes") == nil && metric("comments") == nil && metric("saves") == nil && metric("shares") == nil && metric("completion") == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE schedule SET status='已复盘',actual_views=$1,actual_likes=$2,actual_comments=$3,actual_saves=$4,actual_shares=$5,actual_completion=$6,updated_at=NOW()
		WHERE team_id=$7 AND content_id=$8 AND status='待发布'
	`, metric("views"), metric("likes"), metric("comments"), metric("saves"), metric("shares"), metric("completion"), teamID, contentID)
	return err
}

func toStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, entry := range typed {
			text, ok := entry.(string)
			if !ok {
				return nil, errors.New("Tag 必须是文本")
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, errors.New("Tag 必须是数组")
	}
}

func toInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case string:
		var parsed int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func toFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int64:
		return float64(typed), true
	case int:
		return float64(typed), true
	case string:
		var parsed float64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
