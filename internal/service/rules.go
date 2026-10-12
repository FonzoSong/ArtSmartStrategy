package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// RuleResult describes one rule evaluation without persistence side effects.
type RuleResult struct {
	Matched bool   `json:"matched"`
	Result  string `json:"result"`
}

// EvaluateRule executes the supported content validation rule types.
func EvaluateRule(ruleType, rawContent, text string, tags []string) (RuleResult, error) {
	var config map[string]any
	if err := json.Unmarshal([]byte(rawContent), &config); err != nil {
		return RuleResult{}, fmt.Errorf("规则内容不是有效 JSON: %w", err)
	}
	matched := false
	switch ruleType {
	case "required":
		matched = strings.TrimSpace(text) == ""
	case "format":
		pattern, ok := config["pattern"].(string)
		if !ok || pattern == "" {
			return RuleResult{}, errors.New("格式规则缺少 pattern")
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return RuleResult{}, fmt.Errorf("正则表达式无效: %w", err)
		}
		matched = !compiled.MatchString(text)
	case "sensitive_words":
		words, ok := config["words"].([]any)
		if !ok {
			return RuleResult{}, errors.New("敏感词规则缺少 words 数组")
		}
		for _, item := range words {
			if word, ok := item.(string); ok && word != "" && strings.Contains(text, word) {
				matched = true
				break
			}
		}
	case "length":
		length := utf8.RuneCountInString(text)
		if min, ok := number(config["min"]); ok && float64(length) < min {
			matched = true
		}
		if max, ok := number(config["max"]); ok && float64(length) > max {
			matched = true
		}
	case "tag_constraint":
		if required, ok := config["required"].([]any); ok {
			for _, item := range required {
				if name, ok := item.(string); ok && !contains(tags, name) {
					matched = true
				}
			}
		}
		if max, ok := number(config["max_count"]); ok && float64(len(tags)) > max {
			matched = true
		}
	default:
		return RuleResult{}, fmt.Errorf("不支持的规则类型: %s", ruleType)
	}
	if matched {
		return RuleResult{Matched: true, Result: "matched"}, nil
	}
	return RuleResult{Result: "no_match"}, nil
}

func number(value any) (float64, bool) { number, ok := value.(float64); return number, ok }
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
