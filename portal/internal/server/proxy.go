package server

import (
	"context"
	"errors"
	"strings"

	"backend/internal/biz"
	"backend/internal/service"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

type proxyBody struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	Password    string   `json:"password"`
	NoProxy     []string `json:"no_proxy"`
}

func bodyToProxyMeta(b proxyBody, idFromPath string) *biz.ProxyMeta {
	id := strings.TrimSpace(b.ID)
	if id == "" {
		id = strings.TrimSpace(idFromPath)
	}
	return &biz.ProxyMeta{
		ID:          id,
		Name:        b.Name,
		Description: b.Description,
		Type:        b.Type,
		Host:        b.Host,
		Port:        b.Port,
		User:        b.User,
		Password:    b.Password,
		NoProxy:     b.NoProxy,
	}
}

func proxyJSON(ctx kratoshttp.Context, meta *biz.ProxyMeta) error {
	return ctx.JSON(200, map[string]any{
		"ret":   map[string]any{"code": 0, "message": "ok"},
		"proxy": service.ProxyDTOFromMeta(meta),
	})
}

// CreateProxyHandler POST /api/v1/proxies
func CreateProxyHandler(svc *service.ProxyService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body proxyBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return svc.Create(c, bodyToProxyMeta(body, ""))
		})
		if err != nil {
			return err
		}
		return proxyJSON(ctx, out.(*biz.ProxyMeta))
	}
}

// ListProxiesHandler GET /api/v1/proxies
func ListProxiesHandler(svc *service.ProxyService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		page := int32(parseIntQuery(ctx.Query().Get("page"), 1))
		pageSize := int32(parseIntQuery(ctx.Query().Get("page_size"), 10))
		name := strings.TrimSpace(ctx.Query().Get("name"))
		bindable := parseBoolQuery(ctx.Query().Get("bindable"), false)
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			items, total, err := svc.List(c, page, pageSize, name, bindable)
			if err != nil {
				return nil, err
			}
			dtos := make([]service.ProxyDTO, len(items))
			for i, m := range items {
				dtos[i] = service.ProxyDTOFromMeta(m)
			}
			return map[string]any{"items": dtos, "total": total}, nil
		})
		if err != nil {
			return err
		}
		m := out.(map[string]any)
		return ctx.JSON(200, map[string]any{
			"ret":   map[string]any{"code": 0, "message": "ok"},
			"items": m["items"],
			"total": m["total"],
		})
	}
}

// GetProxyHandler GET /api/v1/proxies/{id}
func GetProxyHandler(svc *service.ProxyService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return svc.Get(c, id)
		})
		if err != nil {
			return err
		}
		return proxyJSON(ctx, out.(*biz.ProxyMeta))
	}
}

// UpdateProxyHandler PUT /api/v1/proxies/{id}
func UpdateProxyHandler(svc *service.ProxyService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		var body proxyBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return svc.Update(c, bodyToProxyMeta(body, id))
		})
		if err != nil {
			return err
		}
		return proxyJSON(ctx, out.(*biz.ProxyMeta))
	}
}

// DeleteProxyHandler DELETE /api/v1/proxies/{id}
func DeleteProxyHandler(svc *service.ProxyService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, svc.Delete(c, id)
		})
		if err != nil {
			var inUse *biz.ProxyInUseError
			if errors.As(err, &inUse) {
				refs := inUse.References
				if refs == nil {
					refs = []biz.ProxyReference{}
				}
				return ctx.JSON(409, map[string]any{
					"ret": map[string]any{
						"code":    409,
						"reason":  "PROXY_IN_USE",
						"message": "proxy is referenced by agents or tools",
					},
					"references": refs,
					"truncated":  inUse.Truncated,
				})
			}
			return err
		}
		return ctx.JSON(200, map[string]any{"ret": map[string]any{"code": 0, "message": "ok"}})
	}
}

// TestProxyHandler POST /api/v1/proxies/{id}/test
func TestProxyHandler(svc *service.ProxyService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, svc.Test(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{
			"ret": map[string]any{"code": 0, "message": "ok"},
			"ok":  true,
		})
	}
}
