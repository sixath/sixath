package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/model"
)

func TestEvalGolden_bf26(t *testing.T) {
	lock := BuildTurnTaskLock(bf26Q, []model.Message{
		{Role: "user", Content: "这条流水4_a8uva8m5tpsl 正常吗"},
		{Role: "assistant", Content: "流水 4_a8uva8m5tpsl 正常。uid=104551174 ugid=796"},
		{Role: "user", Content: bf26Q},
	})
	if lock.Q != bf26Q || lock.Delivery != "" {
		t.Fatalf("%+v", lock)
	}
}

func TestEvalGolden_8555(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore, FamilyCode})}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "会发生什么"}}},
		AssistantText: "加载手册",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "skill_view", Arguments: map[string]any{"name": "demo"}},
		}},
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "family_dropped_all" {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
}

const meaESGoal = "用 elasticsearch 查一下错误日志"

func TestEvalGolden_19a34f_identLock(t *testing.T) {
	lock := BuildTurnTaskLock(sess19a34fQ, []model.Message{
		{Role: "user", Content: sess19a34fOld + " 这个呢"},
		{Role: "assistant", Content: "自建 4 条 vmid 226667"},
		{Role: "user", Content: sess19a34fQ},
	})
	if len(lock.TraceIDs) != 1 || lock.TraceIDs[0] != sess19a34fTrace {
		t.Fatalf("TraceIDs=%v", lock.TraceIDs)
	}
	gate := TurnIntentGate{}
	idle := gate.EvaluateIdle(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(sess19a34fQ, lock),
		AssistantText: "自建 4 条都没有 1058",
		Trace:         &agent.RunTrace{},
	})
	if idle.Decision != agent.PostModelRetry || idle.Reason != "ident_lock" {
		t.Fatalf("idle %v %q", idle.Decision, idle.Reason)
	}
	wrong := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           testLockReq(sess19a34fQ, lock),
		AssistantText: "拉 jaeger",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "http_request", Arguments: map[string]any{
				"method": "GET", "url": "http://cg-pro-jaeger.yuntiancloud.com/api/traces/" + sess19a34fOld,
			},
		}}},
		Trace: &agent.RunTrace{},
	})
	if wrong.Decision != agent.PostModelRetry || wrong.Reason != "ident_lock" {
		t.Fatalf("wrong http %v %q", wrong.Decision, wrong.Reason)
	}
}

func TestEvalGolden_mea_no_fence(t *testing.T) {
	got := AutoChecks(meaESGoal)
	if len(got) != 2 || got[0].Type != "trace_hit_status" || got[1].Type != "empty_hit_speak" {
		t.Fatalf("%#v", got)
	}
}

func TestEvalGolden_mea_chat_skip(t *testing.T) {
	for _, s := range []string{"你好", "有哪些技能", "继续", bf26Q} {
		if got := AutoChecks(s); len(got) != 0 {
			t.Fatalf("%q → %#v", s, got)
		}
	}
}

func TestShouldUseMEA_predicates(t *testing.T) {
	es := AutoChecks(meaESGoal)
	if !ShouldUseMEA(true, "", es, nil) {
		t.Fatal("C5 traceOnly + empty workspace")
	}
	if ShouldUseMEA(true, "", es, []string{"done"}) {
		t.Fatal("traceOnly + acceptance + empty workspace must not enter")
	}
	if ShouldUseMEA(false, "/ws", es, nil) {
		t.Fatal("disabled must not enter")
	}
	if ShouldUseMEA(true, "/ws", nil, nil) {
		t.Fatal("C4 no checks no acceptance")
	}
	file := []mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}}
	if ShouldUseMEA(true, "", file, nil) {
		t.Fatal("C6 file checks need workspace")
	}
	if !ShouldUseMEA(true, "/ws", file, nil) {
		t.Fatal("file checks + workspace")
	}
	got := ResolveAcceptanceChecks(file, true, meaESGoal)
	if len(got) != 1 || got[0].Type != "path_exists" {
		t.Fatalf("C7 %#v", got)
	}
	got = ResolveAcceptanceChecks(nil, false, meaESGoal)
	if len(got) != 2 {
		t.Fatalf("no fence → AutoChecks %#v", got)
	}
	if !ShouldUseMEA(true, "/ws", AutoChecks("你好"), []string{"done"}) {
		t.Fatal("acceptance-only + workspace still enters")
	}
}

