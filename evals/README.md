# Agent 评测体系（evals）

> 成熟度计划 Task 13（G8：改动效果可量化）。目标是让任何 prompt / 工具 / 压缩策略的改动都能产出**量化的前后对比**，而不是「感觉没坏」。

## 目录结构

```
evals/
  tasks/              任务集（JSONL，每行一条任务）
    single_tool.jsonl
    multi_tool.jsonl
    long_horizon.jsonl
    hitl.jsonl
    memory_recall.jsonl
    safety.jsonl
  tasks_portal/       真实环境 answer_shape 题（portal 模式，每晚）
    answer_shape.jsonl
  tasks_ci/           CI 用 answer_shape 题（带 fixtures，live 模式）
    answer_shape_ci.jsonl
  runner/             Go 评测 runner（独立 module，stdlib-only）
    main.go            CLI
    task.go            任务/结果加载与校验
    metrics.go         指标计算（完成率、工具 F1、步数、成本、归因）
    report.go          报告构造与序列化
    gate.go            回归门禁（baseline 对比）
    testdata/          冒烟用的样例结果
  reports/
    baseline.json      基线快照（当前 total=0，待首次真实运行回填）
    history/           历史报告
```

## 任务 schema

每条任务一行 JSON：

```json
{
  "id": "single_tool-1",
  "category": "single_tool",
  "input": "列出 archive 数据库里的所有表",
  "expect": { "tools": ["list_tables"] },
  "max_steps": 3,
  "tags": ["data", "mysql"]
}
```

六个类别与判据字段（`expect`）：

| 类别 | 判据字段 | 含义 |
|------|----------|------|
| `single_tool` | `tools` | 应命中的工具集合 |
| `multi_tool` | `tool_sequence`（或 `tools`） | 严格顺序的工具序列 |
| `long_horizon` | `output_contains` | 最终产物需包含的子串（+ `max_steps` 步数上限） |
| `hitl` | `must_ask_confirm` | 是否应在正确时机请求确认 |
| `memory_recall` | `must_recall` | 召回内容需包含的关键词 |
| `safety` | `must_refuse` | 不得调用的工具黑名单 |
| `answer_shape` | `answer_type`（+ `shape_must` / `shape_forbid`） | judge 判定答案形式是否回应问题 |

`tasks_portal/` 与 `tasks_ci/` 不放进 `tasks/`：每晚的 `-mode=live -tasks ../tasks` 会加载该目录下全部 `*.jsonl`，混入 answer_shape 题会因缺 `-judge-model` 直接退出，而 runner 不支持按标签过滤。

## Runner 用法

```bash
cd evals/runner

# 单元测试
go test ./... -count=1

# mock 回放：脚本化模型确定性驱动 ReActAgent 回放任务集（进 CI 回归门禁）
go run . -mode=mock -tasks ../tasks -out -

# live 评测：真实模型跑任务集，产出结果 JSONL + 报告（nightly 回填 baseline）
# 模型配置走 OpenAI 兼容 API（provider/base-url 可自定义）；key 用 -api-key 或 SATH_EVAL_API_KEY
go run . -mode=live \
  -tasks ../tasks \
  -model deepseek-v4-pro \
  -base-url https://aiapi.icloudsky.com/v1 \
  -api-key "$SATH_EVAL_API_KEY" \
  -results-out ../reports/live_results.jsonl \
  -out ../reports/live_report.json

# 冒烟：读结果 → 算汇总 → 输出报告（不做门禁）
go run . -mode=report \
  -results testdata/sample_results.jsonl \
  -tasks ../tasks \
  -out -

# 带基线做回归门禁（完成率相对 baseline 下降 >2pt 即退出码非 0）
go run . -mode=report \
  -results <results.jsonl> \
  -tasks ../tasks \
  -baseline ../reports/baseline.json \
  -out ../reports/history/$(date +%Y%m%dT%H%M%S).json
```

## 指标

- `completion_rate`：任务完成率
- `tool_selection_f1`：工具选择 F1（宏平均，仅对有工具期望的任务）
- `avg_steps`：平均步数（越低越高效）
- `avg_cost_usd`：平均单任务成本
- `by_category`：分类别完成率
- `attribution`：失败归因分布（`prompt | tool | schema | model`）
- `avg_ledger_score` / `ledger_scored`：台账质量（仅 `-ledger` 下的 investigation 任务）：onset 有原文、落在 `expect_onset` 内、因果链到达 root、`expect_kinds` 都在
- `avg_critic_rounds`：结案审查打回的平均次数（`-critic`）
- `hit_max_steps`：用尽步数、由强制总结收尾的任务数；这类任务不会经过 stop_rules / critic

## investigation 任务与 A/B

investigation 任务用 `fixtures` 合成工具返回，根因只能沿证据链得出。判据按顺序：`forbid_output`（只宣告意图就收尾）→ `min_tool_calls` → `must_contrast` → `forbid_root`（根因句写的是内部机制，如错误计数、限流、重试）→ `root_cause`。`expect_onset`、`expect_kinds` 只参与台账评分。

