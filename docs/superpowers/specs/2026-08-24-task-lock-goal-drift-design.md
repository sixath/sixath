# 任务锁与问题漂移遏制（Task Lock）

**日期**: 2026-08-24  
**状态**: 已确认（2026-08-24）  
**动机**: 会话 `bf26ea59-9116-41fc-91a7-8ffbe7a34919` 追问「access-service 有没有收到游戏启动成功事件、vm-manager 有没有 startGame 成功」。前几步仍在查该流水，vm-manager / access-service 索引空命中后，自动匹配的 `rca-sync-archive-migrate` 把任务改写成「先收集 flow_id」；`ask_user` 被误拦后去查无关 Mongo，终答变成能力清单。一轮在模型不再 `tool_call` 时正常结束——**结束条件不检查原句是否被回答**。  
**对照**:
- 现网结束条件：ReAct `!ToolStep.Used` + 终答闸不 inject；`TurnIntentGate` 的 `topic_drift` 不含 `skill_view` / `ask_user` / `execute_read`
- 工具面：[每轮工具面收窄](./2026-08-09-turn-tool-surface-design.md)、[工具调用合理性](./2026-08-20-tool-call-reasonableness-design.md)（管跨族，不管改题）
- 源码声称闸：[code-intel](./2026-08-20-code-intel-cursor-parity-design.md)（管终答与代码是否一致，不管是否还在答用户句）
- 任务处理现状：[收到任务后怎么处理](./2026-08-15-task-handling-current-design.md)
- 对照会话：`bf26ea59-9116-41fc-91a7-8ffbe7a34919`（agent `a3af7bc6-6888-4dde-b782-ef2bfcb04df1`）
- 行业参照：OpenAI Model Spec（tool/引用文本 **No Authority**）；Anthropic Skills 渐进披露（system 只放 name+description）；goal drift 文献（目标必须外置，不能只活在最近一步上下文里）

**一句话**：本轮用户原句 Q 是循环不变量。Skill / 工具族 / 具体工具都只是答 Q 的假设；假设失败（空命中、错 repo、脚本不存在、手册不重叠）必须换手段或据实答 Q，禁止把 Q 换成手册的 Q′。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 问题分类 | **问题漂移 / goal drift**（改题），不是「少查了一次 ES」 |
| 主路径 | **任务锁**：不变量只有本轮用户原句 Q；会话里已出现的**不透明取值**可钉回，但不维护 flow_id/uid/gid 这类业务字段表。失败假设不得改题；停手改题则注入一次「仍答 Q」 |
| 权威 | 用户本轮原文 > 会话已出现取值 > Skill/工具结果。禁止「已自动匹配则请**优先遵循**此工作流 / 严格遵循」 |
| Skill 注入 | 默认渐进披露（只 name+description）；全文注入仅高置信（用户原文含技能名，或分差达标）且必须带「不得改题」条款 |
| 结束条件 | 仍以「无 tool_calls」为候选终态，但增加 **改题检测 → 最多注入 1 次再终答**；不新加每步 LLM 裁判 |
| 打分卫生 | **附件**：修短标签子串、中英分词，减少误命中。单独修打分不等于本设计已完成 |
| 不另起炉灶 | 不拆子 Agent、不强制走 MEA、不按技能名/表名黑名单、不每步第二模型当裁判 |
| 与 8-20 关系 | 8-20 保证「看不见不该看见的族」；本设计保证「看见了错手段也要回到 Q」 |

---

## 1. 目标与非目标

### 目标（一期）

