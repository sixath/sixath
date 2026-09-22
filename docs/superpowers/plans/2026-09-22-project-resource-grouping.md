# Project Resource Grouping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Project as a new ResourceType with permission inheritance, so org members can group resources and manage permissions at the group level.

**Architecture:** Project is stored as a Resource record (`type="project"`, `payload_ref=self`). Other resources get an optional `ProjectID` field. Permission inheritance happens in `VisiblePayloadRefs` and `AccessChecker.EffectivePerm` by merging project grants with child grants before calling `EvaluatePerm`. No protobuf changes — everything is hand-written handlers.

**Tech Stack:** Go (Kratos v2, GORM MySQL) + React/TypeScript (hand-written API client, no protobuf)

---

## File Structure

| File | Responsibility | Action |
|------|---------------|--------|
| `portal/internal/data/model/resource.go` | GORM Resource model | Modify: add `ProjectID` column |
| `portal/internal/biz/resource.go` | ACL core types + EvaluatePerm + AccessChecker | Modify: add `ResourceTypeProject`, `ProjectID` field, project-inheritance in EffectivePerm |
| `portal/internal/biz/identity.go` | ResourceRepo interface | Modify: add `ListByProject`, `UpdateProjectID` |
| `portal/internal/data/resource_mysql.go` | MySQL CRUD for resources | Modify: update mappers, add `ListByProject`, `UpdateProjectID` |
| `portal/internal/biz/resource_list_acl.go` | Batch ACL for list APIs | Modify: include project grants |
| `portal/internal/biz/acl_api.go` | ACL API usecase | Modify: add `ProjectUsecase` |
| `portal/internal/server/acl_api.go` | HTTP handlers | Modify: add project handlers |
| `portal/internal/server/http.go` | Route registration | Modify: add project routes |
| `portal/cmd/backend/wire.go` | DI wiring | Modify: add project deps |
| `portal/internal/biz/biz.go` | ProviderSet | Modify: add NewProjectUsecase |
| `portal/scripts/migration_project_id.sql` | DB migration | Create |
| `web/src/api/resource.ts` | Resource API client | Modify: add project methods |
| `web/src/pages/ProjectListPage.tsx` | Project list view | Create |
| `web/src/pages/ProjectDetailPage.tsx` | Project detail + member resources | Create |
| `web/src/App.tsx` | Router + sidebar + breadcrumb | Modify |

---

### Task 1: DB schema — add project_id column to resources table + GORM model

**Files:**
- Create: `portal/scripts/migration_project_id.sql`
- Modify: `portal/internal/data/model/resource.go:6-17`

- [ ] **Step 1: Write migration SQL**

```sql
-- migration_project_id.sql
-- Add project_id column to resources table for project-based resource grouping.
-- DEFAULT '' means the resource is not in any project (backward-compatible).

ALTER TABLE resources ADD COLUMN project_id VARCHAR(36) DEFAULT '' NOT NULL AFTER home_org_id;
CREATE INDEX idx_resources_project_id ON resources(project_id);
```

- [ ] **Step 2: Add ProjectID to GORM model**

In `portal/internal/data/model/resource.go`, after line 12 (`HomeOrgID` field):

```go
	ProjectID    string    `gorm:"column:project_id;size:36;index;default:''"`
```

The full struct becomes:

```go
type Resource struct {
	ID           string    `gorm:"column:id;primaryKey;size:36"`
	Type         string    `gorm:"column:type;size:16;not null;uniqueIndex:idx_resource_type_payload"`
	Name         string    `gorm:"column:name;size:128;not null"`
	OwnerUserID  string    `gorm:"column:owner_user_id;size:36;not null;index"`
	Visibility   string    `gorm:"column:visibility;size:16;not null"`
	HomeOrgID    string    `gorm:"column:home_org_id;size:36;index"`
	ProjectID    string    `gorm:"column:project_id;size:36;index;default:''"`
	BoundAgentID string    `gorm:"column:bound_agent_id;size:36;index"`
	PayloadRef   string    `gorm:"column:payload_ref;size:36;not null;uniqueIndex:idx_resource_type_payload"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}
```

- [ ] **Step 3: Commit**

```bash
git add portal/scripts/migration_project_id.sql portal/internal/data/model/resource.go
git commit -m "feat(acl): add project_id column to resources table

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Biz layer — add ResourceTypeProject + ProjectID to Resource struct

**Files:**
- Modify: `portal/internal/biz/resource.go:54-71`

