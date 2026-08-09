package biz

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

func TestMcpServerMaskSensitiveEnv(t *testing.T) {
	env := map[string]string{
		"CONFLUENCE_HOST":      "h.example.com",
		"CONFLUENCE_API_TOKEN": "secret",
		"api_key":              "k",
		"DbPassword":           "p",
		"MY_SECRET":            "s",
		"PLAIN":                "ok",
	}
	got := MaskSensitiveEnv(env)
	if got["CONFLUENCE_HOST"] != "h.example.com" || got["PLAIN"] != "ok" {
		t.Fatalf("non-sensitive = %#v", got)
	}
	for _, k := range []string{"CONFLUENCE_API_TOKEN", "api_key", "DbPassword", "MY_SECRET"} {
		if got[k] != "***" {
			t.Fatalf("key %s = %q, want ***", k, got[k])
		}
	}
	if MaskSensitiveEnv(nil) != nil {
		t.Fatal("nil env should stay nil")
	}
}

func TestMcpServerMergeEnvPreservingMasked(t *testing.T) {
	existing := map[string]string{"TOKEN": "real", "HOST": "old"}
	incoming := map[string]string{"TOKEN": "***", "HOST": "new", "EXTRA": "x"}
	got := MergeEnvPreservingMasked(existing, incoming)
	if got["TOKEN"] != "real" {
		t.Fatalf("TOKEN = %q, want real", got["TOKEN"])
	}
	if got["HOST"] != "new" || got["EXTRA"] != "x" {
		t.Fatalf("got %#v", got)
	}
	// masked key missing from existing keeps ***
	got2 := MergeEnvPreservingMasked(nil, map[string]string{"TOKEN": "***"})
	if got2["TOKEN"] != "***" {
		t.Fatalf("missing existing TOKEN = %q, want ***", got2["TOKEN"])
	}
}

func TestValidateMcpServerInput_RejectsBash(t *testing.T) {
	err := ValidateMcpServerInput(&McpServerMeta{
		ID:        "bad-bash",
		Name:      "bad",
		Transport: "stdio",
		Command:   "bash",
		Args:      []string{"-c", "id"},
	})
	if err == nil {
		t.Fatal("expected deny for bash")
	}
}

func TestValidateMcpServerInput_HTTPRequiresEndpoint(t *testing.T) {
	err := ValidateMcpServerInput(&McpServerMeta{
		ID:        "http1",
		Name:      "h",
		Transport: "http",
	})
	if err == nil {
		t.Fatal("expected endpoint required")
	}
}

func TestValidateMcpServerID(t *testing.T) {
	if err := ValidateMcpServerID("confluence"); err != nil {
		t.Fatalf("valid id: %v", err)
	}
	if err := ValidateMcpServerID("Bad"); err == nil {
		t.Fatal("uppercase should fail")
	}
	if err := ValidateMcpServerID("1bad"); err == nil {
		t.Fatal("leading digit should fail")
	}
}

type fakeMcpServerRepo struct {
	servers     map[string]*McpServerMeta
	listByAgent []*McpServerMeta
}

