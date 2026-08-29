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
| 第一期工具 | `es_log_query`、`execute_read` |
| 阈值（或） | 压缩后行数 **> 50**，或压缩后 JSON **> 8192 字节**（与 `portal/internal/service/chat_stream.go` 的 `toolPayloadFieldLimit` 同量级） |
| 文件 | NDJSON，一行一条记录；单文件 **32MiB** 硬顶 |
| 分页 | 一次工具调用一个文件；`from=continue_from` 再生成新文件，**不**自动拼接 |
| 再处理 | 新工具 `result_stats`（count / group-by / 去重）；统计结果再走同一溢出门 |
| Python | **第一期不开放**。以后若开：只能读 `tmp/results/`，stdout 同样限行 |
| 展示 | 默认摘要 + 样例；气泡不展开 jsonl。下载 UI **第二期**；第一期用户要「全部」时用已有 `ask_user`（下载等到二期 / 确认后按页展开） |
| 写盘失败 | 查询仍成功；**不外置**，退回压缩后的整页结果，可带 `spill_error` |
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
- 会话结束必删文件的新 cron；第一期用写盘时的惰性 TTL（见 §6）。
- 真实 496 条 DiscardUserArchive 的 CI e2e。

### 诚实上限

- 模型仍可能把 sample 当成全集；靠 stub 文案 + `has_more` + 现有翻页闸，不新增 LLM 裁判。
- 写盘失败时行为与今天相同（压缩 hits 进上下文），8KB SSE 截断问题在这条失败路径上仍然存在。
- `extracted_ids` 只覆盖现有 `extractIDsFromHits` 能抽到的字段；更怪的嵌套用 `result_stats` 的点分路径。

---

## 2. 组件

### 2.1 `MaybeSpill`（framework/tool）

查询工具在**已经成功组好压缩后的行列表**之后调用。输入：`ctx`、工具名、行列表（`[]map[string]any`）、以及将返回给模型的 payload（`map[string]any`，已含分页、`extracted_ids`、`hit_status` 等）。

判定用的字节数：对「含完整 hits 的 payload」做 `json.Marshal` 后的长度（`es_log_query` 在 `rcaOK` 之前对内层 payload 测；`execute_read` 对将要序列化给模型的结构测）。行数用压缩后的 `len(hits)` / 行数，不是 ES `total`。

未超阈值：原样返回，payload 仍含 `hits`（或 `execute_read` 仍返回 `*executor.QueryResult`）。

超阈值：把每行写成 jsonl，从 payload **删除** `hits`（`execute_read` 见 §2.4），写入 spill 字段，返回 stub。

`MaybeSpill` 必须可单测：可注入 `workspaceRoot`（测试用临时目录）和时钟；生产从 `ctx.Value(tool.ContextKeyWorkspaceRoot)`、`ContextKeySessionID` 读取。

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

jsonl：UTF-8，每行一个 JSON object，即压缩后的一行（ES hit 或 SQL 行的列名→值）。不含 stub 信封。

单文件 32MiB（`32 << 20`）：写入时累计字节（含换行）达到上限则停止后续行，stub 设 `file_truncated=true` 且 `count` 为**已写入行数**（不是查询返回行数）。查询侧 `has_more` / `truncated` 仍按 ES/SQL 分页，不因文件顶而改写。

### 2.3 Stub 契约

Stub 用 **struct + 显式 `json` 标签**序列化（不要 `map[string]any` 随机键序），保证关键字段出现在序列化结果前部。必有字段：

| JSON 键 | 含义 |
|---------|------|
| `spilled` | 恒为 `true` |
| `path` | 工作区相对路径 |
| `count` | 本文件行数（写入成功的条数） |
| `columns` | 列名；ES 为 sample/首行键的稳定并集（实现可取首行键 + 后续出现的键，顺序稳定即可，测试锁一种：按首次出现顺序） |
| `sample` | 最多 **5** 行，来自文件开头（与 jsonl 前 5 行一致） |
| `hit_status` | 与外置前一致；有行则为 `hits`，不得因删 `hits` 改成 `empty` |
| `queried_index` | `es_log_query` 必有；`execute_read` 仅当入参 `index` 非空（与 evidence spec 一致） |

有则保留、无则省略：`extracted_ids`（**对全页 hits 计算**，再外置）、`has_more`、`continue_from`、`next_from`、`from`、`returned`、`truncated`、`total`（ES 估计总量，可与 `count` 不同）、`unknown_fields`、`similar_fields`、`mapping_error`、`query_rewritten`、`field_hints`、`trace_id`、`file_truncated`、`spill_error`。

**禁止**出现完整 `hits` 数组。`sample` 不是 `hits` 的别名。

`extracted_ids` 仍走现有 `extractIDsFromHits`；在外置前对全页压缩 hits 计算，拷进 stub。

