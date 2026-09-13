package chat

import (
	"context"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type modeFakeModel struct{}

func (modeFakeModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "ok"}, nil
}
func (modeFakeModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "ok"}, nil
}
func (modeFakeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestIsPlanMode(t *testing.T) {
	cases := map[string]bool{
		"react": false, "": false, "chat": false, "rea ct": false,
		"plan": true, "PLAN": true, "plan_execute": true, "plan-execute": true, "  plan  ": true,
	}
	for in, want := range cases {
		if got := IsPlanMode(in); got != want {
			t.Fatalf("IsPlanMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBuildAgent_ModeSelection(t *testing.T) {
	m := modeFakeModel{}
	reg := tool.NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry() returned nil")
	}

	if a := BuildAgent(m, reg, "", 10, "react"); true {
		if _, ok := a.(*agent.ReActAgent); !ok {
			t.Fatalf("react mode: got %T, want *ReActAgent", a)
		}
	}
	if a := BuildAgent(m, reg, "", 10, ""); true {
		if _, ok := a.(*agent.ReActAgent); !ok {
			t.Fatalf("empty mode: got %T, want *ReActAgent", a)
		}
	}
	if a := BuildAgent(m, reg, "", 10, "plan"); true {
		if _, ok := a.(*agent.PlanExecuteAgent); !ok {
			t.Fatalf("plan mode: got %T, want *PlanExecuteAgent", a)
		}
	}
}
