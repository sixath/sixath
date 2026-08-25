# Goal / Delivery 双不变量与凭据闸收窄

**日期**: 2026-08-25  
**状态**: 已确认（2026-08-25）  
**动机**: 会话 `e9d4c37c-bd34-46df-8233-ab8539e9239b`（agent `e8107fb3-e40a-4207-9d9a-6768847aaf79`）在已查到 `GetGameInfo` / Redis `nil` 后，凭据闸把终答冲成 `skills_list`；「把代码和日志打印出来 / 没有打印出来呀」被任务锁钉成新题，压缩只钉 CFG，终答变成 `release_rules::enable`。与 [任务锁](./2026-08-24-task-lock-goal-drift-design.md) 要防的 **bf26 Skill 改题** 看起来都是「能力清单终答」，根因相反：bf26 是手册改写调查题；e9d4 是闸门冲掉已答题、把催交付锁成新题。  
**对照**:
- 任务锁一期：[2026-08-24-task-lock-goal-drift-design.md](./2026-08-24-task-lock-goal-drift-design.md)（Q = 本轮用户原句；B1/B2 idle 回拉；不每步 LLM 裁判）
- 源码声称 / code pin：[2026-08-20-code-intel-cursor-parity-design.md](./2026-08-20-code-intel-cursor-parity-design.md)；现网 `context_code_pin.go` 只钉 `control_flow` / `call_graph`
- 凭据守卫：`framework/tool/catalog_search.go` `MatchCredentialSolicitation`；ReAct `!Used` 时 `credentialSolicitationRedirect`
- 对照会话：e9d4（误伤）；bf26（防改题回归，不得回退）

**一句话**：锁调查目标 G，不锁催交付句 D；凭据闸只在真索取且本轮还没用绑定工具时开火；压缩优先钉源码窗口。Skill 仍然盖不过 G。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 问题分类 | 两个故障：**(1) Skill/intake 改题**（bf26，保留现网闸）；**(2) 闸门误伤**（凭据冲终答、交付句当新题、只钉 CFG） |
| 主路径 | **Goal / Delivery**：G = 调查题（不变量）；D = 本轮用户原句。交付型追问继承上一轮非交付句为 G |
| 凭据闸 | 先否定句豁免，再要求真索取句式，再匹配绑定工具。本轮已有成功证据工具则**禁止**注入。匹配到 skills 族视为未命中。注入文案带 G，禁止 `skills_list` |
| 空闲改题 | 保留 B1/B2；加 **B4**（追问轮、G 不是问技能、本轮工具只有 skills 族且无证据工具）→ 现有 idle Retry 一次 |
| code pin | 最近 3 次 `rca_read` 钉 file + 行号 + **content 窗口**；CFG 缩成函数名 + when；超预算先丢 `call_graph` |
| 权威 | 用户调查目标 G > 本轮交付 D > Skill/工具。闸门注入不得把 G 换成目录或配置开关题 |
| 不另起炉灶 | 不关任务锁、不关凭据闸、不每步 LLM 裁判、不拆子 Agent |
| 与 8-24 关系 | Q 字段语义改为 **G**；`MetadataKeyTaskLockQ` 仍写 G。B1/B2/intake/`ask_user` 不变，比较对象仍是 `lock.Q`（即 G） |

---

## 1. 目标与非目标

### 目标（一期）

1. **bf26 不回退**：错 Skill 全文、intake `ask_user`、空击改成手册第 0 步，仍按 8-24 拦住；终答须覆盖原调查句。
2. **e9d4 终答不被冲掉**：本轮已成功调用 ES / Jaeger / 库 / rca 后，凭据闸不得注入；不得把终答改成 `skills_list` 目录。
3. **催交付不改题**：「打印出来 / 没有打印出来呀 / 补查 / 索引是」在已有上轮助手且无新调查标识时，G 保持上一轮调查题；模型去完成 D（贴证据），不把 D 当成新故障。
4. **真换题仍换 G**：「看另一条流水 4103_xxx」「先放放，改查预启动」→ G = 本轮原句。
5. **真索取仍拦**：「请提供 MySQL 的 host 和密码」→ 注入改用 `execute_read`，不向用户要密码。
6. **源码窗口可引用**：压缩后模型仍能看到最近 `rca_read` 的原文片段与正确 `file`/`repo` 路径，避免「不在 code roots」；声称闸仍用**本轮** `ToolCalls` 原文（不改成读 pin）。

