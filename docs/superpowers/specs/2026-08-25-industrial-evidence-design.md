# 切片 B：证据语义（空击不得说成不存在）

**日期**: 2026-08-26  
**状态**: 已确认（2026-08-26）  
**父规格**: [2026-08-25-industrial-agent-program-design.md](./2026-08-25-industrial-agent-program-design.md)  
**评测网**: [2026-08-25-industrial-eval-design.md](./2026-08-25-industrial-eval-design.md)  
**对照（未展开的分期，不是本文蓝图）**: [2026-08-25-goal-lock-delivery-design.md](./2026-08-25-goal-lock-delivery-design.md) §7 P1  
**下一份**: [2026-08-25-industrial-evidence.md](../plans/2026-08-25-industrial-evidence.md)  
**禁止**: live LLM；改声称闸读 pin；把空击改判失败（从而再开火凭据闸）；强制 `list_tables` 才能结案；改正 Skill 索引名；实现 C/D/E；改 `inbound_empty` / 把 c7aa 标绿；新建平行评测框架。

**一句话**：工具 JSON 写出 `hit_status` + 实际查了哪个 index/repo；终答不得把 0 条说成「从未参与 / 服务不存在」；弱命题必须带上真实范围。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 锁住的性质 | 父规格 **P-nospeak** |
| 根因 | 0 条是观察；「从未参与」是全称否定。现网结果里没有 `hit_status` / 实际 index，模型只能脑补 |
| 写入 | `es_log_query`、`execute_read`、`rca_grep` 出结果时盖章 |
| 终答闸 | 纯函数 `EvaluateEmptyHitSpeakGate`；默认开；不挂在 `CodeClaimGate.Enabled` / `EvidenceGate.Enabled` 后面（后两者现网默认关） |
| 空击与凭据 | **不改** `HasSuccessfulBoundEvidence`：空击且 `Error==""` 仍算已查过 |
| Goal/Delivery P1 | 本文只收「空击 ≠ 不存在」。不收「必须先 list_tables」（ES 日志已禁止走 data 工具）。不收「Skill 正文写死索引名」 |
| 缺字段 | 本轮 fail-open + 日志；**不得**把缺失当成 `hits` |

---

## 1. 目标与非目标

### 目标

1. 三个工具在成功 0 条时 `hit_status=empty`；查询失败时 `error`；有条时 `hits`。成功 0 条时 `ok` 仍为 true（RCA 工具）或等价「查询成功」（`execute_read`）。
2. `es_log_query` 结果含实际使用的 `queried_index`（`params.index` 非空则用它，否则 `ESLogConfig.DefaultIndex`）。夹具能断言请求了 `vm-manager-*` 这类值。
3. `rca_grep` 结果含顶层 `repo`（本轮扫描到的仓；多仓可逗号拼接或第一个非空 match.repo，实现选一种并在测试写死）。
4. 终答闸：本轮有**显式** `empty` 时，禁止无范围的全称否定；禁词表与范围规则见 §4。命中则 Retry inject 一次；提示必须带上本轮真实 `queried_index`/`repo`。
5. `TestEvalGolden_empty_hit` 覆盖盖章 + 闸；破坏盖章或闸匹配必须红。入口仍是 A 的 `-run TestEvalGolden_`。

### 非目标

- 保证索引名永远正确；改 Skill references。
- 空击后强制再探（`list_tables` / 换 index）才能 idle。Retry 提示可以建议换 index，不是硬门。
- `jaeger_trace` / `list_tables` / `describe_table` / `rca_glob` / `rca_symbol` 盖章。
- 用 grep 0 条去拦「代码里不存在该符号」（误伤定位失败的正常说法）。
- 声称闸读 pin；C 用这些字段生成 mea-checks（字段留给 C，本期不消费）。
- live LLM、SSE 整轮回放、提交 `_neo4j_q/`。

### 诚实上限

