# Agent = Model + Workspace + Harness 重启规格

**日期**: 2026-09-05  
**状态**: 已确认（2026-09-05）；**2026-09-05 修订** S1–S3（Hub 管理面退出外壳；Context 迁出 `model`；引入 PromptBuilder 与 `harness`/`workspace` 搬家）  
**范围**: 架构契约与 keep/cut。本文件**不改代码**。P1–P4 减肉见第 11 节；其后增强见第 12 节。  
**一句话**: 旧公式把 Harness 定义成「模型之外的一切」，骨架吞掉血肉、器官和免疫；新公式把三者并列，平台外壳保留，焊进循环的领域闸与 Portal 第二套 Harness 全部移出核心。

**取代（权威冲突时以本文为准）**:

| 旧口径 | 文档 | 本文处理 |
|--------|------|----------|
| Agent = Model + Harness（Harness = 模型之外的一切） | [harness-engineering-gap](./2026-07-11-harness-engineering-gap-design.md) §1.1 | **废止该等式**；控制面 Hook / 预算仍 KEEP |
| 证据门 / 结案短语闸作为脊柱 | 同上 §3.3、§4.3 | CUT：领域闸不得焊进 `ReActConfig` |
| 每轮工具面收窄 + 意图闸 | [turn-tool-surface](./2026-08-09-turn-tool-surface-design.md) | CUT；用「Agent 绑定器官 + workspace」替代 |
| 任务锁 / 改题回拉 | [task-lock-goal-drift](./2026-08-24-task-lock-goal-drift-design.md) | CUT |
| 调查闸默认关、代码保留 | [investigation-gates-off](./2026-09-04-investigation-gates-off-design.md) | **升级为删除**，不是长期开关 |
| MEA 事中审计旁路 ReAct | [mea-minimal-subset](./2026-08-12-mea-minimal-subset-design.md) | 移出核心；不重写成「正确版」 |
| Growth 平行 OS | [growth-system](../../../framework/docs/superpowers/specs/2026-05-10-growth-system-design.md) | 移出 v1 骨架；会话结束 learnings 可列为以后的可选器官 |

**仍有效、被本文升格**:

- [code-root-workspace-mount](./2026-08-13-code-root-workspace-mount-design.md) 的 **默认可写根 + 可选 `code/` 挂载**（**整仓当 workspace 变体退役**）
- [design-agent-runtime-hermes-inspired](../../../framework/docs/design-agent-runtime-hermes-inspired.md) 的 **单一编排核心、canonical 消息、Toolset、取消/预算**（G-O1/G-O2/G-O4/G-O6/G-O7）
- 上下文压缩 L0/L1/L2 管道（目标包 `framework/context`；P1–P4 期间仍在 `framework/model`，由 [S2](./2026-09-05-context-promptbuilder-design.md) 迁出）
- Skills 渐进披露（`load_skill` / `skill_view`，Skill 名是参数不是工具名）

---

## 0. 已锁定决策

| 项 | 选择 |
|----|------|
| 落地顺序 | **先 spec、后代码** |
| 产品外壳 | **保留完整平台**：Web 管理台 + 会话、Portal Agent 配置、Gateway / 企微 |
| Workspace | **每个 Agent 必须有**：平台给默认可写目录；用户可再挂代码根（模式 C） |
| 改造路径 | 契约重写 + 后续外科减肉（不在本阶段抽新 git 仓、不换 Web 技术栈） |

---

## 1. 失败诊断

旧公式（2026-07-11）：**Agent = Model + Harness**，Harness = 上下文、工具、执行环境、验证回压、护栏、可观测、多 Agent、成长闭环。原则「错一次就焊进 harness」在没有脊柱边界时，变成「每错一次加一道短语闸」。

观察到的五层失败：

