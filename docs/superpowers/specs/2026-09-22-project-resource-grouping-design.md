# Project-Based Resource Grouping Design

**Date:** 2026-09-22
**Status:** Design approved — pending spec review

## Motivation

The current ACL system has 4-tier resource-level permissions (view/use/edit/admin) with explicit grants per resource. When an org has many resources, managing permissions individually is repetitive. We need a grouping mechanism so that permissions granted on a group cascade to its members.

## Design

Project is introduced as a new `ResourceType` ("project"), stored in the existing `resources` table. Resources get an optional `ProjectID` foreign key. Permissions on a project inherit down to its member resources via `max(child_grants, project_grants)` — no deny/override semantics.

### Data Model

**resources table — new column:**

```sql
ALTER TABLE resources ADD COLUMN project_id VARCHAR(36) DEFAULT '' NOT NULL;
CREATE INDEX idx_resources_project_id ON resources(project_id);
```

`DEFAULT ''` means existing resources are not in any project. No new tables.

**Project as a Resource record:**

| Field | Value |
|-------|-------|
| `type` | `"project"` |
| `payload_ref` | same as `id` (self-referential) |
| `owner_user_id` | creator |
| `visibility` | `private` (default, no X-Org-Id) or `org` (with X-Org-Id) |
| `home_org_id` | set from session's X-Org-Id |
| `project_id` | empty (projects cannot nest) |

**Child resources:**

| Field | Meaning |
|-------|---------|
| `project_id` | empty or a project resource ID |
| constraint | `child.home_org_id == project.home_org_id` |

### Relationship Model

```
User ──own──▶ Project (OwnerUserID)
User ──grant──▶ Project (ResourceGrant: view/use/edit/admin)

Org ──contain──▶ Project (HomeOrgID)
Org ──contain──▶ User (org member)

Project ──contain──▶ Resource (agent/tool/mcp_server/channel/cron)
                     constraint: same HomeOrgID
```

- One resource belongs to at most one project
- Projects cannot nest
- Cross-org project membership is rejected

### Permission Inheritance

```go
// In VisiblePayloadRefs (resource_list_acl.go) — batch grants now include project grants:

func VisiblePayloadRefs(ctx, repo, callerUserID, resourceType, need Perm) (map[string]struct{}, error) {
    resources := repo.ListAllByType(ctx, resourceType)
    orgIDs := repo.UserOrgIDs(ctx, callerUserID)
    ownedOrgIDs := repo.UserOwnedOrgIDs(ctx, callerUserID)

    // Collect resource IDs + project IDs for batch grant loading
    allIDs := make([]string, 0, len(resources))
    for _, r := range resources {
        allIDs = append(allIDs, r.ID)
        if r.ProjectID != "" {
            allIDs = append(allIDs, r.ProjectID)
        }
    }
    grantsByID := repo.ListGrantsByResourceIDs(allIDs)

    visible := make(map[string]struct{}, len(resources))
    for _, r := range resources {
        combined := grantsByID[r.ID]
        if r.ProjectID != "" {
            combined = append(combined, grantsByID[r.ProjectID]...)
        }
        have := EvaluatePerm(r, orgIDs, ownedOrgIDs, combined, caller, "")
        if PermAtLeast(have, need) {
            visible[r.PayloadRef] = struct{}{}
        }
    }
    return visible, nil
}
```

Key properties:
- `EvaluatePerm` itself is unchanged — it operates on a flat grant list
- Inheritance happens at the caller level (VisiblePayloadRefs / AccessChecker.EffectivePerm), not inside EvaluatePerm
- One batch query still covers all grants; no extra DB round-trips
- `project_id = ""` resources behave identically to before

### API Design

**Project CRUD — follows standard resource pattern:**

| Operation | Route | Permission |
|-----------|-------|------------|
| Create | `POST /api/v1/projects` | authenticated |
| List | `GET /api/v1/projects` | visible projects (via VisiblePayloadRefs) |
| Get | `GET /api/v1/projects/{id}` | PermView on project |
| Update | `PATCH /api/v1/projects/{id}` | PermEdit on project |
| Delete | `DELETE /api/v1/projects/{id}` | PermAdmin on project |

**Resource-to-project membership:**

| Operation | Route | Permission |
|-----------|-------|------------|
| Add to project | `PATCH /api/v1/resources/{id}/project` body: `{"project_id":"..."}` | PermEdit on child resource |
| Remove from project | `DELETE /api/v1/resources/{id}/project` | PermEdit on child resource |

**List project resources:**

| Operation | Route | Permission |
|-----------|-------|------------|
| List members | `GET /api/v1/projects/{id}/resources` | PermView on project |

Handlers are hand-written in `portal/internal/server/acl_api.go`, registered as routes in `http.go`. No protobuf changes.

### Frontend Design

| Page | Route | Content |
|------|-------|---------|
| ProjectListPage | `/projects` | visible projects list (reuses existing list pattern) |
| ProjectDetailPage | `/projects/{id}` | basic info + ResourceGrantPanel + child resource table + add/remove controls |
| ProjectForm | inline/modal | name + visibility (reuses NewOwnedResource pattern) |

**Project selector in resource forms** (AgentForm, ToolForm, McpServerForm, ChannelForm, CronTaskForm):
- Dropdown listing user's accessible projects, default "none"
- On create/edit, `project_id` is sent in the resource payload
- Backend validates same-org constraint

**Child resource table** (ProjectDetailPage):
- Lists all resources whose `project_id` matches the current project
- Each row: name, type, visibility, "remove from project" button

**Navigation:** Sidebar gets "Projects" entry between existing items, routing to `/projects`.

**API client:** Extends `web/src/api/resource.ts` (no new file). Methods: `listProjects`, `getProject`, `createProject`, `updateProject`, `deleteProject`, `addToProject`, `removeFromProject`, `listProjectResources`.

### Migration

1. **DB migration**: `ALTER TABLE ADD COLUMN project_id` — zero-impact, existing resources get `""`
2. **Backend deploy**: new routes + `VisiblePayloadRefs` change, all resources have `project_id = ""` so no behavior change
3. **Frontend deploy**: new pages and selectors
4. **Adoption**: admins create projects, manually move resources in

No backfill script needed. No breaking changes. Each step is independently deployable and rollback-able.

### Dependency Injection

- `ProjectUsecase` depends on `ResourceRepo` + `AccessChecker` (no new data repo)
- Project handlers are hand-written functions (not a protobuf service), registered in `NewHTTPServer`
- Wire set updated in `portal/cmd/wire.go`

## Rejected Alternatives

- **Separate `projects` table**: Would duplicate visibility/owner logic already in resources. Project as ResourceType reuses all existing ACL infrastructure.
- **Project as org alias**: Would break when one org needs multiple projects. Org is an identity boundary, project is a resource grouping — distinct concepts.
- **Deny/override inheritance**: Adds complexity without clear use case. Cumulative `max()` is simpler and sufficient.