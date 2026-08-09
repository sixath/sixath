package chat

import (
	"context"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
)

type stubExtractChatModel struct{ text string }

func (s stubExtractChatModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return s.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}

func (s stubExtractChatModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.text}, nil
}

func (s stubExtractChatModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestExtractionPipeline_WritesViaStore(t *testing.T) {
	store := BuildMemoryStore(memory.NewSessionMemory(), nil, nil, DefaultMemoryStoreOptions())
	pipe := &memory.Pipeline{
		Store:    store,
		Enabled:  true,
		MaxFacts: 5,
		Extractor: &memory.LLMExtractor{
			Model:    stubExtractChatModel{text: `{"facts":[{"content":"smoke prefers tea","scope":"session"}]}`},
			MaxFacts: 5,
		},
	}
	n, err := pipe.AddFromTurn(context.Background(), memory.TurnInput{
		SessionID: "sess-smoke", UserMessage: "I prefer tea", AssistantMessage: "Got it.",
	})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	hits, err := store.Recall(context.Background(), memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: "sess-smoke", Query: "tea",
	})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
}
