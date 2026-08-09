package memory

import (
	"strings"
	"testing"
)

func TestResolveTaskFamily(t *testing.T) {
	if got := ResolveTaskFamily("a1", "ops"); got != "ops" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveTaskFamily("a1", "  "); got != "a1" {
		t.Fatalf("fallback got %q", got)
	}
}

func TestValidateProceduralBinding_UnknownTool(t *testing.T) {
	reg := map[string]struct{}{"ssh_exec": {}}
	_, err := ValidateProceduralBinding(ProceduralBinding{
		TriggerCode: FailureCodeToolRepeatFail,
		ActionKind:  BindingActionToolSequence,
		ToolNames:   []string{"no_such_tool"},
	}, reg)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("want unknown tool err, got %v", err)
	}
}

func TestValidateAndMatch_SkillSuggest(t *testing.T) {
	b, err := ValidateProceduralBinding(ProceduralBinding{
		TriggerQuery: "转人工",
		ActionKind:   BindingActionSkill,
		SkillID:      "escalation",
		Mode:         BindingModeSuggest,
		AgentID:      "zone-4100-agent",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := MatchProceduralBindings([]ProceduralBinding{b}, "zone-4100-agent", "请帮我转人工客服", nil)
	if len(got) != 1 {
		t.Fatalf("match: %#v", got)
	}
	text := FormatBindingSuggest(got[0])
	if !strings.Contains(text, "escalation") || !strings.Contains(text, "建议") {
		t.Fatalf("format: %s", text)
	}
	// wrong agent
	if n := MatchProceduralBindings([]ProceduralBinding{b}, "other", "转人工", nil); len(n) != 0 {
		t.Fatalf("want no match for other agent")
	}
}

func TestMatchProceduralBindings_ByFailureCode(t *testing.T) {
	b, err := ValidateProceduralBinding(ProceduralBinding{
		TriggerCode: FailureCodeToolRepeatFail,
		ActionKind:  BindingActionToolSequence,
		ToolNames:   []string{"ssh_exec"},
	}, map[string]struct{}{"ssh_exec": {}})
	if err != nil {
		t.Fatal(err)
	}
	got := MatchProceduralBindings([]ProceduralBinding{b}, "", "", []string{FailureCodeToolRepeatFail})
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestFilterValidBindings_DropsBad(t *testing.T) {
	items := []ProceduralBinding{
		{TriggerCode: "x", ActionKind: BindingActionSkill, SkillID: "ok"},
		{TriggerCode: "y", ActionKind: BindingActionToolSequence, ToolNames: []string{"missing"}},
	}
	got := FilterValidBindings(items, map[string]struct{}{"ssh_exec": {}}, nil)
	if len(got) != 1 || got[0].SkillID != "ok" {
		t.Fatalf("got %#v", got)
	}
}
