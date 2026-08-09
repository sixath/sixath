# Sixath Agent 运行时 — Hermes 理念适配详细设计

**版本**: 1.1  
**状态**: 设计稿（规范级；不要求与实现逐行一致）  
**范围**: `github.com/sixath/framework` 内 Agent 编排、模型适配、工具运行时、可观测与可选构建；Portal 仅描述接入约束。  
**关联文档**: 记忆与工具护栏的专项设计见 [design-memory-tools-hermes-parity.md](./design-memory-tools-hermes-parity.md)（本设计与其互补：本文聚焦「编排核心、多入口、Provider、Prompt 分层、工具集与钩子、循环语义、会话存储、构建瘦身」）。**产品规格**（验收口径、史诗与用户故事）见 [product-spec-agent-runtime-hermes-inspired.md](./product-spec-agent-runtime-hermes-inspired.md)。

---

## 0. 导读与术语

### 0.1 导读

本文将 Hermes Agent 中已验证的**产品化运行时契约**映射到 Go/Sixath，并写清：**现状代码在何处、目标行为是什么、未决项默认选哪一支、如何验收**。实施时应以本文与 `design-memory-tools-hermes-parity.md` 为 framework 侧权威；若与旧 README 中的 Roadmap 表述冲突，以设计文档为准。

### 0.2 术语表

| 术语 | 含义 |
|------|------|
| **Canonical 消息** | 编排层内部使用的 `[]model.Message`（OpenAI 兼容 role/content/tool_calls 为主），对外厂商请求前再编码。 |
| **Stable Prompt** | 参与身份与长期一致性的 system 片段集合；用于稳定哈希 / 缓存断点（若对接支持缓存的 API）。 |
| **Ephemeral Prompt** | 每轮可变的 system 后缀或注入说明；不得单独破坏 Stable 的哈希键语义。 |
| **Toolset** | 工具逻辑分组名，用于白名单/黑名单过滤，不等价于 MCP server id。 |
| **Assistant 步** | 一次模型返回（可能含多个 `tool_calls`）加上其后紧跟的若干 `tool` 结果消息，直到下一轮模型调用之前。 |

### 0.3 与当前实现映射（便于评审对照）

| 设计概念 | 当前代码锚点（`framework/`） | 备注 |
|----------|------------------------------|------|
| ReAct 主循环 | `agent/react_agent.go`：`Run`、`RunStream`、`RunEvents`；内部 `runPlain` / `runToolEventsSync` / `runToolEvents` | 已为事实上的编排核心。 |
| 单步多工具执行 | `executeToolStep` → 顺序 `executeOneToolCall` | **当前为严格串行**；并行属本文 P0/P1 之后的增强。 |
| 权限 | `PermissionPolicy`、`executeOneToolCall` 内校验 | 与本文 `ToolHook` 顺序需衔接（见第 6 章）。 |
| 事件 | `events/event.go`：`ModelInvoked`、`ToolInvoked` 等 | 第 8 章与现有常量对齐，避免再造一套 Kind。 |
| 工具注册 | `tool/tool.go`：`Registry.Register`、`List`；`tool/toolset.go`：`ListByToolsets`、`PresetHermesCoreTags` | 已有 **`Toolset`** 与 Hermes 对齐映射；**尚无** `Available(ctx)` / `ListForAPI` 合并过滤（见第 6 章）。 |

---

## 0.4 评审修订记录（v1.0 → v1.1）

| 评审点 | v1.0 问题 | v1.1 处理 |
|--------|-----------|-----------|
| 与代码不一致 | 文档暗示「多 tool 并行需核对」，未写明现状 | 在第 0.3、2.2、7.2 节明确**当前串行**；并行写清为**可选增强**及默认并发上限。 |
| 「零拷贝」歧义 | 易被理解为内存零拷贝 | 改为「新入口不复制 ReAct 循环逻辑」，见第 3.3 节。 |
| 7.2 节策略未固定 | 「二选一」未指定默认 | **默认策略 D1**（见第 7.2.1 节）写死；另给出可选策略 D2 为远期优化。 |
| 7.3 节取消与持久化 | 未区分内存历史与 Portal 提交边界 | 增加第 7.3.1 节「内存态 vs 已提交态」与推荐提交点。 |
| `ToolDef` 悬空 | 示例接口未映射仓库类型 | 第 4.2.2 节映射到 `tool.Tool` 与模型层 `openai.FunctionDefinition` 构建路径。 |
| 8 节与现有事件重复 | 新造 Kind 名与 `events` 包不一致 | 改为**扩展现有 Kind + Payload 约定**，避免两套枚举。 |
| 缺少安全与并发边界 | Hook、日志、Registry 并发未写 | 第 6.5、7.6 节补充。 |
| 缺少开放问题收口 | 决策分散 | 第 16 节开放问题表（含 owner 建议）。 |