- [ ] **Step 1: Add ResourceTypeProject constant**

In `portal/internal/biz/resource.go`, after line 59 (`ResourceTypeProxy`):

```go
	ResourceTypeProject ResourceType = "project"
```

- [ ] **Step 2: Add ProjectID to Resource struct**

In `portal/internal/biz/resource.go`, add after line 68 (`HomeOrgID` field):

```go
	ProjectID    string       `json:"project_id,omitempty"`
```

The full struct becomes:

```go
type Resource struct {
	ID           string       `json:"id"`
	Type         ResourceType `json:"type"`
	Name         string       `json:"name"`
	OwnerUserID  string       `json:"owner_user_id"`
	Visibility   Visibility   `json:"visibility"`
	HomeOrgID    string       `json:"home_org_id"`
	ProjectID    string       `json:"project_id,omitempty"`
	BoundAgentID string       `json:"bound_agent_id,omitempty"`
	PayloadRef   string       `json:"payload_ref"`
}
```

- [ ] **Step 3: Build check**

```bash
cd portal && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/biz/resource.go
git commit -m "feat(acl): add ResourceTypeProject and ProjectID field

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Data layer — update resource mappers + add ListByProject, UpdateProjectID

**Files:**
- Modify: `portal/internal/biz/identity.go:67-78`
- Modify: `portal/internal/data/resource_mysql.go:31-88`

- [ ] **Step 1: Add ListByProject + UpdateProjectID to ResourceRepo interface**

In `portal/internal/biz/identity.go`, inside `ResourceRepo` interface, after `ListGrantsByResourceIDs`:

```go
	ListByProject(ctx context.Context, projectID string) ([]*Resource, error)
	UpdateProjectID(ctx context.Context, resourceID, projectID string) error
