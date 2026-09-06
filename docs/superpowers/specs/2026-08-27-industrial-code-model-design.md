# 切片 E0：code 族强制代码模型

**日期**: 2026-08-27  
**状态**: 已确认（2026-08-27）  
**父规格**: [2026-08-25-industrial-agent-program-design.md](./2026-08-25-industrial-agent-program-design.md)  
**对照（现网，要改掉的 fail-open）**: [2026-08-20-code-intel-cursor-parity-design.md](./2026-08-20-code-intel-cursor-parity-design.md) 诚实上限；`portal/internal/chat/code_model.go`  
**评测网**: [2026-08-25-industrial-eval-design.md](./2026-08-25-industrial-eval-design.md)  
**下一份**: `docs/superpowers/plans/2026-08-27-industrial-code-model.md`  
**禁止**: 实现 E1–E5；改 `FamilyCode` 关键词表；默全开 MEA；改声称闸读 pin；改正 Skill 索引；live LLM 进 CI；新建平行评测框架；把缺配改成「警告但仍用对话模型」。

**一句话**：`FamilyCode` 激活时必须换到已配置的 code 模型；没配或构建失败则本轮对用户可见失败，禁止静默用对话模型做源码分析。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 锁住的性质 | 父规格 **P-see** 的模型层（E0）。不锁观察/导航/pin |
| 开火 | `FamilyActive(active, FamilyCode)==true`。非 code 族、「你好」不变 |
| 已配置 | `resolveCodeModelSpec` 之后 **`Model` 非空**（来自 Agent `code_model` / 全局 UI / `SATH_CODE_MODEL`） |
| 禁止继承 | **不得**把会话 `ModelConfig.Model` 填进 code 模型名（现网 `ResolveTurnModel` 70–72 行是静默同名） |
| 失败 | 缺 `Model` 或 `BuildModel` 失败 → 返回 error，**不**跑 ReAct / MEA |
| 工具面关 | `active==nil`（`SATH_TURN_TOOL_SURFACE` off）→ **不开火**，仍用会话模型 |
| 金样例 | `TestEvalGolden_code_model` |

---

## 1. 目标与非目标

### 目标

1. `FamilyCode` 未激活（含 `active==nil`、仅 core/RCA 等）：`ResolveTurnModel` 返回原会话模型，`error==nil`。
2. `FamilyCode` 激活且 code spec 的 `Model` 非空：必须 `BuildModel` 得到**另一实例**（允许与会话模型同名，只要名字来自 code spec，不是从会话模型字段抄来的）。成功则用该实例跑本轮。
3. `FamilyCode` 激活且 code spec `Model==""`（Agent / 全局 / env 都没模型名）：返回哨兵错误，**禁止**返回会话模型。
4. `FamilyCode` 激活且 `BuildModel` 失败或返回 nil：返回哨兵错误，**禁止** fail-open 回会话模型。
5. `SendMessage` 与 `SendMessageStream` 两处都处理该 error：HTTP/SSE 对用户可见，文案见 §4。不进入 `BuildReActAgent`。
6. 现网 `TestResolveTurnModel_noSpecKeepsChat` **改为**断言 error（或删除并由金样例取代）。缺配保 chat 必须红。
7. `TestEvalGolden_code_model` 挂 A 前缀；`scripts/industrial-eval.ps1` 已跑 `./internal/chat`，不必新包。

### 非目标

- E1 grep 上下文、E2 `inbound_empty` / 把 c7aa 标绿、E3 pin、E4 CFG 声称闸、E5 改测。
- 判断 code 模型是否「同级强模型」（无法机检）。E0 只保证**选了配置里的 code 模型**，不保证判断力。
- 为救「源码题打成 RCA 族」另起关键词表。
- 工具面关闭时对全量绑定强制 code 模型（会误伤闲聊）。
- 改 `AppendCodeAnalysisPrompt` 正文来假装已换模型。

### 诚实上限

