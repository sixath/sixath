package agent

import (
	"fmt"
	"strings"
)

const inboundGateSoftPrompt = `未扫入边：宣称「整体流程 / 唯一源头」前必须先 rca_symbol action=references（gopls 失败则用返回里的 grep 回退，看 symbol_ok=false）。多仓时结果会带其它 code roots 的 callers（repos_scanned）。入边为空（inbound_empty）或调用方全在 code roots 外才可以下结论；不要把第一个 handler 当成唯一源头。

Scan inbound callers with rca_symbol action=references before claiming an overall flow / unique source. inbound_empty is a valid stop. Cross-repo callers are included when multiple code roots are configured.

Missing: inbound callers were not scanned this turn.`

var overallFlowClaimPhrases = []string{
	"整体流程", "完整流程", "全部流程", "整个流程",
	"唯一源头", "唯一入口", "只有这一个入口", "只有一个入口",
	"overall flow", "entire flow", "unique source", "only entry",
}

func EvaluateInboundCompletenessGate(records []ToolCallRecord, finalText string) EvidenceGateResult {
	if !claimsOverallFlow(finalText) {
		return EvidenceGateResult{Allow: true}
	}
	if acknowledgesMissingInbound(finalText) {
		return EvidenceGateResult{Allow: true}
	}
	if !usedCodeNavTools(records) {
		return EvidenceGateResult{Allow: true}
	}
	if hasInboundScan(records) {
		return EvidenceGateResult{Allow: true}
	}
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: "inbound callers not scanned",
		Prompt: inboundGateSoftPrompt,
	}
}

func claimsOverallFlow(text string) bool {
	lower := strings.ToLower(text)
	for _, p := range overallFlowClaimPhrases {
		if strings.Contains(text, p) || strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func acknowledgesMissingInbound(text string) bool {
	return strings.Contains(text, "未扫入边") ||
		strings.Contains(strings.ToLower(text), "inbound_empty") ||
		strings.Contains(text, "未扫调用方")
}

func usedCodeNavTools(records []ToolCallRecord) bool {
	for _, rec := range records {
		switch rec.ToolName {
		case "rca_read", "rca_grep", "rca_glob", "rca_symbol":
			return true
		}
	}
	return false
}

func hasInboundScan(records []ToolCallRecord) bool {
	for _, rec := range records {
		if rec.ToolName != "rca_symbol" || rec.Error != "" {
			continue
		}
		m := toolResultMap(rec.Result)
		action := ""
		if m != nil {
			action = anyString(m["action"])
		}
		if action == "" && rec.Arguments != nil {
			action = anyString(rec.Arguments["action"])
		}
		if action != "references" {
			continue
		}
		if m != nil {
			if ok, exists := m["ok"]; exists && fmt.Sprint(ok) == "false" {
				continue
			}
		}
		return true
	}
	return false
}