```

- [ ] **Step 2: Update resourceModelToBiz to include ProjectID**

In `portal/internal/data/resource_mysql.go`, add `ProjectID` to the mapper:

```go
func resourceModelToBiz(m *model.Resource) *biz.Resource {
	return &biz.Resource{
		ID:           m.ID,
		Type:         biz.ResourceType(m.Type),
		Name:         m.Name,
		OwnerUserID:  m.OwnerUserID,
		Visibility:   biz.Visibility(m.Visibility),
		HomeOrgID:    m.HomeOrgID,
		ProjectID:    m.ProjectID,
		BoundAgentID: m.BoundAgentID,
		PayloadRef:   m.PayloadRef,
	}
}
```

- [ ] **Step 3: Update CreateResource to persist ProjectID**

In `CreateResource`, add the `ProjectID` field:

```go
func (r *resourceRepo) CreateResource(ctx context.Context, resource *biz.Resource) (*biz.Resource, error) {
	m := &model.Resource{
		ID:           resource.ID,
		Type:         string(resource.Type),
		Name:         resource.Name,
		OwnerUserID:  resource.OwnerUserID,
		Visibility:   string(resource.Visibility),
		HomeOrgID:    resource.HomeOrgID,
		ProjectID:    resource.ProjectID,
		BoundAgentID: resource.BoundAgentID,
		PayloadRef:   resource.PayloadRef,
	}
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return resourceModelToBiz(m), nil
}
```

- [ ] **Step 4: Update UpdateResource to include project_id**

In `UpdateResource`, add `project_id` to the updates map:

```go
func (r *resourceRepo) UpdateResource(ctx context.Context, resource *biz.Resource) error {
	res := r.db.WithContext(ctx).Model(&model.Resource{}).Where("id = ?", resource.ID).Updates(map[string]any{
		"name":           resource.Name,
		"owner_user_id":  resource.OwnerUserID,
		"visibility":     string(resource.Visibility),
		"home_org_id":    resource.HomeOrgID,
		"project_id":     resource.ProjectID,
		"bound_agent_id": resource.BoundAgentID,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 5: Add ListByProject implementation**

After `ListAllByType`, add:

```go
func (r *resourceRepo) ListByProject(ctx context.Context, projectID string) ([]*biz.Resource, error) {
	var rows []model.Resource
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	resources := make([]*biz.Resource, len(rows))
	for i := range rows {
		resources[i] = resourceModelToBiz(&rows[i])
	}
	return resources, nil
}
```

- [ ] **Step 6: Add UpdateProjectID implementation**

After `ListByProject`, add:

```go
func (r *resourceRepo) UpdateProjectID(ctx context.Context, resourceID, projectID string) error {
	res := r.db.WithContext(ctx).Model(&model.Resource{}).Where("id = ?", resourceID).Update("project_id", projectID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 7: Build check**

```bash
cd portal && go build ./...
```

Expected: compiles. If test fakes need `ListByProject` + `UpdateProjectID` stubs, add panic stubs.

- [ ] **Step 8: Commit**

```bash
git add portal/internal/biz/identity.go portal/internal/data/resource_mysql.go
git commit -m "feat(acl): add ListByProject, UpdateProjectID to ResourceRepo

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 4: ACL — project permission inheritance in VisiblePayloadRefs + AccessChecker

**Files:**
- Modify: `portal/internal/biz/resource_list_acl.go:7-38`
- Modify: `portal/internal/biz/resource.go:131-148`

- [ ] **Step 1: Update VisiblePayloadRefs to include project grants**

Replace the entire function body in `resource_list_acl.go`:

```go
func VisiblePayloadRefs(ctx context.Context, repo ResourceRepo, callerUserID string, resourceType ResourceType, need Perm) (map[string]struct{}, error) {
	resources, err := repo.ListAllByType(ctx, resourceType)
	if err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return map[string]struct{}{}, nil
	}

	orgIDs, err := repo.UserOrgIDs(ctx, callerUserID)
	if err != nil {
		return nil, err
	}

	// Collect resource IDs and their project IDs for a single batch grants query.
	allIDs := make([]string, 0, len(resources)*2)
	for _, r := range resources {
		allIDs = append(allIDs, r.ID)
		if r.ProjectID != "" {
			allIDs = append(allIDs, r.ProjectID)
		}
	}
	grantsByID, err := repo.ListGrantsByResourceIDs(ctx, allIDs)
	if err != nil {
		return nil, err
	}

	visible := make(map[string]struct{}, len(resources))
	for _, r := range resources {
		combined := grantsByID[r.ID]
		if r.ProjectID != "" {
			combined = append(combined, grantsByID[r.ProjectID]...)
		}
		have := EvaluatePerm(r, orgIDs, combined, callerUserID, "")
		if PermAtLeast(have, need) {
			visible[r.PayloadRef] = struct{}{}
		}
	}
	return visible, nil
}
```

- [ ] **Step 2: Update AccessChecker.EffectivePerm to include project grants**

In `portal/internal/biz/resource.go`, replace the `EffectivePerm` method:

```go
func (c *AccessChecker) EffectivePerm(ctx context.Context, callerUserID, resourceID, agentIDForBound string) (Perm, error) {
	res, err := c.r.GetResource(ctx, resourceID)
	if err != nil {
		return "", err
	}

	orgIDs, err := c.r.UserOrgIDs(ctx, callerUserID)
	if err != nil {
		return "", err
	}

	grants, err := c.r.ListGrants(ctx, resourceID)
	if err != nil {
		return "", err
	}

	// Merge project grants for permission inheritance.
	if res.ProjectID != "" {
		projectGrants, err := c.r.ListGrants(ctx, res.ProjectID)
		if err == nil {
			grants = append(grants, projectGrants...)
		}
	}

	return EvaluatePerm(res, orgIDs, grants, callerUserID, agentIDForBound), nil
}
```

- [ ] **Step 3: Build check**

```bash
cd portal && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/biz/resource_list_acl.go portal/internal/biz/resource.go
git commit -m "feat(acl): project permission inheritance in VisiblePayloadRefs and AccessChecker

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 5: ProjectUsecase — CRUD + membership methods

**Files:**
- Modify: `portal/internal/biz/acl_api.go` — add `ProjectUsecase` struct + methods after existing `ACLAPIUsecase`

- [ ] **Step 1: Add ProjectUsecase struct**

At the end of `acl_api.go`, add:

```go
// ProjectUsecase handles project CRUD and resource-in-project membership.
type ProjectUsecase struct {
	resources ResourceRepo
	access    *AccessChecker
}

func NewProjectUsecase(resources ResourceRepo, access *AccessChecker) *ProjectUsecase {
	return &ProjectUsecase{resources: resources, access: access}
}

// Create creates a project resource owned by the caller.
func (uc *ProjectUsecase) Create(ctx context.Context, name, description string) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "name is required")
	}

	visibility := VisibilityPrivate
	homeOrgID := ""
	if orgID, ok := OrgID(ctx); ok {
		visibility = VisibilityOrg
		homeOrgID = orgID
	}

	resource := &Resource{
		Type:        ResourceTypeProject,
		Name:        name,
		OwnerUserID: caller,
		Visibility:  visibility,
		HomeOrgID:   homeOrgID,
	}
	created, err := uc.resources.CreateResource(ctx, resource)
	if err != nil {
		return nil, err
	}
	// Project's payload_ref is its own ID (self-referential).
	if created.PayloadRef == "" {
		created.PayloadRef = created.ID
		_ = uc.resources.UpdateResource(ctx, created)
	}
	return created, nil
}

