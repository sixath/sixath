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

// AgentHubKnowledgeDraftsHandler GET /api/v1/agents/{agent_id}/hub/knowledge/drafts
func AgentHubKnowledgeDraftsHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		source := strings.TrimSpace(ctx.Query().Get("source"))
		limit := parseIntQuery(ctx.Query().Get("limit"), 50)
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			drafts, err := chatSvc.ListAgentKnowledgeDrafts(c, agentID, source, limit)
			if err != nil {
				return nil, err
			}
			if drafts == nil {
				drafts = []chat.KnowledgeDraftItem{}
			}
			return map[string]any{"drafts": drafts}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentHubKnowledgeApproveHandler POST /api/v1/agents/{agent_id}/hub/knowledge/approve
func AgentHubKnowledgeApproveHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		var req chat.ApproveKnowledgeDraftRequest
		body, _ := io.ReadAll(ctx.Request().Body)
		if err := json.Unmarshal(body, &req); err != nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if err := chatSvc.ApproveAgentKnowledgeDraft(c, agentID, req.Source, req.ID, req.Overwrite); err != nil {
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
