# 查询结果外置（Spill）

**日期**: 2026-08-29  
**状态**: 已确认（brainstorming 分段批准）  
**方案**: A — 平台自动把过大查询结果写成工作区临时文件，模型与落库只看 stub  
**关联**: [session-context-compression](../../../framework/docs/superpowers/specs/2026-05-26-session-context-compression-design.md)（L0/L2 管旧消息，不管单次查询塞爆）；[industrial-evidence](./2026-08-25-industrial-evidence-design.md)（`hit_status` / `queried_index` 必须在 stub 上保留）  
**禁止**: 第一期执行 Python/任意脚本；多页自动拼成一个文件；`http_request` / 终端 stdout / 助手长回复走溢出；改 ES mapping 纠错、空击盖章、`execute_read` 禁查 ES；把 jsonl 当权威数据源。

**一句话**：查询工具成功且结果过大时，全量只进 `{workspace}/tmp/results/` 的 jsonl；进模型上下文、RunTrace、SSE、消息落库的是固定 stub；再处理用只读 `result_stats`。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 谁决定外置 | **平台**，不靠模型先 `write_file` |
| 第一期工具 | `es_log_query`、`execute_read`；再处理 `result_stats` |
| 阈值（或） | 压缩后行数 **> 50**，或压缩后 JSON **> 8192 字节**（与 `portal/internal/service/chat_stream.go` 的 `toolPayloadFieldLimit` 同量级） |
| 默认页 | `es_log_query` 默认 `limit=50`，行数阈值通常不触发，**默认页主要靠 8KB** |
| 文件 | NDJSON，一行一条记录；单文件 **32MiB** 硬顶 |
| 分页 | 一次工具调用一个文件；`from=continue_from` 再生成新文件，**不**自动拼接 |
| 再处理 | `result_stats`：`group_by` 与 `unique` **互斥**（见 §2.4）；统计过大则另写 jsonl |
| Python | **第一期不开放**。以后若开：只能读 `tmp/results/`，stdout 同样限行 |
| 展示 | 默认摘要 + 样例；气泡不展开 jsonl。下载 UI **第二期**；第一期用户要「全部」时用已有 `ask_user` |
| 写盘失败 | 查询仍成功；**不外置**，退回压缩后的整页结果，可带 `spill_error` |
| Execute 返回类型（spill） | `*QuerySpillStub`（struct），**不**经 `rcaOK` 再包一层 `map[string]any` |
| 与 L2 | 互补：L2 剪旧对话；Spill 管本步 tool 结果 |

---

## 1. 目标与非目标

### 目标

1. 单次 `es_log_query` / `execute_read` 成功结果超过阈值时，模型当轮 tool 消息里**没有**完整 `hits` / `Rows`。
2. Stub 含足够信息让模型继续工作：`path`、`count`、`columns`、最多 5 行 `sample`、全页（不是 sample）算出的 `extracted_ids`、以及现有分页字段 `has_more` / `continue_from`（或 `execute_read` 的 `truncated`）。
3. `truncated_page_gate` 在 spilled stub 上仍能读到 `has_more`/`truncated` + `continue_from`，用户要全量时继续 inject 翻页。
4. `result_stats` 只读工作区 `tmp/results/` 下的文件，做行数、按点分路径 group-by、抽唯一值；路径逃逸拒绝。
5. 时间线 / SSE / 刷新后再加载的 tool 内容都是 stub，不会把 jsonl 再灌进上下文。

### 非目标（第一期不做）

- Python / shell 跑用户脚本处理文件。
- 多页 jsonl 自动 concat 成「全量结果集」。
- `http_request`、terminal、`read_file` 输出、助手 markdown 表格的溢出。
- 门户「下载 jsonl」按钮或独立下载 API（第二期）。
- 改 `read_file` 硬拦 `tmp/results/`（第一期只在 `es_log_query` / `result_stats` 描述里写：不要 `read_file` 该 jsonl，用 `result_stats`）。`read_file` 现有 ~100K 字符上限保留。
- 会话结束必删文件的新 cron；第一期用写盘时的惰性 TTL（见 §5）。
- 真实 496 条 DiscardUserArchive 的 CI e2e。

### 诚实上限

- 模型仍可能把 sample 当成全集；靠 stub 文案 + `has_more` + 现有翻页闸，不新增 LLM 裁判。
- 写盘失败时行为与今天相同（压缩 hits 进上下文），8KB SSE 截断问题在这条失败路径上仍然存在。
- `extracted_ids` 只覆盖现有 `extractIDsFromHits` 能抽到的字段；更怪的嵌套用 `result_stats` 的点分路径。

