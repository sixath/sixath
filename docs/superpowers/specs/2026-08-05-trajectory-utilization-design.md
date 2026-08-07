# 执行轨迹利用（非 RL）：即时复盘 · 锚定召回 · 产品化

> 状态：已确认（§1–§5 用户拍板；§16 收口审阅 must-fix）  
> 日期：2026-08-05  
> 对照：[hermes-growth-architecture.md](../../../hermes-growth-architecture.md)、Hermes `background_review` / `session_search` / SessionDB  
> 关联：[tool-discovery](../../../framework/docs/superpowers/specs/2026-06-05-tool-discovery-design.md)、[growth-system](../../../framework/docs/superpowers/specs/2026-05-10-growth-system-design.md)、[agent-review-runner](./2026-07-07-agent-review-runner-design.md)  
> 说明：开启 C3 后，**取代**既有「不做主循环 fork」类非目标；异步 Worker 仍保留作兜底。

---

## 0. 已确认决策

| 项 | 选择 |
|----|------|
| 分期 | **C**：一期改 Agent 行为 → 二期产品化；ShareGPT/RL 另开第三期 |
| ShareGPT / Batch / RL | **A3**：仅留 `TrajectoryExporter` 接口（Noop）；本期不实现落盘/训练 |
| 召回暴露面 | **B3**：增强 `memory_recall(source=transcript)` + Portal 只读 API；**不**恢复独立 `session_search` 工具 |
| 复盘吃轨迹 | **C3**：回合内 in-memory messages 即时 fork + 异步 Growth 兜底 |
| 落地架构 | **方案二**：即时 fork **且** 持久化 TurnTrace，供复盘/召回/Insights 共用 |
| 压缩血缘（二期） | 策略 **B**：默认同会话 compact boundary；可选「归档压缩」才 `parent_session_id` 切子会话 |
| 非目标 | Atropos/Tinker、Achievements、替换 L0/L1/L2 算法、Prefetch 自动塞锚定结果 |

---

## 1. 背景与问题

相对 Hermes，Sixath 已有本轮 `RunTrace`（护栏/证据门/压缩可观测）与 transcript→Growth，但执行轨迹未充分回流：

| 缺口 | 影响 |
|------|------|
| `RunTrace` 不落库 | 异步复盘/跨实例看不到 tool 成败细节 |
| Growth 只吃 Markdown transcript | 工具参数/错误/解包名进不了 Skill 策展 |
| transcript FTS 无 tool 字段、无锚点窗口 | 「上次怎么查的库」类召回弱 |
| `LastL2Summary` 未持久化 | compact boundary 刷新即丢 |
| 无 Insights / Rewind / 压缩切会话 | 轨迹无法运营与回滚 |

本设计覆盖除 **RL 训练环** 外的上述能力。

---

## 2. 目标与成功标准

### 2.1 一期（行为）

| ID | 目标 | 成功标准 |
|----|------|----------|
| G1 | TurnTrace 持久化 | Run 结束 upsert；FTS 可搜 tool 投影行；失败不阻断用户回复 |
| G2 | 锚定召回 | `memory_recall(source=transcript)` 返回 anchor±N + bookend；Portal API 同构 |
| G3 | 即时 BackgroundReview | nudge 达阈后 fork 瘦身 Agent，输入含 tool messages；不阻塞用户首包 |
| G4 | 异步合流 | SessionEnd Growth 附带 TraceDigest；与即时路径靠 lease + pending 去重 |
| G5 | Compact boundary | `LastL2Summary` 落库为 `sixath.origin=compact_boundary` 消息 |

### 2.2 二期（产品）

| ID | 目标 | 成功标准 |
|----|------|----------|
| G6 | Insights | 只读 API 从 `turn_traces` 聚合 tool/error 模式 |
| G7 | Rewind | 消息 soft-delete + FTS/Trace 同步；可从锚点续聊 |
| G8 | 压缩血缘 | 可选 fork_session；`SearchAnchored.RootSessionID` 折叠 parent 链 |

### 2.3 第三期占位

| ID | 目标 |
|----|------|
| G9 | ShareGPT JSONL 导出（非 RL 接线） |

---

## 3. 总架构

