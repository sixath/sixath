package main

import (
	"context"
	"slices"
	"strings"
)

// scoreAnswerShape 用 judge 判定一次运行。judge 未配置、调用失败或输出不可解析时记 judge_error（不计入通过率）。
func scoreAnswerShape(ctx context.Context, j *Judge, task Task, answer string, ts TraceSummary) TaskResult {
	res := TaskResult{
		TaskID:   task.ID,
		Category: task.Category,
		Output:   answer,
		Steps:    len(ts.Calls) + 1,
		Trace:    &ts,
	}
	seen := map[string]bool{}
	for _, c := range ts.Calls {
		if c.Tool != "" && !seen[c.Tool] {
			seen[c.Tool] = true
			res.ToolsUsed = append(res.ToolsUsed, c.Tool)
		}
	}
	if j == nil || j.Model == nil {
		res.FailureReason = "judge_error"
		res.Error = "judge not configured"
		return res
	}
	v, err := j.Evaluate(ctx, task, answer, ts)
	if err != nil {
		res.FailureReason = "judge_error"
		res.Error = err.Error()
		return res
	}
	res.Judge = &v
	if v.Passed() {
		res.Passed = true
		return res
	}
	res.FailureReason = "shape_mismatch"
	res.Attribution = v.Attribution
	if res.Attribution == "" {
		res.Attribution = "model"
	}
	return res
}

func isLostRun(r TaskResult) bool {
	return r.FailureReason == "infra_error" || r.FailureReason == "judge_error"
}

// mergeRuns 合并同一任务的多次运行：丢失的运行不计入 Runs；Passed 取严格多数（Passes*2 > Runs）。
// 代表结果取与多数结论一致的最后一次有效运行，便于报告展示对应的输出与轨迹。
func mergeRuns(runs []TaskResult) TaskResult {
	if len(runs) == 0 {
		return TaskResult{}
	}
	var valid []TaskResult
	var lostErrs []string
	lost := 0
	for _, r := range runs {
		if isLostRun(r) {
			lost++
			if e := strings.TrimSpace(r.Error); e != "" && len(lostErrs) < 3 && !slices.Contains(lostErrs, e) {
				lostErrs = append(lostErrs, e)
			}
			continue
		}
		valid = append(valid, r)
	}
	if len(valid) == 0 {
		out := runs[len(runs)-1]
		out.Runs, out.Passes, out.InfraErrors, out.Passed = 0, 0, lost, false
		out.LostErrors = lostErrs
		return out
	}
	passes := 0
	for _, v := range valid {
		if v.Passed {
			passes++
		}
	}
	passed := passes*2 > len(valid)
	out := valid[len(valid)-1]
	for i := len(valid) - 1; i >= 0; i-- {
		if valid[i].Passed == passed {
			out = valid[i]
			break
		}
	}
	out.Runs, out.Passes, out.InfraErrors, out.Passed = len(valid), passes, lost, passed
	out.LostErrors = lostErrs
	return out
}

// runCounts 兼容未经 mergeRuns 的旧结果（Runs 与 InfraErrors 均为 0）。
func runCounts(r TaskResult) (runs, passes, lost int) {
	if r.Runs > 0 || r.InfraErrors > 0 {
		return r.Runs, r.Passes, r.InfraErrors
	}
	if isLostRun(r) {
		return 0, 0, 1
	}
	if r.Passed {
		return 1, 1, 0
	}
	return 1, 0, 0
}

func computeShapeSummary(results []TaskResult, tasks []Task) *ShapeSummary {
	if len(results) == 0 {
		return nil
	}
	types := map[string]string{}
	for _, t := range tasks {
		types[t.ID] = t.Expect.AnswerType
	}
	ss := &ShapeSummary{ByAnswerType: map[string]CategoryStat{}, Attribution: map[string]int{}}
	calls, errs, empties, suspects, rejects, steps := 0, 0, 0, 0, 0, 0
	for _, r := range results {
		ss.Tasks++
		runs, passes, lost := runCounts(r)
		ss.Runs += runs
		ss.Passes += passes
		ss.LostRuns += lost
		at := types[r.TaskID]
		if at == "" {
			at = "unknown"
		}
		cs := ss.ByAnswerType[at]
		cs.Total += runs
		cs.Passed += passes
		ss.ByAnswerType[at] = cs
		if !r.Passed && r.Attribution != "" {
			ss.Attribution[r.Attribution]++
		}
		if r.Judge != nil {
			for _, c := range r.Judge.Checks {
				if c.ID == 4 && !c.Pass {
					ss.Rule4Failures++
				}
			}
		}
		if r.Trace != nil {
			calls += len(r.Trace.Calls)
			errs += r.Trace.ErrorCount()
			empties += r.Trace.EmptyCount()
			suspects += r.Trace.SuspectCount()
			rejects += r.Trace.RejectCount()
			for _, c := range r.Trace.Calls {
				for _, k := range c.RejectKeywords {
					if ss.ArgRejectsByKeyword == nil {
						ss.ArgRejectsByKeyword = map[string]int{}
					}
					ss.ArgRejectsByKeyword[k]++
				}
			}
		}
		steps += r.Steps
	}
	if ss.Runs > 0 {
		ss.PassRate = float64(ss.Passes) / float64(ss.Runs)
	}
	if total := ss.Runs + ss.LostRuns; total > 0 {
		ss.LostRate = float64(ss.LostRuns) / float64(total)
	}
	if calls > 0 {
		ss.ToolErrorRate = float64(errs) / float64(calls)
		ss.EmptyRate = float64(empties) / float64(calls)
		ss.SuspectRate = float64(suspects) / float64(calls)
		ss.ArgRejectRate = float64(rejects) / float64(calls)
	}
	ss.AvgSteps = float64(steps) / float64(ss.Tasks)
	for k, cs := range ss.ByAnswerType {
		if cs.Total > 0 {
			cs.Rate = float64(cs.Passed) / float64(cs.Total)
			ss.ByAnswerType[k] = cs
		}
	}
	return ss
}