### 非目标（一期不做）

- 保证 ES 索引名一定是 `backend-vm_manager-*`（Skill references 正文可作 P1）
- 0 击不得写成「服务从未参与」（P1）
- 每步 LLM 判「是否还在答原题」
- 关掉 `SATH_TURN_INTENT_GATE` 或凭据闸
- 让声称闸从 pin 消息取证（pin 只服务模型上下文；过闸仍靠本轮 rca_read/grep）

### 诚实上限

- 交付句名单是启发式：宁可漏继承（当新题答，可能略偏），不要把「另一条流水」粘在旧 G 上。
- 凭据闸收窄后，模型仍可能用纯文本要连接；靠「真索取句式」拦，不靠「正文出现过数据库二字」。
- pin 有 rune 预算，不能保证整文件都在上下文；保证的是「有可摘抄窗口 + 正确路径」，不是「永不压缩」。

---

## 2. 现网缺口（e9d4 为何既漂又傻）

```text
调查题 G0 = 流水为何卡回收 / GetGameInfo 为何失败
  → 已 es_log_query + rca_read 拿到 redis:nil
  → !Used 终答提到「数据库」或「连接信息」（含否定句）
  → MatchCredentialSolicitation 先走 MatchAskUserIntent（目录 BM25），不要求真索取
  → 注入「请立即调用工具 X」
  → 模型调 skills_list（源码提示禁止随便 execute_read）
  → 终答 = 系统纠正道歉 + 技能目录

下一轮用户 D = 「没有打印出来呀」
  → BuildTurnTaskLock.Q = D
  → 闸门要求仍答 D
  → L2/pin 只留 ReleaseVm.go 的 call_graph（IsVmProcessAllowed / GetReleaseState）
  → 终答 = release_rules::enable
```

| 机制 | 现网保证 | 本事故缺口 |
|------|----------|------------|
| 任务锁 Q | 本轮用户原句不可改写 | 催交付句变成调查题 |
| 凭据闸 | 不要向用户要已绑定连接 | 讨论/否认「连接信息」也开火；纠正指向任意 catalog 命中（含 skills） |
| idle B1/B2 | Skill 改题 / intake ask_user 回拉 | `skills_list` 目录终答过不了 B1/B2 |
| code pin | 压缩后路径表不丢 | 原文被截；模型围着 CFG 节点编故事 |
| 声称闸 | fenced 必须摘抄本轮工具原文 | 新一轮没有成功 rca_read 就贴不出代码 |

---

## 3. 架构与数据流

```text
SendMessage(userText, history)
  → BuildTurnTaskLock：
       D = trim(userText)
       若 HasPriorAssistant 且 isDeliveryUtterance(D) 且 !hasNewOpaqueIdent(D, history)
          G = lastNonDeliveryUserText(history) 或 D（找不到则 D）
       否则 G = D
       Delivery = D（当 G!=D 时写入；G==D 则 Delivery 空）
  → 8-24 其余拼 prompt；末尾【本轮任务锁】钉 G（及非空时的交付句 D）
  → Metadata task_lock / task_lock_q=G
  → ReAct !Used 顺序不变：
       1. EvaluateIdle（B1/B2/B4）→ Retry 拉回 G
       2. credentialSolicitationRedirect（三条同时真）→ 调绑定工具答 G
       3. 证据/声称闸
```

### 3.1 组件落点

| 单元 | 位置 | 职责 | 依赖 |
|------|------|------|------|
| Goal/Delivery 抽取 | `portal/internal/chat/task_lock.go` | `isDeliveryUtterance`、继承 G、`Format` 展示 G+D | 现有 KnownValues / HasPriorAssistant |
| 凭据真索取 | `framework/tool/catalog_search.go` | 否定豁免；真索取后才 BM25；skills 族不算命中 | 现有 catalog |
| 凭据 × 证据 | `framework/agent/react_agent.go` | 本轮 `trace.ToolCalls` 已有成功证据工具则跳过 redirect；注入带 G | TaskLock.Q；RunTrace |
| B4 目录终答 | `portal/internal/chat/turn_intent_gate.go` `looksLikeGoalDrift` | 追问轮纯 skills 工具 → idle Retry | lock.Q = G |
| 源码窗口 pin | `framework/model/context_code_pin.go` | 钉 content 窗口；CFG 摘要；丢 call_graph 保原文 | 现有 pipeline |