---

## 1. 背景与目标

### 1.1 动机

[Nous Hermes Agent](https://github.com/NousResearch/hermes-agent) 在 Python 生态中形成了较完整的产品化运行时：单一 `AIAgent` 核心、多 API 形态、提示词缓存分层、工具注册与 toolset、可中断调用、并行工具与顺序写回、会话 SQLite+FTS、可选依赖与插件扩展等。Sixath 以 Go 实现，**对齐的是能力与契约**，而非复制实现或引入 Python 运行时。

### 1.2 设计目标（可验收）

| ID | 目标 | 验收要点（可测） |
|----|------|------------------|
| G-O1 | **单一编排核心** | 新入口（HTTP/Worker/测试）**不复制** `for step` + `ChatWithTools` 循环；仅组装入参并调用同一 Runner API。 |
| G-O2 | **内部 canonical 消息** | 新增 `APIMode` 时，`react_agent.go` 中「步进循环」**diff 行数**有团队约定上限（建议 ≤80 行或零改循环仅换 `Model` 实现）。 |
| G-O3 | **Prompt 稳定层与临时层分离** | Stable 输入不变、仅 Ephemeral 变化时，`prompt_stable_hash`（若启用）**字节级不变**；单测覆盖。 |
| G-O4 | **工具可发现、可过滤、可审批** | 给定 `enabled_toolsets` 与 `Available` 返回错误时，该工具**不得出现在**当次 `req.Tools` 构建结果中（单测快照）。 |
| G-O5 | **循环语义正确** | 多 `tool` 写回顺序与模型 `tool_calls` **索引顺序一致**；`NormalizeHistory` 有表驱动单测（合法/非法输入对）。 |
| G-O6 | **取消与超时** | `ctx.Done()` 后：无泄漏 goroutine（`go test -race` 子集）；Trace 带 `canceled=true`；**已提交** DB 不出现半截 assistant（见第 7.3.1 节）。 |
| G-O7 | **预算与子会话** | 主 Run 达 `MaxSteps` 停止；若存在子代理，子代理 `max_iterations` **独立计数**，父 budget 不被子循环耗尽。 |
| G-O8 | **可选构建** | 带「最小 tag」的 `go list -deps` 不包含声明的可选驱动包（CI 可脚本化）。 |
| G-O9 | **持久会话（可选）** | WAL + `schema_version`；与 Portal 存储关系符合第 10.3 节选定方案；迁移可回滚。 |

### 1.3 非目标（本文不展开实现规格）

- 替换 Portal 现有会话存储协议或强制迁移用户数据。  
- 实现 Hermes 全量 IM 网关（Telegram/Discord 等）。  
- 在 framework 内实现完整「技能市场」后端；仅保留与 [agentskills.io](https://agentskills.io) 目录结构兼容的加载约定即可作为后续里程碑。

---

## 2. 现状快照（Sixath 已实现与缺口）

### 2.1 已有能力（应保留并作为扩展锚点）

- **`agent.ReActAgent`**：`MaxSteps`、`MaxHistory`、`MaxContextRunes`、`PermissionPolicy`、`events.Bus`、`memory.Orchestrator` 注入等（见 `framework/agent/react_agent.go`）。  
- **`tool.Registry`**：按名注册、`ExecuteFunc`、`SetEventBus` 下发 `ToolInvoked` / `ToolExecuted`、OpenTelemetry span（见 `framework/tool/tool.go`）。  
- **`model` 包**：压缩、`CallConfig`、多轮工具调用、`openai_tools_stream.go` 中基于 `openai.Tool` / `FunctionDefinition` 的 schema 构建。  
- **`events.Bus`**：同步/异步订阅（见 `framework/events/bus.go`）。

### 2.2 与 Hermes 理念对照的主要缺口

| 领域 | 现状 | 目标形态（本文设计） |
|------|------|------------------------|
| 多入口 | Portal 等直接组装 `Request` 调 `ReActAgent`（尚可） | 可选提炼 **`ConversationRunner` 接口**，强制「循环只在一处」；`ReActAgent` 可作为默认实现 **Adapter**，而非第二套循环。 |
| Provider | 以 OpenAI 兼容为主 | 显式 **Adapter 层** + **`APIMode` 枚举**；编码在 `model` 子包，循环不感知 URL。 |
| System prompt | 多为调用方拼接 | **`PromptBuilder`**：Stable + Ephemeral；与记忆文档中的注入元数据一致。 |
| 工具 | 全局 `Register`，无 toolset / `Available` | **`Toolset` + `Available(ctx)`**；`List()` 或新增 **`ListForAPI(ctx, filter ToolFilter) []Tool`**。 |
| 同轮多工具 | **`executeToolStep` 内 for 循环串行** | **默认保持串行**（行为不变）；**可选** errgroup 并行 + **槽位按序写回**（第 7.2 节）；若存在 `RequiresSequential`，采用 **D1** 整轮串行。 |
| 工具钩子 | `EventBus` + span | 可选 **`ToolHook` 切片**（第 6.3 节）；与 Bus 职责划分见第 6.5 节。 |
| 历史合法性 | `stripLeadingOrphanToolsAfterSystem` 等 | 收敛为可导出的 **`NormalizeHistory`**（或包装现有函数）+ 文档化语义与 Trace 字段。 |
| 子代理预算 | 视 Agent 类型而定 | **`IterationBudget`** 与父隔离（第 7.4 节）。 |
| 会话检索 | Portal DB / 未统一 | 可选 **`framework/session`**（第 10 章）。 |

---

## 3. 单一编排核心（G-O1）

### 3.1 问题

若 CLI、HTTP API、定时 Worker 各自复制「append user → model → tools → loop」，则压缩策略、权限、记忆注入、trace 字段会出现分叉。

### 3.2 设计

**定义「编排边界类型」**（名称可调整，以下为逻辑角色）：

```text
ConversationRunner
  Run(ctx, RunInput) (RunOutput, error)

RunInput 至少包含：
  - Messages           // 当前轮输入（或仅 UserTurn + 由 Runner 加载历史）
  - Registry           // *tool.Registry 或受限视图（见 ToolFilter）
  - Model              // model.Model / ToolCallingModel
  - Options            // ReActOption 等价物或子集
  - RequestID, AgentID // 与 context 键 tool.ContextKey* 一致

RunOutput 至少包含：
  - FinalText
  - Messages           // 完整更新后历史（若调用方需要回写）
  - Trace              // *RunTrace 或结构化子类型
  - Usage
```

**与现状关系**：`ReActAgent.Run` 已接近上述契约；P3 阶段可将「循环 + trace 组装」抽为 **`type defaultRunner struct { *ReActAgent }`** 实现 `ConversationRunner`，Portal 仅依赖接口，便于 mock。

**规则**：

1. Portal 的 `ChatService`（或等价）**只负责**：鉴权、从 DB 加载历史、组装 `RunInput`、调用 `Run`、将 `Messages` / trace 写回。  
2. **禁止**在 Portal 内再实现一套 tool 循环（除非明确标注为 legacy 并逐步删除）。  
3. 若存在 `PlanExecuteAgent` 等变体，其 **worker 阶段**应复用同一 `ConversationRunner` 或共享内部 `runPlain` 级函数（与记忆设计文档中对 Planner 的约束一致）。

### 3.3 验收

- 新增一种入口（例如测试用假 HTTP）时，**不在新包内复制** `for step := 0; step < MaxSteps` 与 `ChatWithTools` 组合逻辑；仅构造 `RunInput` 并调用 Runner。（说明：此处**不是** Go 内存「零拷贝」，而是**逻辑不重复**。）  
- 可选 CI：`grep` 或静态规则维护「Portal 内禁止出现 `ChatWithTools`」清单（若采用 Runner 接口后启用）。

---

## 4. Provider 与内部 canonical 消息（G-O2）

### 4.1 问题

不同云厂商对 `tool_calls`、reasoning、system 位置、消息交替的校验不同。若在 `ReActAgent` 内散落 `if provider == X`，不可维护。

### 4.2 设计

**4.2.1 内部表示**

- 继续使用 **`[]model.Message`** 作为 **唯一真相**；字段集合以当前 OpenAI 兼容形态为基线。  
- 增加文档化枚举 **`APIMode`**（示例值）：  
  - `OpenAIChatCompletions`（默认）  
  - `OpenAIResponses`（若未来支持 Codex/Responses 形态）  
  - `AnthropicMessages`（若接入原生 Claude API）

**4.2.2 适配器接口与类型映射**

示意接口可落在 `model` 子包（如 `model/adapters`）：

```go
// ToolSchema 表示发给厂商的单工具 JSON Schema；可由 tool.Tool 转换而来。
type ToolSchema struct {
    Name        string
    Description string
    Parameters  any // JSON Schema 兼容 object
}

type RequestEncoder interface {
    Encode(messages []model.Message, tools []ToolSchema) (any, error)
}
type ResponseDecoder interface {
    Decode(raw []byte) (assistant model.Message, usage *TokenUsage, err error)
}
```

**映射说明**：当前 `OpenAIClient.ChatWithTools` 在 `model/openai_tools_stream.go` 等处将 `tool.Registry` → `[]openai.Tool`（内含 `FunctionDefinition`）。引入 `APIMode` 后，该转换应迁入 **`ToolSchema` 构建函数**（或保留 Registry 但构建路径单一），避免 `ReActAgent` 直接依赖 `openai` 结构体。

**4.2.3 解析顺序（与 Hermes 一致的思想）**

1. 显式配置 `APIMode`  
2. 否则按 Provider 名推断  
3. 否则按 BaseURL 启发式  
4. 默认 `OpenAIChatCompletions`

**4.2.4 失败语义**

- `Encode` 失败：本轮不发起 HTTP，`RunError` 带 `stage=encode`。  
- `Decode` 部分字段缺失：落入 **`model` 包内** 的容错表（与现有 `buildToolCallGeneration` 行为对齐），并在 Trace 记 `decode_repaired=true`。

### 4.3 验收

- 新增一种 `APIMode` 时，步进循环文件 **diff 行数**符合 G-O2 约定。  
- 单测：同一组内部 `Message` + `tool_calls`，经 Encode 再 Decode（在可逆范围内）**`tool_call_id` 集合不变**。

---

## 5. Prompt 稳定层与临时层（G-O3）

### 5.1 问题

把「本回合预算告警」「压缩摘要说明」写进与身份、工具说明同一字符串，会导致缓存键抖动（尤其对接 Anthropic prompt cache 时），且难以审计。

### 5.2 设计

**5.2.1 概念模型**

| 层类型 | 内容示例 | 是否进入「稳定哈希」 |
|--------|----------|----------------------|
| Stable | 身份、工具总述、技能索引、冻结记忆快照、AGENTS.md 类上下文 | 是 |
| Ephemeral | 本轮 token 压力、只出现一次的告警、动态路由提示 | 否 |

**5.2.2 API 形态（建议）**

- 在调用 `ChatWithTools` 前，由 **`PromptBuilder`** 产出：  
  - `StableSystem string`（或 `[]StableBlock` 拼接，块间分隔符固定为 `\n\n`）  
  - `EphemeralSuffix string`  
- **多 system 片段与厂商能力**：若目标 API 仅允许单条 system，则在 **Encode 阶段**将 Stable+Ephemeral 合并为一条；合并规则必须在 `APIMode` 文档中写明（**默认**：单条 `system`，顺序为 `Stable` 在前、`Ephemeral` 在后，中间 `\n\n---\n\n`）。  
- 与 **`model.Message.Metadata` / `sixath.origin`** 约定对齐（见 `design-memory-tools-hermes-parity.md`）：凡框架注入的说明类内容，必须可溯源。

**5.2.3 与压缩的关系**

- **先**决定 Stable/Ephemeral，**再**对 messages 做 `CompressMessagesByRunesBudget`（策略与记忆文档一致）。  
- Trace（可选字段）：`prompt_stable_hash`、`compressed`（bool）、`compression_kind`（string）。

### 5.3 验收

- Stable 不变、仅 Ephemeral 变化 → `prompt_stable_hash` 不变。  
- 对接需 cache control 的厂商时，cache breakpoint 仅覆盖 Stable 段（产品启用时）。

---

## 6. 工具运行时：Toolset、可用性、钩子（G-O4）

### 6.1 扩展现有 `tool.Tool`

在不大改现有 `Register(Tool)` 的前提下，**渐进式**增加可选字段（零值表示旧行为）：

| 字段（建议名） | 类型 | 语义 |
|----------------|------|------|
| `Toolset` | `string` | 逻辑分组；**已实现**：与 Hermes 对齐的 `web` / `file` / `skills` / `memory` / `terminal` 及 `mcp`，见 [toolsets-hermes-mapping.md](./toolsets-hermes-mapping.md) |
| `Available` | `func(ctx context.Context) error` | 非 nil 则本请求排除该工具 |
| `RequiresSequential` | `bool` | 为 true 表示交互类；触发 **D1**（第 7.2.1 节） |
| `RiskClass` | `string` 或枚举 | 供 `PermissionPolicy` 与 UI |

**兼容性**：未设置 `Available` 时等价于始终可用。

### 6.2 Toolset 解析

**配置形态**（示例）：

```yaml
tools:
  enabled_toolsets: ["core", "memory"]
  disabled_tools: ["execute_write"]
```

**解析算法**：

1. 若配置了 `enabled_toolsets`：仅保留 `Toolset` 属于并集的工具。  
2. 再应用 `disabled_tools` 与 `PermissionPolicy`（拒绝的工具不出 schema 或执行前拒绝，**与产品约定二选一并文档化**；默认建议 **不出 schema** 以减少无效 tool 调用）。  
3. 对每个工具调用 `Available(ctx)`；非 nil 则排除。  
4. （可选）**动态 schema 补丁**：参数枚举随当次可用工具收缩。

### 6.3 钩子与 EventBus

**并存策略**：

- **保留** `events.Bus` 的 `ToolInvoked` / `ToolExecuted`（已有）。  
- **新增**（可选）`[]ToolHook`，按注册顺序执行：

```go
type ToolHook interface {
    Before(ctx context.Context, name string, params map[string]any) (map[string]any, error)
    After(ctx context.Context, name string, result any, err error) (any, error)
}
```

**单工具执行顺序（写死）**：

1. 对每个 `ToolHook` 依次调用 `Before`（前一个输出作为后一个输入）。  
2. **`PermissionPolicy`**（与现有 `executeOneToolCall` 行为一致；**仍在所有 Before 之后**）。  
3. `Tool.Execute`。  
4. 对每个 `ToolHook` **按与 Before 相同顺序**调用 `After`（**写死：同序，非逆序**）。  
5. `Bus.Publish(ToolExecuted)`。

**Before 阻断语义（写死）**：`Before` 返回 error（或显式 `Block(reason)`）→ **不调用** `Execute` → 向模型写入 `role=tool` 结果，JSON 含 `blocked: true` 与可操作 `reason` → `Bus.Publish(events.HookBlocked)` → 返回模型继续循环。详见 [Harness Engineering 差距设计](../../docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md) §3.2 C1。

### 6.3.1 EvidenceGate（Final-answer evaluator）

与工具管线分离：在**结案路径**校验本 Run 累积的 `evidence_refs`（见 [gap design §3.3 / E1](../../docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md)）。

| 行为 | 约定 |
|------|------|
| 触发点 | 模型准备最终答复（无 tool_calls），或 MaxSteps / forced summary（`forceFinal`） |
| Soft 回压 | 证据不足时注入一次提示，要求补 `jaeger_trace`/`es_log_query` 或显式写「证据不足」；**循环内 Soft inject ≤1** |
| `forceFinal` | Soft **不加步**：仅 Metadata（如 `evidence_incomplete`）+ 事件；HardHalt 则返回 `ErrEvidenceGateHalt` |
| 默认关闭 | `EvidenceGate.Enabled=false`；Portal **仅 RCA 绑定**路径自动启用 |

实现：`agent.EvaluateEvidenceGate` + `WithReActEvidenceGate`。

### 6.4 Registry 与并发

- **`List` / `ListForAPI`**：构建 schema 时为读路径；**默认约定**：`Register` 仅在进程启动或技能加载阶段调用，热路径只读。若未来支持运行时 `Register`，须 **RW mutex** 或与 `sync.Map` 二选一并文档化。  
- **`Available(ctx)`** 可能被高频调用：实现应 O(1) 或带短 TTL 缓存；**禁止**在 `Available` 内执行网络 IO（除非显式配置并限流）。

### 6.5 安全与职责划分

| 机制 | 用途 | 禁止 |
|------|------|------|
| `ToolHook.Before` | 参数归一、注入默认值、审计 | 长时间阻塞、无界重试 |
| `PermissionPolicy` | 允许/拒绝执行 | 修改业务结果（应仅 bool / error） |
| `events.Bus` | 观测、指标、异步侧车 | 在同步 Listener 内做 IO（默认）；异步 Listener 内仍应避免 panic 拖垮 |

日志中输出 `params` 前应对 **密钥型字段** 做脱敏（与 Portal 日志策略一致）。

### 6.6 验收

- 缺环境变量 → 工具不出 schema。  
- 同一 `Registry`，不同 `enabled_toolsets` → 不同工具名集合。  
- `Before` 返回 error → 无 `Execute` 调用（mock 单测）。

---

## 7. Agent 循环语义（G-O5, G-O6, G-O7）

### 7.1 消息交替与规范化

**目标**：满足 OpenAI/Anthropic 等对 `user/assistant/tool` 顺序的常见约束。

**建议**：导出或包装 **`agent.NormalizeHistory(msgs []model.Message) ([]model.Message, error)`**，与现有 `stripLeadingOrphanToolsAfterSystem` 等组合使用，**单一入口**供 ReAct 与 Portal 拼历史共用。

**行为（默认策略，须单测覆盖）**：

- 禁止连续两条 **无 tool_calls 的** `assistant`：**合并文本**到前一条；若前一条含 `tool_calls` 则返回 **error**（无法无损合并，交由上层丢弃或重试）。  
- `tool` 消息允许连续；必须带有效 `tool_call_id`。  
- `system`：默认仅首部一条；若 PromptBuilder 产出 Stable+Ephemeral，在入模型前已合并（第 5.2.2 节）。  
- Trace：`history_normalized=true` 时附带 `normalize_notes`（string 或短数组）。

### 7.2 多工具并行与顺序写回

**7.2.1 策略 D1（v1 默认，评审固定）**

若 **`RequiresSequential` 为 true 的工具在本轮 `tool_calls` 中出现至少一个**，则该 assistant 步内 **全部** tool **串行**执行，顺序为模型给出的 `tool_calls` 顺序（与当前 `executeToolStep` 行为一致，**零行为回归**）。

**7.2.2 策略 D2（可选增强，远期）**

仅当 **所有** `RequiresSequential` 均为 false 时，允许并行：

- 使用 `errgroup.Group` 或 worker pool；**默认并发上限** `min(8, N)`，可配置 `tool.max_parallel`。  
- 第 `i` 个 tool 结果写入 `slot[i]`，**append 历史**时按 `i` 从 `0` 到 `N-1` 追加 `role=tool`。  
- 任一 tool 失败：默认 **该轮仍按序追加所有结果**（失败槽位为错误 JSON），**不**部分重试；若需「fail-fast」则另加配置 `tool.fail_fast` 并在 Trace 标明。

**7.2.3 现状说明**

当前 `executeToolStep` 已实现 **D1 中的串行子集**（整轮串行）。引入 D2 前须补充第 7.5 节所列并发单测与 race 检测。

### 7.3 context 取消

- `ChatWithTools` 与每个 `Execute(ctx, ...)` 使用同一 `ctx` 或子 `ctx`（同一 deadline）。  
- 取消后不再消费模型流；向调用方返回 `context.Canceled` 包装错误。

**7.3.1 内存态 vs Portal 已提交态（评审补充）**

| 状态 | 含义 | 取消时建议 |
|------|------|------------|
| 内存 `messages` 切片 | 本轮尚未 `Run` 返回 | 直接丢弃；不强制落库。 |
| Portal DB 已写入 user 消息 | 用户可见 | 保持；可标记 `run_status=canceled`。 |
| 尚未提交的 assistant（含 tool_calls） | — | **不得**以「半条 assistant」提交；若业务需要保留部分 tool 结果，采用 **整 assistant 步事务**：要么该步全提交，要么全不提交。 |

默认与 Hermes 精神一致：**不注入半截 assistant** 到持久化存储。

### 7.4 迭代预算

```go
type IterationBudget struct {
    MaxModelSteps int
    Current       int
}
```

- 主 `Run` 一份；子代理独立 `IterationBudget`，配置键建议 `delegation.max_iterations`。  
- 达上限：`stop_reason=budget_exceeded`，错误文案可 i18n。

### 7.5 验收

- **串行路径（现状）**：3 个 tool，`appendToolResultMessages` 顺序与 `tool_calls` 顺序一致（已有行为可加固测试）。  
- **并行路径（若实现 D2）**：随机完成顺序，历史顺序不变；`go test -race` 相关子包。  
- `ctx` cancel：Trace `canceled=true`；无悬挂 goroutine。

### 7.6 与 `MaxSteps` 的关系

- `ReActConfig.MaxSteps` 仍为**模型步数上限**（与现有语义一致）。  
- **`IterationBudget`** 可为同一数值的包装器，便于子代理单独裁剪；避免两处配置语义冲突（文档写明：**子代理默认 min(父剩余步数, delegation.max_iterations)**，或仅使用独立上限二选一并写死）。

---

## 8. 可观测与 Hermes「回调表」对齐

现有 `events.Kind`（`events/event.go`）已包含 `ModelInvoked`、`ModelResponded`、`ToolInvoked`、`ToolExecuted`、`ToolStarted`、`ToolCompleted`、`ToolFailed`、`PermissionDenied`、`RunCompleted`、`RunError` 等。

**扩展原则**：优先 **复用现有 Kind + 规范化 Payload 键**，避免平行枚举。

| 需求（Hermes 回调） | 建议做法 |
|---------------------|----------|
| 流式 token | 流式路径已用 `StreamEvent`；若需 Bus：可发 `RunStarted`/`ModelResponded` 子类型或 **新增** `ModelStreamDelta`（Payload: `delta`, `step`）。 |
| 工具进度 | 使用已有 `ToolStarted` / `ToolCompleted`，或扩展 Payload：`progress`（0–1）、`message`。 |
| 预算告警 | `ModelInvoked` 或新 Kind `BudgetWarning`（Payload: `remaining_steps`）。 |
| Clarify / 人机确认 | 与 `PermissionDenied` 或业务 Kind 区分；若新增 `ClarifyRequested`，须在 Portal 订阅文档中列出。 |

**原则**：Portal 与 CLI 优先订阅 **Bus**；不在 UI 中直接依赖 `ReActAgent` 私有字段。

---

## 9. 可选构建与依赖瘦身（G-O8）

### 9.1 手段

- **`//go:build`**：MCP、特定云 SDK、FTS 迁移等放入 tag 门控文件。  
- **子模块 / workspace**：`framework/ext/...` 承载重依赖。  
- **接口反转**：核心依赖接口，驱动在 portal 注册。

### 9.2 验收

- CI：`go list -deps -tags=minimal`（tag 名以最终实现为准）对比允许列表。

---

## 10. 持久会话与检索（G-O9，可选模块）

### 10.1 何时需要

跨进程会话、CLI+API 共用、或 framework 层 FTS 检索且与 Portal 解耦时。

### 10.2 技术选型

- `modernc.org/sqlite`：**WAL**、`schema_version`、版本化迁移。  
- **FTS5**；CJK 子串需求用 trigram 虚拟表（可选）。

### 10.3 与 Portal 的关系

- **方案 A（默认）**：Portal 权威；framework SQLite 为本地缓存，单向同步。  
- **方案 B（远期）**：framework 权威；Portal 经接口读写。

### 10.4 表字段建议（最小集）

- `sessions(id, source, started_at, parent_session_id, metadata_json)`  
- `messages(id, session_id, role, content, tool_calls_json, tool_call_id, created_at)`  
- `messages_fts`（可选）

---

## 11. 与记忆/护栏专项设计的关系

以下内容以 **design-memory-tools-hermes-parity.md** 为准，本文仅列接口边界：

- **Prefetch / 记忆围栏**：`memory.Orchestrator` 与 ReAct 在模型调用前协作；冻结记忆进入 Stable 的规则见该文档相关章节。  
- **压缩与 LLM 摘要**：Context engine 插件化在该文档中展开；**PromptBuilder** 与压缩顺序须与其一致。  
- **工具护栏**：该文档目标 G3；**IterationBudget** 与护栏互补。

---

## 12. 分阶段落地建议

| 阶段 | 内容 | 依赖 |
|------|------|------|
| P0 | 固定 `NormalizeHistory` 默认策略文档 + 单测骨架；**审计** `executeToolStep` 写回顺序与取消路径 | 无 |
| P1 | `Tool.Toolset` / `Available` / `ListForAPI`；Permission 与 schema 过滤策略（第 6.2 节） | P0 |
| P2 | `PromptBuilder` + Trace 可选字段 | P1 |
| P3 | `ConversationRunner` 接口 + `ReActAgent` 适配实现；Portal 改调接口 | P0–P2 |
| P4 | `APIMode` + Encoder/Decoder 第二厂商 | P3 |
| P5 | 可选 D2 并行工具 + `tool.max_parallel`；race 测 | P1 |
| P6 | 可选 `framework/session` + Portal 同步策略 | 产品选型 |

---

## 13. 风险与权衡

| 风险 | 缓解 |
|------|------|
| Prompt 双层增加认知负担 | 默认 Builder：单字符串 → 全 Stable |
| Hook + Bus 滥用 | 第 6.5 节职责表；代码评审检查 Listener 重量 |
| SQLite 与 Portal 双写 | 方案 A + 禁止双写同一 `session_id` 无同步器 |
| D2 并行改变确定性 | 默认关闭 D2；开启时 Trace 记 `parallel_tools=true` |

---

## 14. Trace 扩展字段（建议，与实现渐进一致）

| 字段 | 类型 | 说明 |
|------|------|------|
| `prompt_stable_hash` | string | Stable 块 SHA256 前缀 16 字符即可 |
| `compressed` | bool | |
| `compression_kind` | string | 如 `runes_budget`、`llm_summary` |
| `history_normalized` | bool | |
| `canceled` | bool | |
| `stop_reason` | string | `budget_exceeded`、`completed`、`error` |
| `parallel_tools` | bool | D2 开启时为 true |

---

## 15. 文档维护

- 实现任一阶段后更新 **版本历史** 表：版本、日期、PR、行为摘要。  
- 架构总览链接维护在 `framework/README.md`（若增加「设计文档」索引节）。

---

## 16. 开放问题（需产品/负责人拍板）

| ID | 问题 | 建议默认 | 建议 Owner |
|----|------|----------|------------|
| Q1 | `PermissionPolicy` 拒绝的工具是「不出 schema」还是「出 schema 但执行拒绝」？ | 不出 schema | 框架 + 产品 |
| Q2 | D2 失败后是否 fail-fast | 否（全量写回错误槽） | 框架 |
| Q3 | `ToolHook.After` 与 Before 同序或逆序 | 同序 | 框架 |
| Q4 | 子代理步数与父 `MaxSteps` 是否耦合 | `min(父剩余, cap)` | 产品 |

---

## 17. 版本历史

| 版本 | 日期 | 说明 |
|------|------|------|
| 1.0 | 2026-05-02 | 初稿：Hermes 理念在 Go/Sixath 下的运行时详细设计 |
| 1.1 | 2026-05-02 | 评审修订：导读/术语/实现映射、串并行默认策略 D1/D2、取消与持久化边界、事件与类型对齐、安全与并发、Trace 表、分阶段 P5/P6、开放问题表 |