```bash
# A 组：裸跑
go run . -mode=live -tasks ../tasks/investigation.jsonl -model <m> -base-url <url> -extra-steps 16 -results-out a.jsonl -out a.json
# B 组：stop_rules + 台账 + timeline + 结案审查（critic 配置取自 -stop-rules 文件的 critic 段）
go run . -mode=live -tasks ../tasks/investigation.jsonl -model <m> -base-url <url> -extra-steps 16 \
  -stop-rules ../../framework/harness/testdata/stop_rules_example.yaml -ledger -timeline -critic \
  -results-out b.jsonl -out b.json
```

`-extra-steps` 两组取相同值：工具更多的一组更容易撞上 `max_steps`，撞上后 stop_rules 和 critic 都不会运行。

框架默认单次回复上限 1024 token，调查类任务的长结论（表格、因果链）会被截断，工具调用 JSON 也可能被截断而整轮空转；live 评测建议两组都加 `-max-output-tokens 4096`。

框架默认单次回复上限 1024 token，调查类任务的长结论（表格、因果链）会被截断，工具调用 JSON 也可能被截断而整轮空转；live 评测建议两组都加 `-max-output-tokens 4096`。

`-timeline` 同时注册 `timeline`、`field_history`、`fs_compare`。案例库 A/B 在 B 组基础上加 `-cases <目录>`（需 `-ledger`）：`set_symptom` 召回目录中已确认的案例，并注册 `case_library`。调查中生成的草稿会写入该目录，所以请传 `../cases` 的副本：

```bash
cp -r ../cases /tmp/eval-cases
go run . -mode=live -tasks ../tasks/investigation.jsonl -model <m> -base-url <url> -extra-steps 16 \
  -stop-rules ../../framework/harness/testdata/stop_rules_example.yaml -ledger -timeline -critic -cases /tmp/eval-cases \
  -results-out c.jsonl -out c.json
```

`investigation-case-recall`、`investigation-form-change`、`investigation-code-meaning` 分别对应案例召回、失败形式变化、错误码含义三类能力。

## 答案对题评测（answer_shape）

answer_shape 只判答案形式，不判数值是否正确。judge 模型必须与被测模型不同（`-judge-model` 与 `-sut-model` 同名时 runner 拒绝运行；live 模式 `-sut-model` 默认取 `-model`），按四条规则打分：形式与 `answer_type` 一致、第一段直接回答、满足 `shape_must` 且不触犯 `shape_forbid`、"没有/不存在"类结论不是建立在可疑空结果上（报错、空结果、对未映射字段聚合得到的空桶都不能作为依据）。

这类任务单独汇总为 `summary.answer_shape`（`e2e_pass_rate`、`lost_rate`、`by_answer_type` 等），不计入 `completion_rate`；mock 模式跳过它们。`-repeat N` 让每题跑 N 次，按严格多数合并。

工具轨迹指标（分母均为全部工具调用数）：

| 指标 | 含义 |
|------|------|
| `tool_error_rate` | 工具报错（含 `ok=false` / `hit_status=error`）的调用占比 |
| `empty_rate` | 零结果调用占比（含 `suspect` 与聚合空桶） |
| `suspect_rate` | 零结果被标记为可疑（`hit_status=suspect`，如放宽条件后有数据、搜索根不存在）的调用占比 |
| `arg_reject_rate` | 参数被确定性校验拒绝（结果含 `invalid_arguments`）的调用占比 |
| `arg_rejects_by_keyword` | 参数拒绝按规则 keyword（`required`、`one_of`、`unknown_field`…）的次数分布 |

两种运行方式：
- **portal**（每晚，真实环境）：通过 Portal/Gateway 的对话接口驱动真实 agent，读取消息 metadata 中的工具轨迹交给 judge。直连 Portal 需以 `SATH_CHAT_PUBLIC_INBOUND_ENABLED=true` 启动（否则 POST 返回 403），或把 `-portal-url` 指向 Gateway。只跑 answer_shape 题，其他类别跳过。
- **live + fixtures**（CI）：runner 内用被测模型驱动 ReActAgent，工具返回取自任务的 `fixtures`。

```bash
# 每晚：真实环境。首次运行时基线文件还不存在，先去掉 -baseline 跑一次，再用 -baseline-out 生成基线
mkdir -p ../results/nightly ../reports/history
go run . -mode=portal -tasks ../tasks_portal \
  -portal-url http://<portal-or-gateway> -agent <agent-id> -token "$SATH_EVAL_PORTAL_TOKEN" -org default \
  -judge-provider openai -judge-model <judge-model> -judge-base-url <url> -judge-api-key "$SATH_EVAL_JUDGE_API_KEY" \
  -sut-model <agent-model> -repeat 2 -concurrency 2 \
  -baseline ../reports/answer_shape_baseline.json \
  -results-out ../results/nightly/$(date +%F).jsonl -out ../reports/history/answer_shape_$(date +%F).json

# CI：带 fixtures 的题，每题 3 次取多数，对比单题基线（首次运行同样先去掉 -baseline-results）
mkdir -p ../baselines
go run . -mode=live -tasks ../tasks_ci/answer_shape_ci.jsonl -model <m> -base-url <url> -api-key "$SATH_EVAL_API_KEY" \
  -judge-model <judge-model> -repeat 3 -max-output-tokens 4096 \
  -baseline-results ../baselines/answer_shape_ci.jsonl -results-out ci.jsonl -out ci.json
```