- 禁词 + 范围约束会漏完全改写且带了 index 的全称否定（例如「查了 `vm-manager-*`，该服务历史上从未打过这条链路」）。本期接受；不引入 LLM 裁判。
- `execute_read` 在现网会拒绝 ES；SQL 空结果通常没有 `queried_index`。此时只走禁词表，不要求正文出现 index。
- 闸是终答机检，不是检索质量。错 index 仍可能 0 条；用户看到的应是「该 index 0 条」，不是「服务不存在」。

---

## 2. 字段契约（与父规格 §4 对齐，不得另起名字）

工具结果 JSON（map 或 `QueryResult` 的 json）增加：

| 字段 | 取值 | 何时必有 |
|------|------|----------|
| `hit_status` | `hits` / `empty` / `error` | 三个工具的**成功**返回；`es_log_query`/`rca_grep` 的 `rcaErr` payload 另写 `error`。`execute_read` 的 Go error 路径不要求 |
| `queried_index` | 实际发给后端的 index/pattern 字符串 | `es_log_query` 每次；`execute_read` 仅当入参 `index` 非空 |
| `repo` | 仓名或汇总 | `rca_grep` 每次成功返回（0 条也要有，可为空字符串若完全没扫到仓——见下） |
| `empty_reason` | 建议 `zero_hits` | 可选；仅 `empty` 时。测试不依赖此字段 |

推断：

- `error`：工具以**结果 payload** 报失败（`es_log_query` / `rca_grep` 的 `rcaErr`，`ok==false`）。不要把失败写成 `empty`。
- `execute_read` 失败保持现网 `return nil, err`，**不**为盖章改 Go error 契约，结果上可以没有 `hit_status`。闸只认 `Error==""` 且显式 `empty`，因此这条不开火。
- `empty`：成功且条数为 0（ES：`hits` 空且 `total==0`；SQL：`Rows` 空；grep：`matches` 空）。
- `hits`：成功且条数 > 0。
- `truncated==true` 且已有条 → `hits`，不是 `empty`。

**缺失语义（给 C/D 也写死）**：读取方不得把「没有 `hit_status`」当成 `hits`。终答闸把缺失当成「本条不是 empty」，因此不开火。

盖章入口：

- RCA 两工具：在组 payload 之后、`rcaOK`/`rcaErr` 之前调用同一 helper（建议 `tool.StampHitContract`）。禁止只在 agent 侧事后猜。
- `execute_read`：给 `executor.QueryResult` 增加 `HitStatus`、`QueriedIndex` 的 `json` 标签字段，在返回前赋值。禁止拆掉现有 `Columns`/`Rows`。

---

## 3. 终答闸

### 3.1 挂点

函数：`EvaluateEmptyHitSpeakGate(trace *RunTrace, finalText string) EvidenceGateResult`（`package agent`）。

调用：`checkAnswerGates` **一开始**就跑（声称闸 / 可选 EvidenceGate 之前）。默认开启。`SATH_EMPTY_HIT_GATE=0` 时整闸跳过（测关闭用；生产不默认关）。

不复用 `CodeClaimNudges`。独立计数（建议 `trace.EmptyHitNudges`）：有 step room 时 inject **每轮最多一次**。

无 step room / `forceFinal`：与现网证据闸相同——**不** inject、**不** halt、**不**改写用户可见正文；返回 `Incomplete`。`Reason=empty_hit_speak` 写入方式与现网声称/证据闸一致：`trace.Errors` 追加 `empty_hit_speak`（或同形前缀），事件 payload 带 `reason`。不新增 `RunTrace.Reason` 字段。

### 3.2 开火条件（同时）

1. `trace` 中至少一条记录：`Error==""`、`!Blocked`，且结果里 **显式** `hit_status=="empty"`（字符串比较）。结果是 `map[string]any` 或可 json 的 `*executor.QueryResult` / `QueryResult`，闸内用同一抽取函数。
2. `finalText` 规范化后命中 §4，且不被允许弱命题豁免。

