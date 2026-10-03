package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

func suspectRec(name string) ToolCallRecord {
	return ToolCallRecord{ToolName: name, Allowed: true, Result: map[string]any{
		"hit_status": tool.HitStatusSuspect,
		"diagnosis":  &executor.Diagnosis{Hint: "without:service:foo returned 57"},
	}}
}

func TestSuspectHook(t *testing.T) {
	h := NewSuspectEvidenceHook()
	ctx := context.Background()

	d := h.OnStop(ctx, StopHookInput{Text: "经查询，没有数据。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}})
	if !d.Continue || !strings.HasPrefix(d.RuleID, SuspectEvidenceRuleID) || !strings.Contains(d.Message, "without:service:foo returned 57") {
		t.Fatalf("got %+v", d)
	}
	if d := h.OnStop(ctx, StopHookInput{Text: "共找到 3 条记录。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}}); d.Continue {
		t.Fatal("no negative claim")
	}
	if d := h.OnStop(ctx, StopHookInput{Text: "如果查不到，请扩大时间范围。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}}); d.Continue {
		t.Fatal("conditional sentence is exempt")
	}
	hits := ToolCallRecord{ToolName: "execute_read", Allowed: true, Result: map[string]any{"hit_status": tool.HitStatusHits}}
	if d := h.OnStop(ctx, StopHookInput{Text: "查不到相关日志。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query"), hits}}); d.Continue {
		t.Fatal("other evidence with hits: do not nudge")
	}
	fixture := ToolCallRecord{ToolName: "es_log_query", Allowed: true, Result: `{"hit_status":"suspect","diagnosis":{"hint":"probe hint"}}`}
	if d := h.OnStop(ctx, StopHookInput{Text: "没有数据", ToolCalls: []ToolCallRecord{fixture}}); !d.Continue || !strings.Contains(d.Message, "probe hint") {
		t.Fatalf("JSON string results (eval fixtures) must be understood: %+v", d)
	}
	glob := ToolCallRecord{ToolName: "rca_glob", Allowed: true, Result: map[string]any{"hit_status": tool.HitStatusSuspect, "roots_missing": []string{"src/x"}}}
	if d := h.OnStop(ctx, StopHookInput{Text: "No data found.", ToolCalls: []ToolCallRecord{glob}}); !d.Continue || !strings.Contains(d.Message, "src/x") {
		t.Fatalf("roots_missing hint: %+v", d)
	}
	empty := ToolCallRecord{ToolName: "es_log_query", Allowed: true, Result: map[string]any{"hit_status": tool.HitStatusEmpty}}
	if d := h.OnStop(ctx, StopHookInput{Text: "No data found.", ToolCalls: []ToolCallRecord{empty}}); d.Continue {
		t.Fatal("only empty (no suspect): do not nudge")
	}
	if p, max := h.(BudgetedStopHook).StopBudget(); p != SuspectEvidenceRuleID || max != 1 {
		t.Fatalf("budget %q %d", p, max)
	}
}

func TestDefaultStopHooksEnv(t *testing.T) {
	if hooks := DefaultStopHooks(); len(hooks) != 1 || !HasStopHookPrefix(hooks, SuspectEvidenceRuleID) {
		t.Fatal("default on")
	}
	t.Setenv(EnvStopSuspect, "off")
	if len(DefaultStopHooks()) != 0 {
		t.Fatal("env off")
	}
	if HasStopHookPrefix([]StopHook{alwaysContinueHook{id: "rule"}}, SuspectEvidenceRuleID) {
		t.Fatal("plain hooks have no budget prefix")
	}
}

func TestEvaluateStopHooks_SuspectHasOwnBudget(t *testing.T) {
	a := NewReActAgent(&criticFakeModel{}, nil, tool.NewRegistry(),
		WithReActStopHooks(alwaysContinueHook{id: "rule"}, NewSuspectEvidenceHook()), WithReActMaxStopNudges(1))
	trace := &RunTrace{ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}}

	d, ok := a.evaluateStopHooks(context.Background(), "没有数据。", nil, trace, nil)
	if !ok || d.RuleID != "rule" {
		t.Fatalf("declarative rule first: %+v", d)
	}
	trace.StopHookContinues = append(trace.StopHookContinues, d.RuleID)

	d, ok = a.evaluateStopHooks(context.Background(), "没有数据。", nil, trace, nil)
	if !ok || d.RuleID != SuspectEvidenceRuleID {
		t.Fatalf("suspect hook must still fire after max_stop_nudges is spent: %+v", d)
	}
	trace.StopHookContinues = append(trace.StopHookContinues, d.RuleID)

	if _, ok := a.evaluateStopHooks(context.Background(), "没有数据。", nil, trace, nil); ok {
		t.Fatal("suspect hook fires at most once")
	}
}

func TestEvaluateStopHooks_SuspectDoesNotConsumeSharedBudget(t *testing.T) {
	a := NewReActAgent(&criticFakeModel{}, nil, tool.NewRegistry(),
		WithReActStopHooks(NewSuspectEvidenceHook(), alwaysContinueHook{id: "rule"}), WithReActMaxStopNudges(1))
	trace := &RunTrace{ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}}

	d, ok := a.evaluateStopHooks(context.Background(), "没有数据。", nil, trace, nil)
	if !ok || d.RuleID != SuspectEvidenceRuleID {
		t.Fatalf("suspect first: %+v", d)
	}
	trace.StopHookContinues = append(trace.StopHookContinues, d.RuleID)

	d, ok = a.evaluateStopHooks(context.Background(), "没有数据。", nil, trace, nil)
	if !ok || d.RuleID != "rule" {
		t.Fatalf("declarative rule keeps its shared budget: %+v", d)
	}
}
