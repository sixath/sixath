# 任务锁与问题漂移遏制 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** P0 让本轮用户原句 Q 成为 ReAct 不变量：错 Skill/intake 不得改题；空命中或停手改题时注入一次「仍答 Q」。

**Architecture:** 打分卫生减少误命中；Skill 三档注入并去掉「优先遵循」；`TurnTaskLock` 钉在完整 system 末尾并放入 `Request.Metadata`；`IdlePostModelPolicy` 让无工具步骤也能 `PostModelRetry`；`TurnIntentGate` 丢掉与 Q 无重叠的 `skill_*`、拦住追问上的 intake 型 `ask_user`，idle 改题最多回压一次。

**Tech Stack:** Go（`framework/skills`、`framework/agent`、`framework/templates`、`portal/internal/chat`、`portal/internal/service`）。不改 Web / Gateway / MEA 入口。不提交 git（除非用户另行要求）。

**Spec:** `docs/superpowers/specs/2026-08-24-task-lock-goal-drift-design.md`（已确认）

**不做：** P1 永不灌全文；P2 子 Agent；每步 LLM 裁判；业务字段名黑名单；改 `topic_drift_all` / `family_dropped_all`。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/skills/route.go` | 中英分词；短标签不计分；`RouteMatch.RunnerUpScore` |
| `framework/agent/post_model_policy.go` | 可选 `IdlePostModelPolicy`；`!Used` 时调 `EvaluateIdle` |
| `framework/agent/trace.go` | `GoalDriftNudges`、`DroppedProposals` |
| `framework/templates/skills_handler.go` | 目录档权威措辞 |
| `portal/internal/chat/task_lock.go` | 抽取 Q / KnownValues / HasPriorAssistant；`Format()` |
| `portal/internal/chat/skill_router.go` | 高/中/低分档；高档条款放正文**之后** |
| `portal/internal/chat/turn_intent_gate.go` | skill drift、intake `ask_user`、`EvaluateIdle`、记录 dropped |
| `portal/internal/service/chat.go` | 两处 prompt 末尾钉锁；`Request.Metadata["task_lock"]` |
| `portal/internal/service/agent.go` | 第三条路径钉锁（只钉 Q） |
| `framework/agent/react_agent.go` | `forceFinalSummary` 附上 TaskLock.Q（若 Metadata 有） |

常量：`portal/internal/chat` 中 `MetadataKeyTaskLock = "task_lock"`。Gate 从 `in.Req.Metadata` 读 `*TurnTaskLock`（或值类型）。重叠与 idle 一律用 **lock.Q**，不要用注入纠偏后的 `lastUserContent`（Retry 会把 user 变成纠偏词）。

---

### Task 1: RouteScoring

**Files:**
- Modify: `framework/skills/route.go`
- Modify: `framework/skills/route_test.go`

- [ ] **Step 1: 失败测试**

在 `route_test.go` 增加（名称可微调，断言必须成立）：

```go
func TestRouteBest_shortTagEsDoesNotMatchAccess(t *testing.T) {
	metas := []skills.SkillMeta{{
		Name:        "rca-sync-archive-migrate",
		Description: "实时存档迁移 union-archiver-manager",
		Tags:        []string{"es", "rca"},
	}}
	q := "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"
	_, ok := skills.RouteBest(q, metas, skills.RouteOptions{MinScore: 5})
	if ok {
		t.Fatal("tag es must not match via substring of access")
	}
}

func TestTokenize_splitsCJKAndLatin(t *testing.T) {
	toks := tokenize("需要看看access-service有没有") // 测试未导出函数：同包
	if _, ok := toks["access"]; !ok {
		t.Fatalf("want access token, got %v", toks)
	}
	if _, ok := toks["service"]; !ok {
		t.Fatalf("want service token, got %v", toks)
	}
}

