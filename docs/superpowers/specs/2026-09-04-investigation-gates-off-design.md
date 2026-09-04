# 调查闸默认关闭（Investigation Gates Off）

**日期**: 2026-09-04  
**状态**: 已确认（2026-09-04）  
**动机**: 8-09 起为堵住 8555 乱调、bf26 改题、e9d4 终答被冲，叠了工具面收窄、跨族丢掉、HTTP 接地、任务锁。叠在一起后，2d0430 类调查（库空必须打技能里的 ES）会空转推理、该调的工具调不成。产品选择先把这三层默认关掉，换回「看见已绑定工具就执行」，接受乱调/改题回归。  
**对照会话**:
- `2d0430f0-2369-43a3-9d64-b17a616159b7`：空转（要修的体验）
- `8555e9df-ad58-4c80-a22d-cbcc592be7db`：乱调（明确放弃）
- `bf26ea59-9116-41fc-91a7-8ffbe7a34919`：技能改题（明确放弃）
- `e9d4c37c-bd34-46df-8233-ab8539e9239b`：闸冲终答（明确放弃）
**对照规格**（代码保留，默认不再走）:
- [每轮工具面收窄](./2026-08-09-turn-tool-surface-design.md)
- [工具调用合理性](./2026-08-20-tool-call-reasonableness-design.md)
- [任务锁与问题漂移](./2026-08-24-task-lock-goal-drift-design.md)
- [Goal / Delivery](./2026-08-25-goal-lock-delivery-design.md)

**一句话**：部署默认关闭 A HTTP 接地、B 工具面+跨族丢掉、C 任务锁/改题回拉；闸代码不删，总开关打开即恢复现网。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 产品选择 | 方案 3，范围 A+B+C 全关 |
| 默认 | `chat.investigation_gates: off` |
| 实现 | 装配期关掉三层；不删除 `http_grounding.go` / `turn_intent_gate.go` / `task_lock.go` |
| 解析失败 | 缺省、空、拼写错 → `off` + warn，禁止 fail-closed 回 `on` |
| 单层覆盖 | 总开关是部署默认；`SATH_TURN_TOOL_SURFACE` / `SATH_TURN_INTENT_GATE` / `SATH_TASK_LOCK` 若已设置则覆盖对应层 |
| 不关 | 空击措辞闸、凭据索取闸、code claim、证据闸、SQL heal、spill、技能自动匹配 |

---

## 1. 目标与非目标

### 目标

1. 默认 `off` 时：本轮 registry 为全部已绑定工具（`PrepareTurnToolSurface` 返回 `active=nil`），跨族 `tool_call` 执行，未接地 `http_request` 执行，system prompt 不含【本轮任务锁】，`PostModelPolicy` 为空（不因族/HTTP/ident/改题丢掉后 Retry）。
2. `on` 时：8555/bf26/HTTP 接地/任务锁现网单测全绿，行为与 9734dea 合入后一致。
3. `off` 夹具：跨族 `skill_view`、指向技能外端口的 `http_request`、无任务锁文案，都必须 `PostModelContinue`（或根本不装 Gate）并执行。
4. `off` 时时间线不应再出现 PostModelRetry 造成的「连续只有 model、没有 tool」串。

### 非目标

- 删除闸源码或测试。
- 改技能自动匹配（`auto_route` 仍可灌 SKILL.md）。
- 关空击措辞闸、凭据闸、code claim、证据闸、SQL heal、query spill。
- 中途动态扩族、每步 LLM 裁判、拆子 Agent。
- 修 2d0430 的反引号锚点（已在 9734dea；`off` 时根本不跑 HTTP 接地）。

### 诚实上限

- `off` 后 8555 乱调、bf26 改题、e9d4 类终答被冲会回来。技能自动匹配仍在，没有任务锁纠偏，改题会更容易。
- `off` 只保证「模型提出的已注册工具会执行」；模型不提 `http_request` 仍查不成 ES。

---

## 2. 三层对照

| 代号 | 层 | 现网入口 | `off` 后 |
|------|----|----------|----------|
| A | HTTP 接地 | `TurnIntentGate.filterUngroundedHTTP`（随 `SATH_TURN_INTENT_GATE`） | 不丢 URL，不为此 Retry |
| B | 工具面 + 跨族丢掉 | `PrepareTurnToolSurface` + `filterCallsByFamily` | 全量 registry；跨族执行 |
| C | 任务锁 / 改题 / ident idle | `AppendTaskLock` / `MergeTaskLockMetadata`；`EvaluateIdle` 的 `goal_drift` / `ident_lock` | 不钉 Q，不注入改题/未查 ID Retry |

