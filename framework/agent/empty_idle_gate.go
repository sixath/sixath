package agent

import (
	"strings"

	"github.com/sixath/framework/model"
)

const (
	emptyIdleSpeakReason = "empty_idle"
	emptyIdleRetryBase   = "刚才没有写出给用户看的正文。不要再只检索或罗列工具。用已有工具结果直接回答用户问题；若没有能完成该操作的工具，明确说做不到，并列出已检索到的相关工具名。"
)

func (a *ReActAgent) checkEmptyIdleGate(req *Request, messages []model.Message, trace *RunTrace, finalText string, allowInject, hasStepRoom bool) evidenceGateCheck {
	if strings.TrimSpace(finalText) != "" {
		return evidenceGateCheck{}
	}
	if trace == nil || len(trace.ToolCalls) == 0 {
		return evidenceGateCheck{}
	}
	if allowInject && hasStepRoom && trace.EmptyIdleNudges == 0 {
		trace.Errors = append(trace.Errors, emptyIdleSpeakReason)
		q := taskLockQFromRequest(req)
		if q == "" {
			q = originalUserQuestion(req, messages)
		}
		return evidenceGateCheck{
			Inject:    true,
			EmptyIdle: true,
			Prompt:    appendTaskLockToSummaryPrompt(emptyIdleRetryBase, q),
		}
	}
	return evidenceGateCheck{}
}