否则 `Allow=true`。

无 empty、只有 `hits`/`error`/缺字段 → 不开火。

### 3.3 Retry 文案（写死结构，index 用本轮抽取值）

中英均可，须含：

- 本轮所有 empty 记录的 `queried_index` 或 `repo`（没有则写工具名）
- 「0 条只能写未查到，不能写从未参与 / 服务不存在」
- 「换 index 再查」为建议，不是命令式硬门

`Reason`：`empty_hit_speak`。

---

## 4. 措辞表（写死）

比较前：NFKC + 小写 + 去空白折叠为单空格。子串匹配。

**允许豁免（先处理豁免，再查禁止项）**：

| 组 | 规则 |
|----|------|
| 弱命题 | 删除子串：`不能据此说从未参与`、`不能说从未参与`、`cannot conclude never` |
| 合法观察 | 删除子串：`未查到`、`0 条`、`0 hits`、`没有匹配行` |
| Redis 真结论 | **不是删子串**。若规范化正文同时含 `redis` 与 (`key` 或 `键`) 且含 (`不存在` 或 `nil`)：跳过禁止 B 里的「不存在」类（`不存在`、`does not exist`），仍查禁止 A |

「该索引 0 条，不能据此说从未参与」删除弱命题 + 合法观察后不再含禁词 → 放行。T3「Redis 里 key 不存在」走 Redis 规则，禁止 B 不开火，禁止 A 未命中 → 放行。

**禁止 A（有 empty 即拒，写了 index 也不放）**：

`从未参与`、`从未出现`、`服务不存在`、`没有这个服务`、`never participated`、`service does not exist`

**禁止 B（有 empty 且正文未包含本轮任一 empty 记录的非空 `queried_index` 或 `repo` 则拒）**：

`不存在`、`没有参与`、`从未调用`、`does not exist`

禁止 B 只按上表子串匹配（例如「这条链路没有参与」）。「完全没有它」等未进表的改写本期不保证（见诚实上限）。一旦正文出现真实 index/repo，禁止 B 不再开火（禁止 A 仍开火）。

**顺序（写死）**：先删豁免子串，再查禁止 A/B。豁免子串必须足够长，避免短词吃掉禁止 A（因此合法观察表不含「索引里没有」）。

`rca_grep` 的 empty **不参与禁止 A/B**（只盖章，供 C/D）。本闸只认工具名 `es_log_query`、`execute_read` 的 empty。避免 grep 0 条误伤「仓库里没这个符号」。

---

## 5. 文件落点

| 路径 | 职责 |
|------|------|
| `framework/tool/evidence.go`（或紧邻新文件） | `StampHitContract` / 常量 `hits` `empty` `error` |
| `framework/tool/es_log_tool.go` | 写入 `hit_status`、`queried_index` |
| `framework/tool/rca_code_tools.go`（grep 分支） | 写入 `hit_status`、顶层 `repo` |
| `framework/executor/reader.go` | `QueryResult` 增加可选 json 字段 |
| `framework/tool/data/execute_read.go` | 返回前盖章 |
| `framework/agent/empty_hit_speak_gate.go` | `EvaluateEmptyHitSpeakGate` |
| `framework/agent/react_agent.go` | `checkAnswerGates` 接入；nudges |
| `framework/agent/evalgolden_test.go` | `TestEvalGolden_empty_hit`（闸） |
| `framework/tool/evalgolden_test.go` | `TestEvalGolden_empty_hit_stamp`：`es_log_query` + `rca_grep` 盖章（假 Reader / 空仓，禁止真 ES） |
| `framework/tool/data/evalgolden_test.go` | `TestEvalGolden_empty_hit_stamp_read`：`execute_read` 0 行盖章。**不要**从 `package tool` 去 import `tool/data`（循环依赖） |
| `scripts/industrial-eval.ps1` | 增加 `go test ./tool` 与 `go test ./tool/data`（均在 `framework` 模块、`-run TestEvalGolden_`） |

