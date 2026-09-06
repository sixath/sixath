# S1 死代码清扫 + Hub 管理面退出外壳

**日期**: 2026-09-05  
**状态**: 已确认（设计评审，2026-09-05）  
**范围**: 行为消失。本文件**不改包名**。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**顺序**: S1 → [S2 context/PromptBuilder](./2026-09-05-context-promptbuilder-design.md) → [S3 harness/workspace 搬家](./2026-09-05-harness-workspace-rename-design.md)

**一句话**: 删掉 P1–P4 拆行为后仍留在默认路径周边的 turn-surface 残骸、零调用死函数、Portal Hub HTTP/UI，以及 procedural 预取注入；framework 的 Hub / Growth / MEA **包不删、不重写**。

---

## 1. 背景

P1–P4 已把领域闸、调查闸、MEA 旁路、Growth 主循环钩子移出默认 Run。磁盘上 P3 闸文件（`turn_surface.go`、`turn_intent_gate.go`、`skill_router.go` 等）已不存在。仍活着的是：

- `ActiveFamilies` 表面过滤字段（发送路径已不再传入，nil = 全开）
- `SATH_TURN_TOOL_SURFACE` / `ToolSurfaceEnabled`
- `queryWithSchemaHeal`（`execute_read` 已直接 `reader.Query`）
- Hub：`WireMemoryHubFromData` / `InitLocalMemoryHub`、HTTP `/memory-hub/*` 与 `/agents/{id}/hub/*`、Web Loadout/Binding
- Procedural：`memory_prefetch_bootstrap` 仍 `ProceduralBindingsForPrefetch`

父规格 P4 只要求 Hub 退出默认 Run。本 spec **升级**：Hub **管理面**也退出产品外壳。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 工具面 | 删每轮截肢 API；保留族名标签（catalog / ES 绑定 / 并行工具判断） |
| 死函数 | 删确认无默认调用者的包装函数；器官细节库函数留下 |
| Hub HTTP | 拆除路由与 Portal 接线；`framework/memory/hub` 包保留 |
| Hub Web | Agent 详情与创建表单去掉 Loadout / Binding / 知识草稿；不再调用 `memoryHubApi` |
| Procedural | Portal 预取默认路径不再注入；删无引用的 `procedural_binding.go`；`framework/memory` procedural 实现留包。**S15：连 extra catalog / auto-commit 接线一并拆掉** |
| Growth | 不删 `framework/growth`；未接线的 `registerGrowthSessionHooks` 可留作可选器官入口 |

---

## 3. CUT

### 3.1 工具面残骸

- `RegistryBuildOptions.ActiveFamilies`、`filterToolsForSurface`、`filterServersForSurface`
- `AgentRuntimeToolsOptions.ActiveFamilies`；注册 skills/memory/web/knowledge 只看 Hermes flags / 绑定，不再 `FamilyActive(ActiveFamilies, …)`
- `McpExpandOnMissOptions.ActiveFamilies`
- `SATH_TURN_TOOL_SURFACE`、`ToolSurfaceEnabled`、`SetTurnToolSurfaceEnabled`
- 测试：`tool_families` 的 surface 开关、`registry_surface_test`、`runtime_tools_surface_test`、builder 里传入 ActiveFamilies 的用例

**KEEP**：`FamilyCode` / `FamilyRCA` 等常量，以及仍被 catalog、ES 绑定、`ShouldEnableParallelTools` 使用的分类逻辑。

### 3.2 零调用死函数

- **删** `queryWithSchemaHeal`（`framework/tool/data/execute_read.go`）
- **留** `HealReadSQL`、`MaybeSpill`（`result_stats` 仍 spill）

### 3.3 Hub 管理面

- 启动路径不再调用 `WireMemoryHubFromData` / `InitLocalMemoryHub` / `SetHubBindingStore`（默认 Chat 构造）
- 删除路由（`portal/internal/server/http.go` 及 handler）：
  - `GET /api/v1/memory-hub/catalog`
  - `GET/POST/DELETE /api/v1/agents/{agent_id}/hub/bindings`
  - `GET /api/v1/agents/{agent_id}/hub/loadout`
  - `POST .../hub/bindings/clear`、`.../hub/assets/status`
  - `GET .../hub/knowledge/drafts`、`POST .../hub/knowledge/approve`
- 删除（及测试）：`portal/internal/server/memory_hub*.go`、`portal/internal/service/hub_*.go`、`portal/internal/chat/hub_*.go`
- 仅被上述接线引用的 `portal/internal/data/binding_store_mysql.go` 一并删除；若还有非 Hub 引用则只拆调用
- Web：`AgentDetail.tsx` / `AgentForm.tsx` 去掉 Hub UI；`memoryHubApi` 倾向从 `web/src/api/client.ts` 删除，避免假入口
- **KEEP**：`framework/memory/hub`

### 3.4 Procedural（portal）

- 预取装配不再设置 `ProceduralBindings`
- 删除 `portal/internal/chat/procedural_binding.go` 及测试（无剩余引用时）
- 相关 e2e / `p3e_live_auto_commit.go` 若只测 procedural 注入：删除或改为断言「默认不注入」
- **KEEP**：`framework/memory` 内 procedural 类型与 five-gate 实现

---

## 4. 非目标

- 不改 `framework/agent` 包名（S3）
- 不引入 PromptBuilder（S2）
- 不重写 Hub / Growth / MEA
- 不迁移已有整仓 Agent（P2 waiver 仍有效）
- 不把 Insights 路由从 `App.tsx` 删掉（P4 已藏导航即可）

---

## 5. 成功标准

1. `BuildRegistry` / `RegisterAgentRuntimeTools` 无 `ActiveFamilies`；默认 registry = 绑定全集。
2. 进程启动不 `InitLocalMemoryHub`；Hub HTTP 不注册。
3. Agent 详情 / 表单无 Hub Loadout、Binding、知识草稿。
4. 默认 Stream / Prefetch 无 procedural 注入。
5. 新建 Agent 仍有默认可写 workspace；`harness/hooks.yaml` + FailureCapture 仍挂在 ReAct extra。
6. `cd framework && go test ./agent ./tool ./tool/data ./model ./events ./mea -count=1` 绿。  
   `cd portal && go test ./internal/chat/... ./internal/service/... ./internal/conf/... ./internal/biz/... ./cmd/backend -count=1` 绿（可 skip 预存在的 `TestNotifySessionMessageIndexed_WithDetachedCaller` 与 `TestSearchSessionsWithAgentFilterRequiresAgentUse`）。

禁止把 `_neo4j_q/` 当夹具。
