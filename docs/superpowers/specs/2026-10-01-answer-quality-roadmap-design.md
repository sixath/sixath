# 答案质量改进路线（一）：路线图、立即修复项与端到端评测

**日期**: 2026-10-01  
**状态**: 设计已确认（待用户审阅）  
**范围**: 本规格只实现 **阶段 0（立即修复）** 与 **阶段 1（端到端评测基线）**。阶段 2、3 只给出路线与约束，有了基线后各自另写规格。  
**输入**: [`framework/docs/improvements/04-harness.md`](../../../framework/docs/improvements/04-harness.md)、[`framework/docs/improvements/05-tools.md`](../../../framework/docs/improvements/05-tools.md)、线上 209 个会话统计（10459 次工具调用，失败 13%，空结果 922 次）。

**一句话**: 04/05 主要解决"工具调不对、查不到"，但复盘会话的失败首先是"没理解问题、答非所问"。先建端到端评测量出基线，再按风险分阶段上线；外层控制机制必须带评测证据才能默认开启。

---

## 1. 背景与问题

### 1.1 04/05 能解决什么

| 问题 | 04/05 覆盖 |
|------|-----------|
| ES 集群缺默认索引（182 次失败） | 05 A1 |
| `execute_read` 被拿来查 ES（124 次 SQL 语法错误） | 05 A2 |
| 字段写错、值猜错，返回 0 条但不报错 | 05 A4/A5/B4/C3/C4 |
| 列举类问题被拖进根因调查 | 04 A1/A2 |
| 台账跨轮串用 | 04 A3 |
| 空的最终回复 | 04 B1 |

### 1.2 04/05 没覆盖的缺口

1. **没有问题理解环节**：复盘会话第 2 轮把"收到成功事件"理解成另一件事，后续工具全对也答不对。
2. **04 A1 用关键词判断类型会误判**："一直预启动失败的 vmid"含"失败"，会被判成 diagnose，实际要的是清单。
3. **没有对题检查**：结案审查只查推理是否完整，不查答案是否回应了问题。
4. **空回复原因未查明**：04 B1 直接加重试，可能掩盖真实原因（输出截断、模型拒答等）。
5. **成功标准是工具失败率**：工具失败率下降不等于用户拿到答案。
6. **机制膨胀风险**：stop 规则、结案审查、台账、护栏已叠加，再加机制没有证据会互相干扰。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 成功标准 | 端到端通过率为主，工具指标为辅 |
| 评测判定 | 只判答案形式，不建标准答案；模型自动打分 |
| 推进方案 | 评测先行；05 A1、05 C2 立即做 |
| 评测环境 | 真实环境每晚全量 + 合成数据子集进 CI |
| 规格拆分 | 本规格 = 路线图 + 阶段 0 + 阶段 1；阶段 2、3 另写规格 |

---

## 3. 路线图（总览）

| 阶段 | 内容 | 上线条件 | 规格 |
|------|------|---------|------|
| 0 立即 | 见 §4 | 无需评测 | 本规格 |
| 1 基线 | 见 §5 | 基线文件入库 | 本规格 |
| 2 工具确定性校验 | 05 A2、A3、A4/B4、A5、B1、B2、C1 | 单测覆盖；工具失败率、空结果率下降；端到端通过率不下降 | 另写 |
| 3 外层机制 | 问题卡片 + 按类型启用调查机制（替代 04 A1/A2）；对题检查；04 A3；04 B1 重试（视阶段 0 日志结论） | 每个机制独立开关、独立 A/B：通过率提升，`diagnose` 子集不下降，平均步数增幅 ≤20%；否则默认关闭 | 另写 |
| 4 另行立项 | 04 D1、05 B5/B6/B7、04 B2 剩余 | 有基线后再评估 | 另写 |

**防膨胀约束**：新增外层机制必须附评测证据；每季度复查一次，命中率低于 5% 或 A/B 无收益的机制删除或改为默认关闭。

