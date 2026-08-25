# Goal / Delivery 双不变量 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** P0 锁调查目标 G 而非催交付句；凭据闸只在真索取且本轮无成功证据工具时开火；压缩钉 `rca_read` 原文窗口。bf26 不回退，e9d4 式冲终答 / 锁错题 / 只钉 CFG 消失。

**Architecture:** `TurnTaskLock.Q` 改为 Goal G，交付型追问继承上一轮非交付 user；`MatchCredentialSolicitation` 先否定再真索取再 catalog，skills 族拒收；ReAct redirect 传入 trace+G；idle 增加 B4；`context_code_pin` 优先 content。

**Tech Stack:** Go（`portal/internal/chat`、`framework/tool`、`framework/agent`、`framework/model`）。不改 Web / Gateway / MEA。不提交 git（除非用户另行要求）。

**Spec:** `docs/superpowers/specs/2026-08-25-goal-lock-delivery-design.md`

**不做：** P1 空击≠不存在、Skill 索引正文；P2 声称闸读 pin；每步 LLM 裁判；关掉任务锁或凭据闸。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `portal/internal/chat/task_lock.go` | G/D、`isDeliveryUtterance`、继承、`Format` |
| `portal/internal/chat/task_lock_test.go` | E6/E7/E11、bf26 T1' |
| `framework/tool/catalog_search.go` | 凭据匹配顺序、否定、连接信息收窄、skills 拒收 |
| `framework/tool/catalog_search_test.go` | E1–E4、现网真索取仍拦 |
| `framework/agent/react_agent.go` | `credentialSolicitationRedirect` 加 trace+G；证据跳过 |
| `framework/agent/react_agent_credential_test.go` | **新建** E5 |
| `portal/internal/chat/turn_intent_gate.go` | B4 |
| `portal/internal/chat/task_lock_gate_test.go` | E8/E9、T6 仍过 |
| `framework/model/context_code_pin.go` | content 窗口、CFG 摘要、预算 |
| `framework/model/context_code_pin_test.go` | E10；L2 后仍有 content |

闸门比较一律用 **`lock.Q`（G）**，不要用 `Delivery`。

---

### Task 1: Goal / Delivery 任务锁

**Files:**
- Modify: `portal/internal/chat/task_lock.go`
- Modify: `portal/internal/chat/task_lock_test.go`

- [ ] **Step 1: 失败测试**

在 `task_lock_test.go` 追加（名称可微调，断言必须成立）：