1. **骨架吞掉血肉、器官和免疫**。[`framework/agent/react_agent.go`](../../../framework/agent/react_agent.go) 的 `ReActConfig` 同时挂压缩、记忆、`EvidenceGate`、`CodeClaimGate`、`PostModelPolicy`。
2. **免疫系统癌变**。领域短语闸焊进主循环（见 §6.1）；Portal 再叠 [`investigation_gates.go`](../../../portal/internal/chat/investigation_gates.go)、task lock、turn tool surface。闸门互伤，只能再加总开关关掉。
3. **Portal 变成第二套 Harness**。[`portal/internal/chat/`](../../../portal/internal/chat/) 在循环外拼 prompt、收窄工具面、接 MEA / Growth / Memory Hub。
4. **并置控制面**。Growth、MEA、Procedural Repair、Memory Hub 明确「并置、不抢权」——没有脊柱，只有平行神经系统。
5. **Workspace 是工具细节，而且是双根**。可写沙箱（`workspace_root` + pathguard）和只读代码仓（`rca_*` code roots）并存。Surrogate / code-claim 闸用短语补「两个根谁算真相」。Skill 已被 framework 渐进披露，Portal `skill_router` 又把 SKILL 全文预注入 system prompt。

**该留的已经有了**：单一 ReAct 循环、`ToolHook` 可 block、[`harness/hooks.yaml`](../../../framework/agent/harness_hooks.go) 落在工作区、L0/L1/L2、Skills 渐进披露、MCP/toolset、模式 C、Confirm cards、Gateway 入站与 Runtime 桥。

---

## 2. 新公式

```text
agent = model + workspace + harness
```

| 角色 | 是什么 | 不是什么 |
|------|--------|----------|
| **Harness 骨架** | 循环、生命周期、预算、工具调度顺序、把 Context 送进 Model | 不是全部非模型代码 |
| **Context 血肉** | 规范化消息、Stable/Ephemeral prompt、L0/L1/L2、从 workspace/记忆取材料 | 不是又一套闸门 OS |
| **Tool / MCP / Skills 器官** | 可插拔能力；读写 workspace | 不是脊柱；禁止 Portal 再复制一套编排 |
| **约束与流程 免疫系统** | Permission、confirm、ToolHook、`workspace/harness/hooks.yaml`、Skill 流程 | 不是焊进 `ReActConfig` 的领域短语闸 |

Context 不进入等式：它是骨架运转时的血肉，由 Harness 从 Workspace + 历史 + 器官结果组装后送给 Model。

```text
Web / Gateway / Portal          平台外壳（装配与分发，不是 Agent）
        │ assemble
        ▼
┌──────────────── Agent ────────────────┐
│  Model     Workspace     Harness      │
│     ▲                      │          │
│     │     Context 血肉     │          │
│     └────────◄─────────────┘          │
│  Organs ◄── Harness 调度              │
│  Organs ──► Workspace 读写            │
│  Immune ──► Harness（Hook/权限/确认） │
│  Workspace ──► Immune（hooks.yaml、Skill）│
└───────────────────────────────────────┘
```

**Portal 的唯一合法角色是装配器**：选出 Model 配置、Workspace 路径、器官注册表、免疫 Hook，然后调用同一个 Harness。禁止在 `portal/internal/chat` 里做第二套循环策略。

**一次 Run（可验收叙述）**：

1. 装配器解析 Agent：model、workspace 根、已绑定器官、hooks。
2. Harness 从 workspace 加载 `harness/hooks.yaml` 与 skills 索引。
3. Harness 组装 Context：从 workspace 加载 skills 索引并渲染为索引文本；PromptBuilder（Stable + Ephemeral）→ Encode 为一条 system → L0/L1/L2 管道（只压 messages）。P1–P4 未引入 Builder；[S2](./2026-09-05-context-promptbuilder-design.md) 引入。
4. 调用 Model（傻 Provider：编码 + 发请求）；按 tool_calls 调度器官；**Hook.Before → PermissionPolicy → Execute → Hook.After**（与现网 `executeOneToolCall` 一致；Before 可改 args，block 发生在鉴权之前）。
5. 器官读写 workspace；结果写回 canonical 消息；循环直到停步或预算耗尽。

