# 工业级 Agent 程序总规格（值班地板 + 编码 Harness）

**日期**: 2026-08-25  
**状态**: 待确认  
**文档角色**: **程序总规格**。只定目标、非目标、切片顺序、切片接口、成功标准。  
**禁止**：用本文直接开一份覆盖 A–E 的实现计划。每个切片另写 spec + plan。  
**动机**: 现网已有 ReAct、工具族、任务锁 G/D、凭据闸、声称闸、Go CFG、code pin、MEA M0/M1（默关）。e9d4 / bf26 / 8555 / c304 证明能力在增加、**可检验性质没锁住**。用户要求补齐工业级能力，并把成功标准从「RCA 值班地板」抬到 **接近 Cursor / Claude Code 的编码 Harness**（产品形态仍是 Portal 聊天 Agent，不是桌面 IDE）。

**对照**:
- Harness 脊柱：[2026-07-11-harness-engineering-gap-design.md](./2026-07-11-harness-engineering-gap-design.md)
- 源码五层：[2026-08-20-code-intel-cursor-parity-design.md](./2026-08-20-code-intel-cursor-parity-design.md)
- 工具族合理性：[2026-08-20-tool-call-reasonableness-design.md](./2026-08-20-tool-call-reasonableness-design.md)
- 任务锁 / Goal-Delivery：[2026-08-24-task-lock-goal-drift-design.md](./2026-08-24-task-lock-goal-drift-design.md)、[2026-08-25-goal-lock-delivery-design.md](./2026-08-25-goal-lock-delivery-design.md)
- MEA：[2026-08-12-mea-minimal-subset-design.md](./2026-08-12-mea-minimal-subset-design.md)、[2026-08-13-mea-m1-llm-auditor-design.md](./2026-08-13-mea-m1-llm-auditor-design.md)
- 任务入口现状：[2026-08-15-task-handling-current-design.md](./2026-08-15-task-handling-current-design.md)

**一句话**：工业级 = 评测网锁住现有闸门 + 证据不可撒谎 + 长任务不能自报完成 + 生产默认拒绝写，**并且** code 族在模型/观察/导航/上下文/核对/改测循环上接近 Cursor，而不是再堆工具或每步 LLM 裁判。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 产品形态 | Portal 聊天 + 企微；**不是** Cursor 桌面 / VS Code fork |
| 工业级口径 | **两条轨**：值班地板（A–D）+ 编码 Harness（E）。缺地板则编码轨不可信；缺 E 则达不到「接近 Cursor」 |
| 主循环 | 继续 ReAct；MEA 仍是外环。不替换、不每步 LLM 判题、不关现网闸门 |
| 证明场 | RCA / 源码分析（D 口径第一客户）同时作为编码轨的金样例来源 |
| 实现粒度 | 本文不写文件级改动。下一份实现 spec **必须是切片 A**。E0（code 模型强制）若要并行，**另写 spec**，不得塞进 A 的 plan |
| 权威 | 用户调查目标 G > 本轮交付 D > Skill/工具。编码任务的 G 是用户要分析/修改的问题，不是 Skill 目录 |

---

## 1. 为什么这样算工业级（论证边界）

对企业 Agent，「工业级」在此程序里是六条**可检验性质**，不是模型体感：

| ID | 性质 | 没有它时的现网症状 |
|----|------|-------------------|
| P-eval | 改 harness 必有红灯 | 闸门互伤只能翻会话（e9d4 vs bf26） |
| P-nospeak | 空证据不能说成「不存在」 | ES 0 击写成从未参与 |
| P-no-self-complete | 完成态不来自 Executor 自报 | 长任务假完成进入下一前提 |
| P-deny-write | 写路径默认拒绝 | executor 零值放行写 |
| P-see | code 族第一次检索就看见门和包围函数 | grep 单行 → 脑补 happy path（c304） |
| P-navigate | 入边/工作集是工具结果 | 停在第一击（c7aa）；txt 顶替源码 |

A–D 锁住 P-eval … P-deny-write。E 锁住 P-see / P-navigate，并加上 **改文件 + 跑测试由 audit 验收**（接近 Claude Code 的循环，而不是只读 RCA）。

本文**不声称**达到与 Cursor 同等判断力。接近的必要条件仍是同级代码模型（E0）；工具再好，弱模型在无 CFG 语言和业务隐喻上仍会输。见 [code-intel §诚实上限](./2026-08-20-code-intel-cursor-parity-design.md)。

---

## 2. 目标与非目标

### 2.1 程序目标（抬高后）

1. **现网闸门可回归**：e9d4 / bf26 / c304 / 8555 的关键不变量有仓库内 replay，CI 能红。
2. **结案不可撒谎**：工具 JSON 区分未查到 / 有击 / 错索引；终答不得把 0 击写成「服务从未参与」。
3. **长任务不可自报完成**：可机检任务走 MEA；`completed` 只来自 audit。
4. **生产默认可值班**：SQL/文件写 opt-in；RCA 结案缺约定证据类型则不得结束。
5. **接近 Cursor 的看见与导航**（Go + 已挂 code roots）：工作集是 `*.go` 不是 workspace txt；grep 带上下文；read 默认整函数；定位后有入边或显式 `inbound_empty`；pin 保留 content + file/repo。
6. **接近 Claude Code 的改测循环**（工作区文件任务）：`write_file`/`patch` + `terminal` 测试的完成，须经 MEA/机检，不得只靠模型说「已改好」。

