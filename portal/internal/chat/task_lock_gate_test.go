package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func testLockReq(q string, lock TurnTaskLock) *agent.Request {
	lock.Q = q
	return &agent.Request{
		Messages: []model.Message{{Role: "user", Content: q}},
		Metadata: map[string]any{MetadataKeyTaskLock: &lock},
	}
}

func TestTurnIntentGate_T4SkillViewDroppedRecorded(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore, FamilyRCA})}
	trace := &agent.RunTrace{}
	lock := TurnTaskLock{Q: bf26Q, KnownValues: []string{"4_a8uva8m5tpsl"}, HasPriorAssistant: true}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(bf26Q, lock),
		AssistantText: "加载手册",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "skill_view", Arguments: map[string]any{"name": "rca-sync-archive-migrate"},
		}}},
		Trace: trace,
	})
	if res.Decision != agent.PostModelRetry && res.Decision != agent.PostModelFilter {
		t.Fatalf("want family drop or filter, got %v %q", res.Decision, res.Reason)
	}
	if res.Decision == agent.PostModelRetry && res.Reason != "family_dropped_all" {
		t.Fatalf("retry reason=%q", res.Reason)
	}
	found := false
	for _, d := range trace.DroppedProposals {
		if d.ToolName == "skill_view" && d.ArgName == "rca-sync-archive-migrate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("DroppedProposals=%#v", trace.DroppedProposals)
	}
}

func TestTurnIntentGate_T5IntakeAskUserRetry(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore})}
	lock := TurnTaskLock{KnownValues: []string{"4_a8uva8m5tpsl", "104551174"}, HasPriorAssistant: true}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(bf26Q, lock),
		AssistantText: "需要流水号",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "ask_user", Arguments: map[string]any{
				"prompt": "请提供 flow_id（优先），或 uuid + ugid",
			},
		}}},
		Trace: &agent.RunTrace{},
	})
	if res.Decision != agent.PostModelRetry {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "intake") && !strings.Contains(res.Reason, "ask_user") {
		t.Fatalf("reason=%q", res.Reason)
	}
	if !strings.Contains(res.Prompt, "不要重新收集") && !strings.Contains(res.Prompt, "任务锁") {
		t.Fatalf("prompt=%q", res.Prompt)
	}
}

func TestSkillNameOverlapsQ_T7(t *testing.T) {
	q := "请按 rca-sync-archive-migrate 查这条流水的存档迁移"
	if !skillNameOverlapsQ("rca-sync-archive-migrate", q) {
		t.Fatal("named skill must overlap Q")
	}
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore, FamilySkills})}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(q, TurnTaskLock{Q: q}),
		AssistantText: "加载",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "skill_view", Arguments: map[string]any{"name": "rca-sync-archive-migrate"},
		}}},
	})
	if res.Decision != agent.PostModelContinue && res.Decision != agent.PostModelFilter {
		t.Fatalf("on-topic skill_view must not be discarded, got %v %q", res.Decision, res.Reason)
	}
	if res.Decision == agent.PostModelFilter {
		keep := false
		for _, c := range res.ToolCalls {
			if c.Name == "skill_view" {
				keep = true
			}
		}
		if !keep {
			t.Fatalf("filter dropped on-topic skill_view: %#v", res.ToolCalls)
		}
	}
}

func TestTurnIntentGate_EvaluateIdleT6(t *testing.T) {
	gate := TurnIntentGate{}
	lock := TurnTaskLock{Q: bf26Q, KnownValues: []string{"4_a8uva8m5tpsl"}, HasPriorAssistant: true}
	req := testLockReq(bf26Q, lock)
	trace := &agent.RunTrace{
		DroppedProposals: []agent.DroppedProposal{{ToolName: "skill_view", ArgName: "rca-sync-archive-migrate"}},
	}
	text := "已匹配的技能需要先收集信息。请提供 flow_id。"
	res := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req: req, AssistantText: text, Trace: trace,
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "goal_drift" {
		t.Fatalf("first idle: %v %q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Prompt, agent.ForcedFinalSummaryPrompt) {
		t.Fatalf("idle retry must reuse forced-summary constraints, prompt=%q", res.Prompt)
	}
	if trace.GoalDriftNudges != 1 {
		t.Fatalf("nudges=%d", trace.GoalDriftNudges)
	}
	res2 := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req: req, AssistantText: text, Trace: trace,
	})
	if res2.Decision != agent.PostModelContinue {
		t.Fatalf("second idle: %v", res2.Decision)
	}
}

