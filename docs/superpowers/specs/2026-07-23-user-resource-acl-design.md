# 用户体系与统一 Resource ACL 设计

**日期**: 2026-07-23  
**状态**: 已落地（portal `feature/user-resource-acl`）  
**方案**: 统一 Resource + Grant（方案 2）；本期不含 Public Hub  
**关联**:  
- `portal/internal/data/model/agent.go`（`agents.workspace`）  
- `portal/internal/data/model/tool.go` / `agent_tool.go`  
- `portal/internal/data/model/chat.go`（`chat_sessions`）  
- `framework/auth/checker.go`（数据源权限占位；本期不强制打通）  
- 后续（另开 spec）：Public Hub 发布/安装  

---

## 1. 背景与问题

Portal 今日的 Agent / Tool 是**全局清单**：无 User、无 Org、无可见性；`agents.workspace` 为自由字符串；`chat_sessions` 仅按 `agent_id` 关联，多人共用 Agent 时会话与磁盘边界不清。

产品目标（目标架构）：多租户 + 将来的公开市场。Agent / Tool / Skill 可为私有或共享；Agent workspace **默认按 `agent_id` 区分**。本期先落地「用户 / 归属 / workspace / Org 共享」，**Hub 另开 spec**。

已确认约束：

| 项 | 选择 |
|----|------|
| 目标边界 | 多租户；`public` 预留；Hub 不做 |
| 可见性 | `private` \| `org` \| `public` |
| 私有收紧 | `owner_user_id` + 可选 `bound_agent_id` |
| Org | 完整多 Org；资源带 `home_org_id`（见下） |
| Agent 身份 | 全局 `id`；路径**不含** org |
| Agent 共享 | Org 成员可 `use` 同一 Agent |
| 隔离 | **Workspace 按 agent 共享**；**会话按 user 分** |
| 实现形状 | 统一 Resource + Grant，payload 表保留 |

---

## 2. 目标与非目标

### 2.1 目标

1. 引入 `User` / `Org` / `OrgMember`，请求具备 `caller_user_id`（及可选当前 `org_id`）。  
2. 以 **Resource + ResourceGrant** 作为 Agent/Tool/Skill 的归属与发现权威；现有 `agents` / `tools` 保留为运行时 payload。  
3. 统一 `AccessChecker`：List/Get/Update/Delete/Bind/Chat 均过 ACL。  
4. 新建 Agent 时默认 `workspace = {data_root}/agents/{agent_id}/`。  
5. `chat_sessions` 增加 `user_id`，会话按用户隔离。  
6. Org 共享 skill：元数据进 Resource，磁盘在约定根下，运行时合并进 Skill Index。  
7. `visibility=public` 枚举存在，**创建/升级拒绝**（`public_not_enabled`）。

### 2.2 非目标

| 项 | 说明 |
|----|------|
| Public Hub 发布/安装 | 另开 spec |
| 真正的 public 可见 | 仅占位 |
| 搬迁既有 workspace 目录 | 旧路径保留；仅新建默认新约定 |
| framework 内多租户 ACL | ACL 只在 portal；framework 消费已解析的 `workspace_root` / skills 路径 |
| 打通 `auth.Checker` ↔ Resource | 可后续 |
| 细粒度 Grant UI / 跨 org 复杂委派 | 一期 API + 最小角色即可 |
| Agent/Tool 改名取消全局 unique | 暂保留 unique |

---

## 3. 架构与组件

```text
Caller (user + optional org)
  → Portal API / Chat
      → AccessChecker (owner | visibility=org+member | grants)
      → agents / tools payload + resources
      → workspace_root = agents.workspace
      → skills_dirs = workspace/skills ∪ visible shared skill roots
          → framework (Index / ReAct / skill_manage)
```

| 单元 | 建议位置 | 职责 |
|------|----------|------|
| User / Org / OrgMember | `portal` 新表 + biz | 身份与成员 |
| Resource / ResourceGrant | `portal` 新表 + biz | 归属、可见性、显式授权 |
| AccessChecker | `portal/internal/biz` | `Can` / `ListVisible` |
| Agent/Tool usecase | 现有 biz 改造 | 写 payload 时同步 Resource；读前 ACL |
| Skill 登记 | Resource `type=skill` + 磁盘 | 共享 skill 权威路径 |
| Session | `chat_sessions.user_id` | 按用户隔离对话 |
| Auth（最小） | Bearer → `caller_user_id` | 见 §4.5；无 token → 401 |
| Service principal | 内部 growth/cron | 不走 `ListVisible`；见 §4.5 |