func TestAutoChecks_deliveryUsesQ(t *testing.T) {
	hist := []model.Message{
		{Role: "user", Content: meaESGoal},
		{Role: "assistant", Content: "正在查"},
		{Role: "user", Content: "没有打印出来呀"},
	}
	lock := BuildTurnTaskLock("没有打印出来呀", hist)
	if lock.Q != meaESGoal {
		t.Fatalf("Q=%q", lock.Q)
	}
	if len(AutoChecks(lock.Q)) != 2 {
		t.Fatal("AutoChecks(G) must be non-empty")
	}
	if len(AutoChecks(lock.Delivery)) != 0 {
		t.Fatalf("AutoChecks(D) must be empty, D=%q", lock.Delivery)
	}
}

func TestMEAAcceptancePrompt_traceOnly(t *testing.T) {
	p := MEAAcceptancePrompt(AutoChecks(meaESGoal), nil)
	if strings.Contains(p, "produce environment state") {
		t.Fatal(p)
	}
	if !strings.Contains(p, "hit_status") {
		t.Fatal(p)
	}
}

func TestMEAAcceptancePrompt_fileKeepsEnv(t *testing.T) {
	p := MEAAcceptancePrompt([]mea.AcceptanceCheck{{Type: "path_exists", Path: "out.txt"}}, nil)
	if !strings.Contains(p, "produce environment state") {
		t.Fatal(p)
	}
}

func TestEvalGolden_deny_write_files(t *testing.T) {
	if DefaultHermesP0ToolFlags.WorkspaceFilesEnabled {
		t.Fatal("workspace files must default off (E5 opt-in)")
	}
	var zero HermesP0ToolFlags
	if zero.WorkspaceFilesEnabled {
		t.Fatal("HermesP0ToolFlags zero value must deny write_file")
	}
}

func TestEvalGolden_close_gate_chat(t *testing.T) {
	if ShouldApplyEvidenceGate(nil, "你好") {
		t.Fatal("hello")
	}
	if ShouldApplyEvidenceGate(nil, "有哪些技能") {
		t.Fatal("skills")
	}
	if ShouldApplyEvidenceGate(nil, bf26Q) {
		t.Fatal("bf26Q must not open close-gate (no RCA keywords)")
	}
	if !ShouldApplyEvidenceGate(nil, meaESGoal) {
		t.Fatal("es goal")
	}
	if len(AutoChecks("你好")) != 0 {
		t.Fatal("share C chat skip; do not fork keyword table")
	}
}

func TestEvalGolden_code_model(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })

	chat := stubTurnModel{name: "chat"}

	got, err := ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{})
	if !errors.Is(err, ErrCodeModelRequired) || got != nil {
		t.Fatalf("missing spec must error, got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(familySet([]string{FamilyCore}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{CodeModel: "gpt-code", CodeAPIKey: "k", CodeProvider: "openai", CodeBaseURL: "http://127.0.0.1:9"},
	})
	if err != nil || got != chat {
		t.Fatalf("non-code must keep chat: got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(nil, chat, biz.AgentMeta{})
	if err != nil || got != chat {
		t.Fatalf("nil active must keep chat: got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{
			Provider: "openai", Model: "gpt-chat",
			CodeProvider: "openai", CodeModel: "gpt-code", CodeAPIKey: "sk-test", CodeBaseURL: "http://127.0.0.1:9",
		},
	})
	if err != nil || got == nil || got == chat {
		t.Fatalf("configured code model must swap: got=%v err=%v", got, err)
	}
}