```go
func TestBuildTurnTaskLock_bf26QUnchanged(t *testing.T) {
	lock := BuildTurnTaskLock(bf26Q, []model.Message{
		{Role: "user", Content: "这条流水4_a8uva8m5tpsl 正常吗"},
		{Role: "assistant", Content: "流水 4_a8uva8m5tpsl 正常。uid=104551174 ugid=796"},
		{Role: "user", Content: bf26Q},
	})
	if lock.Q != bf26Q || lock.Delivery != "" {
		t.Fatalf("%+v", lock)
	}
}

func TestBuildTurnTaskLock_inheritDeliveryChain(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: "GetGameInfo 失败的原因是啥"},
		{Role: "assistant", Content: "Redis key 不存在"},
		{Role: "user", Content: "把相应的代码和日志都打印出来更加直观"},
		{Role: "assistant", Content: "我去贴"},
		{Role: "user", Content: "没有打印出来呀"},
	}
	lock := BuildTurnTaskLock("没有打印出来呀", hist)
	if lock.Q != "GetGameInfo 失败的原因是啥" {
		t.Fatalf("G=%q", lock.Q)
	}
	if lock.Delivery != "没有打印出来呀" {
		t.Fatalf("D=%q", lock.Delivery)
	}
	block := lock.Format()
	if !strings.Contains(block, "GetGameInfo 失败的原因是啥") {
		t.Fatalf("format missing G: %s", block)
	}
	if !strings.Contains(block, "本轮交付") || !strings.Contains(block, "没有打印出来呀") {
		t.Fatalf("format missing D: %s", block)
	}
	gIdx := strings.Index(block, "用户问题（不可改写）：")
	dIdx := strings.Index(block, "本轮交付")
	if gIdx < 0 || dIdx < gIdx {
		t.Fatalf("G must be the locked question before D: %s", block)
	}
}

func TestBuildTurnTaskLock_newFlowDoesNotInherit(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: "GetGameInfo 失败的原因是啥"},
		{Role: "assistant", Content: "Redis nil"},
		{Role: "user", Content: "看另一条流水 4103_E1JAObeKMdw2"},
	}
	q := "看另一条流水 4103_E1JAObeKMdw2"
	lock := BuildTurnTaskLock(q, hist)
	if lock.Q != q || lock.Delivery != "" {
		t.Fatalf("%+v", lock)
	}
}

func TestIsDeliveryUtterance(t *testing.T) {
	for _, s := range []string{"没有打印出来呀", "现在补查", "vm-manager的索引是vm_manager吧", "继续"} {
		if !isDeliveryUtterance(s) {
			t.Fatalf("want delivery: %q", s)
		}
	}
	for _, s := range []string{bf26Q, "先放放，改查预启动", "看另一条流水"} {
		if isDeliveryUtterance(s) {
			t.Fatalf("not delivery: %q", s)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./portal/internal/chat -count=1 -run "TestBuildTurnTaskLock_inheritDeliveryChain|TestBuildTurnTaskLock_newFlowDoesNotInherit|TestIsDeliveryUtterance|TestBuildTurnTaskLock_bf26QUnchanged"`  
Expected: FAIL（`Delivery` 字段不存在或 G 仍等于末句）

- [ ] **Step 3: 最小实现**

在 `TurnTaskLock` 增加 `Delivery string`。`BuildTurnTaskLock`：

```go
func BuildTurnTaskLock(userText string, history []model.Message) TurnTaskLock {
	d := strings.TrimSpace(userText)
	msgs := filterLockHistory(history)
	lock := TurnTaskLock{
		Q:                 d,
		KnownValues:       extractKnownValues(d, msgs),
		HasPriorAssistant: hasPriorAssistant(msgs),
	}
	if lock.HasPriorAssistant && isDeliveryUtterance(d) && !hasNewOpaqueIdent(d, msgs) {
		if g := lastNonDeliveryUserText(msgs, d); g != "" {
			lock.Q = g
			if g != d {
				lock.Delivery = d
			}
		}
	}
	return lock
}
```

`isDeliveryUtterance`：先若含 `另一条` / `换成` / `先放放` / `改查` / `新的流水` / `另外一个` 则 false；否则子串命中 spec §4.2 表（打印/补查/抱怨/整句`继续`）。

`hasNewOpaqueIdent(d, msgs)`：对 d 做现有 `collectOpaque`，每个 token 若不在 `extractKnownValues("", msgsWithoutCurrentUser(msgs, d))` 中则为 true。实现时从 msgs 去掉**最后一条**等于 d 的 user，再 `extractKnownValues("", rest)`。

`lastNonDeliveryUserText`：自新到旧扫 msgs 的 user，跳过正文==d 的本轮句和 `isDeliveryUtterance` 为真的，返回第一条非空。

`Format()`：在「用户问题（不可改写）：」+ Q 之后，若 `Delivery != ""` 追加「本轮交付（完成此项以回答上述问题，不得把问题换成交付句）：」+ Delivery。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./portal/internal/chat -count=1 -run "TestBuildTurnTaskLock|TestIsDeliveryUtterance|TestQLooksLikeIntake|TestAppendTaskLock"`  
Expected: PASS（含原 bf26 KnownValues 测试）

---

### Task 2: 凭据真索取

**Files:**
- Modify: `framework/tool/catalog_search.go`
- Modify: `framework/tool/catalog_search_test.go`

- [ ] **Step 1: 失败测试**

```go
func TestMatchCredentialSolicitation_DenialDoesNotBlock(t *testing.T) {
	cat := boundMySQLCatalog()
	text := "已收到系统纠正。未向用户索取任何连接信息。skills_list 已调用。"
	if _, ok := MatchCredentialSolicitation(cat, text); ok {
		t.Fatal("denial must not block")
	}
}

