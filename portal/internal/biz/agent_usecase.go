package biz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
)

var (
	ErrAgentNotFound             = kratosErrors.NotFound("AGENT_NOT_FOUND", "agent not found")
	ErrAgentDuplicateName        = kratosErrors.Conflict("AGENT_DUPLICATE_NAME", "agent name already exists")
	ErrWorkspaceRequired         = kratosErrors.BadRequest("WORKSPACE_REQUIRED", "workspace root is required")
	ErrWorkspaceWholeRepoRetired = kratosErrors.BadRequest("WORKSPACE_WHOLE_REPO_RETIRED", "whole-repo workspace is retired; leave workspace empty for the default writable root and mount code via workspace/code")
)

// RequireWorkspaceRoot rejects an empty workspace string before a Run.
func RequireWorkspaceRoot(workspace string) error {
	if strings.TrimSpace(workspace) == "" {
		return ErrWorkspaceRequired
	}
	return nil
}

// AgentUsecase is the agent use case
type AgentUsecase struct {
	repo      AgentRepo
	resources ResourceRepo
	access    *AccessChecker
	dataRoot  string
	log       *log.Helper
}

// NewAgentUsecase creates an AgentUsecase
func NewAgentUsecase(repo AgentRepo, resources ResourceRepo, access *AccessChecker, dataRoot string, logger log.Logger) *AgentUsecase {
	return &AgentUsecase{repo: repo, resources: resources, access: access, dataRoot: dataRoot, log: log.NewHelper(logger)}
}

// Create creates an agent
func (uc *AgentUsecase) Create(ctx context.Context, name, description, systemPrompt, workspace string, modelConfig ModelConfig, debugRun bool, wecomChannelID string, runtimeTools RuntimeToolsConfig, toolIDs []string) (*AgentMeta, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := uc.requireToolsUse(ctx, caller, toolIDs); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	if strings.TrimSpace(workspace) == "" {
		workspace = filepath.Join(uc.dataRoot, "agents", id)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, err
	}
	agent, err := uc.repo.Create(ctx, id, name, description, systemPrompt, workspace, modelConfig, debugRun, wecomChannelID, runtimeTools, toolIDs)
	if err != nil && errors.Is(err, pkgErrors.ErrDuplicateName) {
		return nil, ErrAgentDuplicateName
	}
	if err != nil {
		return nil, err
	}

	visibility := VisibilityPrivate
	if visibility == VisibilityPublic {
		return nil, ErrPublicNotEnabled
	}
	if _, err := uc.resources.CreateResource(ctx, &Resource{
		Type:        ResourceTypeAgent,
		Name:        agent.Name,
		OwnerUserID: caller,
		Visibility:  visibility,
		PayloadRef:  agent.ID,
	}); err != nil {
		_ = uc.repo.Delete(ctx, agent.ID)
		return nil, err
	}
	return agent, nil
}

// Get gets an agent by ID
func (uc *AgentUsecase) Get(ctx context.Context, id string) (*AgentMeta, error) {
	if _, err := uc.requireAgentPerm(ctx, id, PermView); err != nil {
		return nil, err
	}
	return uc.get(ctx, id)
}

// GetForUse gets an agent after verifying the caller may run it.
func (uc *AgentUsecase) GetForUse(ctx context.Context, id string) (*AgentMeta, error) {
	if _, err := uc.requireAgentPerm(ctx, id, PermUse); err != nil {
		return nil, err
	}
	return uc.get(ctx, id)
}

// GetForSession loads an agent already bound to a session the caller owns.
// Session ownership is the ACL gate; peer / channel callers typically lack PermUse on the agent resource.
func (uc *AgentUsecase) GetForSession(ctx context.Context, id string) (*AgentMeta, error) {
	return uc.get(ctx, id)
}

