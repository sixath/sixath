package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type criticFakeModel struct {
	reply string
	got   []model.Message
}

func (m *criticFakeModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: m.reply}, nil
}

func (m *criticFakeModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	m.got = msgs
	return &model.Generation{Text: m.reply}, nil
}

func (m *criticFakeModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func probeCalls(n int) []ToolCallRecord {
	out := make([]ToolCallRecord, n)
	for i := range out {
		out[i] = ToolCallRecord{ToolName: "es_log_query", Allowed: true, Arguments: map[string]any{"query": "noise"}, Result: map[string]any{"ok": true}}
	}
	return out
}

func TestCritic_FlagsConclusionCitingTimedOutProbe(t *testing.T) {
	c := NewCriticHook(CriticConfig{Enabled: true}, nil, nil)
	calls := append(probeCalls(6), ToolCallRecord{
		ToolName: "vm_run_cmd", Allowed: true,
		Arguments: map[string]any{"op": "grep", "path": `D:\game\prelaunch.log`, "pattern": "patch applied"},
		Result:    map[string]any{"ok": false, "timed_out": true, "error": "time out"},
	})
	d := c.OnStop(context.Background(), StopHookInput{Text: `D:\game\prelaunch.log 中没有 patch applied 记录，说明补丁未下发。`, ToolCalls: calls})
	if !d.Continue || d.RuleID != "critic:rules" || !strings.Contains(d.Message, CriticIssueMisreadOutput) {
		t.Fatalf("decision=%+v", d)
	}
	if d.Event == nil || d.Event.Kind != events.CriticVerdict || d.Event.Payload["verdict"] != "revise" {
		t.Fatalf("event=%+v", d.Event)
	}

	d = c.OnStop(context.Background(), StopHookInput{Text: `对 D:\game\prelaunch.log 的 grep 超时，未能确认补丁是否下发。`, ToolCalls: calls})
	if d.Continue {
		t.Fatalf("acknowledged failure must pass: %+v", d)
	}

	retried := append(calls, ToolCallRecord{ToolName: "vm_run_cmd", Allowed: true,
		Arguments: map[string]any{"op": "grep", "path": `D:\game\prelaunch.log`, "pattern": "patch applied"}, Result: map[string]any{"ok": true}})
	if d := c.OnStop(context.Background(), StopHookInput{Text: `D:\game\prelaunch.log 中没有 patch applied 记录。`, ToolCalls: retried}); d.Continue {
		t.Fatalf("successful retry must clear the issue: %+v", d)
	}
	if d := c.OnStop(context.Background(), StopHookInput{Text: `D:\game\prelaunch.log 中没有记录。`, ToolCalls: calls[4:]}); d.Continue {
		t.Fatalf("short runs are not reviewed: %+v", d)
	}
}

func TestCritic_LedgerStoppedAtMechanism(t *testing.T) {
	store := tool.NewInMemoryInvestigationStore()
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s1")
	_, _ = store.Update("s1", func(l *tool.InvestigationLedger) error {
		l.Onset, l.LastGood = "04:52", "01:17"
		l.OnsetEvidence = &tool.EvidenceLink{Quote: "a"}
		l.LastGoodEvidence = &tool.EvidenceLink{Quote: "b"}
		l.Hypotheses = []tool.Hypothesis{{ID: "h1", Statement: "计数器保护拒绝预启动", Status: tool.HypothesisAccepted, Kind: tool.HypothesisKindMechanism,
			ChangeUnavailable: "x", Evidence: []tool.EvidenceLink{{Quote: "counter", Verified: true}}}}
		return nil
	})
	calls := append(probeCalls(6), ToolCallRecord{ToolName: tool.InvestigationToolName, Allowed: true})
	c := NewCriticHook(CriticConfig{Enabled: true}, store, nil)
	d := c.OnStop(ctx, StopHookInput{Text: "根因：计数器保护。", ToolCalls: calls})
	if !d.Continue || !strings.Contains(d.Message, CriticIssueStoppedAtMechanism) {
		t.Fatalf("decision=%+v", d)
	}
	short := calls[len(calls)-2:]
	if d := c.OnStop(ctx, StopHookInput{Text: "根因：计数器保护。", ToolCalls: short}); !d.Continue {
		t.Fatalf("a run that used the ledger is an investigation even with few tool calls: %+v", d)
	}
}

func TestCritic_ModelTier(t *testing.T) {
	fm := &criticFakeModel{reply: "审查结果：\n```json\n{\"verdict\":\"revise\",\"issues\":[{\"type\":\"weak_contrast\",\"detail\":\"只对比了报错\",\"next_probe\":\"对比两台机的 config.ini\"},{\"type\":\"made_up\",\"detail\":\"x\"},{\"type\":\"misread_output\",\"detail\":\"把空结果当正常\"}]}\n```"}
	c := NewCriticHook(CriticConfig{Enabled: true}, nil, fm)
	in := StopHookInput{
		Text:      "根因：网络抖动。",
		ToolCalls: probeCalls(6),
		Messages:  []model.Message{{Role: "user", Content: "vm 255266 预启动失败"}, {Role: "user", Content: "nudge", Metadata: map[string]any{model.MetadataKeySixathOrigin: model.OriginStopHook}}},
	}
	d := c.OnStop(context.Background(), in)
	if !d.Continue || d.RuleID != "critic:model" {
		t.Fatalf("decision=%+v", d)
	}
	if strings.Contains(d.Message, "made_up") || strings.Index(d.Message, CriticIssueMisreadOutput) > strings.Index(d.Message, CriticIssueWeakContrast) {
		t.Fatalf("unknown types dropped and misread first: %s", d.Message)
	}
	if !strings.Contains(fm.got[1].Content, "vm 255266 预启动失败") || strings.Contains(fm.got[1].Content, "nudge") {
		t.Fatalf("critic must see the real user question, not harness nudges: %s", fm.got[1].Content)
	}

	fm.reply = `{"verdict":"pass","issues":[]}`
	if d := c.OnStop(context.Background(), in); d.Continue || d.Event == nil || d.Event.Payload["verdict"] != "pass" {
		t.Fatalf("pass verdict: %+v", d)
	}
	fm.reply = "not json"
	if d := c.OnStop(context.Background(), in); d.Continue {
		t.Fatalf("unparseable verdict must fail open: %+v", d)
	}
}

type alwaysContinueHook struct{ id string }

func (h alwaysContinueHook) OnStop(context.Context, StopHookInput) StopDecision {
	return StopDecision{Continue: true, RuleID: h.id, Message: "go on"}
}

func TestEvaluateStopHooks_CriticHasOwnBudget(t *testing.T) {
	fm := &criticFakeModel{reply: `{"verdict":"revise","issues":[{"type":"unsupported_claim","detail":"x"}]}`}
	critic := NewCriticHook(CriticConfig{Enabled: true, MaxRounds: 1}, nil, fm)
	a := NewReActAgent(fm, nil, tool.NewRegistry(),
		WithReActStopHooks(alwaysContinueHook{id: "rule"}, critic), WithReActMaxStopNudges(1))
	var emitted []events.Kind
	emit := func(k events.Kind, _ map[string]any) { emitted = append(emitted, k) }
	trace := &RunTrace{ToolCalls: probeCalls(6)}

	d, ok := a.evaluateStopHooks(context.Background(), "结论", nil, trace, emit)
	if !ok || d.RuleID != "rule" {
		t.Fatalf("declarative rule first: %+v", d)
	}
	trace.StopHookContinues = append(trace.StopHookContinues, d.RuleID)

	d, ok = a.evaluateStopHooks(context.Background(), "结论", nil, trace, emit)
	if !ok || d.RuleID != "critic:model" {
		t.Fatalf("critic must still run after max_stop_nudges is spent: %+v", d)
	}
	trace.StopHookContinues = append(trace.StopHookContinues, d.RuleID)
	if len(emitted) != 1 || emitted[0] != events.CriticVerdict {
		t.Fatalf("critic verdict must be emitted: %v", emitted)
	}

	if _, ok := a.evaluateStopHooks(context.Background(), "结论", nil, trace, emit); ok {
		t.Fatal("both budgets spent: must stop")
	}
}

func writeHooksYAML(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, HarnessHooksFileRel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCriticConfig(t *testing.T) {
	root := t.TempDir()
	if _, ok := WorkspaceCriticConfig(root); ok {
		t.Fatal("missing file: disabled")
	}
	writeHooksYAML(t, root, "critic:\n  enabled: true\n  max_rounds: 3\n")
	cfg, ok := WorkspaceCriticConfig(root)
	if !ok || cfg.MaxRounds != 3 {
		t.Fatalf("cfg=%+v ok=%v", cfg, ok)
	}
	writeHooksYAML(t, root, "critic:\n  enabled: false\n")
	if _, ok := WorkspaceCriticConfig(root); ok {
		t.Fatal("enabled=false: disabled")
	}
}