func (f *fakeMcpServerRepo) Create(_ context.Context, meta *McpServerMeta) (*McpServerMeta, error) {
	cp := *meta
	if meta.Env != nil {
		cp.Env = make(map[string]string, len(meta.Env))
		for k, v := range meta.Env {
			cp.Env[k] = v
		}
	}
	f.servers[cp.ID] = &cp
	return &cp, nil
}
func (f *fakeMcpServerRepo) GetByID(_ context.Context, id string) (*McpServerMeta, error) {
	m, ok := f.servers[id]
	if !ok {
		return nil, errNotFound
	}
	cp := *m
	if m.Env != nil {
		cp.Env = make(map[string]string, len(m.Env))
		for k, v := range m.Env {
			cp.Env[k] = v
		}
	}
	return &cp, nil
}
func (f *fakeMcpServerRepo) List(_ context.Context, opts ListOptions) ([]*McpServerMeta, int, error) {
	items := make([]*McpServerMeta, 0, len(f.servers))
	for _, s := range f.servers {
		items = append(items, s)
	}
	if opts.IDs != nil {
		allow := make(map[string]struct{}, len(opts.IDs))
		for _, id := range opts.IDs {
			allow[id] = struct{}{}
		}
		filtered := make([]*McpServerMeta, 0, len(items))
		for _, s := range items {
			if _, ok := allow[s.ID]; ok {
				filtered = append(filtered, s)
			}
		}
		items = filtered
	}
	return items, len(items), nil
}
func (f *fakeMcpServerRepo) Update(_ context.Context, meta *McpServerMeta) (*McpServerMeta, error) {
	if _, ok := f.servers[meta.ID]; !ok {
		return nil, errNotFound
	}
	cp := *meta
	if meta.Env != nil {
		cp.Env = make(map[string]string, len(meta.Env))
		for k, v := range meta.Env {
			cp.Env[k] = v
		}
	}
	f.servers[cp.ID] = &cp
	return &cp, nil
}
func (f *fakeMcpServerRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.servers[id]; !ok {
		return errNotFound
	}
	delete(f.servers, id)
	return nil
}
func (f *fakeMcpServerRepo) ListByAgent(context.Context, string) ([]*McpServerMeta, error) {
	return f.listByAgent, nil
}
func (f *fakeMcpServerRepo) BindServers(context.Context, string, []string) error { return nil }
func (f *fakeMcpServerRepo) UnbindServers(context.Context, string, []string) error {
	return nil
}

type fakeMcpResourceRepo struct {
	fakeResourceReader
	byPayload map[string]*Resource
	created   []*Resource
}

