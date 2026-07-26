# MemoryStore Facade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 `MemoryStore` 门面统一长期记忆读写（`scope=user|session|agent`），Portal/Prefetch/工具只经门面；一期 Session+Agent 真实可用，User stub，破坏性三工具更名。

**Architecture:** Framework 定义 `MemoryStore` + facade 路由；Session units 的 MySQL 实现在 Portal；`memorysearch` / `sessionsearch` 降为内部 Backend；`Orchestrator` 经 `StorePrefetchBackend` 预取；旧 `memory`/`memory_search`/`session_search` 工具删除。

**Tech Stack:** Go、MySQL（Portal）、SQLite FTS（既有 memorysearch/sessionsearch）、framework `tool.Registry`。

**依据规范:** 所有 `go` 命令在 `framework/` 或 `portal/` 下执行；密钥本期无新增。

**Spec:** `docs/superpowers/specs/2026-07-25-memory-store-facade-design.md`

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/memory/store.go` | Scope、错误、输入输出类型、`MemoryStore` 接口；**以及** `SessionUnitsBackend` / `AgentWorkspaceBackend` / `TranscriptBackend` 接口（避免 `memory↔backend` import cycle） |
| `framework/memory/facade.go` | 组合 backends 的默认 `MemoryStore`；`NewFacade` / `FacadeConfig` |
| `framework/memory/facade_test.go` | User stub、路由、session 软删语义 |
| `framework/memory/session_memory.go` | 内存 `SessionUnitsBackend`（单测 / 无 DB） |
| `framework/memory/agent_workspace.go` | 包装 `memorysearch` 的 `AgentWorkspaceBackend` 实现 |
| `framework/memory/session_transcript.go` | 包装 `sessionsearch` 的 `TranscriptBackend` 实现 |
| `framework/memory/fileedit.go` | MEMORY.md / USER.md 原子写与 add/replace/remove |
| `framework/memory/store_prefetch_backend.go` | 实现 `Backend`：双路 Recall + 合并 parts |
| `framework/memory/manager.go` | 文件头 `Deprecated` 注释 |
| `framework/tool/memory/store_tools.go` | `RegisterMemoryStoreTools`：三新工具 |
| `framework/tool/memory/store_tools_test.go` | 工具契约 |
| `framework/tool/memory/memory_tool.go` 等 | 删除或改为调用 store（最终删除旧注册入口） |
| `portal/migrations/009_memory_units.sql` | `memory_units` 表 |
| `portal/internal/data/memory_units_mysql.go` | MySQL SessionUnits |
| `portal/internal/data/memory_units_mysql_test.go` | CRUD / LIKE / 软删 |
| `portal/internal/chat/memory_wiring.go` | 组装 Store + 注册新工具 |
| `portal/internal/chat/memory_prefetch_bootstrap.go` | `StorePrefetchBackend` |
| `portal/internal/chat/runtime_tools.go` | 改用新注册；agent write flag |
| `portal/internal/service/chat.go` | 去掉独立的 `RegisterSessionSearchTools`；注入 `ContextKeySessionID`；组装 Store 所需 DB/provider |
| `portal/internal/chat/hermes_p0_flags_test.go` 等 | 期望工具名从旧 `memory`/`session_search` 改为新三件套 |
| `portal/docs/memory-integration.md` | 按规格重写 |
| `framework/docs/toolsets-hermes-mapping.md` | 工具名映射更新 |

---

### Task 1: MemoryStore 类型与错误

**Files:**
- Create: `framework/memory/store.go`
- Create: `framework/memory/store_types_test.go`

- [ ] **Step 1: 写失败测试**

```go
package memory

import "testing"

func TestScopeConstants(t *testing.T) {
	if ScopeUser != "user" || ScopeSession != "session" || ScopeAgent != "agent" {
		t.Fatalf("unexpected scope constants")
	}
}

