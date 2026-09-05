# S1 Dead Code + Hub Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删掉 turn-surface 残骸、`queryWithSchemaHeal`、Portal Hub HTTP/UI、以及默认预取里的 procedural 注入；不删 `framework/memory/hub`、`growth`、`mea` 包。

**Architecture:** 默认 registry = 绑定全集（无 `ActiveFamilies`）。Chat 构造与 HTTP 不再 Init Hub。Prefetch backend 不再挂 `ProceduralBindings`。`HealReadSQL` / `MaybeSpill` / Family 常量留下。

**Tech Stack:** Go（portal chat/service/server、framework/tool/data）、React（`AgentDetail.tsx`、`AgentForm.tsx`）

**规格:** [`2026-09-05-dead-code-hub-off-design.md`](../specs/2026-09-05-dead-code-hub-off-design.md)

**分支:** 从 `feature/portal-assembler` 切 `feature/s1-dead-code-hub-off`。不要在 `main` 上改。PowerShell 无 HEREDOC：`git commit -m "..."`。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 拆表面过滤 | `portal/internal/chat/agent_builder.go`、`runtime_tools.go`、`mcp_expand.go`、`tool_families.go` |
| 改/删测试 | `tool_families_test.go`（surface 开关）、`registry_surface_test.go`、`runtime_tools_surface_test.go`、`agent_builder_test.go` 中 ActiveFamilies 用例 |
| 删 heal 包装 | `framework/tool/data/execute_read.go` 的 `queryWithSchemaHeal` |
| 拆 Hub 启动 | `portal/internal/service/chat.go` `ProvideChatServiceWithTurnTrace` |
| 删 Hub HTTP | `portal/internal/server/http.go` 路由；`git rm` `server/memory_hub*.go`、`service/hub_*.go`、`chat/hub_*.go` |
| 删 BindingStore 接线 | `git rm` `portal/internal/data/binding_store_mysql.go` + test（仅 Hub 引用） |
| 拆 knowledge 工具注册 | `runtime_tools.go` 去掉 `RegisterKnowledgeHubTools`（定义在即将删除的 `hub_knowledge_tools.go`） |
| Hub Web | `web/src/pages/AgentDetail.tsx`、`AgentForm.tsx`、`web/src/api/client.ts` 的 `memoryHubApi` |
| Procedural 预取 | `memory_prefetch_bootstrap.go`；**保留** `procedural_binding.go`（config + FailureSignalSink）；删预取/skill_router 注入函数；`procedural_e2e_test.go` 改断言；`p3e_live_auto_commit.go` 改写或 `git rm` |

禁止：删 `framework/memory/hub`、`framework/growth`、`framework/mea`；改包名（S3）；引入 PromptBuilder（S2）；删 Insights 路由。

---

### Task 1: 工具面残骸

**Files:** `portal/internal/chat/agent_builder.go`、`runtime_tools.go`、`mcp_expand.go`、`tool_families.go` 及对应测试

- [ ] **Step 1:** 从 `feature/portal-assembler` 切 `feature/s1-dead-code-hub-off`，`SetActiveBranch`。
- [ ] **Step 2:** 写失败测试：`BuildRegistry` 不再接受/使用 `ActiveFamilies`（调用处只传 Workspace）。`RegisterAgentRuntimeTools` 在 `ActiveFamilies` 为零值时仍按 flags 注册 skills/web（与今日 nil=全开一致），且 **不再**调用 `RegisterKnowledgeHubTools`（Task 3 会删该函数；本步可先改测试期望「knowledge 工具不因 FamilyKnowledge 注册」或与 Task 3 同一提交）。
- [ ] **Step 3:** 删除：
  - `RegistryBuildOptions.ActiveFamilies`
  - `filterToolsForSurface` / `filterServersForSurface` 及 `BuildRegistry` 里对它们的调用
  - `AgentRuntimeToolsOptions.ActiveFamilies`；`registerSkills` / `registerMemory` / web / knowledge 改为只看 flags（skills/memory：flags 或现有 Hermes 开关；web：`flags.WebToolsEnabled`；**不要**再 `FamilyActive(opts.ActiveFamilies, …)`）
  - `McpExpandOnMissOptions.ActiveFamilies` 及 expander 内族过滤
  - `SATH_TURN_TOOL_SURFACE`、`turnToolSurfaceOverride`、`SetTurnToolSurfaceEnabled`、`ToolSurfaceEnabled`
- [ ] **Step 4:** KEEP `FamilyCode` / `FamilyRCA` / `FamilyForBuiltinToolName` / `ToolFamilySplitEnabled`（若 catalog 或 ES 绑定仍用）。删只为表面开关服务的测试。
- [ ] **Step 5:** `cd portal && go test ./internal/chat/... -count=1`（此时 Hub 文件仍在，knowledge 注册若已断开需与 Task 3 一起绿）。
- [ ] **Step 6:** Commit：`fix(portal): drop turn-surface ActiveFamilies and tool-surface env`

---

### Task 2: 删除 `queryWithSchemaHeal`

**Files:** `framework/tool/data/execute_read.go`

- [ ] **Step 1:** 确认无调用者。PowerShell 用：`git grep -E "queryWithSchemaHeal"`（不要写裸 `|`，会被当成管道）。
- [ ] **Step 2:** 删除函数 `queryWithSchemaHeal`（及仅被它使用的私有 helper）。**留** `HealReadSQL`。
- [ ] **Step 3:** `cd framework && go test ./tool/data -count=1`
- [ ] **Step 4:** Commit：`fix(tool): remove unused queryWithSchemaHeal wrapper`