func (f *fakeMcpResourceRepo) CreateResource(_ context.Context, resource *Resource) (*Resource, error) {
	if resource.ID == "" {
		resource.ID = "resource-" + resource.PayloadRef
	}
	f.resources[resource.ID] = resource
	f.byPayload[string(resource.Type)+":"+resource.PayloadRef] = resource
	f.created = append(f.created, resource)
	return resource, nil
}
func (f *fakeMcpResourceRepo) UpdateResource(_ context.Context, resource *Resource) error {
	f.resources[resource.ID] = resource
	return nil
}
func (f *fakeMcpResourceRepo) DeleteResource(_ context.Context, id string) error {
	if _, ok := f.resources[id]; !ok {
		return errNotFound
	}
	delete(f.resources, id)
	return nil
}
func (f *fakeMcpResourceRepo) GetByPayload(_ context.Context, resourceType ResourceType, payloadRef string) (*Resource, error) {
	resource, ok := f.byPayload[string(resourceType)+":"+payloadRef]
	if !ok {
		return nil, errNotFound
	}
	return resource, nil
}
func (f *fakeMcpResourceRepo) ListAllByType(_ context.Context, resourceType ResourceType) ([]*Resource, error) {
	var resources []*Resource
	for _, resource := range f.resources {
		if resource.Type == resourceType {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}
func (f *fakeMcpResourceRepo) CreateGrant(context.Context, ResourceGrant) error { return nil }
func (f *fakeMcpResourceRepo) ListGrantsByResourceIDs(_ context.Context, resourceIDs []string) (map[string][]ResourceGrant, error) {
	out := make(map[string][]ResourceGrant, len(resourceIDs))
	for _, id := range resourceIDs {
		out[id] = f.grants[id]
	}
	return out, nil
}

func newMcpServerACLUsecase() (*McpServerUsecase, *fakeMcpServerRepo, *fakeMcpResourceRepo) {
	servers := &fakeMcpServerRepo{servers: map[string]*McpServerMeta{}}
	resources := &fakeMcpResourceRepo{
		fakeResourceReader: fakeResourceReader{
			resources: map[string]*Resource{},
			grants:    map[string][]ResourceGrant{},
			userOrgs:  map[string][]string{},
		},
		byPayload: map[string]*Resource{},
	}
	return NewMcpServerUsecase(servers, resources, NewAccessChecker(resources), log.NewStdLogger(nil)), servers, resources
}

func TestMcpServerCreateRejectsBash(t *testing.T) {
	uc, _, _ := newMcpServerACLUsecase()
	_, err := uc.Create(WithCallerUserID(context.Background(), "user-1"), &McpServerMeta{
		ID:        "bad-bash",
		Name:      "bad",
		Transport: "stdio",
		Command:   "bash",
		Args:      []string{"-c", "id"},
	})
	if !isReason(err, "MCP_STDIO_CMD_DENIED") {
		t.Fatalf("Create bash error = %v, want MCP_STDIO_CMD_DENIED", err)
	}
}

func TestMcpServerCreateCreatesPrivateResource(t *testing.T) {
	uc, _, resources := newMcpServerACLUsecase()
	meta, err := uc.Create(WithCallerUserID(context.Background(), "user-1"), &McpServerMeta{
		ID:        "confluence",
		Name:      "Confluence",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@atlassian-dc-mcp/confluence"},
		Env:       map[string]string{"CONFLUENCE_API_TOKEN": "secret"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.Env["CONFLUENCE_API_TOKEN"] != "***" {
		t.Fatalf("Create should mask env, got %#v", meta.Env)
	}
	if len(resources.created) != 1 {
		t.Fatalf("created resources = %d, want 1", len(resources.created))
	}
	resource := resources.created[0]
	if resource.Type != ResourceTypeMcpServer || resource.PayloadRef != "confluence" || resource.OwnerUserID != "user-1" {
		t.Fatalf("resource = %#v", resource)
	}
}

func TestMcpServerGetHidesFromStranger(t *testing.T) {
	uc, servers, resources := newMcpServerACLUsecase()
	servers.servers["srv-1"] = &McpServerMeta{ID: "srv-1", Name: "s", Transport: "http", Endpoint: "http://x"}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeMcpServer, PayloadRef: "srv-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["mcp_server:srv-1"] = resource

	if _, err := uc.Get(WithCallerUserID(context.Background(), "stranger"), "srv-1"); !isReason(err, "MCP_SERVER_NOT_FOUND") {
		t.Fatalf("Get without view error = %v, want MCP_SERVER_NOT_FOUND", err)
	}
}

func TestMcpServerUpdateMergesMaskedEnv(t *testing.T) {
	uc, servers, resources := newMcpServerACLUsecase()
	servers.servers["srv-1"] = &McpServerMeta{
		ID: "srv-1", Name: "s", Transport: "stdio", Command: "npx",
		Args: []string{"-y", "pkg"},
		Env:  map[string]string{"API_TOKEN": "real-secret", "HOST": "h"},
	}
	resource := &Resource{ID: "resource-1", Type: ResourceTypeMcpServer, PayloadRef: "srv-1", OwnerUserID: "owner", Visibility: VisibilityPrivate}
	resources.resources[resource.ID] = resource
	resources.byPayload["mcp_server:srv-1"] = resource

	updated, err := uc.Update(WithCallerUserID(context.Background(), "owner"), &McpServerMeta{
		ID: "srv-1", Name: "s2", Transport: "stdio", Command: "npx",
		Args: []string{"-y", "pkg"},
		Env:  map[string]string{"API_TOKEN": "***", "HOST": "h2"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Env["API_TOKEN"] != "***" {
		t.Fatalf("response env not masked: %#v", updated.Env)
	}
	stored := servers.servers["srv-1"]
	if stored.Env["API_TOKEN"] != "real-secret" || stored.Env["HOST"] != "h2" || stored.Name != "s2" {
		t.Fatalf("stored = %#v", stored)
	}
}