1. **任务锁**：每轮冻结本轮用户原文 **Q（唯一不变量，与业务无关）**。另收集会话里已出现的**不透明取值**（`key=value` 右侧、反引号/代码 span、含数字的标识形 token），供「不要把已在上下文里的东西再问一遍」使用。平台**不**认识 `flow_id` / `uid` / `ugid` / 订单号等字段名。Q 在本轮不可被 Skill 或工具结果改写。
2. **权威**：Skill 正文、自动匹配横幅、工具返回值不得覆盖 Q。与用户目标冲突时以用户为准。
3. **失败不改题**：空命中、`permanent` 错 repo、脚本文件不存在、`ask_user` 被拦，下一动作是换同题手段或终答「没查到」，不是手册第 0 步 intake、不是能力清单、不是另开一类排查。
4. **停手仍可能纠偏**：模型不再调工具时，若本轮轨迹命中改题规则，注入一次「用已有证据直接回答 Q」再给一轮；第二次无工具则结束（与证据闸 Soft nudge 次数上限同构）。
5. **泛化**：规则用 Q、token 重叠、**追问延续**（本会话已有助手回复且本轮 Q 不是在做 intake）、工具失败形态。禁止业务字段名 / 技能名 / 表名黑名单。`bf26` 只是验收题：上一轮正文里的 `4_a8uva8m5tpsl` 当作普通取值钉住，不靠「这是 flow_id」。

### 非目标（一期不做）

- 拦住「同族内选错符号但仍在答 Q」（该 grep `startGame` 却 grep 了存档）——那是答偏，不是改题
- 每步 LLM 裁判「是否还在答原题」
- 拆子 Agent / 父执行器隔离（行业有效，但是二期）
- 强制所有对话走 MEA Auditor
- 改 Web 排版、鉴权、MaxSteps=80 本身
- 保证 vm-manager 索引一定能查到 `startGame`（保证的是没查到也要回答原两问）

### 诚实上限

- 提示词无法单独挡住长 SOP 的上下文传染（8555、goal-drift 文献均已证）。任务锁必须有闸，不能只改「请优先遵循」的措辞。
- 改题检测是启发式，会有漏检和误检。误检通过「仅注入 1 次」和「Q 本身就是在请用户提供信息则放行」限制爆炸半径。
- 不透明取值会漏抽（纯中文专有名词、无数字的短码）。漏抽时锁仍钉 Q；追问延续（已有上轮助手回复）仍能拦住「请提供 … 才能开始」。
- 高置信全文注入仍可能把模型拉进手册；一期用「用户原文含技能名」作为高置信主条件。

---

## 2. 现网缺口（为何会漂）

```text
用户追问 Q
  → RouteBest 过线（标签 es ⊂ access，分恰好 5）
  → 整篇 SKILL.md +「高度相关，请**优先遵循**此工作流」写入 system（skill_router.go:84，自动匹配单档，MaxBodyRunes=12000）
  → 底座 skills_handler.go:59：「若已有【已自动匹配 Skill】正文，**直接按该工作流执行**；否则 load_skill 并**严格遵循**」；:72 再补「高度相关时 load_skill」（目录档，仅 name+desc）
  → 前几步仍跟 Q（历史里有 flow_id）
  → 假设失败（vm-manager / access-service 0 击）
  → 最近上下文 = 手册第 0 步 → 改问 Q′
  → 模型不再 tool_call → RunCompleted
```

| 机制 | 现网保证 | 本事故缺口 |
|------|----------|------------|
| 结束条件 | 无 tool_calls 即终答 | 不检查是否覆盖 Q |
| Skill 路由 | 过阈值即全文 + 优先遵循 | 匹配结果变成任务合同 |
| `TurnIntentGate` topic_drift | web/knowledge/memory_search 参数与用户无重叠则丢 | `skill_view` / `ask_user` / `execute_read` 不在名单 |
| `ask_user` 守卫 | 像在要「已有工具能提供的能力」则拦 | 要会话里已有的 flow_id 被判成「去查 Mongo」 |
| 证据闸 | 缺 ES/Jaeger 可 Soft inject | 已有 ES 证据时允许能力清单终答 |
| `forcedFinalSummaryPrompt` | 写了 original question | **仅 MaxSteps 耗尽** 才走 |

---

## 3. 架构与数据流

