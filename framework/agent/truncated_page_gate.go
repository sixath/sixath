package agent

import (
	"fmt"
	"strings"

	"github.com/sixath/framework/tool"
)

const (
	truncatedPageReason    = "truncated_page"
	maxTruncatedPageNudges = 8
)

func EvaluateTruncatedPageGate(trace *RunTrace, q string) EvidenceGateResult {
	if trace == nil || !qWantsCompleteESPage(q) {
		return EvidenceGateResult{Allow: true}
	}
	rec, ok := lastSuccessfulESLogQuery(trace)
	if !ok {
		return EvidenceGateResult{Allow: true}
	}
	view := tool.SpillFields(rec.Result)
	more := view.HasMore || view.Truncated
	if !more {
		return EvidenceGateResult{Allow: true}
	}
	from := 0
	if view.ContinueFrom > 0 {
		from = view.ContinueFrom
	} else if view.NextFrom > 0 {
		from = view.NextFrom
	}
	prompt := fmt.Sprintf(
		"上一页 es_log_query 还没翻完（truncated/has_more，continue_from=%d）。用户要查全量并解析，不要抽样后提前总结。请用 from=%d 继续翻页，直到 truncated=false；用 result_stats 读 spilled path，用 extracted_ids 收集全部 id。",
		from, from,
	)
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: truncatedPageReason,
		Prompt: prompt,
	}
}

func qWantsCompleteESPage(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	for _, p := range []string{"全部", "所有", "解析", "这些", "统计", "分布", "继续", "剩下"} {
		if strings.Contains(q, p) {
			return true
		}
	}
	return false
}

func lastSuccessfulESLogQuery(trace *RunTrace) (ToolCallRecord, bool) {
	if trace == nil {
		return ToolCallRecord{}, false
	}
	for i := len(trace.ToolCalls) - 1; i >= 0; i-- {
		rec := trace.ToolCalls[i]
		if strings.TrimSpace(rec.ToolName) != "es_log_query" || rec.Error != "" {
			continue
		}
		return rec, true
	}
	return ToolCallRecord{}, false
}