// Get returns a project by ID. Caller must have PermView.
func (uc *ProjectUsecase) Get(ctx context.Context, id string) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	canView, err := uc.access.Can(ctx, caller, id, PermView, "")
	if err != nil || !canView {
		return nil, ErrGrantNotFound
	}
	return uc.resources.GetResource(ctx, id)
}

// Update updates a project's name. Caller must have PermEdit.
func (uc *ProjectUsecase) Update(ctx context.Context, id, name string) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "name is required")
	}
	canEdit, err := uc.access.Can(ctx, caller, id, PermEdit, "")
	if err != nil || !canEdit {
		return nil, ErrForbiddenPerm
	}
	res, err := uc.resources.GetResource(ctx, id)
	if err != nil {
		return nil, ErrGrantNotFound
	}
	res.Name = name
	if err := uc.resources.UpdateResource(ctx, res); err != nil {
		return nil, err
	}
	return res, nil
}

// Delete deletes a project. Caller must have PermAdmin.
func (uc *ProjectUsecase) Delete(ctx context.Context, id string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	canAdmin, err := uc.access.Can(ctx, caller, id, PermAdmin, "")
	if err != nil || !canAdmin {
		return ErrForbiddenPerm
	}
	return uc.resources.DeleteResource(ctx, id)
}

// AddToProject sets a resource's project_id. Caller must have PermEdit on the child resource.
// The child resource must share the same HomeOrgID as the project.
func (uc *ProjectUsecase) AddToProject(ctx context.Context, resourceID, projectID string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if resourceID == "" || projectID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "resource_id and project_id are required")
	}
	canEdit, err := uc.access.Can(ctx, caller, resourceID, PermEdit, "")
	if err != nil || !canEdit {
		return ErrForbiddenPerm
	}
	child, err := uc.resources.GetResource(ctx, resourceID)
	if err != nil {
		return ErrGrantNotFound
	}
	project, err := uc.resources.GetResource(ctx, projectID)
	if err != nil {
		return ErrGrantNotFound
	}
	if project.Type != ResourceTypeProject {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "target is not a project")
	}
	if child.HomeOrgID != project.HomeOrgID {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "resource and project must be in the same org")
	}
	return uc.resources.UpdateProjectID(ctx, resourceID, projectID)
}

// RemoveFromProject clears a resource's project_id. Caller must have PermEdit on the resource.
func (uc *ProjectUsecase) RemoveFromProject(ctx context.Context, resourceID string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if resourceID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "resource_id is required")
	}
	canEdit, err := uc.access.Can(ctx, caller, resourceID, PermEdit, "")
	if err != nil || !canEdit {
		return ErrForbiddenPerm
	}
	return uc.resources.UpdateProjectID(ctx, resourceID, "")
}

// ListProjectResources returns all resources belonging to a project. Caller must have PermView on the project.
func (uc *ProjectUsecase) ListProjectResources(ctx context.Context, projectID string) ([]*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	canView, err := uc.access.Can(ctx, caller, projectID, PermView, "")
	if err != nil || !canView {
		return nil, ErrForbiddenPerm
	}
	return uc.resources.ListByProject(ctx, projectID)
}
```

After the `ProjectUsecase` block, add a `ListVisibleResources` method to the existing `ACLAPIUsecase` (this is needed by `ListProjectsHandler`):

```go
// ListVisibleResources returns resources of a given type visible to the caller at the given permission level.
// Uses VisiblePayloadRefs for batch ACL evaluation, then loads full resource objects by ID.
func (uc *ACLAPIUsecase) ListVisibleResources(ctx context.Context, resourceType ResourceType, need Perm) ([]*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := VisiblePayloadRefs(ctx, uc.resources, caller, resourceType, need)
	if err != nil {
		return nil, err
	}
	resources := make([]*Resource, 0, len(allowed))
	for id := range allowed {
		res, err := uc.resources.GetResource(ctx, id)
		if err == nil {
			resources = append(resources, res)
		}
	}
	return resources, nil
}
```

- [ ] **Step 2: Build check**

```bash
cd portal && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/biz/acl_api.go
git commit -m "feat(acl): add ProjectUsecase with CRUD and resource membership

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Project HTTP handlers

