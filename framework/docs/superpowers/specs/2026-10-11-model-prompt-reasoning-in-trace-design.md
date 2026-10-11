# 执行轨迹记录模型提示词与推理设计

**版本**: 0.1
**状态**: 待实现
**日期**: 2026-10-11
**关联**: [event_requirements.md](./event_requirements.md)、[design-agent-runtime-hermes-inspired.md](./design-agent-runtime-hermes-inspired.md)

---

## 1. 背景与问题

执行轨迹（turn_trace / 时间线）当前只能看到**工具的输入与输出**（`ToolCalls` / `tool_call` 事件），以及聚合 token 统计。**模型每次调用发送的提示词（prompt / messages）与模型的推理结果（reasoning_content / 终答）不可见**：

- `RunTrace`（framework harness）只记录 `ToolCalls`、token、各类 nudge 计数，不含模型调用明细；
- 实时 SSE `model_call` 事件由 `ModelInvoked`/`ModelResponded` 事件的载荷映射而来，而这两个事件只带 `message_count`、token、`text_length` 等摘要——既无完整 messages，也无 reasoning / 终答；
- 排查"模型为什么这样决策""这条工具调用基于什么提示词""模型推理到哪一步卡住"等场景时，只能猜测。

**根因**：模型调用路径上 `messages`（本次发送的完整 prompt）与 `gen`（返回的 `Text` 与 `ToolStep.ReasoningContent`）在 harness 循环里都在手边，但从未写入任何 trace。

---

## 2. 目标与非目标

### 2.1 目标

| ID | 目标 | 成功标准 |
|----|------|----------|
| G-T1 | 持久化轨迹（turn_trace）每条模型调用可见完整 prompt | 回查 turn_trace 时，每次模型调用能看到发送的 messages（role/content/tool_calls，脱敏+裁剪） |
| G-T2 | 每条模型调用可见推理链与终答 | 回查 turn_trace 时能看到 `reasoning_content`（thinking 模型）与最终 `Text`、`finish_reason`、token |
| G-T3 | 实时 SSE 时间线可见推理链与终答 | `model_call` 时间线节点展开后能看到 reasoning + answer + finish_reason + token（不含完整 prompt，见 §4） |
| G-T4 | 不破坏现有展示 | 新字段全部 `omitempty`；老前端 / 老数据向后兼容 |

### 2.2 非目标（本期）

- 前端不做独立"模型调用详情页"（见决策 §3，本期在现有轨迹/时间线内加可折叠区块）。
- 实时 SSE 不携带完整 prompt（见决策 §3，prompt 走持久化回查）。
- 不改 turn_trace 的 DB 列结构（新字段随现有 payload_json 落库）。
- 不做提示词/推理的可搜索索引。

---

## 3. 已确认决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 改动层级 | framework + portal + web 端到端 | 用户确认"两处都要" |
| 提示词范围 | 每轮完整 messages（role/content/tool_calls），裁剪到固定长度 | 用户确认 |
| 推理结果 | `reasoning_content` + 终答 `Text` | 用户确认 |
| 消息体积 | 单条 message 内容裁剪到固定上限（默认 2048 字节，UTF-8 边界），脱敏 | 用户确认"裁剪到固定长度（推荐）" |
| 保留策略 | 上限之内全保留，超限才截；优先保留靠后调用 | 用户确认"超上限才截" |
| 实时 SSE 载荷 | **只带 reasoning + answer + finish_reason + token**，不带完整 prompt | `messages` 走持久化回查；实时你关心模型"想什么/回了什么"，SSE 省带宽，见 §4 |
| web 展示位置 | 现有轨迹/时间线"模型调用"节点内加可折叠区块 | 用户确认"工具轨迹内加模型调用区块（推荐）" |

---

## 4. 全链路数据流

```
harness ReActAgent                            portal                           web
─────────────────                           ──────                         ─────
messages + gen(Raw.ReasoningContent)
  │
  ├── 记录 RunTrace.ModelCalls ──→ BuildTurnTrace ──→ TurnTrace.ModelCalls ──→ 持久化回查
  │       (完整 prompt + reasoning + answer)              (payload_json，无需新列)
  │
  └── enrich ModelResponded/ModelInvoked 事件 payload
             (reasoning + answer + finish_reason + token，不含完整 prompt)
                    ──→ modelCallEventFromBus → ModelCallPayload
                              → SSE model_call → ApplyModelCall 时间线节点 ──→ 实时轨迹
```

- **持久化路径**：framework `RunTrace.ModelCalls` 携带完整（裁剪+脱敏）messages + reasoning + answer；`BuildTurnTrace` 映射进 `TurnTrace`，随现有 payload_json 落库。
- **实时路径**：`ModelResponded` 事件载荷补充 `reasoning`、`answer`、`finish_reason`（`ModelInvoked` 保持只带 step/mode/message_count）；portal `modelCallEventFromBus` 读取并补进 `ModelCallPayload`，SSE 下发；web 时间线节点展示。