func TestTurnIntentGate_EvaluateIdleB3AloneDoesNotRetry(t *testing.T) {
	gate := TurnIntentGate{}
	lock := TurnTaskLock{Q: bf26Q, KnownValues: []string{"4_a8uva8m5tpsl"}, HasPriorAssistant: true}
	res := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(bf26Q, lock),
		AssistantText: "已匹配的技能。请提供 flow_id。",
		Trace:         &agent.RunTrace{},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("B3 alone must not retry, got %v %q", res.Decision, res.Reason)
	}
}

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

func TestTurnIntentGate_T8FollowupIntakeBlocked(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore})}
	q := "那错误码是什么"
	lock := TurnTaskLock{HasPriorAssistant: true}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(q, lock),
		AssistantText: "需要单号",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "ask_user", Arguments: map[string]any{"prompt": "请提供单号"},
		}}},
	})
	if res.Decision != agent.PostModelRetry {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_T9FirstTurnIntakeAllowed(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore})}
	q := "帮我查一个单"
	lock := TurnTaskLock{HasPriorAssistant: false}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(q, lock),
		AssistantText: "需要单号",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "ask_user", Arguments: map[string]any{"prompt": "请提供单号"},
		}}},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("first-turn intake must continue, got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_LoadSkillForReleaseQueryIsNotFinish(t *testing.T) {
	q := "自建环境 trace_id=bb110c9194abc73fa8471092d989d5f7,释放下这些vm"
	gate := TurnIntentGate{}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(q, TurnTaskLock{Q: q}),
		AssistantText: "让我先加载 `batchReleaseInstance` 技能，了解具体的操作流程。",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "load_skill", Arguments: map[string]any{"name": "batchReleaseInstance"},
		}}},
	})
	if res.Decision == agent.PostModelFinish {
		t.Fatalf("load_skill must not be discarded as topic_drift_all, got %v %q", res.Decision, res.Reason)
	}
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_SkillViewWrongHandbookStillTopicDrift(t *testing.T) {
	gate := TurnIntentGate{}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(bf26Q, TurnTaskLock{Q: bf26Q}),
		AssistantText: "加载手册",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "skill_view", Arguments: map[string]any{"name": "rca-sync-archive-migrate"},
		}}},
		Trace: &agent.RunTrace{},
	})
	if res.Decision != agent.PostModelFinish || res.Reason != "topic_drift_all" {
		t.Fatalf("wrong skill_view must still finish as topic drift, got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_SkillViewAfterLoadSkillSameNameContinues(t *testing.T) {
	q := "自建环境 trace_id=bb110c9194abc73fa8471092d989d5f7,释放下这些vm"
	gate := TurnIntentGate{}
	trace := &agent.RunTrace{
		ToolCalls: []agent.ToolCallRecord{{
			ToolName:  "load_skill",
			Arguments: map[string]any{"name": "batchReleaseInstance"},
		}},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(q, TurnTaskLock{Q: q}),
		AssistantText: "脚本不存在，先看手册",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "skill_view", Arguments: map[string]any{"name": "batchReleaseInstance"},
		}}},
		Trace: trace,
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("skill_view after load_skill must continue, got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_ExecuteScriptAfterLoadSkillSameNameContinues(t *testing.T) {
	q := "自建环境 trace_id=bb110c9194abc73fa8471092d989d5f7,释放下这些vm"
	gate := TurnIntentGate{}
	trace := &agent.RunTrace{
		ToolCalls: []agent.ToolCallRecord{{
			ToolName:  "load_skill",
			Arguments: map[string]any{"name": "batchReleaseInstance"},
		}},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(q, TurnTaskLock{Q: q}),
		AssistantText: "按手册执行",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "execute_skill_script", Arguments: map[string]any{
				"name": "batchReleaseInstance",
				"path": "scripts/replay.py",
			},
		}}},
		Trace: trace,
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("execute_skill_script after load_skill must continue, got %v %q", res.Decision, res.Reason)
	}
}
