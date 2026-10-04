package investigate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func rcaStubWithEvidence() tool.Tool {
	t := stubTool("rca_grep")
	t.Execute = func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]any{
			"ok": true,
			"evidence_refs": []map[string]any{{
				"kind": "code", "repo": "myrepo", "path": "internal/foo.go", "line": 42,
			}},
		}, nil
	}
	return t
}

func TestExecute_FullFlowReturnsConclusion(t *testing.T) {
	m := &fakeToolModel{finalText: "空响应由缓存未命中导致\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	if err := Register(reg, Config{Model: m}); err != nil {
		t.Fatal(err)
	}
	toolEntry, ok := reg.Get(ToolName)
	if !ok {
		t.Fatal("tool not registered")
	}
	out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "为什么接口偶尔返回空"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map result, got %T", out)
	}
	if res["ok"] != true {
		t.Fatalf("want ok=true, got %v", res)
	}
	conclusion, _ := res["conclusion"].(string)
	if !strings.Contains(conclusion, "缓存未命中") {
		t.Fatalf("conclusion missing model text: %q", conclusion)
	}
	if strings.Contains(conclusion, "状态:") {
		t.Fatalf("status line must be stripped from conclusion: %q", conclusion)
	}
	if res["insufficient_evidence"] != false {
		t.Fatalf("want insufficient_evidence=false, got %v", res["insufficient_evidence"])
	}
	if res["steps_taken"] != 1 {
		t.Fatalf("want steps_taken=1, got %v", res["steps_taken"])
	}
	refs, _ := res["evidence_refs"].([]tool.EvidenceRef)
	if len(refs) != 1 || refs[0].Path != "internal/foo.go" {
		t.Fatalf("evidence refs not collected: %#v", res["evidence_refs"])
	}
}

func TestExecute_InsufficientEvidenceParsed(t *testing.T) {
	m := &fakeToolModel{finalText: "查了代码和三处日志都没有发现异常\n状态: 证据不足"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	_ = Register(reg, Config{Model: m})
	toolEntry, _ := reg.Get(ToolName)
	out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "为什么慢"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	res := out.(map[string]any)
	if res["insufficient_evidence"] != true {
		t.Fatalf("want insufficient_evidence=true, got %v", res["insufficient_evidence"])
	}
}

func TestExecute_EmptyQuestionRejected(t *testing.T) {
	m := &fakeToolModel{finalText: "x"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	_ = Register(reg, Config{Model: m})
	toolEntry, _ := reg.Get(ToolName)
	out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "   "})
	if err == nil {
		t.Fatal("empty question must return error")
	}
	res := out.(map[string]any)
	if res["ok"] != false || res["error_code"] != tool.ErrorPermanent {
		t.Fatalf("want permanent error payload, got %#v", res)
	}
	if m.calls != 0 {
		t.Fatalf("model must not be called for empty question, got %d calls", m.calls)
	}
}

func TestExecute_SubRegistryExcludesSelfAndUnrelated(t *testing.T) {
	m := &fakeToolModel{finalText: "结论\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	_ = reg.Register(stubTool("write_file")) // 不应进入子 registry
	_ = Register(reg, Config{Model: m})
	toolEntry, _ := reg.Get(ToolName)
	if _, err := toolEntry.Execute(context.Background(), map[string]any{"question": "q"}); err != nil {
		t.Fatal(err)
	}
	if m.lastReg == nil {
		t.Fatal("model never received a registry")
	}
	if _, ok := m.lastReg.Get(ToolName); ok {
		t.Fatal("sub registry must not contain deep_investigate itself (recursion)")
	}
	if _, ok := m.lastReg.Get("write_file"); ok {
		t.Fatal("unrelated tools must not enter sub registry")
	}
	if _, ok := m.lastReg.Get("rca_grep"); !ok {
		t.Fatal("code tools must be in sub registry")
	}
	if _, ok := m.lastReg.Get(tool.InvestigationToolName); ok {
		t.Fatal("ledger must not appear when the parent does not have it")
	}
}

func TestExecute_SubRegistryInheritsLedgerAndExtraOptions(t *testing.T) {
	m := &fakeToolModel{finalText: "结论\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	if err := tool.RegisterInvestigationTool(reg, tool.NewInMemoryInvestigationStore()); err != nil {
		t.Fatal(err)
	}
	var observed []string
	_ = Register(reg, Config{Model: m, ExtraOptions: []harness.ReActOption{
		harness.WithReActToolSuccessHook(func(_ context.Context, _ *harness.Request, rec harness.ToolCallRecord) {
			observed = append(observed, rec.ToolName)
		}),
	}})
	toolEntry, _ := reg.Get(ToolName)
	if _, err := toolEntry.Execute(context.Background(), map[string]any{"question": "q"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.lastReg.Get(tool.InvestigationToolName); !ok {
		t.Fatal("sub agent must share the parent's ledger tool")
	}
	if len(observed) != 1 || observed[0] != "rca_grep" {
		t.Fatalf("extra options must reach the sub agent: observed=%v", observed)
	}
}

// unavailableToolModel 在 toolFirst 时先发起一次 rca_grep，之后所有调用都返回 401。
type unavailableToolModel struct {
	fakeToolModel
	toolFirst bool
}

func (f *unavailableToolModel) ChatWithTools(ctx context.Context, msgs []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	f.calls++
	if f.toolFirst && f.calls == 1 {
		return &model.Generation{Raw: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "call-1", Name: "rca_grep", Arguments: map[string]any{"pattern": "x"},
		}}}}, nil
	}
	return nil, &model.APIStatusError{StatusCode: 401, Message: "invalid token"}
}