### 2.4 各工具接线

**`es_log_query`**：现有流程不变直到 `compactESLogHits` + `extractIDsFromHits` + 分页字段 + `StampHitContract`。然后 `MaybeSpill`。再 `rcaOK`。失败 / 空击 / mapping 纠错路径**不**调用 Spill（0 行不会超行数阈值；若仅因异常大的 error 文案超 8KB，第一期忽略——纠错 payload 不外置）。

**`execute_read`**：成功且未 spill 时仍返回 `*executor.QueryResult`（现网类型不变）。spill 时改为返回 stub `map[string]any`：含 `spilled`/`path`/`count`/`columns`/`sample`（行转为列名 map）、`truncated`、`hit_status`。`Rows` 不出现在 stub 里。证据盖章：spill 路径对 map 调 `StampHitContract`，与现网 empty/hits 规则一致。

**`result_stats`**：新只读工具，与上述两工具一起注册（有 workspace 的 agent 即可；不依赖 ES）。参数：

| 参数 | 必填 | 含义 |
|------|------|------|
| `path` | 是 | 工作区相对路径，必须在 `tmp/results/` 下 |
| `group_by` | 否 | 点分路径，如 `operation`、`args.flowIds` |
| `unique` | 否 | 点分路径；数组值**展开一层**再去重 |

至少指定 `group_by` 或 `unique` 之一；都缺省则只返回 `{path, count}`（文件行数）。

点分路径：从每行 JSON object 取值；中间缺失则该行跳过该字段。值为 JSON array 时 unique/group-by 把每个元素当一条键（`flowIds: ["a","b"]` → 两个值）。标量 `fmt` 成字符串；object 不当键（该行跳过）。

返回：`count`（输入行数）、`group_by` 时 `groups` 为 `[{value, count}, ...]` 按 count 降序；`unique` 时 `unique_count` + `unique_values`。`groups` / `unique_values` 再走 `MaybeSpill`（阈值同样 50 行 / 8KB）。未 spill 时 `groups`/`unique_values` 最多先截到 200 再测阈值（避免组数上万时先在内存里组完再 marshal 爆掉：实现按流式/单遍扫描文件，组数超过 **10000** 停止并 `groups_truncated=true`，只返回前 200 组 + stub 或未 spill 的截断列表）。

`result_stats` 描述写明：处理溢出文件请用本工具，不要 `read_file` 整份 jsonl。

### 2.5 展示（门户，第一期）

不新增下载按钮。约束：

- tool 结果已经是 stub 时，SSE 与 `WriteStream` 持久化的 content/timeline **不得**再去读 jsonl 填回 hits。
- 前端若按字段渲染 `hits`：`spilled=true` 时只展示 count / sample / path，不因缺 `hits` 而空白报错。
- 用户要看全部：模型用已有 `ask_user` 问「下载（第二期能力，可告知文件已在工作区 path）还是按页在对话里展开」。展开仍受每页 50 行展示上限——展开等于继续 `es_log_query` 翻页或让用户等二期下载，**不**把 jsonl 一次性贴进助手气泡。

第二期（本文不实施）：workspace 文件下载入口，绑定 `path`。

### 2.6 翻页闸

`EvaluateTruncatedPageGate` 继续从最后一次成功的 `es_log_query` 结果 map 读 `has_more`/`truncated` 与 `continue_from`。Stub 必须带这些键。**不要**把 `spilled=true` 当成已查完。

闸的 inject 文案可补一句「上一页已写入 path，用 `result_stats` 做统计/去重，翻页请继续 `from=continue_from`」——允许改文案，逻辑条件不变。

---

## 3. 数据流

```
查询成功 → 压缩 hits → extracted_ids / 分页字段 → StampHitContract
    → 未超阈值：hits 进 tool 消息
    → 超阈值：写 jsonl → 删除 hits → stub 进 tool 消息 / RunTrace / SSE / 落库
模型 →（可选）result_stats(path) → 统计 stub 或小结果
用户要全量且 Q 命中闸 → 继续 es_log_query from=continue_from → 新文件
```

Spill 发生在工具 `Execute` 返回之前，因此：

- 模型当轮看到的 tool result 已是 stub（不只是 SSE 截断）。
- L0/L2 之后也不会再出现该步的完整 hits（除非写盘失败走了回退）。

权威日志仍在 ES/SQL。jsonl 丢失则 `result_stats` 报文件不存在，模型应重新查询，禁止平台静默回 ES。

---

## 4. 失败与回退