---

## 2. 组件

### 2.1 `MaybeSpill`（framework/tool）

查询工具在**已经成功组好压缩后的行列表**之后调用。

输入：`ctx`、工具名、行列表（`[]map[string]any`）、元数据（分页、`extracted_ids`、`hit_status`、`queried_index` 等，见 §2.3）。

判定：

- 行数 = `len(rows)`（压缩后），**不是** ES `total`。
- 字节数 = `json.Marshal`「若把这些行放进 `hits` 后的查询 payload」的长度（未 spill 时 `es_log_query` 仍走今天的 map + `rcaOK`，测的是内层含 `hits` 的 map）。

未超阈值：不改返回类型。`es_log_query` 仍 `rcaOK(map)` 且含 `hits`；`execute_read` 仍 `*executor.QueryResult`。非溢出路径的 `count` **保持现网**（`es_log_query` 现网 `count` 等于 ES `total`，不要顺手改成 `len(hits)`）。

超阈值：把**全部** `rows` 写入 jsonl（受 32MiB 截断），返回 `*QuerySpillStub`。Stub **没有** `hits`。`count` = 已写入文件的行数。

`MaybeSpill` 必须可单测：可注入 `workspaceRoot` 和时钟；生产从 `ctx.Value(tool.ContextKeyWorkspaceRoot)`、`ContextKeySessionID` 读取。

### 2.2 文件布局

相对工作区（stub 的 `path` 用正斜杠）：

```
tmp/results/{session_id}/{unix_ms}_{tool}_{n}.jsonl
```

- `session_id`：`ContextKeySessionID`；空则 `_nosession`。只保留 `[A-Za-z0-9._-]`，其余替换为 `_`。
- `unix_ms`：写入时刻的 Unix 毫秒。
- `n`：进程内原子计数，避免同毫秒碰撞。
- `tool`：`es_log_query` / `execute_read` / `result_stats`。
- 目录不存在则 `MkdirAll`。
- 路径必须 `filepath.Abs` 后仍落在 `{workspace}/tmp/results/` 下；写盘与 `result_stats` 共用同一守卫（可复用 `file_tools` 的 workspace join，但**额外**要求相对路径前缀为 `tmp/results/`）。

查询 spill 的 jsonl：UTF-8，每行一个 JSON object，即压缩后的一行（ES hit 或 SQL 行的列名→值）。不含 stub 信封。

`result_stats` spill 的 jsonl 见 §2.4（一行一个统计项，不是原始 hit）。

单文件 32MiB（`32 << 20`）：写入时累计字节（含换行）达到上限则停止后续行，stub 设 `file_truncated=true` 且 `count` 为**已写入行数**。查询侧 `has_more` / `truncated` 仍按 ES/SQL 分页，不因文件顶而改写。

### 2.3 Stub 契约与 Execute 类型

Go 类型名：`QuerySpillStub`。`Execute` 在 spill 路径返回 `*QuerySpillStub`。

**不要**再调用 `rcaOK` / `NormalizeRCAResult` 把 stub 塞进 `map[string]any`。`json.Marshal(map)` 按键名排序，`extracted_ids` / `sample` 会排到 `spilled`/`path` 前面，少行大 payload 时 SSE 8KB 会先切掉元数据。

`encoding/json` 对 struct 按**字段声明顺序**输出。字段必须按下面顺序声明（`omitempty` 用于可选字段）：

| 顺序 | JSON 键 | 必有 | 含义 |
|------|---------|------|------|
| 1 | `spilled` | 是 | 恒为 `true` |
| 2 | `path` | 是 | 本次写入的工作区相对路径（统计溢出时是**统计文件**，见 §2.4） |
| 3 | `count` | 是 | 该 `path` 文件已写入行数 |
| 4 | `ok` | 是 | `true`（替代 `rcaOK` 的信封） |
| 5 | `hit_status` | 查询 spill 是 | 与外置前一致；有行则为 `hits`，不得因删 `hits` 改成 `empty` |
| 6 | `queried_index` | `es_log_query` 是 | `execute_read` 仅当入参 `index` 非空 |
| 7 | `has_more` | 若有 | 与外置前一致 |
| 8 | `continue_from` | 若有 | 与外置前一致 |
| 9 | `next_from` / `from` / `returned` / `truncated` / `total` | 若有 | `total` 仍是 ES 估计总量，可与 `count` 不同 |
| 10 | `columns` | 查询 spill 是 | 按行内键**首次出现**顺序的并集 |
| 11 | `extracted_ids` | 若有 | **对全页 hits** 调用现有 `extractIDsFromHits`，再外置 |
| 12 | `evidence_refs` | `es_log_query` spill 是 | 与 `rcaOK`→`deriveESLogRefs` 等价：`Kind=es_log_query`；有 `trace_id` 则带上。有写入行则**不要** `Summary=no hits`（禁止因 stub 无 `hits` 键当成空击） |
| 13 | `source_path` | 统计 spill 是 | 被统计的原始查询 jsonl（`path` 此时指向统计文件） |
| 14 | `unique_count` | unique 统计是 | 去重后的个数（文件被 32MiB 截断时仍报已写入条数对应的个数） |
| 15 | `groups_truncated` | 若触发 10000 帽 | 见 §2.4 |
| 16 | `file_truncated` | 若 32MiB | |
| 17 | `sample` | 是 | 最多 5 行；放在**后部**，使 8KB 截断先丢掉 sample 而不是 path |
| 18 | 其余可选 | 若有 | `unknown_fields`、`similar_fields`、`mapping_error`、`query_rewritten`、`field_hints`、`trace_id`、`spill_error`、`skipped_bad_lines` |

