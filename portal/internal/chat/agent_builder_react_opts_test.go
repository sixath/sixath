package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReActOptionsFromAgent_maxOutputTokens(t *testing.T) {
	opts := ReActOptionsFromAgent(biz.AgentMeta{
		ModelConfig: biz.ModelConfig{MaxOutputTokens: 4096},
	})
	if len(opts) != 1 {
		t.Fatalf("opts len=%d", len(opts))
	}
}

func TestReActOptionsFromAgent_zeroOmits(t *testing.T) {
	if opts := ReActOptionsFromAgent(biz.AgentMeta{}); len(opts) != 0 {
		t.Fatalf("expected no opts")
	}
}

func TestShouldEnableEvidenceGate(t *testing.T) {
	if ShouldEnableEvidenceGate(nil) {
		t.Fatal("nil registry must be false")
	}
	empty := tool.NewRegistry()
	if ShouldEnableEvidenceGate(empty) {
		t.Fatal("empty registry must be false")
	}

	jaeger := tool.NewRegistry()
	if err := jaeger.Register(tool.Tool{
		Name:        "jaeger_trace",
		Description: "jaeger",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("register jaeger: %v", err)
	}
	if !ShouldEnableEvidenceGate(jaeger) {
		t.Fatal("jaeger_trace registry must enable gate")
	}

	es := tool.NewRegistry()
	if err := es.Register(tool.Tool{
		Name:        "es_log_query",
		Description: "es",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("register es: %v", err)
	}
	if !ShouldEnableEvidenceGate(es) {
		t.Fatal("es_log_query registry must enable gate")
	}
}

func TestBuildReActAgent_enablesEvidenceGateForJaeger(t *testing.T) {
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
	if !react.EvidenceGateEnabled() {
		t.Fatal("BuildReActAgent with jaeger_trace must enable EvidenceGate")
	}

	resp, err := react.Run(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "why down?"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr, _ := resp.Metadata["trace"].(*agent.RunTrace)
	if tr == nil || tr.EvidenceNudges != 1 {
		t.Fatalf("expected Soft inject (EvidenceNudges=1), got %#v", tr)
	}
	if resp.Metadata["evidence_incomplete"] != true {
		t.Fatalf("expected evidence_incomplete after Soft retry, got %#v", resp.Metadata)
	}
}

func TestBuildReActAgent_noEvidenceGateWithoutRCATools(t *testing.T) {
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	fake := &builderGateFake{finalReply: "ok"}
	a := BuildReActAgent(fake, reg, "", 10, agent.WithReActMaxSteps(2))
	react := a.(*agent.ReActAgent)
	if react.EvidenceGateEnabled() {
		t.Fatal("non-RCA registry must leave EvidenceGate disabled")
	}
}

func TestShouldApplyEvidenceGate(t *testing.T) {
	mongo := "查询mongodb下uu=193218288的记录"
	if ShouldApplyEvidenceGate(nil, mongo) {
		t.Fatal("surface-off Mongo lookup must not apply EvidenceGate")
	}
	if !ShouldApplyEvidenceGate(nil, "用 elasticsearch 查一下错误日志") {
		t.Fatal("surface-off ES/log query should apply EvidenceGate")
	}
	if ShouldApplyEvidenceGate(familySet([]string{FamilyCore}), "why down?") {
		t.Fatal("core-only surface must not apply EvidenceGate")
	}
	if !ShouldApplyEvidenceGate(familySet([]string{FamilyCore, FamilyRCA}), mongo) {
		t.Fatal("RCA-active surface should apply EvidenceGate even if text is a lookup")
	}
}

func TestEvidenceGateTurnOption_DisablesOnMongoLookup(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "es_log_query",
		Description: "es",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	fake := &builderGateFake{finalReply: "found 2 rows"}
	a := BuildReActAgent(fake, reg, "", 10,
		agent.WithReActMaxSteps(2),
		EvidenceGateTurnOption(reg, nil, "查询mongodb下uu=193218288的记录"),
	)
	react := a.(*agent.ReActAgent)
	if react.EvidenceGateEnabled() {
		t.Fatal("Mongo lookup turn must disable EvidenceGate even when es_log_query is bound")
	}

	resp, err := react.Run(context.Background(), &agent.Request{
		Messages: []model.Message{{Role: "user", Content: "查询mongodb下uu=193218288的记录"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr, _ := resp.Metadata["trace"].(*agent.RunTrace)
	if tr != nil && tr.EvidenceNudges != 0 {
		t.Fatalf("Mongo lookup must not Soft-inject ES, nudges=%d", tr.EvidenceNudges)
	}
}

func TestEvidenceGateTurnOption_KeepsGateOnLogQuery(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "es_log_query",
		Description: "es",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	fake := &builderGateFake{finalReply: "root cause is timeout"}
	a := BuildReActAgent(fake, reg, "", 10,
		agent.WithReActMaxSteps(3),
		EvidenceGateTurnOption(reg, nil, "用 elasticsearch 查错误日志"),
	)
	react := a.(*agent.ReActAgent)
	if !react.EvidenceGateEnabled() {
		t.Fatal("log query turn must keep EvidenceGate")
	}
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
