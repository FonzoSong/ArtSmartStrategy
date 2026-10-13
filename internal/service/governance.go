package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Governance rules are the read-only system rule set's rules: a list of
// conditions combined with AND/OR and a list of descriptive actions. They never
// reject content, but they must be evaluated against real data so the execution
// log reflects the effective rule set.

// governanceFields is the whitelist of fields a condition may reference.
var governanceFields = map[string]bool{
	"lifecycle": true, "workflow_state": true, "category": true, "platform": true,
	"owner": true, "title": true, "notes": true, "views": true, "likes": true,
	"comments": true, "saves": true, "completion": true, "duration": true,
}

// governanceOperators is the whitelist of supported comparisons.
var governanceOperators = map[string]bool{
	"eq": true, "neq": true, "gt": true, "gte": true, "lt": true, "lte": true,
	"contains": true, "not_contains": true, "exists": true, "empty": true,
}

// maxGovernanceConditions bounds how much work one rule may request.
const maxGovernanceConditions = 20

// GovernanceCondition is one comparison of the stored rule schema.
type GovernanceCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

// GovernanceRule is one rule of the read-only rule set.
type GovernanceRule struct {
	Name       string                `json:"name"`
	Priority   int                   `json:"priority"`
	Enabled    bool                  `json:"enabled"`
	Trigger    string                `json:"trigger"`
	Logic      string                `json:"logic"`
	Conditions []GovernanceCondition `json:"conditions"`
}

// ParseGovernanceRules reads the rule array stored in the rule-set payload.
func ParseGovernanceRules(raw json.RawMessage) ([]GovernanceRule, error) {
	var rules []GovernanceRule
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("规则集内容不是有效 JSON: %w", err)
	}
	return rules, nil
}

// EvaluateGovernanceRule reports whether the rule matches the content facts and
// why. Unknown fields or operators are rejected instead of being ignored.
func EvaluateGovernanceRule(rule GovernanceRule, facts map[string]any) (bool, string, error) {
	if len(rule.Conditions) == 0 {
		return false, "", nil
	}
	if len(rule.Conditions) > maxGovernanceConditions {
		return false, "", fmt.Errorf("规则条件过多（最多 %d 条）", maxGovernanceConditions)
	}
	logic := strings.ToUpper(strings.TrimSpace(rule.Logic))
	if logic == "" {
		logic = "ALL"
	}
	if logic != "ALL" && logic != "ANY" {
		return false, "", fmt.Errorf("不支持的组合方式: %s", rule.Logic)
	}
	matchedCount := 0
	details := make([]string, 0, len(rule.Conditions))
	for _, condition := range rule.Conditions {
		field := strings.TrimSpace(condition.Field)
		if !governanceFields[field] {
			return false, "", fmt.Errorf("不支持的规则字段: %s", condition.Field)
		}
		operator := strings.TrimSpace(condition.Operator)
		if !governanceOperators[operator] {
			return false, "", fmt.Errorf("不支持的规则操作符: %s", condition.Operator)
		}
		ok, err := compareGovernance(facts[field], operator, condition.Value)
		if err != nil {
			return false, "", err
		}
		if ok {
			matchedCount++
			details = append(details, fmt.Sprintf("%s %s 满足", field, operator))
		}
	}
	matched := matchedCount == len(rule.Conditions)
	if logic == "ANY" {
		matched = matchedCount > 0
	}
	if !matched {
		return false, strings.Join(details, "；"), nil
	}
	return true, strings.Join(details, "；"), nil
}

func compareGovernance(actual any, operator string, expected any) (bool, error) {
	switch operator {
	case "exists":
		return actual != nil && strings.TrimSpace(fmt.Sprint(actual)) != "", nil
	case "empty":
		return actual == nil || strings.TrimSpace(fmt.Sprint(actual)) == "", nil
	}
	if number, ok := toNumber(actual); ok {
		want, ok := toNumber(expected)
		if !ok {
			return false, errors.New("数值比较的期望值必须是数字")
		}
		switch operator {
		case "eq":
			return number == want, nil
		case "neq":
			return number != want, nil
		case "gt":
			return number > want, nil
		case "gte":
			return number >= want, nil
		case "lt":
			return number < want, nil
		case "lte":
			return number <= want, nil
		default:
			return false, fmt.Errorf("操作符 %s 不支持数值字段", operator)
		}
	}
	text := ""
	if actual != nil {
		text = fmt.Sprint(actual)
	}
	want := ""
	if expected != nil {
		want = fmt.Sprint(expected)
	}
	switch operator {
	case "eq":
		return text == want, nil
	case "neq":
		return text != want, nil
	case "contains":
		return strings.Contains(text, want), nil
	case "not_contains":
		return !strings.Contains(text, want), nil
	default:
		return false, fmt.Errorf("操作符 %s 不支持文本字段", operator)
	}
}

func toNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
