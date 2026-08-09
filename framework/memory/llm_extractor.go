package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sixath/framework/model"
)

// ErrExtractParse is returned when the model output is not valid extraction JSON.
var ErrExtractParse = errors.New("memory: parse extraction JSON")

const (
	maxTurnExtractInputRunes = 4000
	llmExtractSystemPrompt   = `You extract durable user/session facts from one chat turn.
Reply with ONLY valid JSON (no markdown fences):
{"facts":[{"content":"...","scope":"user|session"}]}
Rules:
- scope "user": stable preferences/identity that persist across sessions
- scope "session": facts useful only in this conversation
- content must be short, atomic, no code dumps
- if nothing durable, return {"facts":[]}`
)

// LLMExtractor uses a chat model to propose CandidateFacts as JSON.
type LLMExtractor struct {
	Model    model.Model
	MaxFacts int
}

type llmExtractResponse struct {
	Facts []struct {
		Content string `json:"content"`
		Scope   string `json:"scope"`
	} `json:"facts"`
}

// Extract implements Extractor.
func (e *LLMExtractor) Extract(ctx context.Context, in TurnInput) ([]CandidateFact, error) {
	if e == nil || e.Model == nil {
		return nil, fmt.Errorf("memory: LLMExtractor requires a model")
	}
	maxFacts := e.MaxFacts
	if maxFacts <= 0 {
		maxFacts = 5
	}

	user := truncateRunes(strings.TrimSpace(in.UserMessage), maxTurnExtractInputRunes)
	asst := truncateRunes(strings.TrimSpace(in.AssistantMessage), maxTurnExtractInputRunes)
	prompt := fmt.Sprintf("User:\n%s\n\nAssistant:\n%s\n\nExtract facts JSON.", user, asst)

	gen, err := e.Model.Chat(ctx, []model.Message{
		{Role: "system", Content: llmExtractSystemPrompt},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, err
	}
	if gen == nil {
		return nil, fmt.Errorf("memory: empty extraction generation")
	}

	raw := strings.TrimSpace(gen.Text)
	raw = stripJSONFences(raw)
	var parsed llmExtractResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExtractParse, err)
	}

	out := make([]CandidateFact, 0, len(parsed.Facts))
	for _, f := range parsed.Facts {
		if len(out) >= maxFacts {
			break
		}
		content := strings.TrimSpace(f.Content)
		scope := Scope(strings.TrimSpace(strings.ToLower(f.Scope)))
		if content == "" {
			continue
		}
		if scope != ScopeUser && scope != ScopeSession {
			continue
		}
		out = append(out, CandidateFact{Content: content, Scope: scope})
	}
	return out, nil
}

func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "json") {
		s = strings.TrimSpace(s[4:])
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
