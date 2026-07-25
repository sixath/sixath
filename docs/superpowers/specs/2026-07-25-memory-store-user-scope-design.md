# MemoryStore P2-A：启用 `scope=user`（工具读写 + Prefetch）

> 状态：已实现（待 Task 8 冒烟）· 分支 `feat/memory-store-user-scope`  

> 日期：2026-07-25  
> 回链：[MemoryStore Facade 一期](./2026-07-25-memory-store-facade-design.md) §8 第 1 项  
> 切片：**A only** — 正式 User 主体读写；**不含** Turn LLM 提取 / 冲突消解 / 向量 / Neo4j

---

## 0. 目标与非目标

### 目标

1. 启用 `MemoryStore` 的 **`scope=user`**：工具可 `remember` / `recall` / `get` / `list`（与 session units 同表同语义）。
2. Prefetch 增加 **user/units** 一路，顺序为：`user → session → agent`。
3. User 身份来自现有 Portal 身份体系（`users` + `chat_sessions.user_id` + `CallerUserID`）；缺身份时 **静默跳过**，不报错。
4. 明确写出「本迭代不做 / 下迭代做」清单，避免范围漂移。

### 非目标（下迭代及以后）

| # | 项 | 说明 |
|---|----|------|
| 1 | Turn 后 LLM 提取 / `AddFromTurn` | 仍仅工具手写 |
| 2 | ConflictResolver / supersede 链 | 仍 in-place replace + soft-delete |
| 3 | 向量 Sidecar（Qdrant 等） | FTS/子串检索保持现网 units 行为 |
| 4 | Neo4j 图记忆 | — |
| 5 | Prefetch 配额/去重增强 | 仅加 user 一路，fail-open 不变 |
| 6 | 清理 `memory.Manager` / 旧配置 | — |
| 7 | Go MCP Server | — |

---

## 1. 背景与痛点

一期 Facade 已统一三 scope API，但对 `ScopeUser` 一律返回 `ErrScopeNotEnabled` / 工具 `scope_not_enabled`。Portal 已有：

- `users` 表、`chat_sessions.user_id`（迁移 `007_user_resource_acl.sql`）
- 请求上下文 `biz.CallerUserID`（Auth middleware）
- `memory_units.scope_type` ENUM **已含** `'user'`

缺口是：units 后端拒非 session、Facade stub、工具短路、Prefetch 无 user 路、tool context 未注入 `user_id`。

---

## 2. 决策摘要（已确认）

| 项 | 选择 |
|----|------|
| 本迭代切片 | **A**：启用 user 层 R/W + Prefetch；无自动提取 |
| 缺 `user_id` | **静默**：remember/recall/get/list/prefetch 跳过，**不**返回 `scope_not_enabled` / error |
| 写入内容 | 仅工具：`memory_remember(scope=user)`（偏好、纠正、稳定事实） |
| 存储 | **同表 `memory_units`**：`scope_type='user'`，`scope_id = user_id`；并增加可空列 `user_id`（与 `scope_id` 对齐，便于索引与日后归因） |
| Prefetch 顺序 | `user/units` → `session/units` → `agent/files`；无 user_id 则跳过 user 路 |
| `USER.md` | **仍属 agent 文件**，与 `scope=user` 无关 |

---

## 3. 身份与 ScopeID

### 3.1 权威 user_id

单次 Agent turn 内解析顺序（Portal 组装时固化进 context，工具/Prefetch 只读）：

1. **会话所有者**：`chat_sessions.user_id`（非空）
2. 否则 **调用方**：`biz.CallerUserID(ctx)`（Auth / cron service principal）
3. 否则视为 **无 user_id** → 静默

说明：正常浏览器聊天应同时有 session.user_id 与 CallerUserID，且二者一致；cron/内部路径可能仅有 service principal。P2-A **不**做跨用户记忆；不一致时以 **session.user_id** 为准（会话归属清晰）。

### 3.2 Framework 上下文键

新增（与现有平行）：

```go
// framework/tool/tool.go
const ContextKeyUserID = "user_id"
```

Portal 在 `SendMessage` / SSE stream 构建 tool `ctx` 时：

```go
ctx = context.WithValue(ctx, tool.ContextKeyUserID, resolvedUserID)
```

PrefetchQuery 增加字段（见 §6）。

