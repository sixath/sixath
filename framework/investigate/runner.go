package investigate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// buildExecute 返回 deep_investigate 的执行体：组装子 registry（代码组 + 日志组，
// 不含自身与其他无关工具），跑子 ReAct 循环，把结论与证据引用打包返回。
func buildExecute(parent *tool.Registry, cfg Config) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		started := time.Now()
		question, _ := params["question"].(string)
		question = strings.TrimSpace(question)
		if question == "" {
			err := errors.New("investigate: question is required")
			return map[string]any{"ok": false, "error": err.Error(), "error_code": tool.ErrorPermanent}, err
		}
		hints, _ := params["hints"].(string)

		sub := tool.NewEmptyRegistry()
		names := 0
		for _, name := range append(append([]string{}, codeToolNames...), logToolNames...) {
			if t, ok := parent.Get(name); ok {
				// 重复包装（timeout/参数校验）无副作用：内层先触发，语义不变。
				if err := sub.Register(t); err == nil {
					names++
				}
			}
		}
		if names == 0 {
			err := errors.New("investigate: no investigation tools available")
			return map[string]any{"ok": false, "error": err.Error(), "error_code": tool.ErrorPermanent}, err
		}

		userText := question
		if h := strings.TrimSpace(hints); h != "" {
			userText += "\n\n已知线索：" + h
		}
		agentOpts := []harness.ReActOption{
			harness.WithReActMaxSteps(cfg.maxSteps()),
			harness.WithReActMaxOutputTokens(cfg.maxOutputTokens()),
			harness.WithReActSystemPrompt(playbookPrompt),
		}
		subAgent := harness.NewReActAgent(cfg.Model, nil, sub, agentOpts...)

		parentRID, _ := ctx.Value(tool.ContextKeyRequestID).(string)
		resp, err := subAgent.Run(ctx, &harness.Request{
			RequestID: parentRID,
			Messages:  []model.Message{{Role: "user", Content: userText}},
		})
		if err != nil {
			return map[string]any{
				"ok":          false,
				"error":       err.Error(),
				"error_code":  tool.ErrorTransient,
				"duration_ms": time.Since(started).Milliseconds(),
				"steps_taken": 0,
			}, err
		}

		conclusion, insufficient := parseConclusion(resp.Text)
		refs := collectRefs(resp.Messages)
		return map[string]any{
			"ok":                    true,
			"conclusion":            conclusion,
			"evidence_refs":         refs,
			"insufficient_evidence": insufficient,
			"steps_taken":           countToolMessages(resp.Messages),
			"duration_ms":           time.Since(started).Milliseconds(),
		}, nil
	}
}

// parseConclusion 从子 agent 最终文本中剥离状态行，返回正文与「证据不足」标记。
// 状态行约定见 playbookPrompt：「状态: 证据充分」或「状态: 证据不足」（全角冒号、句末标点、
// 全角空格兼容）。若全文未见状态行，则按证据不足处理。
func parseConclusion(text string) (conclusion string, insufficient bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	keep := lines[:0]
	seen := false
	for _, l := range lines {
		norm := strings.NewReplacer("：", ":", " ", "", "　", "").Replace(strings.TrimSpace(l))
		norm = strings.TrimRight(norm, "。.") // 容忍句末标点
		if norm == "状态:证据充分" {
			seen = true
			continue
		}
		if norm == "状态:证据不足" {
			seen = true
			insufficient = true
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n")), insufficient || !seen
}

// collectRefs 从子 agent 的 tool 消息中提取全部 evidence_refs。
func collectRefs(msgs []model.Message) []tool.EvidenceRef {
	var results []any
	for _, m := range msgs {
		if !strings.EqualFold(m.Role, "tool") {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(m.Content), &v); err == nil {
			results = append(results, v)
		}
	}
	return tool.CollectEvidenceRefs(results...)
}

// countToolMessages 统计子 agent 消息中的工具调用结果数（即实际工具步数）。
func countToolMessages(msgs []model.Message) int {
	n := 0
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "tool") {
			n++
		}
	}
	return n
}
