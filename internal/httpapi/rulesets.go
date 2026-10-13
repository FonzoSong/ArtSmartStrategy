package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/arts-smart/strategy/internal/service"
)

// ruleSetRule is one rule as the rules page renders it. System rules carry their
// governance summary; custom rules carry the editable flat fields.
type ruleSetRule struct {
	ID            int64  `json:"id,omitempty"`
	Name          string `json:"name"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
	RuleType      string `json:"rule_type,omitempty"`
	RuleContent   string `json:"rule_content,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	WorkflowState string `json:"workflow_state,omitempty"`
	Notes         string `json:"notes,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
	Logic         string `json:"logic,omitempty"`
	Conditions    string `json:"conditions_summary,omitempty"`
	Actions       string `json:"actions_summary,omitempty"`
	Scope         string `json:"scope_summary,omitempty"`
}

// ruleSet is one selectable rule set. System sets are read-only.
type ruleSet struct {
	Key         string        `json:"key"`
	Kind        string        `json:"kind"`
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Version     string        `json:"version,omitempty"`
	Description string        `json:"description"`
	Enabled     bool          `json:"enabled"`
	Editable    bool          `json:"editable"`
	Effective   bool          `json:"effective"`
	RuleCount   int           `json:"rule_count"`
	Rules       []ruleSetRule `json:"rules"`
}

// ruleSets lists every available rule set plus which one is effective.
func (s *Server) ruleSets(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	effectiveKind, effectiveID, err := s.effectiveRuleSelection(r.Context(), teamID)
	if err != nil {
		writeError(w, r, 500, "rule_sets_failed", "读取生效规则集失败")
		return
	}
	sets := make([]ruleSet, 0, 4)
	systemRows, err := s.Store.QueryMaps(r.Context(), `SELECT id,name,version,enabled,COALESCE(payload_json->>'description','') AS description,COALESCE(payload_json->'rules','[]'::jsonb) AS rules FROM system_rule_set ORDER BY name,version`)
	if err != nil {
		writeError(w, r, 500, "rule_sets_failed", "读取系统规则集失败")
		return
	}
	for _, row := range systemRows {
		id := intField(row, "id")
		set := ruleSet{
			Key:         fmt.Sprintf("system:%d", id),
			Kind:        "system",
			ID:          id,
			Name:        stringField(row, "name"),
			Version:     stringField(row, "version"),
			Description: stringField(row, "description"),
			Enabled:     boolField(row, "enabled"),
			Editable:    false,
			Effective:   effectiveKind == "system" && effectiveID == id,
			Rules:       []ruleSetRule{},
		}
		set.Rules = systemRuleSummaries(row["rules"])
		set.RuleCount = len(set.Rules)
		sets = append(sets, set)
	}
	customRows, err := s.Store.QueryMaps(r.Context(), `SELECT id,name,description,updated_at FROM content_rule_set WHERE team_id=$1 ORDER BY name`, teamID)
	if err != nil {
		writeError(w, r, 500, "rule_sets_failed", "读取自定义规则集失败")
		return
	}
	for _, row := range customRows {
		id := intField(row, "id")
		items, err := s.ruleSetItems(r.Context(), id)
		if err != nil {
			writeError(w, r, 500, "rule_sets_failed", "读取规则集条目失败")
			return
		}
		sets = append(sets, ruleSet{
			Key:         fmt.Sprintf("custom:%d", id),
			Kind:        "custom",
			ID:          id,
			Name:        stringField(row, "name"),
			Description: stringField(row, "description"),
			Enabled:     true,
			Editable:    true,
			Effective:   effectiveKind == "custom" && effectiveID == id,
			RuleCount:   len(items),
			Rules:       items,
		})
	}
	writeJSON(w, 200, map[string]any{
		"items": sets,
		"effective": map[string]any{
			"kind": effectiveKind,
			"id":   effectiveID,
			"key":  fmt.Sprintf("%s:%d", effectiveKind, effectiveID),
		},
	})
}

func (s *Server) ruleSetItems(ctx context.Context, ruleSetID int64) ([]ruleSetRule, error) {
	rows, err := s.Store.QueryMaps(ctx, `SELECT id,name,priority,enabled,rule_type,rule_content,content_type,workflow_state,notes FROM content_rule_set_item WHERE rule_set_id=$1 ORDER BY priority,id`, ruleSetID)
	if err != nil {
		return nil, err
	}
	items := make([]ruleSetRule, 0, len(rows))
	for _, row := range rows {
		items = append(items, ruleSetRule{
			ID:            intField(row, "id"),
			Name:          stringField(row, "name"),
			Priority:      int(intField(row, "priority")),
			Enabled:       boolField(row, "enabled"),
			RuleType:      stringField(row, "rule_type"),
			RuleContent:   stringField(row, "rule_content"),
			ContentType:   stringField(row, "content_type"),
			WorkflowState: stringField(row, "workflow_state"),
			Notes:         stringField(row, "notes"),
		})
	}
	return items, nil
}

// systemRuleSummaries turns the stored system rule JSON into readable summaries.
func systemRuleSummaries(raw any) []ruleSetRule {
	var encoded string
	switch typed := raw.(type) {
	case string:
		encoded = typed
	case []byte:
		encoded = string(typed)
	default:
		return []ruleSetRule{}
	}
	var rules []struct {
		Name       string            `json:"name"`
		Priority   int               `json:"priority"`
		Enabled    bool              `json:"enabled"`
		Trigger    string            `json:"trigger"`
		Logic      string            `json:"logic"`
		Conditions []json.RawMessage `json:"conditions"`
		Actions    []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"actions"`
		Scope struct {
			Platforms  []string `json:"platforms"`
			Categories []string `json:"categories"`
			Statuses   []string `json:"statuses"`
		} `json:"scope"`
	}
	if err := json.Unmarshal([]byte(encoded), &rules); err != nil {
		return []ruleSetRule{}
	}
	result := make([]ruleSetRule, 0, len(rules))
	for _, rule := range rules {
		actions := make([]string, 0, len(rule.Actions))
		for _, action := range rule.Actions {
			actions = append(actions, strings.TrimSpace(action.Type+":"+action.Value))
		}
		scope := []string{}
		if len(rule.Scope.Categories) > 0 {
			scope = append(scope, "类型 "+strings.Join(rule.Scope.Categories, "/"))
		}
		if len(rule.Scope.Platforms) > 0 {
			scope = append(scope, "平台 "+strings.Join(rule.Scope.Platforms, "/"))
		}
		if len(rule.Scope.Statuses) > 0 {
			scope = append(scope, "状态 "+strings.Join(rule.Scope.Statuses, "/"))
		}
		if len(scope) == 0 {
			scope = append(scope, "全部内容")
		}
		result = append(result, ruleSetRule{
			Name:       rule.Name,
			Priority:   rule.Priority,
			Enabled:    rule.Enabled,
			Trigger:    rule.Trigger,
			Logic:      rule.Logic,
			Conditions: fmt.Sprintf("%d 条条件", len(rule.Conditions)),
			Actions:    strings.Join(actions, "、"),
			Scope:      strings.Join(scope, " · "),
		})
	}
	return result
}

