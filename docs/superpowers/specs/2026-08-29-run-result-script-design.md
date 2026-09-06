# 编程兜底：`run_result_script`

**日期**: 2026-08-29  
**状态**: 已确认（brainstorming 分段批准）  
**方案**: 通用兜底能力，第一期权限收在工作区 `tmp/results/`；无合法数据文件不起解释器  
**关联**: [查询结果外置](./2026-08-29-query-result-spill-design.md)（目录、路径守卫、`QuerySpillStub`、50 行 / 8KB / 32MiB）  
**禁止**: 第一期做 OS 沙箱、确认卡、Node/shell、工作区外读、替代 `terminal` / `execute_skill_script`、LLM 裁判「是否该先用现成工具」

**一句话**：现成查询工具和 `result_stats` 之后，模型可以对已 spill 的文件跑一段 Python；大 stdout 再外置，上下文只看 stub。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 产品定位 | 通用编程兜底；第一期只能处理 `tmp/results/` 下已有文件 |
| 硬闸 | 必须带合法 `path`（已存在、在 `tmp/results/` 下）；没有则不起 Python |
| 软引导 | 工具描述：先查询工具，再 `result_stats`，最后本工具。无 LLM 裁判 |
| 语言 | 仅 Python 3（`python`，找不到再试 `python3`） |
| 确认卡 | 不弹 |
| 脚本入口 | `code` **或** `script_path`，互斥 |
| stdout | 与查询 spill 同一阈值：行数 **> 50** 或字节 **> 8192** → 写 jsonl，返回 `*QuerySpillStub` |
| 超时 | **15s**，`CommandContext` 杀进程 |
| 沙箱 | 第一期不是 OS 隔离；靠参数守卫 + cwd + 超时 + 不装包 |

---

## 1. 目标与非目标

### 目标

1. 没有 `{workspace}/tmp/results/` 下已存在的数据文件时，`run_result_script` 不起解释器。
2. 有该文件时，模型可用内联 `code` 或已有 `.py`（亦须在 `tmp/results/`）做 `result_stats` 做不到的转换。
3. 脚本 stdout 过大时，当轮 tool 消息里没有完整输出，只有 stub（`path` / `count` / `sample` / `source_path` / `exit_code`）。
4. 不替代 `terminal`、`execute_skill_script`、`result_stats`。

### 非目标（第一期不做）

- seccomp / 只读文件系统 / 禁网络的真正沙箱。CPython 仍可能 `open()` 其它路径；不把「故意读 C:\」写成失败测试。
- 确认卡、pip 安装、Node、shell、stdin 入参。
- 无 spill 文件时用代码去替 `es_log_query` / `execute_read`。
- 会话结束立刻删除 `.py` / stdout jsonl（沿用 spill 规格的 24h 惰性 TTL，写盘成功后扫当前 session 目录）。
- 门户下载按钮或新 UI。
- 与 `terminal` 的硬互斥闸。

### 诚实上限

- 「先用现成工具」只靠描述；模型仍可能对本可用 `result_stats` 的任务写 Python。
- 参数守卫挡不住脚本内部乱读盘或打网络。

---

## 2. 组件

### 2.1 工具：`run_result_script`

注册：`RegisterWorkspaceFileToolsWithConfig`（与 `result_stats` 同一路径）。不依赖 `skills.allow_script_execution`。

描述必须写明优先级：查询工具 → `result_stats`（count / group-by / unique）→ 本工具；`path` 必须是已 spill 的工作区相对路径；用 `sys.argv[1]` 打开数据文件，不要 `read_file` 整份 jsonl。

| 参数 | 必填 | 规则 |
|------|------|------|
| `path` | 是 | 数据文件。工作区相对路径；`filepath.Abs` 后必须落在 `{workspace}/tmp/results/` 下；必须是已存在的普通文件。与 `result_stats` 共用路径守卫。 |
| `code` | 与 `script_path` 二选一 | UTF-8 Python 源码；长度 **> 64KiB**（65536 字节）拒绝。 |
| `script_path` | 与 `code` 二选一 | 工作区相对路径；必须在 `tmp/results/` 下；扩展名必须是 `.py`；必须已存在。 |

`code` 与 `script_path` 都给或都缺：永久错误，不起解释器。

有 `code` 时写入：

```
tmp/results/{session_id}/{unix_ms}_run_result_script_{n}.py
```

