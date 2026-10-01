# Harness 运行时控制改进(按问题类型开启控制)

> 记录日期: 2026-10-01
> 范围: `framework/harness`(stop 规则 / critic / guardrail)、`framework/tool`(investigation、es_log_query)、`portal/internal/chatsse`
> 来源: 线上会话复盘 + 与 Claude Code、Hermes Agent 源码设计对比

## 背景

### 复盘会话

会话 `8a9e9efb-a323-403a-813f-404cea99620d`(agent `e8107fb3-…`,模型 deepseek-v4-pro):

| 轮次 | 用户问题 | 结果 |
|------|----------|------|
| 1 | 查看最近一个小时一直预启动失败的 vmid | 97 步。第 5 步左右已从 `backend-sched-planner-*` 拿到 13 台 `err cnt check failed` + 2 台 `code[3036]`,随后转入 investigation 台账,补 onset / 对照 / change,把时间窗拉到 3d、7d 去查 9/23 首次 `reach errLimit` 和发布记录。最终回复在回应结案审查的"四点",**没有列出任何 vmid** |
| 2 | 查看最近一个小时一直预启动但是收到预启动成功事件的 vmid,只需要查日志即可 | 47 步。把问题理解成"失败 VM 里有没有成功回调"(结果 0 条);`agg_field: vmId` / `vmid.keyword` 返回空桶后,逐台单查 13 次;第 15 步模型无正文无工具调用,portal 兜底为 `本轮未生成有效回复` |

另外发现:
- 第 2 轮 `set_symptom` 的返回里仍挂着第 1 轮的 h1/h2/h3,台账跨问题延续。
- `read_skill_file(new-prelaunch-troubleshoot, references/prelaunch-diagnostics.md)` 报文件不存在。
- 第 1 轮多次 `add_evidence` 报 `quote not found in any tool output`,浪费步数。

### 与 Claude Code 的设计差异

Claude Code 的运行时硬约束几乎都是**安全类**(权限模式、Bash 内容校验、沙箱、auto 模式分类器),推理流程交给模型与提示词;Stop hook 默认不配置,由用户自己加。

sixath 在此之外把**排障方法论写进了运行时**:证据原文逐字核对、accepted 必须有对照、stop 规则检查台账缺口、critic 结案审查。这对"为什么"类问题是优势(弱模型也能给出可靠因果链),但默认对所有问题生效,列举 / 统计类问题会被拖进根因调查,即上面的复盘现象。

**结论**: 不是拆掉厚控制,而是**按问题类型开启**,并让调查模式成为显式可选。

## 优先级图例

沿用 [README.md](./README.md):🔴 A 严重 / 🟠 B 中等 / 🟡 C 较小 / 🔵 D 方向。

## 改进项

### 🔴 A1 按问题类型开启调查控制

**现状**: `critic_hook.go` 在"本轮工具调用 ≥6 或用过台账"时触发;`harness_stop_rules.go` 的 `when_investigation_gaps` 只看台账状态,不看用户问的是什么。

**建议**:
- 在一轮开始时判定问题类型,至少区分:
  - `enumerate` / `aggregate` / `lookup`(哪些、多少、是什么)→ 只保留安全类约束(工具护栏、审批)
  - `diagnose`("为什么"、根因、失败原因)→ 启用台账 + stop 规则缺口检查 + critic
- 判定来源按优先级:技能 frontmatter 声明 > 轻量规则(关键词 / 句式)> 可选的小模型分类。
- 结果写入 request metadata,`CriticHook`、`HarnessStopRule`(新增 `when_intent` 条件)、`InvestigationObserver` 统一读取。

**涉及**: `framework/harness/critic_hook.go`、`harness_stop_rules.go`、`request_meta.go`

### 🔴 A2 调查模式显式开启,不因"碰过 investigation 工具"就全程生效

**现状**: 模型一旦调用 `investigation`(哪怕只是 `set_symptom`),后续 stop 规则和 critic 就按调查标准要求补齐。

**建议**:
- 非 `diagnose` 类问题中,`investigation` 工具默认不可见,或调用后不触发缺口检查。
- 提供显式开关:用户说"查根因"、技能声明 `mode: investigate`、或 `deep_investigate` 子任务内部才进入完整调查模式。

**涉及**: `framework/tool/investigation_tool.go`、`framework/harness/harness_stop_rules.go`

### 🔴 A3 台账按问题隔离

**现状**: `InvestigationLedger` 按会话保存,第 2 轮新问题继承第 1 轮的假设与缺口,stop 规则 / critic 据此继续施压。

**建议**:
- 新的用户问题(或 `set_symptom` 与当前 symptom 明显不同)时归档旧台账、开新台账。
- 缺口检查只针对当前台账。

**涉及**: `framework/tool/investigation_tool.go`

### 🟠 B1 最后一步空回复时自动重试

**现状**: 模型返回空正文且无工具调用即视为结束,portal 落库 `EmptyReplyNotice`(`portal/internal/chatsse/sse.go`)。