// effectiveRuleSelection reads the persisted selection, defaulting to the newest
// enabled system rule set when the team has not chosen one yet.
func (s *Server) effectiveRuleSelection(ctx context.Context, teamID int64) (string, int64, error) {
	var systemID, customID sql.NullInt64
	err := s.Store.DB.QueryRowContext(ctx, `SELECT system_rule_set_id,content_rule_set_id FROM team_rule_selection WHERE team_id=$1`, teamID).Scan(&systemID, &customID)
	if err == nil {
		if systemID.Valid {
			var enabled bool
			if err := s.Store.DB.QueryRowContext(ctx, `SELECT enabled FROM system_rule_set WHERE id=$1`, systemID.Int64).Scan(&enabled); err == nil && enabled {
				return "system", systemID.Int64, nil
			}
		}
		if customID.Valid {
			var exists bool
			if err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM content_rule_set WHERE id=$1 AND team_id=$2)`, customID.Int64, teamID).Scan(&exists); err == nil && exists {
				return "custom", customID.Int64, nil
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}
	var fallback sql.NullInt64
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT id FROM system_rule_set WHERE enabled=TRUE ORDER BY updated_at DESC LIMIT 1`).Scan(&fallback); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, nil
		}
		return "", 0, err
	}
	return "system", fallback.Int64, nil
}