---

### Task 3: Hub 管理面退出

**Files:** 见 file map Hub 行

- [ ] **Step 1:** 红测：`RegisterAgentRuntimeTools` 后 registry 无 `knowledge_*`；`ProvideChatServiceWithTurnTrace` 不再调用 Hub Init（可用测试替身或 grep 夹具）。
- [ ] **Step 2:** `ProvideChatServiceWithTurnTrace` 去掉 `WireMemoryHubFromData(d)`。`runtime_tools.go` 删除 `RegisterKnowledgeHubTools` 块。`http.go` 删除 Hub 路由。
- [ ] **Step 3:** `git grep -E "InitLocalMemoryHub|WireMemoryHubFromData|RegisterKnowledgeHubTools"` 生产路径清零后 `git rm`：
  - `portal/internal/server/memory_hub.go`
  - `portal/internal/server/memory_hub_assets.go`
  - `portal/internal/server/memory_hub_knowledge.go`
  - `portal/internal/service/hub_wire.go`
  - `portal/internal/service/hub_assets.go`
  - `portal/internal/service/hub_knowledge.go`
  - `portal/internal/chat/hub_*.go`（含测试）
  - `portal/internal/data/binding_store_mysql.go` + `_test.go`
- [ ] **Step 4:** Web：去掉 `AgentDetail` 的 `data-testid="hub-loadout-section"` 整段及 `memoryHubApi.*` 调用/state。`AgentForm` 去掉 `memoryHubApi.catalog` / `clearBindings` 及治理面变更提示里对 Binding 的依赖。从 `web/src/api/client.ts` 删除 `memoryHubApi` 及仅被它使用的 Hub 类型。Agent `runtime_tools.hub_governance` **显示字段可留**（不是 Loadout API）。
- [ ] **Step 5:** `cd portal && go test ./internal/chat/... ./internal/service/... ./internal/server/... ./cmd/backend -count=1 -skip "TestNotifySessionMessageIndexed_WithDetachedCaller|TestSearchSessionsWithAgentFilterRequiresAgentUse"`
- [ ] **Step 6:** Commit：`feat(portal): remove memory-hub HTTP and web loadout UI`

**KEEP:** `framework/memory/hub` 整包。

---

### Task 4: Procedural 退出默认预取

**Files:** `portal/internal/chat/memory_prefetch_bootstrap.go`、`procedural_binding.go`（**保留文件**）、相关测试

**不要** `git rm procedural_binding.go`：`SetProceduralRepairConfig` 与 `DefaultFailureSignalSink` 仍被 `portal_agent_extra.go`、`service/chat.go` 使用（FailureCapture 免疫系统 KEEP）。

- [ ] **Step 1:** 红测：`BuildPrefetchMemoryOrchestrator` 返回的 backend `ProceduralBindings` 为空 / 不设 `LoadPersistedProcedural`（即使 catalog 有 binding）。
- [ ] **Step 2:** 删除 `BuildPrefetchMemoryOrchestrator` 里 `ProceduralBindingsForPrefetch` 分支。
- [ ] **Step 3:** 删除仅服务预取/skill_router 的函数（`ProceduralBindingsForPrefetch`、`ProceduralBindingsForTurn`、`proceduralPrefetchEnabled`、`ProceduralBindingsForSkillRouter*`、`appendProceduralBindingHints`）及只测 hint 注入的用例。保留 config + FailureSignalSink + auto-commit 相关。
- [ ] **Step 4:** `procedural_e2e_test.go` 里依赖预取注入的用例改为断言不注入，或缩小为 sink/catalog 单测。`portal/scripts/p3e_live_auto_commit.go`：删除对已删函数的调用或整文件 `git rm`（脚本不在默认 `go test` 里，但必须能 `go build`）。
- [ ] **Step 5:** KEEP `framework/memory` procedural 类型。
- [ ] **Step 6:** `cd portal && go test ./internal/chat/... -count=1`
- [ ] **Step 7:** Commit：`fix(portal): stop procedural injection on default prefetch`

若 `portal/docs/memory-integration.md` 仍写 Hub HTTP / Loadout，改一句「管理面已拆除，包保留」。

---

### Task 5: 回归

```
cd framework && go test ./agent ./tool ./tool/data ./model ./events ./mea -count=1
cd portal && go test ./internal/chat/... ./internal/service/... ./internal/conf/... ./internal/biz/... ./cmd/backend -count=1 -skip "TestNotifySessionMessageIndexed_WithDetachedCaller|TestSearchSessionsWithAgentFilterRequiresAgentUse"
```

验收：

- `BuildRegistry` / `RegisterAgentRuntimeTools` 无 `ActiveFamilies`
- 无 `InitLocalMemoryHub` 生产调用；Hub 路由不注册
- Agent 详情无 `hub-loadout-section`
- 默认 Prefetch 不设 `ProceduralBindings`
- 新建 Agent 仍有 workspace；`growthReActOptions` 仍加载 `hooks.yaml` + FailureCapture

UI：Hub 区块是用户可见行为，完成后用浏览器打开 Agent 详情确认 Loadout/Binding 已消失（登录与 dev server 可用时）。
