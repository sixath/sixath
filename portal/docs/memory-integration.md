# Portal MemoryStore 集成指南

Portal 的长期记忆统一通过 framework 的 `memory.MemoryStore` 门面访问。它取代了并行的 `memory`、`memory_search` 与 `session_search` 工具叙事；工作记忆（`BufferMemory` 与 L0/L1/L2 上下文压缩）仍是独立能力，不属于 MemoryStore。

## Scope 与后端

| scope | 后端 | 可用能力 |
|---|---|---|
| `session` | MySQL `memory_units` | 结构化事实的写入、读取与关键词召回 |
| `user` | MySQL `memory_units`（同表） | 跨会话用户偏好/稳定事实的工具读写与 Prefetch 召回 |
| `agent` | 工作区 `MEMORY.md`、`USER.md`、`memory/*.md` | 文件记忆的读、写（opt-in）与检索 |

`USER.md` 是 agent 工作区中的文件，**不是** `scope=user`。`scope=user` 存于 MySQL `memory_units`（`scope_type='user'`，`scope_id = user_id`），与 agent 文件层无关。

缺 `user_id` 时，`scope=user` 的工具调用与 Prefetch **静默跳过**（不写库、不报错、不出现 `scope_not_enabled`）。`memory_remember(scope=user)` 可能返回 `{"skipped": true, "reason": "user_id_missing"}` 便于观测，但 `error` 字段不会出现。

Portal 仅在接线层将 `memorysearch` 和 `sessionsearch` 适配为 MemoryStore 的 agent/transcript 后端；业务调用、Agent 工具与 Prefetch 都通过门面。

## 三个 Agent 工具

| 工具 | 作用 | 主要参数 |
|---|---|---|
| `memory_remember` | 写入 session/user unit 或 agent 文件 | `scope`、`action`、`content`；session/user replace/remove 使用 `unit_id`，agent replace/remove 使用 `old_text` |
| `memory_recall` | 召回 session/user unit、跨会话转录或 agent 文件 | `scope`、`query`、`source`、`limit`、`min_score`；`scope=agent`+`source=files` **排除** `sessions/` 转录噪声，优先 `MEMORY.md` / `USER.md` / `memory/**` |
| `memory_get` | 读取单条 session/user unit 或允许的 agent 记忆文件 | `scope`；session/user 用 `id`，agent 用 `path` |

`memory_remember(scope=session)` 与 `memory_remember(scope=user)` 默认可用（后者需有效 `user_id`）。Agent 文件写入由进程级 `memory_store.agent_workspace.write_enabled` / env **`SATH_AGENT_MEMORY_WRITE_ENABLED`**，以及 Agent `runtime_tools.memory_write_enabled`（OR 合并）控制。旧 env `SATH_MEMORY_WRITE_ENABLED` **已移除**。

推荐在 `agent_extra.yaml` 使用 `memory_store:` 嵌套块（P2-G）；与顶层 `memory_*` 并存时以 `memory_store` 为准。顶层旧键仍可读（无嵌套覆盖时），文档视为 deprecated。

`memorysearch` / `sessionsearch` **保留为内部实现**（agent 文件索引 / transcript FTS），不对外暴露、不物理删除、不合并进 units。

Agent 文件写入只接受：

| target | 文件 |
|---|---|
| `memory` | `MEMORY.md` |
| `user_file` | `USER.md` |

模型仍可主动调用 `memory_remember` 持久化。另见下文「Turn 后提取」：默认关闭的可选后台路径。`workspace_root`、`agent_id`、`session_id` 与 `user_id`（有则注入）必须在运行 Agent 前置入 context。

## Supersede 语义（P2-D1）

`scope=session|user` 的 `action=replace` 不再原地 UPDATE，而是 **supersede 链**：

1. 新建一行 `status=active`，`supersedes_id` 指向被替换的旧 id。
2. 旧行改为 `status=superseded`。
3. 成功响应返回 **新** `id`（**breaking**：缓存旧 id 的调用方须更新）。

`action=remove` 与 `MemoryStore.Delete` 共用同一级联软删：从目标 id 出发，沿 `supersedes_id` 收集整条链（祖先 + 子孙），全部设为 `status=deleted`。

| API | 行为 |
|---|---|
| `memory_recall` / List | 仅 `active` |
| `memory_get` | `active` 或 `superseded` 可读；`deleted` → not found |
| `action=replace` 对已 superseded/deleted 的 id | not found |