// setEffectiveRuleSet persists the team's choice after validating the target.
func (s *Server) setEffectiveRuleSet(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
	}
	if !decode(w, r, &input) {
		return
	}
	teamID := claimsFrom(r).TeamID
	c := claimsFrom(r)
	switch strings.TrimSpace(input.Kind) {
	case "system":
		var enabled bool
		if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT enabled FROM system_rule_set WHERE id=$1`, input.ID).Scan(&enabled); err != nil {
			writeError(w, r, 404, "rule_set_not_found", "系统规则集不存在")
			return
		}
		if !enabled {
			writeError(w, r, 422, "rule_set_disabled", "该规则集已停用，不能设为当前生效规则集")
			return
		}
		if _, err := s.Store.DB.ExecContext(r.Context(), `
			INSERT INTO team_rule_selection (team_id, system_rule_set_id, content_rule_set_id, updated_by_user_id, updated_at)
			VALUES ($1,$2,NULL,$3,NOW())
			ON CONFLICT (team_id) DO UPDATE SET system_rule_set_id=$2, content_rule_set_id=NULL, updated_by_user_id=$3, updated_at=NOW()
		`, teamID, input.ID, c.UserID); err != nil {
			writeError(w, r, 500, "rule_set_save_failed", "保存生效规则集失败")
			return
		}
	case "custom":
		var exists bool
		if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM content_rule_set WHERE id=$1 AND team_id=$2)`, input.ID, teamID).Scan(&exists); err != nil {
			writeError(w, r, 500, "rule_set_check_failed", "校验规则集失败")
			return
		}
		if !exists {
			writeError(w, r, 404, "rule_set_not_found", "规则集不存在或不属于当前团队")
			return
		}
		if _, err := s.Store.DB.ExecContext(r.Context(), `
			INSERT INTO team_rule_selection (team_id, system_rule_set_id, content_rule_set_id, updated_by_user_id, updated_at)
			VALUES ($1,NULL,$2,$3,NOW())
			ON CONFLICT (team_id) DO UPDATE SET system_rule_set_id=NULL, content_rule_set_id=$2, updated_by_user_id=$3, updated_at=NOW()
		`, teamID, input.ID, c.UserID); err != nil {
			writeError(w, r, 500, "rule_set_save_failed", "保存生效规则集失败")
			return
		}
	default:
		writeError(w, r, 422, "rule_set_kind_invalid", "规则集类型必须是 system 或 custom")
		return
	}
	kind, id, err := s.effectiveRuleSelection(r.Context(), teamID)
	if err != nil {
		writeError(w, r, 500, "rule_set_save_failed", "读取生效规则集失败")
		return
	}
	writeJSON(w, 200, map[string]any{"kind": kind, "id": id, "key": fmt.Sprintf("%s:%d", kind, id)})
}