```text
SendMessage(user)
  → IntentResolver / Registry（8-09/8-20 不变）
  → ListMessages
  → TurnTaskLock(Q=本轮原文, 近期消息) → KnownValues（可空，不透明取值）
  → skill_router：Route 返回第一/第二名分数（分差不把 ok 打成 false）
       高 → 可注入 SKILL 正文 + 不得改题条款
       中 → 只注入 name+description
       低 → 不注入
  → 拼完整 effectivePrompt（catalog + skill_router + 意图/代码/web/ask_user/数据源/企微……）
  → **末尾**追加【本轮任务锁】（与 skills 族是否激活无关）
  → lock 放入 Request / TurnIntentGate 后 Run
  → ReAct
       有 tool_calls：现网 TurnIntentGate
            + skill_* 与 Q 无重叠则丢（记入 dropped proposals）
            + ask_user 在做 intake（请提供/请给出）且（KnownValues 非空 **或** 本会话已有助手回复）且 Q 本身不是 intake → Retry「用上下文已有信息答 Q」
       !Used：looksLikeGoalDrift 且未 nudge → Retry「仍答 Q」一次
            否则 → 证据闸 → RunCompleted
```

### 3.1 组件落点（一期四个单元）

| 单元 | 位置 | 职责 | 依赖 |
|------|------|------|------|
| `TurnTaskLock` | `portal/internal/chat/task_lock.go`（抽取可单测；framework 不强制依赖 portal 会话库） | 钉 Q；从近期正文抽出不透明取值 | 只依赖字符串/正则 |
| `GoalDriftGate` | `portal/internal/chat/turn_intent_gate.go` + ReAct idle 分支 | skill 漂移丢弃；intake 型 ask_user；停手改题注入 | TaskLock 经 Request 传入 |
| `SkillRouteAuthority` | `portal/internal/chat/skill_router.go` + `framework/templates/skills_handler.go` | 三档注入；删除「优先遵循 / 直接按工作流执行」 | `skills.RouteBest` |
| `RouteScoring` | `framework/skills/route.go` | 分词与标签计分卫生；暴露 runner-up 分数 | 无 |

**基线校准（已按 E 盘实际代码核实，勘误前一版基于错副本的判断）**：

- `PostModelRetry` **已存在并已实现**（`framework/agent/post_model_policy.go:21` 定义；`:80-92` 处理：返回 `(clearedStep, prompt)`，prompt 非空即注入）。`applyPostModelPolicy` **已是双返回值** `(model.ToolStep, string)`（`:52-59`）。ReAct 三处调用点（`react_agent.go:386/714/843`）**已实现** `if retryPrompt != "" { 注入 assistant+user 消息; continue }`。**故本方案不新增 `PostModelRetry`，也不改这三处调用点。**
- skills **已是独立族**（`portal/internal/chat/tool_families.go:19` `FamilySkills`；`skill_view`/`load_skill` 归 skills 族；`:106` 有 skills 关键词）。`family_dropped_all` **已走 `PostModelRetry`**（`turn_intent_gate.go:98-105`，Prompt=`familyRetryPrompt`）。

**接缝结论（本轮设计，锁定）——只改一处，不动 topic_drift_all**：

1. **不改 `topic_drift_all → Finish`**（`turn_intent_gate.go:141-146`），不改 `family_dropped_all`。因 skills 已独立成族，`bf26` 的错 `skill_view` 在 RCA 主族轮次会先被 `family_dropped_all` 丢光 → 现网已 `PostModelRetry` 转下一步，**根本走不到 `topic_drift_all`**。前一版担心的「skill_* 被迫 Finish」接缝不存在。8-20 收口（web/knowledge/memory 全漂 → Finish）保持不动。
2. **唯一真障碍 = idle 走不到 Evaluate**：`applyPostModelPolicy`（`post_model_policy.go:60`）在 `!stepInfo.Used` 提前 return，`Evaluate` 不被调用；纯 idle 文本步进不了 gate。
3. **专职 idle 接口（不误伤其它 policy）**：新增可选接口
   ```go
   type IdlePostModelPolicy interface {
       EvaluateIdle(ctx context.Context, in PostModelPolicyInput) PostModelPolicyResult
   }
   ```
   `applyPostModelPolicy` 在 `!stepInfo.Used` 时：policy 若实现 `IdlePostModelPolicy` 则调 `EvaluateIdle`，否则维持现有 `return stepInfo, ""`。**有工具路径与其它 `PostModelPolicy` 零改动**；现网证据闸/CodeClaim 等不受 idle 影响。
