package chat

import (
	"os"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
)

var storedExtractionYAML *config.MemoryExtraction

// SetMemoryExtractionConfig stores agent_extra memory_extraction settings.
func SetMemoryExtractionConfig(cfg *config.MemoryExtraction) {
	if cfg == nil {
		storedExtractionYAML = nil
		return
	}
	cp := *cfg
	storedExtractionYAML = &cp
}

func memoryExtractionEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("SATH_MEMORY_EXTRACTION_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return storedExtractionYAML != nil && storedExtractionYAML.Enabled
}

// LastUserMessageContent returns the latest user message body from a session history list.
func LastUserMessageContent(messages []*biz.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}