// createRuleSet creates a team-owned, editable rule set.
func (s *Server) createRuleSet(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &input) {
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 120 {
		writeError(w, r, 422, "rule_set_invalid", "规则集名称不能为空且不超过 120 个字符")
		return
	}
	c := claimsFrom(r)
	var id int64
	err := s.Store.DB.QueryRowContext(r.Context(), `INSERT INTO content_rule_set (team_id,name,description,created_by_user_id,created_at,updated_at) VALUES ($1,$2,$3,$4,NOW(),NOW()) RETURNING id`, c.TeamID, name, strings.TrimSpace(input.Description), c.UserID).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "uq_content_rule_set_team_name") {
			writeError(w, r, 409, "rule_set_exists", "同名规则集已存在")
			return
		}
		writeError(w, r, 500, "rule_set_create_failed", "创建规则集失败")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": name, "description": strings.TrimSpace(input.Description), "editable": true})
}

// ruleSetKind isolates the two rule-set namespaces. `system_rule_set` and
// `content_rule_set` have independent id sequences, so a bare number can name a
// system set and a custom set at the same time. Mutating endpoints therefore
// accept an explicit `kind` (custom by default) and refuse `kind=system`
// outright, which makes the protection impossible to bypass by id reuse.
func ruleSetKind(r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("kind"))
	// Also accept the `system:1` / `custom:1` form the rules page uses as a key.
	if id := r.PathValue("id"); strings.Contains(id, ":") {
		parts := strings.SplitN(id, ":", 2)
		raw = parts[0]
	}
	switch raw {
	case "", "custom":
		return "custom", nil
	case "system":
		return "system", nil
	default:
		return "", fmt.Errorf("规则集类型必须是 system 或 custom")
	}
}

// requireCustomRuleSet resolves a team-owned custom rule set and refuses system
// rule sets with an explicit error, so protection is enforced server-side.
func (s *Server) requireCustomRuleSet(w http.ResponseWriter, r *http.Request, teamID int64) (int64, bool) {
	kind, err := ruleSetKind(r)
	if err != nil {
		writeError(w, r, 422, "rule_set_kind_invalid", err.Error())
		return 0, false
	}
	if kind == "system" {
		writeError(w, r, 403, "rule_set_system_protected", "系统规则集为只读，不能修改或删除")
		return 0, false
	}
	rawID := r.PathValue("id")
	if strings.Contains(rawID, ":") {
		rawID = strings.SplitN(rawID, ":", 2)[1]
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeError(w, r, 404, "rule_set_not_found", "规则集不存在")
		return 0, false
	}
	var exists bool
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM content_rule_set WHERE id=$1 AND team_id=$2)`, id, teamID).Scan(&exists); err != nil {
		writeError(w, r, 500, "rule_set_check_failed", "校验规则集失败")
		return 0, false
	}
	if exists {
		return id, true
	}
	var system bool
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM system_rule_set WHERE id=$1)`, id).Scan(&system); err == nil && system {
		writeError(w, r, 403, "rule_set_system_protected", "系统规则集为只读，不能修改或删除")
		return 0, false
	}
	writeError(w, r, 404, "rule_set_not_found", "规则集不存在或不属于当前团队")
	return 0, false
}

// updateRuleSet renames a custom rule set or changes its description.
func (s *Server) updateRuleSet(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	id, ok := s.requireCustomRuleSet(w, r, c.TeamID)
	if !ok {
		return
	}
	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decode(w, r, &input) {
		return
	}
	assignments := []string{"updated_at=NOW()"}
	args := []any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 120 {
			writeError(w, r, 422, "rule_set_invalid", "规则集名称不能为空且不超过 120 个字符")
			return
		}
		args = append(args, name)
		assignments = append(assignments, fmt.Sprintf("name=$%d", len(args)))
	}
	if input.Description != nil {
		args = append(args, strings.TrimSpace(*input.Description))
		assignments = append(assignments, fmt.Sprintf("description=$%d", len(args)))
	}
	args = append(args, id, c.TeamID)
	if _, err := s.Store.DB.ExecContext(r.Context(), fmt.Sprintf("UPDATE content_rule_set SET %s WHERE id=$%d AND team_id=$%d", strings.Join(assignments, ","), len(args)-1, len(args)), args...); err != nil {
		writeError(w, r, 500, "rule_set_update_failed", "更新规则集失败")
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "saved": true})
}

