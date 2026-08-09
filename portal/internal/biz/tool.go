package biz

import (
	"context"
	"errors"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/structpb"
)

// ToolType is builtin, mcp or datasource
type ToolType string

const (
	ToolTypeBuiltin    ToolType = "builtin"
	ToolTypeMCP        ToolType = "mcp"
	ToolTypeDatasource ToolType = "datasource"
	ToolTypeRCA        ToolType = "rca"
)

// IsValidToolType 返回 t 是否为已知工具类型。
func IsValidToolType(t string) bool {
	switch ToolType(t) {
	case ToolTypeBuiltin, ToolTypeMCP, ToolTypeDatasource, ToolTypeRCA:
		return true
	default:
		return false
	}
}

// ValidRCAFuncPath 返回 fp 是否为受支持的 RCA 子工具。
func ValidRCAFuncPath(fp string) bool {
	switch fp {
	case "rca_code", "rca_symbol", "jaeger_trace", "es_log_query":
		return true
	default:
		return false
	}
}

// ToolMeta represents a tool entity
type ToolMeta struct {
	ID          string
	Name        string
	Description string
	Type        ToolType
	Config      *structpb.Struct
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListOptions for pagination and filtering.
// Name 为模糊匹配（匹配工具名或描述）；空则不过滤名称。
// IDs 非空时限制在给定 id 集合内（ACL 可见集）。
type ListOptions struct {
	Page     int32
	PageSize int32
	Name     string
	Type     string
	IDs      []string
}

// ToolRepo interface for tool storage
type ToolRepo interface {
	Create(ctx context.Context, name, description string, toolType ToolType, config *structpb.Struct) (*ToolMeta, error)
	GetByID(ctx context.Context, id string) (*ToolMeta, error)
	GetByName(ctx context.Context, name string) (*ToolMeta, error)
	List(ctx context.Context, opts ListOptions) ([]*ToolMeta, int, error)
	Update(ctx context.Context, id string, updates map[string]any) (*ToolMeta, error)
	Delete(ctx context.Context, id string) error
	ListByAgent(ctx context.Context, agentID string) ([]*ToolMeta, error)
	IsBoundToAgent(ctx context.Context, toolID string) (bool, error)
}

var (
	ErrToolNotFound      = kratosErrors.NotFound("TOOL_NOT_FOUND", "tool not found")
	ErrToolDuplicateName = kratosErrors.Conflict("TOOL_DUPLICATE_NAME", "tool name already exists")
	ErrToolInUse         = kratosErrors.Conflict("TOOL_IN_USE", "tool is bound to agent(s), unbind first")
)

// ToolUsecase is the tool use case
type ToolUsecase struct {
	repo      ToolRepo
	resources ResourceRepo
	access    *AccessChecker
	log       *log.Helper
}

// NewToolUsecase creates a ToolUsecase
func NewToolUsecase(repo ToolRepo, resources ResourceRepo, access *AccessChecker, logger log.Logger) *ToolUsecase {
	return &ToolUsecase{repo: repo, resources: resources, access: access, log: log.NewHelper(logger)}
}

// Create creates a tool
func (uc *ToolUsecase) Create(ctx context.Context, name, description, toolType string, config *structpb.Struct) (*ToolMeta, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	tt := ToolType(toolType)
	if !IsValidToolType(toolType) {
		tt = ToolTypeBuiltin
	}
	tool, err := uc.repo.Create(ctx, name, description, tt, config)
	if err != nil && errors.Is(err, pkgErrors.ErrDuplicateName) {
		return nil, ErrToolDuplicateName
	}
	if err != nil {
		return nil, err
	}
	if _, err := uc.resources.CreateResource(ctx, &Resource{
		Type:        ResourceTypeTool,
		Name:        tool.Name,
		OwnerUserID: caller,
		Visibility:  VisibilityPrivate,
		PayloadRef:  tool.ID,
	}); err != nil {
		_ = uc.repo.Delete(ctx, tool.ID)
		return nil, err
	}
	return tool, nil
}

// Get gets a tool by ID
func (uc *ToolUsecase) Get(ctx context.Context, id string) (*ToolMeta, error) {
	if _, err := uc.requireToolPerm(ctx, id, PermView); err != nil {
		return nil, err
	}
	tool, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return tool, nil
}

// ListByAgent lists tools bound to an agent
func (uc *ToolUsecase) ListByAgent(ctx context.Context, agentID string) ([]*ToolMeta, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	tools, err := uc.repo.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	usable := make([]*ToolMeta, 0, len(tools))
	for _, tool := range tools {
		resource, err := uc.resources.GetByPayload(ctx, ResourceTypeTool, tool.ID)
		if err != nil {
			continue
		}
		canUse, err := uc.access.Can(ctx, caller, resource.ID, PermUse, "")
		if err != nil {
			return nil, err
		}
		if canUse {
			usable = append(usable, tool)
		}
	}
	return usable, nil
}

// ListByAgentForSession returns all tools bound to an agent for a turn on an owned session.
// Caller tool ACL is skipped; session ownership is the gate (channel peers lack PermUse).
func (uc *ToolUsecase) ListByAgentForSession(ctx context.Context, agentID string) ([]*ToolMeta, error) {
	return uc.repo.ListByAgent(ctx, agentID)
}

// List lists tools with pagination and filters
func (uc *ToolUsecase) List(ctx context.Context, page, pageSize int32, name, toolType string) ([]*ToolMeta, int, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	allowed, err := VisiblePayloadRefs(ctx, uc.resources, caller, ResourceTypeTool, PermView)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []*ToolMeta{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	return uc.repo.List(ctx, ListOptions{Page: page, PageSize: pageSize, Name: name, Type: toolType, IDs: ids})
}

// Update updates a tool
func (uc *ToolUsecase) Update(ctx context.Context, id string, toolType, name, description *string, config *structpb.Struct) (*ToolMeta, error) {
	resource, err := uc.requireToolPerm(ctx, id, PermEdit)
	if err != nil {
		return nil, err
	}
	updates := make(map[string]any)
	if name != nil {
		updates["name"] = *name
	}
	if description != nil {
		updates["description"] = *description
	}
	if config != nil {
		updates["config"] = config
	}
	if toolType != nil {
		updates["type"] = *toolType
	}
	tool, err := uc.repo.Update(ctx, id, updates)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrToolNotFound
	}
	if err != nil {
		return nil, err
	}
	if resource.Name != tool.Name {
		resource.Name = tool.Name
		if err := uc.resources.UpdateResource(ctx, resource); err != nil {
			return nil, err
		}
	}
	return tool, nil
}

// Delete deletes a tool (returns conflict if bound to agent)
func (uc *ToolUsecase) Delete(ctx context.Context, id string) error {
	resource, err := uc.requireToolPerm(ctx, id, PermAdmin)
	if err != nil {
		return err
	}
	bound, err := uc.repo.IsBoundToAgent(ctx, id)
	if err != nil {
		return err
	}
	if bound {
		return ErrToolInUse
	}
	err = uc.repo.Delete(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrToolNotFound
	}
	if err != nil {
		return err
	}
	if err := uc.resources.DeleteResource(ctx, resource.ID); err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return err
	}
	return nil
}

func (uc *ToolUsecase) requireToolPerm(ctx context.Context, toolID string, need Perm) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeTool, toolID)
	if err != nil {
		return nil, ErrToolNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return nil, err
	}
	if !canView {
		return nil, ErrToolNotFound
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