### 2.2 非目标（本程序全集不做）

- 桌面 IDE、17 个 IM、Trajectory→RL、每步 LLM 裁判、关掉任务锁/凭据闸/工具族。
- 用 MEA 重写 Growth / Procedural；Manager/Executor/Auditor 换成 Claude Code 二进制。
- 一期 Python/Java AST、全仓图数据库、符号执行。
- 保证 ES 索引名永远正确（Skill 正文纠正可作 B 的随附，不是「索引永真」）。
- 声称闸改为读 pin（仍只认本轮 `ToolCalls`）。
- 把 Core 族再塞更多永远可见的工具来「接近 Cursor」。

### 2.3 诚实上限

- 同族仍可能搜错符号（8555）；硬规则按表名/skill 名黑名单会误伤，不进本程序。
- 交付句继承仍是启发式（Goal/Delivery 规格已接受）。
- pin 有 rune 预算，不保证整文件永在上下文。
- 无 CFG 的语言：结构正确性优势 fail-open，判断力回到模型。
- 未配置同级代码模型时，E 的「接近 Cursor」条款视为**未达标**，不得靠提示词宣称达标。

---

## 3. 两条轨与切片

```text
轨 1 值班地板          轨 2 编码 Harness（对标 Cursor 五层 + CC 改测）
A 评测网 ─────────────► 夹具同时覆盖 RCA 会话与源码会话
        │
        ▼
B 证据契约 ───────────► 工具 JSON 字段供 A 断言、C 当 checks
        │
        ▼
C MEA 产品化 ─────────► 已开 MEA 时，可机检任务无需手写 fence
        │
        ▼
D 值班默认 ───────────► 写 deny-by-default + 结案证据门 + obs
                          │
E 与 A 并行启动 E0（code 族强制强模型），E1–E3 在 A 有夹具后合入
```

| ID | 名称 | 锁住的性质 | 下一份文档 |
|----|------|------------|------------|
| **A** | 黄金会话评测 | P-eval | `…-industrial-eval-design.md`（**第一个实现 spec**） |
| **B** | 证据语义 | P-nospeak | 在 A 绿之后 |
| **C** | MEA 产品化 | P-no-self-complete | 不默全开；已开 MEA 且可机检时无需手写 fence |
| **D** | 值班底座 | P-deny-write | 新闸必须带 A 用例 |
| **E** | 编码 Harness | P-see / P-navigate + 改测 | 可与 A 并行开 E0；E1 起须有 A 夹具 |

E 内子切片（对应 code-intel 五层，**不在本文展开实现**）：

| 子 ID | 层 | 程序要求（验收口径） | 现网锚点 |
|-------|----|----------------------|----------|
| E0 | 模型 | `FamilyCode` 激活时 **必须** 走已配置的 code 模型；未配置则本轮失败可见（不得静默落到对话模型还声称在做源码分析） | `portal/internal/chat/code_model.go`（现为可选） |
| E1 | 观察 | grep 默认上下文；`rca_read` 窗口在函数内则 content 扩到函数 span | `rca_grep` 已有 `context`；扩函数是否默认仍须在 E spec 核对 |
| E2 | 导航 | 工作集卡每轮注入；读函数后未 `references` 则 open_questions 非空且终答不得称「整体流程」 | `framework/agent/code_workset.go` |
| E3 | 上下文 | pin 含 content + file/repo；MEMORY/txt 不得顶替源码 | `context_code_pin.go`、`surrogate_source_gate.go` |
| E4 | 核对 | 声称闸 + Go CFG when；场景×路径（1105→不得 Insert）按 code-intel P3 | 现网薄机检；P3 未作为默认 |
| E5 | 改测 | 工作区「改代码并证明」类任务：patch/write + 测试命令的 exit code 由 C 的 audit 写回 | `write_file`/`patch`/`terminal` 已有确认雏形 |

E0 可与 A **并行**（配置/路由，不依赖夹具）。E1–E5 合入必须带 A 的对应金样例，禁止无测加闸。

---

## 4. 切片接口（总规格要写死的架构）

唯一跨切片数据契约。实现 spec 不得另起一套平行字段。

```text
RunTrace / 闸门 Decision / 终答
        │ 录制
        ▼
A 夹具（仓库内 JSON 或 Go testdata）
        │ 断言
        ▼
工具结果 JSON ── B 增加 hit_status, queried_index|repo, empty_reason
        │ 被引用
        ▼
C mea-checks / Rules Auditor（机检这些字段，而不是模型散文）
        │
        ▼
D 结案门（缺约定证据类型 → Finish 拒绝或 Retry）
```

**字段最低约定**（B 的实现 spec 可加枚举，不得删）：