`session_id` / `unix_ms` / `n` 与 spill 规格 §2.2 相同。禁止用这段写入覆盖入参 `path` 指向的文件（`n` 与工具名后缀已保证与 jsonl 文件名不同；若仍碰撞则换下一个 `n`）。

### 2.2 进程

- 解释器：`exec.LookPath("python")`，失败再 `LookPath("python3")`；都没有则 Execute error（文案说明需要本机 Python 3），不起其它命令。
- 命令：`python <脚本绝对路径> <数据文件绝对路径>`。`sys.argv[1]` 必须是数据文件的 OS 绝对路径（不用工作区相对路径：cwd 不在 workspace 根时 `open(相对path)` 会失败）。
- `cmd.Dir`：数据文件所在目录（已保证在 `tmp/results/` 下）。相对 `open("foo")` 落在该目录；不把 `workspace_root` 塞进环境变量。
- 超时：**15s**。继承 PATH；设置 `PYTHONNOUSERSITE=1`。不设 stdin。stdout 与 stderr **合并**（与 `execute_skill_script` 相同）。
- 不跑 pip，不改 `PYTHONPATH`。

### 2.3 stdout → 行 → 可选 spill

对合并输出：按 `\n` 拆行；去掉因末尾换行产生的最后一段空串；**其余空行丢弃**。得到 `lines []string`。

规范化成 `[]map[string]any`：

- 若每个元素都能 `json.Unmarshal` 成 JSON **object**（`map[string]any`），则行内容原样作为 jsonl 记录（便于再 `result_stats`）。
- 否则每一行变成 `{"line":"<原文>"}`。某几行是 JSON、某几行不是：整份走 `line` 包装，不混写。

阈值与查询 spill 相同（行数 = `len(lines)`，字节 = `json.Marshal` 这些行后的长度，或等价地 marshal 将写入文件的对象切片）。`len(lines)==0`：**不** spill；返回短 map，`output` 为空字符串。

超阈值：写入

```
tmp/results/{session_id}/{unix_ms}_run_result_script_{n}.jsonl
```

单文件 32MiB 规则与 spill 规格相同。Execute 返回 `*QuerySpillStub`：

- 字段序仍遵守 spill 规格 §2.3（`spilled` / `path` / `count` / `ok` 在前，`sample` 靠后）。
- `path`：stdout jsonl。
- `source_path`：入参数据文件的工作区相对路径。
- `count`：已写入行数。
- `sample`：最多 5 行（规范化后的 object）。
- `exit_code`：进程退出码（超时且无 Wait 码时用 `-1`）。
- `timed_out`：仅超时时为 `true`（`omitempty`）。
- 无 `hits`。不要求 `evidence_refs`。

未超阈值：返回 `map[string]any`（不是 stub）：

| 键 | 含义 |
|----|------|
| `ok` | `true`（含非 0 退出；失败靠 `exit_code`） |
| `exit_code` | 进程退出码 |
| `timed_out` | 仅超时 |
| `path` | 入参数据文件（工作区相对路径） |
| `line_count` | `len(lines)` |
| `output` | 合并输出原文（规范化前）。空输出则为 `""` |
| `spill_error` | 仅当本应 spill 但写盘失败、改为截断内联时 |

非 0 退出：**不是** `Execute` 的 `error`。超时但已有输出：按上面内联或 spill，并设 `timed_out`。超时且无输出：`Execute` error。

### 2.4 与 `QuerySpillStub` 的字段

在 spill 规格 §2.3 可选区增加（`omitempty`）：

- `exit_code`：仅 `run_result_script` 溢出路径需要；`result_stats` / 查询 stub 不设。
- `timed_out`：同上。

`source_path` 已存在（统计 spill）。本工具溢出时必填。

---

## 3. 数据流

```
es_log_query / execute_read 成功且过大 → jsonl + stub
    →（可选）result_stats(path)
    → run_result_script(path, code|script_path)
        → 校验失败：error，无进程
        → 跑 Python
        → 小输出：map{output,...}
        → 大输出：stdout jsonl + *QuerySpillStub
    → 可对 stdout jsonl 再 result_stats 或再 run_result_script
```

Spill 在 `Execute` 返回前完成。SSE / timeline / 落库与查询 spill 相同：只持久化 stub 或短 map。stdout jsonl 不是权威数据源；丢失则重新跑脚本，不回打 ES。

