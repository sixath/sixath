# 切片 C：MEA 产品化（可机检则无需手写围栏）

**日期**: 2026-08-26  
**状态**: 已确认（2026-08-26）  
**父规格**: [2026-08-25-industrial-agent-program-design.md](./2026-08-25-industrial-agent-program-design.md)  
**依赖**: [2026-08-25-industrial-evidence-design.md](./2026-08-25-industrial-evidence-design.md)（B 字段已稳定）  
**评测网**: [2026-08-25-industrial-eval-design.md](./2026-08-25-industrial-eval-design.md)  
**对照（现网，不推翻）**: [2026-08-12-mea-minimal-subset-design.md](./2026-08-12-mea-minimal-subset-design.md)、[portal/docs/mea-m0.md](../../../portal/docs/mea-m0.md)、[2026-08-15-task-handling-current-design.md](./2026-08-15-task-handling-current-design.md)  
**下一份**: `docs/superpowers/plans/2026-08-26-industrial-mea.md`  
**禁止**: 默全开 MEA；每步 LLM 裁判；用 MEA 重写 Growth/Procedural；实现 D/E；改声称闸读 pin；改正 Skill 索引；live LLM 进 CI；新建平行评测框架。

**一句话**：Agent 已开 MEA 且任务可机检时，系统合成验收契约，用户不必写 `mea-checks` 围栏；`completed` 只来自审计；闲聊与合成失败仍走纯 ReAct。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 锁住的性质 | 父规格 **P-no-self-complete** |
| 现网已有 | M0：`ApplyAudit` 才写 `completed`；`ClaimComplete` 不写状态。进门却要求用户围栏 + Workspace 非空 |
| 本期补洞 | 无围栏也可进；trace 类 check 不要求 Workspace；审计读 B 的 `hit_status` + 终答 |
| 默关 | **不推翻**。`MEAEnabledForAgent` 仍为 UI OR `SATH_MEA=1` OR pilot |
| 入口时机 | 仍在 ReAct **之前**分流。B 字段出在工具之后，入口只放下「事后检这些字段」的契约，不在入口读 `hit_status` |
| 用户围栏 | `mea-checks` 解析成功则**优先**，不再跑 `AutoChecks`。仅有 `mea-acceptance`、无 checks 时仍可跑 `AutoChecks`（checks 非空则 Cascade 走 Rules，不再走 M1 LLM） |
| 闲聊 | `AutoChecks` 空 → 不进 MEA |
| 合成失败 | 不进 MEA，打点，纯 ReAct（父规格 §6） |

---

## 1. 目标与非目标

### 目标

1. `useMEA` 改为：`MEAEnabled` **且**（围栏 checks/acceptance 非空 **或** `AutoChecks(G)` 非空）**且**（Workspace 非空 **或** 全部 check 为 trace 类且 acceptance 为空）。
2. `AutoChecks` 纯函数、无 LLM。调查题吐出 `trace_hit_status` + `empty_hit_speak`。非调查题返回空。
3. `ExecutionReport` 带上终答与工具击打摘要；Rules 审计能检上述两 type。文件类 check 行为不变。
4. Executor `ClaimComplete=true` 且审计未 `complete+clean` → requirement **不是** `completed`。金样例钉死。
5. `SATH_MEA=0` 且 Agent 未勾 MEA → 与现网纯 ReAct 一致（含无围栏调查题）。
6. `TestEvalGolden_mea_*` 挂 A 前缀；破坏合成或 `ApplyAudit` 完成闸必须红。

### 非目标

- 全局默认打开 MEA。
- 为「改文件 + go test」自动生成 `path_exists`（E5）。
- 用 LLM 判断是否可机检、是否完成。
- 把 `ClaimComplete` 从 Executor 报告里删掉（可继续存在，状态机忽略即可）。
- 改变 M1 Cascade：有结构化 checks 时仍机检优先，LLM 不得覆盖机检失败。
- D 写拒绝、E0 强模型、声称闸读 pin。

### 诚实上限

- `AutoChecks` 复用现网 `ShouldApplyEvidenceGate(nil, G)`（即 `FamilyRCA` 关键词打分）。**不含** `elasticsearch` / `es_log` / `日志排查` / `jaeger` 等词的调查句不会自动进 MEA。现网金句 `bf26Q`（access-service / vm-manager / startGame，无上述词）本期**不进** AutoChecks。不要为救这类题另起关键词表。
- 合成的 `trace_hit_status` 要求本轮打了 `es_log_query` / `execute_read` / `rca_grep` 之一且带显式 `hit_status`。只 `rca_read` 的源码题不会因此被标完成（应用户围栏或 E）。
- forceFinal / MaxRounds 用尽时 MEA 仍可能 `incomplete`，与现网外环一致；不在本期改成吞终答。

