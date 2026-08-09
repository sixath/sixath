package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"backend/internal/chat"
	"backend/internal/service"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// AgentHubLoadoutHandler GET /api/v1/agents/{agent_id}/hub/loadout
func AgentHubLoadoutHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chatSvc.GetAgentHubLoadout(c, agentID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentHubBindingsHandler GET /api/v1/agents/{agent_id}/hub/bindings
func AgentHubBindingsHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chatSvc.GetAgentHubBindings(c, agentID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentHubBindHandler POST /api/v1/agents/{agent_id}/hub/bindings
func AgentHubBindHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		var req chat.BindAssetsRequest
		body, _ := io.ReadAll(ctx.Request().Body)
		if err := json.Unmarshal(body, &req); err != nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if err := chatSvc.BindAgentHubAssets(c, agentID, req.Assets); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentHubUnbindHandler DELETE /api/v1/agents/{agent_id}/hub/bindings
func AgentHubUnbindHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		var req chat.BindAssetsRequest
		body, _ := io.ReadAll(ctx.Request().Body)
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
			}
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if err := chatSvc.UnbindAgentHubAssets(c, agentID, req.Assets); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentHubClearBindingsHandler POST /api/v1/agents/{agent_id}/hub/bindings/clear
func AgentHubClearBindingsHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			n, err := chatSvc.ClearAgentHubBindings(c, agentID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "cleared": n}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentHubSetStatusHandler POST /api/v1/agents/{agent_id}/hub/assets/status
func AgentHubSetStatusHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		var req chat.SetAssetStatusRequest
		body, _ := io.ReadAll(ctx.Request().Body)
		if err := json.Unmarshal(body, &req); err != nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if err := chatSvc.SetAgentHubAssetStatus(c, agentID, req.Asset, req.Status); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}
