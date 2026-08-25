package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"backend/internal/chat"
	"backend/internal/data"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

type dataCodeModelStore struct {
	d *data.Data
}

func asCodeModelStore(pinger DBPinger) CodeModelSettingsStore {
	d, ok := pinger.(*data.Data)
	if !ok || d == nil {
		return nil
	}
	return dataCodeModelStore{d: d}
}

func (s dataCodeModelStore) GetCodeModelSetting(ctx context.Context) (chat.CodeModelSpec, error) {
	row, err := s.d.GetCodeModelSetting(ctx)
	if err != nil {
		return chat.CodeModelSpec{}, err
	}
	return chat.CodeModelSpec{
		Provider: row.Provider,
		Model:    row.Model,
		APIKey:   row.APIKey,
		BaseURL:  row.BaseURL,
	}, nil
}

func (s dataCodeModelStore) PutCodeModelSetting(ctx context.Context, spec chat.CodeModelSpec) error {
	return s.d.PutCodeModelSetting(ctx, data.StoredCodeModel{
		Provider: spec.Provider,
		Model:    spec.Model,
		APIKey:   spec.APIKey,
		BaseURL:  spec.BaseURL,
	})
}

// CodeModelSettingsStore persists the global code-family model (UI 设置).
type CodeModelSettingsStore interface {
	GetCodeModelSetting(ctx context.Context) (chat.CodeModelSpec, error)
	PutCodeModelSetting(ctx context.Context, spec chat.CodeModelSpec) error
}

func loadGlobalCodeModel(store CodeModelSettingsStore) {
	if store == nil {
		return
	}
	spec, err := store.GetCodeModelSetting(context.Background())
	if err != nil {
		spec = chat.CodeModelSpecFromEnv()
		chat.SetGlobalCodeModel(spec)
		return
	}
	if !spec.Usable() {
		spec = overlayIfEmpty(spec, chat.CodeModelSpecFromEnv())
	}
	chat.SetGlobalCodeModel(spec)
}

func overlayIfEmpty(base, env chat.CodeModelSpec) chat.CodeModelSpec {
	if strings.TrimSpace(base.Provider) == "" {
		base.Provider = env.Provider
	}
	if strings.TrimSpace(base.Model) == "" {
		base.Model = env.Model
	}
	if strings.TrimSpace(base.APIKey) == "" {
		base.APIKey = env.APIKey
	}
	if strings.TrimSpace(base.BaseURL) == "" {
		base.BaseURL = env.BaseURL
	}
	return base
}

// GetCodeModelSettingsHandler serves GET /api/v1/settings/code-model.
func GetCodeModelSettingsHandler(store CodeModelSettingsStore) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if store == nil {
				return nil, kratosErrors.InternalServer("SETTINGS", "store not ready")
			}
			spec, err := store.GetCodeModelSetting(c)
			if err != nil {
				return nil, kratosErrors.InternalServer("SETTINGS", err.Error())
			}
			if !spec.Usable() {
				spec = overlayIfEmpty(spec, chat.CodeModelSpecFromEnv())
			}
			return spec, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// PutCodeModelSettingsHandler serves PUT /api/v1/settings/code-model.
func PutCodeModelSettingsHandler(store CodeModelSettingsStore) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if store == nil {
				return nil, kratosErrors.InternalServer("SETTINGS", "store not ready")
			}
			body, err := io.ReadAll(ctx.Request().Body)
			if err != nil {
				return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "read body")
			}
			var spec chat.CodeModelSpec
			if len(strings.TrimSpace(string(body))) > 0 {
				if err := json.Unmarshal(body, &spec); err != nil {
					return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json")
				}
			}
			spec.Provider = strings.TrimSpace(spec.Provider)
			spec.Model = strings.TrimSpace(spec.Model)
			spec.APIKey = strings.TrimSpace(spec.APIKey)
			spec.BaseURL = strings.TrimSpace(spec.BaseURL)
			if err := store.PutCodeModelSetting(c, spec); err != nil {
				return nil, kratosErrors.InternalServer("SETTINGS", err.Error())
			}
			chat.SetGlobalCodeModel(spec)
			return spec, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}