**禁止**出现完整 `hits`、`Rows`、`groups`、`unique_values`。`sample` 不是这些数组的别名。

`sample` 来自本次写入文件的前 5 行。单行 `json.Marshal` 超过 **512 字节**则该行再截成合法 JSON 字符串摘要（实现可改为只留若干标量键）；目的是 stub 在含 sample 时仍尽量 < 8KB。测试锁：前 2048 字节必须出现 `spilled`、`path`、`count`。

读取 stub 字段（闸、证据）**禁止**只 `rec.Result.(map[string]any)`。提供 `SpillFields(v any)`（或等价）：接受 `*QuerySpillStub` 与 `map[string]any`，返回 `has_more` / `truncated` / `continue_from` / `hit_status` / `queried_index`。`EvaluateTruncatedPageGate` 与 `HitContractFromResult` 改走这个 helper。

`CollectEvidenceRefs` 必须 type switch `*QuerySpillStub`，读取 `evidence_refs`。否则 EvidenceGate 会把大结果外置当成「没查过 ES」，误 inject「请先用 es_log_query」。`execute_read` / `result_stats` 的 stub **不**要求 `evidence_refs`。

### 2.4 各工具接线

**`es_log_query`**：现有流程不变直到 `compactESLogHits` + `extractIDsFromHits` + 分页字段 + `StampHitContract`。然后 `MaybeSpill`。未 spill：再 `rcaOK(map)`。spill：返回 `*QuerySpillStub`（含 `ok=true`、盖章字段、以及与 `deriveESLogRefs` 等价的 `evidence_refs`），**跳过** `rcaOK`。失败 / 空击 / mapping 纠错路径**不**调用 Spill。

**`execute_read`**：成功且未 spill 时仍返回 `*executor.QueryResult`。spill 时返回 `*QuerySpillStub`（不是 map）：`sample` 为列名 map；`Rows` 不出现。`hit_status` 与现网 empty/hits 规则一致（写在 stub 上，不必再 `StampHitContract` 进 map；可抽公共赋值）。

**`result_stats`**：新只读工具，在已注册 workspace 文件工具的同一路径挂上。参数：

| 参数 | 必填 | 含义 |
|------|------|------|
| `path` | 是 | 工作区相对路径，必须在 `tmp/results/` 下 |
| `group_by` | 否 | 点分路径，如 `operation`、`args.flowIds` |
| `unique` | 否 | 点分路径；数组值**展开一层**再去重 |

**互斥**：`group_by` 与 `unique` 不得同时出现；同时出现则永久错误（不读盘）。两者都缺省：只返回 `{path, count}`（输入文件行数），不写新文件。

点分路径：从每行 JSON object 取值；中间缺失则该行跳过该字段。值为 JSON array 时把每个元素当一条键（`flowIds: ["a","b"]` → 两个值）。标量格式化为字符串；object 不当键（该行跳过）。

聚合（单遍扫文件）：

| 帽 | 值 | 作用 |
|----|----|------|
| 内存键数 | **10000** | 即将加入第 10001 个不同键时设 `groups_truncated=true`（unique 路径同一字段名），**停止读文件**；已见键的计数以停止前为准 |
| 模型内联 | **50 项且 marshal ≤ 8192** | 不超过则结果里带完整 `groups` 或 `unique_values`，`spilled` 缺省 |
| spill 写盘 | **内存里的全部键**（≤10000） | 不是只写 200 条。496 个 unique 应整份进统计 jsonl |

