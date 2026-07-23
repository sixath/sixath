# 用户体系与统一 Resource ACL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 portal 落地 User/Org、统一 Resource+Grant、AccessChecker、默认 per-agent workspace、会话按 user 隔离，以及 Org 共享 Skill 路径合并进 Index（不含 Public Hub）。

**Architecture:** 身份与 ACL 全在 portal：Bearer → `caller_user_id` → `AccessChecker`；`agents`/`tools` 保留为 payload，`resources` 为归属权威；创建 Agent 默认 `{data_root}/agents/{agent_id}/`；Chat 注入 `user_id`；`BuildSkillsIndex` 合并 workspace skills 与可见共享 skill 根。Framework 不实现租户 ACL。

**Tech Stack:** Go；Kratos HTTP middleware；GORM/MySQL；`github.com/sixath/framework/skills`（仅消费路径列表）。

**Spec:** `docs/superpowers/specs/2026-07-23-user-resource-acl-design.md`

> **Git：** 设计/计划在 monorepo 根；portal 代码在 `portal/` 嵌套仓库。Commit 步骤按改动所在仓库执行。若用户未要求提交，可跳过 Commit 步。  
> **非目标：** Hub、`visibility=public` 真正启用、搬迁旧 workspace、framework `auth.Checker` 打通。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `portal/internal/biz/identity.go` | User/Org/OrgMember/Token 实体与 repo 接口 |
| Create `portal/internal/biz/resource.go` | Resource/Grant 实体、perm 层级、`AccessChecker` |
| Create `portal/internal/biz/skill_resource.go` | Org 共享 Skill 登记 helper |
| Create `portal/internal/biz/resource_acl_test.go` | ACL 矩阵单测（假 repo） |
| Create `portal/internal/biz/ctx_caller.go` | `context` 读写 `caller_user_id` / `org_id` |
| Create `portal/internal/data/model/identity.go` | GORM：users/orgs/org_members/`user_tokens` |
| Create `portal/internal/data/model/resource.go` | GORM：resources/resource_grants |
| Modify `portal/internal/data/model/chat.go` | `ChatSession.UserID` |
| Create `portal/internal/data/identity_mysql.go` | 身份 + token repo |
| Create `portal/internal/data/resource_mysql.go` | Resource/Grant repo |
| Modify `portal/internal/data/data.go` | AutoMigrate + ProviderSet |
| Create `portal/internal/data/bootstrap.go` | 默认 org/user/token；回填 Resource |
| Modify `portal/internal/service/growth_worker.go`（及 cron 入口） | 内部 job ctx 注入 service principal |
| Create `portal/internal/server/middleware/auth.go` | Bearer + `X-Org-Id` |
| Modify `portal/internal/server/http.go` | 挂载 Auth middleware |
| Modify `portal/internal/conf/conf.proto` (+ pb) | `data.data_root`、`auth.bootstrap_*` |
| Modify `portal/internal/biz/agent.go` / `agent_usecase.go` | ACL + 默认同步 Resource + 默认 workspace |
| Modify `portal/internal/biz/tool.go` / usecase | 同上 |
| Modify `portal/internal/biz/chat.go` + data chat | `user_id` 过滤 |
| Modify `portal/internal/chat/agent_builder.go` | `BuildSkillsIndex(workspace, extraDirs)` |
| Modify chat stream / agent build 调用点 | 组装共享 skill dirs（经 AccessChecker） |
| Create `portal/migrations/007_user_resource_acl.sql` | 显式 DDL（与 AutoMigrate 对齐） |
| Optional API | 最小 Org 成员 list / Resource grant（若现有 proto 不够则加 HTTP 路由或扩展 proto） |

---

### Task 1: 上下文键 + perm 层级纯函数

**Files:**
- Create: `portal/internal/biz/ctx_caller.go`
- Create: `portal/internal/biz/resource.go`（先放类型与 `PermAtLeast`，Checker 下任务）
- Test: `portal/internal/biz/resource_acl_test.go`