---

## 4. 数据模型

### 4.1 身份

```text
User        { id, name, ... }
Org         { id, name, ... }
OrgMember   { org_id, user_id, role: owner|member }
```

### 4.2 Resource

```text
Resource {
  id,
  type: agent|tool|skill,
  name,
  owner_user_id,                 // 必填
  visibility: private|org|public,
  home_org_id,                   // visibility=org 时必填
  bound_agent_id?,               // 可选；use 时收紧到该 agent
  payload_ref,                   // agents.id / tools.id / skill 目录键（通常=resource id）
  created_at, updated_at
}
```

### 4.3 Grant

```text
ResourceGrant {
  resource_id,
  grantee_type: user|org,
  grantee_id,
  perm: view|use|edit|admin
}
```

**权限层级（包含关系）**：`admin` ⊃ `edit` ⊃ `use` ⊃ `view`。  
`Can(caller, resource, needed)` 为真当且仅当 caller 有效权限 ≥ `needed`（按上序）。Grant 存最高档即可；`admin` Grant 满足一切低权限检查。

默认：`owner_user_id` 拥有隐式 `admin`，不必为 owner 写 Grant 行。

`OrgMember.role`（`owner`/`member`）**只**用于 Org 管理（邀请/移除成员、改 org 元数据），**不**参与 Resource ACL 计算。

### 4.4 Payload 与会话

- `agents` / `tools`：保持现有运行时字段；`agents.workspace` 仍是路径权威。  
- `chat_sessions`：新增 `user_id NOT NULL`。  
- `agent_tools`：保留；绑定前校验 Tool `use` + Agent `edit`。

### 4.5 调用方身份（`caller_user_id`）

本期交付最小鉴权，使 ACL 可测：

1. 表 `users` + 发行 **Bearer token**（或复用 portal 若已有 session/cookie；若无则新增 `user_tokens` / 配置态 bootstrap token）。  
2. 每个 API / Chat 请求：从 `Authorization: Bearer …` 解析 `caller_user_id`；缺失或无效 → **401**。  
3. 本地/单测：配置 `BOOTSTRAP_USER_ID` + 对应 token；迁移把既有资源挂到该用户。  
4. 可选 `X-Org-Id`：须证明 `caller ∈ org`，否则忽略或 400（实现选一，推荐 **400** `invalid_org_context`）。

内部 growth/cron：**service principal**（系统用户 id）。不调用 `ListVisible`；只消费任务里已解析的 `workspace_root` / agent_id，并在写盘前用该 principal 对目标 Resource 做 `Can(..., edit|use)`（若任务上下文已绑定 resource）。纯维护路径若无 Resource 行，仅允许操作已注入的绝对 workspace（与今日行为对齐）。

---

## 5. ACL 规则

### 5.1 有效权限决议

对给定 `needed` perm，caller 满足其一即可：

1. **Owner**：`owner_user_id == caller` → 有效 `admin`。  
2. **Org 共享隐含 use**：`visibility=org` 且 caller ∈ `home_org_id` → 有效 **`use`**（因此也含 `view`）。**不含** `edit`/`admin`；改配置/删仍需 owner 或 Grant。  
3. **Grant**：存在对 caller 或 caller 所属 org 的 Grant，且 Grant.perm ≥ needed。  
4. **（预留）** `visibility=public` → 有效 `use`；本期创建/升级已拒绝，决议分支可先不实现或恒 false。

`bound_agent_id`：仅当检查 `use`（或更高且实际动作为 use 场景）时附加：请求上下文 `agent_id` 必须等于 `bound_agent_id`，否则视为无 `use`。

不可见（连 `view` 都不满足）→ **404**。有 `view` 但 `needed` 更高不满足 → **403** `forbidden_perm`。

### 5.2 操作 × 最低 perm

