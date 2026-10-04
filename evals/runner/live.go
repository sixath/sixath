package main

import (
	"context"
	"errors"
	"strings"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/investigate/cases"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// runLive 用真实模型驱动 ReActAgent 跑任务集，产出结果。
// 工具注册表复用 mock 的 no-op 注册（真实名、no-op 实现），因此 live 模式
// 评测的是「模型工具选择」，而非真实工具执行。
// 带 fixtures 的任务改用合成返回的工具，评测「能否沿证据链定位根因」。
func runLive(tasks []Task, m model.Model, lo liveOptions) []TaskResult {
	reg := buildMockRegistry()
	results := make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		if task.Category != "answer_shape" {
			results = append(results, runLiveTask(task, reg, m, lo))
			continue
		}
		n := lo.Repeat
		if n < 1 {
			n = 1
		}
		runs := make([]TaskResult, 0, n)
		for i := 0; i < n; i++ {
			runs = append(runs, runLiveTask(task, reg, m, lo))
		}
		results = append(results, mergeRuns(runs))
	}
	return results
}

// liveOptions 用于 A/B 评估工作区能力：StopHooks 为 stop_rules；Ledger 为 investigation 台账。
type liveOptions struct {
	StopHooks     []agent.StopHook
	MaxStopNudges int
	Ledger        bool
	// LedgerStore 为台账存储；stop_rules 的 when_investigation_gaps 需读同一个存储。
	LedgerStore tool.InvestigationStore
	// Critic 非 nil 时挂结案审查（规则级 + 模型级），CriticModel 为空则复用被测模型。
	Critic      *agent.CriticConfig
	CriticModel model.Model
	// Timeline 注册 timeline 工具（对 fixture 的文本日志在本地分桶）。
	Timeline bool
	// ExtraSteps 加到每个任务的 max_steps 上；A/B 两组应取相同值。
	ExtraSteps int
	// MaxOutputTokens 为单次模型回复上限；0 使用框架默认（1024，长结论会被截断）。
	MaxOutputTokens int
	// Cases 非 nil 时接入案例库（需同时 Ledger）：set_symptom 召回已确认案例，并注册 case_library。
	Cases cases.Store
	// Judge answer_shape 任务的打分器；Repeat 为 answer_shape 每题运行次数（<=1 为 1 次），其他类别不重复。
	Judge  *Judge
	Repeat int
	// JudgeTimeout 为单次 judge 调用超时；<=0 不限时。
	JudgeTimeout time.Duration
}

