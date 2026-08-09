package chat

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/obs"
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

func memoryExtractionMaxFacts() int {
	if storedExtractionYAML != nil && storedExtractionYAML.MaxFactsPerTurn > 0 {
		return storedExtractionYAML.MaxFactsPerTurn
	}
	return 5
}

func formatExtractDrops(drops map[string]int) string {
	if len(drops) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(drops))
	for k, v := range drops {
		if v <= 0 {
			continue
		}
		parts = append(parts, k+":"+strconv.Itoa(v))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

// NotifyMemoryExtractFromTurn runs AddFromTurn asynchronously (fail-open).
func NotifyMemoryExtractFromTurn(
	ctx context.Context,
	store memory.MemoryStore,
	session *biz.ChatSession,
	userMessage, assistantMessage string,
	agentMeta *biz.AgentMeta,
) {
	if !memoryExtractionEnabled() || store == nil || session == nil {
		return
	}
	userMessage = strings.TrimSpace(userMessage)
	assistantMessage = strings.TrimSpace(assistantMessage)
	if userMessage == "" && assistantMessage == "" {
		return
	}
	// Copy values for the goroutine; do not rely on request ctx cancellation.
	bg := context.Background()
	userID := ResolveMemoryUserID(ctx, session)
	sessionID := session.ID
	agentID := session.AgentID
	var metaCopy *biz.AgentMeta
	if agentMeta != nil {
		cp := *agentMeta
		metaCopy = &cp
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("memory extract panic: session_id=%s recover=%v", sessionID, r)
				obs.ObserveMemoryExtract("panic", 0, 0, nil, 0)
			}
		}()
		start := time.Now()
		m, err := buildExtractionModel(metaCopy)
		if err != nil || m == nil {
			log.Printf("memory extract skip model: session_id=%s agent_id=%s err=%v", sessionID, agentID, err)
			obs.ObserveMemoryExtract("skip_model", 0, 0, nil, time.Since(start))
			return
		}
		pipe := &memory.Pipeline{
			Store:     store,
			Enabled:   true,
			MaxFacts:  memoryExtractionMaxFacts(),
			Extractor: &memory.LLMExtractor{Model: m, MaxFacts: memoryExtractionMaxFacts()},
		}
		st, err := pipe.AddFromTurnWithStats(bg, memory.TurnInput{
			UserID:           userID,
			SessionID:        sessionID,
			AgentID:          agentID,
			UserMessage:      userMessage,
			AssistantMessage: assistantMessage,
		})
		obs.ObserveMemoryExtract(st.Result, st.Candidates, st.Written, st.Drops, st.Duration)
		errStr := ""
		if err != nil {
			errStr = err.Error()
			if len(errStr) > 200 {
				errStr = errStr[:200] + "…"
			}
		}
		log.Printf(
			"memory extract done session_id=%s agent_id=%s result=%s candidates=%d written=%d drops=%s parse_fail=%v dur_ms=%d err=%q",
			sessionID, agentID, st.Result, st.Candidates, st.Written, formatExtractDrops(st.Drops), st.ParseFail, st.Duration.Milliseconds(), errStr,
		)
	}()
}

func buildExtractionModel(agentMeta *biz.AgentMeta) (model.Model, error) {
	return resolveMemoryAuxModel(agentMeta)
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