不改 Web / Gateway / MEA 入口。不改 8-24 的 RouteScoring / Skill 三档 / intake `ask_user`。

---

## 4. 详细设计

### 4.1 TurnTaskLock 字段

```go
type TurnTaskLock struct {
    Q                 string   // G：调查目标，闸门与 task_lock_q 只用这个
    Delivery          string   // D：本轮原句；G==D 时为空
    KnownValues       []string
    HasPriorAssistant bool
}
```

`Format()`：

- 始终：「用户问题（不可改写）：」+ G
- `Delivery != ""` 时追加：「本轮交付（完成此项以回答上述问题，不得把问题换成交付句）：」+ D
- KnownValues / 「Skill 与工具只是手段…」保持 8-24 口径，其中「用户问题」指 G

`MergeTaskLockMetadata`：`task_lock_q` = G。`forceFinalSummary` / `AnswerOriginalQuestionPrompt` 继续读 `task_lock_q`。

### 4.2 交付句（必须窄）

`isDeliveryUtterance(s)` 为真当且仅当规范化后命中下列**子串之一**（中英小写）：

| 组 | 子串 |
|----|------|
| 打印 | `打印`、`贴出来`、`打出来`、`原文`、`更加直观` |
| 补查 | `补查`、`再查`、`现在补`、`换索引`、`索引是`、`索引名` |
| 抱怨交付 | `上面有查`、`没有打印`、`没打印`、`没贴出来`、`没有贴` |
| 继续 | 整句 trim 后等于 `继续` |

**禁止**当交付（即使含上表某词）：

- 含 `另一条`、`换成`、`先放放`、`改查`、`新的流水`、`另外一个`
- `hasNewOpaqueIdent`：用现有 `identRe` + `isIdentOpaque` 从 D 抽出 token，若存在**未出现**在「不含本轮 user 的 history」的 `extractKnownValues` 里，则不继承（例如新流水号 `4103_E1JAObeKMdw2`）

继承：沿用现网 `maxLockHistoryMsgs=8`。从 history 自新到旧扫 **user** 消息，跳过正文等于本轮 D 的条目以及 `isDeliveryUtterance` 为真的，取第一条非空正文为 G。若没有，G = D。`portal/internal/service/chat.go` 传入的 history 往往不含本轮 user，跳过 D 仍然正确。

正例：D=`没有打印出来呀`，上轮 user=`把相应的代码和日志都打印出来更加直观` 也是交付句，再上一轮 `GetGameInfo 失败的原因是啥` → G 落到该句。

反例：D=`看另一条流水 4103_E1JAObeKMdw2` → 含「另一条」且有新 opaque → G = D。

无上轮助手：不继承（agent.go 无 ListMessages 路径只钉 G = D，与 8-24 一致）。

### 4.3 凭据闸

**`MatchCredentialSolicitation` 顺序改为：**

1. 空文本 → 不拦。
2. `deniesCredentialSolicitation(text)` → 不拦。命中任一：`未向用户索取`、`不会再向用户索取`、`未索取任何连接`、`不需要你提供任何连接`、`已由 Agent 绑定`、`已绑定`。
3. `looksLikeCredentialSolicitation(text)` 为假 → 不拦。  
   保留现网关键词表（含 `port`、`qyapi.weixin`）。`请提供` / `请给出` / `需要你提供` / `请回复` / `尚未保存` **单独出现仍为真**（现网真索取用例依赖此点）。  
   **收窄**：`连接信息` / `连接串` 单独出现不够，须同时有上述祈使。双关键词 ≥2（host/端口/port/password/密码/webhook/用户名/mysql/数据库/`qyapi.weixin`）也须带祈使，避免「查了数据库」误拦。
4. 再 `MatchAskUserIntent` / `fallbackBoundCredentialTool`。
5. 命中工具名属于 skills 族（`skills_list`、`load_skill`、`skill_view`、`skill_manage`、`read_skill_file`、`execute_skill_script`）则**剔除**，改用下一个绑定工具（`fallbackBoundCredentialTool`）。若剔除后没有绑定工具 → 不拦。禁止把 `skills_list` 当作纠正目标。

