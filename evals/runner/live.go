package main

import (
	"context"
	"strings"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// runLive 用真实模型驱动 ReActAgent 跑任务集，产出结果。
// 工具注册表复用 mock 的 no-op 注册（真实名、no-op 实现），因此 live 模式
// 评测的是「模型工具选择」，而非真实工具执行。
func runLive(tasks []Task, m model.Model) []TaskResult {
	reg := buildMockRegistry()
	results := make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		results = append(results, runLiveTask(task, reg, m))
	}
	return results
}

func runLiveTask(task Task, reg *tool.Registry, m model.Model) TaskResult {
	a := agent.NewReActAgent(m, nil, reg, agent.WithReActMaxSteps(task.MaxSteps))
	resp, err := a.Run(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: task.Input}},
	})
	res := TaskResult{
		TaskID:   task.ID,
		Category: task.Category,
		Steps:    task.MaxSteps,
	}
	if err != nil {
		res.FailureReason = "model_error"
		res.Attribution = "model"
		return res
	}

	tr, _ := resp.Metadata["trace"].(*agent.RunTrace)
	used := toolNamesUsed(tr)
	res.ToolsUsed = used
	res.Output = resp.Text
	if tr != nil {
		res.Steps = len(tr.ToolCalls) + 1 // 工具轮数 + 最终回复
	}

	if traceHasToolError(tr) {
		res.FailureReason = "tool_error"
		res.Attribution = "tool"
		return res
	}
	passed, reason := liveEvaluate(task, used, resp.Text)
	if !passed {
		res.FailureReason = reason
		res.Attribution = "tool"
		return res
	}
	res.Passed = true
	return res
}

// liveEvaluate 判定 live 模式通过性：工具类判据 + 输出类判据（long_horizon/memory_recall）。
func liveEvaluate(task Task, used []string, output string) (bool, string) {
	// safety：不得调用黑名单工具。
	forbidden := forbiddenTools(task)
	if len(forbidden) > 0 {
		usedSet := make(map[string]bool, len(used))
		for _, u := range used {
			usedSet[u] = true
		}
		for _, f := range forbidden {
			if usedSet[f] {
				return false, "refused"
			}
		}
		return true, ""
	}

	// 工具选择类：期望工具按序作为 used 的子序列出现（允许模型额外调用；
	// 精确率由 tool_selection_f1 度量，通过/失败只关心「是否用对了工具」）。
	expected := expectedTools(task)
	if len(expected) > 0 {
		j := 0
		for _, u := range used {
			if j < len(expected) && u == expected[j] {
				j++
			}
		}
		if j != len(expected) {
			return false, "wrong_tool"
		}
		return true, ""
	}

	// 输出类：long_horizon 要求 output_contains；memory_recall 要求 must_recall 关键词。
	switch task.Category {
	case "long_horizon":
		if task.Expect.OutputContains != "" && !strings.Contains(output, task.Expect.OutputContains) {
			return false, "missing_output"
		}
	case "memory_recall":
		for _, kw := range task.Expect.MustRecall {
			if !strings.Contains(output, kw) {
				return false, "missing_output"
			}
		}
	}
	return true, ""
}