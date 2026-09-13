package harness

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PlanStep 规划中的一个步骤。
type PlanStep struct {
	ID              string   `json:"id"`
	Goal            string   `json:"goal"`
	SuggestedTools  []string `json:"suggested_tools,omitempty"`
	SuccessCriteria string   `json:"success_criteria"`
	Readonly        bool     `json:"readonly"`
}

// Plan 结构化规划。
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// ParsePlan 从 planner 文本中提取并校验结构化 Plan。
// 容错：允许文本被 markdown code fence 或前后缀文字包裹；步骤缺少 id 时自动补 step-N。
func ParsePlan(text string) (*Plan, error) {
	raw := extractJSON(text)
	var plan Plan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, fmt.Errorf("plan is not valid JSON: %w", err)
	}
	if len(plan.Steps) == 0 {
		return nil, fmt.Errorf("plan has no steps")
	}
	for i := range plan.Steps {
		s := &plan.Steps[i]
		s.Goal = strings.TrimSpace(s.Goal)
		if s.Goal == "" {
			return nil, fmt.Errorf("step %d missing goal", i)
		}
		if s.ID == "" {
			s.ID = fmt.Sprintf("step-%d", i+1)
		}
	}
	return &plan, nil
}

// extractJSON 提取文本中的 JSON 对象：先剥离 markdown code fence，再取首个 { 到末个 }。
func extractJSON(text string) string {
	t := strings.TrimSpace(text)
	if strings.Contains(t, "```") {
		start := strings.Index(t, "```")
		rest := t[start+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:] // 去掉 ```json 这一行的语言标签
		}
		if end := strings.LastIndex(rest, "```"); end >= 0 {
			t = rest[:end]
		} else {
			t = rest
		}
		t = strings.TrimSpace(t)
	}
	lo := strings.Index(t, "{")
	hi := strings.LastIndex(t, "}")
	if lo >= 0 && hi > lo {
		return t[lo : hi+1]
	}
	return t
}
