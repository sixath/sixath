package biz

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

type fakeAgentACLRepo struct {
	agents    map[string]*AgentMeta
	listItems []*AgentMeta
	created   *AgentMeta
}

func (f *fakeAgentACLRepo) Create(_ context.Context, id, name, description, systemPrompt, workspace string, modelConfig ModelConfig, debugRun bool, wecomChannelID string, runtimeTools RuntimeToolsConfig, toolIDs []string) (*AgentMeta, error) {
	f.created = &AgentMeta{ID: id, Name: name, Workspace: workspace}
	f.agents[f.created.ID] = f.created
	return f.created, nil
}
func (f *fakeAgentACLRepo) CountByWecomChannelID(context.Context, string) (int, error) {
	return 0, nil
}
func (f *fakeAgentACLRepo) GetByID(_ context.Context, id string) (*AgentMeta, error) {
	agent, ok := f.agents[id]
	if !ok {
		return nil, errNotFound
	}
	return agent, nil
}
func (f *fakeAgentACLRepo) GetByName(context.Context, string) (*AgentMeta, error) {
	return nil, errNotFound
}
func (f *fakeAgentACLRepo) List(_ context.Context, page, pageSize int32) ([]*AgentMeta, int, error) {
	if f.listItems != nil {
		if page < 1 {
			page = 1
		}
		if pageSize < 1 {
			pageSize = 10
		}
		start := int((page - 1) * pageSize)
		if start >= len(f.listItems) {
			return nil, len(f.listItems), nil
		}
		end := start + int(pageSize)
		if end > len(f.listItems) {
			end = len(f.listItems)
		}
		return f.listItems[start:end], len(f.listItems), nil
	}
	items := make([]*AgentMeta, 0, len(f.agents))
	for _, agent := range f.agents {
		items = append(items, agent)
	}
	return items, len(items), nil
}
func (f *fakeAgentACLRepo) ListByIDs(_ context.Context, ids []string, page, pageSize int32) ([]*AgentMeta, int, error) {
	allow := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allow[id] = struct{}{}
	}
	var items []*AgentMeta
	if f.listItems != nil {
		for _, agent := range f.listItems {
			if _, ok := allow[agent.ID]; ok {
				items = append(items, agent)
			}
		}
	} else {
		for _, agent := range f.agents {
			if _, ok := allow[agent.ID]; ok {
				items = append(items, agent)
			}
		}
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	start := int((page - 1) * pageSize)
	if start >= len(items) {
		return []*AgentMeta{}, len(items), nil
	}
	end := start + int(pageSize)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], len(items), nil
}
func (f *fakeAgentACLRepo) Update(_ context.Context, id string, _ map[string]any) (*AgentMeta, error) {
	return f.GetByID(context.Background(), id)
}
func (f *fakeAgentACLRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.agents[id]; !ok {
		return errNotFound
	}
	delete(f.agents, id)
	return nil
}
func (f *fakeAgentACLRepo) BindTools(context.Context, string, []string) error   { return nil }
func (f *fakeAgentACLRepo) UnbindTools(context.Context, string, []string) error { return nil }
func (f *fakeAgentACLRepo) ListDistinctWorkspaces(context.Context, int) ([]CuratorWorkspace, error) {
	return nil, nil
}
func (f *fakeAgentACLRepo) ListAgentIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}

type fakeAgentResourceRepo struct {
	fakeResourceReader
	byPayload map[string]*Resource
	created   []*Resource
}