```
ReAct Run
  ├─ messages（含 tool）──► BackgroundReview（即时 fork，子 Agent nudge=0）
  ├─ ToolCallRecord[] ──► BuildTurnTrace（脱敏）──► turn_traces + FTS role=tool
  └─ LastL2Summary ──► ChatMessage(origin=compact_boundary)

异步 GrowthWorker
  └─ transcript + TurnTraceStore.ListBySession ──► Skill/Memory patch

Agent / Portal
  └─ memory_recall / GET transcript/search
        └─ FTS hit → SearchAnchored（window + bookend）
```

**共用水源**：`turn_traces`（权威）+ FTS 投影（检索）。ChatMessage **不**为每条 tool 增行（避免污染会话 UI）。

### 3.1 包边界与所有权

| 单元 | 位置 | 职责 |
|------|------|------|
| `TurnTrace` / `TurnToolCall` / `BuildTurnTrace` | `framework/agent`（或 `framework/turntrace` 小包） | 从 `RunTrace` 转换；无 DB 依赖 |
| `TurnTraceStore` 接口 | `framework/turntrace` | 存储抽象 |
| `TurnTraceStore` MySQL 实现 | `portal/internal/data` | `turn_traces` 表 |
| FTS 投影写入 | Portal chat 收尾调用 `sessionsearch.IndexMessage` | framework 只扩 `MessageDoc` |
| `TrajectoryExporter` | `framework/turntrace`；Portal 注入 Noop | 第三期再换实现 |
| 即时 `spawnBackgroundReview` | **Portal** `GrowthWorker` / `ChatService` 协作 | framework/growth **不** import `agent`；fork 仅 portal |
| `SearchAnchored` | `framework/sessionsearch` | 消息级 API；Portal TranscriptBackend / HTTP 适配 |

---

## 4. 一期：TurnTrace 模型与持久化

### 4.1 类型

```go
type TurnTrace struct {
    SessionID, AgentID, RequestID string
    TurnSeq   int
    CreatedAt time.Time
    Calls     []TurnToolCall
}

type TurnToolCall struct {
    Step, DurationMS int
    ToolCallID, ToolName, BridgeName string // ToolName 为解包后真实名
    Arguments map[string]any               // 已脱敏
    ResultPreview, Error, Decision string
    Blocked bool
}
```

`BuildTurnTrace` 从 `RunTrace` 转换；`tool_call` 解包规则与 `react_agent` 一致。

### 4.2 存储

**Portal 表 `turn_traces`**

- 列：`id`, `session_id`, `agent_id`, `request_id`, `turn_seq`, `payload_json`, `created_at`, `active`（bool，默认 true；二期 Rewind 软隐）
- 唯一：`(session_id, request_id)`（幂等 upsert，覆盖 payload）
- **`TurnSeq`**：会话内单调递增；由 Portal 在 Upsert 前 `MAX(turn_seq)+1` 分配（同 `request_id` 重试不递增，只覆盖）

**Growth 会话扩展列**（或等价 metadata）

- `last_background_review_at`、`last_review_request_id`
- `bg_review_in_flight`（bool；即时 fork 开始置 true，结束清 false）— 供 Worker 跳过抢跑
- **`bg_review_in_flight_since`**（timestamp）：Worker 若见 in_flight 且 `now - since > background_review.in_flight_ttl`（默认 15m，≥ AgentReviewTimeout）→ **强制清 in_flight** 并允许认领（防进程崩溃永久堵死异步兜底）

**FTS 投影**（`framework/sessionsearch`）

- `MessageDoc` 增加可选 `ToolName`；SQLite `messages` / `messages_fts` **schema bump**（迁移：重建 FTS 或加列 + 回填）
- 每条 call 一行：`ID=trace:{request_id}:{tool_call_id}`，`Role=tool`，`Content` 含 name/err/args/result **预览**（与 `ResultPreview` 同截断规则，禁止原始 Result）
- `SessionMeta.ParentSessionID` 写入时带上现值（折叠逻辑二期加强）
- `BridgeName`：可选；若经 `tool_call` 桥接，从调用名派生，也可省略（权威以解包后 `ToolName` 为准）

### 4.3 脱敏与预算（默认）

| 字段 | 规则 |
|------|------|
| Arguments | ≤2KiB；password/token/secret/api_key/authorization 等 → `[redacted]` |
| ResultPreview | ≤4KiB；大块 base64 → `[omitted binary]` |
| 每 turn | 最多 40 calls；超出失败优先 |

配置：`trace.persist.enabled`（默认 true）、字节上限可配。

### 4.4 写入时机

Chat `SendMessage` / Stream 在拿到 `RunTrace` 后：`Upsert` → `IndexMessage`（每 call）。落库失败只打日志。

