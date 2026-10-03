# 答案质量改进路线（二）：工具确定性校验（阶段 2）

**日期**: 2026-10-01  
**状态**: 设计已确认，规格评审一轮后修订（待用户审阅）  
**上游**: [`2026-10-01-answer-quality-roadmap-design.md`](2026-10-01-answer-quality-roadmap-design.md) §3 阶段 2；条目来源 [`framework/docs/improvements/05-tools.md`](../../../framework/docs/improvements/05-tools.md)  
**范围**: 05 中的 A2、A3、A4、A5、B1、B2（部分）、B4、C1，一份规格、一次实现计划。

**一句话**: 在工具注册层（`Registry.Register`）加一层声明式校验中间件：执行前用动态候选、格式、内容特征和字段引用拦下"参数填错"，执行后对零结果做有预算的探测并标记 `suspect`；harness 在模型基于可疑空结果下"查不到"结论时提醒一次。

---

## 1. 背景

线上 209 个会话、10459 次工具调用：失败 13%，空结果 922 次。阶段 0 已修 A1（ES 默认索引），剩余高频问题：

| 现象 | 条目 | 现状（代码审计） |
|------|------|------------------|
| `execute_read` 被塞 Lucene/DSL（124 次 SQL 语法错误） | A2 | 主参数名为 `dsl`（语义是 SQL），另有 `query` 别名与 `index` 参数，`required:[]`；无非 SQL 预检；`cfg.Exec==nil` 时即使有 Reader 也报"未配置" |
| datasource / repo / skill 名写错，只得到"不存在" | A3 | 数据源、skill 无候选；rca repo 列出全部但无模糊匹配；三份私有 levenshtein 重复（`harness/tool_not_found.go`、`tool/es_log_mapping.go`、`tool/es_log_index.go`） |
| 字段不存在却返回 0 条，模型当成"没有数据" | A4/A5 | es 仅在 0 条后才拉 mapping（`es_log_tool.go`），`sort`/`fields` 不校验，mapping 无缓存；MySQL 无预检；`HitStatus` 只有 hits/empty/error |
| 0 条时不知道是条件错还是真没数据 | B4 | 无 `diagnosis` |
| trace_id 格式错、路径猜错 | B1 | `jaeger_trace` 无 hex 校验；`rca_read` 无候选 |
| 必填/默认值不清 | B2 | execute_read 未声明必填；`http_request` 默认 GET 分支不可达（schema 与运行时都要求 method） |
| 参数 JSON 解析失败时模型不知道该填什么 | C1 | `harness/react_agent.go` 解析错误不回显必填字段 |
| MCP 工具入参校验 | — | `InputSchema` 是结构体，`ValidateArguments` 静默跳过 |

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 规格拆分 | 一份规格覆盖全部条目 |
| 实现方式 | 注册层声明式校验中间件（方案三） |
| 执行前发现未知字段 | **不执行**，返回错误 + 候选字段 + 正文字段提示 |
| 零结果探测 | **默认开启**，最多 3 次 count、总超时 3s，工具配置可关 |
| `execute_read` 旧参数 | **软兼容**：保留 `dsl` 为主参数并设为 required；`query` 作为别名归一化到 `dsl`；schema 删除 `query`、`index`；内容为非 SQL 或传了 `index` 时拒绝并指向 `es_log_query` |
| `suspect` 的消费 | 记入 trace/指标；最终回答断言"查不到"且依据只有 suspect/empty 时，stop hook 提醒一次 |

## 3. 架构

### 3.1 执行链与包装位置

现状：`Register` 依次构造 事件/trace 包装 → 超时包装 → schema 校验包装，因此实际调用顺序（外→内）是 **schema 校验 → 超时 → 事件/trace → Execute**；被 schema 拒绝的调用不产生 `ToolInvoked`/`ToolExecuted` 事件和 span。

本期把校验移到事件/trace 之内，由一个新的中间件包装统一承载，最终顺序（外→内）：

