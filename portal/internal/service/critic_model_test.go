package service

import (
	"context"
	"testing"

	"backend/internal/data"

	"github.com/sixath/framework/model"
)

type fakeCriticCatalog struct{ usable []data.UsableModel }

func (f fakeCriticCatalog) ListUsable(context.Context) ([]data.UsableModel, error) {
	return f.usable, nil
}

func (f fakeCriticCatalog) GetProviderSecret(_ context.Context, id string) (string, string, string, bool, error) {
	return data.KindOpenAICompat, "http://llm-" + id, "key-" + id, true, nil
}

func TestCriticModelResolver(t *testing.T) {
	cat := fakeCriticCatalog{usable: []data.UsableModel{
		{ProviderID: "p1", ProviderName: "internal", Model: "glm-5.1"},
		{ProviderID: "p2", ProviderName: "backup", Model: "qwen3"},
		{ProviderID: "p3", ProviderName: "other", Model: "qwen3"},
	}}
	type built struct{ provider, model, key, base string }
	var got built
	resolve := criticModelResolver(cat, func(provider, m, key, base string) (model.Model, error) {
		got = built{provider, m, key, base}
		return nil, nil
	})
	if _, err := resolve("glm-5.1"); err != nil || got != (built{"openai", "glm-5.1", "key-p1", "http://llm-p1"}) {
		t.Fatalf("by model name: got=%+v err=%v", got, err)
	}
	if _, err := resolve("other/qwen3"); err != nil || got.key != "key-p3" {
		t.Fatalf("by provider/model: got=%+v err=%v", got, err)
	}
	if _, err := resolve("gpt-x"); err == nil {
		t.Fatal("unknown model must fail so the critic falls back to the agent model")
	}
}
