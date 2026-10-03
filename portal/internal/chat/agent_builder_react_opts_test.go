package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReActOptionsFromAgent_maxOutputTokens(t *testing.T) {
	opts := ReActOptionsFromAgent(biz.AgentMeta{
		ModelConfig: biz.ModelConfig{MaxOutputTokens: 4096},
	})
	// token 计数器 + max_output_tokens
	if len(opts) != 2 {
		t.Fatalf("opts len=%d want 2", len(opts))
	}
}

// 契约变化（Task 4）：无论是否配置 max_output_tokens，都会注入 token 计数器，
// 使真实 usage 能回填校准上下文压缩的估算口径。
func TestReActOptionsFromAgent_zeroOmitsMaxTokens(t *testing.T) {
	opts := ReActOptionsFromAgent(biz.AgentMeta{})
	if len(opts) != 1 {
		t.Fatalf("opts len=%d want 1 (token counter only)", len(opts))
	}
	cfg := agent.ReActConfig{}
	opts[0](&cfg)
	if cfg.TokenCounter == nil {
		t.Fatal("expected token counter to be injected")
	}
	if cfg.MaxOutputTokens != 0 {
		t.Fatalf("MaxOutputTokens=%d want 0 (BuildReActAgent applies its own default)", cfg.MaxOutputTokens)
	}
}

func TestHarnessReActOptions_LoadsWorkspaceHooks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
version: 1
rules:
  - id: block-demo
    tools: [demo]
    action: block
    reason: "no demo"
`)
	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := HarnessReActOptions(root, nil)
	var cfg agent.ReActConfig
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.Workspace != root {
		t.Fatalf("workspace=%q", cfg.Workspace)
	}
	if len(cfg.ToolHooks) != 1 {
		t.Fatalf("ToolHooks=%d want 1", len(cfg.ToolHooks))
	}
	if len(cfg.StopHooks) != 1 || !agent.HasStopHookPrefix(cfg.StopHooks, agent.SuspectEvidenceRuleID) {
		t.Fatalf("StopHooks=%d want only the default suspect hook without stop_rules", len(cfg.StopHooks))
	}
}

func TestHarnessReActOptions_NoWorkspaceStillRegistersDefaultStopHooks(t *testing.T) {
	var cfg agent.ReActConfig
	for _, o := range HarnessReActOptions("", nil) {
		o(&cfg)
	}
	if len(cfg.StopHooks) != 1 || !agent.HasStopHookPrefix(cfg.StopHooks, agent.SuspectEvidenceRuleID) {
		t.Fatalf("StopHooks=%d want the default suspect hook", len(cfg.StopHooks))
	}

	t.Setenv(agent.EnvStopSuspect, "off")
	cfg = agent.ReActConfig{}
	for _, o := range HarnessReActOptions("", nil) {
		o(&cfg)
	}
	if len(cfg.StopHooks) != 0 {
		t.Fatalf("StopHooks=%d want 0 with %s=off", len(cfg.StopHooks), agent.EnvStopSuspect)
	}
}

func TestHarnessReActOptions_LoadsStopRules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
version: 1
max_stop_nudges: 3
stop_rules:
  - id: intent
    match: "让我(进一步)?查"
    message: "请直接调用工具"
`)
	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg agent.ReActConfig
	for _, o := range HarnessReActOptions(root, nil) {
		o(&cfg)
	}
	if len(cfg.StopHooks) != 2 || cfg.MaxStopNudges != 3 {
		t.Fatalf("StopHooks=%d MaxStopNudges=%d", len(cfg.StopHooks), cfg.MaxStopNudges)
	}
}

func TestInvestigationLedgerWiring(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}

	reg := tool.NewRegistry()
	if err := RegisterInvestigationTools(reg, &builderGateFake{finalReply: "ok"}, root); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get(tool.InvestigationToolName); ok {
		t.Fatal("ledger must stay off unless the workspace enables it")
	}
	var cfg agent.ReActConfig
	for _, o := range HarnessReActOptions(root, nil) {
		o(&cfg)
	}
	if cfg.ToolSuccessHook != nil {
		t.Fatal("observer must stay off unless the workspace enables the ledger")
	}

	body := []byte("investigation_ledger: true\nstop_rules:\n  - id: gaps\n    when_investigation_gaps: [open_hypotheses]\n    message: \"{{investigation_gaps}}\"\n")
	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	reg = tool.NewRegistry()
	if err := RegisterInvestigationTools(reg, &builderGateFake{finalReply: "ok"}, root); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get(tool.InvestigationToolName); !ok {
		t.Fatal("ledger must be registered when investigation_ledger is true")
	}
	cfg = agent.ReActConfig{}
	for _, o := range HarnessReActOptions(root, nil) {
		o(&cfg)
	}
	if cfg.ToolSuccessHook == nil || len(cfg.StopHooks) != 2 {
		t.Fatalf("observer=%v stopHooks=%d", cfg.ToolSuccessHook != nil, len(cfg.StopHooks))
	}
	if ic := InvestigateConfig(&builderGateFake{finalReply: "ok"}, root); len(ic.ExtraOptions) != 1 || len(ic.StopHooks) != 1 {
		t.Fatalf("deep_investigate must inherit observer and stop rules: extra=%d stop=%d", len(ic.ExtraOptions), len(ic.StopHooks))
	}
}