```
超时
  → 事件/trace（被拒调用也有事件与 span，payload 含 invalid_arguments）
    → 校验中间件:
        ArgAliases 归一化
        → ValidateArguments（现有 JSON Schema 子集）
        → ArgChecks（按声明顺序；收集全部错误一次返回）
        → Execute
        → EmptyProbe（仅当结果判定为 0 条；在超时之内）
        → hit_status 归一（hits / empty / suspect / error）
```

- 开关**每次调用时读取**（现状在 Register 时读一次，需改）：
  - `SATH_TOOL_ARG_VALIDATION=off`：跳过 `ValidateArguments` 与 `ArgChecks`；`ArgAliases` 仍执行（保证 schema 变更后的兼容）。
  - `SATH_TOOL_EMPTY_PROBE=off`：跳过 `EmptyProbe`；工具配置 `empty_probe: false` 对单个工具生效。
- **防重复包装**：`Tool` 增加未导出字段 `checked bool`，中间件包装后置为 true；`Register` 遇到 `checked` 为 true 的工具不再包装中间件（investigate 子代理 `investigate/runner.go:30-43` 会把已包装工具重新注册到子 registry，否则探测会执行两次并覆盖 `diagnosis`）。超时与事件包装维持现状。

### 3.2 `Tool` 新增字段

```go
type Tool struct {
    // ...现有字段...
    ArgAliases map[string]string // 别名 -> 正式名；正式名已存在时丢弃别名
    ArgChecks  []ArgCheck
    EmptyProbe *EmptyProbe
    checked    bool
}
```

### 3.3 规则类型（`ArgCheck`）

`ArgCheck` 为接口：`Name() string`、`Check(ctx, params) ([]SchemaError, error)`；返回 `error` 表示规则自身失败（走 fail-open）。内置四种实现，另允许工具提供自定义实现：

| 类型 | 字段 | 语义 |
|------|------|------|
| `OneOf` | `Param`, `Source func(ctx, params) ([]string, error)`, `Normalize func(string) string`（可选） | 归一化后的值须在动态候选集内；否则 `keyword=one_of`，附最相近的 3 个候选 |
| `Pattern` | `Param`, `Regex`, `Hint` | 格式校验，`keyword=pattern` |
| `Reject` | `Detect func(params) (reason string, hit bool)`, `Redirect string` | 命中特征即拒绝，`keyword=reject`，`hint` 指向正确工具 |
| `FieldRefs` | `Extract func(params) []FieldRef`, `Fields func(ctx, params) (FieldSet, error)`, `BodyHint func(FieldSet) string` | 抽取参数中的字段引用并与 mapping/表结构比对；未知字段 `keyword=unknown_field`，附候选和正文字段提示 |

参数缺失或为空时，`OneOf`/`Pattern` 不判定（必填由 JSON Schema 负责）。

### 3.4 共享基础设施（`framework/tool`）

- **`suggest.go`**：`Suggest(input string, candidates []string, n int) []string`。排序：大小写不敏感精确匹配 > 前缀 > 子串 > 编辑距离（阈值 `max(2, len/3)`），稳定排序；新规则统一使用。三份私有实现合并编辑距离函数（`tool.Levenshtein`），各自既有的排序规则（工具名、索引模式、ES 字段规范化匹配）保留不动。
- **`schemacache.go`**：
  - ES：键为 `cluster + index pattern`，值为所有匹配索引字段的并集（含类型、keyword 子字段）。
  - 默认 TTL 5 分钟，空结果不缓存；`FieldRefs` 发现未知字段时对该键**强制刷新一次**再判定，避免新增字段被误拒。
  - MySQL 不另建缓存：表结构已由 `metadata.InMemoryStore` 缓存，读取用 `metadata.EnsureSchemaForDatasource`，强制刷新用 `metadata.RefreshFromRegistry`。

### 3.5 错误格式

沿用 `InvalidArgumentsError` 与 `{"ok":false,"tool","error","error_code":"permanent","invalid_arguments":[...]}`。`SchemaError` 新增：

```go
Candidates []string `json:"candidates,omitempty"`
Hint       string   `json:"hint,omitempty"`
```

`Keyword` 新增取值：`one_of`、`pattern`、`reject`、`unknown_field`。`Error()` 文本在每条消息后追加 `(did you mean: a, b, c)` 与 hint。

### 3.6 fail-open