func TestExecute_SubAgentModelUnavailableReportsTransient(t *testing.T) {
	t.Setenv(harness.EnvModelRecoveryDelays, "")
	for _, toolFirst := range []bool{true, false} {
		m := &unavailableToolModel{toolFirst: toolFirst}
		reg := tool.NewEmptyRegistry()
		_ = reg.Register(rcaStubWithEvidence())
		_ = reg.Register(stubTool("es_log_query"))
		var slept []time.Duration
		_ = Register(reg, Config{Model: m, ExtraOptions: []harness.ReActOption{
			harness.WithReActModelRecoveryDelays(time.Millisecond),
			harness.WithReActModelRecoverySleep(func(_ context.Context, d time.Duration) error {
				slept = append(slept, d)
				return nil
			}),
		}})
		toolEntry, _ := reg.Get(ToolName)
		out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "q"})
		if err == nil {
			t.Fatalf("toolFirst=%v: want error, got %v", toolFirst, out)
		}
		res := out.(map[string]any)
		if res["ok"] != false || res["error_code"] != tool.ErrorTransient || res["model_unavailable"] != true {
			t.Fatalf("toolFirst=%v: res=%v", toolFirst, res)
		}
		if len(slept) != 0 {
			t.Fatalf("toolFirst=%v: sub agent must not run its own cooldowns: %v", toolFirst, slept)
		}
		if _, has := res["conclusion"]; has {
			t.Fatalf("degraded text must not be reported as a conclusion: %v", res)
		}
	}
}

func TestExecute_SubAgentGetsDefaultSuspectHook(t *testing.T) {
	run := func() int {
		m := &fakeToolModel{finalText: "没有数据。\n状态: 证据不足"}
		reg := tool.NewEmptyRegistry()
		grep := stubTool("rca_grep")
		grep.Execute = func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true, "hit_status": tool.HitStatusSuspect, "roots_missing": []string{"src"}}, nil
		}
		_ = reg.Register(grep)
		_ = reg.Register(stubTool("es_log_query"))
		_ = Register(reg, Config{Model: m})
		toolEntry, _ := reg.Get(ToolName)
		if _, err := toolEntry.Execute(context.Background(), map[string]any{"question": "q"}); err != nil {
			t.Fatal(err)
		}
		return m.calls
	}
	if calls := run(); calls != 3 {
		t.Fatalf("suspect evidence must nudge the sub agent exactly once: model calls=%d", calls)
	}
	t.Setenv(harness.EnvStopSuspect, "off")
	if calls := run(); calls != 2 {
		t.Fatalf("%s=off disables the default hook: model calls=%d", harness.EnvStopSuspect, calls)
	}
}

func TestParseConclusion(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantText     string
		wantInsuffic bool
	}{
		{"sufficient", "结论正文\n状态: 证据充分", "结论正文", false},
		{"insufficient", "结论正文\n状态: 证据不足", "结论正文", true},
		{"full-width colon", "结论\n状态：证据不足", "结论", true},
		{"trailing period", "结论\n状态: 证据充分。", "结论", false},
		{"status not last line", "状态: 证据充分\n结论正文", "结论正文", false},
		{"missing status line", "只有结论没有状态行", "只有结论没有状态行", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotText, gotIns := parseConclusion(c.in)
			if gotText != c.wantText || gotIns != c.wantInsuffic {
				t.Fatalf("parseConclusion(%q) = (%q, %v), want (%q, %v)", c.in, gotText, gotIns, c.wantText, c.wantInsuffic)
			}
		})
	}
}
