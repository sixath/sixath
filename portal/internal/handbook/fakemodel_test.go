package handbook

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/sixath/framework/model"
)

// fakeModel answers by the first rule whose key occurs in the system or user prompt.
type fakeModel struct {
	mu        sync.Mutex
	rules     []fakeRule
	calls     []string // user prompts in call order
	maxTokens []int    // WithMaxTokens of each call
	failErr   error    // returned by every call when set
}

type fakeRule struct {
	key   string
	reply func(user string) string
}

func (m *fakeModel) on(key string, reply func(user string) string) *fakeModel {
	m.rules = append(m.rules, fakeRule{key, reply})
	return m
}

func (m *fakeModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var sys, user string
	for _, msg := range msgs {
		if msg.Role == "system" {
			sys = msg.Content
		} else {
			user = msg.Content
		}
	}
	m.mu.Lock()
	var cfg model.CallConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	m.calls = append(m.calls, user)
	m.maxTokens = append(m.maxTokens, cfg.MaxTokens)
	fail := m.failErr
	m.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	for _, r := range m.rules {
		if strings.Contains(sys, r.key) || strings.Contains(user, r.key) {
			return &model.Generation{Text: r.reply(user), TokenUsage: &model.TokenUsage{InputTokens: 10, OutputTokens: 5}, FinishReason: "stop"}, nil
		}
	}
	return nil, errors.New("fake model: no rule")
}

func (m *fakeModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return m.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}

func (m *fakeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, errors.New("fake model: no embeddings")
}

func (m *fakeModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}
