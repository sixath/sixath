package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"backend/internal/biz"
	"backend/internal/service"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

type mcpServerBody struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	Endpoint    string            `json:"endpoint"`
	Backend     string            `json:"backend"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	TimeoutSec  int               `json:"timeout_sec"`
}

type bindMcpServersBody struct {
	ServerIDs []string `json:"server_ids"`
}

func bodyToMcpServerMeta(b mcpServerBody, idFromPath string) *biz.McpServerMeta {
	id := strings.TrimSpace(b.ID)
	if id == "" {
		id = strings.TrimSpace(idFromPath)
	}
	return &biz.McpServerMeta{
		ID:          id,
		Name:        b.Name,
		Description: b.Description,
		Transport:   b.Transport,
		Endpoint:    b.Endpoint,
		Backend:     b.Backend,
		Command:     b.Command,
		Args:        b.Args,
		Env:         b.Env,
		TimeoutSec:  b.TimeoutSec,
	}
}

func readJSONBody(ctx kratoshttp.Context, dst any) error {
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "read body failed")
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
	}
	return nil
}

// CreateMcpServerHandler POST /api/v1/mcp-servers
func CreateMcpServerHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body mcpServerBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return svc.Create(c, bodyToMcpServerMeta(body, ""))
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{
			"ret":    map[string]any{"code": 0, "message": "ok"},
			"server": service.McpServerDTOFromMeta(out.(*biz.McpServerMeta)),
		})
	}
}

// ListMcpServersHandler GET /api/v1/mcp-servers
func ListMcpServersHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		page := int32(parseIntQuery(ctx.Query().Get("page"), 1))
		pageSize := int32(parseIntQuery(ctx.Query().Get("page_size"), 10))
		name := strings.TrimSpace(ctx.Query().Get("name"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			items, total, err := svc.List(c, page, pageSize, name)
			if err != nil {
				return nil, err
			}
			dtos := make([]service.McpServerDTO, len(items))
			for i, m := range items {
				dtos[i] = service.McpServerDTOFromMeta(m)
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

// GetMcpServerHandler GET /api/v1/mcp-servers/{id}
func GetMcpServerHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
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
		return ctx.JSON(200, map[string]any{
			"ret":    map[string]any{"code": 0, "message": "ok"},
			"server": service.McpServerDTOFromMeta(out.(*biz.McpServerMeta)),
		})
	}
}

// UpdateMcpServerHandler PUT /api/v1/mcp-servers/{id}
func UpdateMcpServerHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		var body mcpServerBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return svc.Update(c, bodyToMcpServerMeta(body, id))
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{
			"ret":    map[string]any{"code": 0, "message": "ok"},
			"server": service.McpServerDTOFromMeta(out.(*biz.McpServerMeta)),
		})
	}
}

// DeleteMcpServerHandler DELETE /api/v1/mcp-servers/{id}
func DeleteMcpServerHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, svc.Delete(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ret": map[string]any{"code": 0, "message": "ok"}})
	}
}

// TestMcpServerHandler POST /api/v1/mcp-servers/{id}/test
func TestMcpServerHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		if id == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return svc.Test(c, id)
		})
		if err != nil {
			return err
		}
		names := out.([]string)
		if names == nil {
			names = []string{}
		}
		return ctx.JSON(200, map[string]any{
			"ret":        map[string]any{"code": 0, "message": "ok"},
			"tool_names": names,
		})
	}
}

// BindAgentMcpServersHandler POST /api/v1/agents/{id}/mcp-servers
func BindAgentMcpServersHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		var body bindMcpServersBody
		if err := readJSONBody(ctx, &body); err != nil {
			return err
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, svc.BindToAgent(c, agentID, body.ServerIDs)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ret": map[string]any{"code": 0, "message": "ok"}})
	}
}

// UnbindAgentMcpServersHandler DELETE /api/v1/agents/{id}/mcp-servers
func UnbindAgentMcpServersHandler(svc *service.McpServerService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "id required")
		}
		serverIDs := parseServerIDsQueryOrBody(ctx)
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, svc.UnbindFromAgent(c, agentID, serverIDs)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ret": map[string]any{"code": 0, "message": "ok"}})
	}
}

func parseServerIDsQueryOrBody(ctx kratoshttp.Context) []string {
	raw := strings.TrimSpace(ctx.Query().Get("server_ids"))
	if raw != "" {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	var body bindMcpServersBody
	_ = readJSONBody(ctx, &body)
	return body.ServerIDs
}
