# 切片 D：值班底座（写默认拒绝 + 结案证据门 + obs）

**日期**: 2026-08-27  
**状态**: 已确认（2026-08-27）  
**父规格**: [2026-08-25-industrial-agent-program-design.md](./2026-08-25-industrial-agent-program-design.md)  
**依赖**: [2026-08-25-industrial-evidence-design.md](./2026-08-25-industrial-evidence-design.md)（B：空击仍算已查过；不改 `HasSuccessfulBoundEvidence`）  
**评测网**: [2026-08-25-industrial-eval-design.md](./2026-08-25-industrial-eval-design.md)  
**对照（现网，不推翻）**: `ExecuteOptions` 零值拒写、`execute_write` HITL、`EvaluateEvidenceGate` Soft、`WorkspaceFilesEnabled` 默认 false  
**下一份**: `docs/superpowers/plans/2026-08-27-industrial-duty.md`  
**禁止**: 把结案改成必须 `hit_status=hits`；改 `HasSuccessfulBoundEvidence`；用 `SATH_ALLOW_WRITE` 卸掉 `execute_write`；把 `write_file`/`patch` 从 E5 opt-in 改成全局拒绝；默全开 MEA；实现 E0–E5；改声称闸读 pin；改正 Skill 索引；live LLM 进 CI；新建平行评测框架。

**一句话**：生产默认可值班 = SQL 写零值拒绝、文件写须 opt-in、调查题缺 jaeger/ES 证据不得 idle 结案；这三块现网已有地板，D 钉进 `TestEvalGolden_*` 并补上 `hit_status`/index/repo 的结构化观测。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 锁住的性质 | 父规格 **P-deny-write** |
| 策略 | **锁现网地板**，不另起 Agent 黑名单、不另起关键词表 |
| 写 | `ExecuteOptions{}` → `ErrReadOnlyViolation`；`execute_write` 无 `confirm_token` 只提议；`write_file`/`patch` 默认不注册 |
| 结案门 | 继续 `EvaluateEvidenceGate`：`RequireAnyOf` = `jaeger_trace` / `es_log_query`；调查题谓词 = 现网 `ShouldApplyEvidenceGate` |
| 「成功证据」 | `evidence_refs` 的 `Kind` ∈ RequireAnyOf。**含空击**（B：0 条且 `Error==""` 仍盖 ref） |
| 短问答 | 靠 `ShouldApplyEvidenceGate(nil, G)==false` 挡「你好」。**不**用 `HasPriorAssistant` 当开火条件（见 §2.3） |
| obs | slog + otel 属性：`hit_status`、`queried_index`、`repo`。Prometheus **不加** index/repo 标签 |
| 金样例 | `TestEvalGolden_deny_write`、`TestEvalGolden_close_gate`、`TestEvalGolden_close_gate_chat`、`TestEvalGolden_obs_hit` |

---

## 1. 目标与非目标

### 目标

1. SQL/ES DSL 写：`ExecuteOptions{}`（零值）执行 INSERT/UPDATE/DELETE（及现网已识别的 ES 写 DSL）必须 `ErrReadOnlyViolation`。只有 `AllowWrite: true` 或经 HITL 确认后的 `executorAsWriter` 才放行。
2. `execute_write` 无 `confirm_token`：返回 pending，**不得**调用 Writer/Executor 写。有合法 token 的确认路径保持现网（`AllowWrite: true`）。
3. `HermesP0ToolFlags` 零值 / `DefaultHermesP0ToolFlags`：`WorkspaceFilesEnabled==false`，生产默认不注册 `write_file`/`patch`。不得改 env/YAML 的 opt-in 语义（E5 仍靠 `SATH_WORKSPACE_FILES_ENABLED` 或 Agent 配置打开）。
4. RCA 调查题 + registry 挂了 `jaeger_trace` 或 `es_log_query` + 本轮 idle 终答：无 RequireAnyOf 的 ref → Soft inject，不得当结案。有 `es_log_query`/`jaeger_trace` ref（含 Summary=`no hits`）→ Allow。仅 `rca_grep` ref → 仍 inject。终答含「证据不足」/ `insufficient evidence` → Allow（现网豁免保留）。
5. 「你好」「有哪些技能」：`ShouldApplyEvidenceGate` 为假，生产用 `EvidenceGateTurnOption` 关掉闸；即使 `EvaluateEvidenceGate(Enabled:true, refs:nil)` 会对任意正文 inject，**值班路径不得对闲聊开闸**。
6. 盖章/executor 关键路径打出 B 契约字段，夹具能断言一次空击 ES 日志带 `hit_status=empty` 和 `queried_index`。
7. 上列不变量各有 `TestEvalGolden_*`；破坏对应生产函数必须红。入口仍是 `scripts/industrial-eval.ps1`。

