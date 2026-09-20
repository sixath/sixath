package main

// TaskResult 一次运行的单任务结果（由 mock/live 驱动产出）。
type TaskResult struct {
	TaskID        string   `json:"task_id"`
	Category      string   `json:"category"`
	Passed        bool     `json:"passed"`
	Steps         int      `json:"steps"`
	ToolsUsed     []string `json:"tools_used,omitempty"`
	Output        string   `json:"output,omitempty"`
	FailureReason string   `json:"failure_reason,omitempty"` // wrong_tool|tool_sequence|missing_output|over_steps|refused|model_error
	Attribution   string   `json:"attribution,omitempty"`    // prompt|tool|schema|model
	CostUSD       float64  `json:"cost_usd,omitempty"`
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
}

// CategoryStat 单类别统计。
type CategoryStat struct {
	Total  int     `json:"total"`
	Passed int     `json:"passed"`
	Rate   float64 `json:"completion_rate"`
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

	var stepsSum, costSum, f1Sum float64
	f1N := 0
	for _, r := range results {
		s.Total++
		if r.Passed {
			s.Passed++
		}
		stepsSum += float64(r.Steps)
		costSum += r.CostUSD

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

	s.CompletionRate = float64(s.Passed) / float64(s.Total)
	s.AvgSteps = stepsSum / float64(s.Total)
	s.AvgCostUSD = costSum / float64(s.Total)
	if f1N > 0 {
		s.ToolF1 = f1Sum / float64(f1N)
	}
	for k, cs := range s.ByCategory {
		if cs.Total > 0 {
			cs.Rate = float64(cs.Passed) / float64(cs.Total)
			s.ByCategory[k] = cs
		}
	}
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