package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/sixath/framework/investigate/cases"
)

// agentCaseStore 解析 agent 工作区下的案例库（需要该 agent 的编辑权限）。
func agentCaseStore(c context.Context, agentUC *biz.AgentUsecase, agentID string) (*cases.FileStore, error) {
	agent, err := agentUC.GetForEdit(c, agentID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(agent.Workspace) == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent has no workspace")
	}
	return cases.ForWorkspace(agent.Workspace), nil
}

func caseError(err error) error {
	if errors.Is(err, cases.ErrNotFound) {
		return kratosErrors.NotFound("NOT_FOUND", "case not found")
	}
	return err
}

// AgentCasesListHandler serves GET /api/v1/agents/{agent_id}/cases?status=draft|confirmed.
func AgentCasesListHandler(agentUC *biz.AgentUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		status := strings.TrimSpace(ctx.Request().URL.Query().Get("status"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			s, err := agentCaseStore(c, agentUC, agentID)
			if err != nil {
				return nil, err
			}
			items, err := s.List(status)
			if err != nil {
				return nil, err
			}
			if items == nil {
				items = []cases.Case{}
			}
			return map[string]any{"items": items}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentCaseGetHandler serves GET /api/v1/agents/{agent_id}/cases/{case_id}.
func AgentCaseGetHandler(agentUC *biz.AgentUsecase) func(kratoshttp.Context) error {
	return agentCaseHandler(agentUC, func(s *cases.FileStore, id string, _ []byte) (any, error) {
		c, err := s.Get(id)
		return c, caseError(err)
	})
}

// AgentCasePatchHandler serves PATCH /api/v1/agents/{agent_id}/cases/{case_id}.
// Body: cases.Patch 字段，另可带 {"confirm": true} 在修正后直接确认。
func AgentCasePatchHandler(agentUC *biz.AgentUsecase) func(kratoshttp.Context) error {
	return agentCaseHandler(agentUC, func(s *cases.FileStore, id string, body []byte) (any, error) {
		var req struct {
			cases.Patch
			Confirm bool `json:"confirm"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
			}
		}
		c, err := s.Update(id, req.Patch)
		if err != nil {
			return nil, caseError(err)
		}
		if req.Confirm {
			c, err = s.Confirm(id, "")
		}
		return c, caseError(err)
	})
}

// AgentCaseDeleteHandler serves DELETE /api/v1/agents/{agent_id}/cases/{case_id}.
func AgentCaseDeleteHandler(agentUC *biz.AgentUsecase) func(kratoshttp.Context) error {
	return agentCaseHandler(agentUC, func(s *cases.FileStore, id string, _ []byte) (any, error) {
		if err := s.Discard(id); err != nil {
			return nil, caseError(err)
		}
		return map[string]any{"deleted": id}, nil
	})
}

func agentCaseHandler(agentUC *biz.AgentUsecase, fn func(s *cases.FileStore, id string, body []byte) (any, error)) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		caseID := strings.TrimSpace(ctx.Vars().Get("case_id"))
		if agentID == "" || caseID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id and case_id required")
		}
		body, _ := io.ReadAll(io.LimitReader(ctx.Request().Body, 1<<20))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			s, err := agentCaseStore(c, agentUC, agentID)
			if err != nil {
				return nil, err
			}
			return fn(s, caseID, body)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}