### 非目标

- 空击后再探才能 idle（B 已否决）。
- 把 `RequireAnyOf` 扩成 `IsBoundEvidenceTool` 全名单（grep 过就能过 RCA 结案门）。
- 把 `RequireAnyOf` 缩成「必须 hits>0」。
- 新环境变量关掉整个 `execute_write` 注册。
- HardHalt 结案门（保持 Soft inject + 「证据不足」豁免）。
- 为 obs 新建日志平台、改 Prometheus 高基数标签。
- E0 强模型、E5 改测循环、默全开 MEA、声称闸读 pin。

### 诚实上限

- 结案门与 C 一样复用 `FamilyRCA` 关键词。无 `elasticsearch` / `日志` 等词的调查句（现网 `bf26Q`）**不会**开结案门。不要为救这类题另起词表。
- Soft inject 一次之后，模型仍可能写「证据不足」过闸，或 forceFinal 时与现网证据闸一样不 halt。D 不把 Soft 改成 Hard。
- obs 不能保证索引名正确，只保证「实际查了什么」可检索。错 index 的 empty 仍过结案门（已查过）。
- HITL 确认后的写是用户同意的写，不是漏洞。D 不在确认之后再拦一层 Agent 黑名单。

---

## 2. 与父规格的口径（写死，避免按字面扩闸）

### 2.1 「与 Goal/Delivery 证据名单一致」

父规格 §5 D 写「本轮无成功证据工具（与 Goal/Delivery 证据名单一致）」。Goal/Delivery 的 `IsBoundEvidenceTool` **宽于**现网结案门（含 `execute_read` / `rca_grep` / `rca_read` 等）。

**本期解释**：调查题**是否开火**与 C 共用 `ShouldApplyEvidenceGate`（FamilyRCA）。**过闸所需工具**保持现网 `RequireAnyOf` = `jaeger_trace`、`es_log_query`。只 grep 的源码排查若被打成 RCA 调查题，仍会被 inject——这是现网已有行为，金样例钉死，不在 D 放宽。

禁止把 `defaultRequireAnyOf` 改成 `IsBoundEvidenceTool` 的全集。

### 2.2 「成功」不是 `hits>0`

父规格 §4 说 D 消费 `hit_status`。消费方式是 **obs 录制 + 结案门读 ref.Kind**，不是「`empty` 则不得结案」。

空击 ES 仍 `deriveESLogRefs` → `Kind=es_log_query`（可带 Summary=`no hits`）。`EvaluateEvidenceGate` 见该 Kind 即 Allow。这与 B「不改 `HasSuccessfulBoundEvidence`」一致。

### 2.3 短问答不用 `HasPriorAssistant`

父规格 §6：「仅当 `HasPriorAssistant` 且 G 像调查题且本轮声称结案时开火」。

若照做，**第一轮**调查题 idle 会漏过结案门。`HasPriorAssistant` 是任务锁里「已有助手正文」的追问标记，不是 RCA 谓词。

**本期开火（同时）**：