### 4.5 接口

```go
type TurnTraceStore interface {
    Upsert(ctx context.Context, t TurnTrace) error
    GetByRequest(ctx context.Context, sessionID, requestID string) (*TurnTrace, error)
    ListBySession(ctx context.Context, sessionID string, limit int) ([]TurnTrace, error)
}
```

```go
type TrajectoryExporter interface {
    Export(ctx context.Context, input TrajectoryExportInput) error
}
// 默认 NoopExporter
```

---

## 5. 一期：锚定召回（B3）

### 5.1 framework `sessionsearch`

现网 `Search` 经 `collapseHits` 返回**会话级** `SessionHit`。锚定需要**消息级** discovery，故新增并行 API，**不改变**现有 `Search` 契约。

```go
type AnchorOpts struct {
    Window  int // 默认 5
    Bookend int // 默认 3；首/末 user+assistant
}

type AnchoredHit struct {
    SessionID, RootSessionID, Title string
    Anchor       MessageDoc
    Window       []MessageDoc
    BookendStart, BookendEnd []MessageDoc
    Score        float64
}

// SearchAnchored：FTS 消息级命中（不做 collapseHits），再对每个 hit 取 window/bookend。
SearchAnchored(ctx, SearchOpts, AnchorOpts) ([]AnchoredHit, error)
GetMessagesAround(ctx, agentID, sessionID, messageID string, window int) ([]MessageDoc, error)
```

**Window 数据源**

| 行类型 | 来源 |
|--------|------|
| user/assistant | 同 agent FTS/SQLite 索引中该 `session_id` 的消息行（按 `created_at`） |
| tool 投影 | 同上（索引内 role=tool）；**不**要求存在于 `chat_messages` |
| bookend | 仅 user/assistant；按时间取会话最早/最晚 N 条 |

- Discovery：`RoleFilter` 默认 `user,assistant,tool`
- **一期不做 LLM 摘要**；Limit 默认 3、最大 5
- 同一会话多 hit：按 score 去重为「每会话最多 1 个最佳锚点」（可配置），避免窗口刷屏
- 一期 `RootSessionID == SessionID`；二期走 parent 折叠

### 5.2 `memory_recall`

扩展参数（向后兼容）：`anchor_window`、`include_tools`（默认 true）、`exclude_current`（默认 true）。

| `source=transcript` + query | 行为 |
|-----------------------------|------|
| 非空 query | `SearchAnchored` → 返回 `hits: []AnchoredHit` JSON（字段名稳定，供模型阅读） |
| 空 / 省略 query | **放宽 schema**：`query` 改为非 required；走 `ListRecent` 目录（无 window），形状 `{sessions:[...], count}` |

其它 `source`：`query` 仍按各 source 原语义校验（空 query → 明确错误或空结果，**不**走 ListRecent）。Tool description 诱导「上次/之前」时使用 transcript。

**TurnSeq 并发**：同会话并行 Upsert 时用 DB 事务/`SELECT … FOR UPDATE` 或唯一失败重试分配 `MAX(turn_seq)+1`。

### 5.3 Portal API

`GET /api/agents/{agent_id}/transcript/search?q=&exclude_session=&include_tools=1&window=5`  
响应与 `AnchoredHit` 同构；权限同 agent 读权限。

### 5.4 回源

Window 内优先用索引 content；需更全 preview 时按 `trace:{request_id}:{tool_call_id}` 查 `TurnTraceStore`（仍不回填未脱敏全文）。

---

## 6. 一期：C3 BackgroundReview + 异步合流

### 6.1 即时路径与 Wake 竞态（必须）

现网：`OnToolSuccess` / `OnAssistantTurn` 达阈 → 置 `pending_*` + `growthwake.Wake()`，Worker 可能**立刻**抢跑，早于 stream Done 的 C3 fork。

**C3 开启时的状态机**

```
工具成功 / assistant 落库
  → 仅递增计数；达阈则置 pending_*，并置 deferred_wake=true
  → **不**立即 growthwake.Wake()     ← 仅 background_review.enabled=true

ReAct/Stream Done（同一 request）
  → Persist TurnTrace + compact boundary
  → 若 pending_*：
       置 bg_review_in_flight=true, bg_review_in_flight_since=now
       spawnBackgroundReviewAsync(messages_snapshot)  // 本进程
  → 否则若 deferred_wake：Wake()
  // deferred_wake：进程内/单 request 标志，不入 DB；crash 后靠 Worker poll pending_*

BackgroundReview 结束
  → 清 bg_review_in_flight / since
  → 成功：ClearGrowthPending + last_background_review_at/request_id
  → 失败：保留 pending_* + Wake() 异步补做
```

