package main

// TaskResult 一次运行的单任务结果（由 mock/live 驱动产出）。
type TaskResult struct {
	TaskID        string   `json:"task_id"`
	Category      string   `json:"category"`
	Passed        bool     `json:"passed"`
	Steps         int      `json:"steps"`
	ToolsUsed     []string `json:"tools_used,omitempty"`
	Output        string   `json:"output,omitempty"`
	FailureReason string   `json:"failure_reason,omitempty"` // wrong_tool|tool_sequence|missing_output|over_steps|refused|model_error|premature_stop|too_few_tool_calls|no_contrast|stopped_at_mechanism|missing_root_cause|shape_mismatch|infra_error|judge_error|timeout|run_error|hitl_required|tool_error
	Attribution   string   `json:"attribution,omitempty"`    // prompt|tool|schema|model|harness|understanding
	CostUSD       float64  `json:"cost_usd,omitempty"`
	StopNudges    int      `json:"stop_nudges,omitempty"`
	// CriticRounds 被结案审查打回的次数（-critic 开启时）。
	CriticRounds int `json:"critic_rounds,omitempty"`
	// HitMaxSteps 用尽步数后由强制总结收尾：此时 stop_rules / critic 都不会运行。
	HitMaxSteps bool `json:"hit_max_steps,omitempty"`
	// LedgerScore 台账质量（0-1，-ledger 开启时的调查任务才有）；LedgerChecks 为各检查项结果。
	LedgerScore  *float64        `json:"ledger_score,omitempty"`
	LedgerChecks map[string]bool `json:"ledger_checks,omitempty"`
	// Runs / Passes answer_shape 多次运行中有效运行数与通过数；InfraErrors 为因 infra_error/judge_error 丢弃的运行数。
	Runs        int `json:"runs,omitempty"`
	Passes      int `json:"passes,omitempty"`
	InfraErrors int `json:"infra_errors,omitempty"`
	// LostErrors 被丢弃运行的错误（去重，最多 3 条），合并后仍可追查丢失原因。
	LostErrors []string      `json:"lost_errors,omitempty"`
	Judge      *JudgeVerdict `json:"judge,omitempty"`
	Trace      *TraceSummary `json:"trace,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// Summary 汇总指标。
type Summary struct {
	Total          int                     `json:"total"`
	Passed         int                     `json:"passed"`
	CompletionRate float64                 `json:"completion_rate"`
	ToolF1         float64                 `json:"tool_selection_f1"`
	AvgSteps       float64                 `json:"avg_steps"`
	AvgCostUSD     float64                 `json:"avg_cost_usd"`
	ByCategory     map[string]CategoryStat `json:"by_category"`
	Attribution    map[string]int          `json:"attribution"`
	// LedgerScored / AvgLedgerScore 只统计带台账评分的任务。
	LedgerScored    int     `json:"ledger_scored,omitempty"`
	AvgLedgerScore  float64 `json:"avg_ledger_score,omitempty"`
	AvgCriticRounds float64 `json:"avg_critic_rounds,omitempty"`
	HitMaxSteps     int     `json:"hit_max_steps,omitempty"`
	// AnswerShape answer_shape 任务的端到端汇总；这类任务不计入上面的 Total/CompletionRate。
	AnswerShape *ShapeSummary `json:"answer_shape,omitempty"`
}

// CategoryStat 单类别统计。
type CategoryStat struct {
	Total  int     `json:"total"`
	Passed int     `json:"passed"`
	Rate   float64 `json:"completion_rate"`
}

// ShapeSummary answer_shape 端到端汇总。ByAnswerType 中 Total/Passed 按运行次数计。
type ShapeSummary struct {
	Tasks         int                     `json:"tasks"`
	Runs          int                     `json:"runs"`
	Passes        int                     `json:"passes"`
	PassRate      float64                 `json:"e2e_pass_rate"`
	LostRuns      int                     `json:"lost_runs"`
	LostRate      float64                 `json:"lost_rate"`
	ByAnswerType  map[string]CategoryStat `json:"by_answer_type"`
	ToolErrorRate float64                 `json:"tool_error_rate"`
	EmptyRate     float64                 `json:"empty_rate"`
	SuspectRate   float64                 `json:"suspect_rate"`
	ArgRejectRate float64                 `json:"arg_reject_rate"`
	// ArgRejectsByKeyword 按规则 keyword 统计参数拒绝次数（一次调用命中多条规则时各计一次）。
	ArgRejectsByKeyword map[string]int `json:"arg_rejects_by_keyword,omitempty"`
	Rule4Failures       int            `json:"rule4_failures"`
	AvgSteps            float64        `json:"avg_steps"`
	Attribution         map[string]int `json:"attribution"`
}

// ComputeSummary 由结果与任务集计算汇总。tasks 用于工具选择 F1 的期望比对。
func ComputeSummary(results []TaskResult, tasks []Task) Summary {
	s := Summary{
		ByCategory:  map[string]CategoryStat{},
		Attribution: map[string]int{},
	}
	if len(results) == 0 {
		return s
	}

	expectTools := map[string][]string{}
	expectSeq := map[string][]string{}
	for _, t := range tasks {
		if len(t.Expect.ToolSequence) > 0 {
			expectSeq[t.ID] = t.Expect.ToolSequence
		} else if len(t.Expect.Tools) > 0 {
			expectTools[t.ID] = t.Expect.Tools
		}
	}

	var shapeResults []TaskResult
	var stepsSum, costSum, f1Sum, ledgerSum float64
	f1N, criticSum := 0, 0
	for _, r := range results {
		if r.Category == "answer_shape" {
			shapeResults = append(shapeResults, r)
			continue
		}
		s.Total++
		if r.Passed {
			s.Passed++
		}
		stepsSum += float64(r.Steps)
		costSum += r.CostUSD
		criticSum += r.CriticRounds
		if r.HitMaxSteps {
			s.HitMaxSteps++
		}
		if r.LedgerScore != nil {
			s.LedgerScored++
			ledgerSum += *r.LedgerScore
		}

		cs := s.ByCategory[r.Category]
		cs.Total++
		if r.Passed {
			cs.Passed++
		}
		s.ByCategory[r.Category] = cs

		if r.Attribution != "" {
			s.Attribution[r.Attribution]++
		}

		if seq, ok := expectSeq[r.TaskID]; ok {
			if sc := sequenceF1(r.ToolsUsed, seq); sc >= 0 {
				f1Sum += sc
				f1N++
			}
		} else if exp, ok := expectTools[r.TaskID]; ok {
			if sc := setF1(r.ToolsUsed, exp); sc >= 0 {
				f1Sum += sc
				f1N++
			}
		}
	}

	if s.Total > 0 {
		s.CompletionRate = float64(s.Passed) / float64(s.Total)
		s.AvgSteps = stepsSum / float64(s.Total)
		s.AvgCostUSD = costSum / float64(s.Total)
		s.AvgCriticRounds = float64(criticSum) / float64(s.Total)
	}
	if s.LedgerScored > 0 {
		s.AvgLedgerScore = ledgerSum / float64(s.LedgerScored)
	}
	if f1N > 0 {
		s.ToolF1 = f1Sum / float64(f1N)
	}
	for k, cs := range s.ByCategory {
		if cs.Total > 0 {
			cs.Rate = float64(cs.Passed) / float64(cs.Total)
			s.ByCategory[k] = cs
		}
	}
	s.AnswerShape = computeShapeSummary(shapeResults, tasks)
	return s
}

// setF1 工具集合选择的 F1；期望为空时返回 -1（不参与统计）。
func setF1(used, expected []string) float64 {
	if len(expected) == 0 {
		return -1
	}
	if len(used) == 0 {
		return 0
	}
	exp := stringSet(expected)
	inter := 0
	seen := map[string]bool{}
	for _, u := range used {
		if exp[u] && !seen[u] {
			inter++
		}
		seen[u] = true
	}
	p := float64(inter) / float64(len(seen))
	r := float64(inter) / float64(len(expected))
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// sequenceF1 严格工具序列的按位匹配 F1；期望为空时返回 -1。
func sequenceF1(used, expected []string) float64 {
	if len(expected) == 0 {
		return -1
	}
	if len(used) == 0 {
		return 0
	}
	match := 0
	for i := 0; i < len(expected) && i < len(used); i++ {
		if used[i] == expected[i] {
			match++
		}
	}
	p := float64(match) / float64(len(used))
	r := float64(match) / float64(len(expected))
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func stringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
