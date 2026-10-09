package handbook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

// errBadReply marks a model reply that could not be used; the call itself succeeded.
var errBadReply = errors.New("handbook: unusable model reply")

// llmTemperature stays above 0: the OpenAI-compatible client replaces 0 with its default.
const llmTemperature = 0.2

// usage accumulates token counts across concurrent calls.
type usage struct{ in, out atomic.Int64 }

func (u *usage) add(g *model.Generation) {
	if u != nil && g != nil && g.TokenUsage != nil {
		u.in.Add(int64(g.TokenUsage.InputTokens))
		u.out.Add(int64(g.TokenUsage.OutputTokens))
	}
}

// llmJSON sends one system + user exchange and decodes the JSON object in the reply into out.
func llmJSON(ctx context.Context, m model.Model, system, user string, maxTokens int, out any, u *usage) error {
	g, err := m.Chat(ctx, []model.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		model.WithMaxTokens(maxTokens), model.WithTemperature(llmTemperature))
	if err != nil {
		return err
	}
	if g == nil {
		return fmt.Errorf("%w: no generation", errBadReply)
	}
	u.add(g)
	if g.FinishReason == "length" {
		return fmt.Errorf("%w: reply truncated at %d tokens", errBadReply, maxTokens)
	}
	raw := extractJSONObject(g.Text)
	if raw == "" {
		return fmt.Errorf("%w: no JSON object", errBadReply)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("%w: %v", errBadReply, err)
	}
	return nil
}

// extractJSONObject returns the first JSON object in s, trying each '{' in order so prose
// braces before or after the object are skipped.
func extractJSONObject(s string) string {
	for off := 0; off < len(s); {
		i := strings.IndexByte(s[off:], '{')
		if i < 0 {
			return ""
		}
		off += i
		var raw json.RawMessage
		if err := json.NewDecoder(strings.NewReader(s[off:])).Decode(&raw); err == nil {
			return string(raw)
		}
		off++
	}
	return ""
}

// clipRunes trims s and keeps at most n runes.
func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