### 3.1 阶段 3 的方向约束（供后续规格使用，本规格不实现）

- **问题卡片**：每轮开始前用辅助模型单独调用一次，产出 `{answer_type, target, filter, time_window, deliverable, ambiguity}`。判定依据是期望的答案形式，不是关键词。`ambiguity` 仅在两种理解导致查询完全不同时填写并触发 `ask_user`。
- **对题检查**：结束前用低成本模型按 §5.2 规则 1–3 检查，最多打回一次，独立预算，调用失败放行。
- 后续规格必须回答的问题（spec 评审中发现的代码事实）：
  1. `AuxiliaryModel` 目前只是 `ContextCompressionConfig` 的字段，仅在 `L2Enabled` 时使用，`ReActConfig` 不保存它；需要新选项把辅助模型传进 agent。
  2. 请求 metadata 在 `Run` 之前构造，`StopHookInput` 不带 metadata；卡片的传递方式（ctx 或 `StopHookInput` 新字段）要明确。
  3. `evaluateStopHooks` 只返回第一个要求继续的 hook；对题检查必须注册在 `CriticHook` 之前，先保证对题再查推理。
  4. "非 diagnose 不启用台账"取"工具可见但不做缺口检查与结案审查"还是"按轮隐藏工具"，需二选一。
  5. 各机制开关的位置（Portal agent 配置 / `hooks.yaml`），以及 portal 模式 A/B 如何切换配置。
- 04 D1：后台复盘已在 [S16](./2026-09-05-background-review-off-design.md) 退出默认路径，GrowthWorker 仅在 `growth.worker_enabled` 时构造；立项时复用该 worker。

---

## 4. 阶段 0：立即修复

### 4.1 ES 集群默认索引（05 A1）

- 运维：为 Portal 数据源中的 `zj-elk`、`zj-elk_flow`、`mg-rca-es` 配置 `default_index`。
- 代码兜底（`portal/internal/chat/es_log_clusters.go` 及 `es_log_query` 描述生成处）：集群 `default_index` 为空时，工具描述标明"该集群必须传 index"，并列出该集群 `IndexCatalog` 中的索引模式；报错 `index is required…` 时附带同一列表。

### 4.2 凭据脱敏（05 C2）

- 执行时间线落库前、终端/`ssh_exec`/`http` 工具参数回显前，对以下模式脱敏为 `***`：`-u user:pass` 的密码部分、`password=`/`passwd=`/`pwd=` 的值、`Authorization:` 头的值、URL 中 `user:pass@`。
- 只改展示与持久化，不改实际执行参数。
- 脱敏函数集中在一个文件，表驱动单测覆盖上述模式。

### 4.3 缺失的技能参考文件（04 C2）

为 `new-prelaunch-troubleshoot` 补上 `references/prelaunch-diagnostics.md`；若内容无人能写，则从技能正文删除该引用。二选一，不保留悬空引用。

### 4.4 空回复诊断日志（04 B1 前置）

最终回复为空时，在 turn trace 与服务日志中记录：`finish_reason`、输入/输出 token 数、原始响应前 2KB、最后一步是否有工具调用。只记录，不重试。积累一周后决定阶段 3 是否做重试。

---

## 5. 阶段 1：端到端评测

### 5.1 任务格式

在 `evals/runner/task.go` 中：

- `ValidCategories` 新增 `answer_shape`。
- `Task` 新增可选字段 `SourceSession string \`json:"source_session,omitempty"\``。
- `Expectation` 新增：
  - `AnswerType string \`json:"answer_type,omitempty"\``：`enumerate | count | lookup | diagnose | howto`。
  - `ShapeMust []string \`json:"shape_must,omitempty"\``：给 judge 看的自然语言要求。
  - `ShapeForbid []string \`json:"shape_forbid,omitempty"\``：给 judge 看的自然语言禁止项（区别于正则 `forbid_output`）。