func TestMatchCredentialSolicitation_NarratingDatabaseDoesNotBlock(t *testing.T) {
	cat := boundMySQLCatalog()
	text := "上面排查动作是：1. Jaeger 2. ES 3. 数据库：查 VM 232464 的绑定 flow_id"
	if _, ok := MatchCredentialSolicitation(cat, text); ok {
		t.Fatal("narration without imperative must not block")
	}
}

func TestMatchCredentialSolicitation_SkillsFamilyIsNotHit(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{
		{Name: "skills_list", Available: true,
			SearchHints: []string{"mysql", "host", "password", "数据库", "请提供"},
			Description: "List skills"},
		{Name: "execute_read", Available: true,
			Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
			SearchHints: []string{"mysql", "host", "password"}},
	}}
	text := "请提供 MySQL 的 host 和 password"
	match, ok := MatchCredentialSolicitation(cat, text)
	if !ok || match.Name != "execute_read" {
		t.Fatalf("must skip skills_list and use bound tool, ok=%v name=%q", ok, match.Name)
	}
	onlySkills := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "skills_list", Available: true,
		SearchHints: []string{"mysql", "host", "password"},
	}}}
	if _, ok := MatchCredentialSolicitation(onlySkills, text); ok {
		t.Fatal("skills-only catalog must not block")
	}
}

func boundMySQLCatalog() ToolCatalog {
	return ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
}
```

保留现有 `TestMatchCredentialSolicitation_BlocksPlainTextAsk` 等真索取用例。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./framework/tool -count=1 -run "TestMatchCredentialSolicitation_"`  
Expected: FAIL 在 Denial / Narrating

- [ ] **Step 3: 最小实现**

`MatchCredentialSolicitation` 改为：

1. trim 空 → false  
2. `deniesCredentialSolicitation`（spec 否定子串）→ false  
3. `looksLikeCredentialSolicitation` 为假 → false  
4. 现有 `MatchAskUserIntent` / fallback  
5. 命中若为 skills 族 → `fallbackBoundCredentialTool`；仍没有绑定工具则不拦。禁止纠正目标为 `skills_list`。  

`looksLikeCredentialSolicitation`：保留现网 `port`、`qyapi.weixin` 等关键词。`请提供` 等祈使子串仍单独为真。仅收窄：`连接信息`/`连接串` 单独出现不够，须同时有祈使；双关键词 ≥2 也须带祈使（避免「查了数据库」误拦）。现网 `TestMatchCredentialSolicitation_BlocksPlainTextAsk`（含「请提供」）必须仍绿。

`isSkillsFamilyTool`：`skills_list`、`load_skill`、`skill_view`、`skill_manage`、`read_skill_file`、`execute_skill_script`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./framework/tool -count=1 -run "TestMatchCredential|TestMatchAskUserIntent|TestSearchCatalog"`  
Expected: PASS

---

### Task 3: Redirect 跳过已有证据并钉 G

**Files:**
- Modify: `framework/agent/react_agent.go`（三处 `credentialSolicitationRedirect` 调用 + 函数签名）
- Create: `framework/agent/evidence_tools.go`（`IsBoundEvidenceTool`、`IsSkillsFamilyToolName`、`HasSuccessfulBoundEvidence`）
- Create: `framework/agent/react_agent_credential_test.go`（`package agent`）
- Create: `framework/agent/evidence_tools_test.go`

- [ ] **Step 1: 失败测试**

```go
func TestCredentialSolicitationRedirect_skipsWhenEvidenceExists(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings: map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolCatalog, cat)
	trace := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query", Allowed: true, Result: map[string]any{"ok": true},
	}}}
	ask := "请提供 MySQL 的 Host、Port、用户名、密码"
	if _, _, ok := credentialSolicitationRedirect(ctx, ask, 0, trace, "GetGameInfo 失败原因"); ok {
		t.Fatal("already used bound evidence this turn; must not redirect")
	}
}