agent 文件记忆（`scope=agent`）仍原地编辑，不经 supersede 链。Turn 提取仍只 `add` + `content_hash` 去重。

## 语义冲突消解（P2-D2）

`scope=session|user` 的 `action=add` 在写入前可经 LLM 与已有 active peers 做语义判断（`ignore` / `supersede` / `keep_both`）：

- 配置：`agent_extra.yaml` 的 `memory_conflict.enabled`（**默认 false**）
- 环境变量覆盖：`SATH_MEMORY_CONFLICT_ENABLED=true|false`
- `k`：peer Recall 条数上限（省略或 ≤0 时 Facade 默认 8）
- 工具路径：仅当 `memory_conflict.enabled`（或 env）为 true 时启用
- Turn 提取：Metadata `source=turn_extract` 在已装配 resolver 时**始终**走语义门（不受工具开关影响）
- 模型：与提取共用 `memory_extraction.auxiliary`，否则用当前 Agent chat model；模型解析失败则跳过写入（fail-closed）

## 向量 Sidecar（P2-E1）

D2 的 peer 发现可由可插拔 `UnitVectorIndex` 提供语义近邻，替代 `LIKE` 子串：

- 配置：`agent_extra.yaml` 的 `memory_vector.provider`（`sqlite` 默认 / `none` 关闭）、`path`
- 存储：独立 SQLite 文件（默认 `data_root/memory_unit_vectors.db`），**不改 `memory_units` 表**
- Embed 模型：复用 `memory_extraction.auxiliary`，否则当前 Agent chat model（按调用解析）
- E1 写门控：仅当本次写入会走 D2 语义门时才 Embed/Upsert；**E2 起已解耦**（见下节）
- Embed 失败（如网关无 `/embeddings`）→ **进程级熔断**，回退 LIKE，需重启恢复
- 命中后回主表校验 `active`，故 sidecar 陈旧条目不会污染裁决

## Hybrid Recall（P2-E2）

`Facade.Recall(source=units)` 在向量路径可用时做 **LIKE ∪ 向量** RRF 融合（`memory_recall` 与 Prefetch 共用）：

- 融合：k=60；两路各取 `2*limit`，同 id 去重求和，截断到 `limit`；`MemoryHit.Score` 为 RRF 分
- **MinScore**：units 路径继续忽略 `RecallQuery.MinScore`（RRF 分量纲 ≈ `1/(60+rank)`，不可当 cosine 阈值；agent/files 路径语义不变）
- 读路径 Embed：独立 800ms 超时（超时/父取消不熔断，只本轮降级）；其他错误触发 E1 进程熔断；进程内 LRU（key=`agentID+query`，容量 64）
- 写路径解耦：凡成功 units 写入（add / KeepBoth / supersede / replace）在向量路径可用时 Upsert，**不再要求 D2 开启**（取代 E1「D2 关零 Embed」）；Delete 门控不变
- Prefetch 的 user/session 两路 Recall 现带 `AgentID`（供门控与动态 Embedder）
- 不含：Qdrant、可配置 k/超时（存量 backfill 见下节；前端开关见下）

Agent 级读开关 `runtime_tools.hybrid_recall`（proto `optional bool`）：

```json
{
  "runtime_tools": {
    "hybrid_recall": false
  }
}
```

- unset / 省略 = 开；`false` = 该 Agent 读路径只走 LIKE；写索引不受此开关影响
- presence 全链路 `*bool`；Portal `hybridRecallGate` fail-open（查不到 agent / 空 `agentID` → 开）
- **Web Agent 编辑**：三态 select「跟随默认（开）/ 开 / 关」；新建默认 omit；编辑且库中已有显式值时选「跟随默认」会保存为 `true`（portal Update omit 会保留库中值）。详见 monorepo 规格 `2026-07-26-agent-hybrid-recall-ui-design.md`。

## 向量 Backfill（P2-E2.1）

把部署前已存在的 `status=active` session/user units 补进 sidecar，使 hybrid 向量支路对老数据也有效：

- **共享核心**：framework `UnitBackfiller`（经 `SessionUnitsBackend.List`，**禁止**经 Facade.List）
- **启动 job**：`newChatService` 在 `BuildMemoryStore` 后 `StartUnitVectorBackfill(sessionUnits)`（`sync.Once`、Force=false、批间 200ms）；Index/Embedder 不可用则 no-op，不阻塞就绪
- **CLI**：