- 开火完全跟现网 `PrepareTurnToolSurface` 的 `FamilyCode`。无「源码 / 代码分析」等词的句子不会激活，E0 不拦。不要在本期扩 `familyKeywords`。
- `SATH_TURN_TOOL_SURFACE` 关闭时 `active==nil`，E0 不开火。关工具面的部署仍可能用对话模型做源码分析。
- 用户把 `code_model` 配成与对话模型**同一字符串**时，E0 视为已配置（显式选择）。禁止的是 **没配 code 模型名却拿会话模型顶上**。
- 未配同级代码模型时，父规格 E 的「接近 Cursor」条款仍视为**未达标**；E0 只阻止假装。

---

## 2. 现网与本期差异

现网（必须改掉）：

```text
FamilyCode 且 !Usable()     → 返回 chatModel
FamilyCode 且 Build 失败    → 返回 chatModel
modelName 空                → 用 meta.ModelConfig.Model（会话模型名）
Usable()                    → Model 或 APIKey 或 BaseURL 任一非空
```

本期：

```text
!FamilyActive(FamilyCode)   → (chatModel, nil)
FamilyCode && spec.Model=="" → (nil, ErrCodeModelRequired)
FamilyCode && Build 失败     → (nil, ErrCodeModelBuild)
FamilyCode && Build 成功     → (codeModel, nil)
```

`Provider` / `APIKey` / `BaseURL` 仍可从会话 `ModelConfig` 继承（同一网关）。**只有模型名不得从会话 `Model` 字段继承。**

`Usable()`：E0 换模型与叠 env **都不用它**。设置页其它逻辑可不改。已配置 = `strings.TrimSpace(spec.Model) != ""`。

---

## 3. API 与接线

函数：`ResolveTurnModel(active map[string]struct{}, chatModel model.Model, meta biz.AgentMeta) (model.Model, error)`

- `chatModel==nil`：与现网一样返回 `(nil, nil)`（调用方已先建会话模型；不必为 nil chat 新造错误）。
- 哨兵（`errors.Is` 可测）：
  - `ErrCodeModelRequired`：缺 code 模型名
  - `ErrCodeModelBuild`：`BuildModel` 失败或 nil（`Unwrap` 保留底层 err）

`portal/internal/service/chat.go` 两处（约 L368、L630）：

```go
m, err = chat.ResolveTurnModel(active, m, *agentMeta)
if err != nil {
    // 打日志（不要打 api_key）
    return … err  // Stream 为 return nil, "", err
}
```

禁止 catch 后改回 `m = chatModel`。禁止只改一处。

`resolveCodeModelSpec` 的 overlay 顺序不变：全局 →（全局 **Model 空** 则叠 env）→ Agent 字段覆盖。叠 env 的条件必须是 **`base.Model==""`**，禁止继续用 `Usable()`（否则「全局只配了 APIKey、env 里有 `SATH_CODE_MODEL`」会被当成已配置而**不再读 env**，变成 `ErrCodeModelRequired`）。`Usable()` 可留着给设置页/其它调用；E0 换模型与叠 env **都不**用它。不要改 `code_model_settings.go` 的存取语义。

---

## 4. 用户可见文案（写死结构）

中文为主，须同时让运维知道去哪配。**禁止**把 key、完整 BaseURL 查询串打进用户正文。

`ErrCodeModelRequired`（可含英文关键词便于检索）：

```text
本轮是源码分析，需要配置 code 模型后才能继续，不会用对话模型代替。
请在 Agent 的 code_model、门户全局 code 模型，或环境变量 SATH_CODE_MODEL 中填写模型名。
```

`ErrCodeModelBuild`：上段第一句改为「code 模型创建失败」+ `err.Error()` 的**截断**（现网 `BuildModel` 错误即可，不要拼 api_key）。

接线：与现网 `BuildModel` 失败一样，`SendMessage` / `SendMessageStream` **直接 `return err`**。非流式走现网 `errorEncoder` 的 `Message`；SSE 走 `err.Error()`。不必另做 HTTP 映射层。哨兵的 `Error()` 即 §4 文案。`ErrCodeModelBuild` 追加底层错误时截断写死为 **200 rune**（plan 按此实现）。