`group_by` 未 spill 时：`count` = 输入行数，`groups` = `[{value, count}, ...]` 按 count 降序。  
`unique` 未 spill 时：`count` = 输入行数，`unique_count`、`unique_values`（字符串列表，顺序：首次出现）。

spill 时删除 `groups` / `unique_values`，返回 `*QuerySpillStub`：

- `path` = **新**统计 jsonl
- `source_path` = 入参那个查询 jsonl
- `count` = 统计 jsonl 行数
- `unique_count`：仅 `unique` 模式，等于去重个数（与文件行数相同，除非 32MiB 截断）
- `sample`：统计 jsonl 前 5 行
- 无 `hits`

统计 jsonl 一行（写入顺序锁定，禁止 `range map` 导致测试抖）：

- `group_by`：`{"value":"<key>","count":N}`，按 **count 降序**（与未 spill 的 `groups` 一致）
- `unique`：`{"value":"<key>"}`，按 **首次出现** 顺序

`result_stats` 描述写明：处理溢出文件请用本工具，不要 `read_file` 整份 jsonl。

### 2.5 展示（门户，第一期）

不新增下载按钮。约束：

- tool 结果已经是 stub 时，SSE 与 `WriteStream` 持久化的 content/timeline **不得**再去读 jsonl 填回 hits。
- 前端若按字段渲染 `hits`：`spilled=true` 时只展示 count / sample / path，不因缺 `hits` 而空白报错。
- 用户要看全部：模型用已有 `ask_user` 问「下载（第二期能力，可告知文件已在工作区 path）还是按页在对话里展开」。展开仍受每页 50 行展示上限——展开等于继续 `es_log_query` 翻页或让用户等二期下载，**不**把 jsonl 一次性贴进助手气泡。

第二期（本文不实施）：workspace 文件下载入口，绑定 `path`。

### 2.6 翻页闸

`EvaluateTruncatedPageGate` 经 `SpillFields`（§2.3）读取最后一次成功的 `es_log_query` 结果。Stub 必须带 `has_more`/`continue_from`。**不要**把 `spilled=true` 当成已查完。

闸的 inject 文案可补一句「上一页已写入 path，用 `result_stats` 做统计/去重，翻页请继续 `from=continue_from`」——允许改文案，逻辑条件不变。

---

## 3. 数据流

```
查询成功 → 压缩 hits → extracted_ids / 分页字段 → StampHitContract
    → 未超阈值：hits 进 tool 消息（类型与现网相同）
    → 超阈值：写 jsonl → Execute 返回 *QuerySpillStub
模型 →（可选）result_stats(path)
    → 未超阈值：groups / unique_values 内联
    → 超阈值：写统计 jsonl → *QuerySpillStub（path=统计文件，source_path=原查询文件）
用户要全量且 Q 命中闸 → 继续 es_log_query from=continue_from → 新文件
```

Spill 发生在工具 `Execute` 返回之前，因此：

- 模型当轮看到的 tool result 已是 stub（不只是 SSE 截断）。`toolMessageContent` 对 `result` 做 `json.Marshal` 时，嵌套 struct 保持 §2.3 字段序。
- L0/L2 之后也不会再出现该步的完整 hits（除非写盘失败走了回退）。

权威日志仍在 ES/SQL。jsonl 丢失则 `result_stats` 报文件不存在，模型应重新查询，禁止平台静默回 ES。

---

## 4. 失败与回退

| 情况 | 行为 |
|------|------|
| `workspace_root` 空 | 不外置；压缩后整页返回；`spill_error=workspace_root_missing`（仅当本应 spill 时附加） |
| `MkdirAll` / 写文件失败 | 不外置；整页返回；`spill_error` 为简短错误类名（不要把 OS 绝对路径塞给模型） |
| jsonl 达到 32MiB | 停止写入；stub `file_truncated=true`；`count`=已写行数；`sample` 仍最多 5 行；查询分页字段不变 |
| `result_stats` 路径不在 `tmp/results/` 或逃出 workspace | 明确错误，不读盘 |
| `group_by` 与 `unique` 同时出现 | 明确错误，不读盘 |
| 文件不存在或不是文件 | `file not found; re-query` 类错误；不打 ES |
| 损坏 jsonl（某行非法 JSON） | 跳过坏行并在结果里给 `skipped_bad_lines`；其余行继续。全部坏则错误 |
| 统计结果超阈值 | 统计 jsonl 写入内存中的全部键（≤10000），stub 无 `unique_values`/`groups` |
| 查询本身失败 | 不 Spill；现有 error / mapping 路径不变 |

回退路径（未 spill）仍返回 map / `*QueryResult`，可把 `spill_error` 写在 map 上；不引入 stub struct。

---

## 5. 生命周期

