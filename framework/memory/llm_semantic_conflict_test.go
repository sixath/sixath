package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/model"
)

type errChatModel struct{ err error }

func (e errChatModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return nil, e.err
}

func (e errChatModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return nil, e.err
}

func (e errChatModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, e.err
}

func TestLLMSemanticConflict_KeepBoth(t *testing.T) {
	r := &LLMSemanticConflictResolver{
		Model: stubChatModel{text: `{"decision":"keep_both","target_unit_id":""}`},
	}
	v, err := r.ResolveAdd(context.Background(), RememberInput{Content: "likes coffee"}, []MemoryHit{
		{ID: "u1", Content: "likes tea"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != ConflictKeepBoth || v.TargetUnitID != "" {
		t.Fatalf("got %+v", v)
	}
}

func TestLLMSemanticConflict_Ignore(t *testing.T) {
	r := &LLMSemanticConflictResolver{
		Model: stubChatModel{text: `{"decision":"ignore","target_unit_id":""}`},
	}
	v, err := r.ResolveAdd(context.Background(), RememberInput{Content: "likes tea"}, []MemoryHit{
		{ID: "u1", Content: "likes tea"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != ConflictIgnore {
		t.Fatalf("got %+v", v)
	}
}

func TestLLMSemanticConflict_SupersedeWithTarget(t *testing.T) {
	r := &LLMSemanticConflictResolver{
		Model: stubChatModel{text: `{"decision":"supersede","target_unit_id":"u1"}`},
	}
	v, err := r.ResolveAdd(context.Background(), RememberInput{Content: "timezone is PST"}, []MemoryHit{
		{ID: "u1", Content: "timezone is UTC"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != ConflictSupersede || v.TargetUnitID != "u1" {
		t.Fatalf("got %+v", v)
	}
}

func TestLLMSemanticConflict_IllegalJSON(t *testing.T) {
	r := &LLMSemanticConflictResolver{Model: stubChatModel{text: "not-json"}}
	_, err := r.ResolveAdd(context.Background(), RememberInput{Content: "a"}, []MemoryHit{{ID: "u1", Content: "b"}})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLLMSemanticConflict_SupersedeMissingTarget(t *testing.T) {
	r := &LLMSemanticConflictResolver{
		Model: stubChatModel{text: `{"decision":"supersede","target_unit_id":""}`},
	}
	_, err := r.ResolveAdd(context.Background(), RememberInput{Content: "a"}, []MemoryHit{{ID: "u1", Content: "b"}})
	if err == nil {
		t.Fatal("expected error for supersede without target")
	}
}

func TestLLMSemanticConflict_ChatError(t *testing.T) {
	r := &LLMSemanticConflictResolver{Model: errChatModel{err: errors.New("boom")}}
	_, err := r.ResolveAdd(context.Background(), RememberInput{Content: "a"}, []MemoryHit{{ID: "u1", Content: "b"}})
	if err == nil {
		t.Fatal("expected chat error")
	}
}

func TestLLMSemanticConflict_NilModel(t *testing.T) {
	r := &LLMSemanticConflictResolver{}
	_, err := r.ResolveAdd(context.Background(), RememberInput{Content: "a"}, nil)
	if err == nil {
		t.Fatal("expected nil model error")
	}
}
