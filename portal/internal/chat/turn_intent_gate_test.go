package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

func TestTurnIntentGate_FamilyPartialDropFiltersCalls(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, "mcp:gitlab"}),
		ToolFamily: map[string]string{
			"jaeger_trace":  FamilyRCA,
			"list_projects": "mcp:gitlab",
		},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查 GitLab 项目"}}},
		AssistantText: "查项目",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "jaeger_trace", Arguments: map[string]any{"service": "gitlab"}},
			{ID: "2", Name: "list_projects", Arguments: map[string]any{"search": "gitlab"}},
		}},
	})
	if res.Decision != agent.PostModelFilter {
		t.Fatalf("decision=%v reason=%q", res.Decision, res.Reason)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "list_projects" {
		t.Fatalf("tool calls=%#v", res.ToolCalls)
	}
}

func TestTurnIntentGate_BuiltinFamilyWhenNotInToolFamilyIndex(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, "mcp:gitlab"}),
		ToolFamily: map[string]string{
			"list_projects": "mcp:gitlab",
		},
	}
	onlyRCA := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查 GitLab 项目"}}},
		AssistantText: "查 trace",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "jaeger_trace", Arguments: map[string]any{"service": "gitlab"},
		}}},
	})
	if onlyRCA.Decision != agent.PostModelRetry || onlyRCA.Reason != "family_dropped_all" {
		t.Fatalf("jaeger only: %v %q", onlyRCA.Decision, onlyRCA.Reason)
	}
	partial := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查 GitLab 项目"}}},
		AssistantText: "查项目",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "jaeger_trace", Arguments: map[string]any{"service": "gitlab"}},
			{ID: "2", Name: "list_projects", Arguments: map[string]any{"search": "gitlab"}},
		}},
	})
	if partial.Decision != agent.PostModelFilter || partial.Reason != "family_partial" {
		t.Fatalf("partial: %v %q", partial.Decision, partial.Reason)
	}
	if len(partial.ToolCalls) != 1 || partial.ToolCalls[0].Name != "list_projects" {
		t.Fatalf("tool calls=%#v", partial.ToolCalls)
	}
}

func TestTurnIntentGate_FamilyNotActive(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, "mcp:gitlab"}),
		ToolFamily:     map[string]string{"jaeger_trace": FamilyRCA, "list_projects": "mcp:gitlab"},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查 GitLab 项目"}}},
		AssistantText: "查 trace",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "jaeger_trace", Arguments: map[string]any{"service": "gitlab"},
		}}},
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "family_dropped_all" {
		t.Fatalf("%v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_FamilyActiveContinuesThenTopicRules(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, FamilyWeb}),
		ToolFamily:     map[string]string{"web_search": FamilyWeb},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查一下七日无理由退货的法律规定"}}},
		AssistantText: "检索",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "web_search", Arguments: map[string]any{"query": "七日无理由退货 法律规定"},
		}}},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("%v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_FinalAnswerDiscardsTools(t *testing.T) {
	gate := TurnIntentGate{}
	text := strings.Repeat("模块调用关系分析如下：A→B→C。细节若干，继续说明架构分层与依赖。", 4) + "如有需要请告诉我。"
	if !looksLikeFinalAnswer(text) {
		t.Fatalf("fixture text should look like final answer (runes=%d)", utf8RuneCount(text))
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "分析 cloudgame 仓库模块"}}},
		AssistantText: text,
		ToolStep: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "1",
				Name:      "web_search",
				Arguments: map[string]any{"query": "七日无理由退货"},
			}},
		},
	})
	if res.Decision != agent.PostModelFinish {
		t.Fatalf("decision=%v reason=%q", res.Decision, res.Reason)
	}
	if res.Reason != "final_answer_discard_tools" {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestTurnIntentGate_TopicDriftDropsWebSearch(t *testing.T) {
	gate := TurnIntentGate{}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "分析 cloudgame 仓库模块调用关系"}}},
		AssistantText: "继续查找资料",
		ToolStep: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "1",
				Name:      "web_search",
				Arguments: map[string]any{"query": "消费者权益保护法 七日无理由退货"},
			}},
		},
	})
	if res.Decision != agent.PostModelFinish {
		t.Fatalf("expected finish on all-drift, got %v %q", res.Decision, res.Reason)
	}
	if res.Reason != "topic_drift_all" {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestTurnIntentGate_OnTopicWebSearchContinues(t *testing.T) {
	gate := TurnIntentGate{}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查一下七日无理由退货的法律规定"}}},
		AssistantText: "正在检索",
		ToolStep: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "1",
				Name:      "web_search",
				Arguments: map[string]any{"query": "七日无理由退货 法律规定"},
			}},
		},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("expected continue, got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_NonSensitiveToolsNotFiltered(t *testing.T) {
	gate := TurnIntentGate{}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "分析 cloudgame"}}},
		AssistantText: "读取文件",
		ToolStep: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "1",
				Name:      "rca_read",
				Arguments: map[string]any{"path": "main.go"},
			}},
		},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("rca_read should continue, got %v", res.Decision)
	}
}