1. registry 能 `Get("jaeger_trace")` 或 `Get("es_log_query")`（`ShouldEnableEvidenceGate`）
2. `ShouldApplyEvidenceGate(active, userText)` 为真（有 active 族则看 `FamilyRCA`；否则对 userText 打 RCA 分 > 0）
3. 本轮走到终答评估（idle / 声称结案），调用 `EvaluateEvidenceGate`

生产入参 **不改**：`chat.go` 继续 `EvidenceGateTurnOption(reg, active, userForIntent)`。禁止为了「对齐 C 的 G」把第三参换成 `lock.Q`，也禁止用催打印的 D 当开火句。

金样例 `close_gate_chat` 测纯函数：`ShouldApplyEvidenceGate(nil, "你好")` 为假；`ShouldApplyEvidenceGate(nil, "用 elasticsearch 查一下错误日志")` 为真（与 C / 现网 `TestEvidenceGateTurnOption_*` 同句）。第一轮调查题只要 (2) 对 userText 为真即可开火，不要求 `HasPriorAssistant`。

---

## 3. 写路径

现网（不得静默改回去）：

```text
ExecuteOptions{}.AllowsWrite() == false
→ MySQL/ES executor 写 DSL → ErrReadOnlyViolation

execute_write 无 confirm_token → proposeWrite（pending + token），不 Exec
execute_write 有合法 confirm_token → executorAsWriter.Exec（AllowWrite: true）

WorkspaceFilesEnabled 默认 false → 不注册 write_file / patch
```

本期只加金样例与（若缺）一行注释/断言，**不**再包一层「Agent 禁止写」中间件。

允许动的代码：把现有 `TestExecuteOptions_DefaultDenyWrite` 等薄封装成 `TestEvalGolden_*`；HITL 提议阶段断言 Writer 未被调用。禁止为了金样例改 `AllowsWrite` 默认值。

---

## 4. 结案门

现网函数继续当生产实现：

| 函数 | 包 | D 锁定的行为 |
|------|-----|----------------|
| `ShouldEnableEvidenceGate` | `portal/internal/chat` | registry 无 jaeger/es → 生产不挂闸 |
| `ShouldApplyEvidenceGate` | 同上 | 非 RCA → 假 |
| `EvidenceGateTurnOption` | 同上 | 非 RCA 时 `Enabled: false` 覆盖 BuildReActAgent 的自动开启 |
| `EvaluateEvidenceGate` | `framework/agent` | 见 §1 目标 4；`HardHalt` 默认假 |
| `collectEvidenceRefsFromTrace` | `framework/agent` | 从工具结果抽 ref，不读 `hit_status` 判成败 |

`BuildReActAgent` 在 `ShouldEnableEvidenceGate` 时挂 `Enabled: true`；每轮再叠 `EvidenceGateTurnOption`。D 不改这个顺序。

Idle 无 refs、未写豁免句 → `Action=inject`，Reason 含 missing required evidence，Prompt 含 jaeger/es / 证据不足（现网 `evidenceGateSoftPrompt`）。D 不改 Prompt 正文，除非测试发现它不再点名 jaeger/es。

---

## 5. obs

### 5.1 字段（与 B 同名，不得另起）

| 键 | 何时有值 |
|----|----------|
| `hit_status` | `hits` / `empty` / `error`；缺失则**不要**写成 `hits` |
| `queried_index` | ES 盖章时；SQL `execute_read` 仅入参 `index` 非空 |
| `repo` | `rca_grep` 盖章时（含 0 条；可空字符串） |

### 5.2 写入点

单一 helper，建议 `obs.LogHitContract(ctx, toolName, status, queriedIndex, repo)`：

- `slog.InfoContext`（或现网已有 logger）带上述键 + `tool`
- 若 ctx 有 span：`SetAttributes` 同名键（`sixath.hit_status` 等，实现写死一种前缀并在测试用同一前缀）