---

## 3. 目标包边界（契约现在写死；包搬家可后置）

实施减肉时**先删行为、再考虑是否改包名**。新目录是目标形态，不是本规格的阻塞项。

```text
framework/harness     循环、Hook、预算、生命周期     禁止领域闸、禁止拼业务 prompt
framework/workspace   根路径、守卫、约定目录、挂载     禁止执行模型
framework/context     PromptBuilder + L0/L1/L2       禁止跑模型、禁止领域闸
framework/model       Provider（编码 + 发请求）       禁止压缩管道、禁止 import harness/context
framework/tool        器官注册与 MCP
framework/skills      Skill 索引与加载
framework/memory      仅 Context 材料（buffer + 可选检索）
portal/internal/chat  装配器 + 会话 IO               禁止策略闸、禁止第二循环
```

**依赖方向**：`portal` → `harness` → `context` / `workspace` / `tool`；`context` → `model`（Message）；`model` **不得** import `harness` / `context` / `agent`（现有 TraceSink 回调约定保留）。P1–P4 代码锚点仍是 `framework/agent` 与 `framework/model` 管道；包名由 [S3](./2026-09-05-harness-workspace-rename-design.md) 落地。

当前代码锚点（搬家前）：

| 目标角色 | 现状路径 |
|----------|----------|
| harness | `framework/agent/react_agent.go`、`tool_hook.go`、`harness_hooks.go`、`chat_session_hook.go` |
| workspace | `framework/tool/pathguard.go`、`file_tools.go`；Portal `code_roots.go`、Agent `workspace` 列 |
| context | P1–P4：`framework/model/context_pipeline.go` 及 L0/L1/L2；S2 后：`framework/context` |
| 装配器 | `portal/internal/chat/agent_builder.go` |

---

## 4. Workspace 契约

每个 Agent **必须**有可写 workspace：

| 项 | 规则 |
|----|------|
| 默认 | `{data_root}/agents/{id}/`（创建 Agent 时由平台保证存在） |
| 代码根 | 可选：`{workspace}/code` → symlink 到所选路径。**整仓当 workspace 变体退役**（旧模式 C 的「整仓」不再支持）。只读代码根只挂 `code/`，写路径永远走默认可写根。 |
| 守卫 | 一切文件器官经 `ResolveWorkspacePath`；空 root 非法 |
| 约定目录 | `skills/`、`harness/hooks.yaml`、`MEMORY.md` / `USER.md`（可选简单文件记忆） |

**双根必须收成一个故事**：代码根是 workspace 的挂载（`code/`），不是另一套 `rca_*` 宇宙。去掉「file 工具 vs rca 工具」分流后，`surrogate_source_gate` 失去存在理由。RCA 代码导航若仍需要，作为 **可选器官** 注册，路径仍落在 workspace 守卫下（含 `code/`）。

Turn Tool Surface 的根因是「一个身体装了所有器官，再每轮截肢」。新规则：

- Agent 的器官列表是**配置**，不是每轮猜测。
- 需要跨域时：**不要**在同一循环里 Fail-narrow；配置另一个 Agent（或用户换 Agent）。**P1–P4 不实现子 Agent / 子 Run 运行时。**
- 约束写在 `workspace/harness/hooks.yaml` 和 Skill，不写在终答短语表。

---

## 5. KEEP

### 5.1 骨架