**Worker 侧门闩**（认领 job 前）

1. 若 `bg_review_in_flight` 且未过 `in_flight_ttl` → skip  
2. 若 `bg_review_in_flight` 已过 TTL → 清陈旧标志，继续  
3. 若 `now - last_background_review_at < dedupe_window` 且窗口内无新 pending 置位 → skip  
4. 否则 lease + Run review（附 TraceDigest）

`dedupe_window` **独立配置**（默认 10m），**不得**复用 `IdleCheckInterval`。

**Fork 约束**

- 实现挂在 Portal：复用/抽取 `GrowthWorker.spawnReviewAgent`，入参改为可选 `[]model.Message` snapshot；framework/growth 仍只提供 Runner 编排接口
- Toolset：skills（+ memory 若需要）；禁 web/mcp/terminal
- 子 Agent growth nudge **强制关闭**
- MaxSteps / timeout 复用现有 AgentReview 配置
- Combined：双 pending 一次 fork
- 上下文过长：最近 K 轮 + **失败 tool 优先**保留

**失败**：日志 + metrics；保留 pending + Wake 异步补做。

### 6.2 与 Scheme A / SessionEnd

- 保留 `TrySessionEnd*` / C2s session-end skill 开关语义
- **可跳过条件（满足其一即可）**：
  - `!pending_skill && !pending_memory`，或
  - `last_review_request_id` 对应该会话最近已复盘 request，且在 `dedupe_window` 内，且无**新的** pending 置位
- `TrySessionEnd*` 若仍因 C2s 置 pending：允许 Wake；Worker 仍受 in_flight 门闩约束
- SessionEnd 跳过看的是 **pending/去重窗口**，不是「计数是否归零」 alone

### 6.3 异步升级

`fetchReviewTraceDigest`：最近 N turn（`active=true`），失败优先，拼进 review user 内容（`# Turn traces`）。无 Trace 时与现网一致。

### 6.4 写盘合流

两路径均须：workspace lease → patch/`skill_manage` → ClearGrowthPending + 更新 last_review_* + 清 in_flight。抢锁失败则本轮放弃（保留 pending）。Cron 反写复用 `rewriteCronAfterForkReview`。

### 6.5 配置

```yaml
growth:
  background_review:
    enabled: true                 # false 时恢复现网：达阈立即 Wake，无主循环 fork
    # interval：>0 覆盖；否则用 NudgeConfig / Defaults（优先级：本块 > NudgeConfig > NewDefaults）
    skill_tool_interval: 10
    memory_turn_interval: 3
    max_snapshot_messages: 80
    prefer_failed_tools: true
    dedupe_window: 10m            # 非 IdleCheckInterval
    in_flight_ttl: 15m            # ≥ AgentReviewTimeout；陈旧 in_flight 强制清理
  async_include_turn_traces: true
  async_trace_turn_limit: 5
```

### 6.6 观测

`growth_bg_review_spawned|ok|fail`、`growth_async_skipped_recent_bg`、`growth_review_lease_conflict`、`growth_bg_in_flight_stale_cleared`。

---

## 7. 一期收尾：Compact boundary

Run 结束且 `LastL2Summary != ""`：

- `CreateMessageWithMetadata`，`metadata.sixath.origin = compact_boundary`，含 `middle_removed`、`request_id`
- **幂等**：写入前按 `session_id + metadata.request_id + origin=compact_boundary` 查询；已存在则跳过（或更新 content）。无 DB 唯一索引时以此应用层去重为准
- 写失败不阻断用户
- **一期不**因 L2 自动建子 session

---

## 8. 二期：Insights

- `GET /api/agents/{id}/insights?from=&to=`
- 数据源：`turn_traces`（非 chat 正文）
- 维度：turns、tool calls、error rate、top tools、top sessions、blocked/guardrail 计数
- token/cost：有字段再加；一期不做
- UI：只读页；无成就系统

---

## 9. 二期：Rewind

- `chat_messages.active`（或 `deleted_at`）+ `sessions.rewind_count`
- `POST /api/sessions/{id}/rewind { message_id }`
- 锚点之后消息 `active=false`；FTS 删除/标记对应行；`turn_traces.active=false`（`created_at` ≥ 锚点时间或 request 序之后）
- ListMessages / ListBySession 默认仅 active
- 不回滚已写入的 Skill；进行中的 stream 先取消