- [ ] **Step 1: 写失败测试 — perm 层级**

```go
func TestPermAtLeast(t *testing.T) {
	if !PermAtLeast(PermAdmin, PermView) { t.Fatal("admin>=view") }
	if PermAtLeast(PermView, PermUse) { t.Fatal("view!<use") }
	if !PermAtLeast(PermUse, PermUse) { t.Fatal("use>=use") }
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd portal && go test ./internal/biz/ -run TestPermAtLeast -count=1`  
Expected: FAIL（未定义）

- [ ] **Step 3: 实现**

`ctx_caller.go`：

```go
package biz

import "context"

type ctxKey int
const (
	ctxCallerUserID ctxKey = iota + 1
	ctxOrgID
)

func WithCallerUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxCallerUserID, userID)
}
func CallerUserID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxCallerUserID).(string)
	return v, ok && v != ""
}
func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, ctxOrgID, orgID)
}
func OrgID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxOrgID).(string)
	return v, ok && v != ""
}
```

`resource.go` 常量与层级：

```go
type Perm string
const (
	PermView  Perm = "view"
	PermUse   Perm = "use"
	PermEdit  Perm = "edit"
	PermAdmin Perm = "admin"
)

func permRank(p Perm) int {
	switch p {
	case PermView: return 1
	case PermUse: return 2
	case PermEdit: return 3
	case PermAdmin: return 4
	default: return 0
	}
}
func PermAtLeast(have, need Perm) bool { return permRank(have) >= permRank(need) }
```

- [ ] **Step 4: 测试通过**

Run: `cd portal && go test ./internal/biz/ -run TestPermAtLeast -count=1`  
Expected: PASS

- [ ] **Step 5: Commit（portal 仓库）**

```bash
cd portal && git add internal/biz/ctx_caller.go internal/biz/resource.go internal/biz/resource_acl_test.go && git commit -m "feat(biz): add caller context and perm hierarchy"
```

---

### Task 2: AccessChecker 矩阵（假 repo）

**Files:**
- Modify: `portal/internal/biz/resource.go`
- Modify: `portal/internal/biz/resource_acl_test.go`

- [ ] **Step 1: 定义 Resource 结构与 `AccessChecker` 接口依赖**

```go
type Visibility string
const (
	VisibilityPrivate Visibility = "private"
	VisibilityOrg     Visibility = "org"
	VisibilityPublic  Visibility = "public"
)

type ResourceType string
const (
	ResourceTypeAgent ResourceType = "agent"
	ResourceTypeTool  ResourceType = "tool"
	ResourceTypeSkill ResourceType = "skill"
)

type Resource struct {
	ID            string
	Type          ResourceType
	Name          string
	OwnerUserID   string
	Visibility    Visibility
	HomeOrgID     string
	BoundAgentID string
	PayloadRef    string
}

type ResourceGrant struct {
	ResourceID  string
	GranteeType string // user|org
	GranteeID   string
	Perm        Perm
}

type ResourceReader interface {
	GetResource(ctx context.Context, id string) (*Resource, error)
	ListGrants(ctx context.Context, resourceID string) ([]ResourceGrant, error)
	UserOrgIDs(ctx context.Context, userID string) ([]string, error)
}

type AccessChecker struct{ r ResourceReader }

func NewAccessChecker(r ResourceReader) *AccessChecker { return &AccessChecker{r: r} }

// EffectivePerm 返回 caller 对该资源的最高有效权限；无权限返回 ""。
func (c *AccessChecker) EffectivePerm(ctx context.Context, callerUserID, resourceID, agentIDForBound string) (Perm, error)

// Can 在 EffectivePerm >= need 时为 true；bound 不满足则 use 失败。
func (c *AccessChecker) Can(ctx context.Context, callerUserID, resourceID string, need Perm, agentIDForBound string) (bool, error)
```