规则约定：`(nil, nil)` 表示通过或不适用（未配置候选源、无 mapper、候选集为空）；`(nil, err)` 表示本应判定但失败（拉取出错、超时，单条上限 2s）。后者跳过该规则、记日志，并在结果 map 上写 `check_skipped`，不拒绝调用。

### 3.7 MCP schema 归一化

注册 MCP 工具时把 `InputSchema`（结构体）经 JSON 往返转为 `map[string]any` 后赋给 `Parameters`。超出支持子集（`format`/`pattern`/`oneOf` 等）的仍按现有规则整体跳过，因此收益仅限于简单 schema；本期不扩展支持子集。

## 4. 零结果探测与 `suspect`

### 4.1 `EmptyProbe`

是否为空统一由 `HitContractFromResult(result) == empty` 判定；诊断写回由 `tool.AttachDiagnosis`/`tool.MarkSuspect` 统一处理 map、`*executor.QueryResult`、`*QuerySpillStub` 三种结果，工具只需提供变体与计数。`Diagnosis` 定义在 `executor` 包（`tool` 依赖 `executor`，避免循环）。

```go
type EmptyProbe struct {
    Relax func(params map[string]any) []ProbeVariant // 有序，越靠前越有信息量；返回空表示不探测
    Count func(ctx context.Context, v ProbeVariant) (int64, error)
}
type ProbeVariant struct {
    Label  string // 如 "time_window_only"、"without:service:foo"
    Params map[string]any
}
type Diagnosis struct {
    Probes    []ProbeCount `json:"probes"`
    Truncated bool         `json:"truncated,omitempty"`
    Hint      string       `json:"hint,omitempty"`
    Errors    []string     `json:"errors,omitempty"`
}
```

中间件负责预算：最多执行 3 个变体，共享超时取 `min(3s, 距工具 deadline 的剩余时间)`，超出即停并置 `truncated`；探测期间触到工具 deadline 时仍返回原结果（无 error），不得把成功的查询变成超时失败。任一变体 count>0 时 `hit_status=suspect`。探测失败只记入 `Errors`，不改变原结果。

- **ES（`es_log_query`）**：
  - 变体依次为"只保留时间窗"（未给 `time_from`/`time_to` 时跳过此项）、"逐个去掉一个顶层 `AND` 子句"、"时间窗放宽到 24h"（仅当 `time_from` 为 `now-<N>s|m|h` 且短于 24h 时生成，避免反而收窄或平移窗口）。
  - `trace_id` 精确查询与 JSON body 查询不探测（`Relax` 返回空）。
  - `Count` 走现有 Reader，`size:0` + `track_total_hits`。
  - 探测在现有 `rewriteEmptyHitQuery` 类型改写重查**之后**仍为 0 条时才执行。
- **SQL（`execute_read`）**：仅当能解析出单一主表时，一个变体 `SELECT COUNT(*) FROM <表>`，判断是表空还是条件不中；否则不探测。`execute_read` 结果为 `*executor.QueryResult`，两者已有 `HitStatus` 字段（`framework/executor/reader.go`、`framework/tool/query_spill.go`），只需在 `QueryResult` 与 `QuerySpillStub` 上新增 `Diagnosis *Diagnosis \`json:"diagnosis,omitempty"\``，由 `Attach` 写入并把 `HitStatus` 置为 suspect。

### 4.2 `hit_status=suspect`

- `evidence.go` 新增 `HitStatusSuspect`，并把它加入 `hitStatusString` 白名单（`evidence.go:88-96`，否则会被清空）；`HitContractFromResult` 能读出该值。
- `evals/runner/trace_summary.go` 已把 `suspect` 计为零结果，无需改。
- 更新文案：`investigate/playbook.go`、`es_log_tool.go` 的工具描述中对 hit_status 取值的说明。

设置条件：

1. 零结果探测显示放宽条件后有数据。
2. `rca_glob`、`search_files` 0 命中且搜索根不存在；`rca_glob` 结果附 `roots`、`roots_missing`，`search_files` 附 `root`、`root_missing`（现有遍历函数不返回扫描数，不另做统计）。
3. 有规则因 fail-open 被跳过且结果为 0 条。

