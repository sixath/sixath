# 每轮工具面收窄（Turn Tool Surface）设计

**日期**: 2026-08-09  
**状态**: 已确认（2026-08-09）  
**动机**: Agent 同时绑定多个 MCP（如 GitLab、Confluence）与 RCA 内置工具时，模型意图识别不准，会把多域能力糊成一个模糊目标，并调用无关工具（典型：用户问 GitLab，却调 `jaeger_trace`）。现有 `TurnIntentGate`（终答丢工具 + 少数 drift-sensitive 工具 token 重叠）拦不住此类跨族误调。

**关联**:
- framework: `PostModelPolicy`（`framework/agent/post_model_policy.go`）
- portal: `TurnIntentGate`（`portal/internal/chat/turn_intent_gate.go`）、`BuildRegistry` / `BuildReActAgent`
- 前序话题漂移：issue7 / L2 v0（终答 + web/knowledge overlap）
- 非本设计：鉴权失败 fail-closed（401 后禁换族）、子 Agent 拆分、MCP stdio 本身

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 根因定位 | 意图识别不准 → 乱调无关 RCA 工具 + 多 MCP 糊成一团 |
| 主路径 | **方案 1**：Turn-scoped Tool Surface（装配收窄）+ 扩展 `TurnIntentGate` 兜底 |
| 意图来源 | **混合**：规则/启发式优先；低置信或显式多意图 → 轻量分类步 |
| 分类仍低置信 | **Fail-narrow**（候选族 ∪ `core`；候选空则仅 `core`）；**禁止 Fail-open** |
| 族划分 | **混合**：每个已绑定 MCP Server 自动成族 `mcp:<server_id>`；内置工具用固定目录标签 |
| 同轮扩族 | 一期不做 ReAct 中途动态扩族；换域以下一轮重新 Resolve；多意图在装配时并集 |
| 关闭开关 | `SATH_TURN_TOOL_SURFACE=0` 时回到全量绑定工具 + 现有 TurnIntentGate v0 |

---

## 1. 目标与非目标

### 目标（一期）

1. 本轮意图清晰时，模型**只看见**相关工具族 ∪ `core`，从源头减少多域糊成一团。
2. 用户仅问 GitLab 时，registry 中**不应出现** `jaeger_trace` 等 RCA 工具（除非意图明确命中 `rca` 或显式多意图并集）。
3. 漏网跨族 `tool_call` 由扩展后的 `TurnIntentGate` 在执行前丢弃。
4. 规则优先；低置信/多意图走轻量分类；失败路径 Fail-narrow，永不因错误放大工具面。

### 非目标（一期不做）

- 拆子 Agent / 改变「一 Agent 绑多 MCP」的产品模型
- 通用鉴权 fail-closed（工具 401 后禁止换族续跑）——可二期叠加
- 同一次 ReAct 循环中途动态扩族
- 独立重型意图微服务；分类步须短、结构化、可超时降级
- 把关键词表产品化到完整 Admin UI（一期代码内置 + env 总开关即可；YAML 外置可二期）

---

## 2. 架构与每轮数据流

```text
SendMessage(user)
  → IntentResolver(user, BoundFamilies)
       规则打分
         ├─ 唯一高分 → ActiveFamilies = {that} ∪ {core}
         └─ 低置信 | 多高分 → Classifier(BoundFamilies, Candidates)
              ├─ 成功 → ActiveFamilies = 所选 ∪ {core}
              └─ 仍低置信 / 失败 → Fail-narrow(Candidates ∪ {core}
                 或仅 {core})
  → BuildRegistry(仅 ActiveFamilies ∪ core 内的工具 / MCP)
  → BuildReActAgent(+ TurnIntentGate 持有 ActiveFamilies 与 tool→family)
  → 模型若仍提出跨族 tool_call → Gate Filter / Finish
```

### 2.1 组件落点

| 组件 | 位置 | 职责 |
|------|------|------|
| 族目录 / 关键词表 | `portal/internal/chat` | 内置族标签、MCP 自动成族、规则别名 |
| `IntentResolver` | `portal/internal/chat` | 规则 → 可选分类 → ActiveFamilies |
| `BuildRegistry` 过滤 | `portal/internal/chat/agent_builder.go`（或紧邻） | 只注册 Active 族工具 |
| `TurnIntentGate` 扩展 | `portal/internal/chat/turn_intent_gate.go` | 族感知过滤 + 保留 v0 规则 |
| framework | 原则上不改 `PostModelPolicy` 接口 | ActiveFamilies 由 Portal 侧 Gate 结构体/闭包携带 |

---

## 3. 族目录

| Family ID | 来源 | 典型成员 |
|-----------|------|----------|
| `core` | 内置固定 | memory / todo / skills / session 等常开能力 |
| `rca` | 内置固定标签 | `jaeger_trace`、`es_log_query` 等 |
| `web` | 内置固定标签 | `web_search`、`web_extract`（仍受 Agent `webToolsEnabled` 约束） |
| `knowledge` | 内置固定标签 | `knowledge_search`、`knowledge_read` 等 |
| `mcp:<server_id>` | 每个已绑定 MCP Server 自动成族 | 该 server `ListTools` 注册的全部工具 |

**BoundFamilies**：Agent 本轮理论上可启用的族（已绑定 MCP + 已启用的内置族）。  
**ActiveFamilies**：本轮实际暴露的族；`core` 永远属于 Active。

Legacy `type=mcp` Tool：一期归入以其 MCP `id`（或等价 server 标识）形成的 `mcp:<id>` 族；若无法解析 id，则不进入可收窄族（实现计划中写清兜底：视为独立族或挂 `core` 之外的 `legacy_mcp`——**选定：无法解析则单独 `mcp:legacy:<tool_id>`，默认不进规则高置信，除非 user 文本命中工具名**）。

