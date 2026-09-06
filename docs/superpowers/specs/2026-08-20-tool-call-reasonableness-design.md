# 工具调用合理性（Tool Call Reasonableness）

**日期**: 2026-08-20  
**状态**: P0 已实现（待确认合入）  
**动机**: 会话 8555（问 union 注册在「区域已有用户」时会发生什么）在已读到 `Register` 的同时，执行了 `skill_view`（VM 调度手册）、存档向 `rca_grep`、`memory_recall`、Mongo `list_tables`/`execute_read`，随后因错 `repo` 与 CFG 把 `int`/`int64` 当受控调用而空转。现网工具面能挡住「问 GitLab 却调 Jaeger」，挡不住永远挂在 `core` 上的数据源 / 技能 / 记忆。  
**对照**:
- 现网设计：[每轮工具面收窄](./2026-08-09-turn-tool-surface-design.md)（已落地，族目录过粗）
- 源码机检：[code-intel cursor-parity](./2026-08-20-code-intel-cursor-parity-design.md)（管终答，不管选工具）
- 对照会话：`8555e9df-ad58-4c80-a22d-cbcc592be7db`

**一句话**：合理性靠「本轮只看见该看见的族 + 执行前丢掉未激活族」，不靠提示词，不靠表名/skill 名黑名单。精度（错 repo、生成代码、永久错误重试）另层补，避免机检改题诱发后半段乱调。

---

## 0. 决策摘要

| 项 | 选择 |
|---|---|
| 根因 | `core` 是杂物间（数据源/技能/记忆永远在工具面）；代码意图词对不上「会发生什么」；执行前闸名单不含这些工具；同一步允许跨族并行 |
| 主路径 | **补完 8-09 工具面**：拆族 + Agent 主族 Fail-narrow + 同一步跨族丢掉 + 滤空则注入再试 |
| 不另起炉灶 | 不新做意图微服务、不用第二 LLM 当每步裁判、不拆「一 Agent 绑多域」的产品模型 |
| 泛化约束 | 按**工具族**和**用户有没有激活该族**过滤；禁止业务词/表名/路径段 `branch/` 黑名单 |
| Cursor 可搬 | 专职工具面（看不见就调不了）、同族才并行、schema 写清何时用；搬不走「默认不绑 Mongo」——Sixath 必须用族收窄补 |

---

## 1. 现网实际保证了什么

每轮 `SendMessage`：

```text
用户原文
  → IntentResolver（规则关键词 → 可选分类器 → Fail-narrow）
  → ActiveFamilies ∪ core
  → BuildRegistry 只注册这些族
  → 模型在可见工具里选择（绑了 rca 则并行开启）
  → TurnIntentGate（族过滤 AND 6 个 drift 工具的 token 重叠）
  → Execute
```

| 机制 | 保证 | 不保证 |
|---|---|---|
| 工具面收窄 | 问 GitLab 时 registry 无 `jaeger_trace` | `list_tables` / `skill_view` / `memory_recall` 属 Core，收窄也在 |
| `TurnIntentGate` 族过滤 | 漏网跨族 call 丢掉 | Core 内调用全部放行 |
| drift-sensitive 词重叠 | 网页/知识库/memory_search/session_search 参数与用户无公共 token 则丢 | 不含 skill_view、memory_recall、execute_read、全部 rca_*；表名带 `union` 会假放行 |
| 终答线索 + tool_calls | 丢掉工具直接结束 | 不管选错工具 |
| 提示（code / 数据源 / Skills 摘要） | 软引导 | 8555 仍 `skill_view` + 查集合 |
| 工具护栏 | 同参失败次数、连续只调工具 | `ok:false` JSON 不算 Go error，错 repo 可连打 |
| quote / CFG / 入边闸 | 终答与原文/路径表一致 | 会改题，诱发后半段为交差再调工具 |

**结论**：现网合理性 = 跨 MCP/RCA 别糊成一团。同族选错、以及 Core 杂物，没有闸。

---

## 2. 目标与非目标

### 目标

1. **合理性**：用户未激活的族，本轮既不出现在 registry，漏网 `tool_call` 也不得执行。
2. **多域 Agent 仍合法**：同一 Agent 可绑代码 + Mongo + Skill；只有用户点到（或本轮主族就是）才暴露。
3. **低置信不丢主业**：源码分析 Agent 在「会发生什么」这类无关键词问题上，仍能看见 `rca_*`，而不是只剩 Mongo。
4. **滤空不当终答**：全部 tool_call 被丢掉时注入「用本轮主族工具」再走一轮。
5. **泛化**：规则对任意绑了多族的 Agent 成立；8555 只是验收题。

### 非目标