func TestRouteBest_runnerUpScore(t *testing.T) {
	metas := []skills.SkillMeta{
		{Name: "alpha-skill", Description: "cluster ops", Tags: []string{"kubernetes"}},
		{Name: "beta-other", Description: "cluster ops", Tags: []string{"kubernetes"}},
	}
	m, ok := skills.RouteBest("debug kubernetes pod crash", metas, skills.RouteOptions{})
	if !ok {
		t.Fatal("both should pass min via tag kubernetes")
	}
	if m.RunnerUpScore < 5 {
		t.Fatalf("want runner-up >= 5, got %+v", m)
	}
}
```

保留现有 `TestRouteBest_nameInQuery` / tagHit。

`route_test.go` 已是 `package skills`：粘贴时去掉 `skills.` 前缀，直接调 `RouteBest` / `SkillMeta` / `RouteOptions`。

- [ ] **Step 2: 跑测确认失败**

Run: `go test ./skills/ -count=1 -run "TestRouteBest_shortTagEs|TestTokenize_splitsCJK|TestRouteBest_runnerUp"`
（在 `framework/` 下）

Expected: FAIL（`tokenize` 粘连或 `es` 仍 +5 或无 `RunnerUpScore`）

- [ ] **Step 3: 最小实现**

`RouteMatch` 增加 `RunnerUpScore int`。

`RouteBest`：`opts.MaxResults = 2`，取 `matches[0]`，若 `len>=2` 则 `RunnerUpScore = matches[1].Score`。

`tokenize`：ASCII 字母数字与其它 `unicode.IsLetter` **换类即 flush**；非字母数字仍 flush。`access-service` → `access`,`service`；`需要看看access` → `需要看看`,`access`。`len<3` 的 latin（如 `vm`）仍丢弃。

`scoreSkill` 标签循环：仅当 `len(tag) >= 4` 才允许 `strings.Contains(q, tag)`；`len(tag) < 3` 整段跳过；`tagContainedInTokens` 保持 `len<3` 守卫。

- [ ] **Step 4: 跑测通过**

Run: `go test ./skills/ -count=1`

Expected: PASS

验收：T1 的打分半边（`es` 不能把存档技能打过线）。

---

### Task 2: IdlePostModelPolicy + GoalDriftNudges

**Files:**
- Modify: `framework/agent/post_model_policy.go`
- Modify: `framework/agent/post_model_policy_test.go`
- Modify: `framework/agent/trace.go`

- [ ] **Step 1: 失败测试**

`post_model_policy.go` 增加：

```go
type IdlePostModelPolicy interface {
	EvaluateIdle(ctx context.Context, in PostModelPolicyInput) PostModelPolicyResult
}
```

测试用 policy：

```go
type idleRetryOnce struct{}

func (idleRetryOnce) Evaluate(context.Context, PostModelPolicyInput) PostModelPolicyResult {
	return PostModelPolicyResult{Decision: PostModelContinue}
}
func (idleRetryOnce) EvaluateIdle(_ context.Context, in PostModelPolicyInput) PostModelPolicyResult {
	if in.Trace != nil && in.Trace.GoalDriftNudges > 0 {
		return PostModelPolicyResult{Decision: PostModelContinue}
	}
	if in.Trace != nil {
		in.Trace.GoalDriftNudges++
	}
	return PostModelPolicyResult{Decision: PostModelRetry, Reason: "goal_drift", Prompt: "仍回答原问题"}
}
```

`TestReActAgent_IdlePolicyRetryThenFinish`：给 `fakeOpenAIClient` 增加按步文本（例如 `plainReplies []string`，每步 `Used=false` 弹出下一句）。第一步「请提供 flow_id」；第二步「access-service 无命中」。`WithReActMaxSteps(5)`。断言：`resp.Text` 为第二段；`trace.GoalDriftNudges==1`；`trace.Errors` 含 `post_model_policy:retry:goal_drift`。

若 fake 短期不好改：退化为只断言 `GoalDriftNudges==1` 与 retry error，**不要**依赖单一 `finalReply` 表达两段不同终答。

另：`finishAllPolicy`（不实现 Idle）+ 第一步无工具 → 行为与现网相同（立即终答），证明未实现 Idle 的 policy 零改动。

- [ ] **Step 2: 跑测确认失败**

Run: `go test ./agent/ -count=1 -run IdlePolicy`

Expected: FAIL（`!Used` 仍提前 return）

- [ ] **Step 3: 实现 `applyPostModelPolicy`**

把开头改成：

```go
if a == nil || a.config.PostModelPolicy == nil {
	return stepInfo, ""
}
if !stepInfo.Used {
	if idle, ok := a.config.PostModelPolicy.(IdlePostModelPolicy); ok {
		res := idle.EvaluateIdle(ctx, PostModelPolicyInput{
			Req: req, Step: step, AssistantText: assistantText,
			ToolStep: stepInfo, Trace: trace,
		})
		if res.Decision == PostModelRetry {
			reason := res.Reason
			if reason == "" {
				reason = "retry"
			}
			if trace != nil {
				trace.Errors = append(trace.Errors, "post_model_policy:retry:"+reason)
			}
			prompt := strings.TrimSpace(res.Prompt)
			if prompt == "" {
				prompt = defaultPostModelRetryPrompt
			}
			return clearToolStep(stepInfo), prompt
		}
	}
	return stepInfo, ""
}
// 以下保持现网：len(calls)==0 则 return；Evaluate 处理 Finish/Retry/Filter
```

`RunTrace` 增加：

```go
GoalDriftNudges int `json:"goal_drift_nudges,omitempty"`
DroppedProposals []DroppedProposal `json:"dropped_proposals,omitempty"`