func TestCredentialSolicitationRedirect_trueAskWithoutEvidenceStillFires(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings: map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolCatalog, cat)
	prompt, match, ok := credentialSolicitationRedirect(ctx, "请提供 MySQL 的 Host、Port、用户名、密码", 0, &RunTrace{}, "查流水失败原因")
	if !ok || match.Name != "execute_read" {
		t.Fatalf("ok=%v match=%#v", ok, match)
	}
	if !strings.Contains(prompt, "查流水失败原因") {
		t.Fatalf("prompt must include G: %s", prompt)
	}
	if !strings.Contains(prompt, "禁止调用 skills_list") {
		t.Fatalf("prompt must forbid skills_list: %s", prompt)
	}
}
```

第二则里 `strings.Contains(prompt, "skills_list") && !strings.Contains(...)` 写乱了——实现时断言只要：`Contains(G)` 且 `Contains("禁止调用 skills_list")`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./framework/agent -count=1 -run "TestCredentialSolicitationRedirect_"`  
Expected: FAIL（签名仍是 3 参数）

- [ ] **Step 3: 最小实现**

```go
func credentialSolicitationRedirect(ctx context.Context, text string, retries int, trace *RunTrace, goalG string) (string, tool.ToolCatalogEntry, bool) {
	if retries >= 1 {
		return "", tool.ToolCatalogEntry{}, false
	}
	if hasSuccessfulBoundEvidence(trace) {
		return "", tool.ToolCatalogEntry{}, false
	}
	cat, ok := ctx.Value(tool.ContextKeyToolCatalog).(tool.ToolCatalog)
	if !ok || len(cat.Entries) == 0 {
		return "", tool.ToolCatalogEntry{}, false
	}
	match, blocked := tool.MatchCredentialSolicitation(cat, text)
	if !blocked {
		return "", tool.ToolCatalogEntry{}, false
	}
	prompt := tool.FormatCredentialSolicitationRedirect(match)
	if g := strings.TrimSpace(goalG); g != "" {
		prompt += "用已有或即将调用的绑定工具回答：" + g + "。禁止调用 skills_list / load_skill 交差，禁止向用户索取 host/密码/webhook。"
	}
	return prompt, match, true
}
```

在 `evidence_tools.go` 实现 `IsBoundEvidenceTool` / `IsSkillsFamilyToolName` / `HasSuccessfulBoundEvidence`（成功 = `Error=="" && !Blocked`，工具名 ∈ spec 4.3）。`credentialSolicitationRedirect` 调用 `HasSuccessfulBoundEvidence(trace)`。Task 4 的 B4 必须复用这些函数，禁止再写一份 switch。

三处调用改为：

```go
credentialSolicitationRedirect(ctx, gen.Text, credentialRedirects, trace, taskLockQFromRequest(req))
```

