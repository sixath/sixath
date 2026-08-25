# Code Intel Remainders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this session) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补上 P0–P4 之后仍值得做的四项：L2 摘要钉 CFG、禁止 MEMORY/txt 顶替源码、跨仓入边、code 族换模型。

**Architecture:** 钉针走 `model.PrepareChatContextCtx`（摘要前抽 `control_flow`/`call_graph` 进 leading system）；另两闸挂既有 `checkCodeClaimGate`；跨仓在 `rca_symbol references` 上对其它 roots 做 grep；模型路由对标 `SATH_VISION_*`，仅在 `FamilyCode` 激活时替换 ReAct 模型。

**Tech Stack:** Go（`framework/`、`portal/`），既有 ReAct 机检与 RCA 工具契约。

**Spec:** `docs/superpowers/specs/2026-08-20-code-intel-cursor-parity-design.md` §L0 / §L2 / §L3

**不做：** 提交 git；改 Web 表单；Python/Java AST；全仓图数据库；proto 字段（本期 env + Agent JSON `code_*`）。

---

## File Structure

| 文件 | 责任 |
|---|---|
| `framework/model/metadata_sixath.go` | 新增 `OriginCodePin` |
| `framework/model/context_code_pin.go` | 从 tool JSON 抽出最后 N 次 `rca_read` 路径表，注入 leading system |
| `framework/model/context_pipeline.go` | pre-prune 之后、L0 之前调用 pin |
| `framework/model/snip_compact.go` | pin 消息不可 snip |
| `framework/agent/surrogate_source_gate.go` | MEMORY / `*.txt` 顶替源码 → 回压 |
| `framework/agent/react_agent.go` | cascade 里接 surrogate 闸 |
| `framework/tool/rca_symbol_tool.go` | references 扫其它 roots |
| `portal/internal/chat/code_model.go` | `SATH_CODE_*` + Agent `code_model` |
| `portal/internal/service/chat.go` | FamilyCode 时替换 ReAct 模型 |
| `portal/internal/chat/code_analysis_prompt.go` | 提示：跨仓 callers、禁止 txt |

---

### Task 1: L2/L0 钉住 CFG（P5）

**Files:**
- Modify: `framework/model/metadata_sixath.go`
- Create: `framework/model/context_code_pin.go`
- Create: `framework/model/context_code_pin_test.go`
- Modify: `framework/model/context_pipeline.go`
- Modify: `framework/model/snip_compact.go`
- Modify: `framework/model/l2_runtime_test.go`

- [x] **Step 1: 失败测试** — 含 `control_flow` 的 tool 消息经 L2 摘要后，leading system 仍含 `when` / `InsertUnionUserAreaInfo`
- [x] **Step 2: 实现** `ensureCodePinMessages`：解析 `{"tool":"rca_read","result":{control_flow,call_graph,file,...}}`，最多 3 次、8000 runes，origin=`code_pin`
- [x] **Step 3: pipeline** 在 pre-prune 之后、L0 之前插入；snip 保护 `OriginCodePin`
- [x] **Step 4:** `go test ./model/...`

验收：窄窗口读过的路径表在摘要后仍可见，不融进散文。

---

### Task 2: MEMORY/txt 机检（P6）

**Files:**
- Create: `framework/agent/surrogate_source_gate.go`
- Create: `framework/agent/surrogate_source_gate_test.go`
- Modify: `framework/agent/react_agent.go`（`checkCodeClaimGate`）
- Modify: `portal/internal/chat/code_analysis_prompt.go`

- [x] **Step 1: 失败测试**
  - 只用 `read_file(MEMORY.md)` / `*.txt` 却讲「整体流程 / 会调用」→ 不放行
  - 有 `rca_read` 的 `.go` 且终答不引用 txt → 放行
  - 同时读了 txt 且正文点名 `MEMORY.md` → 不放行
- [x] **Step 2: 实现** `EvaluateSurrogateSourceGate`
- [x] **Step 3: 接入** inbound 之后、quote cascade 之前
- [x] **Step 4:** `go test ./agent/...`

---

### Task 3: 跨仓入边（P7）

**Files:**
- Modify: `framework/tool/rca_symbol_tool.go`
- Modify: `framework/tool/rca_symbol_tool_test.go`
- Modify: `framework/agent/inbound_gate.go`（prompt 提到其它仓）
- Modify: `portal/internal/chat/code_analysis_prompt.go`

- [x] **Step 1: 失败测试** — 两仓 roots，仓 A 声明函数、仓 B 调用；`references` 的 `callers` 含 B，且有 `repos_scanned`
- [x] **Step 2: 实现** gopls 当前仓 + 其它仓 `grepSymbolCallers`；`inbound_empty` 看全集；`callers[].repo` 用 location 所属仓
- [x] **Step 3:** `go test ./tool/...`

---

### Task 4: code 族模型（P8 / L0）

**Files:**
- Create: `portal/internal/chat/code_model.go`
- Create: `portal/internal/chat/code_model_test.go`
- Modify: `portal/internal/biz/agent.go`（`CodeProvider/Model/APIKey/BaseURL`）
- Modify: `portal/internal/data/agent_mysql.go`（JSON `code_*`）
- Modify: `portal/internal/service/agent.go`（Update 时保留已有 `code_*`）
- Modify: `portal/internal/service/chat.go`（SendMessage / Stream）

- [x] **Step 1: 测试** `ResolveTurnModel`：非 code 族保持会话模型；code 族优先 env，其次 Agent JSON；构建失败 fail-open
- [x] **Step 2: 实现** env：`SATH_CODE_PROVIDER` / `SATH_CODE_MODEL` / `SATH_CODE_API_KEY` / `SATH_CODE_BASE_URL`（对标 vision）
- [x] **Step 3: 接线** `PrepareTurnToolSurface` 之后替换 ReAct 用的 `m`
- [x] **Step 4:** `go test ./internal/chat/...`（在 `portal/`）

绑定方式（运维）：设 env 或在 Agent `model_config` JSON 写 `code_model` / `code_provider`。本期不上表单。

---

## 验收对照

| 项 | 必须 |
|---|---|
| 长会话 L2 | 摘要后仍能看到 `control_flow.when` |
| c7aa 类 | 读 MEMORY/txt 当源码 → 回压 |
| 跨仓 | `references` 返回其它 root 的 caller |
| 换模型 | code 族走 `SATH_CODE_MODEL`；闲聊仍会话模型 |