- 用规则拦住「同是 rca 但搜错符号」（只 grep 存档不 grep `Register`）——那是模型与 `rca_symbol`，硬规则会误伤 `1105` / `ErrAlreadyExist`
- 按表名、skill 名、`union`、`branch/` 写黑名单
- 用「grep 参数必须和用户词重叠」滤 `rca_grep`
- 每步再调一个 LLM 裁判
- 本期改 Web 排版、鉴权、子 Agent 拆分
- 本期必改 Agent 表单（主族 P0 从已绑定工具推断）

### 诚实上限

- 工具面 + 闸能去掉**跨族**诱惑；同族内选错入口仍会发生。
- 提示词无法替代闸（8555 已证）。
- Cursor 的合理性大量来自「默认工具面只有代码工具」；Sixath 选择一 Agent 多域，就必须把族拆对。

---

## 3. 四层架构（补完，不替换 8-09）

```text
用户问题
  L0 意图 → 本轮工具面（模型只能看见这些族）
  L1 同一步只保留已激活族（跨族并行丢掉未激活兄弟）
  L2 执行前闸（漏网跨族丢掉；滤空 → 注入再试，不是 Finish）
  L3 精度（永久失败不重试；repo 唯一落点；CFG 预声明转换不改题）
```

L0–L2 是合理性；L3 是精度，但 8555 后半段「无关工具」有一半是机检改题诱发的，故写入同一方案、实现可分期。

---

## 4. L0 族目录（相对 8-09 的变更）

### 4.1 拆开 `core`

8-09 规定 `core` 永远 Active。一期把数据源/技能/记忆放进 Core，导致收窄失效。

| Family ID | 成员 | 何时进入 Active |
|---|---|---|
| `core`（瘦） | todo、ask_user、list_tools、以及现有「会话内必开」且无域的工具 | 永远 |
| `code` | `rca_grep` `rca_read` `rca_glob` `rca_symbol` | 规则 / 分类器 / **主族** / 本步已出现 rca_* |
| `data` | `list_tables` `describe_table` `execute_read`（`execute_write` 同族） | 用户激活数据意图 |
| `skills` | `skill_view` `load_skill` `skills_list` `read_skill_file` `execute_skill_script` | 点名 skill，或「按手册/技能」 |
| `memory` | `memory_recall` `memory_search` `memory_get` `memory_remember` | 「上次 / 记忆 / 我们讨论过」 |
| `rca` | `jaeger_trace` `es_log_query` | 保持 8-09 |
| `web` `knowledge` `mcp:<id>` | 保持 8-09 | 保持 8-09 |

`FamilyForBuiltinToolName`：上表工具不得再落到 `FamilyCore`。  
`BoundFamiliesFrom`：`ToolTypeDatasource` 计入 `data`，不再当 core。

### 4.2 用户激活口子（规则层关键词）

只认**用户原文**，不认工具参数（避免 `t_union_user_game_area_storage_info` 里的 `union` 假激活 `data`）。

| 族 | 关键词（可再补语言，实现用同一 tokenize + contains） |
|---|---|
| `data` | 查库、查表、集合、mongo、mysql、sql、实际数据、有哪些记录、线上数据、这条数据 |
| `skills` | skill、技能、手册、按技能；或用户文本包含某个已安装 skill **全名** |
| `memory` | 上次、记忆、我们讨论过、之前说过、session 里 |
| `code` | 保持现有；**不**为咪咕加「注册/union/会发生什么」 |
| `rca` `web` `knowledge` | 保持 8-09 |

显式多意图并集：用户同时说「根据代码和 mongo 看」→ `code ∪ data ∪ core`。

### 4.3 主族（Fail-narrow 修正）

8-09：分类失败且 Candidates 空 → **仅 core**。源码 Agent 会看不见 rca、仍看见（旧）Core 里的 Mongo。

**P0 推断，不改 proto：**

```text
primary_families =
  已绑定 code 工具 → {code}
  已绑定 rca 工具 → {rca}     // 可与 code 并存
  仅数据源、无 code/rca → {data}
  否则 → 空
```

Fail-narrow：`Active = (Candidates ∪ primary_families ∪ 瘦 core)`，Candidates 与 primary 都空才仅瘦 core。

仍 **禁止 Fail-open** 成全量绑定。

每轮重新 Resolve；主族不跨轮继承（但每轮从绑定重新推断，结果稳定）。

### 4.4 本步已出现 rca_* 锁 code

即使 L0 因关键词没打开 `code`，若**这一步** `tool_calls` 已含 `rca_*`，L1/L2 视主族含 `code`，丢掉未激活的 `data`/`skills`/`memory`。覆盖「分类器打开了 code+旧 core」和「模型已经在 grep 还顺手查库」。