```bash
go run ./cmd/backfill-vectors -conf configs \
  [--force] [--dry-run] [--scope session|user|all] [--batch 50] [--sleep 200ms]
```

- 默认只补缺（`UnitVectorIndex.Has`）；`--force` 全量重 Embed；`--dry-run` 只计数
- Embed 按 unit `agent_id` 解析模型；无法解析 → Skipped；能力性 Embed 错误与 Facade **共享**进程熔断
- **多副本勿并行 `--force`**（无分布式锁）；换 embedding 模型维度冲突时：**删 sidecar DB** 再 `--force`
- 退出：装配/`Run` 错误 → 非 0；仅 `Tripped` → stderr warning + 退 0

## Turn 后提取（P2-C）

assistant 落库成功后，可选异步调用 framework `memory.Pipeline.AddFromTurn`（与 `NotifyMemorySessionDirty` 并列，fail-open）：

- 配置：`agent_extra.yaml` 的 `memory_extraction.enabled`（**默认 false**）
- 环境变量覆盖：`SATH_MEMORY_EXTRACTION_ENABLED=true|false`
- 仅写入 `scope=session|user` units；`content_hash` 去重；**不**自动写 `MEMORY.md` / agent 文件
- 模型：可选 `memory_extraction.auxiliary`，否则用当前 Agent chat model
- Metadata：`source=turn_extract`；user 事实可带 `source_session_id`

### Turn 提取观测（Phase 1）

开启提取后，每轮异步会：

1. 打结构化日志：`memory extract done session_id=… result=… candidates=… written=… drops=… parse_fail=… dur_ms=…`
2. 上报 Prometheus（framework `obs`，Portal **`GET /metrics`**，Auth 白名单）：`memory_extract_turns_total`、`memory_extract_candidates_total`、`memory_extract_written_total`、`memory_extract_drop_total{reason}`、`memory_extract_duration_seconds`、`memory_extract_written_per_turn`

派生：解析失败率 ≈ `parse_fail/(parse_fail+success)`；写入率 ≈ `written/candidates`。

## 失败信号 FailureSignal（P3-A）

流式对话每轮私有 `turnBus` 上挂接 `memory.AttachFailureSignalBridge`（**不是**进程级 `DefaultBus`）：

- 映射：`ToolFailed` → `tool_failed`；`ToolGuardrailWarn` → `tool_repeat_fail`；`PermissionDenied` → `policy_violation`
- 身份：从 ctx 的 `agent_id` / `agent_name` / `session_id` 读取；`task_family` 优先 `agent_name`，否则回落 `agent_id`
- 副作用：结构化日志（`failure_signal`）+ 进程内 ring（64）+ **本轮** `EpisodeLocalBuffer`（turn 结束 `Clear`）；**不写** `memory_units`
- 与 G4 `FailureCaptureHook`（`.learnings/ERRORS.md`）正交、可并存
- 非流式 `SendMessage` 仍可能走空的 DefaultBus，本切片不覆盖

### 本轮 vs 全局（P3-B）

| 通道 | 寿命 | MemoryStore |
|------|------|-------------|
| `EpisodeLocalBuffer`（失败信号 / 本轮笔记） | 当前流式 turn 结束即弃 | 否 |
| Turn 事实提取 | 持久 | 仅 `kind=fact`（metadata） |
| `kind=procedural` Remember | — | **拒绝**（`ErrProceduralRememberBlocked`）；写入仅经 `CommitProceduralRepair`（P3-E） |

### 过程绑定 ProceduralBinding（P3-C/D/E）

手写绑定（`memory_store.procedural_repair`）；`auto_commit` 默关，仅试点 Agent 可开：

```yaml
memory_store:
  procedural_repair:
    enabled: true
    auto_commit: false                 # 默认关；true 时 catalog activate → CommitProceduralRepair
    min_support: 2
    max_procedural: 3
    pilot_agents: ["zone-4100-agent"]  # id 或 name；空列表则 auto_commit 永不写
    inject:
      prefetch: true
      skill_router: true
    bindings:
      - trigger_code: tool_failed
        action_kind: skill
        skill_id: escalation
        mode: suggest
```

