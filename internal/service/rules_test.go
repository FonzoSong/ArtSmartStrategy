package service

import "testing"

func TestEvaluateRuleSupportedTypes(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		content  string
		text     string
		tags     []string
		want     bool
	}{
		{"required missing", "required", `{}`, " ", nil, true},
		{"required present", "required", `{}`, "标题", nil, false},
		{"format mismatch", "format", `{"pattern":"^标题"}`, "正文", nil, true},
		{"sensitive word", "sensitive_words", `{"words":["待核实"]}`, "需要待核实", nil, true},
		{"length exceeded", "length", `{"max":2}`, "三个字符", nil, true},
		{"required tag absent", "tag_constraint", `{"required":["演出"]}`, "文案", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EvaluateRule(tt.typeName, tt.content, tt.text, tt.tags)
			if err != nil {
				t.Fatal(err)
			}
			if result.Matched != tt.want {
				t.Fatalf("Matched = %v, want %v", result.Matched, tt.want)
			}
		})
	}
}

func TestEvaluateRuleRejectsInvalidConfiguration(t *testing.T) {
	if _, err := EvaluateRule("format", `{"pattern":"["}`, "text", nil); err == nil {
		t.Fatal("expected invalid regular expression error")
	}
	if _, err := EvaluateRule("not-supported", `{}`, "text", nil); err == nil {
		t.Fatal("expected unsupported rule error")
	}
}