func runLiveTask(task Task, reg *tool.Registry, m model.Model, lo liveOptions) TaskResult {
	var rec *fixtureRecorder
	if len(task.Fixtures) > 0 {
		rec = &fixtureRecorder{}
		reg = buildFixtureRegistry(task.Fixtures, rec)
	}
	maxSteps := task.MaxSteps + lo.ExtraSteps
	opts := []agent.ReActOption{agent.WithReActMaxSteps(maxSteps)}
	if lo.MaxOutputTokens > 0 {
		opts = append(opts, agent.WithReActMaxOutputTokens(lo.MaxOutputTokens))
	}
	store := lo.LedgerStore
	if store == nil {
		store = tool.DefaultInvestigationStore
	}
	hooks := append([]agent.StopHook{}, lo.StopHooks...)
	if !agent.HasStopHookPrefix(hooks, agent.SuspectEvidenceRuleID) {
		hooks = append(hooks, agent.DefaultStopHooks()...)
	}
	if lo.Critic != nil {
		cm := lo.CriticModel
		if cm == nil {
			cm = m
		}
		hooks = append(hooks, agent.NewCriticHook(*lo.Critic, store, cm))
	}
	if len(hooks) > 0 {
		opts = append(opts, agent.WithReActStopHooks(hooks...), agent.WithReActMaxStopNudges(lo.MaxStopNudges))
	}
	if (lo.Ledger || lo.Timeline) && len(task.Fixtures) == 0 {
		reg = buildFixtureRegistry(nil, &fixtureRecorder{})
	}
	if lo.Ledger {
		if _, exists := reg.Get(tool.InvestigationToolName); !exists {
			var resolve tool.CaseStoreResolver
			if lo.Cases != nil {
				resolve = func(context.Context) cases.Store { return lo.Cases }
			}
			_ = tool.RegisterInvestigationToolWithOptions(reg, store, tool.InvestigationToolOptions{Cases: resolve})
			if resolve != nil {
				_ = tool.RegisterCaseLibraryTool(reg, store, resolve)
			}
		}
		opts = append(opts, agent.WithReActToolSuccessHook(agent.InvestigationObserver(store)))
	}
	if lo.Timeline {
		if _, exists := reg.Get(tool.TimelineToolName); !exists {
			_ = tool.RegisterTimelineTool(reg)
		}
		if _, exists := reg.Get(tool.FieldHistoryToolName); !exists {
			_ = tool.RegisterFieldHistoryTool(reg)
		}
		if _, exists := reg.Get(tool.FsCompareToolName); !exists {
			_ = tool.RegisterFsCompareTool(reg)
		}
	}
	a := agent.NewReActAgent(m, nil, reg, opts...)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "eval-"+task.ID)
	resp, err := a.Run(ctx, &agent.Request{
		Messages: []model.Message{{Role: "user", Content: task.Input}},
	})
	res := TaskResult{
		TaskID:   task.ID,
		Category: task.Category,
		Steps:    maxSteps,
	}
	if err != nil {
		res.FailureReason = "model_error"
		res.Attribution = "model"
		res.Error = err.Error()
		res.HitMaxSteps = strings.Contains(err.Error(), "max steps")
		var re *agent.RunError
		if errors.As(err, &re) && re.Trace != nil {
			res.ToolsUsed = toolNamesUsed(re.Trace)
			res.Steps = len(re.Trace.ToolCalls) + 1
			res.StopNudges = len(re.Trace.StopHookContinues)
			res.CriticRounds = criticRounds(re.Trace.StopHookContinues)
			if len(re.Trace.Errors) > 0 {
				res.Error = strings.Join(re.Trace.Errors, "; ")
			}
		}
		// answer_shape 的模型调用失败（超时、网关错误）记为丢失运行，不进通过率分母；跑满步数仍算失败。
		if task.Category == "answer_shape" && !res.HitMaxSteps {
			res.FailureReason = "infra_error"
			res.Attribution = ""
		}
		return res
	}

	tr, _ := resp.Metadata["trace"].(*agent.RunTrace)
	used := toolNamesUsed(tr)
	res.ToolsUsed = used
	res.Output = resp.Text
	if tr != nil {
		res.Steps = len(tr.ToolCalls) + 1 // 工具轮数 + 最终回复
		res.StopNudges = len(tr.StopHookContinues)
		res.CriticRounds = criticRounds(tr.StopHookContinues)
		res.HitMaxSteps = tr.ModelCalls > maxSteps
	}
	// 模型最终不可用时 harness 返回由工具结果拼出的降级答复（无 error），按模型调用失败同样归类。
	if tr != nil && tr.ModelUnavailable {
		res.FailureReason, res.Attribution = "model_error", "model"
		res.Error = strings.Join(tr.Errors, "; ")
		if task.Category == "answer_shape" {
			res.FailureReason, res.Attribution = "infra_error", ""
		}
		return res
	}

	if task.Category == "answer_shape" {
		jctx, cancel := context.Background(), context.CancelFunc(func() {})
		if lo.JudgeTimeout > 0 {
			jctx, cancel = context.WithTimeout(jctx, lo.JudgeTimeout)
		}
		shaped := scoreAnswerShape(jctx, lo.Judge, task, resp.Text, summarizeRunTrace(tr))
		cancel()
		shaped.StopNudges, shaped.CriticRounds, shaped.HitMaxSteps = res.StopNudges, res.CriticRounds, res.HitMaxSteps
		return shaped
	}

	if task.Category == "investigation" {
		ev := investigationEvidence{Output: resp.Text}
		if tr != nil {
			ev.ToolCalls = len(tr.ToolCalls)
		}
		if rec != nil {
			ev.ContrastHits = rec.contrastHits
		}
		if lo.Ledger {
			q := scoreLedger(task, store.Get("eval-"+task.ID))
			res.LedgerScore, res.LedgerChecks = &q.Score, q.Checks
		}
		if ok, reason := evaluateInvestigation(task, ev); !ok {
			res.FailureReason = reason
			res.Attribution = investigationAttribution(reason)
			return res
		}
		res.Passed = true
		return res
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