| 字段 | 谁写入 | 谁消费 |
|------|--------|--------|
| `hit_status`：`hits` / `empty` / `error` | ES / SQL / grep 类工具 | A 断言、C checks、D 结案、终答机检 |
| `queried_index` 或 `repo` | 查询/读代码工具 | A、D；错索引可观测 |
| `inbound_empty` | `rca_symbol` references | E2、A（c7aa） |

A 的夹具**禁止**依赖 live LLM。允许：固定 `RunTrace` + 闸门纯函数；可选「假模型」只回预定 tool_calls。

**A 合入范围（写死）**：只断言**现网已有**的 `RunTrace` / 闸门 `Decision` / 终答约束。`hit_status`、`queried_index`、`inbound_empty` 等由 B / E2 写入后再往**同一夹具**加断言。禁止为了「提前消费 §4 契约」在 A 里改工具 JSON。

---

## 5. 每切片成功标准（给后续 spec 用，不是实现步骤）

### A 评测

- 仓库命令（模块目录，禁止根 `go test ./portal/...`）能跑通金样例包。
- 至少覆盖：bf26、e9d4、c304、8555（见切片 A spec）。四个不是上限；扩面规则见 [industrial-eval §7](./2026-08-25-industrial-eval-design.md)。
- c7aa（入边 / `inbound_empty`）**不是** A 最低覆盖；A 可为 c7aa 留脱敏夹具骨架，变绿是 E2 合入条件。
- 故意破坏一处闸门实现时，对应用例必须 FAIL。

### B 证据

- `es_log_query` / `execute_read` / `rca_grep` 在 0 击时 `hit_status=empty`，且存在一条机检：终答含「从未参与 / 服务不存在」类断言则 Retry 或拒绝（措辞表在 B spec 写死）。
- 查询结果带实际 `queried_index` 或等价字段，夹具能断言「请求了 `vm-manager-*`」。

### C MEA

- **不推翻 MEA 默关**：全局默认仍关。仅当 Agent 已开 MEA（或 `SATH_MEA` / pilot）**且**任务可机检时进入外环。
- 可机检 = 非空 `mea-checks` **或** 系统从 B 字段自动生成 checks。此时**无需**用户手写 fence 才算有验收。闲聊、无可机检契约 → 不进 MEA。
- Executor 自报成功不得把 requirement 标 `completed`。
- `SATH_MEA=0` 且 Agent 未开 MEA 时行为与现网纯 ReAct 一致。

### D 值班

- `execute_write` / 等价写：配置默认拒绝，须显式 opt-in 或 HITL。
- RCA 结案门：本轮无成功证据工具（与 Goal/Delivery 证据名单一致）且用户问题是调查题 → 不得 idle 成终答结案（与「缺证据则回压」一致；具体阈值在 D spec）。
- datasource/executor 关键路径有结构化日志字段（index/repo/hit_status），能支撑 A 的录制。

### E 编码 Harness

- E0：仅绑对话模型、未配 code 模型时，code 主族回合必须对用户可见失败，而不是静默用弱模型分析源码。
- E1–E3：c304 夹具在 A 中为绿（when 或整函数 content）。c7aa 在 E2 合入后为绿（工作集 `*.go`、入边或 `inbound_empty`）。
- E5：至少一条「改测试文件 + 跑 go test」的 MEA 金样例：测试失败则不得 completed。

---

## 6. 错误处理与开关

| 情况 | 行为 |
|------|------|
| A 夹具与现网闸冲突 | 以已确认的 Goal/Delivery、任务锁、工具族规格为准，改夹具或改实现，禁止静默跳过 |
| B 字段缺失（旧工具） | fail-open 本轮 + 日志；不得把缺失当成 `hits` |
| C 自动 checks 生成失败 | 不进 MEA，走纯 ReAct（与现网「无有效验收」一致），并打点 |
| E0 未配置 code 模型 | 可见错误；禁止假装源码分析成功 |
| D 结案门误伤短问答 | 仅当 `HasPriorAssistant` 且 G 像调查题且本轮声称结案时开火（细节 D spec） |

---

## 7. 测试策略（程序级）

- **A 是唯一允许「先写实现 spec」的切片**；其后每切片的 plan 必须引用 A 的用例名。
- 单测为主；不强制 CI 调付费 LLM。
- 禁止把 `_neo4j_q/` 会话 JSON（含密钥）提交进仓库。夹具须脱敏：只留 tool 名、参数形状、hit_status、终答片段。

---

## 8. 后续文档顺序

1. 本文用户确认后 → **只开 A** 的实现 spec + plan。  
2. E0 若并行：单独 spec，不并入 A plan。A 合入后：**B** 与 **E1**（E1 合入须 A 已有 c304 夹具）。  
3. **C**（依赖 B 字段稳定）。  
4. **D** 与 **E2–E5**（新闸带 A 用例）。

---

## 9. 验收清单（本文自身）

- [ ] 读者能说出「工业级」在本程序里是六条性质，不是工具清单。
- [ ] 读者不会把本文当成一份可编码的大 plan。
- [ ] 下一份实现文档只能是 A，除非修订本文。
