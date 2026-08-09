package biz

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeToolACLRepo struct {
	tools       map[string]*ToolMeta
	listItems   []*ToolMeta
	listByAgent []*ToolMeta
	created     *ToolMeta
}

func (f *fakeToolACLRepo) Create(_ context.Context, name, description string, toolType ToolType, config *structpb.Struct) (*ToolMeta, error) {
	f.created = &ToolMeta{ID: "tool-created", Name: name, Description: description, Type: toolType, Config: config}
	f.tools[f.created.ID] = f.created
	return f.created, nil
}
func (f *fakeToolACLRepo) GetByID(_ context.Context, id string) (*ToolMeta, error) {
	tool, ok := f.tools[id]
	if !ok {
		return nil, errNotFound
	}
	return tool, nil
}
func (f *fakeToolACLRepo) GetByName(context.Context, string) (*ToolMeta, error) {
	return nil, errNotFound
}
func (f *fakeToolACLRepo) List(_ context.Context, opts ListOptions) ([]*ToolMeta, int, error) {
	items := f.listItems
	if items == nil {
		items = make([]*ToolMeta, 0, len(f.tools))
		for _, tool := range f.tools {
			items = append(items, tool)
		}
	}
	if opts.IDs != nil {
		allow := make(map[string]struct{}, len(opts.IDs))
		for _, id := range opts.IDs {
			allow[id] = struct{}{}
		}
		filtered := make([]*ToolMeta, 0, len(items))
		for _, tool := range items {
			if _, ok := allow[tool.ID]; ok {
				filtered = append(filtered, tool)
			}
		}
		items = filtered
	}
	page, pageSize := opts.Page, opts.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	start := int((page - 1) * pageSize)
	if start >= len(items) {
		return nil, len(items), nil
	}
	end := start + int(pageSize)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], len(items), nil
}
func (f *fakeToolACLRepo) Update(_ context.Context, id string, updates map[string]any) (*ToolMeta, error) {
	tool, err := f.GetByID(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if name, ok := updates["name"].(string); ok {
		tool.Name = name
	}
	return tool, nil
}
func (f *fakeToolACLRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.tools[id]; !ok {
		return errNotFound
	}
	delete(f.tools, id)
	return nil
}
func (f *fakeToolACLRepo) ListByAgent(context.Context, string) ([]*ToolMeta, error) {
	return f.listByAgent, nil
}
func (f *fakeToolACLRepo) IsBoundToAgent(context.Context, string) (bool, error) { return false, nil }

type fakeToolResourceRepo struct {
	fakeResourceReader
	byPayload map[string]*Resource
	created   []*Resource
}

func (f *fakeToolResourceRepo) CreateResource(_ context.Context, resource *Resource) (*Resource, error) {
	if resource.ID == "" {
		resource.ID = "resource-" + resource.PayloadRef
	}
	f.resources[resource.ID] = resource
	f.byPayload[string(resource.Type)+":"+resource.PayloadRef] = resource
	f.created = append(f.created, resource)
	return resource, nil
}
func (f *fakeToolResourceRepo) UpdateResource(_ context.Context, resource *Resource) error {
	f.resources[resource.ID] = resource
	return nil
}
func (f *fakeToolResourceRepo) DeleteResource(_ context.Context, id string) error {
	if _, ok := f.resources[id]; !ok {
		return errNotFound
	}
	delete(f.resources, id)
	return nil
}
func (f *fakeToolResourceRepo) GetByPayload(_ context.Context, resourceType ResourceType, payloadRef string) (*Resource, error) {
	resource, ok := f.byPayload[string(resourceType)+":"+payloadRef]
	if !ok {
		return nil, errNotFound
	}
	return resource, nil
}
func (f *fakeToolResourceRepo) ListAllByType(_ context.Context, resourceType ResourceType) ([]*Resource, error) {
	var resources []*Resource
	for _, resource := range f.resources {
		if resource.Type == resourceType {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}
func (f *fakeToolResourceRepo) CreateGrant(context.Context, ResourceGrant) error { return nil }
func (f *fakeToolResourceRepo) ListGrantsByResourceIDs(_ context.Context, resourceIDs []string) (map[string][]ResourceGrant, error) {
	out := make(map[string][]ResourceGrant, len(resourceIDs))
	for _, id := range resourceIDs {
		out[id] = f.grants[id]
	}
	return out, nil
}

func newToolACLUsecase() (*ToolUsecase, *fakeToolACLRepo, *fakeToolResourceRepo) {
	tools := &fakeToolACLRepo{tools: map[string]*ToolMeta{}}
	resources := &fakeToolResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	return NewToolUsecase(tools, resources, NewAccessChecker(resources), log.NewStdLogger(nil)), tools, resources
}

func TestToolCreateCreatesPrivateResourceForCaller(t *testing.T) {
	uc, _, resources := newToolACLUsecase()

	tool, err := uc.Create(WithCallerUserID(context.Background(), "user-1"), "tool", "", string(ToolTypeMCP), nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(resources.created) != 1 {
		t.Fatalf("created resources = %d, want 1", len(resources.created))
	}
	resource := resources.created[0]
	if resource.Type != ResourceTypeTool || resource.PayloadRef != tool.ID || resource.OwnerUserID != "user-1" || resource.Visibility != VisibilityPrivate {
		t.Fatalf("resource = %#v, want private tool resource owned by user-1", resource)
	}
}

func TestToolListReturnsVisibleTotalAcrossPages(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	tools.listItems = []*ToolMeta{{ID: "tool-1"}, {ID: "tool-2"}, {ID: "tool-3"}}
	for _, tool := range tools.listItems {
		resource := &Resource{ID: "resource-" + tool.ID, Type: ResourceTypeTool, PayloadRef: tool.ID, OwnerUserID: "user-1", Visibility: VisibilityPrivate}
		resources.resources[resource.ID] = resource
		resources.byPayload["tool:"+tool.ID] = resource
	}

	items, total, err := uc.List(WithCallerUserID(context.Background(), "user-1"), 2, 1, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "tool-2" || total != 3 {
		t.Fatalf("List = %#v, total=%d; want second visible tool and total 3", items, total)
	}
}

func TestToolListFiltersResourcesCallerCannotView(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	tools.tools["visible"] = &ToolMeta{ID: "visible"}
	tools.tools["hidden"] = &ToolMeta{ID: "hidden"}
	resources.resources["visible-resource"] = &Resource{ID: "visible-resource", Type: ResourceTypeTool, PayloadRef: "visible", OwnerUserID: "user-1", Visibility: VisibilityPrivate}
	resources.resources["hidden-resource"] = &Resource{ID: "hidden-resource", Type: ResourceTypeTool, PayloadRef: "hidden", OwnerUserID: "user-2", Visibility: VisibilityPrivate}
	resources.byPayload["tool:visible"] = resources.resources["visible-resource"]
	resources.byPayload["tool:hidden"] = resources.resources["hidden-resource"]

	items, total, err := uc.List(WithCallerUserID(context.Background(), "user-1"), 1, 10, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "visible" || total != 1 {
		t.Fatalf("List = %#v, total=%d; want only visible tool", items, total)
	}
}

func TestToolListByAgentFiltersToolsCallerCannotUse(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	tools.listByAgent = []*ToolMeta{{ID: "usable"}, {ID: "view-only"}}
	for _, tool := range tools.listByAgent {
		resource := &Resource{ID: "resource-" + tool.ID, Type: ResourceTypeTool, PayloadRef: tool.ID, OwnerUserID: "owner", Visibility: VisibilityPrivate}
		resources.resources[resource.ID] = resource
		resources.byPayload["tool:"+tool.ID] = resource
	}
	resources.grants["resource-usable"] = []ResourceGrant{{ResourceID: "resource-usable", GranteeType: "user", GranteeID: "user-1", Perm: PermUse}}
	resources.grants["resource-view-only"] = []ResourceGrant{{ResourceID: "resource-view-only", GranteeType: "user", GranteeID: "user-1", Perm: PermView}}

	items, err := uc.ListByAgent(WithCallerUserID(context.Background(), "user-1"), "agent-1")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(items) != 1 || items[0].ID != "usable" {
		t.Fatalf("ListByAgent = %#v, want only usable tool", items)
	}
}

func TestToolGetHidesResourceCallerCannotView(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	tools.tools["tool-1"] = &ToolMeta{ID: "tool-1"}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeTool, PayloadRef: "tool-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["tool:tool-1"] = resource

	if _, err := uc.Get(WithCallerUserID(context.Background(), "stranger"), "tool-1"); !isReason(err, "TOOL_NOT_FOUND") {
		t.Fatalf("Get without view error = %v, want TOOL_NOT_FOUND", err)
	}
}

func TestToolUpdateForbidsViewOnlyCaller(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	tools.tools["tool-1"] = &ToolMeta{ID: "tool-1"}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeTool, PayloadRef: "tool-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["tool:tool-1"] = resource
	resources.grants[resource.ID] = []ResourceGrant{{ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView}}

	if _, err := uc.Update(WithCallerUserID(context.Background(), "stranger"), "tool-1", nil, nil, nil, nil); !isReason(err, "TOOL_NOT_FOUND") {
		t.Fatalf("Update without view error = %v, want TOOL_NOT_FOUND", err)
	}
	if _, err := uc.Update(WithCallerUserID(context.Background(), "viewer"), "tool-1", nil, nil, nil, nil); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("Update with view-only error = %v, want FORBIDDEN_PERM", err)
	}
}

func TestToolDeleteForbidsViewOnlyCaller(t *testing.T) {
	uc, tools, resources := newToolACLUsecase()
	tools.tools["tool-1"] = &ToolMeta{ID: "tool-1"}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeTool, PayloadRef: "tool-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["tool:tool-1"] = resource
	resources.grants[resource.ID] = []ResourceGrant{{ResourceID: resource.ID, GranteeType: "user", GranteeID: "viewer", Perm: PermView}}

	if err := uc.Delete(WithCallerUserID(context.Background(), "stranger"), "tool-1"); !isReason(err, "TOOL_NOT_FOUND") {
		t.Fatalf("Delete without view error = %v, want TOOL_NOT_FOUND", err)
	}
	if err := uc.Delete(WithCallerUserID(context.Background(), "viewer"), "tool-1"); !isReason(err, "FORBIDDEN_PERM") {
		t.Fatalf("Delete with view-only error = %v, want FORBIDDEN_PERM", err)
	}
}