决议（与 spec §5.1）：owner→admin；`visibility=org` 且 caller∈home_org→use；Grant 取最高；public 本期不授予；`bound_agent_id` 非空且 `need` 需要 use 路径时要求 `agentIDForBound` 匹配。

- [ ] **Step 2: 表驱动测试（至少覆盖）**

| case | 期望 |
|------|------|
| owner | admin |
| org 成员 + visibility=org | use（非 edit） |
| 非成员 | 无权限 |
| user grant edit | edit |
| org grant use | use |
| bound 不匹配 | 无 use（可有 view 若 org） |
| public visibility | 仍无（本期） |

- [ ] **Step 3: 实现 `EffectivePerm` / `Can` 使测试通过**

- [ ] **Step 4: Run**

`cd portal && go test ./internal/biz/ -run AccessChecker -count=1`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd portal && git add internal/biz/resource.go internal/biz/resource_acl_test.go && git commit -m "feat(biz): AccessChecker ownership and org/grant rules"
```

---

### Task 3: GORM 模型 + AutoMigrate + SQL migration

**Files:**
- Create: `portal/internal/data/model/identity.go` — `User`、`Org`、`OrgMember`、`UserToken`
- Create: `portal/internal/data/model/resource.go`
- Modify: `portal/internal/data/model/chat.go` — 加 `UserID string \`gorm:"column:user_id;size:36;not null;index"\``
- Modify: `portal/internal/data/data.go` — AutoMigrate 新模型（含 `UserToken`）
- Create: `portal/migrations/007_user_resource_acl.sql`

- [ ] **Step 1: 模型字段与 spec §4 对齐**（users/orgs/org_members/**user_tokens**/resources/resource_grants；resources 唯一索引 `(type, payload_ref)`）

`UserToken` 最小字段：`token_hash`（PK 或 unique）、`user_id`、`created_at`。Bootstrap 将配置明文 token SHA-256 后写入；Auth 只查 hash。

- [ ] **Step 2: SQL migration** 含上述表 + `chat_sessions.user_id`（先可空或 `DEFAULT ''`，Task 5 回填）

- [ ] **Step 3: `go build ./internal/data/...`**

- [ ] **Step 4: Commit**

```bash
cd portal && git add internal/data/model internal/data/data.go migrations/007_user_resource_acl.sql && git commit -m "feat(data): identity, user_tokens, resource tables + session user_id"
```

---

### Task 4: Identity / Resource MySQL repos + wire

**Files:**
- Create: `portal/internal/biz/identity.go`（repo 接口 + 最小 usecase：EnsureMember、LookupUserByTokenHash、IssueToken）
- Create: `portal/internal/data/identity_mysql.go`
- Create: `portal/internal/data/resource_mysql.go`
- Modify: `portal/internal/data/data.go` ProviderSet
- Modify: `portal/internal/biz/biz.go` ProviderSet — 绑定 `NewAccessChecker`

- [ ] **Step 1: 实现 CRUD 最小集**  
  `CreateUser` / `GetUser` / `CreateOrg` / `AddMember` / `UserOrgIDs` / `UpsertTokenHash` / `UserIDByTokenHash` / `CreateResource` / `GetByPayload` / `ListAllByType`（可见性过滤在 usecase 用 AccessChecker；SQL 优化二期）

- [ ] **Step 2: 仓库级单测**（可用 sqlite 若项目已有；否则跳过集成、保留接口假实现测 usecase）

- [ ] **Step 3: `cd portal && go build ./...`**

- [ ] **Step 4: Commit**

```bash
cd portal && git add internal/data/identity_mysql.go internal/data/resource_mysql.go internal/biz/identity.go internal/biz/biz.go internal/data/data.go && git commit -m "feat(data): identity and resource mysql repos"
```

---

### Task 5: Conf + Bootstrap（默认 org/user/token + 回填）