**Files:**
- Modify: `portal/internal/server/acl_api.go` — add project handlers at end of file

- [ ] **Step 1: Add project handler structs and functions**

At the end of `acl_api.go`, add:

```go
// ── Project handlers ──

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func CreateProjectHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createProjectRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.Create(c, body.Name, body.Description)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func GetProjectHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.Get(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

type updateProjectRequest struct {
	Name string `json:"name"`
}

func UpdateProjectHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		var body updateProjectRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.Update(c, id, body.Name)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func DeleteProjectHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.Delete(c, id)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

type setProjectRequest struct {
	ProjectID string `json:"project_id"`
}

func AddResourceToProjectHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		resourceID := ctx.Vars().Get("id")
		var body setProjectRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.AddToProject(c, resourceID, body.ProjectID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

func RemoveResourceFromProjectHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		resourceID := ctx.Vars().Get("id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.RemoveFromProject(c, resourceID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

func ListProjectResourcesHandler(uc *biz.ProjectUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		projectID := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.ListProjectResources(c, projectID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"resources": out})
	}
}

// ListProjectsHandler uses ACLAPIUsecase (not ProjectUsecase) because listing
// projects goes through VisiblePayloadRefs for batch ACL evaluation.
func ListProjectsHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.ListVisibleResources(c, biz.ResourceTypeProject, biz.PermView)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"projects": out})
	}
}
```

- [ ] **Step 2: Build check**

```bash
cd portal && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/server/acl_api.go
git commit -m "feat(acl): add project HTTP handlers

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 7: Project routes + wire DI

**Files:**
- Modify: `portal/internal/server/http.go` — add routes + new constructor param
- Modify: `portal/cmd/backend/wire.go` — add ProjectUsecase to wire
- Modify: `portal/internal/biz/biz.go` — add NewProjectUsecase to ProviderSet

- [ ] **Step 1: Update NewHTTPServer signature to accept ProjectUsecase**

In `portal/internal/server/http.go`, add `projectUC *biz.ProjectUsecase` to function params (after `agentUC` in the signature):

```go
func NewHTTPServer(c *conf.Server, tool *service.ToolService, agent *service.AgentService, chat *service.ChatService, channelSvc *service.ChannelService, cronSvc *cron.CronService, channelUC *biz.ChannelUsecase, identityRepo biz.IdentityRepo, aclAPI *biz.ACLAPIUsecase, authUC *biz.AuthUsecase, mcpServer *service.McpServerService, proxy *service.ProxyService, runtimeSvc *runtime.Service, pinger DBPinger, agentUC *biz.AgentUsecase, projectUC *biz.ProjectUsecase, codeRoots []string, logger log.Logger) *httptransport.Server {
```

- [ ] **Step 2: Add project routes**

In `http.go`, after `r.GET("/api/v1/resources/by-payload/{type}/{ref}", ...)`, add:

```go
	// Project CRUD
	r.POST("/api/v1/projects", CreateProjectHandler(projectUC))
	r.GET("/api/v1/projects", ListProjectsHandler(aclAPI))
	r.GET("/api/v1/projects/{id}", GetProjectHandler(projectUC))
	r.PATCH("/api/v1/projects/{id}", UpdateProjectHandler(projectUC))
	r.DELETE("/api/v1/projects/{id}", DeleteProjectHandler(projectUC))

	// Resource-to-project membership
	r.PATCH("/api/v1/resources/{id}/project", AddResourceToProjectHandler(projectUC))
	r.DELETE("/api/v1/resources/{id}/project", RemoveResourceFromProjectHandler(projectUC))

	// Project resources listing
	r.GET("/api/v1/projects/{id}/resources", ListProjectResourcesHandler(projectUC))
```

`ListProjectsHandler` (for `GET /api/v1/projects`) is in Task 6 — it depends on `*biz.ACLAPIUsecase` (not `*biz.ProjectUsecase`) because it routes through `VisiblePayloadRefs` for batch ACL evaluation.

- [ ] **Step 3: Update wire DI**

In `portal/internal/biz/biz.go`, add `NewProjectUsecase` to the `ProviderSet`:

```go
var ProviderSet = wire.NewSet(NewToolUsecase, NewMcpServerUsecase, NewProxyUsecase, NewAgentUsecase, NewSkillResourceUsecase, NewChatUsecase, NewChannelUsecase, NewChannelPeerUsecase, NewCronUsecase, NewProjectUsecase, ProvideAccessChecker, ProvideACLAPIUsecase, ProvideAuthUsecase)
```

Wire auto-resolves the dependency: `NewProjectUsecase(ResourceRepo, *AccessChecker)` — both already in the ProviderSet.

- [ ] **Step 4: Build check**

```bash
cd portal && go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add portal/internal/server/http.go portal/internal/server/acl_api.go portal/internal/biz/acl_api.go portal/internal/biz/biz.go
git commit -m "feat(acl): project routes, listing handler, and wire DI

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 8: Backend build + test

**Files:** none (verification only)

- [ ] **Step 1: Build portal**

```bash
cd portal && go build ./... 2>&1
```

Expected: compiles with no errors.

- [ ] **Step 2: Run existing tests**

```bash
cd portal && go test ./internal/biz/ -v -run 'TestGrant|TestCreateOrg|TestAddOrgMember' 2>&1 | tail -20
```

Expected: existing tests still pass.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "chore: backend build and test verification for project feature

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 9: Frontend — resource API client additions

**Files:**
- Modify: `web/src/api/resource.ts`

- [ ] **Step 1: Add project API methods**

At the end of `resource.ts`, after `deleteGrant`, add:

```ts
// ── Project API ──

export interface ProjectItem {
  id: string
  type: string
  name: string
  visibility: string
  owner_user_id: string
  home_org_id: string
  payload_ref: string
}

export async function listProjects(): Promise<ProjectItem[]> {
  const res = await fetch(`${API_BASE}/projects`, { headers: authHeaders() })
  if (!res.ok) throw new Error(`list projects failed: ${res.status}`)
  const data = await res.json()
  return data.projects ?? []
}

export async function getProject(id: string): Promise<ProjectItem> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(id)}`, { headers: authHeaders() })
  if (!res.ok) throw new Error(res.status === 404 ? 'not_found' : `get project failed: ${res.status}`)
  return res.json()
}