func TestTurnIntentGate_DiscoveryLoopAfterExecuteReadRetries(t *testing.T) {
	gate := TurnIntentGate{}
	req := &agent.Request{Messages: []model.Message{{Role: "user", Content: "统计最近一天丢弃存档的量"}}}
	trace := &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{{
		ToolName: "execute_read", Allowed: true,
		Result: map[string]any{"Columns": []string{"cnt"}, "Rows": []any{[]any{0}}},
	}}}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           req,
		AssistantText: "再看表结构",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "describe_table", Arguments: map[string]any{"table_name": "t_user_archive_delete_record"}},
			{ID: "2", Name: "list_tables", Arguments: map[string]any{"datasource_id": "cgarchive"}},
		}},
		Trace: trace,
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "discovery_loop" {
		t.Fatalf("decision=%v reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Prompt, "直接回答") {
		t.Fatalf("prompt=%q", res.Prompt)
	}
}

func TestTurnIntentGate_DescribeTableBeforeExecuteReadContinues(t *testing.T) {
	gate := TurnIntentGate{}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "统计最近一天丢弃存档的量"}}},
		AssistantText: "先看表",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "describe_table", Arguments: map[string]any{"table_name": "t_user_archive_delete_record"}},
		}},
		Trace: &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{{
			ToolName: "list_tables", Allowed: true,
		}}},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("schema-before-query must continue, got %v %q", res.Decision, res.Reason)
	}
}

func TestBuildReActAgent_TurnIntentGateOptionSetsActiveFamilies(t *testing.T) {
	t.Setenv(turnIntentGateEnv, "1")
	reg := tool.NewRegistry()
	fake := &builderGateFake{finalReply: "ok"}
	active := familySet([]string{FamilyCore, "mcp:gitlab"})
	toolFamily := map[string]string{"list_projects": "mcp:gitlab"}

	// Mirror BuildReActAgent option order: default gate then extra override (last wins).
	cfg := agent.ReActConfig{}
	if gate := NewTurnIntentGate(); gate != nil {
		agent.WithReActPostModelPolicy(gate)(&cfg)
	}
	TurnIntentGateOption(active, toolFamily)(&cfg)
	g, ok := cfg.PostModelPolicy.(TurnIntentGate)
	if !ok {
		t.Fatalf("PostModelPolicy type=%T want TurnIntentGate", cfg.PostModelPolicy)
	}
	if !FamilyActive(g.ActiveFamilies, "mcp:gitlab") || !FamilyActive(g.ActiveFamilies, FamilyCore) {
		t.Fatalf("ActiveFamilies=%v", g.ActiveFamilies)
	}
	if g.ToolFamily["list_projects"] != "mcp:gitlab" {
		t.Fatalf("ToolFamily=%v", g.ToolFamily)
	}

	a := BuildReActAgent(fake, reg, "", 10, TurnIntentGateOption(active, toolFamily))
	if _, ok := a.(*agent.ReActAgent); !ok {
		t.Fatalf("expected *ReActAgent, got %T", a)
	}
}

