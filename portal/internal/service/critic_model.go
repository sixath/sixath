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

// criticModelResolver 按 hooks.yaml critic.model 在模型目录中查找可用模型：
// 先按模型名精确匹配，再按 "<provider 名称或 ID>/<模型名>" 匹配。
func criticModelResolver(cat criticModelCatalog, build func(provider, modelName, apiKey, baseURL string) (model.Model, error)) func(name string) (model.Model, error) {
	return func(name string) (model.Model, error) {
		ctx, cancel := context.WithTimeout(context.Background(), criticModelLookupTimeout)
		defer cancel()
		usable, err := cat.ListUsable(ctx)
		if err != nil {
			return nil, err
		}
		m, ok := pickCriticModel(usable, strings.TrimSpace(name))
		if !ok {
			return nil, fmt.Errorf("critic model %q not found among usable catalog models", name)
		}
		kind, baseURL, apiKey, enabled, err := cat.GetProviderSecret(ctx, m.ProviderID)
		if err != nil {
			return nil, err
		}
		provider, ok := chat.ProviderForKind(kind)
		if !enabled || apiKey == "" || !ok {
			return nil, fmt.Errorf("critic model %q: provider %s unavailable", name, m.ProviderName)
		}
		return build(provider, m.Model, apiKey, baseURL)
	}
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
