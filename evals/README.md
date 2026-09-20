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

## 回归门禁

`baseline.json` 是基线 Summary。门禁规则：`completion_rate` 相对 baseline 下降超过 **2pt** 即失败（`gate_passed=false`，runner 退出码非 0）。`total=0` 表示尚无基线，不设门禁。

## 当前状态与后续

- ✅ **已落地**：任务集 schema + 6 类 48 条任务；runner 的加载/校验/指标/报告/门禁。
- ✅ **mock 驱动已接入**：`--mode=mock` 用脚本化模型确定性回放任务集，驱动真实 ReActAgent 校验工具执行管线，进 CI 回归门禁。runner 已 import framework（`go 1.26` + `replace ../framework`）。
- ✅ **live 模式已接入**：`--mode=live` 用真实模型（OpenAI 兼容，`provider`/`base-url`/`model` 可配）跑任务集，产出结果 JSONL + 报告，用于 nightly 回填 baseline。工具选择判据为「期望工具按序作为子序列出现」（精确率由 `tool_selection_f1` 度量）。
- ✅ **工具名已对齐**：任务集里的工具名已逐一核对 `framework/tool` 注册表（`read_file`/`write_file`/`terminal`/`memory_recall`/`todo`/`jaeger_trace`/`es_log_query`/`execute_read`/`execute_write` 等均为真实注册名）。

## CI 集成

`ci.yml` 的 `evals` job：`go test ./...` + `-mode=mock` 回归门禁（脚本化回放 48 条任务，校验工具执行管线）+ `-mode=report` 冒烟。后续接入 `-baseline` 后 mock 完成率即成为硬门禁。