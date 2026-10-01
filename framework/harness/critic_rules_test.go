package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func criticLedgerFixture(t *testing.T, fn func(l *tool.InvestigationLedger)) (tool.InvestigationStore, context.Context, []ToolCallRecord) {
	t.Helper()
	store := tool.NewInMemoryInvestigationStore()
	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Onset, l.LastGood = "08:10", "02:42"
		l.OnsetEvidence = &tool.EvidenceLink{Quote: "a"}
		l.LastGoodEvidence = &tool.EvidenceLink{Quote: "b"}
		l.Hypotheses = []tool.Hypothesis{{ID: "h1", Statement: "补丁被重启打断", Status: tool.HypothesisAccepted, Kind: tool.HypothesisKindRoot,
			Evidence: []tool.EvidenceLink{{Quote: "repair", Verified: true, Change: true}}}}
		fn(l)
		return nil
	})
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s1")
	return store, ctx, append(probeCalls(6), ToolCallRecord{ToolName: tool.InvestigationToolName, Allowed: true})
}

func TestCritic_PriorCaseIgnored(t *testing.T) {
	store, ctx, calls := criticLedgerFixture(t, func(l *tool.InvestigationLedger) { l.RecalledCases = []string{"case-1"} })
	c := NewCriticHook(CriticConfig{Enabled: true}, store, nil)
	d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁被重启打断。", ToolCalls: calls})
	if !d.Continue || !strings.Contains(d.Message, CriticIssuePriorCaseIgnored) {
		t.Fatalf("decision=%+v", d)
	}
	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Hypotheses = append(l.Hypotheses, tool.Hypothesis{ID: "h2", Status: tool.HypothesisRejected, FromCase: "case-1"})
		return nil
	})
	if d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁被重启打断。", ToolCalls: calls}); d.Continue {
		t.Fatalf("tested case must pass: %+v", d)
	}
}

func TestCritic_CaseReminderOnce(t *testing.T) {
	store, ctx, calls := criticLedgerFixture(t, func(l *tool.InvestigationLedger) { l.CasesEnabled = true })
	c := NewCriticHook(CriticConfig{Enabled: true}, store, nil)
	d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁被重启打断。", ToolCalls: calls})
	if !d.Continue || d.RuleID != criticRuleIDCase || !strings.Contains(d.Message, CriticIssueCaseNotProposed) {
		t.Fatalf("decision=%+v", d)
	}
	if d := c.OnStop(ctx, StopHookInput{Text: "x", ToolCalls: calls, Continues: []string{criticRuleIDCase}}); d.Continue {
		t.Fatalf("reminder must fire only once: %+v", d)
	}
	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error { l.CaseProposed = "case-2"; return nil })
	if d := c.OnStop(ctx, StopHookInput{Text: "x", ToolCalls: calls}); d.Continue {
		t.Fatalf("proposed case must pass: %+v", d)
	}
}

func TestCritic_UnexplainedFormChange(t *testing.T) {
	store, ctx, calls := criticLedgerFixture(t, func(l *tool.InvestigationLedger) {
		l.Hypotheses[0].Evidence = []tool.EvidenceLink{{Quote: "patch applied repair_mode=on", Verified: true, Change: true}}
	})
	timeline := ToolCallRecord{ToolName: tool.TimelineToolName, Allowed: true, Result: map[string]any{
		"ok": true, "interval": "1h",
		"suggested_onset": map[string]any{"time": "2026-09-20T10:30:00.000Z"},
		"change_points": []map[string]any{
			{"time": "2026-09-20T10:30:00.000Z", "kind": "starts", "detail": "failed (failure) first appears"},
			{"time": "2026-09-21T03:00:00.000Z", "kind": "starts", "detail": "timeout (failure) first appears"},
		},
	}}
	calls = append(calls, timeline)
	c := NewCriticHook(CriticConfig{Enabled: true}, store, nil)
	d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁被重启打断。", ToolCalls: calls})
	if !d.Continue || !strings.Contains(d.Message, CriticIssueUnexplainedForm) || strings.Contains(d.Message, "10:30") {
		t.Fatalf("decision=%+v", d)
	}

	calls = append(calls, ToolCallRecord{ToolName: tool.FieldHistoryToolName, Allowed: true, Result: map[string]any{
		"ok": true, "transitions": []map[string]any{{"time": "2026-09-21T02:50:00.000Z", "from": "300", "to": "600",
			"quote": "2026-09-21T02:50:00Z set wait_timeout_sec=600", "last_before": "set wait_timeout_sec=300"}},
	}})
	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Hypotheses[0].Evidence = append(l.Hypotheses[0].Evidence, tool.EvidenceLink{Quote: "set wait_timeout_sec=600", Verified: true, Change: true})
		return nil
	})
	if d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁被重启打断。", ToolCalls: calls}); d.Continue {
		t.Fatalf("explained form change must pass: %+v", d)
	}
}