4. **gate 的 idle 分支**：`TurnIntentGate` 实现 `EvaluateIdle`，跑 `looksLikeGoalDrift(lock, assistantText, trace)`。命中且 `trace.GoalDriftNudges==0` 且有步数 → 返回 `PostModelRetry` + 改题 Prompt（复用 `forcedFinalSummaryPrompt` 约束 + 显式附 TaskLock 的 Q 与 KnownValues）；注入后 `GoalDriftNudges++`。第二次 idle 无条件结束。
5. **两个计数器互不相干**：`family_dropped_all` 丢光回压**保持现网无上限**（每次丢光都注入，靠 MaxSteps 兵底，8-20 现状不动）；idle 改题回压用**新增 `GoalDriftNudges`**（`==0` 上限，单轮至多一次，同构 `EvidenceNudges`）。二者不共享，`bf26` 停手那次能被拉回。

伪代码（idle 分支）：

```go
// applyPostModelPolicy，!stepInfo.Used 时
if idle, ok := a.config.PostModelPolicy.(IdlePostModelPolicy); ok {
    res := idle.EvaluateIdle(ctx, in) // in.ToolStep.Used == false
    if res.Decision == PostModelRetry {
        return clearToolStep(stepInfo), res.Prompt // 复用现网注入路径
    }
}
return stepInfo, "" // 维持现状 → 交给证据闸
```

**lock 传递（锁定）**：对象放进 `agent.Request.Metadata`（`agent.go:18`，`map[string]any`）。Gate 从 `in.Req` 读取。`TurnIntentGate` 在 `ListMessages` 之前就随 `BuildReActAgent` 建好，故 lock 不能当它的构造入参，只能经 `Request` 每轮传入。

---

## 4. 单元设计

### 4.1 TurnTaskLock

**不变量只有 Q。** `flow_id` / `uid` / `gid` 是某个部署里的字段名，不能写进平台。其它任务（查日志关键字、问代码「会发生什么」、追问上一句结论）往往根本没有这类 ID；若锁依赖它们，那些轮次会整段退化，也说明规则不够泛化。

**输入**：本轮 `userText`；本会话最近消息（建议上限：最近 8 条 user/assistant **正文**，不含工具 JSON 全量）。

**输出**：

```text
【本轮任务锁】
用户问题（不可改写）：<Q 原文>
上下文已出现的取值（禁止再向用户索取同等信息）：<value1>；<value2>；…
规则：Skill 与工具只是手段。查询 0 击时用现有证据直接回答上述用户问题，包括明确「未查到」。不要把本轮问题换成另一套排查的 intake。
```

取值列表可空。空时锁仍然完整（只钉 Q）。

**KnownValues 抽取（形态规则，不写字段名）**：

| 形态 | 规则 |
|------|------|
| 键值 | `\b[\w.-]{2,24}\s*[:=]\s*(\S{3,})` 的**右侧**（任意 key，不解释 key 含义） |
| 引用 | 反引号、直引号中长度 ≥ 4 的 token |
| 标识形 | 同时含字母与数字、或含 `_`/`-` 且总长 ≥ 6 的拉丁/数字串（如 `4_a8uva8m5tpsl`、`ABC-1234`） |

不抽纯中文词、不抽长度 &lt; 6 且无数字的英文（避免把 `access` 当案值）。本轮 Q 里出现的取值优先于历史。去重、上限约 16 个。