export async function createProject(name: string): Promise<ProjectItem> {
  const res = await fetch(`${API_BASE}/projects`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) throw new Error(`create project failed: ${res.status}`)
  return res.json()
}

export async function updateProject(id: string, name: string): Promise<ProjectItem> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) throw new Error(`update project failed: ${res.status}`)
  return res.json()
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`delete project failed: ${res.status}`)
}

export async function addToProject(resourceId: string, projectId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/project`, {
    method: 'PATCH',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ project_id: projectId }),
  })
  if (!res.ok) throw new Error(`add to project failed: ${res.status}`)
}

export async function removeFromProject(resourceId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/project`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`remove from project failed: ${res.status}`)
}

export async function listProjectResources(projectId: string): Promise<ResourceInfo[]> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(projectId)}/resources`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`list project resources failed: ${res.status}`)
  const data = await res.json()
  return data.resources ?? []
}
```

- [ ] **Step 2: TypeScript build check**

```bash
cd web && npx tsc --noEmit src/api/resource.ts 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/api/resource.ts
git commit -m "feat(web): add project API methods to resource client

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 10: Frontend — ProjectListPage

**Files:**
- Create: `web/src/pages/ProjectListPage.tsx`

- [ ] **Step 1: Create ProjectListPage**

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listProjects, createProject, type ProjectItem } from '../api/resource'

export default function ProjectListPage() {
  const [projects, setProjects] = useState<ProjectItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = () => {
    setLoading(true)
    setError('')
    listProjects()
      .then(setProjects)
      .catch((e) => setError(e instanceof Error ? e.message : '加载失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleCreate = async () => {
    const name = window.prompt('项目名称')
    if (!name || !name.trim()) return
    setError('')
    try {
      const p = await createProject(name.trim())
      setProjects((prev) => [p, ...prev])
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
    </div>
  )
  if (error) return <div className="error">加载失败：{error}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>项目</h1>
            <span className="page-count">{projects.length}</span>
          </div>
          <p className="page-sub">管理项目，对资源进行分组和授权。</p>
        </div>
        <button type="button" className="btn" onClick={handleCreate}>+ 创建项目</button>
      </div>
      {projects.length === 0 ? (
        <div className="section-card empty-state">
          <p>还没有项目。</p>
          <button type="button" className="btn" onClick={handleCreate}>+ 创建项目</button>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>可见性</th>
                <th>Owner</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => (
                <tr key={p.id}>
                  <td><Link to={`/projects/${p.id}`}>{p.name}</Link></td>
                  <td><span className="badge">{p.visibility}</span></td>
                  <td><code>{p.owner_user_id}</code></td>
                  <td>
                    <Link to={`/projects/${p.id}`} className="btn btn-secondary btn-sm">查看</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: TypeScript build check**

```bash
cd web && npx tsc --noEmit 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/ProjectListPage.tsx
git commit -m "feat(web): add ProjectListPage

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 11: Frontend — ProjectDetailPage