---

## 2. 进门

现网（不得静默改回去）：

```text
useMEA = (len(meaChecks)>0 || len(meaAcceptance)>0)
       && MEAEnabledForAgent(...)
       && Workspace != ""
```

本期：

```text
checks = mea-checks 解析成功 ? 围栏checks : AutoChecks(G)
acceptance = 围栏成功的 mea-acceptance（AutoChecks 不合成 acceptance）
traceOnly = 所有 checks 的 type 都属于 {trace_hit_status, empty_hit_speak}
             且 acceptance 为空
useMEA = MEAEnabledForAgent(...)
       && (len(checks)>0 || len(acceptance)>0)
       && (Workspace != "" || traceOnly)
```

- `G` = 本轮 `TurnTaskLock.Q`（调查目标，不是催打印的 D）。在 `useMEA` 判断处调用 `AutoChecks`（`chat.go` 已有 `lock := buildTurnTaskLockFromHistory`），不要在更早的 `ParseMEAChecks` 处用未锁的用户句。无锁时用用户正文。
- `MEAEnabledForAgent` 为假时不得因 AutoChecks 非空而进 MEA；可不调用 `AutoChecks`。
- 围栏非法/空数组：与现网一样 `ok=false`，再尝试 `AutoChecks`。不要把坏围栏当验收。仅 `mea-acceptance`、无 `mea-checks`：仍跑 `AutoChecks`。
- `SATH_MEA=0` 只关**未勾 UI / 非 pilot** 的 Agent；UI 勾选仍开（与现网 `MEAEnabledForAgent` 一致）。不得改成「环境变量 0 压过 UI」。

`RunRulesMEA` 在 `MEAEnabled` 为假时仍 `Skipped/disabled`。Workspace 空且 checks 含任何文件类 type（`path_exists` / `file_contains` / `json_path`）→ **不进** MEA（与现网「空工作区不进」一致）；不要用空 WorkDir 跑文件审计。

---

## 3. AutoChecks

函数：`AutoChecks(goal string) []mea.AcceptanceCheck`（`portal/internal/chat`，与 `ParseMEAChecks` 同包）。

**可机检（返回两条）** 当且仅当：

```go
ShouldApplyEvidenceGate(nil, strings.TrimSpace(goal)) == true
```

即：对 `FamilyRCA` 做现网 `scoreFamilies`，分 > 0。禁止另起关键词表。

返回（顺序写死）：

```json
[
  {"type": "trace_hit_status"},
  {"type": "empty_hit_speak"}
]
```

否则返回 `nil`（不是空 type 的假 check）。

panic / 打分异常 → 视为合成失败：`AutoChecks` 用 recover 返回 nil，调用方打点（日志字段 `mea_autochecks=error`），走 ReAct。非调查题与异常在返回值上都是 nil，靠日志区分。

正例（必须非空，与 `TestShouldApplyEvidenceGate` 对齐）：`用 elasticsearch 查一下错误日志`。  
反例（必须空）：`你好`、`有哪些技能`、`继续`（整句）、现网 `bf26Q`（无 RCA 关键词）。

「没有打印出来呀」不得单独送进 `AutoChecks`；送 **G**。夹具：history 使 `BuildTurnTaskLock` 的 Q 为上述正例句时，`AutoChecks(lock.Q)` 非空；Delivery 可以是「没有打印出来呀」。

---

## 4. ExecutionReport 与审计

### 4.1 报告字段（mea 包追加，不得另起平行结构）

```go
type ToolHit struct {
    ToolName     string `json:"tool_name"`
    HitStatus    string `json:"hit_status,omitempty"` // 显式 hits|empty|error；缺失留空
    QueriedIndex string `json:"queried_index,omitempty"`
    Repo         string `json:"repo,omitempty"`
    Error        string `json:"error,omitempty"`
    Blocked      bool   `json:"blocked,omitempty"`
}

type ExecutionReport struct {
    // 现有字段不变
    FinalText string    `json:"final_text,omitempty"`
    ToolHits  []ToolHit `json:"tool_hits,omitempty"`
}
```