| 情况 | 行为 |
|------|------|
| `workspace_root` 空 | 不外置；压缩后整页返回；`spill_error=workspace_root_missing`（仅当本应 spill 时附加，避免小结果也带这个键） |
| `MkdirAll` / 写文件失败 | 不外置；整页返回；`spill_error` 为简短错误类名（不要把整段 OS 错误里的绝对路径塞给模型） |
| jsonl 达到 32MiB | 停止写入；stub `file_truncated=true`；`count`=已写行数；`sample` 仍最多 5 行；查询分页字段不变 |
| `result_stats` 路径不在 `tmp/results/` 或逃出 workspace | 返回错误 payload（`ok=false` 或 Go error，与同目录只读工具一致：**明确错误字符串**，不读盘） |
| 文件不存在或不是文件 | `file not found; re-query` 类错误；不打 ES |
| 损坏 jsonl（某行非法 JSON） | 跳过坏行并在结果里给 `skipped_bad_lines`；其余行继续。全部坏则错误 |
| 统计结果超阈值 | 再写一个 jsonl + stub，不得把完整 `unique_values` 送回模型 |
| 查询本身失败 | 不 Spill；现有 error / mapping 路径不变 |

---

## 5. 生命周期

- jsonl 不是记忆、不是知识库，不进 memory index。
- 第一期清理：**每次成功写入** `tmp/results/` 时，扫描该 session 目录（及可选的整个 `tmp/results/`）删除 **mtime 超过 24h** 的文件。失败只记日志，不影响本次 Spill。
- 不在第一期做「会话结束立刻 rm -rf」。

---

## 6. 测试

不连真实 ES。夹具用假 Reader / 临时 workspace。

### `MaybeSpill` / `es_log_query`

- 行数 ≤ 50 且 marshal ≤ 8192：无文件、无 `spilled`、有 `hits`。
- 行数 > 50：有 jsonl，行数=`count`，stub 无 `hits`，`sample`≤5，`extracted_ids` 与全页抽取一致（构造 `args.flowIds` 只出现在第 6 行之后，断言 stub 仍含该 id）。
- 行少但每行很大导致 marshal > 8192：仍 spill。
- 压缩后低于阈值：不外置（hits 含已 drop 字段被去掉后变小）。
- 无 workspace / 注入写失败：有 `hits`，有 `spill_error`，无 `spilled`。
- 路径守卫：workspace 外写不到。
- 32MiB：可用短行重复写到上限；断言 `file_truncated` 与 `count`。
- stub JSON 字节的前 2048 字节含 `spilled`、`path`、`count`（回归 8KB 截断看不到元数据）。

### 闸

- 最后一次 `es_log_query` 结果为 spilled stub 且 `has_more=true`、`continue_from=50`：`EvaluateTruncatedPageGate` 仍 inject，`from` 为 50。
- `spilled=true` 且 `has_more` 缺省/false：不因 spilled 误 inject。

### `result_stats`

- `group_by=operation` 计数正确。
- `unique=args.flowIds` 展开数组、去重。
- 路径 `../` 或 `tmp/other/`：拒绝。
- 缺文件：错误，且假 Reader 的 Query 调用次数为 0。
- 唯一值 > 50 或体积 > 8KB：再 spill，返回无超长 `unique_values`。

### `execute_read`

- 未 spill：仍是 `*executor.QueryResult`。
- 行数 > 50：返回 map stub，无 `Rows`/`hits` 全表。
- ES datasource 仍拒绝（现有测试不删）。

### 门户

- SSE 截断函数作用在 spilled stub 上：截断后仍含 `path`（stub 远小于 8KB 时整段保留）。
- 持久化 timeline 的 tool 节点 content 不含完整 hits 数组（可用 60 行假结果走聊天夹具，断言落库字符串无第 6 行之后的唯一 marker）。

### 不进 CI

- 手工：查全部 DiscardUserArchive 一类任务，上下文无整页 hits，仍能 `result_stats` + 翻页 nudge。

---

## 7. 实现落点（给 plan 用，不是本期写代码）

| 单位 | 职责 |
|------|------|
| `framework/tool/query_spill.go` | 阈值常量、路径守卫、写 jsonl、`MaybeSpill`、TTL 惰性删 |
| `framework/tool/query_spill_test.go` | 溢出门与回退 |
| `framework/tool/result_stats.go` | `result_stats` 注册与扫描 |
| `framework/tool/es_log_tool.go` | 成功路径接 `MaybeSpill` |
| `framework/tool/data/execute_read.go` | 成功路径接 `MaybeSpill` |
| `framework/agent/truncated_page_gate.go` | 仅文案可选；断言 stub 字段可读即可 |
| Portal SSE | 仅当现有渲染假定必有 `hits` 时补 spilled 分支；不新增下载 API |

注册：`result_stats` 在已注册 workspace 文件工具的同一路径挂上（Portal 组 registry 处），避免无 workspace 的 agent 拿到只会失败的工具。

---

## 8. 成功标准

同一类「结果太多」任务：模型上下文与落库看不到整页查询 hits；stub 能翻页、能 `result_stats` 做分布/去重；写盘失败时功能不低于改前。
