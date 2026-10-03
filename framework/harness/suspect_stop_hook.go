package harness

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sixath/framework/tool"
)

const (
	// SuspectEvidenceRuleID 为 suspect_evidence stop hook 的 RuleID 与独立预算前缀。
	SuspectEvidenceRuleID = "suspect_evidence"
	// EnvStopSuspect=off 时关闭默认的 suspect_evidence stop hook。
	EnvStopSuspect = "SATH_STOP_SUSPECT"
)

var (
	negativeClaim  = regexp.MustCompile(`(?i)(没有(查到|找到|数据|记录|日志|结果|相关)|查不到|未查到|未找到|找不到|不存在|未发现|无(数据|记录|结果|日志|匹配)|(?:^|[^0-9.])0\s*条|(?:^|[^0-9.])0\s*(rows?|results?|hits|records|matches|logs)\b|\bno (data|results?|matches|records|logs)\b|not found|nothing found)`)
	negativeExempt = regexp.MustCompile(`(?i)(如果|若|假如|一旦|如未|如无|\bif\b|\bunless\b|\bwhen\b)`)
)

type suspectEvidenceHook struct{}

// NewSuspectEvidenceHook 在最终回答断言"查不到/没有数据"、而本轮检索结果只有 empty/suspect 且至少一个 suspect 时提醒一次。
func NewSuspectEvidenceHook() StopHook { return suspectEvidenceHook{} }

// DefaultStopHooks 返回框架内置、默认开启的 stop hook；调用方须与工作区规则合并后一次性传给 WithReActStopHooks（该选项为替换语义）。
func DefaultStopHooks() []StopHook {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStopSuspect))) {
	case "0", "false", "off", "no", "disable", "disabled":
		return nil
	}
	return []StopHook{NewSuspectEvidenceHook()}
}

// HasStopHookPrefix 报告 hooks 中是否已有预算前缀为 prefix 的 BudgetedStopHook（用于合并默认 hook 时去重）。
func HasStopHookPrefix(hooks []StopHook, prefix string) bool {
	for _, h := range hooks {
		if b, ok := h.(BudgetedStopHook); ok {
			if p, _ := b.StopBudget(); p == prefix {
				return true
			}
		}
	}
	return false
}

func (suspectEvidenceHook) StopBudget() (string, int) { return SuspectEvidenceRuleID, 1 }

func (suspectEvidenceHook) OnStop(_ context.Context, in StopHookInput) StopDecision {
	if !hasNegativeClaim(in.Text) {
		return StopDecision{}
	}
	var lines []string
	for _, rec := range in.ToolCalls {
		if rec.Error != "" || !rec.Allowed {
			continue
		}
		result := tool.DecodeJSONResult(rec.Result)
		st, _, _ := tool.HitContractFromResult(result)
		switch st {
		case tool.HitStatusHits, tool.HitStatusError:
			return StopDecision{}
		case tool.HitStatusSuspect:
			lines = append(lines, fmt.Sprintf("- %s：%s", rec.ToolName, suspectHint(result)))
		}
	}
	if len(lines) == 0 {
		return StopDecision{}
	}
	msg := "你的结论是「查不到/没有数据」，但以下检索结果被标记为可疑（suspect），更可能是查询条件写错而不是真的没有数据：\n" +
		strings.Join(lines, "\n") +
		"\n请按提示修正条件后重查；若确认无法查到，在回答中明确说明这些结果可疑及原因。"
	return StopDecision{Continue: true, RuleID: SuspectEvidenceRuleID, Message: msg}
}

func suspectHint(result any) string {
	if d := tool.DiagnosisFromResult(result); d != nil && d.Hint != "" {
		return d.Hint
	}
	if m, ok := result.(map[string]any); ok {
		if miss, ok := m["roots_missing"]; ok {
			return fmt.Sprintf("search roots missing: %v", miss)
		}
		if m["root_missing"] == true {
			return fmt.Sprintf("search root %v does not exist", m["root"])
		}
	}
	return "result flagged as suspect"
}

func hasNegativeClaim(text string) bool {
	for _, loc := range negativeClaim.FindAllStringIndex(text, -1) {
		if !negativeExempt.MatchString(enclosingSentence(text, loc[0], loc[1])) {
			return true
		}
	}
	return false
}