流式两处同样。`FormatCredentialSolicitationRedirect` 本体可保持；G 与禁止 skills 在 redirect 函数追加，避免破坏只测 Format 的用例（若无则直接改进 Format 签名也可，二选一，测试跟签名）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./framework/agent -count=1 -run "TestCredentialSolicitationRedirect_|TestReAct"`  
Expected: PASS（全包 `go test ./framework/agent -count=1` 若太慢，至少跑 credential + `TestReActCodeClaimGate` / idle 相关）

---

### Task 4: Idle B4 目录终答

**Files:**
- Modify: `portal/internal/chat/turn_intent_gate.go`（`looksLikeGoalDrift`）
- Modify: `portal/internal/chat/task_lock_gate_test.go`

- [ ] **Step 1: 失败测试**

```go
func TestTurnIntentGate_EvaluateIdleB4SkillsCatalog(t *testing.T) {
	gate := TurnIntentGate{}
	lock := TurnTaskLock{Q: "为什么回收流程没有彻底完成呢", HasPriorAssistant: true}
	req := testLockReq(lock.Q, lock)
	trace := &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{{ToolName: "skills_list", Allowed: true}}}
	res := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req: req, AssistantText: "当前可用技能共 3 个", Trace: trace,
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "goal_drift" {
		t.Fatalf("B4: %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_EvaluateIdleB4DoesNotFireWhenAskingSkills(t *testing.T) {
	gate := TurnIntentGate{}
	q := "现在有哪些技能 skills_list"
	lock := TurnTaskLock{Q: q, HasPriorAssistant: true}
	trace := &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{{ToolName: "skills_list", Allowed: true}}}
	res := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req: testLockReq(q, lock), AssistantText: "三个技能", Trace: trace,
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("asking for skills must not B4, got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_EvaluateIdleB4DoesNotFireWithEvidence(t *testing.T) {
	gate := TurnIntentGate{}
	lock := TurnTaskLock{Q: "为什么回收流程没有彻底完成呢", HasPriorAssistant: true}
	trace := &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{
		{ToolName: "es_log_query", Allowed: true},
		{ToolName: "skills_list", Allowed: true},
	}}
	res := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req: testLockReq(lock.Q, lock), AssistantText: "目录", Trace: trace,
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("evidence tools present: B4 must be false, got %v %q", res.Decision, res.Reason)
	}
}
```

原 `TestTurnIntentGate_EvaluateIdleT6` / `B3AloneDoesNotRetry` 必须仍 PASS。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./portal/internal/chat -count=1 -run "TestTurnIntentGate_EvaluateIdleB4"`  
Expected: FAIL

- [ ] **Step 3: 最小实现**

`looksLikeGoalDrift` 末尾 `return b1 || b2 || idleCatalogInsteadOfAnswer(lock, trace)`。

`idleCatalogInsteadOfAnswer(lock, trace)`：`lock == nil` 或 `trace == nil` → false。否则 HasPriorAssistant；G 不含 `技能`/`手册`/`skills_list`/`load_skill`/`skill_view`；`len(ToolCalls)>0`；每个 `agent.IsSkillsFamilyToolName`；`!agent.HasSuccessfulBoundEvidence(trace)`。portal 的 `isSkillTool` 改为调用 `agent.IsSkillsFamilyToolName`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./portal/internal/chat -count=1 -run "TestTurnIntentGate_"`  
Expected: PASS

---

### Task 5: code pin 钉原文窗口

**Files:**
- Modify: `framework/model/context_code_pin.go`
- Modify: `framework/model/context_code_pin_test.go`

- [ ] **Step 1: 失败测试**

改写 `TestEnsureCodePinMessages_extractsControlFlow`：pin 必须含 when 里的 `errcode == 0`，以及 **content 原文** `Insert()`。**不要**再要求 `InsertUnionUserAreaInfo`（那是 `calls` 表，P0 摘要不钉 calls）。

同样改写 `TestPrepareChatContextCtx_L2KeepsCodePin`：L2 之后须仍能看到 content 窗口（`xxxx` 前缀）或 when 短句 `errcode == 0`；禁止再断言 `InsertUnionUserAreaInfo`。

新增：

```go
func TestEnsureCodePinMessages_pinsContentWithoutCFG(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tool": "rca_read",
		"result": map[string]any{
			"file":    "GameInfoModel.go",
			"content": "func GetGameInfo() error { return redis.Nil }",
		},
	})
	out := ensureCodePinMessages([]Message{
		{Role: "system", Content: "sys"},
		{Role: "tool", Content: string(body)},
	})
	if !isCodePinMessage(out[1]) || !strings.Contains(out[1].Content, "redis.Nil") {
		t.Fatalf("content-only rca_read must pin: %#v", out)
	}
}