func TestErrScopeNotEnabled(t *testing.T) {
	if ErrScopeNotEnabled == nil || ErrScopeNotEnabled.Error() == "" {
		t.Fatal("ErrScopeNotEnabled must be set")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./memory/ -run 'TestScopeConstants|TestErrScopeNotEnabled' -count=1
```

Expected: FAIL（符号不存在）

- [ ] **Step 3: 实现 `store.go`**

按 spec §3.4 定义：`Scope`、`ErrScopeNotEnabled`、`ErrNotSupported`、`RememberAction`、`RememberInput`（含 `Target`）、`RecallSource`、`RecallQuery`、`MemoryHit`、`ListFilter`、`GetRef`、`MemoryStore` 接口。导出 `ContentHash(content string) string`（SHA-256 hex）。  

**同文件或 `backends.go`** 写出三个 Backend 接口完整签名（实现者不得另发明）：

```go
type SessionUnitsBackend interface {
    Remember(ctx context.Context, in RememberInput) (MemoryHit, error)
    Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
    Get(ctx context.Context, ref GetRef) (MemoryHit, error)
    List(ctx context.Context, filter ListFilter) ([]MemoryHit, error)
    Delete(ctx context.Context, ref GetRef) error
}
type AgentWorkspaceBackend interface {
    Remember(ctx context.Context, in RememberInput) (MemoryHit, error)
    Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
    Get(ctx context.Context, ref GetRef) (MemoryHit, error)
}
type TranscriptBackend interface {
    Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error)
}
```

**禁止**另建 `memory/backend` 子包承载接口。

- [ ] **Step 4: 运行确认通过**

```bash
cd framework
go test ./memory/ -run 'TestScopeConstants|TestErrScopeNotEnabled' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add framework/memory/store.go framework/memory/store_types_test.go
git commit -m "feat(memory): add MemoryStore types and scope errors"
```

---

### Task 2: 内存 SessionUnits + User stub + Facade

**Files:**
- Create: `framework/memory/backends.go`（若 Task 1 未写全三个 Backend 接口，此处补齐）
- Create: `framework/memory/session_memory.go`
- Create: `framework/memory/facade.go`
- Create: `framework/memory/facade_test.go`

- [ ] **Step 1: 写失败测试（facade_test.go）**

覆盖：
1. `Remember(scope=user)` → `ErrScopeNotEnabled`
2. `Remember(scope=session, add)` → hit 含 id；`Recall(source=units, query)` LIKE 命中
3. `Remember(replace)` 原地更新 content；`remove` 后 Recall 不返回；`Get` 对 deleted 返回 not found
4. `List(scope=agent)` → `ErrNotSupported`；`Delete(scope=agent)` → `ErrNotSupported`

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./memory/ -run TestFacade -count=1
```

Expected: FAIL

- [ ] **Step 3: 实现内存 Session + facade**

```go
type FacadeConfig struct {
    Session    SessionUnitsBackend    // required for session scope
    Agent      AgentWorkspaceBackend  // optional until Task 4
    Transcript TranscriptBackend      // optional until Task 4
}
func NewFacade(cfg FacadeConfig) *Facade
```

`Agent`/`Transcript` 为 nil 时：对该 source/scope 返回空 hits 或明确错误（agent Remember → error `agent backend not configured`）。  
User：所有方法 → `ErrScopeNotEnabled`。

Session 内存 backend：map + mutex；实现 spec 软删 / 原地 replace / LIKE（`strings.Contains` 即可）/ 空 query 最近 N 条。  
**包规则**：所有接口与 Facade 均在 `package memory`；实现文件也在同包或仅 Portal 实现 `SessionUnitsBackend`（Portal 可 import `memory`，`memory` 不 import Portal）。

- [ ] **Step 4: 测试通过**

```bash
cd framework
go test ./memory/ -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add framework/memory/
git commit -m "feat(memory): facade with in-memory session units and user stub"
```

---

### Task 3: StorePrefetchBackend

**Files:**
- Create: `framework/memory/store_prefetch_backend.go`
- Create: `framework/memory/store_prefetch_backend_test.go`
- Modify: `framework/memory/manager.go`（文件注释 Deprecated）

- [ ] **Step 1: 写失败测试**

注入 fake `MemoryStore`：session+agent 各返回一段 content；断言 `Prefetch` 得到 parts；Store 错误时返回 error（由 Orchestrator fail-open）。空 query → 空 parts。

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./memory/ -run TestStorePrefetch -count=1
```

- [ ] **Step 3: 实现**

```go
type StorePrefetchBackend struct {
    Store       MemoryStore
    MaxSnippets int
}
func (b *StorePrefetchBackend) Name() string { return "memory_store_prefetch" }
```

`Prefetch`：对 `session/units` 与 `agent/files` 各 `Recall`（Limit=`MaxSnippets` 或 5），合并为 `[]PrefetchPart`（Label 区分 session/agent）。保留既有 `SearchPrefetchBackend` 直至 Portal 切换完成（下一任务再标 Deprecated）。

- [ ] **Step 4: 测试通过 + Commit**

```bash
cd framework
go test ./memory/ -count=1
git add framework/memory/
git commit -m "feat(memory): StorePrefetchBackend for Orchestrator"
```

---

### Task 4: Agent workspace + Session transcript adapters

**Files:**
- Create: `framework/memory/fileedit.go`
- Create: `framework/memory/agent_workspace.go`
- Create: `framework/memory/session_transcript.go`
- Create: `framework/memory/agent_workspace_test.go`
- Create: `framework/memory/session_transcript_test.go`

- [ ] **Step 1: 写失败测试**

Agent：用 temp dir + 最小 fake 或真实 `memorysearch` builtin（若过重则 interface 注入 `SearchFunc`/`WriteFunc`）。至少测：`Remember(add)` 写出 `MEMORY.md`；`target=user_file` → `USER.md`；写成功后调用 `Sync`（或等价），`Recall` 能搜到。  
Transcript：注入 fake searcher；空 query 拒绝（error 或空+error，与现 `session_search` 一致）。

- [ ] **Step 2–4: 实现适配器并测试**

Agent `Remember` 复用现 `tool/memory` 中 `applyMemoryAction` / 原子写逻辑——**抽出**到 `framework/memory/fileedit.go`（`tool/memory` 可改为调用或删除重复代码，避免 `memory` → `tool` 依赖）。写文件成功后必须 `memorysearch.Sync(reason=memory_tool)`（与现网一致），否则 Recall 测不过。

- [ ] **Step 5: Commit**

```bash
git add framework/memory/
git commit -m "feat(memory): agent workspace and session transcript backends"
```

---

### Task 5: 新三工具 `RegisterMemoryStoreTools`

**Files:**
- Create: `framework/tool/memory/store_tools.go`
- Create: `framework/tool/memory/store_tools_test.go`
- Modify: `framework/tool/toolset.go`（若有默认 toolset 映射）  
  （`RememberInput.Target` 已在 Task 1 定义，本任务只接线工具参数）

- [ ] **Step 1: 写失败测试**

注册后 `reg.Get("memory_remember")` 等存在；Execute：
- user scope → JSON `error` + `code=scope_not_enabled`（spec §7.3）
- session add：工具参数**不传** `session_id`；从 `ctx` 的 `tool.ContextKeySessionID`（若无此 key，在 Task 5 前于 `framework/tool` 增加并与 Portal 注入对齐）填充 `RememberInput.ScopeID`；注入 facade → ok
- agent `target=user_file` → 写入 `USER.md`（非 `scope=user`）
- agent `target=memory` → `MEMORY.md`
- 无 workspace 的 agent → `workspace_root_missing`
- `memory_get(scope=agent, path=../secret.md)` 或非允许路径 → 拒绝；允许 `MEMORY.md` / `USER.md` / `memory/**/*.md` / 配置 `extra_paths`

- [ ] **Step 2–4: 实现**

```go
func RegisterMemoryStoreTools(reg *tool.Registry, store memory.MemoryStore, opts StoreToolsOptions) error
```

`StoreToolsOptions`：`AgentWriteEnabled bool`（false 时 agent remember 返回 disabled）；可选 `ExtraPaths []string` 供 `memory_get` 白名单。

**Context 绑定（强制）**：工具 JSON **不**要求调用方传 `session_id` / `agent_id` / `workspace_root`；Execute 内从 context 读取：
- `tool.ContextKeyWorkspaceRoot`
- `tool.ContextKeyAgentID`
- `tool.ContextKeySessionID`（Task 5 若缺失则新增常量；Portal `chat.go` 在 `a.Run` 前注入，与 workspace/agent 同级）

`memory_remember` 参数含 `target`（enum `memory`|`user_file`，仅 scope=agent 有意义）。

**不要**在此任务删除旧 `RegisterMemoryWriteTool` / `RegisterMemorySearchTools`（Portal 切换时再删）。

- [ ] **Step 5: Commit**

```bash
git add framework/tool/memory/ framework/tool/toolset.go framework/memory/store.go
git commit -m "feat(memory): register memory_remember/recall/get tools"
```

---

### Task 6: Portal `memory_units` 迁移 + MySQL backend

**Files:**
- Create: `portal/migrations/009_memory_units.sql`
- Create: `portal/internal/data/memory_units_mysql.go`
- Create: `portal/internal/data/memory_units_mysql_test.go`
- Create: `portal/internal/data/memory_units_backend.go`（实现 `memory.SessionUnitsBackend`）

- [ ] **Step 1: 写迁移 SQL**（严格按 spec §4.1）

- [ ] **Step 2: 写 data 层测试**（跟现有 portal data 测试同一套 DB helper；若无集成 DB 则用 sqlmock 或 skip tag）

覆盖：Insert、Replace 原地、软删、LIKE、空 query 最近 N、`source_session_id=scope_id`、`content_hash`。

- [ ] **Step 3: 实现 MySQL repo + `SessionUnitsBackend` 适配**

- [ ] **Step 4: 测试**

```bash
cd portal
go test ./internal/data/ -run MemoryUnit -count=1
```

- [ ] **Step 5: Commit**

```bash
git add portal/migrations/009_memory_units.sql portal/internal/data/memory_units_*.go
git commit -m "feat(portal): memory_units MySQL store for session scope"
```

---

### Task 7: Portal wiring + Prefetch 切换

**Files:**
- Modify: `portal/internal/chat/memory_wiring.go`
- Modify: `portal/internal/chat/memory_prefetch_bootstrap.go`
- Modify: `portal/internal/chat/runtime_tools.go`
- Modify: `portal/internal/chat/session_search.go`（删除或停止导出 `RegisterSessionSearchTools`）
- Modify: `portal/internal/service/chat.go` — **必须**移除 `RegisterSessionSearchTools` 调用（现网在 SendMessage/Stream **单独**注册，不经 `RegisterAgentRuntimeTools`）
- Modify: `portal/internal/chat/hermes_p0_flags_test.go` 及所有断言旧工具名的测试
- Modify: 任何 `RegisterMemorySearchTools` / `RegisterSessionSearchTools` 调用点

- [ ] **Step 1: 写/改 wiring 测试**

断言完整注册路径（与生产一致：`RegisterAgentRuntimeTools` **加上**原 `chat.go` 里会挂工具的那段）之后存在 `memory_remember`/`memory_recall`/`memory_get`，**不存在** `memory`、`memory_search`、`session_search`。

另测：`ContextKeySessionID` 在 `a.Run` 前注入（可与现有 workspace/agent context 测试并列）。

- [ ] **Step 2: 实现组装**

```go
store := memory.NewFacade(memory.FacadeConfig{
    Session:    data.NewSessionUnitsBackend(db),
    Agent:      memory.NewAgentWorkspace(...),
    Transcript: memory.NewSessionTranscript(...),
})
toolmem.RegisterMemoryStoreTools(reg, store, opts)
```

Prefetch：`memory.NewStorePrefetchBackend(store, 5)` 注册到 Orchestrator。

`MemoryWriteEnabled` **仅**控制 agent 文件写。  
`session_search` 能力仅通过 `memory_recall(scope=session, source=transcript)`，**禁止**再单独 Register。

- [ ] **Step 3: 测试**

```bash
cd portal
go test ./internal/chat/ ./internal/service/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/chat/ portal/internal/service/chat.go
git commit -m "feat(portal): wire MemoryStore facade and new memory tools"
```

---

### Task 8: 删除旧工具注册入口

**Files:**
- Modify/Delete: `framework/tool/memory/memory_tool.go` 导出的旧 Register（若仍被测试引用则删测试或改测新工具）
- Modify: `framework/tool/memory/memory_search.go`、`session_search.go` — 移除 `Register*` 或标内部-only
- Modify: `framework/docs/toolsets-hermes-mapping.md`
- Modify: `portal/docs/memory-integration.md`（按 spec 重写）
- Modify: `framework/memory/search_prefetch_backend.go` — Deprecated 或删除（确认无引用后删）

- [ ] **Step 1: ripgrep 确认无残留注册 / 旁路**

```bash
rg "RegisterMemoryWriteTool|RegisterMemorySearchTools|RegisterSessionSearchTools|Name:\\s+\"memory\"|Name:\\s+\"memory_search\"|Name:\\s+\"session_search\"" framework portal -g '*.go'
rg "memorysearch\"|sessionsearch\"" portal/internal -g '*.go'
```

旧工具名不应再被 Register。除 `portal/internal/chat/memory_wiring.go` / `memory_prefetch_bootstrap.go` / transcript provider 外，Portal 业务包不应直接 import `memorysearch`/`sessionsearch`。

配置：一期继续读 `MemoryWriteEnabled` / `DefaultMemoryConfig` 映射到 Store 组装；**不要求**落地完整 `memory_store:` YAML 重塑（可二期）。

- [ ] **Step 2: 更新文档**（migration 表 + 新工具 + 二期清单摘要）；明确 `memory_get` agent 路径白名单；旧 `target=user` → 新 `target=user_file`

- [ ] **Step 3: 全量相关测试**

```bash
cd framework && go test ./memory/... ./tool/memory/... -count=1
cd portal && go test ./internal/chat/... ./internal/data/... -count=1
```

- [ ] **Step 4: Commit**

```bash
git add framework portal/docs
git commit -m "breaking(memory): remove legacy memory tools; docs for MemoryStore facade"
```

---

### Task 9: 冒烟验收清单（手工）

- [ ] Agent 开聊：Prefetch 不报错（可关索引仍 fail-open）
- [ ] `memory_remember(scope=session, action=add)` → DB 有行
- [ ] `memory_recall(scope=session, source=units)` 命中
- [ ] agent 写 flag off → remember agent 拒绝；flag on → `MEMORY.md` 更新且 recall files 可命中
- [ ] `scope=user` → `scope_not_enabled`
- [ ] 旧工具名调用失败（未注册）

勾选后在 PR 描述粘贴结果；无需新 commit，除非修 bug。

---

## 风险与注意

- **import cycle**：`tool/memory` → `memory` OK；`memory/backend` 勿 import `tool`。文件编辑逻辑放 `memory` 包。
- **破坏性发布**：PR 含 Task 7–8 时写 release note（工具映射表）。
- **二期**：勿在本期实现 `AddFromTurn` / users / Qdrant；规格 §8 已记账。

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-25-memory-store-facade.md`. Two execution options:

1. **Subagent-Driven (recommended)** — 每任务新 subagent，任务间 review  
2. **Inline Execution** — 本会话按 executing-plans 批量推进并设检查点  

Which approach?