**Files:**
- Modify: `portal/internal/conf/conf.proto`：
  - `message Data`：在现有字段后追加 `string data_root = 3;`（若 3 已被占用则用下一可用号）
  - `message Bootstrap`：追加 `Auth auth = 5;`（若 5 占用则下一可用号）
  - 新 `message Auth { string bootstrap_user_id = 1; string bootstrap_token = 2; string bootstrap_org_id = 3; string service_principal_user_id = 4; }`
- Regenerate or hand-edit `conf.pb.go`
- Create: `portal/internal/data/bootstrap.go` — `BootstrapACL(ctx, db, conf)`：
  1. 确保 bootstrap user/org/member  
  2. upsert bootstrap token hash → `user_tokens`  
  3. 为无 Resource 行的 agent/tool 插入 **private** Resource：`owner=bootstrap`，`home_org_id=默认 org`（锚点，非共享），`bound_agent_id` 空  
  4. 空 `user_id` 会话回填 bootstrap user  
  5. 确保 service principal user 行存在（可与 bootstrap 相同 id，若配置不同则另建）
- Modify: main / `NewData` 调用点在 migrate 后跑 bootstrap
- Modify: `portal/configs/config.yaml` 示例

- [ ] **Step 1: 配置字段 + 文档注释**

- [ ] **Step 2: Bootstrap 幂等**（多次启动不重复插）

- [ ] **Step 3: 本地启动或单测验证 Resource 行数 = agents+tools；`user_tokens` 有 bootstrap 行**

- [ ] **Step 4: Commit**

```bash
cd portal && git add internal/conf internal/data/bootstrap.go configs/config.yaml && git commit -m "feat(portal): ACL bootstrap user/org/token and backfill resources"
```

---

### Task 6: Auth middleware

**Files:**
- Create: `portal/internal/server/middleware/auth.go`
- Create: `portal/internal/server/middleware/auth_test.go`
- Modify: `portal/internal/server/http.go` — 在 MetaData 后加 Auth（webhook 路径可跳过：复用 WebhookVerify 白名单思路）

行为：
1. `Authorization: Bearer <token>` → hash 后查 `user_tokens`（Task 3 已建表；bootstrap 由 Task 5 写入）→ `WithCallerUserID`
2. 无 token → 401（Kratos `errors.Unauthorized`）
3. `X-Org-Id` 若存在：校验成员，否则 400 `invalid_org_context`；通过则 `WithOrgID`
4. **不**提供默认关闭鉴权的生产开关；单测用 `WithCallerUserID` 直接注入 ctx

第二用户验收：Task 5/11 提供 `IssueToken(userID)`（或 bootstrap 脚本插入第二 `user_tokens` 行）。

- [ ] **Step 1: 单测 middleware 用假 transporter / 直接测 parse 函数**

- [ ] **Step 2: 挂到 HTTP Server**

- [ ] **Step 3: `go test ./internal/server/middleware/ -count=1`**

- [ ] **Step 4: Commit**

```bash
cd portal && git add internal/server/middleware/auth.go internal/server/middleware/auth_test.go internal/server/http.go && git commit -m "feat(server): bearer auth middleware for caller_user_id"
```

---

### Task 7: Agent usecase — ACL + 默认 workspace + 同步 Resource

**Files:**
- Modify: `portal/internal/biz/agent.go` — `AgentMeta` 可附 `ResourceID`（可选）
- Modify: `portal/internal/biz/agent_usecase.go`
- Modify: `portal/internal/data/agent_mysql.go` — Create 时若 workspace 空：先生成 id，设 `filepath.Join(dataRoot, "agents", id)`（dataRoot 经 usecase 注入）
- Modify: `portal/internal/service/agent.go` — 传 ctx（已有）
- Test: `portal/internal/biz/agent_acl_test.go`
- 错误码可先用本任务内 kratos errors，Task 11 再统一 reason 字符串