现网测试「请提供 MySQL Host…」必须仍拦。e9d4 式「未向用户索取任何连接信息」必须不拦。终答里叙述「查了数据库」无祈使 → 不拦。

**`credentialSolicitationRedirect(ctx, text, retries, trace, goalG)`：**

- `retries >= 1` 仍只一次。
- `hasSuccessfulBoundEvidence(trace)` → 不拦。成功 = `Error==""` 且 `!Blocked`，工具名 ∈  
  `es_log_query`、`jaeger_trace`、`execute_read`、`list_tables`、`describe_table`、`rca_read`、`rca_grep`、`rca_glob`、`rca_symbol`。
- `FormatCredentialSolicitationRedirect` 追加：`用已有或即将调用的绑定工具回答：` + G + `。禁止调用 skills_list / load_skill 交差，禁止向用户索取 host/密码/webhook。`

ReAct 三处 `!Used` 调用点（非流、流式、另一循环）均传入 `trace` 与 `taskLockQFromRequest(req)`。

### 4.4 B4 目录终答

`looksLikeGoalDrift` 现为 `b1 || b2`。改为 `b1 || b2 || b4`。

**B4** 同时成立：

1. `lock.HasPriorAssistant`
2. G 与 skills 族关键词无重叠：用现网 `skillNameOverlapsQ` 的分词，G **不含** `技能`、`手册`、`skills_list`、`load_skill`、`skill_view`（用户本轮就是在问有哪些技能则放行）
3. `trace.ToolCalls` 非空，且**每一个**的 `ToolName` 都是 skills 族六工具之一
4. 不存在成功证据工具（同 4.3 名单）

B4 不得单独靠终答文风（不恢复已否决的「B3 单独开火」）。

Idle Retry prompt 仍用 `goalDriftRetryPrompt(lock)`（钉 G）。nudge 上限 1 不变。

### 4.5 code pin

`pinFromToolContent`：

- `rca_read` 且存在 `content` **或** `control_flow` **或** `call_graph` 即可钉（现网要求必须有 CFG，无 CFG 的短文件钉不上原文）。
- 每条 read 写入：`file`、`start_line`、`end_line`、`content`（截断到 `codePinContentMaxRunes`，建议 4000）、`control_flow` 摘要（每函数：`function` + 各 path 的 `when` 拼成短数组，**去掉**完整 `calls` 大表若超预算）、**默认不钉**完整 `call_graph`。
- 组装 `[code_pin]` JSON 后若超过现网 `codePinMaxRunes`（8000）：先确保无 `call_graph`；再缩短 `control_flow` 摘要；最后截 `content`。禁止为保住 CFG 把 `content` 截成空。

现网 `TestEnsureCodePinMessages_extractsControlFlow`：pin 仍须含 `errcode == 0`（when 摘要里）；**改为同时含** `content` 里的 `Insert()` 原文。`TestPrepareChatContextCtx_L2KeepsCodePin`：L2 之后 pin 或消息中须仍能看到 **content 窗口**（例如 `xxxx` 前缀）以及 when 短句；不要求完整 call_graph。

声称闸：`CollectCodeQuoteSources(trace.ToolCalls)` 不变。pin 只降低「repo 写错 / 宣称仓不存在」；本轮仍应 `rca_read` 才能过摘抄闸。

---

## 5. 错误处理与开关

| 情况 | 行为 |
|------|------|
| 找不到可继承的非交付 user | G = D，Delivery 空 |
| 无 trace / 无 ToolCalls | 凭据闸不因「已有证据」跳过；仍走真索取三条 |
| `SATH_TURN_INTENT_GATE=0` | 与 8-24 相同，B4 一起关 |
| 凭据与 idle 同轮 | 顺序已是 idle 先；B4 命中则不再凭据注入 |
| MaxSteps | `task_lock_q` 已是 G，`forceFinalSummary` 不改语义 |

---

## 6. 测试与验收

单测，不绑现网 ES。**两场必须一起过。**

### 6.1 bf26 回归（不得回退）

沿用 8-24 T1–T9。额外：

| # | 检查 | 通过条件 |
|---|------|----------|
| T1' | 锁字段 | `BuildTurnTaskLock(bf26Q, history)` 的 `Q==bf26Q`、`Delivery==""` |

### 6.2 e9d4 / 凭据 / 交付