func TestCriticWiring(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("stop_rules:\n  - id: r\n    match: 'x'\n    message: m\ncritic:\n  enabled: true\n  max_rounds: 1\n")
	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &builderGateFake{finalReply: "ok"}
	var cfg agent.ReActConfig
	for _, o := range HarnessReActOptionsFor(fake, root, nil) {
		o(&cfg)
	}
	if len(cfg.StopHooks) != 3 {
		t.Fatalf("stop rules + suspect + critic expected, got %d", len(cfg.StopHooks))
	}
	if b, ok := cfg.StopHooks[2].(agent.BudgetedStopHook); !ok {
		t.Fatalf("critic must come last and carry its own budget: %T", cfg.StopHooks[2])
	} else if p, _ := b.StopBudget(); p != agent.CriticRulePrefix {
		t.Fatalf("critic must come last: prefix=%q", p)
	}
	if ic := InvestigateConfig(fake, root); len(ic.StopHooks) != 2 {
		t.Fatalf("deep_investigate must inherit the critic: %d", len(ic.StopHooks))
	}

	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), []byte("critic:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg = agent.ReActConfig{}
	for _, o := range HarnessReActOptionsFor(fake, root, nil) {
		o(&cfg)
	}
	if len(cfg.StopHooks) != 2 {
		t.Fatalf("critic (with the default suspect hook) must still be installed: %d", len(cfg.StopHooks))
	}
}

func TestHarnessReActOptions_BadStopRulesSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("stop_rules:\n  - id: bad\n    match: '('\n    message: m\n")
	if err := os.WriteFile(filepath.Join(root, "harness", "hooks.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg agent.ReActConfig
	for _, o := range HarnessReActOptions(root, nil) {
		o(&cfg)
	}
	if len(cfg.StopHooks) != 1 || cfg.Workspace != root {
		t.Fatalf("bad stop_rules must be skipped without breaking assembly: %+v", cfg.StopHooks)
	}
}

func TestBuildReActAgent_jaegerDoesNotSoftInject(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "jaeger_trace",
		Description: "jaeger",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	fake := &builderGateFake{finalReply: "premature RCA answer"}
	a := BuildReActAgent(fake, reg, "", 10, agent.WithReActMaxSteps(3))
	react, ok := a.(*agent.ReActAgent)
	if !ok {
		t.Fatalf("expected *ReActAgent, got %T", a)
	}
	if react.ParallelToolsEnabled() {
		t.Fatal("jaeger-only registry must not enable ParallelTools")
	}

	resp, err := react.Run(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "why down?"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Text != "premature RCA answer" {
		t.Fatalf("expected final answer without Soft inject, got %#v", resp)
	}
	tr, _ := resp.Metadata["trace"].(*agent.RunTrace)
	if tr != nil && tr.EvidenceNudges != 0 {
		t.Fatalf("EvidenceNudges=%d want 0", tr.EvidenceNudges)
	}
	if resp.Metadata["evidence_incomplete"] == true {
		t.Fatalf("must not set evidence_incomplete: %#v", resp.Metadata)
	}
}

func TestBuildReActAgent_calculatorRunsWithoutRCATools(t *testing.T) {
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	fake := &builderGateFake{finalReply: "ok"}
	a := BuildReActAgent(fake, reg, "", 10, agent.WithReActMaxSteps(2))
	react := a.(*agent.ReActAgent)
	resp, err := react.Run(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Text != "ok" {
		t.Fatalf("got %#v", resp)
	}
}

func TestBuildReActAgent_rcaReadEnablesParallelTools(t *testing.T) {
	reg := registerTestRCARead(t)
	fake := &builderGateFake{finalReply: "ok"}
	a := BuildReActAgent(fake, reg, "", 10, agent.WithReActMaxSteps(2))
	react := a.(*agent.ReActAgent)
	if !react.ParallelToolsEnabled() {
		t.Fatal("rca_read registry must enable ParallelTools")
	}
}

func registerTestRCARead(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "rca_read",
		Description: "read",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	}); err != nil {
		t.Fatalf("register rca_read: %v", err)
	}
	return reg
}

type builderGateFake struct {
	finalReply string
	toolCalls  int
}

func (f *builderGateFake) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalReply}, nil
}

func (f *builderGateFake) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalReply, Raw: model.ToolStep{Used: false}}, nil
}

func (f *builderGateFake) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return make([]model.Embedding, len(texts)), nil
}

func (f *builderGateFake) ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	f.toolCalls++
	return &model.Generation{Text: f.finalReply, Raw: model.ToolStep{Used: false}}, nil
}