行为摘要：
- `Create`：要求 caller；建 agent；建 Resource（private，owner=caller；若 ctx 有 org 且请求 visibility=org 则设 home_org）；`public` → 400 `PUBLIC_NOT_ENABLED`
- `Get`/`List`：**无 view → 404**（防枚举）/ List 过滤不可见
- `Update` payload：无 view → **404**；有 view 但无 edit → **403 `FORBIDDEN_PERM`**；改 visibility/owner：需 admin（同样 404/403 分支）
- `Delete`：无 view → 404；有 view 无 admin → 403；有 admin → 删
- `BindTools`：Agent 侧 edit、Tool 侧 use；任一侧无 view → 对该 id 404；有 view 缺 perm → 403

- [ ] **Step 1: 单测假 repo 覆盖 Create 默认 workspace、List 过滤、Update 403 vs 404**

- [ ] **Step 2: 实现**

- [ ] **Step 3: `go test ./internal/biz/ -run Agent -count=1`**

- [ ] **Step 4: Commit**

```bash
cd portal && git add internal/biz/agent*.go internal/data/agent_mysql.go internal/service/agent.go && git commit -m "feat(agent): ACL gates and default per-agent workspace"
```

---

### Task 8: Tool usecase — ACL + 同步 Resource

**Files:**
- Modify: `portal/internal/biz/tool.go` / tool usecase / `data/tool_mysql.go` / `service/tool.go`
- Test: 对称于 Agent（Create/List/Update/Delete），含 **403 vs 404** 分支

- [ ] **Step 1–4:** 同 Task 7 模式；Commit `feat(tool): ACL gates and resource sync`

---

### Task 9: Chat sessions — `user_id` 隔离 + Agent use

**Files:**
- Modify: `portal/internal/biz/chat.go` — `ChatSession.UserID`；`CreateSession(ctx, agentID, …)` 写 caller；`Get`/`List`/`Send` 校验 `session.UserID == caller` 否则 `ErrSessionNotFound`（404）
- Modify: `portal/internal/data` chat mysql — Create/List 带 user_id
- Modify: `portal/internal/service/chat.go` / `chat_stream.go` — 创建会话前 `Can(agent, use)`
- Test: `portal/internal/biz/chat_user_isolation_test.go`

- [ ] **Step 1: 失败测试 — 用户 B 不能读用户 A 的 session**

- [ ] **Step 2: 实现过滤**

- [ ] **Step 3: `go test ./internal/biz/ -run Session -count=1`**

- [ ] **Step 4: Commit** `feat(chat): isolate sessions by user_id`

---

### Task 10: 共享 Skill Resource + BuildSkillsIndex 多根

**Files:**
- Create: `portal/internal/biz/skill_resource.go` — **唯一创建路径（一期）**：内部 helper（非 Hub）

```go
// RegisterOrgSkill 在 {data_root}/skills/{id}/ 写入 SKILL.md（调用方已备好 content），
// 并创建 Resource{type:skill, visibility:org, home_org_id, owner}。需 caller 为 home_org 成员。
func (uc *SkillResourceUsecase) RegisterOrgSkill(ctx context.Context, homeOrgID, name, content string) (*Resource, error)
```

- 可选：`POST /api/v1/skills` 手写 JSON 路由调用上述 helper（Task 11 可一并加）
- Modify: `portal/internal/chat/agent_builder.go`

```go
// BuildSkillsIndex 合并 workspace/skills 与额外共享 skill 根目录。
func BuildSkillsIndex(workspace string, extraSkillDirs []string) (*skills.Index, error) {
	var dirs []string
	if workspace != "" {
		ws := filepath.Join(workspace, "skills")
		if st, err := os.Stat(ws); err == nil && st.IsDir() {
			dirs = append(dirs, ws)
		}
	}
	for _, d := range extraSkillDirs {
		if d == "" { continue }
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	return skills.NewIndex(dirs, nil, nil)
}
```