**追问延续（比取值更泛化）**：`HasPriorAssistant` = 本会话在本轮之前已有一条助手正文。用户没有在 Q 里做 intake（Q 不含「请提供 / 请给出 / 麻烦提供」）时，本轮视为对已有上下文的追问，而不是新开 case。这覆盖「没有流水号的任务」：上一句刚分析完一段日志，这句问「那错误码呢」。

**钉入位置（锁定）**：在 `portal/internal/service/chat.go` 两处拼 system 的路径上（同步 `SendMessage`：`OnSurface`:458 → `AppendAskUserToolPrompt`:464 → `appendWecomBoundSystemPrompt`:468；流式 `SendMessageStream`：`OnSurface`:826 → ask_user:832 → wecom:836），**所有** `Append*Prompt` / 企微绑定说明（`appendWecomBoundSystemPrompt`）完成之后（即紧接 :468 / :836 之后），再 `effectivePrompt += TaskLock.Format()`。不要写进 `BuildEffectiveSystemPromptForTurnOnSurface`（`skill_router.go:50`；8-20 下 skills 族未在本轮 surface 时该函数直接 `return userPrompt`，RCA 主路径会整段没有锁）。第三条路径 `agent.go` 用最外层 `BuildEffectiveSystemPromptForTurn`（`skill_router.go:39`，active 恒 nil、无 surface 门控），处理见 §8。Gate 所需的 lock 对象在 `ListMessages` 之后构造，经 `agent.Request.Metadata`（`agent.go:18`）传入 `Run`，不要在更早的 `BuildReActAgent` 里假定历史已在。

### 4.2 Skill 权威与渐进披露

**删除或改写**（照实际代码文本，非臆造串）：

- `skill_router.go:84`（自动匹配单档全文注入的横幅）：「本轮用户问题与该技能高度相关。以下内容来自 SKILL.md，请**优先遵循**此工作流」
- `framework/templates/skills_handler.go:59`（目录档，name+desc）：「当你判断与某个 Skill 高度相关时：若 system 中已有【已自动匹配 Skill】正文，**直接按该工作流执行，不要再 load_skill**；否则调用一次 `load_skill(name)` 获取完整内容并**严格遵循**。」——**这句是 skills_handler 侧真正的权威转移串**（把「已自动匹配」直接升格为「按工作流执行」），须一并改写。
- `skills_handler.go:72`：「使用建议：任务与已知 Skill **高度相关**且尚未出现在上下文时，调用一次 load_skill(name)，再依据工作流调用其它工具…」（较软，改为「可选手册」语气）

（勘误：前一版曾断言「仓库中不存在『已自动匹配则直接按该工作流执行』这类串」——**错**。该串确在 `skills_handler.go:59`。故 skill_router:84（全文横幅「优先遵循」）+ skills_handler:59（「直接按该工作流执行」）是两处必改的权威转移措辞；skills_handler:72 为辅。skill_router:84 是唯一注入 SKILL 正文全文处；skills_handler 只在目录里放 name+desc。）

**改为**：

- 目录语义：Skills 是可选手册；与用户问题冲突时以用户问题为准。
- 已列出 name+description 时，需要细节再 `load_skill` / `skill_view` 一次。
- 高置信全文注入时的固定条款：「此手册不得替换【本轮任务锁】中的用户问题；会话已有标识禁止再问。」**位置对抗**：该条款须放在 SKILL 正文**之后**（正文尾部），不要放正文之前——长 SOP 上下文传染下，正文在后更易盖过前置约束（§5 诚实上限）。

**分档**：

| 档 | 条件 | 注入 |
|----|------|------|
| 高 | `strings.Contains(lower(Q), skillName)` 或 `Contains(spacedName)`，**或**（`score >= min` **且** `score - second >= MinMargin(默认 2)` **且** 计分未使用长度 &lt; 3 的标签子串） | 可注入正文（仍截断 `MaxBodyRunes`）+ 不得改题条款 |
| 中 | `RouteBest` 仍命中（`score >= min`）但不满足高档 | 只注入 `name` + `description` 一行 |
| 低 | `RouteBest` 未过 `min` | 不注入该 Skill |