调用（最低）：`tool.StampHitContract` 在写入 status 之后调一次。这是 ES / grep 的盖章口，也是 `obs_hit` **必须**钉死的路径。

`executor.opRecorder.finish` 吃的是 `*executor.Result`，该类型**没有** `HitStatus`（字段在 `QueryResult` 上，由 `execute_read` 在 Query 返回后赋值）。因此：

- **不要**为了 obs 给 `Result` 加字段，除非实现时发现不加点不上 execute_read。
- `finish` 只记已有键（rows、allow_write 等）。**禁止**用 `len(Rows)==0` 猜 `hit_status=empty`（B：缺失 ≠ empty；写操作 rows 空更不能当 empty）。
- `execute_read` 赋值 `QueryResult.HitStatus` 之后**可以**再调一次 `LogHitContract`。这是加分，**不是** `obs_hit` 的必过条件。

金样例 `obs_hit` 钉：`StampHitContract` 空击 ES（`queried_index=vm-manager-*`）→ 日志或测试钩子含 `hit_status=empty` 且该 index。空 SELECT 不进最低夹具。

禁止：Prometheus counter/histogram 增加 `queried_index` / `repo` 标签。现有 `executor_errors_total` 的 `error_kind=readonly` 可保留，不在本期扩。

缺字段：照记已有键，缺的省略。不得补默认 `hits`。

---

## 6. 文件落点