func (f *fakeAgentResourceRepo) CreateResource(_ context.Context, resource *Resource) (*Resource, error) {
	if resource.ID == "" {
		resource.ID = "resource-" + resource.PayloadRef
	}
	f.resources[resource.ID] = resource
	f.byPayload[string(resource.Type)+":"+resource.PayloadRef] = resource
	f.created = append(f.created, resource)
	return resource, nil
}
func (f *fakeAgentResourceRepo) UpdateResource(_ context.Context, resource *Resource) error {
	f.resources[resource.ID] = resource
	return nil
}
func (f *fakeAgentResourceRepo) DeleteResource(_ context.Context, id string) error {
	if _, ok := f.resources[id]; !ok {
		return errNotFound
	}
	delete(f.resources, id)
	return nil
}
func (f *fakeAgentResourceRepo) GetByPayload(_ context.Context, resourceType ResourceType, payloadRef string) (*Resource, error) {
	resource, ok := f.byPayload[string(resourceType)+":"+payloadRef]
	if !ok {
		return nil, errNotFound
	}
	return resource, nil
}
func (f *fakeAgentResourceRepo) ListAllByType(_ context.Context, resourceType ResourceType) ([]*Resource, error) {
	var resources []*Resource
	for _, resource := range f.resources {
		if resource.Type == resourceType {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}
func (f *fakeAgentResourceRepo) CreateGrant(context.Context, ResourceGrant) error { return nil }
func (f *fakeAgentResourceRepo) ListGrantsByResourceIDs(_ context.Context, resourceIDs []string) (map[string][]ResourceGrant, error) {
	out := make(map[string][]ResourceGrant, len(resourceIDs))
	for _, id := range resourceIDs {
		out[id] = f.grants[id]
	}
	return out, nil
}

func newAgentACLUsecase() (*AgentUsecase, *fakeAgentACLRepo, *fakeAgentResourceRepo) {
	return newAgentACLUsecaseAt("/portal-data")
}

func newAgentACLUsecaseAt(dataRoot string) (*AgentUsecase, *fakeAgentACLRepo, *fakeAgentResourceRepo) {
	agents := &fakeAgentACLRepo{agents: map[string]*AgentMeta{}}
	resources := &fakeAgentResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	return NewAgentUsecase(agents, resources, NewAccessChecker(resources), dataRoot, log.NewStdLogger(nil)), agents, resources
}

func TestAgentCreateCreatesPrivateResourceForCaller(t *testing.T) {
	root := t.TempDir()
	uc, agents, resources := newAgentACLUsecaseAt(root)
	ctx := WithOrgID(WithCallerUserID(context.Background(), "user-1"), "org-1")

	agent, err := uc.Create(ctx, "agent", "", "", "", ModelConfig{}, false, "", RuntimeToolsConfig{}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if agents.created != agent {
		t.Fatal("Create did not persist the returned agent")
	}
	if want := filepath.Join(root, "agents", agent.ID); agent.Workspace != want {
		t.Fatalf("workspace = %q, want %q", agent.Workspace, want)
	}
	if st, err := os.Stat(agent.Workspace); err != nil || !st.IsDir() {
		t.Fatalf("workspace dir missing: %v", err)
	}
	if len(resources.created) != 1 {
		t.Fatalf("created resources = %d, want 1", len(resources.created))
	}
	resource := resources.created[0]
	if resource.Type != ResourceTypeAgent || resource.PayloadRef != agent.ID || resource.OwnerUserID != "user-1" || resource.Visibility != VisibilityPrivate || resource.HomeOrgID != "" {
		t.Fatalf("resource = %#v, want private agent resource owned by user-1", resource)
	}
}

func TestAgentListFiltersResourcesCallerCannotView(t *testing.T) {
	uc, agents, resources := newAgentACLUsecase()
	agents.agents["visible"] = &AgentMeta{ID: "visible"}
	agents.agents["hidden"] = &AgentMeta{ID: "hidden"}
	resources.resources["visible-resource"] = &Resource{ID: "visible-resource", Type: ResourceTypeAgent, PayloadRef: "visible", OwnerUserID: "user-1", Visibility: VisibilityPrivate}
	resources.resources["hidden-resource"] = &Resource{ID: "hidden-resource", Type: ResourceTypeAgent, PayloadRef: "hidden", OwnerUserID: "user-2", Visibility: VisibilityPrivate}
	resources.byPayload["agent:visible"] = resources.resources["visible-resource"]
	resources.byPayload["agent:hidden"] = resources.resources["hidden-resource"]

	items, total, err := uc.List(WithCallerUserID(context.Background(), "user-1"), 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "visible" || total != 1 {
		t.Fatalf("List = %#v, total=%d; want only visible agent", items, total)
	}
}

func TestAgentListReturnsVisibleTotalAcrossPages(t *testing.T) {
	uc, agents, resources := newAgentACLUsecase()
	agents.listItems = []*AgentMeta{{ID: "agent-1"}, {ID: "agent-2"}, {ID: "agent-3"}}
	for _, agent := range agents.listItems {
		resource := &Resource{ID: "resource-" + agent.ID, Type: ResourceTypeAgent, PayloadRef: agent.ID, OwnerUserID: "user-1", Visibility: VisibilityPrivate}
		resources.resources[resource.ID] = resource
		resources.byPayload["agent:"+agent.ID] = resource
	}

	items, total, err := uc.List(WithCallerUserID(context.Background(), "user-1"), 2, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "agent-2" || total != 3 {
		t.Fatalf("List = %#v, total=%d; want second visible agent and total 3", items, total)
	}
}

func TestAgentUpdateHidesMissingViewButForbidsReadOnlyCaller(t *testing.T) {
	uc, agents, resources := newAgentACLUsecase()
	agents.agents["agent-1"] = &AgentMeta{ID: "agent-1"}
	resources.resources["resource-1"] = &Resource{ID: "resource-1", Type: ResourceTypeAgent, PayloadRef: "agent-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.byPayload["agent:agent-1"] = resources.resources["resource-1"]
	resources.grants["resource-1"] = []ResourceGrant{{ResourceID: "resource-1", GranteeType: "user", GranteeID: "viewer", Perm: PermView}}

	if _, err := uc.Update(WithCallerUserID(context.Background(), "stranger"), "agent-1", map[string]any{"name": "new"}); !isReason(err, "AGENT_NOT_FOUND") {
		t.Fatalf("Update without view error = %v, want AGENT_NOT_FOUND", err)
	}
	if _, err := uc.Update(WithCallerUserID(context.Background(), "viewer"), "agent-1", map[string]any{"name": "new"}); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("Update with view-only error = %v, want FORBIDDEN_PERM", err)
	}
}

func TestAgentGetAndDeleteHideOrForbidByPermission(t *testing.T) {
	uc, agents, resources := newAgentACLUsecase()
	agents.agents["agent-1"] = &AgentMeta{ID: "agent-1"}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeAgent, PayloadRef: "agent-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["agent:agent-1"] = resource
	resources.grants[resource.ID] = []ResourceGrant{{ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView}}

	if _, err := uc.Get(WithCallerUserID(context.Background(), "stranger"), "agent-1"); !isReason(err, "AGENT_NOT_FOUND") {
		t.Fatalf("Get without view error = %v, want AGENT_NOT_FOUND", err)
	}
	if err := uc.Delete(WithCallerUserID(context.Background(), "viewer"), "agent-1"); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("Delete with view-only error = %v, want FORBIDDEN_PERM", err)
	}
}

func TestAgentGetForUseForbidsViewOnlyCaller(t *testing.T) {
	uc, agents, resources := newAgentACLUsecase()
	agents.agents["agent-1"] = &AgentMeta{ID: "agent-1"}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeAgent, PayloadRef: "agent-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["agent:agent-1"] = resource
	resources.grants[resource.ID] = []ResourceGrant{{ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView}}

	if _, err := uc.GetForUse(WithCallerUserID(context.Background(), "viewer"), "agent-1"); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("GetForUse with view-only error = %v, want FORBIDDEN_PERM", err)
	}
}

func TestAgentBindToolsRequiresAgentEditAndToolUse(t *testing.T) {
	uc, agents, resources := newAgentACLUsecase()
	agents.agents["agent-1"] = &AgentMeta{ID: "agent-1"}
	agentResource := &Resource{ID: "agent-resource", Type: ResourceTypeAgent, PayloadRef: "agent-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	toolResource := &Resource{ID: "tool-resource", Type: ResourceTypeTool, PayloadRef: "tool-1", OwnerUserID: "tool-owner", Visibility: VisibilityPrivate}
	resources.resources[agentResource.ID] = agentResource
	resources.resources[toolResource.ID] = toolResource
	resources.byPayload["agent:agent-1"] = agentResource
	resources.byPayload["tool:tool-1"] = toolResource
	resources.grants[agentResource.ID] = []ResourceGrant{{ResourceID: agentResource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView}}
	resources.grants[toolResource.ID] = []ResourceGrant{{ResourceID: toolResource.ID, GranteeType: "user", GranteeID: "owner", Perm: PermView}}

	if err := uc.BindTools(WithCallerUserID(context.Background(), "stranger"), "agent-1", []string{"tool-1"}); !isReason(err, "AGENT_NOT_FOUND") {
		t.Fatalf("BindTools stranger error = %v, want AGENT_NOT_FOUND", err)
	}
	if err := uc.BindTools(WithCallerUserID(context.Background(), "viewer"), "agent-1", []string{"tool-1"}); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("BindTools view-only agent error = %v, want FORBIDDEN_PERM", err)
	}
	if err := uc.BindTools(WithCallerUserID(context.Background(), "owner"), "agent-1", []string{"tool-1"}); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("BindTools tool without use error = %v, want FORBIDDEN_PERM", err)
	}
}

func isReason(err error, reason string) bool {
	return err != nil && kratosErrors.FromError(err).Reason == reason
}

func TestRequireWorkspaceRoot(t *testing.T) {
	if err := RequireWorkspaceRoot("  "); err != ErrWorkspaceRequired {
		t.Fatalf("empty = %v, want ErrWorkspaceRequired", err)
	}
	if err := RequireWorkspaceRoot("/ws"); err != nil {
		t.Fatalf("non-empty: %v", err)
	}
}