- jsonl 不是记忆、不是知识库，不进 memory index。
- 第一期清理：**每次成功写入**后，只扫描**当前 session 目录** `tmp/results/{session_id}/`，删除 **mtime 超过 24h** 的文件。不扫整个 `tmp/results/`。失败只记日志，不影响本次 Spill。
- 不在第一期做「会话结束立刻 rm -rf」。

---

## 6. 测试

不连真实 ES。夹具用假 Reader / 临时 workspace。

### `MaybeSpill` / `es_log_query`

- 行数 ≤ 50 且 marshal ≤ 8192：无文件、无 `spilled`、有 `hits`；返回类型仍为 map（经 `rcaOK`）；`count` 仍等于 `total`（现网）。
- 行数 > 50：返回 `*QuerySpillStub`，jsonl 行数=`count`，无 `hits`，`sample`≤5，`extracted_ids` 与全页抽取一致（构造 `args.flowIds` 只出现在第 6 行之后，断言 stub 仍含该 id）。
- 行少但每行很大导致 marshal > 8192：仍 spill。
- 压缩后低于阈值：不外置。
- 无 workspace / 注入写失败：有 `hits`，有 `spill_error`，无 `spilled`，类型仍为 map。
- 路径守卫：workspace 外写不到。
- 32MiB：短行重复写到上限；断言 `file_truncated` 与 `count`。
- `json.Marshal(stub)` 的前 2048 字节含 `"spilled"`、`"path"`、`"count"`。
- `es_log_query` spill：`CollectEvidenceRefs(stub)` 含 `Kind=es_log_query`；EvidenceGate 不得因 spill 误判缺证据。

### 闸

- 最后一次结果为 `*QuerySpillStub` 且 `has_more=true`、`continue_from=50`：仍 inject `from=50`。
- 最后一次结果为含同样键的 `map[string]any`：行为不变（回归）。
- `spilled=true` 且 `has_more` 缺省/false：不因 spilled 误 inject。

### `result_stats`

- `group_by=operation` 计数正确；未 spill 时结果含 `groups`，`path` 为入参路径。
- `unique=args.flowIds` 展开数组、去重。
- 同时传 `group_by` 与 `unique`：错误，不读文件。
- 路径 `../` 或 `tmp/other/`：拒绝。
- 缺文件：错误，且假 Reader 的 Query 调用次数为 0。
- 80 个 unique：spill；统计 jsonl **80 行**（不是 200）；stub 无 `unique_values`；有 `unique_count=80`、`source_path`、`path` 指向统计文件。
- 10001 个不同 `group_by` 键：`groups_truncated=true`，统计文件最多 10000 行。

### `execute_read`

- 未 spill：仍是 `*executor.QueryResult`。
- 行数 > 50：`*QuerySpillStub`，无 `Rows`。
- ES datasource 仍拒绝（现有测试不删）。

### 门户

- SSE `truncateField` 作用在 stub 上：即使截断，字符串仍含 `path`（把 sample 做大以逼近 8KB 时，前部键仍在）。
- 持久化 timeline 的 tool 节点 content 不含完整 hits（60 行假结果，落库字符串无第 6 行之后的唯一 marker）。

### 不进 CI

- 手工：查全部 DiscardUserArchive 一类任务，上下文无整页 hits，仍能 `result_stats` + 翻页 nudge。

---

## 7. 实现落点（给 plan 用，不是本期写代码）

| 单位 | 职责 |
|------|------|
| `framework/tool/query_spill.go` | 阈值常量、`QuerySpillStub`、路径守卫、写 jsonl、`MaybeSpill`、`SpillFields`、TTL（仅 session 目录） |
| `framework/tool/query_spill_test.go` | 溢出门与回退 |
| `framework/tool/result_stats.go` | `result_stats` 注册与扫描 |
| `framework/tool/es_log_tool.go` | 成功路径接 `MaybeSpill`；spill 时不 `rcaOK` |
| `framework/tool/data/execute_read.go` | 成功路径接 `MaybeSpill` |
| `framework/tool/evidence.go` | `HitContractFromResult` 与 `CollectEvidenceRefs` 识别 `*QuerySpillStub`；spill 合成 `evidence_refs` |
| `framework/agent/truncated_page_gate.go` | 改走 `SpillFields`；文案可选 |
| Portal SSE | 仅当现有渲染假定必有 `hits` 时补 spilled 分支；不新增下载 API |

---

## 8. 成功标准

同一类「结果太多」任务：模型上下文与落库看不到整页查询 hits；stub 能翻页、能 `result_stats` 做分布/去重；写盘失败时功能不低于改前。