| 能力 | 锚点 |
|------|------|
| 单一 ReAct 循环、`MaxSteps`、串行默认、可选并行写回 | `framework/agent/react_agent.go` |
| `ToolHook`（Before 可 block） | `framework/agent/tool_hook.go` |
| 会话结束钩子 | `framework/agent/chat_session_hook.go` |
| 工作区声明式 hooks | `framework/agent/harness_hooks.go`（`harness/hooks.yaml`） |
| `PermissionPolicy`、confirm / `ask_user` | `framework/agent/trace.go`（`PermissionPolicy`）、`framework/tool` confirm |
| 事件与观测 | `framework/events`、`obs`、middleware recovery/logging/metrics/tracing |
| 工具契约 | Registry、Toolset、`ListForAPI` / CheckFn、MCP |
| 可选进度护栏接口 | `GuardrailEvaluator`（通用 warn/halt，**不含**领域短语表） |

### 5.2 Workspace

- `framework/tool/pathguard.go`、`file_tools.go`、terminal
- 模式 C 挂载：`portal/internal/chat/code_roots.go`、`portal/internal/server/code_roots.go`
- `MEMORY.md` / `USER.md` 作为 workspace 文件记忆（`framework/memory/agent_workspace.go` 的薄路径），不是 Hub

### 5.3 血肉（逻辑保留，从 `ReActConfig` 领域字段里拆出去）

- `framework/model`：L1 sanitize → snip → L2 pre-prune → L0 budget → orphan strip → L2 summarize
- `framework/skills`：Index / loader；`load_skill` / `skill_view` / `read_skill_file` / `execute_skill_script`
- 记忆：**本会话 buffer + 可选检索注入 Context**。向量/图/Neo4j/语义冲突不当作默认操作系统。
- Prompt：P1–P4 只保留 **一条装配路径**（Agent `systemPrompt` + 管道）。Stable/Ephemeral PromptBuilder 由 [S2](./2026-09-05-context-promptbuilder-design.md) 引入，禁止把已裁的 catalog/web/datasource/任务锁叠层焊回。

### 5.4 器官（可插拔，默认不绑全）

- 内置工具、MCP、Skills 脚本
- 数据源 / executor / RCA 代码工具 / 浏览器 / process / vision：可选 toolset，**不是**框架内核
- 产品外壳：对话、Agent CRUD、工具/MCP 管理、渠道、企微 Gateway、会话历史、登录组织、Confirm UI

### 5.5 免疫系统（薄）

调用顺序写死：

```text
Model tool_calls
  → ListForAPI(CheckFn)
  → Hook.Before[]  ──block──→ tool 结果（blocked=true）→ Model
  → PermissionPolicy
  → Execute
  → Hook.After[]
  → 可选 GuardrailEvaluator（通用，无领域短语）
  → EventBus + Trace
```

流程用 Skill + workspace 文件表达，**不用**终答短语回压。

### 5.6 平台外壳（KEEP，不等于骨架）

| 层 | KEEP | 不进入 Harness |
|----|------|----------------|
| Web | 对话、Agent（含 workspace picker）、sessions、tools/MCP、登录/设置、Confirm cards | Insights 随 Growth 降级；Hub Loadout/Binding 随 [S1](./2026-09-05-dead-code-hub-off-design.md) 退出外壳 |
| Gateway | Web SSE/session 代理、Runtime 桥、Webhook、企微长连接（分发层） | 不跑 ReAct |
| Portal | 会话持久化、Auth/ACL/Org、Agent CRUD、workspace 绑定、Runtime API、Tools/MCP/Skills 管理、Channel 投递、SSE/rewind/turn-trace、Confirm 协议 | 调查闸、技能预注入、MEA 旁路、Hub/Growth 主循环；Hub HTTP 管理面随 S1 拆除 |

`agent_builder` **退化后**保留为唯一接线：`model + registry(from bindings) + workspace → ReAct`。

Cron 留作平台功能，不进入 Harness。

---

## 6. CUT

领域逻辑若还要，**只能**以 Skill、`hooks.yaml` 或可选器官复活，禁止焊回循环。

### 6.1 焊进循环的领域闸（`framework/agent/`）

删除或停止装配（含测试）：