A 与 B 的跨族丢掉、discovery_loop、intake `ask_user` 都挂在同一 `TurnIntentGate` 上。关掉 intent gate 等于 A 以及这些 Evaluate/EvaluateIdle 规则一起停。这是刻意的：不单独留 HTTP 接地。

---

## 3. 数据流

`off`：

```text
用户原文
  → BuildRegistry(全部已绑定；ActiveFamilies=nil)
  → 不追加【本轮任务锁】
  → ReAct：模型 tool_calls → 直接执行
```

`on`：保持 9734dea 后现网（IntentResolver → 收窄 registry → 任务锁 → TurnIntentGate）。

装配点（只改这些，不改 ReAct 循环本身）：

| 点 | 文件 | `off` 行为 |
|----|------|------------|
| 配置 | `portal/internal/conf/chat_config.go`、`portal/configs/config.yaml` | 新字段 `investigation_gates` |
| 启动 | `portal/cmd/backend/main.go` | 应用到 chat 包三层 setter |
| 工具面 | `SetTurnToolSurfaceEnabled(false)` | 已有 |
| Intent gate | 新增与 surface 同构的 process override；`NewTurnIntentGate()` 读它 | `nil` policy |
| 任务锁 | `chat.go`（同步+SSE）、`agent.go` 的 `AppendTaskLock` | 跳过 append 与 metadata |

`PrepareTurnToolSurface` 在 surface 关闭时已返回 `active=nil` 且 `Source=disabled`。`BuildRegistry` / `RegisterAgentRuntimeTools` 对 nil active 已是全量。不要再分叉一套 registry 逻辑。

---

## 4. 配置与优先级

YAML：

```yaml
chat:
  investigation_gates: off   # on | off；缺省或非法值 = off
```

环境变量 `SATH_INVESTIGATION_GATES`（`on`/`off`/`1`/`0`/`true`/`false`）覆盖 YAML 总开关。

**单层覆盖**（仅当该变量已设置）：

| 变量 | 层 |
|------|----|
| `SATH_TURN_TOOL_SURFACE` | B 工具面 |
| `SATH_TURN_INTENT_GATE` | A + 整闸（含跨族、HTTP、idle 改题） |
| `SATH_TASK_LOCK` | C 任务锁文案（新；未设置则跟总开关） |

解析顺序：单层 env（若 set）→ 总开关（YAML 或 `SATH_INVESTIGATION_GATES`）→ 总开关缺省 `off`。

禁止：非法总开关当 `on`。

---

## 5. 错误处理

- 工具执行失败（SQL、ES 4xx）仍回传模型；不得再把失败伪装成「只有推理」。
- 配置非法只 warn + `off`，进程继续启动。
- `off` 时 `TurnIntentGateOption` 必须是 no-op（已有 `NewTurnIntentGate()==nil` 路径），避免 `BuildReActAgent` 内部又装上默认 gate。

---

## 6. 测试

现网 `on`：不改断言。跑现有 `turn_intent_gate_test`、`http_grounding_test`、`task_lock*_test`、`turn_surface_wire_test` 时把总开关/单层 env 设为开启（测试里本来就直接构造 Gate 或 `t.Setenv`）。

新增 `off`：

1. `LoadChatFromConfigPath`：缺 `investigation_gates` → off；`garbage` → off。
2. `investigation_gates: off` 时 `ToolSurfaceEnabled()==false`、`NewTurnIntentGate()==nil`、`AppendTaskLock` 原样返回（无【本轮任务锁】）。
3. 直接 `Evaluate` 不作为 `off` 验收（gate 为 nil 根本不会被调用）；用装配夹具或 `NewTurnIntentGate` 为 nil 断言。
4. `SATH_TURN_INTENT_GATE=1` 在总开关 off 时仍能装上 gate（单层覆盖）。

禁止把 `_neo4j_q/` 会话 JSON 当测试夹具。

---

## 7. 验收对照

| 开关 | 必须成立 | 明确放弃 |
|------|----------|----------|
| off | 2d0430 类未接地/技能内 ES `http_request` 不被丢掉、无无限 Retry | 8555 跨族乱调可以发生 |
| off | system prompt 无【本轮任务锁】 | bf26 技能改题可以发生 |
| on | HTTP 接地、拆族、任务锁单测绿 | — |