**建议**: 在 `ReActAgent.Run` 内识别空回复,注入一次"请基于已有工具结果直接给出答案"的提示后重试(独立预算,1 次即可);仍为空再走 portal 兜底。

**涉及**: `framework/harness/react_agent.go`、`portal/internal/chatsse/sse.go`

### 🟠 B2 `es_log_query` 聚合字段不存在时明确报错,并支持从文本提取计数

**现状**: `agg_field` 指向 mapping 中不存在的字段(如 `vmId`,实际只在 `M` 文本里)时静默返回空桶;对 `M.keyword` 聚合则返回整条日志,无法按 vmid 计数。模型只好分页拉 500 行人工数或逐个单查。

**建议**:
- 聚合字段不在 mapping 中时返回明确错误,并给出候选字段(复用 `field_hints`)。
- 新增按正则从文本字段提取再计数的能力,例如 `extract: {field: "M", pattern: "vmId\\[(\\d+)\\]"}`,对全量命中(或落盘结果)做 group-by,配合 `result_stats` 使用。

**涉及**: `framework/tool/es_log_tool.go`、`es_log_query_opts.go`、`es_log_mapping.go`、`result_stats.go`

> 已在 [05-tools.md](./05-tools.md) 展开:聚合字段校验并入 A5(执行前统一校验字段),正文提取计数见 B7。

### 🟡 C1 `add_evidence` 引用失败的提示要可操作

**现状**: 多次 `quote not found in any tool output of this session`,模型反复重写 quote 仍失败(常见原因是引用了源码或截断后的内容)。

**建议**: 错误中附上最接近的候选片段及其 `tool_call_id`,或提示"该内容来自被截断的结果,请先用 result_stats / rca_read 取出原文"。

**涉及**: `framework/tool/investigation_tool.go`

### 🟡 C2 技能包引用了不存在的文件

**现状**: `new-prelaunch-troubleshoot` 的正文引用 `references/prelaunch-diagnostics.md`,文件不存在。

**建议**: 补齐文件;并在技能加载 / 校验时检查 SKILL.md 中引用的相对路径是否存在,缺失时告警。

**涉及**: 工作区 `skills/new-prelaunch-troubleshoot/`、`framework/skills/loader.go`

### 🔵 D1 经验沉淀移到后台复盘(借鉴 Hermes)

**现状**: 案例沉淀发生在用户等待的那一轮里,由主 agent 调 `case_library` 生成草稿(如会话"4103_3qmPeUyM9c93 这条流水串流关机后…"的回复以"案例已沉淀为草稿(`case-20260930-184906-121f8b`,状态 pending)"开头)。这既占用本轮步数和上下文,也让答复内容掺杂沉淀动作。

**Hermes 的做法**: 主循环只负责答复;每隔若干轮由 `agent/background_review.py` fork 一个只允许使用记忆 / 技能工具的复盘 agent,异步修补或新建技能;`agent/curator.py` 定期统计使用情况、归档和合并。Hermes 不设人工审批,sixath 应保留审批。

**建议**:
- 一轮结束后(仅 `diagnose` 类、且台账有 accepted 结论或 `set_root_unknown` 的轮次),异步触发复盘任务,输入为该轮台账 + 关键工具调用摘要(可从 `turntrace` 取)。
- 复盘任务产出:案例草稿(`investigate/cases.FileStore.SaveDraft`)和 / 或技能修改提案(`EvolutionProposal`),一律进入 pending,经 Portal 人工审批后生效。
- 主 agent 本轮不再调用 `case_library` 写草稿,只保留检索(召回相似案例)。
- 复盘任务使用独立预算与辅助模型,失败不影响主会话;同一会话去重,避免重复提案。

**涉及**: `framework/tool/case_tool.go`、`framework/investigate/cases/file_store.go`、`framework/turntrace/store.go`、`portal/internal/biz/evolution.go`

## 实施节奏

- **Phase 1**: A3(台账隔离)、B1(空回复重试)、C2(补文件 + 加载校验)—— 改动小、直接止血
- **Phase 2**: A1 + A2(问题类型判定与调查模式开关)—— 需要设计 metadata 字段与技能 frontmatter 约定,并补 eval 用例
- **Phase 3**: B2(ES 文本提取聚合)、C1(引用失败提示)
- **后续立项**: D1(后台复盘沉淀)—— 依赖 A1 的问题类型判定,需单独评估复盘触发条件与审批流

## 验收用例

- "最近一小时一直预启动失败的 vmid" → 直接返回 vmid 清单与次数,不触发台账 / critic,步数显著下降(参考值 ≤10)。
- "vm=xxx 最近一次预启动为什么失败" → 仍走完整调查流程,stop 规则与 critic 行为不变。
- 同一会话先问根因、再问列举 → 第二问不受第一问台账缺口影响。
- 建议把上述用例加入 `evals/tasks/investigation.jsonl`。
