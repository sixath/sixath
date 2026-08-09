package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/model"
)

type stubChatModel struct{ text string }

func (s stubChatModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return s.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}
func (s stubChatModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.text}, nil
}
func (s stubChatModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestLLMExtractor_ParseFailIsErrExtractParse(t *testing.T) {
	ex := &LLMExtractor{Model: stubChatModel{text: "not-json"}, MaxFacts: 5}
	_, err := ex.Extract(context.Background(), TurnInput{UserMessage: "u", AssistantMessage: "a"})
	if !errors.Is(err, ErrExtractParse) {
		t.Fatalf("err=%v want ErrExtractParse", err)
	}
}

func TestLLMExtractor_OK(t *testing.T) {
	ex := &LLMExtractor{
		Model:    stubChatModel{text: `{"facts":[{"content":"likes tea","scope":"session"}]}`},
		MaxFacts: 5,
	}
	facts, err := ex.Extract(context.Background(), TurnInput{UserMessage: "tea", AssistantMessage: "ok"})
	if err != nil || len(facts) != 1 || facts[0].Content != "likes tea" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}
