package domain

import "time"

// ContentRule is a team-authored validation rule, persisted in PostgreSQL.
type ContentRule struct {
	ID            int64     `json:"id"`
	TeamID        int64     `json:"team_id"`
	Name          string    `json:"name"`
	Priority      int       `json:"priority"`
	Enabled       bool      `json:"enabled"`
	Trigger       string    `json:"trigger"`
	Logic         string    `json:"logic"`
	Conditions    string    `json:"conditions_json"`
	Scope         string    `json:"scope_json"`
	Actions       string    `json:"actions_json"`
	RuleType      string    `json:"rule_type"`
	RuleContent   string    `json:"rule_content"`
	ContentType   string    `json:"content_type"`
	WorkflowState string    `json:"workflow_state"`
	Notes         string    `json:"notes"`
	CreatedBy     *int64    `json:"created_by_user_id,omitempty"`
	UpdatedBy     *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