**Files:**
- Create: `web/src/pages/ProjectDetailPage.tsx`

- [ ] **Step 1: Create ProjectDetailPage**

```tsx
import { type FormEvent, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  getProject, updateProject, deleteProject,
  listProjectResources, removeFromProject, addToProject,
  listProjects, type ProjectItem,
  type ResourceInfo,
} from '../api/resource'
import ResourceGrantPanel from '../components/ResourceGrantPanel'

export default function ProjectDetailPage() {
  const { id = '' } = useParams()
  const [project, setProject] = useState<ProjectItem | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editingName, setEditingName] = useState(false)
  const [newName, setNewName] = useState('')
  const [saving, setSaving] = useState(false)

  // Resources in this project
  const [resources, setResources] = useState<ResourceInfo[]>([])
  const [resLoading, setResLoading] = useState(false)

  // Add resource to project
  const [addResId, setAddResId] = useState('')
  const [adding, setAdding] = useState(false)

  // Remove resource
  const [removingId, setRemovingId] = useState<string | null>(null)

  const load = async () => {
    if (!id) return
    setLoading(true)
    setError('')
    try {
      const p = await getProject(id)
      setProject(p)
      setNewName(p.name)
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
      setProject(null)
    } finally {
      setLoading(false)
    }
  }

  const loadResources = async () => {
    if (!id) return
    setResLoading(true)
    try {
      const items = await listProjectResources(id)
      setResources(items)
    } catch {
      setResources([])
    } finally {
      setResLoading(false)
    }
  }

  useEffect(() => { load(); loadResources() }, [id])

  const handleSaveName = async (e: FormEvent) => {
    e.preventDefault()
    if (!id || !newName.trim() || newName.trim() === project?.name) {
      setEditingName(false)
      return
    }
    setSaving(true)
    try {
      const updated = await updateProject(id, newName.trim())
      setProject(updated)
      setEditingName(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!id) return
    try {
      await deleteProject(id)
      window.location.href = '/projects'
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  const handleAddResource = async (e: FormEvent) => {
    e.preventDefault()
    if (!id || !addResId.trim()) return
    setAdding(true)
    setError('')
    try {
      await addToProject(addResId.trim(), id)
      setAddResId('')
      loadResources()
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加失败')
    } finally {
      setAdding(false)
    }
  }

  const handleRemoveResource = async (resourceId: string) => {
    setRemovingId(resourceId)
    setError('')
    try {
      await removeFromProject(resourceId)
      setResources((prev) => prev.filter((r) => r.id !== resourceId))
    } catch (err) {
      setError(err instanceof Error ? err.message : '移除失败')
    } finally {
      setRemovingId(null)
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
    </div>
  )
  if (!project) return (
    <div>
      <div className="error">未找到该项目。</div>
      <Link to="/projects" className="btn btn-secondary" style={{ marginTop: '1rem' }}>返回项目列表</Link>
    </div>
  )

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            {editingName ? (
              <form onSubmit={handleSaveName} style={{ display: 'inline-flex', gap: '0.5rem', alignItems: 'center' }}>
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  style={{ fontSize: '1.5rem', fontWeight: 700 }}
                  autoFocus
                />
                <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>保存</button>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setEditingName(false)}>取消</button>
              </form>
            ) : (
              <h1 style={{ cursor: 'pointer' }} onClick={() => setEditingName(true)} title="点击编辑名称">
                {project.name}
              </h1>
            )}
          </div>
          <p className="page-sub">
            {project.visibility === 'org' ? 'Org 共享' : 'Private'} ·{' '}
            Owner: <code>{project.owner_user_id}</code>
            {project.home_org_id ? <> · Org: <code>{project.home_org_id}</code></> : null}
          </p>
        </div>
        <div className="actions">
          <Link to="/projects" className="btn btn-secondary">返回列表</Link>
          <button type="button" className="btn btn-danger" onClick={handleDelete}>删除项目</button>
        </div>
      </div>

      {error && <div className="error" style={{ marginBottom: '1rem' }}>{error}</div>}

      <ResourceGrantPanel resourceType="project" payloadRef={project.id} />

      <section className="section">
        <h2 className="section-title">项目资源</h2>

        <div className="section-card" style={{ padding: '1.25rem', marginBottom: '1rem' }}>
          <form onSubmit={handleAddResource}>
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div className="form-group" style={{ margin: 0, flex: '1 1 250px' }}>
                <label style={{ fontSize: '0.8rem' }}>Resource ID</label>
                <input
                  value={addResId}
                  onChange={(e) => setAddResId(e.target.value)}
                  placeholder="输入资源 ID (resource 表中的 id)"
                  disabled={adding}
                />
              </div>
              <button type="submit" className="btn btn-primary btn-sm" disabled={adding || !addResId.trim()}>
                {adding ? '添加中…' : '加入项目'}
              </button>
            </div>
          </form>
        </div>

        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>类型</th>
                <th>可见性</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {resLoading ? (
                <tr><td colSpan={4} className="muted">加载中…</td></tr>
              ) : resources.length === 0 ? (
                <tr><td colSpan={4} className="muted">暂无资源</td></tr>
              ) : (
                resources.map((r) => {
                  const isRemoving = removingId === r.id
                  return (
                    <tr key={r.id}>
                      <td>{r.name}</td>
                      <td><span className="badge">{r.type}</span></td>
                      <td><span className="badge">{r.visibility}</span></td>
                      <td>
                        <button
                          type="button"
                          className="btn btn-danger btn-sm"
                          disabled={isRemoving}
                          onClick={() => handleRemoveResource(r.id)}
                        >
                          {isRemoving ? '移除中…' : '移出项目'}
                        </button>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
```