`bf26` 在 L1 修好后应为低（`es` 不计分）；即使打分未合入，横幅也不再「优先遵循」，高档条件不满足故最多中档一行 description。

### 4.3 RouteScoring（附件，一期仍做）

改 `framework/skills/tokenize` / `scoreSkill`：

1. 拉丁/数字与汉字分开切；`access-service` → `access`, `service`。
2. 标签只整 token 命中；`len(tag) < 3` 不计分（废掉 `es` ⊂ `access`）。**缺口锚点**：现网 `scoreSkill`（`framework/skills/route.go:92-100`）的 tag 分支用 `strings.Contains(q, tag) || tagContainedInTokens(...)`；后半 `tagContainedInTokens`（route.go:124）已守卫 `len<3`，但前半 `strings.Contains(q, tag)` **无长度守卫**——短 tag 作为裸子串命中即 +5，正是本缺口。
3. `strings.Contains(q, tag)` 对短标签禁止；长标签仍可用 contains，但 tag 长度 ≥ 4。
4. `Route` / `RouteBest` **不**因分差把命中打成 `ok=false`。API 需能读到第一名与第二名分数。**现状**：`RouteBest`（route.go:55）以 `MaxResults=1`（:56）调 `Route`，只返回单个最佳、**不含 runner-up**；须改为返回 ≥2 条，或 `RouteBest` 附带 `RunnerUpScore`。`MinMargin`（默认 2）**只**作为 router 高档全文条件：`score-second < MinMargin` 时降为中档一行，而不是「未匹配」。默认 `MinScore` 保持 5（`route.go:22` `defaultRouteMinScore = 5`）。

正例：`请按 rca-sync-archive-migrate 排查` 或原文含「实时存档迁移 / SyncDispatch」且 description/tags 整 token 命中 → 仍可高/中档。

### 4.4 GoalDriftGate

#### A. 有工具时（扩展现网 drift 名单）

`driftSensitiveTools`（现网 `turn_intent_gate.go:26-33` 仅含 web_search/web_extract/knowledge_search/knowledge_read/memory_search/session_search）增加：`skill_view`、`load_skill`、`execute_skill_script`。

重叠：用参数 `name`（及 path）与 **Q 的 token**（用修好后的分词）比较。无重叠则丢。用户写了技能名或手册主题词则放行。

`ask_user`：**守卫层归属**——现网拦截在 framework `AskUserGuardConfig`（把「像查库」判成 `execute_read`）。本方案的判断在 `TurnIntentGate`（能读 lock）。

**intake 型 ask_user**（不认字段名）：`prompt`/`field` 匹配「请提供 / 请给出 / 麻烦提供 / 至少一项 / 必填」等索取句式，且下列之一成立，则丢掉该 call；若本步只剩它 → `PostModelRetry`「用任务锁中的用户问题与上下文已有取值继续作答，不要重新收集立案信息」：

- `KnownValues` 非空（上下文里已有案值，包括 Q 里自带的 `ABC-123`）；或
- `HasPriorAssistant` 且 Q 本身不是 intake（追问延续）

首轮用户只说「帮我查一个单」且无取值、无上轮助手 → 允许 `ask_user`。不要再误导向 `execute_read`。

本期不把 `execute_read` 塞进 drift 名单（8-20 已按族收窄）。切断链靠：拦住 **intake 型 ask_user**（追问或已有取值时），使模型不必去 Mongo 抽样交差。

#### B. 无工具时（改题 → 注入一次）

`looksLikeGoalDrift` 为真当且仅当（可单测的启发式，全部基于锁与轨迹，不调模型）。**主判据 B1/B2 是行为特征（稳）；B3 是文风匹配（脆，与黑名单同构），仅作辅助信号——不得单独 B3 命中就开火，须与 B1 或 B2 之一同时成立**：

