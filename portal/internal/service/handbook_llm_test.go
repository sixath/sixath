package service

import (
	"context"
	"errors"
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

// oneRepo serves a single active repository r1, with handbook model override model, whose
// handbook is not built yet.
type oneRepo struct {
	biz.RepoRegistryRepo
	model *string
}

func (r oneRepo) GetRepositoriesByIDs(context.Context, []string) (map[string]*biz.Repository, error) {
	return map[string]*biz.Repository{"r1": {ID: "r1", Status: biz.RepoStatusActive, HeadCommit: "abc", HandbookModel: *r.model}}, nil
}

func TestConfigureHandbookLLM(t *testing.T) {
	ConfigureHandbookLLM(nil, &conf.HandbookConfig{Model: "x"}, fakeHandbookCatalog{})

	ctx := context.Background()
	override := ""
	uc := biz.NewHandbookUsecase(oneRepo{model: &override}, nil, t.TempDir(), log.DefaultLogger)
	ConfigureHandbookLLM(uc, nil, fakeHandbookCatalog{})
	if err := uc.RequestEnrich(ctx, "r1", false); !errors.Is(err, biz.ErrHandbookLLMDisabled) {
		t.Fatalf("nil config: RequestEnrich err=%v, want LLM disabled", err)
	}
	if uc.LLMAvailable() {
		t.Fatal("nil config: LLMAvailable = true, want false without a resolver")
	}

	ConfigureHandbookLLM(uc, &conf.HandbookConfig{Model: " qwen/qwen-max ", Concurrency: 2, MaxRunMinutes: 99}, fakeHandbookCatalog{})
	got := uc.LLMConfig()
	if got.Model != "qwen/qwen-max" || got.Concurrency != 2 || got.MaxRunMinutes != 20 || got.MaxCardsPerRun != 600 {
		t.Fatalf("config = %+v, want trimmed model, concurrency 2, clamped run minutes and defaults", got)
	}
	// With a resolver installed the LLM layer is enabled and the next check is handbook readiness.
	if err := uc.RequestEnrich(ctx, "r1", false); !errors.Is(err, biz.ErrHandbookNotReady) {
		t.Fatalf("configured: RequestEnrich err=%v, want handbook not ready", err)
	}
	// An empty global model still installs the resolver for repo overrides.
	ConfigureHandbookLLM(uc, &conf.HandbookConfig{}, fakeHandbookCatalog{})
	if !uc.LLMAvailable() {
		t.Fatal("empty model: LLMAvailable = false, want the resolver installed")
	}
	if err := uc.RequestEnrich(ctx, "r1", false); !errors.Is(err, biz.ErrHandbookLLMDisabled) {
		t.Fatalf("empty model: RequestEnrich err=%v, want LLM disabled without a repo override", err)
	}
	override = "qwen/qwen-max"
	if err := uc.RequestEnrich(ctx, "r1", false); !errors.Is(err, biz.ErrHandbookNotReady) {
		t.Fatalf("empty model with repo override: RequestEnrich err=%v, want handbook not ready", err)
	}
}

func TestHandbookModelResolver(t *testing.T) {
	cat := fakeHandbookCatalog{usable: []data.UsableModel{{ProviderID: "p1", ProviderName: "qwen", Model: "qwen-max"}}}
	want := fakeModel{}
	resolve := handbookModelResolver(cat, func(provider, m, key, base string) (model.Model, error) {
		if provider != "openai" || m != "qwen-max" || key != "key-p1" || base != "http://llm-p1" {
			t.Fatalf("build(%s, %s, %s, %s)", provider, m, key, base)
		}
		return want, nil
	})
	got, err := resolve(context.Background(), "qwen/qwen-max")
	if err != nil || got != want {
		t.Fatalf("resolve = %v, %v; want the built model", got, err)
	}
}

type fakeModel struct{ model.Model }