| 文件 | 为何 CUT |
|------|----------|
| `inbound_gate.go` | 「整体流程」话术前强制 `rca_symbol` |
| `evidence_gate.go` 及 react 接线 | 终答必须特定证据工具 |
| `code_claim_auditor.go` / code claim 接线 | RCA 源码声明审计 |
| `code_quote_gate.go` | 伪源码启发式 |
| `empty_idle_gate.go` | 空正文强制再答 |
| `empty_hit_speak_gate.go` | 0 击禁止特定话术 |
| `truncated_page_gate.go` | ES 翻页未完禁止总结 |
| `scenario_path_gate.go` | regex 对齐场景 vs control_flow |
| `surrogate_source_gate.go` | 禁止 MEMORY/txt 冒充源码（双根补丁） |

`PostModelPolicy`：**接口可留到 P3**（Portal 调查闸目前经它注入）。RCA / 意图 / HTTP 接地 / 改题等**实现**不进默认装配。P3 拆完调查闸后，若无调用者再删接口。P1 结束时 `ReActConfig` 必须已去掉 `EvidenceGate` / `CodeClaimGate` 字段。

`code_workset.go`：每轮注入 `[code_workset]` system card，属 RCA 皮肉特化。**P1 从 `react_agent.go` 三处 `appendToolResultsWithWorkset` 拆除并删除该文件**（含测试）；工具结果写回改回普通 append。

`plan_agent.go`：第二套循环，当前无默认装配。**P1 不删除**（避免扩大 diff）；P4 若仍无调用者再删。默认路径禁止新增对它的接线。

### 6.2 Portal 第二套 Harness（`portal/internal/chat/`）

| 组 | 文件（非穷尽，减肉时按引用清） |
|----|------------------------------|
| 调查闸 | `investigation_gates.go`、`http_grounding.go`、`turn_intent_gate.go`、`task_lock.go`、`turn_surface.go`、`tool_families.go`、`intent_resolver.go`、`intent_classifier.go` |
| Skill 预注入 | `skill_router.go` 中把 SKILL 全文塞进 system prompt 的路径 |
| MEA 旁路 | `mea_*.go`；`service` 中 `streamWithRulesMEA` |
| Prompt 叠层 | `web_prompt.go`、`catalog_prompt.go`、`code_analysis_prompt.go`、`datasource_prompt.go`、task lock 注入 → 只留 Agent `systemPrompt` + Context 管道，不新建 PromptBuilder 包 |
| Hub / procedural | `hub_*.go`、`procedural_*.go`、复杂 `memory_*` 冲突/围栏（会话 buffer 除外） |

配置：删除 `chat.investigation_gates` 与 `SATH_INVESTIGATION_GATES` / `SATH_TURN_TOOL_SURFACE` / `SATH_TURN_INTENT_GATE` / `SATH_TASK_LOCK` 作为产品开关（代码删除后无需 off）。

### 6.3 平行控制面（移出默认装配，不在本规格内重写）

- `framework/mea/` 整包
- `framework/growth/` 主循环 nudge / fork-agent / curator 作为默认路径
- Memory Hub / Neo4j graph / procedural five-gate（`framework/memory/procedural_commit.go` 等）
- `framework/tool/hypertool.go`
- SQL heal / query spill **作为默认执行自愈**（溢出落盘若仍为器官实现细节，不得成为 Harness 策略）

Web `/agents/:id/insights` 随 Growth 降级（可隐藏路由，不进默认导航）。

### 6.4 复活规则

| 若产品仍要 | 合法落点 |
|------------|----------|
| 「查代码必须先 grep」 | Skill 正文 + 可选 `hooks.yaml` |
| 「写文件要确认」 | 已有 confirm（KEEP） |
| 「这个 Agent 不要 jaeger」 | 不要绑定该器官 |
| 「跨 GitLab 与 RCA」 | 两个 Agent（用户切换或分别配置），不是每轮截肢；P1–P4 不交付子 Run 运行时 |
| 成长沉淀 | 以后的可选器官，挂 `on_chat_session_end`，不进 v1 默认 |