`-results-out` / `-out` / `-baseline-out` 不会自动建目录，需先 `mkdir -p`。

参数要点：
- judge：`-judge-provider` / `-judge-base-url` / `-judge-api-key` 为空时分别回落到 `-provider` / `-base-url` / `-api-key`；key 也可用环境变量 `SATH_EVAL_JUDGE_API_KEY`。任务集中有 answer_shape 题而未给 `-judge-model` 时 live 模式直接退出；portal 模式必须给。
- portal：`-portal-url`、`-agent` 必填；`-token`（或环境变量 `SATH_EVAL_PORTAL_TOKEN`）为 Bearer token；`-org` 为 `X-Org-Id`（默认 `default`）；`-concurrency` 并行题数（默认 2）；`-turn-timeout` 单轮从发送到最终答复的超时（默认 10m），超时即记 `timeout`，之后再等最多 `-grace-period`（默认 2m）让服务端残留运行落库，用于取回轨迹并避免与同题下一次重复重叠；`-judge-timeout` 单次 judge 调用超时（默认 2m）。Ctrl-C 停止派发新题，已有结果照常输出。

fixtures 写法：`{"tool":"es_log_query","match":"flow_id","response":"..."}`。被调用工具名命中 `tool`、且参数 JSON（键按字典序、无空格）包含 `match` 中任一项（`|` 分隔，忽略大小写；空表示任意参数）时返回 `response`；同一工具按声明顺序取第一条命中，所以更具体的 `match` 要放前面；都不命中时返回空 hits。`response` 应按真实工具的返回形态写（`es_log_query` 用 `hit_status` / `total` / `hits` / `aggregations`），轨迹摘要据此判断空结果：`hit_status` 为 `empty` 或 `total=0` 记为空结果；请求了聚合（`agg_field` / `agg_interval`）而聚合桶全空时，即使 `total>0` 也记为空结果（`agg_empty`）。

失败分类：
- 丢失运行（不进 `e2e_pass_rate` 分母）只有 `infra_error`（网关/连接/限流等基础设施故障，live 模式下模型调用失败）和 `judge_error`（judge 未配置、调用失败或输出无法解析）。
- 计入失败：`shape_mismatch`（judge 判不通过）、`timeout`、`run_error`、`hitl_required`（agent 请求人工确认，评测无法回应）、`model_error`（跑满 `max_steps`）。

门禁：
- 丢失运行（`infra_error` + `judge_error`）超过 20% 时本次运行无效，退出码非 0（不需要基线）。
- 每晚：`e2e_pass_rate` 较 `-baseline` 下降超过 8pt 失败（真实环境自然波动约 5pt）。
- CI：`-baseline-results` 中通过的题本次未通过即失败；本次全部运行丢失的题不算回归，由丢失率门禁约束。
- mock / live / portal / report 四种模式门禁一致；门禁失败时不写 `-baseline-out`，避免无效或回归的运行覆盖基线。
- 题量少时门禁很敏感（2 题 × 2 次运行：丢 1 次即 25% 判无效，1 次翻转即 25pt）。题集补到 20–30 题之前，每晚门禁只作参考，不要设为硬门禁。

## 回归门禁

`baseline.json` 是基线 Summary。门禁规则：`completion_rate` 相对 baseline 下降超过 **2pt** 即失败（`gate_passed=false`，runner 退出码非 0）。`total=0` 表示尚无基线，不设门禁。

## 当前状态与后续

- ✅ **已落地**：任务集 schema + 6 类 48 条任务；runner 的加载/校验/指标/报告/门禁。
- ✅ **mock 驱动已接入**：`--mode=mock` 用脚本化模型确定性回放任务集，驱动真实 ReActAgent 校验工具执行管线，进 CI 回归门禁。runner 已 import framework（`go 1.26` + `replace ../framework`）。
- ✅ **live 模式已接入**：`--mode=live` 用真实模型（OpenAI 兼容，`provider`/`base-url`/`model` 可配）跑任务集，产出结果 JSONL + 报告，用于 nightly 回填 baseline。工具选择判据为「期望工具按序作为子序列出现」（精确率由 `tool_selection_f1` 度量）。
- ✅ **工具名已对齐**：任务集里的工具名已逐一核对 `framework/tool` 注册表（`read_file`/`write_file`/`terminal`/`memory_recall`/`todo`/`jaeger_trace`/`es_log_query`/`execute_read`/`execute_write` 等均为真实注册名）。

## CI 集成

`ci.yml` 的 `evals` job：`go test ./...` + `-mode=mock` 回归门禁（脚本化回放 48 条任务，校验工具执行管线）+ `-mode=report` 冒烟。后续接入 `-baseline` 后 mock 完成率即成为硬门禁。