SSE 不发一条「正在分析源码」的 assistant delta。

---

## 5. 文件落点

| 路径 | 职责 |
|------|------|
| `portal/internal/chat/code_model.go` | 签名改返回 error；去掉会话模型名回填；哨兵 |
| `portal/internal/chat/code_model_test.go` | `noSpecKeepsChat` 改为期望 error；非 code 族仍保 chat |
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_code_model` |
| `portal/internal/service/chat.go` | 两处处理 error |
| [industrial-eval §7](./2026-08-25-industrial-eval-design.md) | 加一行 `code_model` |

不要新建 `evalgolden/` 包。不要改 `framework/agent`。`scripts/industrial-eval.ps1` 已 `go test ./internal/chat -run TestEvalGolden_`，**不必**加新包行。

---

## 6. 测试与验收

单测，不调付费 LLM。`BuildModel` 可用无效端口 URL（现网 `codeFamilyUsesAgentSpec` 已如此）证明「有 Model 名则尝试构建」，不要对真实 API 发请求。

| ID | 检查 | 通过 |
|----|------|------|
| `code_model` 缺配 | `FamilyCode` + 空全局/env/Agent `CodeModel` | `errors.Is(err, ErrCodeModelRequired)`，返回的 model 不是 chat stub（应为 nil） |
| `code_model` 闲聊 | `FamilyCore`（或无 FamilyCode）+ 即使 Agent 配了 code_model | `err==nil` 且返回值 **就是** 传入的 chat 实例 |
| `code_model` 已配 | `FamilyCode` + Agent `CodeModel`/`CodeAPIKey`/`CodeBaseURL` 非空 | `err==nil` 且返回值 **不是** chat 实例 |
| `code_model` 构建失败 | `FamilyCode` + Model 名非空但 `BuildModel` 必然失败（非法 provider 或现网可测的失败路径） | `errors.Is(err, ErrCodeModelBuild)`，不是 chat |
| 回归 | 非 code 族 | `TestResolveTurnModel_nonCodeKeepsChat` 仍过 |
| 接线 | 纯函数即可；`chat.go` 两处必须编译期使用 `(model, err)`。可用 `gofmt`/`go test` 保证调用方处理 err，不必起 HTTP |

`TestEvalGolden_code_model` = 上表「缺配」+「闲聊」表驱动两行（已配可作为第三行同测试）。构建失败可同文件非金样例名，或作为第四行。

故意让缺 `Model` 时再 `return chatModel, nil` → `TestEvalGolden_code_model` FAIL。

---

## 7. 错误处理与开关

| 情况 | 行为 |
|------|------|
| 非 code 族 / `active==nil` | 会话模型，无 error |
| 缺 `SATH_CODE_MODEL` 且全局、Agent 都空 | `ErrCodeModelRequired` |
| 只有 `SATH_CODE_API_KEY`、没有模型名 | 同上（密钥不是配置） |
| `BuildModel` 失败 | `ErrCodeModelBuild`，本轮失败 |
| 用户显式把 code_model 写成与对话模型同名 | 允许；用 code spec 去 Build |
| E1–E5 未做 | c7aa 仍 Skip；c304 声称闸不在本期改 |

没有「E0 关闭」环境变量。要回到 fail-open 只能改代码（金样例会红）。

---

## 8. 与 A / 父规格 / E1

- A：扩面 ID `code_model`。plan 必须引用 `TestEvalGolden_code_model`。
- 父规格 §5 E0、§6「E0 未配置 code 模型 → 可见错误」。
- E1 另开 spec。E0 合入后源码题在**已配模型**的环境才有资格谈「接近 Cursor」。

---

## 9. 验收清单（本文自身）

- [ ] 实现者不会把缺配再 fail-open 回对话模型。
- [ ] 「你好」不要求 code 模型；源码族缺配不能开跑。
- [ ] 下一份是 E0 的 implementation plan，且引用 `TestEvalGolden_code_model`。
