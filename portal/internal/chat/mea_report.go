package chat

import (
	"strings"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func LastAssistantText(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func FinalTextFromDone(doneText string, msgs []model.Message) string {
	if t := strings.TrimSpace(doneText); t != "" {
		return t
	}
	return LastAssistantText(msgs)
}

func ToolHitsFromTrace(tr *agent.RunTrace) []mea.ToolHit {
	if tr == nil {
		return nil
	}
	out := make([]mea.ToolHit, 0, len(tr.ToolCalls))
	for _, c := range tr.ToolCalls {
		st, idx, repo := tool.HitContractFromResult(c.Result)
		out = append(out, mea.ToolHit{
			ToolName:     c.ToolName,
			HitStatus:    st,
			QueriedIndex: idx,
			Repo:         repo,
			Error:        c.Error,
			Blocked:      c.Blocked,
		})
	}
	return out
}