- `answer_shape` 的校验：`answer_type` 必须是上述五值之一；`max_steps` 照常必填（live 模式使用，portal 模式忽略，约定填 30）。

任务文件 `evals/tasks/answer_shape.jsonl`，20–30 题，必须包含复盘会话的两道失败题，`diagnose` 不少于 5 题。带 `fixtures` 且打 `ci` 标签的题进入 CI 子集（5–8 题，至少含"VM 预启动失败列举"与"字段写错导致空结果"两类）。入库前人工检查问题文本，去除内部主机名、账号等敏感信息。

```json
{"id":"vm-prelaunch-list","category":"answer_shape","max_steps":30,
 "input":"最近一小时一直预启动失败的 vmid 有哪些",
 "expect":{"answer_type":"enumerate","shape_must":["开头给出 vmid 清单"],"shape_forbid":["未被要求的根因分析"]},
 "source_session":"8a9e9efb-a323-403a-813f-404cea99620d","tags":["ci"],"fixtures":[...]}
```

### 5.2 打分规则

judge 逐条输出 pass/fail 与理由，四条全过才算通过：

1. 答案形式与 `answer_type` 一致（列举给清单、计数给数字、查值给值、诊断给原因与依据、操作给步骤）。
2. 第一段直接回答，不先铺垫过程。
3. 满足 `shape_must`，不触犯 `shape_forbid`，没有其他未被要求的扩展。
4. 凡声称"没有/不存在/未发现"，对应查询不能是可疑空结果（字段不存在、值猜测、时间窗过短）。

失败归因沿用现有 `prompt|tool|schema|model|harness`，新增 `understanding`（理解错问题），由 judge 给出。

live 模式中 `answer_shape` 像 `investigation` 一样在 `runLiveTask` 里提前分支走 judge，不经过"工具报错即判 `tool_error`"的通用判定。

### 5.3 统一轨迹摘要

新增 `evals/runner/trace_summary.go`，定义两种模式共用的 `TraceSummary`：每次工具调用的工具名、参数（截断 500 字符）、是否报错、命中数（能解析时）、是否空结果。

- portal 模式：从 `metadata.timeline` 构造。
- live 模式：从 `RunTrace` 与 fixture 命中记录构造。

judge 与辅助指标只依赖 `TraceSummary`。

### 5.4 打分器 `evals/runner/judge.go`

- 输入：问题、`expect`、最终答案、`TraceSummary`。
- 输出 JSON：`{"checks":[{"id":1,"pass":true,"reason":"..."},...],"attribution":"understanding"}`。
- 模型参数：`-judge-provider`、`-judge-model`、`-judge-base-url`、`-judge-api-key`（默认读 `SATH_EVAL_JUDGE_API_KEY`）。
- 被测模型由 `-sut-model` 声明（live 模式默认取 `-model`）；与 `-judge-model` 相同时报错退出。
- 规则 4 确定性预检：答案命中否定词表（没有、不存在、未发现、未找到、0 条、无记录、查不到）且 `TraceSummary` 中有空结果时，把这些查询单独列给 judge 核对。词表为包级变量，单测覆盖。

### 5.5 Portal 模式 `evals/runner/portal.go`

- 流程：创建会话 → `POST /messages/stream` 发送问题并读 SSE 至结束事件 → 拉取 `/sessions/{id}/messages` 与 `metadata.timeline`。SSE 中断时改为轮询 `ListMessages` 兜底；从发送起整体超时 10 分钟。
- 参数：`-portal-url`、`-agent`、`-token`（默认读 `SATH_EVAL_PORTAL_TOKEN`）、`-concurrency`（默认 2）、`-repeat`（默认 1）。
- 每题新建会话，名称前缀 `eval-<run_id>`。
- A/B：两组各指定一个 agent（`-agent`），同一批题分别运行，再用 report 模式对比两份结果。