---

## 5. L1 / L2 执行前闸

挂在现有 `TurnIntentGate`（`PostModelPolicy`）。与工具面 AND：面上没有的调不了；面上漏网的这里再丢。

### 5.1 同一步跨族

1. 确定本步有效族 = `ActiveFamilies` ∪（本步实际提出的族里、已在 Bound 且被激活或属于主族的）。
2. 丢掉：所属族 ∉ 本轮 Active 的 call。
3. **同族多个 call 全留**（并行 `Register` grep + `1105` grep 合法）。

禁止用「兄弟 grep 与用户词重叠」滤 `rca_grep`。

### 5.2 drift 名单

- 保留网页/知识库的 token 重叠（8-09 v0）。
- **不要**把 `execute_read` 加进 overlap 名单（表名污染）。
- `skill_view` / `memory_recall` / `list_tables` 只走族激活，不走 overlap。

`memory_search` 已在 drift 名单；`memory_recall` 改走 `memory` 族，不再依赖 overlap。

### 5.3 滤空 → 注入再试（接口要改）

现网 `PostModelFilter` 且 `ToolCalls` 空 = **Finish**，模型「我去查集合」会变成用户可见终答。

在 `framework/agent/post_model_policy.go` 增加一档：

```text
PostModelRetry
  Decision: Retry
  Reason:   family_dropped_all | ...
  Prompt:   「本轮激活的工具族是 {X}。刚才那些调用不属于这些族，不要执行。请只用 {X} 内工具继续，或直接作答。」
```

`applyPostModelPolicy`：清空本步 tools，追加 assistant 原文 + user/system 注入，`continue` 下一 ReAct 步（与 credential redirect / evidence inject 同一路径）。

`TurnIntentGate.Evaluate` 在「族过滤后长度为 0」时返回 Retry 而不是 Finish。  
「确像终答 + 还带 tool_calls」仍 Finish（v0 行为保留）。

### 5.4 提示配套（软，非验收）

- `AppendCodeAnalysisPrompt`：补一句「未激活时不要 skill_view / list_tables / memory_recall」；**删掉仍不够，闸才是验收。**
- Skills 摘要：代码主族轮次不要写「高度相关就 load_skill」压过 rca（可把该段限制在 `skills` 已激活时注入）。P0 可做：`BuildSkillsAwarePrompt` 仅当 Active 含 `skills` 时追加。

---

## 6. L3 精度（防后半段空转；可晚于 L0–L2 合入）

| 项 | 规则 | 泛化 |
|---|---|---|
| repo 别名 | `repo` 未命中配置根，但 `join(root, file)` 在**唯一** root 存在 → 成功，带 `repo_resolved` / `requested_repo`；多命中仍报错并列出候选 | 单根 monorepo 把目录名当仓名 |
| 永久错误 | `error_code=permanent` 写入 `ToolCallRecord.Error`（或等价）；同工具同参不再 Execute，返回短提示 | 任意 rca 工具 |
| 生成代码 | grep 默认排除与现网一致：vendor、`*_gen.go`、`*.txt`；**增加** `*.pb.go` | 各 Go 仓 |
| 不默认排除 | `**/branch/**` | 咪咕副本目录，可做 Agent 级 exclude，不当全局 |
| CFG / quote | `int` `int8`…`int64` `uint*` `float*` `byte` `rune` `string` `bool` `error` 不进 `control_flow.calls`、不进 quote 受控调用、call_graph 不建 unresolved 节点 | Go 预声明转换 |

「同一文件整篇成功读 ≥3 次再注入」为 P2，阈值易误伤扩窗口。

---

## 7. 明确不做

| 不做 | 原因 |
|---|---|
| 并行 grep「有重叠就丢零重叠兄弟」 | 误杀同步的 `Register` + `1105` |
| 全局忽略 `branch/` | 过拟合咪咕仓布局 |
| 表名 / `user_game_area` 黑名单 | 假泛化 |
| 禁止绑定 Mongo / Skill | 用户明确查库、按手册时必须还能用 |
| 第二 LLM 逐步裁判 | 慢、不稳；L0 已有低置信分类器 |
| 用 MEA/终答机检代替选工具闸 | 机检改题会增加乱调 |

---

## 8. 数据流（目标态）

```text
SendMessage(user)
  → BoundFamilies（含 data/skills/memory/code，不再把数据源算 core）
  → primary = InferPrimary(bound)
  → IntentResolver：规则 → 分类 → Fail-narrow(Candidates ∪ primary ∪ 瘦core)
  → BuildRegistry(Active)
  → 模型 tool_calls
  → TurnIntentGate
       终答线索？ → Finish
       跨族丢掉
       空？ → PostModelRetry 注入
       非空 → Filter 后 Execute
  → 护栏（permanent 同参）
```