### 3.3 RememberInput / RecallQuery

- `scope=user` 时：`ScopeID` **必须** = `user_id`（由工具/Prefetch 从 context 填入）。
- `AgentID` / `source_session_id`：可选；remember 时 Portal 可把当前 `session_id` 写入 `source_session_id`、`agent_id` 写入列，便于审计（不参与 scope 隔离）。
- `UnitID`：user 的 replace/remove 与 session 相同（按 id）。

---

## 4. 数据模型

### 4.1 迁移 `010_memory_units_user_id.sql`（建议名）

```sql
ALTER TABLE memory_units
  ADD COLUMN user_id VARCHAR(36) NULL AFTER agent_id,
  ADD INDEX idx_mu_user (user_id, status);
```

| 列 | `scope=session` | `scope=user` |
|----|-----------------|--------------|
| `scope_type` | `session` | `user` |
| `scope_id` | `session_id` | `user_id` |
| `user_id` | 可选（本迭代可空；不强制回填） | **= scope_id** |
| `agent_id` | 可选 | 可选（写入时的 agent） |
| `source_session_id` | 可选 | 建议填当前 session |

GORM `MemoryUnit` 同步加 `UserID *string`。

**不做**：为 user 新建表；不做 user↔session 自动迁移；不强制改写历史 session 行的 `user_id`。

### 4.2 后端形态

将现「仅 session」的 units 实现泛化为 **UnitsBackend**（可保留 Go 接口名 `SessionUnitsBackend` 以避免大范围 rename，但实现必须接受 `ScopeUser` + `ScopeSession`）：

- `Remember`：校验 `in.Scope` ∈ {session, user}；`ScopeID` 非空；写 `scope_type` / `scope_id`；user 时同时写 `user_id`。
- `Recall` / `List` / `Get` / `Delete`：按 `scope_type + scope_id` 过滤；检索算法与 session units **同一套**（子串/FTS，以现实现为准）。
- 空 `ScopeID`：**不由 backend 抛业务错**；由 Facade 在入口静默（见 §5）。

内存 fake（framework 测试）同步支持 `ScopeUser`。

---

## 5. Facade 语义

### 5.1 去掉 User stub

删除 `case ScopeUser: return ErrScopeNotEnabled`。`ScopeUser` 与 `ScopeSession` 一样路由到 units backend（`SourceUnits` / 默认 source）。

### 5.2 静默规则（缺 ScopeID）

当 `scope == ScopeUser` 且 `strings.TrimSpace(ScopeID) == ""`：

| 方法 | 行为 |
|------|------|
| `Remember` | `(MemoryHit{}, nil)` — 不写库 |
| `Recall` / `List` | `([], nil)` |
| `Get` | `(MemoryHit{}, ErrNotFound 或等价 not_found)` — **例外**：Get 按 id 查时可仍查库；若无 ScopeID 则返回 not_found **无** `scope_not_enabled` |
| `Delete` | `nil`（no-op） |

配置未挂 units backend：与 session 一致（配置错误可返回 backend not configured；与「无 user_id」区分）。

### 5.3 错误码变化

| 旧（一期） | 新（P2-A） |
|-----------|-----------|
| `scope=user` → `scope_not_enabled` | **移除**该路径；改为真实读写或静默 skip |
| — | 工具层可对静默 remember 返回 `{"skipped": true, "reason": "user_id_missing"}`（**非** error 字段），便于观测且不诱导模型重试 |

`scope_not_enabled` 仍保留给「配置关闭整个 units / 某 scope」等未来开关，但 **默认 user 启用**。

---

## 6. 工具

### 6.1 `memory_remember`

- 去掉 `scope == user → scopeNotEnabledResult()`。
- `scope=user`：
  - `ScopeID` / 内部 user_id ← `ContextKeyUserID`
  - 若空：`{"skipped": true, "reason": "user_id_missing"}`，`error` 不出现
  - `unit_id` 用于 replace/remove（与 session 相同）
  - **不**使用 `target`（那是 agent 文件）
- Description 更新：可写 user 偏好/稳定事实；说明无登录身份时跳过。

### 6.2 `memory_recall` / `memory_get`

- `scope=user`：`source` 默认 `units`；绑定 `ContextKeyUserID`。
- 无 user_id：recall → 空 `hits`；get → `not_found`（或 skipped），均无 `scope_not_enabled`。

