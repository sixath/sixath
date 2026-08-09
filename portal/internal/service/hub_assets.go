package service

import (
	"context"

	"backend/internal/chat"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// GetAgentHubLoadout returns merged loadout for the agent.
func (s *ChatService) GetAgentHubLoadout(ctx context.Context, agentID string) (*chat.LoadoutView, error) {
	if agentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	agentMeta, err := s.agentUC.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	extra, err := s.sharedSkillDirs(ctx, agentID)
	if err != nil {
		return nil, err
	}
	skillsIdx, err := chat.BuildSkillsIndex(agentMeta.Workspace, extra)
	if err != nil {
		return nil, err
	}
	view, err := chat.ListAgentLoadout(ctx, agentMeta.RuntimeTools, agentID, skillsIdx)
	if err != nil {
		return nil, kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return &view, nil
}

// GetAgentHubBindings returns explicit hub bindings.
func (s *ChatService) GetAgentHubBindings(ctx context.Context, agentID string) (*chat.BindingsView, error) {
	if agentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	agentMeta, err := s.agentUC.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	view, err := chat.ListAgentBindings(ctx, agentMeta.RuntimeTools, agentID)
	if err != nil {
		return nil, kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return &view, nil
}

// BindAgentHubAssets binds assets onto the agent's loadout.
func (s *ChatService) BindAgentHubAssets(ctx context.Context, agentID string, assets []chat.AssetJSON) error {
	if agentID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	if len(assets) == 0 {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "assets required")
	}
	agentMeta, err := s.agentUC.GetForEdit(ctx, agentID)
	if err != nil {
		return err
	}
	if err := chat.BindAgentAssets(ctx, agentMeta.RuntimeTools, agentID, assets); err != nil {
		return kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return nil
}

// UnbindAgentHubAssets removes bindings.
func (s *ChatService) UnbindAgentHubAssets(ctx context.Context, agentID string, assets []chat.AssetJSON) error {
	if agentID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	if len(assets) == 0 {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "assets required")
	}
	agentMeta, err := s.agentUC.GetForEdit(ctx, agentID)
	if err != nil {
		return err
	}
	if err := chat.UnbindAgentAssets(ctx, agentMeta.RuntimeTools, agentID, assets); err != nil {
		return kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return nil
}

// ClearAgentHubBindings clears all explicit bindings (provider-switch helper).
func (s *ChatService) ClearAgentHubBindings(ctx context.Context, agentID string) (int, error) {
	if agentID == "" {
		return 0, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	agentMeta, err := s.agentUC.GetForEdit(ctx, agentID)
	if err != nil {
		return 0, err
	}
	n, err := chat.ClearAgentBindings(ctx, agentMeta.RuntimeTools, agentID)
	if err != nil {
		return 0, kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return n, nil
}

// SetAgentHubAssetStatus sets status on a hub asset (approve draft → active).
func (s *ChatService) SetAgentHubAssetStatus(ctx context.Context, agentID string, asset chat.AssetJSON, status string) error {
	if agentID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	agentMeta, err := s.agentUC.GetForEdit(ctx, agentID)
	if err != nil {
		return err
	}
	if err := chat.SetAgentAssetStatus(ctx, agentMeta.RuntimeTools, asset, status); err != nil {
		return kratosErrors.BadRequest("INVALID_HUB", err.Error())
	}
	return nil
}