// deleteRuleSet removes a custom rule set, refusing while it is the effective one.
func (s *Server) deleteRuleSet(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	id, ok := s.requireCustomRuleSet(w, r, c.TeamID)
	if !ok {
		return
	}
	kind, effectiveID, err := s.effectiveRuleSelection(r.Context(), c.TeamID)
	if err != nil {
		writeError(w, r, 500, "rule_set_delete_failed", "删除规则集失败")
		return
	}
	if kind == "custom" && effectiveID == id {
		writeError(w, r, 409, "rule_set_in_use", "该规则集正在生效，请先切换生效规则集后再删除")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM content_rule_set WHERE id=$1 AND team_id=$2`, id, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "rule_set_delete_failed", "删除规则集失败")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		writeError(w, r, 404, "rule_set_not_found", "规则集不存在")
		return
	}
	if _, err = s.Store.DB.ExecContext(r.Context(), `DELETE FROM team_rule_selection WHERE team_id=$1 AND content_rule_set_id=$2`, c.TeamID, id); err != nil {
		writeError(w, r, 500, "rule_set_delete_failed", "清理生效配置失败")
		return
	}
	w.WriteHeader(204)
}

// ruleSetItemInput is the editable rule contract of a custom rule set.
type ruleSetItemInput struct {
	Name          string `json:"name"`
	RuleType      string `json:"rule_type"`
	RuleContent   string `json:"rule_content"`
	ContentType   string `json:"content_type"`
	WorkflowState string `json:"workflow_state"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
	Notes         string `json:"notes"`
}

func (in ruleSetItemInput) validate() error {
	if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 120 {
		return errors.New("规则名称不能为空且不超过 120 个字符")
	}
	if in.Priority < 0 || in.Priority > 10000 {
		return errors.New("优先级应为 0 到 10000")
	}
	return validateRuleInput(ruleInput{Name: in.Name, RuleType: in.RuleType, RuleContent: in.RuleContent, Priority: in.Priority, ContentType: in.ContentType, WorkflowState: in.WorkflowState, Enabled: in.Enabled, Notes: in.Notes})
}

// addRuleSetItem appends a rule to a custom rule set.
func (s *Server) addRuleSetItem(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	id, ok := s.requireCustomRuleSet(w, r, c.TeamID)
	if !ok {
		return
	}
	var input ruleSetItemInput
	if !decode(w, r, &input) {
		return
	}
	if err := input.validate(); err != nil {
		writeError(w, r, 422, "rule_invalid", err.Error())
		return
	}
	var itemID int64
	err := s.Store.DB.QueryRowContext(r.Context(), `
		INSERT INTO content_rule_set_item (rule_set_id,name,priority,enabled,rule_type,rule_content,content_type,workflow_state,notes,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW()) RETURNING id
	`, id, strings.TrimSpace(input.Name), input.Priority, input.Enabled, input.RuleType, input.RuleContent, input.ContentType, input.WorkflowState, input.Notes).Scan(&itemID)
	if err != nil {
		writeError(w, r, 500, "rule_save_failed", "保存规则失败")
		return
	}
	s.touchRuleSet(r.Context(), id)
	writeJSON(w, 201, map[string]any{"id": itemID, "rule_set_id": id, "saved": true})
}

// updateRuleSetItem updates one rule of a custom rule set, including its
// enabled flag and priority.
func (s *Server) updateRuleSetItem(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	id, ok := s.requireCustomRuleSet(w, r, c.TeamID)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "rule_not_found", "规则不存在")
		return
	}
	var current ruleSetItemInput
	err = s.Store.DB.QueryRowContext(r.Context(), `SELECT name,priority,enabled,rule_type,rule_content,content_type,workflow_state,notes FROM content_rule_set_item WHERE id=$1 AND rule_set_id=$2`, itemID, id).Scan(&current.Name, &current.Priority, &current.Enabled, &current.RuleType, &current.RuleContent, &current.ContentType, &current.WorkflowState, &current.Notes)
	if err != nil {
		notFoundOrError(w, r, err, "规则不存在")
		return
	}
	var input struct {
		Name          *string `json:"name"`
		RuleType      *string `json:"rule_type"`
		RuleContent   *string `json:"rule_content"`
		ContentType   *string `json:"content_type"`
		WorkflowState *string `json:"workflow_state"`
		Priority      *int    `json:"priority"`
		Enabled       *bool   `json:"enabled"`
		Notes         *string `json:"notes"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Name != nil {
		current.Name = *input.Name
	}
	if input.RuleType != nil {
		current.RuleType = *input.RuleType
	}
	if input.RuleContent != nil {
		current.RuleContent = *input.RuleContent
	}
	if input.ContentType != nil {
		current.ContentType = *input.ContentType
	}
	if input.WorkflowState != nil {
		current.WorkflowState = *input.WorkflowState
	}
	if input.Priority != nil {
		current.Priority = *input.Priority
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	if input.Notes != nil {
		current.Notes = *input.Notes
	}
	if err = current.validate(); err != nil {
		writeError(w, r, 422, "rule_invalid", err.Error())
		return
	}
	if _, err = s.Store.DB.ExecContext(r.Context(), `UPDATE content_rule_set_item SET name=$1,priority=$2,enabled=$3,rule_type=$4,rule_content=$5,content_type=$6,workflow_state=$7,notes=$8,updated_at=NOW() WHERE id=$9 AND rule_set_id=$10`, strings.TrimSpace(current.Name), current.Priority, current.Enabled, current.RuleType, current.RuleContent, current.ContentType, current.WorkflowState, current.Notes, itemID, id); err != nil {
		writeError(w, r, 500, "rule_update_failed", "更新规则失败")
		return
	}
	s.touchRuleSet(r.Context(), id)
	writeJSON(w, 200, map[string]any{"id": itemID, "saved": true})
}

// deleteRuleSetItem removes one rule from a custom rule set.
func (s *Server) deleteRuleSetItem(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	id, ok := s.requireCustomRuleSet(w, r, c.TeamID)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "rule_not_found", "规则不存在")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM content_rule_set_item WHERE id=$1 AND rule_set_id=$2`, itemID, id)
	if err != nil {
		writeError(w, r, 500, "rule_delete_failed", "删除规则失败")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		writeError(w, r, 404, "rule_not_found", "规则不存在")
		return
	}
	s.touchRuleSet(r.Context(), id)
	w.WriteHeader(204)
}