- [ ] **Step 2: TypeScript build check**

```bash
cd web && npx tsc --noEmit 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/ProjectDetailPage.tsx
git commit -m "feat(web): add ProjectDetailPage with resource membership management

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 12: Frontend — App.tsx routes + sidebar + breadcrumb

**Files:**
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Add imports**

After `import CronTaskDetail from './pages/CronTaskDetail'`, add:

```tsx
import ProjectListPage from './pages/ProjectListPage'
import ProjectDetailPage from './pages/ProjectDetailPage'
```

- [ ] **Step 2: Add sidebar nav item**

After the orgs NavLink (line 191-193), add:

```tsx
            <NavLink to="/projects" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">📁</span>
              项目
            </NavLink>
```

- [ ] **Step 3: Add breadcrumb case**

In the `Breadcrumb` function, after the orgs block (lines 84-86), add:

```tsx
  } else if (segments[0] === 'projects') {
    if (segments[1]) { current = '项目详情'; icon = '📁' }
    else { current = '项目管理'; icon = '📁' }
```

- [ ] **Step 4: Add routes**

After `<Route path="/orgs/:id" element={<OrgDetailPage />} />`, add:

```tsx
            <Route path="/projects" element={<ProjectListPage />} />
            <Route path="/projects/:id" element={<ProjectDetailPage />} />
```

- [ ] **Step 5: TypeScript build check**

```bash
cd web && npx tsc --noEmit 2>&1 | tail -10
```

- [ ] **Step 6: Commit**

```bash
git add web/src/App.tsx web/src/pages/ProjectListPage.tsx
git commit -m "feat(web): add project routes, sidebar, breadcrumb to App

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 13: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Portal build + test**

```bash
cd portal && go build ./... && go test ./internal/biz/ -v -run 'TestGrant|TestCreateOrg|TestAddOrgMember' 2>&1 | tail -20
```

Expected: build succeeds, existing tests pass.

- [ ] **Step 2: Web build**

```bash
cd web && npm run build 2>&1 | tail -10
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "chore: project resource grouping regression pass

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Verification

After all tasks complete:
1. **Run migration** `portal/scripts/migration_project_id.sql` on dev DB
2. **Create project** via `POST /api/v1/projects` `{"name":"Test Project"}`
3. **List projects** via `GET /api/v1/projects` — verify it appears
4. **Add resource to project** via `PATCH /api/v1/resources/{id}/project` `{"project_id":"..."}` 
5. **List project resources** via `GET /api/v1/projects/{id}/resources` — verify resource appears
6. **Grant permission on project** via `POST /api/v1/resources/{project_id}/grants`
7. **Verify inheritance** — a different user with grant on project can see the child resource via `GET /api/v1/resources/by-payload/{type}/{ref}`
8. **UI** — visit `/projects`, create a project, click into it, verify ResourceGrantPanel + resource table