## 5. 逐工具接入

| 工具 | 接入 |
|------|------|
| `execute_read` | `ArgAliases{query: dsl}`；schema 删除 `query`、`index`，`dsl` 设为 required，描述写明"SQL 语句"；`Reject`（**仅当数据源类型为 mysql/hive**；MongoDB 等原生 JSON 查询不受影响）：传入 `index` 参数，或 `dsl` 内容为 Lucene（`field:value`、`AND/OR` 且不以 `SELECT/SHOW/DESC/DESCRIBE/EXPLAIN/WITH` 开头）、JSON DSL（以 `{` 开头）→ hint 指向 `es_log_query`；`OneOf(datasource_id)`，`Normalize` 用 `ResolveDatasourceID`（`"default"` 等别名先解析再判定）；表列校验（仅 mysql）：仅单表 `SELECT ... FROM t [WHERE ...] [ORDER BY ...]`，校验表名与列名，含 JOIN/子查询/CTE 或解析失败一律放行；`EmptyProbe`（主表 COUNT）。修复：有 Reader 而 `cfg.Exec==nil` 时不再报"未配置"。 |
| `es_log_query` | 索引校验沿用现有实现（`es_log_tool.go:186` + `es_log_index.go` 缓存与候选），不另加 `OneOf`；`FieldRefs` 覆盖 query_string 中的 `field:` 引用、`agg_field`、`fields`、`sort`、`time_field`，mapping 来自 `schemacache`，`BodyHint` 给出 message/log 等正文字段名；保留 `rewriteEmptyHitQuery` 的类型改写（字段存在但类型不匹配）；删除"0 条后发现 unknown_fields"分支（执行前已拦截）；`EmptyProbe`。 |
| `jaeger_trace` | `Pattern(trace_id, ^[0-9a-fA-F]{1,32}$)`（jaeger-client 省略前导零，长度不固定）；"trace_id 与 service 至少一个"已有运行时校验，不改。 |
| `rca_*` | `OneOf(repo)`，修正描述中误导的 repo 说明；`rca_read` 路径不存在时，用 `Suggest` 在同目录文件名中给候选，同目录无匹配再在仓库内按文件名匹配（遍历上限 2000 个文件）。 |
| `skill_view` / `load_skill` / `read_skill_file` | `OneOf(name)`，候选来自已加载 skill 索引。 |
| `skill_manage` | 仅 `action` 为非 `create` 时 `OneOf(name)`；其配置当前无 skill 索引，`Source` 取不到时按 fail-open 跳过。 |
| 数据源工具（`describe_table`、`list_tables`） | `OneOf(datasource_id)`，同样先 `ResolveDatasourceID`。 |
| `http_request` | schema `required` 与运行时校验同时去掉 `method`，缺省 GET。 |
| `read_file` | 已有 `similar` 与根目录提示，不改。 |

**C1**：`harness/react_agent.go` 参数 JSON 解析失败时，用 `a.tools.Get(call.Name)` 取 schema，错误中追加必填参数与类型摘要（如 `required: dsl(string), datasource_id(string)`）。`call.Name` 为延迟加载包装工具 `tool_call` 时，先用正则从原始参数中提取内层 `"name"`，提取不到或工具不存在时退化为通用提示（"参数必须是合法 JSON 对象"）。

## 6. harness：`suspect_evidence` stop hook

- 实现为 `BudgetedStopHook`，**独立预算 1 次**、独立前缀，不占用声明式规则共享的 `max_stop_nudges`（`stop_hook.go`），因而同一次 Run 至多提醒一次。
- 触发条件（同时满足）：
  1. 最终回答含否定性断言：没有数据、查不到、未查到、不存在、未发现、0 条、no data、not found、no results 等；复用现有 stop 规则的豁免句检查（如"若查不到请…"不算）。
  2. 本轮 `ToolCallRecord` 中，凡 `HitContractFromResult(rec.Result)` 能读出 hit_status 的结果，全部为 `suspect` 或 `empty`，且至少一个 `suspect`。
