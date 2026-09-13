package main

import (
	"context"
	"fmt"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// scriptedModel 按脚本依次返回工具调用，耗尽后返回 final 文本，用于确定性回放。
type scriptedModel struct {
	steps []model.ToolStep
	final string
	calls int
}

func (m *scriptedModel) Generate(_ context.Context, _ string, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: m.final}, nil
}

func (m *scriptedModel) Chat(_ context.Context, _ []model.Message, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: m.final}, nil
}

func (m *scriptedModel) Embed(_ context.Context, _ []string, _ ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (m *scriptedModel) ChatWithTools(_ context.Context, _ []model.Message, _ *tool.Registry, _ ...model.Option) (*model.Generation, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.steps) {
		return &model.Generation{Raw: m.steps[idx]}, nil
	}
	return &model.Generation{Text: m.final}, nil
}

// mockToolNames 为 eval 任务集引用的真实工具名（与 framework/tool 注册表对齐）。
var mockToolNames = []string{
	"list_tables", "describe_table", "execute_read", "execute_write",
	"web_search", "read_file", "write_file", "terminal",
	"memory_recall", "memory_remember", "todo", "ask_user",
	"ssh_exec", "scp", "browser_navigate", "jaeger_trace", "es_log_query", "send_to_wecom",
}

// buildMockRegistry 注册 eval 引用的工具（真实名、no-op 实现），用于校验工具选择与执行管线。
func buildMockRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	for _, n := range mockToolNames {
		name := n
		_ = reg.Register(tool.Tool{
			Name:        name,
			Description: "mock " + name,
			Execute: func(_ context.Context, _ map[string]any) (any, error) {
				return "ok", nil
			},
		})
	}
	return reg
}

// expectedTools 返回该任务期望调用的工具序列。
func expectedTools(task Task) []string {
	switch task.Category {
	case "multi_tool":
		if len(task.Expect.ToolSequence) > 0 {
			return task.Expect.ToolSequence
		}
		return task.Expect.Tools
	case "single_tool":
		return task.Expect.Tools
	case "hitl":
		return []string{"ask_user"}
	default:
		return nil
	}
}

// forbiddenTools 返回该任务不得调用的工具（safety 黑名单）。
func forbiddenTools(task Task) []string {
	if task.Category == "safety" {
		return task.Expect.MustRefuse
	}
	return nil
}

// runMock 对每条任务用脚本化模型驱动 ReActAgent，校验工具选择与执行管线。
func runMock(tasks []Task) []TaskResult {
	reg := buildMockRegistry()
	results := make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		results = append(results, runMockTask(task, reg))
	}
	return results
}

func runMockTask(task Task, reg *tool.Registry) TaskResult {
	expected := expectedTools(task)

	// 脚本化：依次调用期望工具，然后 final。
	steps := make([]model.ToolStep, 0, len(expected))
	for i, name := range expected {
		steps = append(steps, model.ToolStep{
			Used:       true,
			ToolCallID: fmt.Sprintf("call-%d", i+1),
			ToolName:   name,
			Arguments:  map[string]any{},
		})
	}
	m := &scriptedModel{steps: steps, final: "mock final"}
	a := agent.NewReActAgent(m, nil, reg, agent.WithReActMaxSteps(len(steps)+1))

	resp, err := a.Run(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: task.Input}},
	})
	res := TaskResult{
		TaskID:   task.ID,
		Category: task.Category,
		Steps:    len(steps) + 1,
	}
	if err != nil {
		// 脚本化模型确定性返回、不会失败，Run 报错只可能是工具执行/管线问题（如 tool not found）。
		res.FailureReason = "tool_error"
		res.Attribution = "tool"
		return res
	}

	tr, _ := resp.Metadata["trace"].(*agent.RunTrace)
	used := toolNamesUsed(tr)
	res.ToolsUsed = used
	res.Output = resp.Text

	if traceHasToolError(tr) {
		res.FailureReason = "tool_error"
		res.Attribution = "tool"
		return res
	}
	if !mockPassed(task, used) {
		res.FailureReason = "wrong_tool"
		res.Attribution = "tool"
		return res
	}
	res.Passed = true
	return res
}

// traceHasToolError 判断是否有工具执行失败（如 tool not found）。
func traceHasToolError(tr *agent.RunTrace) bool {
	if tr == nil {
		return false
	}
	for _, c := range tr.ToolCalls {
		if c.Error != "" {
			return true
		}
	}
	return false
}

// toolNamesUsed 提取 trace 中实际调用的工具名（去重保持顺序）。
func toolNamesUsed(tr *agent.RunTrace) []string {
	if tr == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range tr.ToolCalls {
		if c.ToolName == "" || seen[c.ToolName] {
			continue
		}
		seen[c.ToolName] = true
		out = append(out, c.ToolName)
	}
	return out
}

// mockPassed 判定脚本化回放是否命中期望工具。
func mockPassed(task Task, used []string) bool {
	expected := expectedTools(task)
	forbidden := forbiddenTools(task)

	// safety：不得调用黑名单工具。
	if len(forbidden) > 0 {
		usedSet := map[string]bool{}
		for _, u := range used {
			usedSet[u] = true
		}
		for _, f := range forbidden {
			if usedSet[f] {
				return false
			}
		}
		return true
	}

	// 工具选择类：期望工具需按序命中。
	if len(expected) > 0 {
		if len(used) != len(expected) {
			return false
		}
		for i, want := range expected {
			if used[i] != want {
				return false
			}
		}
		return true
	}

	// 无工具期望的类别（long_horizon/memory_recall）：mock 模式下视为通过（真实语义需 live 模式）。
	return true
}