- Prefetch：匹配后追加 `label=procedural` 短提示（计入 `max_total`）；并 Recall 本会话已持久化的 `kind=procedural`
- Skill router：在 system prompt 追加「过程修复建议」；合并 catalog + 持久化条目
- 未知 `tool_names`：装载时丢弃并打 `procedural_binding_rejected` 日志
- **生命周期（P3-D）**：`trigger_code` 绑定默认 `candidate`，同 code 失败信号达 `min_support`（默认 2）→ `active`；`trigger_query` 仅绑定直接 `active`
- **Disable**：`DisableProceduralEntry(id)` / `DisableProceduralByCode(agent, code)` 后 Prefetch/Router 不再露出
- **观测**：命中打日志 `procedural_entry_hit`（含 prefetch_hits / router_hits）
- **持久化（P3-E）**：迁移 `011_memory_units_kind.sql`；事实 Recall/List 默认排除 procedural；`CommitProceduralRepair` 跳过 D2/向量；身份含 `agent_name`（`ContextKeyAgentName`）供 pilot 匹配

## Units 向量 Sidecar（P2-E / P2-H）

`scope=session|user` 的 units 可选向量索引（MySQL 仍为权威）：

- 配置：`agent_extra.yaml` 的 `memory_vector.enabled` / `memory_store.vector`（**默认 false**）
- 环境变量覆盖：`SATH_MEMORY_VECTOR_ENABLED=true|false`
- `provider`：`sqlite`（开发默认）| `qdrant`（生产）；`none` 或未启用 = 关
- `store_dir`：仅 sqlite；默认 `data/memory_units_vectors`（库文件 `units_vectors.db`）
- `qdrant`：`url`（必填）、`collection`（默认 `sixath_memory_units`）、`api_key`、可选 `grpc_port`（REST `:6333` → gRPC `:6334`）
- Embedder 优先级：`memory_vector.embedding` → `memory_extraction.auxiliary` → 当前 Agent chat `Embed`（需 context `agent_id`）
- 写成功后异步 Upsert；supersede/remove/Delete 删旧向量；失败 fail-open（不注入 / 不阻断对话）
- `memory_recall(source=units)`：向量就绪时语义检索并 hydrate；失败回退 LIKE
- 语义冲突 peer 发现：向量就绪时用向量 top-K；失败回退 LIKE

## Units 图 Sidecar（P2-I）

独立于 fact 提取的二次 LLM 图提取 + Neo4j（MySQL 仍为权威）：

- 配置：`memory_store.graph` / 顶层 `memory_graph`（**默认 false**）
- 环境变量：`SATH_MEMORY_GRAPH_ENABLED=true|false`
- `provider`：`neo4j`；缺 `neo4j.uri` 或连接失败 → 不注入
- 分区：`user` / `session`（`scope_type` + `scope_id`）；Expand 不跨分区
- assistant 落库后与 fact extract **并列**异步 `AddGraphFromTurn`；模型优先 `graph.auxiliary`，否则 extraction auxiliary / Agent chat
- supersede/remove/Delete → `InvalidateByMemoryID`；`Recall(units)`：向量命中后 Expand + RRF（无向量时按实体名 MatchSeeds 降级）
- 框架级泛化抽取（人/组织/地点/产品/概念/系统组件等）；谓词开放 `snake_case`；输入约 8000 runes；默认 `max_entities=32`
- 可调：`min_relation_confidence`、`max_entities`
- 日志：`memory graph done session_id=… result=… cand_ent=… cand_rel=… written_ent=… written_rel=… drops=…`

## Go Memory MCP Server（P2-J）

Framework 可独立暴露同一门面三工具（默认 in-memory；可注入自定义 `MemoryStore`）：

```bash
# stdio（Cursor / Claude MCP 常见形态）
go run ./cmd/memory-mcp --transport=stdio

# Streamable HTTP（默认 :8765，路径 /mcp）
go run ./cmd/memory-mcp --transport=http --addr=:8765
```

工具：`memory_remember` / `memory_recall` / `memory_get`。MCP 调用需按 scope 传入 `user_id` / `session_id` / `workspace_root` 等（Agent 路径仍走 context）。无鉴权，仅建议本机/可信网络。

规格：[P2-J MCP Server](../../docs/superpowers/specs/2026-07-27-memory-store-mcp-server-design.md)。

## Prefetch（P2-A / P2-F）

每轮 Prefetch（启用时）经 `MemoryStore.Recall` 进行，顺序为：

