package service

import (
	"encoding/json"
	"testing"
)

func TestEvaluateGovernanceRuleCombinesConditions(t *testing.T) {
	facts := map[string]any{"lifecycle": "已发布", "views": 1200.0, "completion": 66.0, "owner": ""}
	rule := GovernanceRule{
		Name: "高表现内容", Logic: "all",
		Conditions: []GovernanceCondition{
			{Field: "lifecycle", Operator: "eq", Value: "已发布"},
			{Field: "views", Operator: "gte", Value: 1000.0},
		},
	}
	matched, reason, err := EvaluateGovernanceRule(rule, facts)
	if err != nil || !matched {
		t.Fatalf("expected a match, got matched=%v reason=%q err=%v", matched, reason, err)
	}
	rule.Logic = "any"
	rule.Conditions = append(rule.Conditions, GovernanceCondition{Field: "completion", Operator: "gte", Value: 90.0})
	if matched, _, err = EvaluateGovernanceRule(rule, facts); err != nil || !matched {
		t.Fatalf("OR logic must match when one condition holds: %v %v", matched, err)
	}
	rule.Conditions = []GovernanceCondition{{Field: "views", Operator: "lte", Value: 10.0}}
	if matched, _, _ = EvaluateGovernanceRule(rule, facts); matched {
		t.Fatal("conditions that do not hold must not match")
	}
}

func TestEvaluateGovernanceRuleUsesAWhitelist(t *testing.T) {
	rule := GovernanceRule{Name: "bad", Logic: "all", Conditions: []GovernanceCondition{{Field: "secret_column", Operator: "eq", Value: "x"}}}
	if _, _, err := EvaluateGovernanceRule(rule, map[string]any{}); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
	rule.Conditions = []GovernanceCondition{{Field: "views", Operator: "drop table", Value: 1}}
	if _, _, err := EvaluateGovernanceRule(rule, map[string]any{"views": 1}); err == nil {
		t.Fatal("unknown operators must be rejected")
	}
}

func TestEvaluateGovernanceRuleHandlesEmptyAndMissingValues(t *testing.T) {
	facts := map[string]any{"owner": "", "platform": "视频号"}
	rule := GovernanceRule{Name: "缺失负责人", Logic: "all", Conditions: []GovernanceCondition{{Field: "owner", Operator: "empty"}}}
	if matched, _, err := EvaluateGovernanceRule(rule, facts); err != nil || !matched {
		t.Fatalf("empty operator must match blank values: %v %v", matched, err)
	}
	rule.Conditions = []GovernanceCondition{{Field: "owner", Operator: "exists"}}
	if matched, _, _ := EvaluateGovernanceRule(rule, facts); matched {
		t.Fatal("exists must not match a blank value")
	}
	rule.Conditions = []GovernanceCondition{{Field: "notes", Operator: "not_contains", Value: "广告"}}
	if matched, _, err := EvaluateGovernanceRule(rule, facts); err != nil || !matched {
		t.Fatalf("missing text must satisfy not_contains: %v %v", matched, err)
	}
}

func TestParseGovernanceRulesReadsTheStoredPayload(t *testing.T) {
	raw := json.RawMessage(`[{"name":"规则一","priority":900,"enabled":true,"trigger":"content.create","logic":"all","conditions":[{"field":"views","operator":"gte","value":1000}],"actions":[{"type":"add_tag","value":"高表现"}]}]`)
	rules, err := ParseGovernanceRules(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Name != "规则一" || len(rules[0].Conditions) != 1 {
		t.Fatalf("unexpected rules: %#v", rules)
	}
	if _, err := ParseGovernanceRules(json.RawMessage(`{"not":"an array"}`)); err == nil {
		t.Fatal("invalid payload must be rejected")
	}
}