func TestCritic_RootChangeAfterOnset(t *testing.T) {
	store, ctx, calls := criticLedgerFixture(t, func(l *tool.InvestigationLedger) {
		l.Onset = "2026-09-26 22:41:10"
		l.OnsetEvidence = &tool.EvidenceLink{Quote: "2026-09-26 22:41:10 ERROR vm=300700 prestart game launch failed exit_code=1", Verified: true}
		l.Hypotheses[0].Evidence = []tool.EvidenceLink{{Quote: "2026-09-27 09:55:12 INFO config push prestart.wait_timeout_sec=600", Verified: true, Change: true}}
	})
	c := NewCriticHook(CriticConfig{Enabled: true}, store, nil)
	d := c.OnStop(ctx, StopHookInput{Text: "根因：超时配置被改为 600 秒。", ToolCalls: calls})
	if !d.Continue || !strings.Contains(d.Message, CriticIssueUnexplainedChange) {
		t.Fatalf("a root change after onset must be flagged: %+v", d)
	}
	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Hypotheses[0].Evidence = append(l.Hypotheses[0].Evidence, tool.EvidenceLink{Quote: "2026-09-26 22:36:30 INFO patch agent: apply hotfix-0926 mode=repair", Verified: true, Change: true})
		return nil
	})
	if d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁修复被中断。", ToolCalls: calls}); d.Continue {
		t.Fatalf("a change before onset must pass: %+v", d)
	}
}

func TestCritic_UnverifiedCodeMeaning(t *testing.T) {
	calls := probeCalls(6)
	c := NewCriticHook(CriticConfig{Enabled: true}, nil, nil)
	d := c.OnStop(context.Background(), StopHookInput{Text: "安装失败，错误码 1603 表示安装包损坏。", ToolCalls: calls})
	if !d.Continue || !strings.Contains(d.Message, CriticIssueUnverifiedCode) {
		t.Fatalf("decision=%+v", d)
	}
	if d := c.OnStop(context.Background(), StopHookInput{Text: "| 错误码 | 1603 (安装程序初始化失败) | 0 (成功) |", ToolCalls: calls}); !d.Continue || !strings.Contains(d.Message, CriticIssueUnverifiedCode) {
		t.Fatalf("a glossed code in a table must be flagged: %+v", d)
	}
	if d := c.OnStop(context.Background(), StopHookInput{Text: "安装返回错误码 1603。", ToolCalls: calls}); d.Continue {
		t.Fatalf("quoting a code without interpreting it must pass: %+v", d)
	}
	withCode := append(calls, ToolCallRecord{ToolName: "rca_grep", Allowed: true, Result: map[string]any{"ok": true, "matches": []any{"ERROR_INSTALL_FAILURE = 1603 // fatal error during installation"}}})
	if d := c.OnStop(context.Background(), StopHookInput{Text: "安装失败，错误码 1603 表示安装包损坏。", ToolCalls: withCode}); d.Continue {
		t.Fatalf("code read in source must pass: %+v", d)
	}
}

func TestCritic_MissingProducerEvidence(t *testing.T) {
	store, ctx, calls := criticLedgerFixture(t, func(l *tool.InvestigationLedger) {
		l.Hypotheses = append(l.Hypotheses, tool.Hypothesis{ID: "h2", Statement: "游戏没写就绪标记", Status: tool.HypothesisAccepted,
			Kind: tool.HypothesisKindTrigger, Absence: "cloud_game_init_finish", ChangeUnavailable: "x",
			Evidence: []tool.EvidenceLink{{Quote: "ret=false", Verified: true}}})
	})
	c := NewCriticHook(CriticConfig{Enabled: true}, store, nil)
	d := c.OnStop(ctx, StopHookInput{Text: "根因：补丁被重启打断。", ToolCalls: calls})
	if !d.Continue || !strings.Contains(d.Message, CriticIssueMissingProducer) {
		t.Fatalf("decision=%+v", d)
	}
}