### 5.6 指标与门禁

- `metrics.go`：`answer_shape` 结果单独汇总为 `e2e_pass_rate`、按 `answer_type` 拆分的通过率，以及辅助指标（工具错误率、空结果率、规则 4 失败数、平均步数）。`answer_shape` 任务不计入现有 `completion_rate`，现有门禁不变。
- `infra_error`（Portal 超时/5xx）与 `judge_error`（judge 输出重试 1 次仍不可解析）不计入通过率，报告单列；两者合计超过 20% 时本次运行判为无效，门禁直接失败。
- 每晚门禁：`-repeat 2` 取平均，`e2e_pass_rate` 较基线下降超过 **8 个百分点**失败（高于 §7 允许的 5 个百分点自然波动）。`diagnose` 子集在阶段 1 只报告不门禁。
- CI 门禁：CI 子集每题跑 3 次，3 次中至少 2 次通过算通过；基线中通过的题变为不通过则门禁失败。CI 沿用现有 live 模式的运行方式（需要模型与 judge 的 API key）。
- 重复运行：live 与 portal 模式都支持 `-repeat`。同一题多次运行合并为一条 `TaskResult`，新增 `runs`、`passes` 字段；`passed` 按多数判定（每晚 2 次取 `passes/runs` 计入平均）。
- 基线：现有 `Summary` 基线只有汇总，不足以做单题回归。CI 门禁的基线改用一份入库的结果 JSONL（`evals/baselines/answer_shape_ci.jsonl`）；每晚门禁继续用 `Summary` 中的 `e2e_pass_rate`。

### 5.7 环境

- 每晚：真实环境 portal 模式全量；结果写入 `evals/results/nightly/<date>.jsonl`，与基线比较。
- CI：live 模式 + fixtures 跑 CI 子集。

### 5.8 错误处理

| 情况 | 处理 |
|------|------|
| Portal 超时 / 5xx | 记 `infra_error`，不计入通过率 |
| judge 输出无法解析 | 重试 1 次，仍失败记 `judge_error` |
| 两者合计 >20% | 本次运行无效，门禁失败 |
| `-sut-model` 与 `-judge-model` 相同 | 启动时报错退出 |

---

## 6. 非目标

- 不建标准答案库，不评估答案数值是否正确。
- 不录制回放真实数据源响应。
- 不做网页报表，复用现有 markdown 报告。
- 不实现阶段 2、3、4 的任何改动（问题卡片、对题检查、05 工具校验、04 D1 等）。

---

## 7. 测试与成功标准

**测试**：

- 脱敏函数：表驱动覆盖 §4.2 全部模式及不应误伤的普通文本。
- ES 描述兜底：`default_index` 为空时描述与报错包含索引列表。
- `task.go`：`answer_shape` 合法/非法 `answer_type`、缺 `max_steps`。
- `trace_summary.go`：从 timeline 样例与 `RunTrace` 样例各构造一次。
- `judge.go`：固定模型输出覆盖解析成功、解析失败重试、规则 4 预检、同模型报错。
- `portal.go`：`httptest` 模拟正常结束、超时、5xx。
- `metrics.go` / `gate.go`：`e2e_pass_rate` 计算、错误排除、20% 无效判定、8 个百分点阈值、CI 3 次 2 过、按单题基线回归。
- `live.go`：`answer_shape` 任务在工具报错时仍走 judge；`-repeat` 合并结果。

**成功标准**：

1. 阶段 0 完成后，三个集群的"index is required"失败归零；时间线与工具参数回显中不再出现明文凭据；技能无悬空引用；空回复有诊断日志。
2. 阶段 1 产出可复现的端到端基线：连续两次每晚运行的 `e2e_pass_rate` 相差 ≤5 个百分点。
3. 基线报告中，复盘会话的两道题被判为不通过且归因为 `understanding` 或 `harness`（验证评测能抓到真实失败）。