- 动作：提醒内容列出各 suspect 结果的工具名、`diagnosis.hint` 或候选。
- 注册点：新增 `harness.DefaultStopHooks()` 返回该 hook。`WithReActStopHooks` 是**替换**而非追加（`stop_hook.go`），因此在 `portal/internal/chat/agent_builder.go`、`evals/runner/live.go`、`investigate/runner.go` 三处都要把默认 hook **合并进同一个 hook 切片后只调用一次**；`agent_builder.go` 中无论是否配置 workspace 都合并（现状仅在有 workspace 时注册 stop hooks）；`investigate/runner.go` 的合并须在 `len(cfg.StopHooks) > 0` 判断之前。`SATH_STOP_SUSPECT=off` 时返回空。

## 7. 可观测性

- `ToolCallRecord` 新增：`hit_status`、`check_rejects[]`（命中的规则 keyword）。`check_skipped` 留在结果 map 中，探测次数由 `diagnosis.probes` 体现，不另设字段。
- `evals/runner/trace_summary.go` 汇总：工具失败率、参数拒绝率（按 keyword 分布）、零结果率、suspect 率；写入 answer_shape 报告 summary（与现有 `tool_error_rate`/`empty_rate` 并列）。

## 8. 测试

- **单元**：
  - 每类规则表驱动（命中 / 未命中 / 参数缺失不判定 / fail-open）；`Suggest` 排序与阈值；`schemacache` TTL 与强制刷新。
  - 中间件：`ArgAliases` 不覆盖正式名；开关每次调用读取；被拒调用产生事件；`checked` 防重复包装（同一工具注册到两个 registry 只探测一次）。
  - 探测预算（3 次、3s 截断、`truncated=true`）；无时间窗时跳过对应变体；`Attach` 写入 `QueryResult`。
  - `HitStatusSuspect` 经 `hitStatusString`/`HitContractFromResult` 保留。
  - suspect hook：触发、豁免句、独立预算一次、不影响声明式规则预算。
  - MCP schema 归一化后 required 生效；C1 回显（普通工具、`tool_call` 包装、未知工具）。
- **工具层**：fake ES / fake DB 覆盖 es_log_query、execute_read 的拒绝、候选、探测与 suspect；execute_read `datasource_id="default"` 仍可用；Reader-only 配置可执行；`query` 别名调用仍可执行。
- **CI eval**：`evals/tasks_ci/answer_shape_ci.jsonl` 新增 3 个夹具任务：用错字段被拒后改正、datasource 写错按候选改正、0 条但探测显示放宽后有数据。

## 9. 验收

1. `go test ./framework/...`、portal 相关包与 `evals/runner` 全部通过。
2. CI eval 无 per-task 回退，新增 3 个任务通过，整体不低于当前基线（39%）。
3. 真实环境 portal 夜跑：工具失败率、零结果率低于 r1 基线，answer_shape 通过率不下降（按路线图 8pt 门禁）。

## 10. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 误拒正常调用（mapping 过期、解析误判） | 未知字段先强制刷新一次；解析不了一律放行；规则失败 fail-open；开关每次调用读取，可秒级回退 |
| 探测增加 ES 负载与延迟 | 只用 count；3 次 / 3s 硬上限；在工具超时之内；工具与全局可关 |
| 通配索引字段并集导致"存在但部分索引没有" | 并集判定只用于拒绝；部分缺失不拒绝，交给探测 |
| suspect hook 引发多余轮次 | 独立预算一次；只在否定性断言且全部依据可疑时触发 |
| 校验移入事件层后事件量增加 | 被拒调用本就是失败调用，计入失败率是期望行为 |

## 11. 不在本期范围

- `memory_recall` 空 query：现状仅 units/files 来源拒绝空 query、transcript 已列最近会话；"返回最近 N 条"需要新的 store 列表接口，留到后续。
- 复杂 SQL（JOIN、子查询、CTE）的字段校验。
- ES JSON DSL body 的字段校验（本期只校验 query_string 与显式字段参数）。
- 扩展 `ValidateArguments` 支持子集（`format`/`pattern`/`oneOf`）。
- 跨工具的自动重试或自动改写。
- portal API 明文返回密钥（另行处理）、台账 stop rule 对简单查询的误触发（阶段 3）。