type DroppedProposal struct {
	ToolName string `json:"tool_name,omitempty"`
	ArgName  string `json:"arg_name,omitempty"` // skill name 等
}
```

**禁止**改 ReAct 三处调用点（已处理 `retryPrompt != ""`）。

- [ ] **Step 4:** `go test ./agent/ -count=1`

Expected: PASS（含原 Finish/Filter/Retry 测）

---

### Task 3: TurnTaskLock

**Files:**
- Create: `portal/internal/chat/task_lock.go`
- Create: `portal/internal/chat/task_lock_test.go`

- [ ] **Step 1: 失败测试（T3 / T8 / T9）**

```go
const bf26Q = "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"

func TestBuildTurnTaskLock_bf26values(t *testing.T) {
	prior := "流水 4_a8uva8m5tpsl 正常。uid=104551174 ugid=796"
	lock := BuildTurnTaskLock(bf26Q, []model.Message{
		{Role: "user", Content: "这条流水4_a8uva8m5tpsl 正常吗"},
		{Role: "assistant", Content: prior},
		{Role: "user", Content: bf26Q},
	})
	if lock.Q != bf26Q {
		t.Fatalf("Q=%q", lock.Q)
	}
	if !lock.HasPriorAssistant {
		t.Fatal("want prior assistant")
	}
	joined := strings.Join(lock.KnownValues, " ")
	for _, v := range []string{"4_a8uva8m5tpsl", "104551174", "796"} {
		if !strings.Contains(joined, v) {
			t.Fatalf("KnownValues missing %s: %v", v, lock.KnownValues)
		}
	}
	block := lock.Format()
	if !strings.Contains(block, "【本轮任务锁】") || !strings.Contains(block, bf26Q) {
		t.Fatalf("format=%s", block)
	}
}

func TestBuildTurnTaskLock_noIDFollowup(t *testing.T) {
	lock := BuildTurnTaskLock("那错误码是什么", []model.Message{
		{Role: "assistant", Content: "超时由网关重试导致"},
		{Role: "user", Content: "那错误码是什么"},
	})
	if !lock.HasPriorAssistant || lock.Q != "那错误码是什么" {
		t.Fatalf("%+v", lock)
	}
}