---

## 5. 数据结构（framework）

`harness/trace.go` 新增：

```go
// ModelCallRecord 记录一次模型调用：发送的提示词（裁剪+脱敏）+ 推理与终答。
type ModelCallRecord struct {
    Step         int              `json:"step"`
    Mode         string           `json:"mode,omitempty"`  // tools / plain / stream
    Messages     []TracedMessage  `json:"messages,omitempty"`
    Reasoning    string           `json:"reasoning,omitempty"`  // thinking 模型 reasoning_content
    Answer       string           `json:"answer,omitempty"`     // gen.Text 终答
    FinishReason string           `json:"finish_reason,omitempty"`
    InputTokens  int              `json:"input_tokens,omitempty"`
    OutputTokens int              `json:"output_tokens,omitempty"`
}

// TracedMessage 序列化的一条提示词消息（裁剪+脱敏后）。
type TracedMessage struct {
    Role        string             `json:"role"`
    Content     string             `json:"content,omitempty"`
    ToolCallID  string             `json:"tool_call_id,omitempty"`
    ToolCalls   []TracedToolCall   `json:"tool_calls,omitempty"`
}
```

`RunTrace` 新增字段与方法：

```go
ModelCalls []ModelCallRecord `json:"model_calls,omitempty"`

// recordModelCall 记录一次模型调用（messages 提示词 + gen 推理/终答）。
func (t *RunTrace) recordModelCall(messages []model.Message, gen *Generation, mode string, step int)
```

`recordModelCall` 职责：
1. 用现有 `redact.String` / `redact.Value` 脱敏 messages 中每个字符串、reasoning、answer；
2. 单条 message `Content` 与 tool_calls 参数裁剪到固定上限（默认 2048 字节，UTF-8 边界截断 + 追加截断标记），复用 `truncateUTF8Bytes` 类 helper；
3. `gen == nil` 时只记录调用事实（step/mode），跳过 reasoning/answer；
4. `Reasoning` 取自 `gen.Raw.(model.ToolStep).ReasoningContent`；
5. 保留策略：总量上限内全保留；超限时截断，优先保留靠后调用。

`model.TracedMessage` 的 tool_calls 序列化——`ToolStep.ToolCalls` 含 `ID/Name/Arguments`，据此构造 `TracedToolCall`：

```go
type TracedToolCall struct {
    ID        string         `json:"id,omitempty"`
    Name      string         `json:"name,omitempty"`
    Arguments map[string]any `json:"arguments,omitempty"`  // 脱敏+裁剪
}
```

---

## 6. 采集点（harness）

`runModelResponded` / `modelRespondedPayload`（[react_agent.go](E:\workspace\github\sixath\sixath\framework\harness\react_agent.go:390)）并排调用 `trace.recordModelCall(messages, gen, mode, step)`，覆盖主循环全部模型调用：

- 主循环 tools 路径 `ChatWithTools`（~L445）
- 主循环 plain 路径 `Chat`（~L747）
- 流式路径 `ChatStream`（~L770）与 `ChatWithToolsStream`（~L917）——两者在拿到 `finalGen` 后调用，同时在流式期间无法提前记录 prompt，故流式调用点同样在拿到 `gen` 后一并记录
- `runPlain` 的 `Chat`（~L1056）与兜底 `Chat`（~L1312）
- `plan_agent.go` 的规划调用（若复用 `RunTrace`）

同时丰富 `ModelResponded` 事件载荷（`modelRespondedPayload` 内）：
- 加 `reasoning`（`ReasoningContent`，脱敏+裁剪）
- 加 `answer`（`gen.Text`，脱敏+裁剪）
- 加 `finish_reason`（`gen.FinishReason`）
- `ModelInvoked` 载荷保持不变（仅 step/mode/message_count；不含 prompt，满足 §3 实时不带完整 prompt 的决策）

`modelRespondedPayload` 现签名接受 `model.Generation` 与 `step`；事件载荷需具备 messages 才能包含 reasoning tile 时无需 messages，仅从 `gen` 提取即可，故此处无需改签名依赖 messages。

---

## 7. TurnTrace（framework）与持久化

`harness/turn_trace.go` 的 `BuildTurnTrace`：把 `tr.ModelCalls` 复制映射进 `TurnTrace`（复用 redact 与裁剪 helper），随现有 payload_json 落库。`TurnTrace` 净增字段：

```go
ModelCalls []ModelCallRecord `json:"model_calls,omitempty"`
```