// GetForEdit gets an agent after verifying the caller may modify its workspace.
func (uc *AgentUsecase) GetForEdit(ctx context.Context, id string) (*AgentMeta, error) {
	if _, err := uc.requireAgentPerm(ctx, id, PermEdit); err != nil {
		return nil, err
	}
	return uc.get(ctx, id)
}

func (uc *AgentUsecase) get(ctx context.Context, id string) (*AgentMeta, error) {
	agent, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// List lists agents with pagination
func (uc *AgentUsecase) List(ctx context.Context, page, pageSize int32) ([]*AgentMeta, int, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	// ACL filtering has to precede pagination; otherwise total and page contents
	// reveal the unfiltered result set. Batch ACL + ListByIDs avoids N+1 RTT to remote MySQL.
	allowed, err := VisiblePayloadRefs(ctx, uc.resources, caller, ResourceTypeAgent, PermView)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	return uc.repo.ListByIDs(ctx, ids, page, pageSize)
}

// Update updates an agent
func (uc *AgentUsecase) Update(ctx context.Context, id string, updates map[string]any) (*AgentMeta, error) {
	resource, err := uc.requireAgentPerm(ctx, id, PermEdit)
	if err != nil {
		return nil, err
	}
	agent, err := uc.repo.Update(ctx, id, updates)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	if resource.Name != agent.Name {
		resource.Name = agent.Name
		if err := uc.resources.UpdateResource(ctx, resource); err != nil {
			return nil, err
		}
	}
	return agent, nil
}

// Delete deletes an agent
func (uc *AgentUsecase) Delete(ctx context.Context, id string) error {
	resource, err := uc.requireAgentPerm(ctx, id, PermAdmin)
	if err != nil {
		return err
	}
	err = uc.repo.Delete(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrAgentNotFound
	}
	if err != nil {
		return err
	}
	if err := uc.resources.DeleteResource(ctx, resource.ID); err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return err
	}
	return nil
}

// BindTools binds tools to an agent
func (uc *AgentUsecase) BindTools(ctx context.Context, agentID string, toolIDs []string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if _, err := uc.requireAgentPerm(ctx, agentID, PermEdit); err != nil {
		return err
	}
	if err := uc.requireToolsUse(ctx, caller, toolIDs); err != nil {
		return err
	}
	err = uc.repo.BindTools(ctx, agentID, toolIDs)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrAgentNotFound
	}
	return err
}

// UnbindTools unbinds tools from an agent
func (uc *AgentUsecase) UnbindTools(ctx context.Context, agentID string, toolIDs []string) error {
	if _, err := uc.requireAgentPerm(ctx, agentID, PermEdit); err != nil {
		return err
	}
	err := uc.repo.UnbindTools(ctx, agentID, toolIDs)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrAgentNotFound
	}
	return err
}

func (uc *AgentUsecase) requireAgentPerm(ctx context.Context, agentID string, need Perm) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeAgent, agentID)
	if err != nil {
		return nil, ErrAgentNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return nil, err
	}
	if !canView {
		return nil, ErrAgentNotFound
	}
	can, err := uc.access.Can(ctx, caller, resource.ID, need, "")
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, ErrForbiddenPerm
	}
	return resource, nil
}

func (uc *AgentUsecase) requireToolsUse(ctx context.Context, caller string, toolIDs []string) error {
	for _, toolID := range toolIDs {
		resource, err := uc.resources.GetByPayload(ctx, ResourceTypeTool, toolID)
		if err != nil {
			return ErrToolNotFound
		}
		canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
		if err != nil {
			return err
		}
		if !canView {
			return ErrToolNotFound
		}
		canUse, err := uc.access.Can(ctx, caller, resource.ID, PermUse, "")
		if err != nil {
			return err
		}
		if !canUse {
			return ErrForbiddenPerm
		}
	}
	return nil
}

func requireCaller(ctx context.Context) (string, error) {
	caller, ok := CallerUserID(ctx)
	if !ok {
		return "", kratosErrors.Unauthorized("UNAUTHORIZED", "caller identity is required")
	}
	return caller, nil
}
