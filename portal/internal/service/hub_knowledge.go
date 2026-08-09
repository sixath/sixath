package service

import (
	"context"

	"backend/internal/chat"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// ListAgentKnowledgeDrafts lists pending wiki/units drafts for the agent.
func (s *ChatService) ListAgentKnowledgeDrafts(ctx context.Context, agentID, source string, limit int) ([]chat.KnowledgeDraftItem, error) {
	if agentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	agentMeta, err := s.agentUC.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	drafts, err := chat.ListKnowledgeDrafts(ctx, agentMeta.RuntimeTools, agentID, source, limit)
	if err != nil {
		return nil, kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return drafts, nil
}

// ApproveAgentKnowledgeDraft promotes a knowledge draft (wiki or units).
func (s *ChatService) ApproveAgentKnowledgeDraft(ctx context.Context, agentID, source, id string, overwrite bool) error {
	if agentID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	agentMeta, err := s.agentUC.GetForEdit(ctx, agentID)
	if err != nil {
		return err
	}
	if err := chat.ApproveKnowledgeDraft(ctx, agentMeta.RuntimeTools, agentID, source, id, overwrite); err != nil {
		return kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return nil
}