---

## 4. IntentResolver

### 4.1 输出

| 字段 | 说明 |
|------|------|
| `ActiveFamilies` | 本轮暴露族 |
| `Confidence` | `high` \| `low` |
| `Source` | `rules` \| `classifier` \| `fail_narrow` |
| `Candidates` | 规则命中但未独赢的候选，供分类/收窄 |
| `Reason` | 写入 trace / 日志 |

### 4.2 规则层

1. 对 user 文本做与现有 `TurnIntentGate` 类似的 token 化（含 CJK bigram）。
2. 各族维护关键词/别名（例：gitlab、mr、pipeline → `mcp:gitlab`；trace、jaeger、span → `rca`；confluence、wiki → `mcp:confluence`）。
3. MCP 族额外用 `server_id` / display name 参与匹配。
4. 打分后：
   - **唯一高分族** → `Active = {that} ∪ {core}`，`Confidence=high`，`Source=rules`
   - **多个高分族** → 标多意图，进入分类步（带上 Candidates）
   - **无一高分** → `Confidence=low`，进入分类步

### 4.3 分类步

- 一次短结构化调用：仅在 `BoundFamilies` 内选 1..N 个族（禁止发明未绑定族）。
- 成功且置信够 → `Active = 所选 ∪ {core}`，`Source=classifier`
- 仍低置信 / 解析失败 / 超时 / 模型错误 → **Fail-narrow**：`Active = Candidates ∪ {core}`；Candidates 空则 **仅 `core`**
- 分类选出未绑定族 → 忽略非法项；若清空则 Fail-narrow
- 分类模型：复用 Agent 同配置或进程级轻量模型（实现计划选定）；**超时/失败必须 Fail-narrow，不得 Fail-open**

### 4.4 会话边界

- 每轮 `SendMessage` 重新 Resolve；上一轮 `ActiveFamilies` **不继承**。
- 用户换域：下一轮重新计算即可。

---

## 5. 装配与门控

### 5.1 BuildRegistry

在现有 `BuildRegistry(tools, servers, …)` 路径增加过滤：

- 仅注册 `ActiveFamilies` 内的 MCP Server（及对应 legacy MCP 工具）
- 仅注册带对应族标签且族在 Active 中的内置工具
- `core` 族工具始终注册（若 Agent/进程开关本身允许）

### 5.2 TurnIntentGate 扩展

在现有能力之上增加：

1. **族感知过滤**：Gate 持有本轮 `ActiveFamilies`（已含 `core`）+ `tool→family` 映射；若 call 所属族 ∉ `ActiveFamilies` → `PostModelFilter` 丢弃；全丢则 Finish，`Reason=family_not_active`
2. **装配已收窄时仍保留 Gate**：防止旁路与未来漏配
3. **保留 v0**：`final_answer_discard_tools`；对 drift-sensitive 工具的 topic overlap 继续生效；与族过滤为 **AND**（先族，再 overlap）

### 5.3 开关

| Env | 行为 |
|-----|------|
| `SATH_TURN_TOOL_SURFACE=0`（及 false/off/no） | 不收窄 registry；意图解析跳过 |
| `SATH_TURN_INTENT_GATE=0` | 现有：关闭整个 PostModelPolicy Gate（含 v0）；surface 收窄仍可独立存在，但推荐文档说明两者关系——**一期约定**：surface=0 时不注入族过滤所需映射；intent gate=0 时不挂 PostModelPolicy（与今日一致）。surface 开启且 intent gate 关闭时，仅靠装配收窄、无执行前兜底（允许，需在测试中覆盖） |

---

## 6. 错误处理与可观测性

| 场景 | 行为 |
|------|------|
| 规则高置信唯一族 | 只装该族 ∪ core |
| 分类超时 / JSON 非法 / 模型错误 | Fail-narrow；写 Reason；禁止全量工具 |
| 分类选出未绑定族 | 忽略非法项；清空则 Fail-narrow |
| 用户换域（下一轮） | 重新 Resolve |
| surface 关闭 | 全量绑定 + 现有 Gate v0（若未关） |

**可观测性**：trace/日志字段 `active_families`、`confidence`、`source`、`candidates`、gate `Reason`。默认不向终端用户展示内部 Reason；若因仅 `core` 无法完成任务，由模型据可见工具如实说明。

---

## 7. 测试金路径

1. **GitLab 单意图**：Bound=`{mcp:gitlab, rca}`；user「查 GitLab 项目」→ registry **无** `jaeger_trace`；模型硬编该名时 Gate 丢弃（`family_not_active`）。
2. **显式多意图**：user「GitLab 部署失败，看下 Jaeger」→ Active 含 `mcp:gitlab` 与 `rca`。
3. **低置信 Fail-narrow**：无关键词 / 分类失败 → 仅 `core`（或少量 Candidates），RCA 与 GitLab 均不可见（Candidates 空时）。
4. **回归**：纯 RCA 问询仍可见 `jaeger_trace`；关闭 surface / gate 时行为符合 §5.3。
5. **单测**：IntentResolver 打分、分类 mock、BuildRegistry 过滤、Gate `family_not_active`。

---

## 8. 风险与后续

| 风险 | 缓解 |
|------|------|
| 规则误杀合法交叉查询 | 多意图关键词并集；分类步；用户下轮换域 |
| Fail-narrow 过狠（仅 core） | Candidates 保留规则弱命中；可观测 Reason；二期可 ask-user |
| MCP 新 server 无别名 | `server_id` / name 参与匹配；分类步兜底 |
| 分类增加延迟 | 仅低置信/多意图触发；短超时 + Fail-narrow |

**二期可选项**：鉴权 fail-closed；YAML/UI 关键词；同轮升族；ask-first 澄清。