由于 `ModelCallRecord` 在 `recordModelCall` 时已裁剪+脱敏，`BuildTurnTrace` 直接浅拷贝 `tr.ModelCalls` 映射即可，无需二次裁剪。turn 内模型调用数量也按 `maxCalls` 上限收紧（多为工具多、模型调用不会超，但兜底保证 payload 体积），超限时优先保留靠后调用。portal 的 `turn_trace_persist.go` 无需改列结构——payload_json 自动带上新字段。

---

## 8. portal `ModelCallPayload` / 时间线

`ModelCallPayload`（[chat_stream.go](E:\workspace\github\sixath\sixath\portal\internal\service\chat_stream.go:90)）净增字段：

```go
type ModelCallPayload struct {
    Step         int              `json:"step"`
    Phase        string           `json:"phase"`
    Mode         string           `json:"mode,omitempty"`
    Model        string           `json:"model,omitempty"`
    InputTokens  int              `json:"input_tokens,omitempty"`
    OutputTokens int              `json:"output_tokens,omitempty"`
    MessageCount int              `json:"message_count,omitempty"`
    EstimatedCostUSD float64      `json:"estimated_cost_usd,omitempty"`
    // 新增：
    Reasoning    string           `json:"reasoning,omitempty"`
    Answer       string           `json:"answer,omitempty"`
    FinishReason string           `json:"finish_reason,omitempty"`
}
```

`modelCallEventFromBus`（[chat_stream.go](E:\workspace\github\sixath\sixath\portal\internal\service\chat_stream.go:189)）在 `ModelResponded` 分支读取 `reasoning`/`answer`/`finish_reason`（`ModelInvoked` 分支不读，仅 step/mode/message_count）。

`timeline_persist.go` 的 `TimelineAccumulator.ApplyModelCall` 与 web `ModelCallPayload` 类型同步扩展，把 reasoning/answer/finish_reason 并入模型节点，供持久化的时间线回放。

---

## 9. web 展示

`web/src/api/chatStream.ts` 的 `ModelCallPayload` 接口补 `reasoning`/`answer`/`finish_reason` 字段。web 时间线模型调用节点（`ApplyModelCall` 对应组件）内加可折叠区块，展示：

- **sent 提示词**（仅持久化回查路径有：turn_trace 的 messages；实时模板没有，为空则隐藏）
- **推理链**（reasoning，thinking 模型才有，为空则隐藏；内容较长默认折叠）
- **终答**（answer + finish_reason，可为空）
- **token**（input/output，已有）

门控：字段为空（`omitempty` 未下发）时相应区块不渲染，老前端不受影响。

---

## 10. 错误处理与边界

- **脱敏**：messages/reasoning/answer 全部经 `redact.String`/`redact.Value`，绝不泄漏 API key 或凭据；这些值本就不在 messages 中，但工具产生的中间内容也要遮盖。
- **裁剪**：单条 message、reasoning、answer 各自按 2048 字节上限（UTF-8 边界截断 + 截断标记）；超总量上限（复用 `maxCalls` 风格）时丢弃较早调用，保留靠后（终答相关优先）。**超上限才截**。
- **流式**：流式返回无具名终止帧时 `FinishReason == "eof"`；`gen == nil` / `finalGen` 缺失时只记录调用事实，不 panic。
- **兼容**：所有新字段 `omitempty`；`ModelCallRecord`、`TracedMessage`、`ModelCallPayload` 均后向兼容；老前端不认识的字段忽略。
- **脱敏后长度**：保证 `Messages` 中任意字符串在裁剪后仍为合法 UTF-8。

---

## 11. 测试要点

- **framework unit**：
  - `trace_test.go`：`recordModelCall` 记录 messages/reasoning/answer/finish_reason；脱敏（API key 样式值被遮盖）；单条超长 message 被裁剪到上限且 UTF-8 合法；`gen==nil` 只记录事实；超总量上限保留靠后调用。
  - `trace_json_test.go`：`RunTrace` JSON 序列化含 `model_calls`，且空值 `omitempty`。
  - `react_agent_events_token_test.go` 类：`ModelResponded` 事件载荷含 reasoning/answer/finish_reason。
  - `turn_trace_test.go`：`BuildTurnTrace` 把 `ModelCalls` 映射进 `TurnTrace`。
- **portal unit**：
  - `chat_stream` 相关：`modelCallEventFromBus` 在 `ModelResponded` 分支映射新字段，`ModelInvoked` 不映射。
  - `timeline_persist` / `ApplyModelCall`：模型节点含新字段。
- **web**（若本期覆盖前端）：`chatStream.ts` 类型与组件渲染新字段的 smoke。
- **端到端**（可选）：`trajectory_phase1_integration_test.go` 扩展——带 reasoning 的回合，turn_trace 回查含 prompt 与推理。