1. `memory_recall(scope=user, source=units)` — 仅当已解析出 `user_id` 时
2. `memory_recall(scope=session, source=units)` 召回当前会话的结构化记忆
3. `memory_recall(scope=agent, source=files)` 召回工作区文件记忆
4. **P2-F**：按 `ContentHash(TrimSpace)` 跨 label 去重（先到先得），再按 `max_total` 截断
5. 将结果写入 `<sixath-memory-context>` 围栏 system message

配置（`memory_orchestrator_prefetch`）：

- `max_snippets`：每路 Recall limit（省略或 ≤0 → **5**）
- `max_total`：去重后全局条数顶（省略 → **8**；显式 `0` 或负数 → 不截断，仍去重）

超时或后端错误维持 fail-open：对话继续，不把预取失败暴露为模型上下文。跨会话原文检索由 `memory_recall(scope=session, source=transcript)` 提供；空 query 仍会被拒绝，避免无意浏览历史会话。

## user_id 解析

单次 Agent turn 内，Portal 通过 `ResolveMemoryUserID` 解析并注入 `tool.ContextKeyUserID` 与 Prefetch metadata：

1. **会话所有者**：`chat_sessions.user_id`（非空，优先）
2. 否则 **调用方**：`biz.CallerUserID(ctx)`（Auth / cron service principal）
3. 否则视为 **无 user_id** → 静默跳过 user scope

## 迁移

| 旧工具或参数 | 新调用 |
|---|---|
| `memory` | `memory_remember(scope=agent, ...)` |
| `memory_search` | `memory_recall(scope=agent, source=files, ...)` |
| `memory_get` | `memory_get(scope=agent, path=...)` |
| `session_search` | `memory_recall(scope=session, source=transcript, ...)` |
| 旧 `target=user` | `target=user_file` |
| 原 `SATH_MEMORY_WRITE_ENABLED` | **已移除**；改用 `SATH_AGENT_MEMORY_WRITE_ENABLED` 或 `memory_store.agent_workspace.write_enabled` |

现有 `MEMORY.md`、`USER.md` 无需迁移到 `memory_units`。会话消息仍以 MySQL `chat_messages` 为权威来源，transcript 索引可按现有同步逻辑重建。

启用 `scope=user` 需应用迁移 `010_memory_units_user_id.sql`：为 `memory_units` 增加可空列 `user_id` 及索引 `idx_mu_user (user_id, status)`。user scope 写入时 `user_id` 与 `scope_id` 对齐。

## Portal 接线位置

| 位置 | 职责 |
|---|---|
| `internal/chat/memory_store.go` | 组装 session、agent 与 transcript 后端为一个 Store |
| `internal/chat/memory_user.go` | 从 session / caller 解析 MemoryStore 用的 `user_id` |
| `internal/chat/memory_extract.go` | `NotifyMemoryExtractFromTurn` 与提取开关 |
| `internal/chat/memory_conflict.go` | 语义冲突开关、`DefaultMemoryStoreOptions`、dynamic resolver |
| `internal/chat/memory_vector.go` | 向量开关（`memory_vector.enabled`）、sqlite/qdrant Sidecar、Embedder 优先级；以及 P2-E1 `UnitVectors` sqlite 单例 + dynamic embedder（hybrid_recall 用） |
| `internal/chat/memory_graph.go` | 图开关、Neo4j Sidecar、GraphPipeline 通知 |
| `internal/chat/memory_hybrid.go` | Agent 级 `runtime_tools.hybrid_recall` 读门控（fail-open） |
| `internal/chat/memory_backfill.go` | 启动增量 backfill job（Once + 共享熔断） |
| `internal/chat/memory_backfill_cli.go` | CLI flag → BackfillConfig |
| `cmd/backfill-vectors` | 存量补缺 / `--force` 重建 CLI |
| `internal/chat/runtime_tools.go` | 仅注册 `memory_remember`、`memory_recall`、`memory_get` |
| `internal/chat/memory_prefetch_bootstrap.go` | 用 `StorePrefetchBackend` 装配 Orchestrator |
| `internal/chat/transcript_provider.go` / `session_search.go` | 提供转录与索引同步适配 |
| `internal/data/memory_units_*.go` | session 与 user scope 的 MySQL backend |
| `internal/service/chat.go` | 注入 `ContextKeyUserID`；Prefetch `user_id`；assistant 落库后触发提取 |

## Memory Hub（P0–P3b）