权威查询数据仍在 ES/SQL。`.py` 与 stdout jsonl 适用 spill 规格 §5 的 session 目录 24h TTL（本工具成功写 `.py` 或 jsonl 后同样触发该扫描）。

---

## 4. 失败与回退

| 情况 | 行为 |
|------|------|
| `workspace_root` 空 | error `workspace_root_missing`；不写文件、不起 Python |
| `path` 不在 `tmp/results/`、逃出 workspace、不是已有普通文件 | 明确错误，不起 Python |
| `script_path` 非法、不是 `.py`、不是已有文件 | 同上 |
| `code` 与 `script_path` 都给 / 都缺 / `code` > 64KiB | 同上 |
| `python` / `python3` 都不在 PATH | error，提示需要 Python 3 |
| 写 `.py` 失败 | error，不起 Python |
| 超时且有输出 | 杀进程；输出按 §2.3 内联或 spill；`timed_out=true`；`exit_code=-1`（除非已有真实码） |
| 超时且无输出 | error |
| stdout jsonl 写失败（脚本已结束） | 不外置；`output` 截到 **8192 字节**（按 rune 安全截断到合法 UTF-8）；设 `spill_error` 为短类名，不含 OS 绝对路径 |
| stdout 达 32MiB | 停写；`file_truncated=true`；`count`=已写行数 |
| 进程非 0 | `Execute` error 为 nil；结果带 `exit_code` 与输出 |
| 输入 jsonl 损坏 | 本工具不解析数据文件；由脚本处理 |

不在失败时改调 `terminal`。

---

## 5. 测试

不连 ES。夹具：临时 workspace、`ContextKeyWorkspaceRoot`、`ContextKeySessionID`。

解释器探测：测试包提供 `lookPath` 可注入；生产用 `exec.LookPath`。无 `python`/`python3` 时：**守卫用例仍跑**；需要起进程的用例 `t.Skip`。

### 不起 Python

- 无 workspace。
- `path` 为 `tmp/other/x.jsonl` 或含 `../`。
- `path` 指向不存在的文件。
- `code` 与 `script_path` 都给；都缺；`code` 长度 65537。
- `script_path` 为 `.js` 或不在 `tmp/results/`。

断言：临时目录下没有新进程副作用（无新 `.py`，或写 `.py` 前就失败则无该文件）。

### 起 Python（有解释器时）

- `code` 打印少于阈值：除 `.py` 外无 spill jsonl；结果是 map，含完整 `output`，`exit_code=0`。
- 打印 60 行文本：返回 `*QuerySpillStub`，jsonl 60 行且为 `{"line":...}`，结果无完整 `output`，`source_path` 为数据文件相对路径。
- 行少但总字节 > 8192：仍 spill。
- 每行一个 JSON object：jsonl 原样，不含外层 `line` 键（除非 object 自己有）。
- `script_path` 指向预先写入的 `.py`：与 inline 等价（读 `sys.argv[1]` 打印行数）。
- 数据文件内容可被脚本读到（夹具 jsonl 若干行）。
- `sys.exit(1)`：`Execute` error 为 nil，`exit_code=1`。
- 超时：脚本 `time.sleep` 超过 15s（测试可注入更短超时，例如 200ms）；`timed_out` 为 true。

### 不测

- 真实 ES e2e、门户 UI、OS 级读隔离、与 `terminal` 硬互斥。

---

## 6. 实现落点（给 plan 用，不是本期写代码）

| 单位 | 职责 |
|------|------|
| `framework/tool/run_result_script.go` | 注册、守卫、写 `.py`、起进程、规范化 stdout、接 `MaybeSpill` |
| `framework/tool/run_result_script_test.go` | §5 |
| `framework/tool/query_spill.go` | stub 增加 `ExitCode` / `TimedOut`；TTL 在写 `.py`/jsonl 后复用 |
| `framework/tool/file_tools.go` | `RegisterWorkspaceFileToolsWithConfig` 挂上本工具 |
| `result_stats` / `es_log_query` 描述 | 一句：复杂转换用 `run_result_script`，不要 `read_file` 整份 jsonl |

---

## 7. 成功标准

没有合法 `tmp/results/` 文件就跑不了 Python；有文件时能跑完并读到 `sys.argv[1]`；大 stdout 进 stub 不进当轮上下文；写盘失败时输出仍可见（截断）且不崩工具框架。
