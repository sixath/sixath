package chat

import (
	"strings"

	"github.com/sixath/framework/model"
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
