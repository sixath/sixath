package service

import (
	"context"
	"strings"
	"testing"

	"backend/internal/biz"
	"backend/internal/conf"
	"backend/internal/data"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/model"
)

type fakeHandbookCatalog struct {
	usable   []data.UsableModel
	disabled map[string]bool
}

func (f fakeHandbookCatalog) ListUsable(context.Context) ([]data.UsableModel, error) {
	return f.usable, nil
}

func (f fakeHandbookCatalog) GetProviderSecret(_ context.Context, id string) (string, string, string, bool, error) {
	return data.KindOpenAICompat, "http://llm-" + id, "key-" + id, !f.disabled[id], nil
}

func TestCatalogModelResolver(t *testing.T) {
	cat := fakeHandbookCatalog{
		usable: []data.UsableModel{
			{ProviderID: "p1", ProviderName: "qwen", Model: "qwen-max"},
			{ProviderID: "p2", ProviderName: "backup", Model: "qwen-max"},
			{ProviderID: "p3", ProviderName: "off", Model: "glm"},
		},
		disabled: map[string]bool{"p3": true},
	}
	var gotKey, gotModel string
	resolve := catalogModelResolver(cat, func(provider, m, key, base string) (model.Model, error) {
		gotModel, gotKey = m, key
		return nil, nil
	})
	ctx := context.Background()
	if _, err := resolve(ctx, "backup/qwen-max"); err != nil || gotKey != "key-p2" || gotModel != "qwen-max" {
		t.Fatalf("provider name/model: key=%s model=%s err=%v", gotKey, gotModel, err)
	}
	if _, err := resolve(ctx, "p1/qwen-max"); err != nil || gotKey != "key-p1" {
		t.Fatalf("provider id/model: key=%s err=%v", gotKey, err)
	}
	if _, err := resolve(ctx, "glm"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("disabled provider: err=%v, want unavailable", err)
	} else if strings.Contains(err.Error(), "key-p3") {
		t.Fatalf("error leaks the API key: %v", err)
	}
	if _, err := resolve(ctx, "nope/qwen-max"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown provider: err=%v, want not found", err)
	}
}

func TestConfigureHandbookLLM(t *testing.T) {
	ConfigureHandbookLLM(nil, &conf.HandbookConfig{Model: "x"}, fakeHandbookCatalog{})

	uc := biz.NewHandbookUsecase(nil, nil, t.TempDir(), log.DefaultLogger)
	ConfigureHandbookLLM(uc, nil, fakeHandbookCatalog{})
	if got := uc.LLMConfig().Model; got != "" {
		t.Fatalf("nil config: model=%q, want empty", got)
	}
	ConfigureHandbookLLM(uc, &conf.HandbookConfig{Model: " qwen/qwen-max ", Concurrency: 2, MaxRunMinutes: 99}, fakeHandbookCatalog{})
	got := uc.LLMConfig()
	if got.Model != "qwen/qwen-max" || got.Concurrency != 2 || got.MaxRunMinutes != 20 || got.MaxCardsPerRun != 600 {
		t.Fatalf("config = %+v, want trimmed model, concurrency 2, clamped run minutes and defaults", got)
	}
}