func TestTurnIntentGateOption_NoopWhenDisabled(t *testing.T) {
	t.Setenv(turnIntentGateEnv, "0")
	cfg := agent.ReActConfig{}
	TurnIntentGateOption(familySet([]string{FamilyCore}), nil)(&cfg)
	if cfg.PostModelPolicy != nil {
		t.Fatalf("expected no-op when gate off, got %#v", cfg.PostModelPolicy)
	}
}

func TestRegisterAgentRuntimeTools_WebDisabledNoBochaFallback(t *testing.T) {
	t.Setenv("SATH_BOCHA_API_KEY", "test-key-should-not-auto-register")
	SetWebSettings(WebSettings{BochaAPIKey: "test-key-should-not-auto-register"})
	t.Cleanup(func() { SetWebSettings(WebSettings{}) })

	reg := tool.NewRegistry()
	flags := HermesP0ToolFlags{WebToolsEnabled: false}
	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{Flags: &flags}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("web_search"); ok {
		t.Fatal("web_search must not register when WebToolsEnabled=false")
	}
}

func TestRegisterAgentRuntimeTools_CodeSurfaceOmitsSkillsAndMemory(t *testing.T) {
	t.Setenv(toolFamilySplitEnv, "1")
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "demo")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n# D\n"), 0o644)
	idx, err := skills.NewIndex([]string{filepath.Join(dir, "skills")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	flags := HermesP0ToolFlags{SkillRuntimeManageEnabled: true, MemoryWriteEnabled: true}
	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{
		Flags:          &flags,
		SkillsIdx:      idx,
		ActiveFamilies: familySet([]string{FamilyCore, FamilyCode}),
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skill_view", "load_skill", "memory_recall", "memory_search"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("%s must not register on code-only surface", name)
		}
	}
}

func TestTurnIntentGate_SkillViewDroppedOnCodeSurface(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, FamilyCode}),
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "union在注册的时候会发生什么"}}},
		AssistantText: "先加载技能",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "rca_grep", Arguments: map[string]any{"pattern": "Register"}},
			{ID: "2", Name: "skill_view", Arguments: map[string]any{"name": "migu-cloud-game-vm-allocate"}},
			{ID: "3", Name: "list_tables", Arguments: map[string]any{"datasource_id": "migu_mongodb"}},
		}},
	})
	if res.Decision != agent.PostModelFilter || res.Reason != "family_partial" {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "rca_grep" {
		t.Fatalf("keep only rca_grep, got %#v", res.ToolCalls)
	}
}

func TestTurnIntentGate_OnlySkillViewRetries(t *testing.T) {
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
	if !strings.Contains(res.Prompt, FamilyCode) {
		t.Fatalf("prompt=%q", res.Prompt)
	}
}

func TestTurnIntentGate_ParallelRCAGrepKept(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore, FamilyCode})}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "union注册"}}},
		AssistantText: "搜索",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "rca_grep", Arguments: map[string]any{"pattern": "Register"}},
			{ID: "2", Name: "rca_grep", Arguments: map[string]any{"pattern": "1105"}},
		}},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
}

func TestAppendTurnIntentPrompt_ContainsBoundary(t *testing.T) {
	got := AppendTurnIntentPrompt("")
	if !strings.Contains(got, "任务边界") {
		t.Fatalf("missing hint: %q", got)
	}
	if !strings.Contains(got, "立即作答结束") {
		t.Fatalf("missing stop-after-lookup hint: %q", got)
	}
}

func TestLooksLikeFinalAnswer(t *testing.T) {
	short := "请告诉我"
	if looksLikeFinalAnswer(short) {
		t.Fatal("short text should not count")
	}
	long := strings.Repeat("分析内容。", 20) + "如有需要请告诉我。"
	if !looksLikeFinalAnswer(long) {
		t.Fatal("expected final answer cue")
	}
}