1. （主）本轮**提议过**（含被 Gate 丢掉、未执行的）`skill_view`/`load_skill`/`execute_skill_script`，且其 `name` 与 Q 无 token 重叠（因此 A 丢掉错 Skill 后，B1 仍能在停手时开火；需在 trace 记录 dropped proposals）；或
2. （主）本轮 `ask_user` 被判为 intake 型且（KnownValues 非空或追问延续），含工具层 `ask_user blocked` 或 Gate 丢掉；或
3. （辅，须叠加 B1 或 B2）终答同时含「已匹配 / 已自动匹配」与「请提供 / 请给出」，且（KnownValues 非空或 HasPriorAssistant），且 Q 本身不是 intake。

命中且 `GoalDriftNudges==0` 且还有步数：`PostModelRetry`，Prompt 复用 `forcedFinalSummaryPrompt`（定义在 `react_agent.go:945`）的约束，并显式附上 TaskLock 的 Q 与 KnownValues。注入后 `GoalDriftNudges++`。**现状**：`RunTrace` 现有 `EvidenceNudges` 与 `CodeClaimNudges`，须新增 `GoalDriftNudges`。

第二次 `!Used` 无论是否仍像能力清单都结束（避免死循环）。允许不完美终答，但 `bf26` 那种「第一次停手就是改题」会被拉回来答一次 Q。

---

## 5. 错误处理与开关

| 情况 | 行为 |
|------|------|
| TaskLock 抽取失败 | 只钉 Q，KnownValues 空；不阻塞发送 |
| 无 PostModelPolicy | 行为与现网一致（测试/关闸） |
| `SATH_TURN_INTENT_GATE=0` | 现网：整闸关闭。一期 GoalDrift 挂在同一闸上，关则一起关。若需独立开关，二期再加 `SATH_GOAL_DRIFT_GATE` |
| MaxSteps 耗尽 | 现网 `forceFinalSummary` 已要求 original question；追加把 TaskLock.Q 写入该 prompt |
| 证据闸与改题闸同时 inject | 同一 `!Used` 只注入一条。优先改题闸（先回到 Q），避免先补证据再漂 |

---

## 6. 测试与验收

### 6.1 必须用 `bf26` 原句回归（单测，不绑现网 ES）

用户本轮：

> 需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功

上文助手已含 `4_a8uva8m5tpsl`、`uid=104551174`、`ugid=796`。

| # | 检查 | 通过条件 |
|---|------|----------|
| T1 | 打分 | `RouteBest` 不得因 tag `es` 把 `rca-sync-archive-migrate` 送到高档全文注入 |
| T2 | 权威 | 注入文本不得含「请优先遵循此工作流」；含任务锁 Q 原文 |
| T3 | 取值 | TaskLock.KnownValues 含 `4_a8uva8m5tpsl`、`104551174`、`796`（当普通 token，不要求标成 flow_id/uid） |
| T4 | skill 漂移 | `skill_view(name=rca-sync-archive-migrate)` 相对该 Q 应被丢掉 |
| T5 | ask_user | 「请提供 flow_id」在追问 + 已有取值时 → Retry 用上下文作答，不导向 Mongo；**不**依赖把字段识别为 flow_id |
| T6 | 停手 | 终答含「已匹配的技能」+「请提供」且 B1 或 B2 成立 → 第一次不得 RunCompleted |
| T7 | 正例技能 | Q=`请按 rca-sync-archive-migrate 查这条流水的存档迁移` → 允许匹配该技能；`skill_view` 放行 |
| T8 | 无 ID 任务 | 上轮助手已回复「超时由网关重试导致」；本轮 Q=`那错误码是什么`（无 flow/uid）→ 锁仍钉 Q；intake 型 ask_user 因 HasPriorAssistant 被拦 |
| T9 | 首轮合法澄清 | 新会话、Q=`帮我查一个单`、无取值 → 允许 ask_user 要单号 |

### 6.2 其它单测