| 操作 | 最低 perm | 额外 |
|------|-----------|------|
| List / Get / 读 SKILL 文件 | view | |
| 更新 Agent/Tool/Skill **payload**（含改 `workspace`、模型配置、SKILL 正文等） | edit | |
| 对话 / 跑 Agent | use | session.`user_id=caller` |
| 绑定 Tool→Agent | Tool use + Agent edit | |
| Agent 加载共享 Skill | Skill use + bound | |
| 改 visibility / owner / 发放 Grant | admin | |
| 删除 | admin | |
| 创建 | — | owner=caller；`org` 时 home_org 必填且为成员；`public` → 400 `public_not_enabled` |

### 5.3 Org 上下文

- 请求可带当前 `org_id`（如 `X-Org-Id`），校验见 §4.5。  
- 创建 `visibility=org` 时默认 `home_org_id = 当前 org`。  
- 列表「本 org」= 自己的 private ∪（`visibility=org AND home_org_id=当前`）∪ grants。

### 5.4 Framework 边界

Framework **不**实现租户 ACL。Portal 在 `BuildRegistry`、注入 `workspace_root`、组装 `skills_dirs` 之前完成鉴权与路径解析。

---

## 6. Workspace 与 Skill 落盘

```text
{data_root}/
  agents/{agent_id}/
    skills/            # 工作区内 skill（成长 / skill_manage）
    memory/            # 可选
    .learnings/        # 现有约定可保留
  skills/{resource_id}/  # Resource(type=skill) 共享实体目录
    SKILL.md
```

| 类别 | Resource | 磁盘 | 进 Index |
|------|----------|------|----------|
| Agent 工作区 skill | 可选登记；`bound_agent_id`+private | `{workspace}/skills/<name>/` | 扫描 workspace |
| Org/跨 Agent 共享 | **必须** Resource；`visibility=org` 等 | `{data_root}/skills/{resource_id}/` | portal 合并可见且 use 通过的路径 |

- 同 Agent 多用户：**共享 workspace**；文件写冲突不做锁，依赖现有 confirm。  
- 共享 skill 进 Index 默认只读；改写需 Skill `edit`/`admin` 且路径受允许根约束。

---

## 7. 会话隔离

- 创建会话：校验 Agent `use`；写入 `user_id=caller`。  
- List/Get/发消息：`session.user_id == caller`，否则 404。  
- 不按 user 拆分 workspace 目录。

---

## 8. 迁移

1. 建表：`users`、`orgs`、`org_members`、`resources`、`resource_grants`。  
2. Bootstrap：默认 Org + 迁移/管理员用户。  
3. 现有 Agent/Tool → Resource：`owner=bootstrap`，`visibility=private`，`home_org_id=默认 org`（锚点，非共享），`bound_agent_id` 空。  
4. 写路径：创建/更新 payload 时同步 Resource。  
5. `chat_sessions.user_id`：回填 bootstrap（历史会话归属迁移用户，文档标明）。  
6. 既有 `agents.workspace`：**不搬盘**；仅新建默认 `{data_root}/agents/{id}`。

---

## 9. 错误契约

| 场景 | 响应 |
|------|------|
| 未登录 / token 无效 | 401 |
| 不可见（无 view） | 404 |
| 有 view 但 perm 不足 | 403 `forbidden_perm` |
| `visibility=public` 创建/升级 | 400 `public_not_enabled` |
| 非成员设 `home_org` / 非法 `X-Org-Id` | 400 `invalid_home_org` / `invalid_org_context` |
| `bound_agent_id` 不可见 | 400 |
| 共享 skill 路径越界 | 拒绝写；读 Index 跳过 + warn |

---

## 10. 测试

- **单元**：AccessChecker 矩阵（owner / org 成员 / 非成员 / grant / bound / public 拒绝）。  
- **单元**：默认 workspace 路径；会话按 `user_id` 过滤。  
- **集成**：同 Org 两用户 use 同一 Agent → 同 workspace、不同 session；非成员 Get → 404。  
- **集成**：绑定双端 ACL；Index = workspace skills ∪ 可见共享 skill。  
- **回归**：growth/cron/skill_manage 在注入 `workspace_root` 后行为不变；内部 job 用 service principal。

---

## 11. 后续（另开 spec）

- Public Hub：发布、隔离区、安装（fork vs 引用）、审计。  
- `visibility=public` 真正启用。  
- 旧 workspace 搬迁工具。  
- Resource ACL 与 datasource `auth.Checker` 统一。  
- Agent/Tool 名称作用域（取消全局 unique）。