### 6.3 破坏性说明

对已接入一期文档的调用方：原先依赖 `scope_not_enabled` 探测 user 未上线的逻辑需改为「真实成功或 skipped」。Release note 写明。

---

## 7. Prefetch

### 7.1 `PrefetchQuery`

```go
type PrefetchQuery struct {
    // ...现有字段
    UserID string // 新增；空则跳过 user 路
}
```

（若已有 `Identity` 结构，可将 UserID 放其上，但 StorePrefetchBackend 必须以显式 UserID 为准。）

### 7.2 `StorePrefetchBackend` 顺序

1. **user**：`Recall(scope=user, source=units, ScopeID=UserID)` — 仅当 `UserID != ""`
2. **session**：现有
3. **agent**：现有

合并 `PrefetchPart`：`Label` 分别为 `user` / `session` / `agent`。任一路失败：保留现网 **partial success + fail-open**。

### 7.3 Portal 接线

`memory_prefetch_bootstrap` / chat 构建 PrefetchQuery 时填入 §3.1 解析的 `UserID`。配置 `memory_store.prefetch.scopes` 默认改为 `[user, session, agent]`（若已实现 scopes 过滤；否则硬编码三路即可，YAGNI）。

---

## 8. Portal 文件变更清单（设计级）

| 区域 | 变更 |
|------|------|
| `migrations/010_…sql` | 加 `user_id` 列 + 索引 |
| `internal/data/memory_units_*.go` | 模型 + backend 接受 `ScopeUser`；写 `user_id` |
| `internal/chat/memory_store.go` | 无需拆 Store；确认 units 注入不变 |
| `internal/chat/memory_prefetch_bootstrap.go` | 填 `UserID` |
| `internal/service/chat.go`（及 SSE） | tool ctx 注入 `ContextKeyUserID`；Prefetch 同源 |
| `docs/memory-integration.md` | 启用 user；静默语义；与 `USER.md` 对照表 |

Framework：

| 区域 | 变更 |
|------|------|
| `memory/facade.go` | 路由 user → units；静默空 ScopeID |
| `memory/session_memory.go`（或改名） | 支持 ScopeUser |
| `memory/store_prefetch_backend.go` | user 路优先 |
| `memory/store.go` | 注释更新；必要时 PrefetchQuery.UserID |
| `tool/tool.go` | `ContextKeyUserID` |
| `tool/memory/store_tools.go` | 启用 user；静默 skipped |

---

## 9. 测试与验收

### 9.1 Framework

- Facade：user remember/recall 走 units fake；空 ScopeID 静默。
- Prefetch：有 UserID 时三路；无 UserID 时仅 session+agent。
- 工具：有 context user_id 写入成功；无则 `skipped` / 空 hits，无 `scope_not_enabled`。

### 9.2 Portal

- 迁移可应用；GORM 无 TEXT DEFAULT 类回归。
- Data：`scope_type=user` CRUD 按 `scope_id`。
- 集成（可选 E2E）：登录会话 remember user → 新会话 recall 同 user 可见；未注入 user_id 时工具 skipped。

### 9.3 验收标准

1. 认证会话下 `memory_remember(scope=user)` 落库且跨 session 可 `memory_recall`。
2. Prefetch 围栏可出现 `user` 标签片段（有命中时）。
3. 无 `user_id` 时不报错、不写库。
4. `USER.md` / `scope=agent,target=user_file` 行为不变。
5. 下迭代清单（§0 非目标）未混入本 PR。

---

## 10. 与一期规格的关系

修订 [facade-design §8](./2026-07-25-memory-store-facade-design.md)：

1. ~~正式 User 主体~~ → **本规格 P2-A 交付**  
2–8 仍为后续迭代。

一期验收条「`scope=user` 稳定未启用」对本分支失效，以本规格 §9 为准。

---

## 11. 开放问题

| 问题 | 状态 |
|------|------|
| 缺 user_id 行为 | **已关闭：静默** |
| 存储方案 | **已关闭：同表 + 可空 user_id 列** |
| session.user_id vs CallerUserID 冲突 | **已关闭：以 session.user_id 为准** |
| Get 在无 ScopeID 时 | **not_found / skipped，非 error code scope_not_enabled** |

无阻塞开放问题 → 可进入实现计划。