func (s *Server) touchRuleSet(ctx context.Context, id int64) {
	_, _ = s.Store.DB.ExecContext(ctx, `UPDATE content_rule_set SET updated_at=NOW() WHERE id=$1`, id)
}

// previewRuleSetItem dry-runs one rule of the set without persisting it.
func (s *Server) previewRuleSetItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rule ruleSetItemInput `json:"rule"`
		Text string           `json:"text"`
		Tags []string         `json:"tags"`
	}
	if !decode(w, r, &in) {
		return
	}
	result, err := service.EvaluateRule(in.Rule.RuleType, in.Rule.RuleContent, in.Text, in.Tags)
	if err != nil {
		writeError(w, r, 422, "rule_evaluation_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"matched": result.Matched, "result": result.Result})
}

// contentFacts flattens a content input into the facts the rule engine evaluates.
func contentFacts(content domain.Content) map[string]any {
	return map[string]any{
		"id":             content.ID,
		"title":          content.Title,
		"notes":          content.Notes,
		"category":       content.Category,
		"platform":       content.Platform,
		"lifecycle":      content.Lifecycle,
		"workflow_state": content.WorkflowState,
		"owner":          content.Owner,
		"views":          content.Views,
		"likes":          content.Likes,
		"comments":       content.Comments,
		"saves":          content.Saves,
		"duration":       content.Duration,
		"completion":     content.Completion,
	}
}