---

## 7. 成功标准（规格可验收；减肉 PR 对照此表）

1. 能用 §2 的五步说清一次 Run，无需提到 investigation / MEA / Growth / Hub。
2. `ReActConfig` 不再出现 RCA / 调查 / MEA 专用字段（无 `EvidenceGate`、`CodeClaimGate`；无领域 `PostModelPolicy` 实现装配进默认路径）。
3. Portal chat 的 system prompt **不含**【本轮任务锁】、调查闸、SKILL 全文预注入。
4. 每个新建 Agent 都有默认可写 workspace 根路径；**空 root 字符串**不可进入 Run（新建后尚未落文件的空目录仍合法）。
5. 平台外壳仍在：登录、Agent 配置、对话 SSE、Gateway/企微、工具与 MCP 管理、Confirm cards。
6. 默认装配的 Agent 绑定量器官（配置列表），本轮 registry = 已绑定集合，不再 `PrepareTurnToolSurface`。
7. `go test ./framework/agent/...` 与 `go test ./portal/internal/chat/...` 在删除闸后仍能通过；针对已删闸的测试删除或改成「确认不再接线」。

---

## 8. 非目标

- 不新建产品、不换 Web 技术栈、不抽新 git 仓库
- 不做 Trajectory→RL、不对标 Claude Code 全套 IDE
- **不**把 Growth / MEA / Hub 重写成正确版本；先移出核心
- 本规格阶段 **不改代码**；不强制同步完成 `framework/harness` 包搬家
- 不删除 Gateway 企微适配（分发层 KEEP）

---

## 9. 风险与迁移

| 风险 | 处理 |
|------|------|
| 现网靠闸「看起来像在调查」 | 接受乱调回归；用绑定器官 + Skill 约束，而不是恢复短语闸 |
| RCA 评测 / evalgolden 绑死闸行为 | 随 CUT 删或改夹具；工业评测不阻塞核心减肉 |
| 已关但未删的 investigation 开关 | 减肉时连源码一起删，避免「再打开」 |
| 双根路径仍在 Portal UI | 整仓变体退役；rca 工具改为走 `workspace/code`（P2），不在 P1 做 |

---

## 10. 测试与回归口径

减肉每个切片必须带：

- **行为消失测试**：默认 builder 上，相关类型/函数不存在或不再被 `BuildReActAgent` 引用（编译期删除即可；保留的测试断言「无任务锁文案」「无 EvidenceGate 字段」）。
- **外壳仍活**：Portal 创建 Agent → 有 workspace；对话 SSE 仍能跑通假 Model + 无工具或计算器级器官。
- **免疫仍活**：`harness/hooks.yaml` block 仍生效；confirm 写路径仍 pending。

禁止把 `_neo4j_q/` 当夹具。

---

## 11. 实施切分（后续计划，本文件不实施）

每份计划必须单独可合并、可测试。禁止一份 PR 同时搬家包名 + 删闸 + 改 UI。

