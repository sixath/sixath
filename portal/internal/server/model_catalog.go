package server

import (
	"context"
	"strings"

	"backend/internal/data"
	"backend/internal/service"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

func catalogJSON(extra map[string]any) map[string]any {
	out := map[string]any{"ret": map[string]any{"code": 0, "message": "ok"}}
	if extra == nil {
		return out
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

type createProviderBody struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Enabled *bool  `json:"enabled"`
}

type patchProviderBody struct {
	Name    *string `json:"name"`
	Kind    *string `json:"kind"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

type createCatalogBody struct {
	ProviderID  string `json:"provider_id"`
	Model       string `json:"model"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"`
}

type patchCatalogBody struct {
	DisplayName *string `json:"display_name"`
	Hidden      *bool   `json:"hidden"`
}

type setSessionModelBody struct {
	Choice          string `json:"choice"`
	ModelProviderID string `json:"model_provider_id"`
	Model           string `json:"model"`
}

func ListModelProvidersHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.ListProviders(c)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"providers": out}))
	}
}

func CreateModelProviderHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createProviderBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.CreateProvider(c, data.ProviderInput{
				Name: body.Name, Kind: body.Kind, BaseURL: body.BaseURL, APIKey: body.APIKey, Enabled: enabled,
			})
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"provider": out}))
	}
}

func GetModelProviderHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.GetProvider(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"provider": out}))
	}
}

func PatchModelProviderHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var body patchProviderBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.PatchProvider(c, id, data.ProviderPatch{
				Name: body.Name, Kind: body.Kind, BaseURL: body.BaseURL, APIKey: body.APIKey, Enabled: body.Enabled,
			})
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"provider": out}))
	}
}

func DeleteModelProviderHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, chat.DeleteProvider(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(nil))
	}
}

func SyncModelProviderHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			n, err := chat.SyncProvider(c, id)
			if err != nil {
				return nil, err
			}
			return n, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"synced": out}))
	}
}

func ListModelCatalogHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		providerID := strings.TrimSpace(ctx.Query().Get("provider_id"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.ListCatalog(c, providerID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"items": out}))
	}
}

func CreateModelCatalogHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createCatalogBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.CreateCatalogEntry(c, data.CatalogInput{
				ProviderID: body.ProviderID, Model: body.Model, DisplayName: body.DisplayName, Source: body.Source,
			})
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{"item": out}))
	}
}

func PatchModelCatalogHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var body patchCatalogBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, chat.PatchCatalogEntry(c, data.EntryPatch{ID: id, DisplayName: body.DisplayName, Hidden: body.Hidden})
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(nil))
	}
}

func DeleteModelCatalogHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, chat.DeleteCatalogEntry(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(nil))
	}
}

func ListModelChoicesHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		sessionID := strings.TrimSpace(ctx.Query().Get("session_id"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.ListModelChoices(c, agentID, sessionID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(map[string]any{
			"items":    out.(*service.ModelChoicesReply).Items,
			"selected": out.(*service.ModelChoicesReply).Selected,
		}))
	}
}

func PatchSessionModelHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		sessionID := strings.TrimSpace(ctx.Vars().Get("session_id"))
		var body setSessionModelBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, chat.SetSessionModel(c, sessionID, service.SetSessionModelInput{
				Choice: body.Choice, ModelProviderID: body.ModelProviderID, Model: body.Model,
			})
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, catalogJSON(nil))
	}
}