---

## 10. 二期：压缩血缘（策略 B）

- 默认：同会话 compact boundary（§7）
- `compact.fork_session_on_l2` 或显式「归档压缩」：新建 session，`parent_session_id=旧`；旧只读；新从摘要续跑
- `SearchAnchored` 使用已有 parent 折叠填充 `RootSessionID`

---

## 11. 错误处理与多实例

| 场景 | 策略 |
|------|------|
| Trace 落库失败 | 日志；即时 review 仍可用内存 snapshot |
| FTS 投影失败 | 日志；权威数据仍在 `turn_traces` |
| 即时 review 失败 | pending 留给异步 |
| 多实例 | 异步只读库；即时只在本进程；写盘靠 lease |
| 脱敏遗漏 | 键名黑名单 + 长度上限；禁止把原始 Result 写入 FTS |

---

## 12. 测试计划

### 一期

- TurnTrace upsert 幂等；`tool_call` 解包名；脱敏；超长截断
- FTS 命中 tool；`include_tools=false`；`exclude_current`
- Anchored window/bookend 边界（短会话）
- BackgroundReview：nudge 触发、子 Agent 不递归、SessionEnd 去重、lease 冲突
- 异步无 Trace 回退；有 Trace 时 prompt 含 digest
- Compact boundary 刷新后仍可见

### 二期

- Insights 聚合正确性
- Rewind 后模型与召回不可见后续消息
- fork_session parent 折叠

---

## 13. 实施切片建议

| 切片 | 内容 | 依赖 |
|------|------|------|
| P1-A | TurnTraceStore + 表 + BuildTurnTrace + chat 写入 | — |
| P1-B | FTS MessageDoc.ToolName + tool 投影 | P1-A |
| P1-C | SearchAnchored + memory_recall + Portal search API | P1-B |
| P1-D | BackgroundReview 即时 fork + 去重标记 | —（可与 A 并行） |
| P1-E | 异步 TraceDigest + lease 合流 hardening | P1-A, P1-D |
| P1-F | Compact boundary 持久化 | — |
| P2-A | Insights | P1-A |
| P2-B | Rewind | P1-B |
| P2-C | fork_session 血缘 | P1-F, sessionsearch parent |

**实施计划（一期）：** [2026-08-05-trajectory-utilization-phase1.md](../plans/2026-08-05-trajectory-utilization-phase1.md)  
**实施计划（二期）：** [2026-08-06-trajectory-utilization-phase2.md](../plans/2026-08-06-trajectory-utilization-phase2.md)

---

## 14. 关键代码锚点（现状）

| 关注点 | 位置 |
|--------|------|
| RunTrace / ToolCallRecord | `framework/agent/trace.go` |
| tool_call 解包 | `framework/agent/react_agent.go` |
| sessionsearch MessageDoc / ParentSessionID | `framework/sessionsearch/types.go` |
| OriginCompactBoundary | `framework/model/metadata_sixath.go` |
| LastL2Summary 字段 | `framework/agent/trace.go`（写入见 `context_ops.go`） |
| Growth fork review | `portal/internal/service/growth_agent_review.go` |
| Growth nudge | `framework/growth/config.go` |
| Chat ParentSessionID | `portal/internal/data/model/chat.go` |

---

## 15. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-08-05 | 初稿：§1–§5 对话确认后落盘 |
| 2026-08-05 | §16：审阅 must-fix（Wake 竞态、所有权、消息级 FTS、schema/去重、recall 契约） |
| 2026-08-05 | 复审：§6.2 措辞、`in_flight_ttl`、TurnSeq 并发、非 transcript 空 query |

---

## 16. 审阅收口摘要

相对首版补强：

1. **延迟 Wake + `bg_review_in_flight`**，避免 Worker 抢在 C3 fork 前跑  
2. **独立 `dedupe_window`**，不复用 `IdleCheckInterval`  
3. **framework/portal 所有权表**；fork 仅 Portal  
4. **`SearchAnchored` 消息级、不 collapse**；tool 行只存在于索引  
5. **`TurnSeq` / growth 去重列 / `turn_traces.active` / FTS schema bump / compact 应用层幂等**  
6. **`memory_recall`：query 非必填；空=ListRecent；有 query=AnchoredHit JSON**  
7. **`in_flight_ttl` 陈旧清理**；§6.2 跳过条件改为「满足其一」