| 序号 | 计划（建议文件名） | 可验收交付 |
|------|-------------------|------------|
| P1 | `docs/superpowers/plans/2026-09-05-slim-harness-strip.md` | 删除 §6.1 短语闸**类型、文件、循环接线**与 `ReActConfig` 的 `EvidenceGate`/`CodeClaimGate`；拆除 `code_workset` 默认注入；**同时拆除所有编译引用**（含 portal 的 `WithReActEvidenceGate` / `CodeClaimGateTurnOption`）并改掉引用已删类型的测试。`ShouldApplyEvidenceGate`（portal 启发式）**P1 不删**（`mea_autochecks` 用到 P4）。**不**删调查闸 / turn-surface / task lock（P3）。`PostModelPolicy` 接口与 `plan_agent.go` 留到后续切片。`go test ./framework/agent/...` 绿；`cd portal && go test ./internal/chat/... ./internal/service/...` 绿。 |
| P2 | workspace 一等公民 | **仅拦新建**：创建 Agent 必有默认可写根；空 root 字符串拒跑；UI/API **不再提供整仓当 workspace**。已有整仓/空 root 行本切片不强制迁移（读取时：空 root 拒跑；整仓仍能打开但文档标明退役）。验收必须包含：`rca_*` 路径经 `workspace/code`（或明确仍走独立 roots 的 waiver 并开后续任务）。**Run 拒整仓见 S5**；**RCA 只走 mount 见 S4** |
| P3 | Portal 降为装配器 | 删除调查闸 / 任务锁 / turn-surface / skill 预注入 / Prompt 叠层（`catalog_prompt.go`、`web_prompt.go`、`code_analysis_prompt.go`、`datasource_prompt.go` 及 `FormatToolCatalogPrompt` 接线）。**不删** `mea_*.go` / Hub（P4）。`agent_builder` 只装配 Agent `systemPrompt` + Context；无调用者则删 `PostModelPolicy` |
| P4 | 平行面降级 | 默认路径不再接线 growth/mea/hub/hypertool；SQL heal / query spill 退出**默认**自愈；Insights 退出默认导航；拆 MEA 后删除 `ShouldApplyEvidenceGate`；若仍无调用者再删 `plan_agent.go` |

**顺序**：P1 必须先于 P3（P3 仍依赖 `PostModelPolicy` 接口直到它自己拆闸）。P2 可与 P1 并行。P4 最后。

P1 是减肉主路径，单独成实施计划。P1 允许改 Portal **仅限**去掉对已删 framework 类型的引用，不算提前做 P3。**P1 必须保留** `framework/agent/evidence_tools.go`（`IsSkillsFamilyToolName` / `HasSuccessfulBoundEvidence`：P3 的 `turn_intent_gate.go` 仍引用）。

## 12. P4 之后（S1–S6）

P1–P4 完成后的下一轮。禁止一份 PR 同时清扫 + 迁管道 + 改包名。

| 序号 | 规格 | 可验收交付 |
|------|------|------------|
| S1 | [dead-code-hub-off](./2026-09-05-dead-code-hub-off-design.md) | 删 ActiveFamilies / turn-surface 开关 / `queryWithSchemaHeal`；Hub HTTP+Web UI 拆除；procedural 退出默认预取。不删 `framework/memory/hub`、`growth`、`mea` |
| S2 | [context-promptbuilder](./2026-09-05-context-promptbuilder-design.md) | 新建 `framework/context`；L0/L1/L2 迁出 `model`；PromptBuilder Stable/Ephemeral + `prompt_stable_hash`；Provider 内部不再压缩 |
| S3 | [harness-workspace-rename](./2026-09-05-harness-workspace-rename-design.md) | `agent` → `harness`（可留一季别名）；抽出 `workspace`（pathguard + `code/` 挂载纯函数）。不改循环语义 |
| S4 | [rca-code-mount-only](./2026-09-05-rca-code-mount-only-design.md) | 关掉 P2 RCA 独立 `roots` waiver：`rca_code`/`rca_symbol` 只走 `workspace/code`；无挂载不注册。整仓 workspace 仍不强制迁移 |
| S5 | [whole-repo-run-reject](./2026-09-05-whole-repo-run-reject-design.md) | 关掉 P2「已有整仓仍能打开」waiver：Chat / Stream / 快捷 Chat / ExecuteSkill 与 Create 同一错误码拒绝整仓。不自动 `LinkCode`、不改库、不拦 Update |
| S6 | [whole-repo-update-reject](./2026-09-05-whole-repo-update-reject-design.md) | 关掉 S5「Update 不拦」waiver：Update 写入整仓路径与 Create 同一错误码；空字符串落到 `{data_root}/agents/{id}`。不自动 `LinkCode`、不改未保存的行 |

**顺序**：S1 → S2 → S3 → S4 → S5 → S6。