| 路径 | 职责 |
|------|------|
| `framework/executor/evalgolden_test.go` | `TestEvalGolden_deny_write`（零值 INSERT → `ErrReadOnlyViolation`） |
| `framework/tool/data/evalgolden_test.go` | `TestEvalGolden_deny_write` 的 HITL 子用例**或**同文件第二条 `TestEvalGolden_deny_write_pending`：无 token 不写 |
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_close_gate_chat`（你好 / bf26Q → `ShouldApplyEvidenceGate` 假）；可选断言 `DefaultHermesP0ToolFlags.WorkspaceFilesEnabled==false` 作为 `deny_write` 文件侧，或单独子测试放同文件 |
| `framework/agent/evalgolden_test.go` | `TestEvalGolden_close_gate`（无 ref inject；es 空击 ref Allow；仅 grep inject；证据不足 Allow） |
| `framework/obs/hit_contract.go`（新，名称可微调） | `LogHitContract` |
| `framework/tool/evidence.go` | `StampHitContract` 调用日志 helper |
| `framework/executor/observe.go` | `finish` 带契约字段 |
| `framework/obs/evalgolden_test.go` 或 `framework/tool/evalgolden_test.go` | `TestEvalGolden_obs_hit` |
| `scripts/industrial-eval.ps1` | 增加 `./executor`、`./obs`（若测试落在这些包） |
| [industrial-eval §7](./2026-08-25-industrial-eval-design.md) | 实现时加四行 ID（文档同步，不是新框架） |

禁止新建 `evalgolden/` 目录包。HITL 与 executor 零值可以两个 `TestEvalGolden_deny_write*`，但 **plan 必须至少引用** `TestEvalGolden_deny_write` 与 `TestEvalGolden_close_gate`。

`LogHitContract` 放 `obs` 包。`StampHitContract` 在 `framework/tool/evidence.go`：现网 `es_log_tool.go` **不** import `obs`（`tool/mcp.go` 与 `tool/data/execute_read.go` 才 import）。若 `tool` → `obs` 造成循环依赖，改为 `StampHitContract` 调一个 `tool` 包内函数变量（测试可替换），由 `obs` 在 `init` 或显式 `obs.HookHitContract(tool.SetHitContractLogger)` 接线；禁止 `obs` import `agent`。实现选一种，plan 写死，不要两条并行。

---

## 7. 测试与验收

单测，不绑现网 ES，不调付费 LLM。可复用现有 sqlmock / 内存 pending store。

| ID | 检查 | 通过 |
|----|------|------|
| `deny_write` | MySQL executor，`ExecuteOptions{}`，`INSERT ...` | `errors.Is(err, ErrReadOnlyViolation)`；sqlmock **无** ExpectExec |
| `deny_write` opt-in 对照（同一测试表驱动第二行，不必新 ID） | `AllowWrite: true` 的 INSERT | 现网已有行为，必须仍成功（防 D 把 opt-in 砍掉） |
| `deny_write_pending` | `execute_write` 无 `confirm_token`，Writer 为计数 mock | 返回 pending/token；Writer.Exec 次数 = 0 |
| `deny_write` 文件 | `DefaultHermesP0ToolFlags.WorkspaceFilesEnabled` | `false` |
| `close_gate` | `EvaluateEvidenceGate(Enabled:true, refs:nil, "root cause is OOM")` | `Allow==false`，`Action==inject` |
| `close_gate` 空击 | refs=`[{Kind:es_log_query, Summary:no hits}]` | `Allow==true` |
| `close_gate` 仅 grep | refs=`[{Kind:rca_grep}]` | inject |
| `close_gate` 豁免 | refs=nil，正文含「证据不足」 | Allow |
| `close_gate_chat` | `ShouldApplyEvidenceGate(nil, "你好")`、`AutoChecks` 反例句 | 假 / 空；`EvidenceGateTurnOption` 在非 RCA 时关掉 Enabled |
| `obs_hit` | `StampHitContract(..., empty, queried_index=vm-manager-*)` | 日志或测试钩子含 `hit_status=empty` 且 `queried_index=vm-manager-*` |

故意让 `AllowsWrite` 零值为 true，或让无 ref 的 `EvaluateEvidenceGate` 直接 Allow，或让 `StampHitContract` 不再调 `LogHitContract` → 对应金样例 FAIL。

`close_gate_chat` 与 C 的 `mea_chat_skip` 可共享「你好」夹具，禁止复制第二套 RCA 词表。

---

## 8. 错误处理与开关

| 情况 | 行为 |
|------|------|
| 闲聊 / 非 RCA | `EvidenceGateTurnOption` 关闸；不 inject |
| registry 无 jaeger/es | 生产不挂结案门（源码-only Agent 不被 SQL 证据门误伤） |
| 空击 ES | 过结案门；终答撒谎仍由 B 的 `EvaluateEmptyHitSpeakGate` 管 |
| 缺 `hit_status` 的旧结果 | obs 省略该键；结案门仍只看 ref.Kind |
| HITL 拒绝 / token 过期 | 现网错误返回，不得降级成直接写 |
| `SATH_WORKSPACE_FILES_ENABLED=1` | 仍可注册文件写（E5）；D 的金样例测**默认** false，不测 env 真值路径为红 |
| forceFinal / 无 step | 与现网 EvidenceGate Soft：不 halt、不改写用户可见终答 |

---

## 9. 与 A / B / C / E

- A：扩面 ID `deny_write`、`close_gate`、`close_gate_chat`、`obs_hit`。不另起名单。
- B：D 不改三个工具盖章语义，只在盖章口加观测。空击过结案门。
- C：结案门与 AutoChecks 共用 `ShouldApplyEvidenceGate`。D 不改 MEA 进门，不默全开。
- E5：文件写保持 opt-in。D 的「默认拒绝」是 flags 零值，不是删除工具实现。

---

## 10. 验收清单（本文自身）

- [ ] 实现者不会把结案门改成「0 条必须再探」。
- [ ] 实现者不会用 `HasPriorAssistant` 挡住第一轮调查题。
- [ ] 「你好」不开结案门；零值 INSERT 红灯在 `TestEvalGolden_deny_write`。
- [ ] 下一份是 D 的 implementation plan，且引用 `TestEvalGolden_deny_write` 与 `TestEvalGolden_close_gate`。
