# 切片 A：黄金会话评测网

**日期**: 2026-08-25  
**状态**: 待确认  
**父规格**: [2026-08-25-industrial-agent-program-design.md](./2026-08-25-industrial-agent-program-design.md)  
**下一份**: 实现计划 `docs/superpowers/plans/2026-08-25-industrial-eval.md`  
**禁止**: 改工具 JSON 加 `hit_status`；做 live LLM；实现 B/C/D/E1–E5；把 c7aa 标为必须绿；新建 `evalgolden/` 子包。

**一句话**：用仓库内夹具调用**现网闸门纯函数**，把 bf26 / e9d4 / c304 / 8555 的不变量变成 `TestEvalGolden_*` + 一条脚本；夹具脱敏，不含密钥。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 运行方式 | `go test` 两包，无付费模型 |
| 夹具形态 | Go 结构体；**不**引入 JSON schema |
| 断言对象 | 现网函数（见 §2） |
| 文件 | 同包测试文件，**不是**子目录包 |
| c7aa | `framework/agent/evalgolden_test.go` 里 `t.Skip("E2 inbound")`，E2 同文件变绿 |

---

## 1. 目标与非目标

### 目标

1. `scripts/industrial-eval.ps1` 跑完 A（内部即 §4 两条命令）。
2. 四条不变量各有 `TestEvalGolden_<ID>`；破坏对应生产函数必须 FAIL。
3. 后续 B/E 往**同名测试或同文件**加断言。

### 非目标

- SSE / 整段 ReAct 回放。
- `_neo4j_q/` 或 API key。
- 统一 eval 二进制。
- 断言 `hit_status` / `inbound_empty`。

---

## 2. 用例

| ID | 不变量 | 生产函数 | 夹具（写死） |
|----|--------|----------|--------------|
| `bf26` | `Q==bf26Q` 且 `Delivery==""` | `BuildTurnTaskLock` | 与 `TestBuildTurnTaskLock_bf26QUnchanged` 相同 hist |
| `e9d4` | redirect `ok==false` | `credentialSolicitationRedirect` | 与 `TestCredentialSolicitationRedirect_skipsWhenEvidenceExists` 相同：真索取句 + 成功 `es_log_query` + 绑定 catalog |
| `c304` | `Allow==false` | `EvaluateScenarioPathGate` | 与 `TestEvaluateScenarioPathGate_1105WriteWithoutCallName` 相同：`helperSource()` + 1105 问 + 「会把 UID 写入本地映射表」 |
| `8555` | `Decision==PostModelRetry` 且 `Reason=="family_dropped_all"` | `TurnIntentGate.Evaluate` | 对齐 `TestTurnIntentGate_OnlySkillViewRetries`：`ActiveFamilies={core,code}`（**非 nil**），仅 `skill_view` |
| `c7aa` | Skip | — | 文件：`framework/agent/evalgolden_test.go`；注释：E2 在本测试去掉 Skip 并断言入边 |

禁止复制第二套闸门实现。可薄封装上述现有夹具。

---

## 3. 文件落点（唯一）

| 路径 | `package` | 测试 |
|------|-----------|------|
| `portal/internal/chat/evalgolden_test.go` | `chat` | `TestEvalGolden_bf26`、`TestEvalGolden_8555` |
| `framework/agent/evalgolden_test.go` | `agent` | `TestEvalGolden_e9d4`、`TestEvalGolden_c304`、`TestEvalGolden_c7aa`（Skip） |
| `scripts/industrial-eval.ps1` | — | 只跑 §4 两条 `go test` |

不要创建 `evalgolden/` 目录包（调不到未导出的 `credentialSolicitationRedirect`）。

---

## 4. 成功标准

```
cd portal; go test ./internal/chat -count=1 -run TestEvalGolden_
cd framework; go test ./agent -count=1 -run TestEvalGolden_
```

两者 PASS（c7aa 计为 Skip 仍 ok）。故意让 `BuildTurnTaskLock` 把 bf26 的 Q 换成末句 → `TestEvalGolden_bf26` FAIL。

---

## 5. 错误处理

| 情况 | 行为 |
|------|------|
| 与 Goal/Delivery 冲突 | 改夹具或实现，禁止 Skip bf26/e9d4 |
| SQLITE flake | `-run TestEvalGolden_` 不含 session index |
| c7aa | 仅此条允许 Skip |

---

## 6. 与父规格

A 只断言现网 Decision/trace/闸门。`hit_status` 留给 B。

---

## 7. 扩面（四个钉子不是上限）

A **不追求代码覆盖率**。更多场景按事故类型追加，入口仍是 `-run TestEvalGolden_`。

**流水线：** 定性（改题 / 凭据 / 证据撒谎 / 错族 / 源码胡说 / 假完成）→ 脱敏最小夹具（用户句 + 工具成败 + 终答片段；禁止 `_neo4j_q/` 与密钥）→ 调用现网纯函数 → `TestEvalGolden_<短名>` → 破坏对应生产函数必须红。

**能压成纯函数的，继续留在 A。** 需要新字段的往**同名测试**加断言，不另起平行名单。单步闸门都绿、整轮仍傻，才上固定 `tool_calls` + 假模型。live LLM 不进 A 的 CI。

**一期不实现、可后续追加的 ID（现网已有规格或测试）：**

| ID | 锁什么 | 何时 |
|----|--------|------|
| `delivery` | 催打印继承 G，不把 D 当新题 | Goal/Delivery 已有 `inheritDeliveryChain` |
| `new_flow` | 另一条 / 再查+新 opaque → 换 G | 已有 `newFlow` / `newOpaque` 测试 |
| `cred_deny` | 否定句「未索取连接」不得 redirect | 已有 Denial 测试 |
| `b4` | 调查题只调 `skills_list` → `goal_drift` | 已有 B4 测试 |
| `empty_hit` | 0 击不得说「从未参与」 | **B**（[industrial-evidence](./2026-08-25-industrial-evidence-design.md)） |
| `c7aa` | 入边 / `inbound_empty` | **E2**（本期 Skip） |
| `mea_no_fence` | 调查题无围栏 → AutoChecks 两条 | **C** |
| `mea_chat_skip` | 你好 / bf26Q → 空 checks | **C** |
| `mea_claim` | incomplete 审计不得 completed | **C** |
| `mea_empty_speak` | 空击「从未参与」→ incomplete | **C** |
| `deny_write` | 零值 INSERT 拒写；pending 不 Exec；files 默认关 | **D** |
| `close_gate` | 调查题无 jaeger/es ref → inject；空击 ES 过闸 | **D** |
| `close_gate_chat` | 你好 / bf26Q 不开结案门 | **D** |
| `obs_hit` | StampHitContract 打出 hit_status + queried_index | **D** |
| `code_model` | FamilyCode 缺配不得用对话模型 | **E0** |

不要把仓库里所有 `*_test.go` 改名前缀冒充金样例。