- 更新**全部** `BuildSkillsIndex(` 调用点（含 `ExecuteSkillScript` wiring、hermes 测试）：组装 `extraSkillDirs` = 对当前 `caller` + `agentID` 满足 `Can(..., PermUse, agentID)` 的 skill，路径 `filepath.Join(dataRoot, "skills", resource.ID)`
- Test: `agent_builder_test.go` 多目录 Index

- [ ] **Step 1–4 + Commit** `feat(chat): merge org-shared skill dirs into skills index`

---

### Task 11: 错误码对齐 + 最小 Org/Grant/Token HTTP

**Files:**
- `portal/internal/pkg/errors` 或 kratos errors：`PUBLIC_NOT_ENABLED`、`INVALID_HOME_ORG`、`INVALID_ORG_CONTEXT`、`FORBIDDEN_PERM`
- 手写路由（避免大改 proto，除非已有流程很顺）：
  - `POST /api/v1/orgs/{id}/members`
  - `POST /api/v1/resources/{id}/grants`（需 Resource admin）
  - `POST /api/v1/users/{id}/tokens`（仅 bootstrap/admin 或本人；返回明文 token 一次，存 hash）— **第二用户验收依赖此接口或等价 SQL**

- [ ] **Step 1: 错误码常量 + 单测映射**

- [ ] **Step 2: Grant / member / token API**

- [ ] **Step 3: Commit** `feat(api): org grants, tokens, and ACL error codes`

---

### Task 12: Service principal 接入 growth/cron

**Files:**
- Modify: `portal/internal/service/growth_worker.go` — worker tick / spawn 入口：`ctx = biz.WithCallerUserID(ctx, servicePrincipalUserID)`
- Modify: cron 执行入口（`portal/internal/cron` 或 `chat/cronjob_wiring.go` 实际 Run Agent 处）同样注入
- 若任务已绑定 agent Resource：写盘前 `Can(principal, resourceID, PermEdit|PermUse, agentID)`；纯维护且无 Resource 行时仅允许已注入的绝对 `workspace_root`（与今日一致）
- Test: wiring 单测断言 ctx 带 principal（假 checker 可选）

- [ ] **Step 1: 定位 growth/cron 构建 agent 的 ctx 入口**

- [ ] **Step 2: 注入 service principal + 必要 Can 检查**

- [ ] **Step 3: `go test` 相关包不破**

- [ ] **Step 4: Commit** `feat(growth): run internal jobs as service principal`

---

### Task 13: 回归与验收

- [ ] **Step 1:** `cd portal && go test ./...`（或至少 `./internal/biz/ ./internal/server/middleware/ ./internal/chat/ ./internal/service/`）
- [ ] **Step 2:** 手工：`Bearer bootstrap` 建 org-visible Agent → `POST members` + `POST tokens` 得第二用户 → 能 chat、同 workspace、不同 session
- [ ] **Step 3:** 非成员 List 无该 Agent；Get 404；有 view 无 edit 时 Update → 403
- [ ] **Step 4:** growth worker 带 Auth 中间件的环境下仍能跑（principal token 不走 HTTP 时靠 ctx 注入）
- [ ] **Step 5:** 更新 spec 状态；根仓库可提交计划勾选状态

---

## 验收清单

- [ ] AccessChecker 矩阵绿
- [ ] 新建 Agent workspace = `{data_root}/agents/{id}/`
- [ ] 同 Org 两用户共享 Agent workspace、会话隔离
- [ ] Tool 绑定双端 ACL
- [ ] 共享 skill 进 Index；workspace skill 仍扫描
- [ ] `visibility=public` 创建失败
- [ ] 无 Bearer → 401
- [ ] growth/cron 仍可用 service principal / 已解析 workspace（不破）

---

## 执行备注

- 实现时优先 **TDD**：先红测再实现。  
- SQL 优化 `ListVisible` 可第二波；一期正确性优先。  
- Proto 变更后按 portal 既有 `Makefile`/`protoc` 流程生成。  
- Web 前端传 Bearer / `X-Org-Id` 可另开任务；本期后端可测即可。