---

## 9. 分期与文件

### P0 合理性（建议先做）

| 文件 | 变更 |
|---|---|
| `portal/internal/chat/tool_families.go` | 新族常量；builtin 映射；关键词；`BoundFamiliesFrom` 数据源 → data |
| `portal/internal/chat/intent_resolver.go` | Fail-narrow 并入 primary；InferPrimary |
| `portal/internal/chat/agent_builder.go` | `filterToolsForSurface` 按新族 |
| `portal/internal/chat/turn_intent_gate.go` | 跨族；滤空 Retry；不要 overlap 滤 data/skills |
| `portal/internal/chat/runtime_tools.go` / skills 注入 | Active 不含 skills 时不注入「请 load_skill」长摘要 |
| `framework/agent/post_model_policy.go` + `react_agent.go`（三处循环） | `PostModelRetry` + 注入 continue |
| 测试 | 见 §10 |

不改 proto、不改 UI、不 commit。

### P1 精度

| 文件 | 变更 |
|---|---|
| `framework/tool/rca_repos.go` + `rca_code_tools.go` | 未知 repo 唯一落点 |
| `framework/agent/tool_guardrail.go` / rca 错误写入 | permanent 可见 |
| `framework/tool/rca_grep.go` | 排除 `*.pb.go` |
| `framework/tool/go_cfg.go` + `code_quote_gate.go` | 预声明转换 skip |

### P2

- Agent 可选 `primary_families` 覆盖推断（需 proto/UI 时再开）
- code 关键词补行为类说法（多语言，需单独评测误杀）
- 整文件重复读软提示

---

## 10. 验收

金路径以 **跨族是否执行** 为准，不以终答文采为准。

1. **8555 类问法**（「区域侧有用户信息了，union 在注册的时候会发生什么」）  
   - registry / 执行：**无** `list_tables` `execute_read` `skill_view` `memory_recall`  
   - **有** `rca_grep` / `rca_read`  
   - 同一步可并行多个 rca_grep  

2. **显式查库**（「查 mongo 里有没有这个用户」）  
   - `data` 激活，`list_tables`/`execute_read` 可执行  

3. **GitLab 单意图**（8-09 回归）  
   - 无 `jaeger_trace`  

4. **滤空**  
   - 模型只调 `skill_view` 且 skills 未激活 → 不把该步正文当终答；下一轮能改调 rca 或直接回答  

5. **P1**：`repo=area-servo` + `file=area-servo/handler/handler.go` 在单根 `migu` 下成功；`int(` 不触发 quote 闸改题  

单测优先：`intent_resolver_test.go`、`turn_intent_gate_test.go`、`agent_builder` 过滤、`post_model_policy` Retry。不强制本期 e2e 打真实 8555。

---

## 11. 风险

| 风险 | 缓解 |
|---|---|
| 查库说法未进词表（「帮我看看库里」） | 分类器仍可开 data；漏了则下一轮用户说「查表」即激活；词表可增不可写死业务 |
| 主族推断把纯闲聊 Agent 打开 code | 仅「已绑定 rca_code 工具」才 primary=code |
| Skills 不注入导致真需要手册时模型不知道有 skill | 用户点名或「按手册」激活；列表仍可通过 `skills_list` 仅在族激活后可见 |
| Retry 增加步数 | 比执行错误 Mongo 更便宜；MaxSteps 已有上限 |
| 拆族后旧测试假定 datasource=core | 改测试期望，这是故意破坏 |

---

## 12. 与 Cursor 的对应（实现时不要神化）

| Cursor | Sixath 落地 |
|---|---|
| 默认工具面几乎只有代码工具 | L0：代码轮次看不见 data/skills/memory |
| 同族并行（多次 Grep） | L1：同族全留，跨族丢掉 |
| schema 写何时用/不用 | P0 可加长 rca/data 工具 Description；验收仍靠闸 |
| 无 quote 闸改题 | P1 CFG skip 预声明转换 |
| 没有 Mongo 绑在 Core | 产品仍允许多域绑定 → 必须拆族，不能假装 Cursor |

---

## 13. 开关

保持 8-09：

- `SATH_TURN_TOOL_SURFACE=0`：不收窄，回到全量绑定（本方案拆族在 surface 开启时才有意义）
- `SATH_TURN_INTENT_GATE=0`：不挂 PostModelPolicy（无执行前兜底）

新增（可选，默认开）：`SATH_TOOL_FAMILY_SPLIT=0` 关闭 data/skills/memory 拆分，回退 8-09 Core 杂物间（紧急回滚）。