- 分词：`access-service` 与中文邻接时得到 `access`、`service`，而不是超长中英粘连 token
- `MinMargin`：两技能同分或分差 1 → **中档一行**，`RouteBest` 仍 ok
- `applyPostModelPolicy`：`Used==false` + Retry 会 continue 而不是当终答
- 改题 nudge 上限 1：第二次 idle 结束

### 6.3 不做

- 一期不要求 live ES 上再跑一条 `bf26` 才能合入（单测锁契约；有环境再手测）
- 不把「终答必须出现 startGame 成功时间戳」写成机检（日志可能确实没有）

---

## 7. 分期

| 期 | 内容 | 合入后 `bf26` 应消失的现象 |
|----|------|---------------------------|
| P0（本期） | TaskLock 钉入；权威措辞；RouteScoring；skill/ask_user 闸；idle 改题 1 次注入；policy 对 `!Used` 可 Retry | 不再全文强制存档手册；不再问已有 flow_id；第一次改题停手会被拉回 Q |
| P1 | 高置信以外永不注入 SKILL 正文（单独过线且 Q 不含技能名 → 中档一行）；`AnswerOriginalQuestionPrompt` 供 `forcedFinalSummary` 与 idle 改题共用 | 中档误匹配也没有长 SOP 可传染 |
| P2 | 子 Agent 跑手册、父 Agent 只收证据答 Q；可选 MEA acceptance「覆盖用户原句」 | 长 SOP 与父目标物理隔离 |

一期范围 = **P0**。P1/P2 不进同一 implementation plan。

---

## 8. 文件地图（规划用）

| 文件 | 变更 |
|------|------|
| `framework/skills/route.go` + `route_test.go` | 分词、短标签、runner-up 分数（MinMargin 只在 router 分档用） |
| `framework/agent/post_model_policy.go` + `react_agent.go` + tests | `!Used` 也可 Retry |
| `framework/templates/skills_handler.go` + tests | 权威措辞 |
| `portal/internal/chat/task_lock.go` + `_test.go` | **新建** 锁抽取与钉入文本 |
| `portal/internal/chat/skill_router.go` + tests | 分档注入（高/中/低）；**不**在此钉 TaskLock |
| `portal/internal/service/chat.go` | 完整 effectivePrompt 末尾追加 TaskLock；`Request` 带 lock |
| `portal/internal/service/agent.go` | **第三条拼 prompt 路径**（`effectivePrompt` 在 agent.go:362-373 拼装，:366 用非 scoped `BuildEffectiveSystemPromptForTurn`、**不调** `AppendAskUserToolPrompt`、无 sessionID）：一期同样在 :373（wecom 之后）末尾钉锁。`agentReq.Metadata` 此路径**已由** `RequestMetadataFromContext`（:391-397）建好，lock 直接塞入即可。因请求是 system+user 两三条的新切片、**无 `ListMessages`**，KnownValues 常空；此路径**只钉 Q**，靠追问延续以外的闸（本路径往往是新会话）。 |
| `portal/internal/chat/turn_intent_gate.go` + tests | drift 名单、intake 型 ask_user、idle 改题；记录 dropped skill 提议 |
| `framework/agent` RunTrace | `GoalDriftNudges` 计数 |

不改 Web、Gateway、MEA 入口。

---

## 9. 评审关注点（给评审人）

1. 「无工具也走 PostModelPolicy」是否比新建 FinalAnswerPolicy 更干净，有无隐藏调用点只在 Used 时假设政策。
2. 无 ID 的追问（T8）是否仍被拦住 intake。靠 **HasPriorAssistant**，不靠抽到流水号。新会话首轮要单号（T9）必须放行。
3. P0 是否足够宣称「权威已纠正」。**评审结论：不宣称**。`bf26` 能过是因为 L1 打分修复让它降到低档；一次改题 nudge 对「高置信全文注入」最可能失效。P0 只做到「措辞不再优先遵循 + 位置对抗 + 打分降档」；真正的权威纠正靠 P1「永不灌全文」。
4. `SATH_TURN_INTENT_GATE=0` 时 GoalDrift 一起关掉是否可接受。