func TestEnsureCodePinMessages_dropsCallGraphBeforeContent(t *testing.T) {
	// content 很长 + 巨大 call_graph 时，pin 截断后 content 仍非空
	content := strings.Repeat("SRC", 3000)
	cg := map[string]any{"nodes": []any{map[string]any{"id": strings.Repeat("N", 5000)}}}
	body, _ := json.Marshal(map[string]any{
		"tool": "rca_read",
		"result": map[string]any{"file": "ReleaseVm.go", "content": content, "call_graph": cg},
	})
	out := ensureCodePinMessages([]Message{
		{Role: "system", Content: "sys"},
		{Role: "tool", Content: string(body)},
	})
	if !isCodePinMessage(out[1]) {
		t.Fatalf("expected pin, got %#v", out)
	}
	raw := out[1].Content
	if !strings.Contains(raw, "SRC") {
		t.Fatalf("must keep content window: %s", raw[:min(200, len(raw))])
	}
}
```

`TestPrepareChatContextCtx_L2KeepsCodePin`：断言 L2 后仍能看到 content 里的 `x` 重复或 when 短句；**不要**要求完整 call_graph JSON。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./framework/model -count=1 -run "TestEnsureCodePinMessages_|TestPrepareChatContextCtx_L2KeepsCodePin"`  
Expected: FAIL（无 CFG 不钉；content 未写入 pin）

- [ ] **Step 3: 最小实现**

`pinFromToolContent`：`tool` 空或 `rca_read`；有 content 或 cf 或 cg 即可。写入截断后的 `content`（`codePinContentMaxRunes=4000`）。`control_flow` 用摘要：每函数 `function` + paths[].when，不复制完整 calls/call_graph（call_graph 默认不写入）。

组装后若 `utf8.RuneCountInString(content) > codePinMaxRunes`：删除任何残留 call_graph；缩短 when 列表；最后 `TruncateMessageRunes` 整段 pin，但截断前若 content 字段会被砍光，则先丢掉 control_flow 摘要、保 content 前缀。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./framework/model -count=1 -run "TestEnsureCodePinMessages_|TestPrepareChatContextCtx_L2KeepsCodePin|TestPruneToolBody"`  
Expected: PASS

---

### Task 6: 包级回归

- [ ] **Step 1: 跑相关包**

Run:

```
cd portal && go test ./internal/chat -count=1
cd framework && go test ./tool ./agent ./model -count=1
```

根目录 `go.mod` 是空 module，不要在仓库根跑 `go test ./portal/...`。

若 `./agent` 过慢，至少：

```
cd portal && go test ./internal/chat -count=1
cd framework && go test ./tool -count=1 -run "TestMatchCredential|TestMatchAskUser|TestSearchCatalog"
cd framework && go test ./agent -count=1 -run "TestCredentialSolicitationRedirect_|TestReAct.*Idle|TestReActCodeClaim|TestHasSuccessfulBoundEvidence"
cd framework && go test ./model -count=1 -run "Pin|CodePin|L2Keeps"
```

Expected: PASS

- [ ] **Step 2: 手工核对清单（不跑 live）**

- bf26：`lock.Q` 仍是原两问，Delivery 空  
- e9d4 第 8 轮：有 `es_log_query` 成功 → 无凭据注入  
- e9d4 第 11 轮：G=`GetGameInfo 失败的原因是啥`（或链上更早的非交付调查句），Format 含「本轮交付」  
- 真索取请提供 host 仍注入 `execute_read`

---

## Execution

Plan 写完后不要自动 commit。实现时按 Task 1→6 顺序，每 Task 先红后绿。