func TestQLooksLikeIntake(t *testing.T) {
	if qLooksLikeIntake("帮我查一个单") {
		t.Fatal("first-turn must not look like intake")
	}
	if !qLooksLikeIntake("请提供 flow_id 或 uuid") {
		t.Fatal("want intake")
	}
}
```

`access` 不得进入 KnownValues（无数字且短）。

- [ ] **Step 2:** `go test ./internal/chat/ -count=1 -run TurnTaskLock`（在 `portal/`）

Expected: FAIL（文件不存在）

- [ ] **Step 3: 实现**

`TurnTaskLock` 字段：`Q string`、`KnownValues []string`、`HasPriorAssistant bool`。

抽取（spec §4.1）：键值右侧 `\b[\w.-]{2,24}\s*[:=]\s*(\S{3,})`；反引号/直引号 ≥4；同时含字母数字或含 `_`/`-` 且总长 ≥6 的拉丁数字串。去重，上限 16。本轮 Q 中的取值优先。不抽纯中文、不抽无数字且长度 &lt; 6 的英文。

`HasPriorAssistant`：传入的 messages 里，在**最后一条 user 之前**存在 `role==assistant` 且正文非空。

`qLooksLikeIntake`：Q 含「请提供」「请给出」「麻烦提供」。

`Format()` 按 spec 输出；KnownValues 空则省略取值行或写「（无）」，但必须含 Q 与「不可改写」。

`const MetadataKeyTaskLock = "task_lock"`

`TaskLockFromRequest(req *agent.Request) *TurnTaskLock`：从 Metadata 断言取出。

- [ ] **Step 4:** `go test ./internal/chat/ -count=1 -run "TurnTaskLock|QLooksLikeIntake"`

Expected: PASS

---

### Task 4: Skill 权威与三档注入

**Files:**
- Modify: `framework/templates/skills_handler.go`
- Modify: `portal/internal/chat/skill_router.go`
- Modify: `portal/internal/chat/skill_router_test.go`
- 若 templates 无测：Create `framework/templates/skills_handler_test.go`（只测 `BuildSkillsAwarePrompt` 字符串）

- [ ] **Step 1: 失败测试**

改现有 `TestBuildEffectiveSystemPromptForTurn_injectsMatchedSkill`：查询仍含 `demo-route`（高档），**必须**含正文 `Follow step A`，**不得**含 `请优先遵循此工作流`；条款用整句「不得替换【本轮任务锁】」或「不可改写」，且 `strings.Index(out,"Follow step A") < strings.Index(out,"不得替换")`（不要只搜「不得」，以免撞上目录档其它措辞）。

新增（中档必须两名都过 min 且分差 &lt; 2，名称都不在 Q 里）：

```go
func TestBuildEffectiveSystemPromptForTurn_midTierNoBody(t *testing.T) {
	dir := t.TempDir()
	writeSkill := func(name, desc string) {
		d := filepath.Join(dir, "skills", name)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(
			"---\nname: "+name+"\ndescription: "+desc+"\ntags: [kubernetes]\n---\n# BodyOf"+name+"\nDo not inject this heading in mid tier.\n",
		), 0o644)
	}
	writeSkill("alpha-helper", "cluster ops")
	writeSkill("beta-helper", "cluster ops")
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 5, MaxBodyRunes: 4000})
	out := BuildEffectiveSystemPromptForTurn("", idx, "debug kubernetes pod crash")
	if strings.Contains(out, "BodyOf") || strings.Contains(out, "Do not inject this heading") {
		t.Fatalf("mid tier must not inject SKILL body, got %s", out)
	}
	if !strings.Contains(out, "【候选 Skill:") {
		t.Fatalf("want one-line candidate, got %s", out)
	}
}
```

两技能都靠 tag `kubernetes` +5 过线，名称不在 Q 中，分差 0 → 中档一行。不要用只有 desc token +2 的夹具（达不到 min 5）。
bf26 句 + 两个技能（存档 migrate tags=`es`；vm-allocate desc 含 access-service）在 **Task 1 合入后**：存档不得全文；若 vm 技能高档（名称不在 Q、但分差可能满足）允许 vm 全文——**不要**把存档全文注入。单独测：

```go
func TestBuildEffectiveSystemPromptForTurn_bf26DoesNotInjectArchiveBody(t *testing.T) {
	dir := t.TempDir()
	write := func(name, fm, body string) {
		d := filepath.Join(dir, "skills", name)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\n"+fm+"\n---\n"+body+"\n"), 0o644)
	}
	write("rca-sync-archive-migrate", "description: 实时存档迁移 SyncDispatch\ntags: [es, rca]", "# Archive\n### 第 0 步：收集关键信息\n")
	write("migu-cloud-game-vm-allocate", "description: call chain across access-service union-access vm-manager", "# VM\nAssignVM path\n")
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	SetSkillRouteSettings(SkillRouteSettings{Enabled: true, MinScore: 5, MaxBodyRunes: 4000})
	q := "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"
	out := BuildEffectiveSystemPromptForTurn("", idx, q)
	if strings.Contains(out, "第 0 步") || strings.Contains(out, "请优先遵循") {
		t.Fatal(out)
	}
}
```

`BuildSkillsAwarePrompt`：不得含「直接按该工作流执行」；不得含「严格遵循」。

- [ ] **Step 2:** 跑测 FAIL

- [ ] **Step 3: 实现**

`skills_handler.go:59` 改为：已自动匹配则**不要再 load_skill**；手册与用户问题冲突时以用户问题为准；需要细节再 `load_skill` 一次。删「直接按该工作流执行」「严格遵循」。`:72` 改为可选手册语气。

`skill_router.go`：`matches := skills.Route(..., MaxResults: 2)`（或 `RouteBest` 读 `RunnerUpScore`）。

分档：`nameInQuery := strings.Contains(lower(Q), name) || strings.Contains(lower(Q), spacedName)`。`high := nameInQuery || (score>=min && score-second >= 2)`；仅一名过线时 `second=0`，分差视为满足（spec）。`min` 用 `skillRouteSettings.MinScore`，≤0 则 5。

- 高：注入正文，横幅**不要**「高度相关/优先遵循」；正文后追加：「此手册不得替换【本轮任务锁】中的用户问题；上下文已有取值禁止再向用户索取。」
- 中：只一行 `【候选 Skill: name】description`（截断描述至 ~200 runes）
- 低：不注入

`MinScore` 设置 0 时与现网一样走 default 5。

- [ ] **Step 4:** `go test ./internal/chat/ -count=1 -run BuildEffectiveSystemPrompt`（portal）  
`go test ./templates/ -count=1`（framework，若新增测）

Expected: PASS

---

### Task 5: GoalDriftGate

**Files:**
- Modify: `portal/internal/chat/turn_intent_gate.go`
- Modify: `portal/internal/chat/turn_intent_gate_test.go`

**约束：** 不改 `topic_drift_all → Finish`（web 全漂仍 Finish）。skill 在 RCA 轮次先被 `family_dropped_all` Retry，走不到 topic_drift_all。

- [ ] **Step 1: 失败测试**

T4：`Evaluate`，ActiveFamilies=`core,rca`（无 skills），`skill_view` name=`rca-sync-archive-migrate`，user/Q=bf26Q，Req.Metadata 带 lock。期望：`family_dropped_all` **或** Filter 丢掉 skill_view；`trace.DroppedProposals` 含 `skill_view` + argName。**实现时在 family 丢光与 drift 丢弃两处都 `recordDroppedSkill`。**

T5：Active 含 core；唯一 call `ask_user`，prompt=`请提供 flow_id（优先），或 uuid + ugid`；lock 有 KnownValues 且 HasPriorAssistant，Q=bf26Q。期望：`Decision==Retry`，Prompt 含「不要重新收集」或「任务锁」，Reason 含 `intake` 或 `ask_user`。

T7 正例：Q=`请按 rca-sync-archive-migrate 查这条流水的存档迁移`，`skill_view` 同名 → 不因 drift 丢（若 skills 族激活：Continue/Filter 保留该 call）。测 `toolOverlapsUser`/`skillNameOverlapsQ` 即可。

T6 idle：`EvaluateIdle`，assistant 含「已匹配的技能」+「请提供」，trace.DroppedProposals 已有错 skill **或** 上一步 ask_user intake；lock 有值；`GoalDriftNudges==0` → Retry。再调一次 `GoalDriftNudges==1` → Continue。

T8：Q=`那错误码是什么`，HasPriorAssistant，ask_user「请提供单号」→ Retry 拦住。

T9：Q=`帮我查一个单`，无 KnownValues、`HasPriorAssistant=false`，ask_user「请提供单号」→ Continue（放行执行）。

T6 单独 B3：无 DroppedProposals、无 ask_user 记录，仅终答「已匹配」「请提供」→ **不得** Idle Retry。

- [ ] **Step 2:** FAIL

- [ ] **Step 3: 实现要点**

`driftSensitiveTools` 增加 `skill_view`、`load_skill`、`execute_skill_script`。

`recordDroppedSkill(trace, call)`：从 args[`name`] 取 ArgName。拦掉的 intake `ask_user` 同样记进 `DroppedProposals`（`ArgName` 可空），T6 idle 的 B2 才能在停手时看到「本轮出现过 intake」。

`filterCallsByFamily` 丢掉的每个 skill_* 也 record。drift 丢掉的同样 record。

`ask_user`：**不要**放进 driftSensitive 的 token 重叠（避免 prompt 里偶然撞到 Q 的 `access`）。单独函数 `isIntakeAskUser(args)`（请提供/请给出/麻烦提供/至少一项/必填）+ `blockIntakeAskUser(lock, q)` = intake 且 `(len(KnownValues)>0 || (HasPriorAssistant && !qLooksLikeIntake(q)))`。

若本步在 family 过滤后只剩 intake ask_user 且应拦 → `PostModelRetry`（与 spec 一致）。若还有其它工具 → Filter 掉 ask_user，其余照走。

`EvaluateIdle`（`TurnIntentGate` 实现 `IdlePostModelPolicy`）：

```go
func (g TurnIntentGate) EvaluateIdle(_ context.Context, in agent.PostModelPolicyInput) agent.PostModelPolicyResult {
	lock := TaskLockFromRequest(in.Req)
	if looksLikeGoalDrift(lock, in.AssistantText, in.Trace) && in.Trace != nil && in.Trace.GoalDriftNudges == 0 {
		in.Trace.GoalDriftNudges++
		return agent.PostModelPolicyResult{
			Decision: agent.PostModelRetry,
			Reason:   "goal_drift",
			Prompt:   goalDriftRetryPrompt(lock),
		}
	}
	return agent.PostModelPolicyResult{Decision: agent.PostModelContinue}
}
```

`looksLikeGoalDrift`：B1 = DroppedProposals 里 skill_* 且 `!skillNameOverlapsQ(argName, lock.Q)`；B2 = 本轮 trace.ToolCalls 或 Dropped 含 intake ask_user 且 blockIntake；B3 = 文风「已匹配/已自动匹配」且「请提供/请给出」且 (KnownValues 非空或 HasPriorAssistant) 且 Q 非 intake。开火：`(B1 || B2) && (true)`；B3 只能与 B1|B2 同时。

`goalDriftRetryPrompt`：附 lock.Format() + 「不要调用工具。用已有工具结果直接回答任务锁中的用户问题；0 击就写未查到；禁止换成另一套排查的 intake。」可摘录 `forcedFinalSummaryPrompt` 的事实约束（不要编造）。

`Evaluate` 里 skill overlap / intake 判断同样读 `lock.Q`（不要只用 `lastUserContent`：Retry 后最后一条 user 会变成纠偏词）。缺 lock 时才 fallback `lastUserContent`。

- [ ] **Step 4:** `go test ./internal/chat/ -count=1 -run "TurnIntent|GoalDrift|Intake"`

Expected: PASS；旧 `looksLikeFinalAnswer` / GitLab 测仍过。

---

### Task 6: 接线 chat.go / agent.go / forceFinalSummary

**Files:**
- Modify: `portal/internal/service/chat.go`（`SendMessage` ~468 之后、`SendMessageStream` ~836 之后；两处 `&agent.Request{Metadata: ...}`）
- Modify: `portal/internal/service/agent.go`（~373 之后钉锁；Metadata 塞 lock）
- Modify: `framework/agent/react_agent.go`（`forceFinalSummary` 把 Metadata 里的 Q 拼进 prompt）
- 可选：`portal/internal/chat/task_lock.go` 增加 `AppendTaskLock(prompt string, lock TurnTaskLock) string`
- Test: `portal/internal/chat` 已覆盖 Format；`framework/agent` 增加 forceFinal 含 Q 的测（可用 metadata）

- [ ] **Step 1: 失败测试**

`forceFinalSummary` / `forceFinalSummaryStream` 今日签名无 `*Request`。实现时给二者增加 `req *Request`（`Run`、`runToolEventsSync`、`runToolEvents` 调用处都有 `req`），从 `req.Metadata["task_lock_q"]` 取 Q。抽出 `appendTaskLockToSummaryPrompt(base, q string)` 单测。

**锁定传递：** 同时放：

- `"task_lock"`：`*chat.TurnTaskLock`（Portal Gate 用）
- `"task_lock_q"`：`string`（framework `forceFinalSummary` 用，避免 agent→portal 循环依赖）

测：MaxSteps=1 且第一步带工具，forced summary 的最终文本由 fake 返回；更简单：单测抽出 `appendTaskLockToSummaryPrompt(base, q string)` 在 agent 包。

```go
func TestAppendTaskLockToSummaryPrompt(t *testing.T) {
	got := appendTaskLockToSummaryPrompt(forcedFinalSummaryPrompt, "原问题XYZ")
	if !strings.Contains(got, "原问题XYZ") {
		t.Fatal(got)
	}
}
```

先写这个纯函数再让 `forceFinalSummary` 调用。

- [ ] **Step 2: 实现接线**

在 `chat.go` **ListMessages 之后**（已有 history）：

```go
lock := chat.BuildTurnTaskLock(content, messagesAfterHistoryConversion /* user/assistant only */)
effectivePrompt = chat.AppendTaskLock(effectivePrompt, lock)
md := prefetchRequestMetadata(...)
if md == nil { md = map[string]any{} }
md[chat.MetadataKeyTaskLock] = &lock
md["task_lock_q"] = lock.Q
```

注意：现网先拼 `effectivePrompt` 再把 history 转成 messages。应 **用 history 的 user/assistant 正文** 建 lock（不要含即将写入的 system）。顺序：

1. ListMessages → `histMsgs`
2. `lock := BuildTurnTaskLock(userContent, histUserAssistantOnly)` — 只保留 user/assistant 正文，去掉 tool JSON；最多最近 8 条（spec §4.1）
3. 拼 effectivePrompt + AppendTaskLock
4. Request.Metadata 写入 lock

同步路径 `content`、流式 `userContent` 各做一次。Stream 的 `req := &agent.Request{Metadata: prefetch...}` 必须 merge lock。流式若在 `CreateMessage` 后**重载** history，锁必须在**最终** `history` 上重建，不要用过期切片。

`agent.go`：无 ListMessages，`BuildTurnTaskLock(content, nil)` 只钉 Q；`appendWecom` 之后 `AppendTaskLock`。`RequestMetadataFromContext` 若为 nil，与 `chat.go` 一样先建空 map 再写入 lock。

`AppendTaskLock`：空 Q 不追加；否则 `\n\n---\n\n` + Format()。

- [ ] **Step 3:** `go test ./agent/ -count=1 -run TaskLockToSummary`  
`go test ./internal/chat/ ./internal/service/ -count=1`（portal；service 若无新测至少保证编译）

Expected: PASS

`go build ./...` 在 portal + framework。

---

### Task 7: 回归包（T1–T9 清单）

**Files:** 已有测为主；补 `portal/internal/chat/task_lock_gate_test.go` 若 Task 5 未一次写全。

- [ ] **Step 1: 对照 spec §6.1 打勾**

| ID | 落点测试 |
|----|----------|
| T1 | Task 1 `TestRouteBest_shortTagEs` + Task 4 `bf26DoesNotInjectArchiveBody` |
| T2 | Task 4 高档无「优先遵循」+ Task 6 Format 在 prompt 末尾（可用 `AppendTaskLock` 单测：锁在 catalog 假字符串之后） |
| T3 | Task 3 `bf26values` |
| T4 | Task 5 family/drift dropped + DroppedProposals |
| T5 | Task 5 intake ask_user Retry |
| T6 | Task 5 EvaluateIdle；禁单独 B3 |
| T7 | Task 5 overlap 放行 |
| T8 | Task 3 + Task 5 follow-up intake 拦 |
| T9 | Task 5 首轮放行 |

- [ ] **Step 2: 全量**

Run:

```
cd framework && go test ./skills/ ./agent/ ./templates/ -count=1
cd portal && go test ./internal/chat/ -count=1
```

Expected: PASS

若 `internal/service` 有编译依赖，`go test ./internal/service/ -count=1 -timeout 60s`。

---

## 实现顺序与依赖

```
Task 1 (skills) ──┐
Task 2 (agent idle) ┤
Task 3 (task_lock) ─┼→ Task 4 (router 依赖 1) → Task 5 (gate 依赖 2+3) → Task 6 (接线) → Task 7
```

可并行：1、2、3。4 依赖 1。5 依赖 2+3。6 依赖 3+4+5。

---

## 风险

- Retry 注入的 user 消息会污染 `lastUserContent` → overlap/idle **必须**读 lock.Q。
- 仅一名技能过线时高档会灌全文（可能灌对的 vm 手册）。P0 接受；P1 再收。验收只禁存档「第 0 步」。
- `SATH_TURN_INTENT_GATE=0` 时 idle 改题一并关闭（spec 已接受）。
