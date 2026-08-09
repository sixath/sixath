package biz

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

const maskedEnvValue = "***"

var mcpServerIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,35}$`)

// McpServerMeta represents an MCP server entity.
type McpServerMeta struct {
	ID          string
	Name        string
	Description string
	Transport   string
	Endpoint    string
	Backend     string
	Command     string
	Args        []string
	Env         map[string]string
	TimeoutSec  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// McpServerRepo persists MCP servers and agent bindings.
type McpServerRepo interface {
	Create(ctx context.Context, meta *McpServerMeta) (*McpServerMeta, error)
	GetByID(ctx context.Context, id string) (*McpServerMeta, error)
	List(ctx context.Context, opts ListOptions) ([]*McpServerMeta, int, error)
	Update(ctx context.Context, meta *McpServerMeta) (*McpServerMeta, error)
	Delete(ctx context.Context, id string) error
	ListByAgent(ctx context.Context, agentID string) ([]*McpServerMeta, error)
	BindServers(ctx context.Context, agentID string, serverIDs []string) error
	UnbindServers(ctx context.Context, agentID string, serverIDs []string) error
}

var (
	ErrMcpServerNotFound     = kratosErrors.NotFound("MCP_SERVER_NOT_FOUND", "mcp server not found")
	ErrMcpServerDuplicateID  = kratosErrors.Conflict("MCP_SERVER_DUPLICATE_ID", "mcp server id already exists")
	ErrMcpServerInvalidInput = kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid mcp server input")
)

// MaskSensitiveEnv returns a copy of env with sensitive values replaced by ***.
// Keys containing TOKEN/SECRET/PASSWORD/KEY (case-insensitive) are masked.
func MaskSensitiveEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		if isSensitiveEnvKey(k) {
			out[k] = maskedEnvValue
		} else {
			out[k] = v
		}
	}
	return out
}

// MergeEnvPreservingMasked merges incoming into a new map; values equal to ***
// keep the corresponding existing value when present.
func MergeEnvPreservingMasked(existing, incoming map[string]string) map[string]string {
	if incoming == nil {
		if existing == nil {
			return nil
		}
		out := make(map[string]string, len(existing))
		for k, v := range existing {
			out[k] = v
		}
		return out
	}
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		if v == maskedEnvValue {
			if existing != nil {
				if prev, ok := existing[k]; ok {
					out[k] = prev
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

func isSensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "KEY")
}

// ValidateMcpServerID checks the user-facing slug id.
func ValidateMcpServerID(id string) error {
	if !mcpServerIDRe.MatchString(id) {
		return fmt.Errorf("mcp server id must match %s", mcpServerIDRe.String())
	}
	return nil
}

// ValidateMcpServerInput validates transport-specific fields and stdio allowlist.
func ValidateMcpServerInput(m *McpServerMeta) error {
	if m == nil {
		return errors.New("mcp server is required")
	}
	if err := ValidateMcpServerID(m.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("name is required")
	}
	transport := strings.ToLower(strings.TrimSpace(m.Transport))
	switch transport {
	case "http":
		if strings.TrimSpace(m.Endpoint) == "" {
			return errors.New("http transport requires endpoint")
		}
		backend := strings.TrimSpace(m.Backend)
		if backend != "" && backend != "metoro" && backend != "mark3labs" {
			return fmt.Errorf("http backend %q not supported", m.Backend)
		}
	case "stdio":
		if strings.TrimSpace(m.Command) == "" {
			return errors.New("stdio transport requires command")
		}
		backend := strings.TrimSpace(m.Backend)
		if backend != "" && backend != "mark3labs" {
			return fmt.Errorf("stdio backend must be empty or mark3labs, got %q", m.Backend)
		}
		if err := tool.ValidateStdioMcp(m.Command, m.Args, m.Env); err != nil {
			return err
		}
	default:
		return fmt.Errorf("transport must be http or stdio, got %q", m.Transport)
	}
	if m.TimeoutSec < 0 {
		return errors.New("timeout_sec must be >= 0")
	}
	return nil
}

func maskMcpServerMeta(m *McpServerMeta) *McpServerMeta {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Env = MaskSensitiveEnv(m.Env)
	if m.Args != nil {
		cp.Args = append([]string(nil), m.Args...)
	}
	return &cp
}

// McpServerUsecase is the MCP server use case.
type McpServerUsecase struct {
	repo      McpServerRepo
	resources ResourceRepo
	access    *AccessChecker
	log       *log.Helper
}

// NewMcpServerUsecase creates a McpServerUsecase.
func NewMcpServerUsecase(repo McpServerRepo, resources ResourceRepo, access *AccessChecker, logger log.Logger) *McpServerUsecase {
	return &McpServerUsecase{repo: repo, resources: resources, access: access, log: log.NewHelper(logger)}
}

// Create creates an MCP server and a private ACL resource for the caller.
func (uc *McpServerUsecase) Create(ctx context.Context, meta *McpServerMeta) (*McpServerMeta, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, ErrMcpServerInvalidInput
	}
	if meta.TimeoutSec == 0 {
		meta.TimeoutSec = 60
	}
	if err := ValidateMcpServerInput(meta); err != nil {
		return nil, wrapMcpServerValidateErr(err)
	}
	created, err := uc.repo.Create(ctx, meta)
	if err != nil && errors.Is(err, pkgErrors.ErrDuplicateName) {
		return nil, ErrMcpServerDuplicateID
	}
	if err != nil {
		return nil, err
	}
	if _, err := uc.resources.CreateResource(ctx, &Resource{
		Type:        ResourceTypeMcpServer,
		Name:        created.Name,
		OwnerUserID: caller,
		Visibility:  VisibilityPrivate,
		PayloadRef:  created.ID,
	}); err != nil {
		_ = uc.repo.Delete(ctx, created.ID)
		return nil, err
	}
	return maskMcpServerMeta(created), nil
}

// Get returns an MCP server by ID with sensitive env masked.
func (uc *McpServerUsecase) Get(ctx context.Context, id string) (*McpServerMeta, error) {
	if _, err := uc.requireMcpServerPerm(ctx, id, PermView); err != nil {
		return nil, err
	}
	meta, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrMcpServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return maskMcpServerMeta(meta), nil
}

// List lists MCP servers visible to the caller (env masked).
func (uc *McpServerUsecase) List(ctx context.Context, page, pageSize int32, name string) ([]*McpServerMeta, int, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	allowed, err := VisiblePayloadRefs(ctx, uc.resources, caller, ResourceTypeMcpServer, PermView)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []*McpServerMeta{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	items, total, err := uc.repo.List(ctx, ListOptions{Page: page, PageSize: pageSize, Name: name, IDs: ids})
	if err != nil {
		return nil, 0, err
	}
	out := make([]*McpServerMeta, len(items))
	for i, item := range items {
		out[i] = maskMcpServerMeta(item)
	}
	return out, total, nil
}

// Update updates an MCP server; masked env values (***) preserve existing secrets.
func (uc *McpServerUsecase) Update(ctx context.Context, meta *McpServerMeta) (*McpServerMeta, error) {
	if meta == nil {
		return nil, ErrMcpServerInvalidInput
	}
	resource, err := uc.requireMcpServerPerm(ctx, meta.ID, PermEdit)
	if err != nil {
		return nil, err
	}
	existing, err := uc.repo.GetByID(ctx, meta.ID)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrMcpServerNotFound
	}
	if err != nil {
		return nil, err
	}
	merged := *meta
	merged.Env = MergeEnvPreservingMasked(existing.Env, meta.Env)
	if merged.TimeoutSec == 0 {
		merged.TimeoutSec = existing.TimeoutSec
	}
	if err := ValidateMcpServerInput(&merged); err != nil {
		return nil, wrapMcpServerValidateErr(err)
	}
	updated, err := uc.repo.Update(ctx, &merged)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrMcpServerNotFound
	}
	if err != nil {
		return nil, err
	}
	if resource.Name != updated.Name {
		resource.Name = updated.Name
		if err := uc.resources.UpdateResource(ctx, resource); err != nil {
			return nil, err
		}
	}
	return maskMcpServerMeta(updated), nil
}

// Delete deletes an MCP server; agent bindings cascade via FK.
func (uc *McpServerUsecase) Delete(ctx context.Context, id string) error {
	resource, err := uc.requireMcpServerPerm(ctx, id, PermAdmin)
	if err != nil {
		return err
	}
	err = uc.repo.Delete(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrMcpServerNotFound
	}
	if err != nil {
		return err
	}
	if err := uc.resources.DeleteResource(ctx, resource.ID); err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return err
	}
	return nil
}

// ListByAgent lists MCP servers bound to an agent that the caller can use.
// Env is NOT masked — intended for chat/runtime registration (like Tool ListByAgent).
func (uc *McpServerUsecase) ListByAgent(ctx context.Context, agentID string) ([]*McpServerMeta, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := uc.repo.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	usable := make([]*McpServerMeta, 0, len(servers))
	for _, server := range servers {
		resource, err := uc.resources.GetByPayload(ctx, ResourceTypeMcpServer, server.ID)
		if err != nil {
			continue
		}
		canUse, err := uc.access.Can(ctx, caller, resource.ID, PermUse, "")
		if err != nil {
			return nil, err
		}
		if canUse {
			usable = append(usable, server)
		}
	}
	return usable, nil
}

// ListByAgentForSession returns all MCP servers bound to an agent for a turn on an owned session.
// Caller MCP ACL is skipped; session ownership is the gate (channel peers lack PermUse).
func (uc *McpServerUsecase) ListByAgentForSession(ctx context.Context, agentID string) ([]*McpServerMeta, error) {
	return uc.repo.ListByAgent(ctx, agentID)
}

// BindToAgent binds MCP servers to an agent (full replace). Requires agent edit + each server PermUse.
func (uc *McpServerUsecase) BindToAgent(ctx context.Context, agentID string, serverIDs []string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if err := uc.requireAgentEdit(ctx, caller, agentID); err != nil {
		return err
	}
	if err := uc.requireServersUse(ctx, caller, serverIDs); err != nil {
		return err
	}
	err = uc.repo.BindServers(ctx, agentID, serverIDs)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrAgentNotFound
	}
	return err
}

// UnbindFromAgent removes the given MCP server bindings from an agent. Requires agent edit.
func (uc *McpServerUsecase) UnbindFromAgent(ctx context.Context, agentID string, serverIDs []string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if err := uc.requireAgentEdit(ctx, caller, agentID); err != nil {
		return err
	}
	err = uc.repo.UnbindServers(ctx, agentID, serverIDs)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrAgentNotFound
	}
	return err
}

// TestConnection acquires the server via the process pool, lists tools, and releases.
// Returns tool names. Env values are never logged.
func (uc *McpServerUsecase) TestConnection(ctx context.Context, id string) ([]string, error) {
	if _, err := uc.requireMcpServerPerm(ctx, id, PermUse); err != nil {
		return nil, err
	}
	meta, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrMcpServerNotFound
	}
	if err != nil {
		return nil, err
	}
	cfg := McpServerToConfig(meta)
	if cfg == nil {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid mcp server config")
	}
	pool := tool.DefaultMcpProcessPool()
	cli, release, err := pool.Acquire(ctx, cfg)
	if err != nil {
		uc.log.Warnf("mcp test connection acquire failed: id=%s transport=%s command=%s err=%v", meta.ID, meta.Transport, meta.Command, err)
		return nil, kratosErrors.BadRequest("MCP_TEST_FAILED", redactMcpErr(err))
	}
	defer release()
	tools, err := cli.ListTools(ctx)
	if err != nil {
		uc.log.Warnf("mcp test connection list tools failed: id=%s err=%v", meta.ID, err)
		return nil, kratosErrors.BadRequest("MCP_TEST_FAILED", redactMcpErr(err))
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// McpServerToConfig maps portal meta to framework tool.McpConfig.
func McpServerToConfig(m *McpServerMeta) *tool.McpConfig {
	if m == nil {
		return nil
	}
	env := m.Env
	if env != nil {
		env = make(map[string]string, len(m.Env))
		for k, v := range m.Env {
			env[k] = v
		}
	}
	args := m.Args
	if args != nil {
		args = append([]string(nil), m.Args...)
	}
	return &tool.McpConfig{
		Transport:  m.Transport,
		Endpoint:   m.Endpoint,
		Id:         m.ID,
		Backend:    m.Backend,
		Command:    m.Command,
		Args:       args,
		Env:        env,
		TimeoutSec: m.TimeoutSec,
	}
}

func (uc *McpServerUsecase) requireAgentEdit(ctx context.Context, caller, agentID string) error {
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeAgent, agentID)
	if err != nil {
		return ErrAgentNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return err
	}
	if !canView {
		return ErrAgentNotFound
	}
	canEdit, err := uc.access.Can(ctx, caller, resource.ID, PermEdit, "")
	if err != nil {
		return err
	}
	if !canEdit {
		return ErrForbiddenPerm
	}
	return nil
}

func (uc *McpServerUsecase) requireServersUse(ctx context.Context, caller string, serverIDs []string) error {
	for _, serverID := range serverIDs {
		resource, err := uc.resources.GetByPayload(ctx, ResourceTypeMcpServer, serverID)
		if err != nil {
			return ErrMcpServerNotFound
		}
		canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
		if err != nil {
			return err
		}
		if !canView {
			return ErrMcpServerNotFound
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

func redactMcpErr(err error) string {
	if err == nil {
		return "mcp test failed"
	}
	msg := err.Error()
	// Drop likely secret-bearing fragments from process env dumps.
	lower := strings.ToLower(msg)
	for _, key := range []string{"token", "secret", "password", "api_key", "apikey"} {
		if strings.Contains(lower, key) {
			return "mcp connection failed (details redacted)"
		}
	}
	return msg
}

func (uc *McpServerUsecase) requireMcpServerPerm(ctx context.Context, serverID string, need Perm) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeMcpServer, serverID)
	if err != nil {
		return nil, ErrMcpServerNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return nil, err
	}
	if !canView {
		return nil, ErrMcpServerNotFound
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

func wrapMcpServerValidateErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "not allowed") || strings.Contains(msg, "forbidden") {
		return kratosErrors.BadRequest("MCP_STDIO_CMD_DENIED", msg)
	}
	return kratosErrors.BadRequest("INVALID_ARGUMENT", msg)
}