管理面已拆除（无 Hub HTTP / Loadout UI / 默认 `knowledge_*` 注册）；`framework/memory/hub` 包保留。

进程启动不再调用 `InitLocalMemoryHub`。下表为历史阶段对照。

| 阶段 | 有 | 无 |
|---|---|---|
| P0 | Catalog + defaults=`local`；Resolve / EnforceHub | 外部 Adapter |
| P1a | `RuntimeToolsConfig.hub_*`；Agent 下拉；`GET /api/v1/memory-hub/catalog`；`knowledge_*` 按 Resolve 注册 | — |
| P1b | `agent_asset_bindings`；loadout/bindings API；Detail 配装 UI | — |
| P2a | `SkillTrustGate` + FS 物化；`hub/fake`；外部 Bind 前物化；未签名→draft | 真实 Tencent HTTP |
| P2b | `POST .../hub/assets/status`；Detail「确认激活」；draft→active 进 Loadout | — |
| P3a | LocalKnowledge 可选 wiki/codegraph 后端 + Capabilities；默认 search 不含二者 | — |
| P3b | `DirWiki` / `DirCodeGraph`；`SATH_HUB_WIKI_ROOT` / `SATH_HUB_CODEGRAPH_ROOT` 接线；显式 `source=wiki\|codegraph` 可搜 | 完整 ingest 产品 / AST 依赖图 |
| P3c | `knowledge_search` `source=graph` → 惰性 Neo4j `GraphSearcher`（需 `memory_graph.enabled` + provider=neo4j） | — |
| Knowledge write | `knowledge_write` / `knowledge_approve`；`*.draft.md`；units `hub_status=draft`；`GET/POST .../hub/knowledge/drafts\|approve`；Detail Knowledge drafts UI | Confluence/Feishu；CodeGraph write；HTTP write |

### Knowledge write（draft → approve）

一期为 Local Knowledge 增加写回路径：**一律先 draft，再 approve 成 active**；默认召回 / `knowledge_search` **不含 draft**。门控是「draft 不进默认召回」，**不是**双人审批——允许 Agent 自批（工具 `knowledge_approve`）。

| 面 | 行为 |
|---|---|
| 工具 | `knowledge_write` / `knowledge_approve`；wiki 为主路径，units 可选 |
| Wiki | 在 `SATH_HUB_WIKI_ROOT` 下写 `*.draft.md`；`knowledge_search` 跳过 draft；approve 落正式 `.md`，已存在正式页须 `overwrite=true` |
| Units | `metadata.hub_status=draft`；approve **删除**该键（同一 unit id）；Prefetch **跳过 draft**；写/批需 Agent `runtime_tools.memory_write_enabled`（与全局 OR，经 `NewGatedMemoryUnitWriter`） |
| HTTP | `GET .../hub/knowledge/drafts`、`POST .../hub/knowledge/approve`；**无** HTTP write（v1） |
| UI | Agent Detail「Knowledge drafts」+ Approve；与 Hub Binding「确认激活」（skill draft→active）分开 |

**已知局限（units）：** MySQL units 不支持 `ScopeAgent`，draft 暂存为 `ScopeUser` + `ScopeID=agentID`。因此 **approve 后默认 Prefetch（按真实 user_id）通常仍看不到**；读路径以 `knowledge_search(source=units)` / 显式 List 为主。后续若要进 Prefetch，需独立 agent-scope 存储或 Prefetch 增补 agent 车道。

规格与计划：[knowledge-write design](../../docs/superpowers/specs/2026-08-08-knowledge-write-design.md)、[knowledge-write plan](../../docs/superpowers/plans/2026-08-08-knowledge-write.md)。

**环境变量（可选）：**

| 变量 | 作用 |
|------|------|
| `SATH_HUB_WIKI_ROOT` | 本地 Wiki 根目录（markdown/text）；声明 `Capabilities.wiki` |
| `SATH_HUB_CODEGRAPH_ROOT` | 源码树根；路径+简易符号检索；声明 `Capabilities.code_graph` |
| `SATH_HUB_FAKE_ADAPTER` | 注册 in-process fake 外部 Adapter（P2a） |
| `SATH_HUB_SKILLS_CACHE` | Skill 物化缓存目录 |
| `SATH_MEMORY_GRAPH_ENABLED` | 覆盖 `agent_extra.memory_graph.enabled` |

