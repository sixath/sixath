package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"backend/internal/chat"
	"backend/internal/data"

	"github.com/sixath/framework/model"
)

const criticModelLookupTimeout = 5 * time.Second

type criticModelCatalog interface {
	ListUsable(ctx context.Context) ([]data.UsableModel, error)
	GetProviderSecret(ctx context.Context, id string) (kind, baseURL, apiKey string, enabled bool, err error)
}

type modelBuilder func(provider, modelName, apiKey, baseURL string) (model.Model, error)

// criticModelResolver 按 hooks.yaml critic.model 在模型目录中查找可用模型：
// 先按模型名精确匹配，再按 "<provider 名称或 ID>/<模型名>" 匹配。
func criticModelResolver(cat criticModelCatalog, build modelBuilder) func(name string) (model.Model, error) {
	return func(name string) (model.Model, error) {
		ctx, cancel := context.WithTimeout(context.Background(), criticModelLookupTimeout)
		defer cancel()
		return lookupCatalogModel(ctx, cat, build, "critic model", name)
	}
}

// catalogModelResolver resolves "model" or "<provider name or ID>/<model>" among usable catalog models.
func catalogModelResolver(cat criticModelCatalog, build modelBuilder) func(ctx context.Context, name string) (model.Model, error) {
	return func(ctx context.Context, name string) (model.Model, error) {
		return lookupCatalogModel(ctx, cat, build, "model", name)
	}
}

// lookupCatalogModel builds the usable catalog model named name; label prefixes its errors.
func lookupCatalogModel(ctx context.Context, cat criticModelCatalog, build modelBuilder, label, name string) (model.Model, error) {
	usable, err := cat.ListUsable(ctx)
	if err != nil {
		return nil, err
	}
	m, ok := pickCriticModel(usable, strings.TrimSpace(name))
	if !ok {
		return nil, fmt.Errorf("%s %q not found among usable catalog models", label, name)
	}
	kind, baseURL, apiKey, enabled, err := cat.GetProviderSecret(ctx, m.ProviderID)
	if err != nil {
		return nil, err
	}
	provider, ok := chat.ProviderForKind(kind)
	if !enabled || apiKey == "" || !ok {
		return nil, fmt.Errorf("%s %q: provider %s unavailable", label, name, m.ProviderName)
	}
	return build(provider, m.Model, apiKey, baseURL)
}

func pickCriticModel(usable []data.UsableModel, name string) (data.UsableModel, bool) {
	if name == "" {
		return data.UsableModel{}, false
	}
	for _, u := range usable {
		if u.Model == name {
			return u, true
		}
	}
	if prefix, modelName, ok := strings.Cut(name, "/"); ok {
		for _, u := range usable {
			if u.Model == modelName && (strings.EqualFold(u.ProviderName, prefix) || u.ProviderID == prefix) {
				return u, true
			}
		}
	}
	return data.UsableModel{}, false
}