| # | 检查 | 通过条件 |
|---|------|----------|
| E1 | 否定句 | `MatchCredentialSolicitation(…, "已收到系统纠正。未向用户索取任何连接信息。")` → 不拦 |
| E2 | 叙述查库 | 终答含「数据库：查了 VM 232464」无祈使 → 不拦 |
| E3 | 真索取 | 现网「请提供 MySQL Host…」仍拦，且命中 `execute_read` 或 `send_to_wecom` |
| E4 | skills 命中 | BM25 顶部是 `skills_list` 但 catalog 另有绑定 `execute_read`、文本为真索取 → 命中 `execute_read`，不得是 `skills_list`。仅有 skills 族条目时 → 不拦 |
| E5 | 已有证据 | `credentialSolicitationRedirect` 在 trace 含成功 `es_log_query` 时，即使文本是真索取也不注入（本轮已用绑定工具） |
| E6 | 交付继承 | history 助手已回复；user1=`GetGameInfo 失败的原因是啥`；user2=`把相应的代码和日志都打印出来更加直观`；user3=`没有打印出来呀` → G=`GetGameInfo 失败的原因是啥`，Delivery=`没有打印出来呀` |
| E7 | 真换题 | `看另一条流水 4103_E1JAObeKMdw2`（新 opaque）→ Q=该句，Delivery 空 |
| E8 | B4 | HasPriorAssistant、G=回收原因、ToolCalls 仅 `skills_list` → `EvaluateIdle` 第一次 Retry `goal_drift` |
| E9 | B4 不误伤 | 用户问「有哪些技能」或 G 含 `skills_list` → B4 假 |
| E10 | pin 原文 | `rca_read` 带 content+CFG 时 pin JSON 含 content 子串；超预算时 content 非空、可无 call_graph |
| E11 | Format | 继承时正文含 G 与「本轮交付」，且「不可改写」后是 G 不是 D |

### 6.3 不做

- live 重放 e9d4 / bf26 才能合入
- 终答必须出现某条 ES 原文（环境没有日志时允许「未查到」）

---

## 7. 分期

| 期 | 内容 |
|----|------|
| P0（本期） | Goal/Delivery；凭据真索取 + 证据跳过 + 禁 skills 命中；B4；code pin 钉 content |
| P1 | 空击不得当「从未参与」（须先 list_tables / 探索引）；老调度索引名进入 SKILL 正文 |
| P2 | 声称闸可读 pin 作为摘抄来源（减少本轮重复 rca_read） |

一期范围 = **P0**。

---

## 8. 文件地图（规划用）

| 文件 | 变更 |
|------|------|
| `portal/internal/chat/task_lock.go` + `_test.go` | G/D、交付名单、继承、Format |
| `framework/tool/catalog_search.go` + `_test.go` | 匹配顺序、否定、连接信息收窄、skills 族拒绝 |
| `framework/agent/react_agent.go` + tests | redirect 传入 trace+G；证据跳过；注入文案 |
| `portal/internal/chat/turn_intent_gate.go` + `task_lock_gate_test.go` | B4 |
| `framework/model/context_code_pin.go` + `_test.go` | content 窗口、CFG 摘要、预算优先级 |
| `framework/agent/trace.go` | 仅当需要观测时加 `CredentialRedirects`（可选；B4 不依赖此字段也能用 ToolCalls） |

`portal/internal/service/chat.go` / `agent.go` 继续 `BuildTurnTaskLock` + `AppendTaskLock`，无新钉入点。

---

## 9. 评审关注点

1. `Q` 改义为 G 后，所有 `lock.Q` 比较（skill 重叠、intake）是否仍应用调查题而非交付句——必须是 G，否则「没有打印出来呀」会让 skill 重叠失败、乱丢工具。
2. E5「真索取但本轮已查过 ES 仍不注入」是否过宽：一期接受（绑定工具已用过，再问连接是模型口误，把已写完的根因发出去更重要）。若必须拦，二期改为「注入但不丢弃当前终答」（更复杂，P0 不做）。
3. 交付名单是否与「继续深入查另一类日志」冲突：含新 opaque 则不继承，覆盖「再查 4103_新流水」。
4. pin 不接入声称闸是否导致「有 pin 仍贴不出代码」：P0 靠正确路径再 rca_read；P2 再接。