Portal 的 MEA Executor（`mea_stream.go`）必须拿到本轮已有的 `RunTrace`（`streamAgentEvents` 里 `persistTurnTrace` 用的那份）和终答文本，填 `FinalText` / `ToolHits`。现网该函数只返回 `summary string`：允许扩返回值或 out-param，**禁止**另跑一轮 Agent、禁止从 SSE 字符串反解析工具 JSON。`FinalText` 必须是本轮 episode 终答（与内环 `EvaluateEmptyHitSpeakGate` 同一段 idle 文本，例如 `StreamEventDone` 的最后一条 assistant），**不得**用全部 `StreamEventDelta` 拼成的 `summary`（会混入思考/工具旁白，T-speak 对不上）。`hit_status` 用 `tool.HitContractFromResult`。**禁止**在 agent 侧再猜字段。

`ClaimComplete`：成功跑完 ReAct 仍可 `true`（现网如此）。`ApplyAudit` **继续忽略**它。禁止增加「ClaimComplete → completed」的任何路径。

`messagesForMEAContract`：文件类 checks 可保留现网「produce environment state…」块。**仅含** `trace_hit_status` / `empty_hit_speak` 时不得再用这段（会诱使模型去写文件）。改成一两句：本轮用 ES/SQL/grep 调查；验收读工具 JSON 的 `hit_status` 与终答，不要为过检去 `write_file`。

### 4.2 新 check type（RulesAuditor）

现有 `path_exists` / `file_contains` / `json_path` 不变；它们仍 `resolvePath`，空 WorkDir 不得拿来跑这些 type（进门已拦）。

| type | 通过 | 失败 |
|------|------|------|
| `trace_hit_status` | `ToolHits` 中至少一条：`ToolName` ∈ {`es_log_query`,`execute_read`,`rca_grep`} 且 `Error==""` 且 `!Blocked` 且 `HitStatus` 为 `hits`/`empty`/`error` 之一 | 否则 incomplete |
| `empty_hit_speak` | 用 `ToolHits`+`FinalText` 拼出等价 `RunTrace`，`EvaluateEmptyHitSpeakGate` 的 `Allow==true` | `Allow==false` → incomplete，不得 proposed completed |

拼 trace：每条 `ToolHit` → `ToolCallRecord{ToolName, Result: map 含 hit_status/queried_index/repo, Error, Blocked}`。缺 `hit_status` 的条目与 B 闸 fail-open 一致。

混合契约：同一 `AcceptanceChecks` 里文件类与 trace 类可并存；任一条失败则整体 incomplete（与现网循环一致）。trace 类**不得**先 `resolvePath`（Path 为空时现网会 `empty path` → violation，会把 AutoChecks 整单打成违规）。空 WorkDir 只跑 trace 类必须能 Audit，不得因 WorkDir 空而 panic。

未知 type：仍走现网 `unknown check type` → 该条失败，不得当 pass。

无 checks：现网 RulesAuditor 已 incomplete+suspect。AutoChecks 不会产出空列表。

### 4.3 ApplyAudit

不改完成闸语义：`StatusCompleted` 仅当 `Completion==complete && Integrity==clean`。金样例必须覆盖：Executor 报告 `ClaimComplete=true`、审计 incomplete → 记录仍 `pending`。

---

## 5. 文件落点