现有 `NormalizeRCAResult` 不自动给所有 RCA 工具盖章（避免 jaeger 等语义不清）。只三个工具显式调用。

---

## 6. 测试与验收

单测，不绑现网 ES。破坏对应生产函数必须红。

| # | 检查 | 通过 |
|---|------|------|
| G1 | `es_log_query` 0 行成功 | `hit_status==empty`，`queried_index` 等于传入或默认 index（夹具用 `vm-manager-*`） |
| G2 | `es_log_query` 有行 | `hit_status==hits` |
| G3 | `es_log_query` 查询 error | `hit_status==error`，不是 `empty` |
| G4 | `execute_read` 0 行 | `hit_status==empty`；无 index 入参则无/空 `queried_index` |
| G5 | `rca_grep` 0 match | `hit_status==empty`；有顶层 `repo` 键 |
| T1 | empty ES +「该服务从未参与」 | `Allow==false`，`Reason==empty_hit_speak` |
| T2 | empty ES +「该索引 0 条，不能据此说从未参与」且含 `vm-manager-*` | `Allow==true` |
| T3 | empty ES +「Redis 里 key 不存在」 | `Allow==true` |
| T4 | empty ES +「这条链路没有参与」（无 index） | `Allow==false`（禁止 B） |
| T5 | empty ES +「`vm-manager-*` 上没有匹配行」 | `Allow==true` |
| T6 | 仅 grep empty +「从未参与」 | `Allow==true`（grep 不参与本闸） |
| T7 | 无 `hit_status` 的旧式 0 击 map +「从未参与」 | `Allow==true`（fail-open） |
| T8 | 缺字段不得被抽取成 `hits` | 抽取函数对缺失返回空状态，不是 `hits` |

`TestEvalGolden_empty_hit` 至少包含 T1–T3 与 T7。T4（禁止 B）必须是 `TestEvalGolden_empty_hit_unscoped`（或同前缀），以便 A 脚本 `-run TestEvalGolden_` 跑到。G1–G3、G5 在 `./tool` 的 stamp 测试；G4 在 `./tool/data`。

故意把 `StampHitContract` 在 0 行时写成 `hits`，或删掉禁止 A 的 `从未参与` 匹配 → 对应测试 FAIL。

---

## 7. 错误处理与开关

| 情况 | 行为 |
|------|------|
| `SATH_EMPTY_HIT_GATE=0` | 闸跳过；盖章仍做（C/D 还要字段） |
| 结果类型无法解析 | 该条当缺失，fail-open |
| 多个 empty、多个 index | 禁止 B 的「包含」：正文含**任意一个**非空 queried_index/repo 即算有范围；Retry 提示列出全部 |
| 与凭据闸同轮 | 顺序不变：idle → 凭据 → 终答闸。空击仍跳过凭据注入 |
| 与声称闸同轮 | 空击闸先判；已 inject 则本轮不再跑声称闸（与现网 `checkAnswerGates` 遇 Inject 即返回一致） |

---

## 8. 与父规格 / A / Goal-Delivery

- 字段名与枚举不得偏离父规格 §4。`empty_reason` 可加，不得改名三个必有字段。
- A 的 `empty_hit` 行由本文变绿；不另起评测名单。
- Goal/Delivery P1 的「须先 list_tables / Skill 写索引」**不在本文**。ES 探索义务若以后做，工具是换 `es_log_query` 的 index，不是打开 `list_tables`。

---

## 9. 验收清单（本文自身）

- [ ] 实现者能写出三个工具各在 0 条时的 JSON 键，而不去改声称闸。
- [ ] 「不能据此说从未参与」不会因为含子串「从未参与」被误杀。
- [ ] 读者不会把本文当成必须先 `list_tables` 或改正 Skill 索引。
- [ ] 下一份是 B 的 implementation plan，且引用 `TestEvalGolden_empty_hit`。