**Graph（Neo4j）**：在 `agent_extra.yaml` 设 `memory_graph.enabled: true`、`provider: neo4j` 及 `neo4j.uri/username/password`。`knowledge_search` 默认源含 `graph`；按 ctx 的 session/user/agent scope 做 MatchSeeds→Expand。

**Prefetch 现网行为不变**：units 召回仍走 `MemoryStore` / `StorePrefetchBackend`。

规格与计划：[governance-knowledge-plugins design](../../docs/superpowers/specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md)、[P0](../../docs/superpowers/plans/2026-08-07-memory-hub-governance-knowledge-p0.md)、[P1](../../docs/superpowers/plans/2026-08-07-memory-hub-governance-knowledge-p1.md)、[P2](../../docs/superpowers/plans/2026-08-07-memory-hub-governance-knowledge-p2.md)、[P2b/P3a](../../docs/superpowers/plans/2026-08-07-memory-hub-governance-knowledge-p2b-p3a.md)、[P3b](../../docs/superpowers/plans/2026-08-07-memory-hub-governance-knowledge-p3b.md)、[knowledge-write](../../docs/superpowers/specs/2026-08-08-knowledge-write-design.md)。

## 后续 Backlog

1. ~~向量索引（开发 SQLite、生产 Qdrant）及可选图记忆。~~ → **P2-E sqlite / P2-H qdrant / P2-I Neo4j 已交付**。
2. ~~Hybrid Recall（LIKE∪向量 RRF）及存量 backfill。~~ → **P2-E2 / P2-E2.1 已交付**（`runtime_tools.hybrid_recall` 门控 + `cmd/backfill-vectors`）。
3. ~~Prefetch 配额与去重增强。~~ → **P2-F 已交付**（`max_total` + hash 去重）。
4. ~~旧配置键重命名与冗余 FTS 评估。~~ → **P2-G 已交付**（`memory_store:` + `SATH_AGENT_MEMORY_WRITE_ENABLED`；FTS 包保留内部）。
5. ~~更后续可由 Go MCP Server 暴露同一 MemoryStore。~~ → **P2-J 已交付**（`framework/memory/mcp` + `cmd/memory-mcp`；stdio/HTTP；默认 in-memory）。
6. Memory Hub：P0–P3b 本地可搜 + knowledge write（draft→approve）已交付；后续真实 Tencent Adapter、完整 Wiki ingest / AST CodeGraph、Confluence/Feishu / HTTP write。

相关规格：[MemoryStore 门面设计](../../docs/superpowers/specs/2026-07-25-memory-store-facade-design.md)、[P2-A user scope](../../docs/superpowers/specs/2026-07-25-memory-store-user-scope-design.md)、[P2-C Turn 提取](../../docs/superpowers/specs/2026-07-25-memory-store-turn-extract-design.md)、[P2-D1 supersede / ConflictResolver](../../docs/superpowers/specs/2026-07-25-memory-store-conflict-resolver-design.md)、[P2-D2 LLM 语义冲突](../../docs/superpowers/specs/2026-07-26-memory-store-llm-conflict-design.md)、[P2-E1 向量 Sidecar](../../docs/superpowers/specs/2026-07-27-memory-store-vector-sidecar-design.md)、[P2-E2 Hybrid Recall](../../docs/superpowers/specs/2026-07-26-memory-store-hybrid-recall-design.md)、[P2-E2.1 Vector Backfill](../../docs/superpowers/specs/2026-07-26-memory-store-vector-backfill-design.md)、[Agent Hybrid Recall UI](../../docs/superpowers/specs/2026-07-26-agent-hybrid-recall-ui-design.md)、[P2-H Qdrant](../../docs/superpowers/specs/2026-07-27-memory-store-qdrant-design.md)、[P2-I Neo4j 图](../../docs/superpowers/specs/2026-07-27-memory-store-neo4j-graph-design.md)、[P2-J MCP Server](../../docs/superpowers/specs/2026-07-27-memory-store-mcp-server-design.md)、[P2-F Prefetch 配额](../../docs/superpowers/specs/2026-07-27-memory-store-prefetch-quota-design.md)、[P2-G 配置清理](../../docs/superpowers/specs/2026-07-27-memory-store-config-cleanup-design.md)、[Memory Hub P0](../../docs/superpowers/specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md)、[Knowledge write](../../docs/superpowers/specs/2026-08-08-knowledge-write-design.md)。