| 路径 | 职责 |
|------|------|
| `portal/internal/chat/mea_autochecks.go` | `AutoChecks`、`ShouldUseMEA`（进门谓词，供 service 与单测） |
| `portal/internal/service/chat.go` | 在已有 `lock` 处调用 `ShouldUseMEA` |
| `portal/internal/service/mea_stream.go` | 填充 `FinalText`/`ToolHits` |
| `framework/mea/types.go` | `ToolHit`、`ExecutionReport` 新字段 |
| `framework/mea/rules_auditor.go` | 两 type；trace 类跳过 `resolvePath` |
| `framework/mea/apply.go` | 无行为变更则可不改；测试钉死 |
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_mea_no_fence`、`TestEvalGolden_mea_chat_skip` |
| `framework/mea/evalgolden_test.go` | `TestEvalGolden_mea_claim`、`TestEvalGolden_mea_empty_speak` |
| `scripts/industrial-eval.ps1` | 增加 `cd framework; go test ./mea -count=1 -run TestEvalGolden_` |

`framework/mea` 可 import `framework/agent` 以调用 `EvaluateEmptyHitSpeakGate`。禁止 `agent` import `mea`。

---

## 6. 测试与验收

单测，不绑现网 ES，不调付费 LLM。

| # | 检查 | 通过 |
|---|------|------|
| C1 | `AutoChecks("用 elasticsearch 查一下错误日志")` | len==2，type 顺序为 `trace_hit_status`、`empty_hit_speak` |
| C2 | `AutoChecks("你好")`、`AutoChecks("有哪些技能")`、`AutoChecks(bf26Q)` | nil / len==0 |
| C3 | 交付继承：Q=C1 那句，Delivery=「没有打印出来呀」 | `AutoChecks(lock.Q)` 非空；`AutoChecks(lock.Delivery)` 为空 |
| C4 | `ShouldApplyEvidenceGate(nil, goal)==false` 时 | 即使 MEAEnabled、Workspace 满，无围栏 → `useMEA` 逻辑为假（纯函数测合成+进门谓词，不必起 HTTP） |
| C5 | 仅 trace checks、Workspace `""` | 进门谓词为真（`traceOnly`） |
| C6 | 围栏含 `path_exists`、Workspace `""` | 进门谓词为假 |
| C7 | 围栏合法 checks | **不**被 AutoChecks 覆盖 |
| T-claim | `ApplyAudit`：pending 记录 + 审计 incomplete + 报告 ClaimComplete 真 | 状态仍 pending |
| T-speak | RulesAuditor：checks=AutoChecks 两条；ToolHits 一条 `es_log_query` `empty`；FinalText「该服务从未参与」 | Completion incomplete，无 completed 更新 |
| T-speak-ok | 同上但终答「该索引 0 条，不能据此说从未参与」且含 `vm-manager-*` | complete+clean 且可 proposed completed（TargetRecordID 非空时） |
| T-hit | 无 ToolHits / 只有缺 hit_status 的 hits | `trace_hit_status` 失败 |

`TestEvalGolden_mea_no_fence` = C1。  
`TestEvalGolden_mea_chat_skip` = C2。  
`TestEvalGolden_mea_claim` = T-claim。  
`TestEvalGolden_mea_empty_speak` = T-speak；T-speak-ok 作为**同一测试**的表驱动第二行，不必新 ID。

进门谓词抽成可测纯函数（例如 `ShouldUseMEA(...)`），避免只靠 SSE 测分流。

故意让 `ApplyAudit` 在 incomplete 时仍写 completed，或让 `AutoChecks("你好")` 返回两条 → 对应测试 FAIL。

回归：`SATH_MEA` 未设 + Agent 未勾 + 无围栏 → 不调用 `RunRulesMEA`（现有 `TestMEAEnabled*` 仍过）。`ClaimComplete` 金样例不得回退。

---

## 7. 错误处理与开关

| 情况 | 行为 |
|------|------|
| `SATH_MEA` 未设、未勾、非 pilot | 纯 ReAct；不跑 AutoChecks 也行（短路）。若先跑 AutoChecks 不得因此进 MEA |
| AutoChecks 异常 | 不进 MEA；日志 `mea_autochecks=error` |
| 审计缺 FinalText/ToolHits | trace check 失败（incomplete），不得 completed |
| 与 B 终答闸同轮 | ReAct 内环仍跑 `EvaluateEmptyHitSpeakGate`；外环再检一次。允许双重；外环以报告里的终答为准 |
| 与凭据闸 | 不改 `HasSuccessfulBoundEvidence` |
| M1 LLM | Checks 非空时 Cascade 仍先 Rules；T-speak 失败不得被 LLM 改成 complete |

---

## 8. 与父规格 / A / B / M0

- 不推翻 MEA 默关；可机检 = 围栏非空 **或** AutoChecks 非空。
- B 的 `hit_status` 由 C 在审计中消费，不改三个工具的盖章。
- A 扩面：`mea_no_fence` / `mea_chat_skip` / `mea_claim` / `mea_empty_speak`；不另起名单。实现计划可在 [industrial-eval §7](./2026-08-25-industrial-eval-design.md) 表里加四行（文档同步，不是新框架）。
- M0 `ApplyAudit` 完成闸保留；C 只补入口与 trace 审计。

---

## 9. 验收清单（本文自身）

- [ ] 实现者能写出无围栏调查题如何进 MEA，而不去默全开。
- [ ] 读者不会把本文当成「入口去读 hit_status」。
- [ ] 「你好」不会进外环；Executor 自报不能 completed。
- [ ] 下一份是 C 的 implementation plan，且引用 `TestEvalGolden_mea_no_fence` 与 `TestEvalGolden_mea_claim`。
