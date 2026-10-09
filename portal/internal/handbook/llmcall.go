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
	if g != nil && g.TokenUsage != nil {
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

// extractJSONObject returns the text from the first '{' to the last '}'.
func extractJSONObject(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return ""
	}
	return s[i : j+1]
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
