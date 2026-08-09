package growth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeLLM struct {
	resp string
	err  error
	last string
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string) (string, error) {
	f.last = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.resp, nil
}

func TestLLMSkillProposer_ParsesArray(t *testing.T) {
	llm := &fakeLLM{resp: `[{"path":"skills/foo/SKILL.md","op":"create","content":"---\nname: foo\ndescription: bar\n---\n# foo"}]`}
	p := NewLLMSkillProposer(llm, LLMRunnerConfig{})
	if p == nil {
		t.Fatal("nil proposer")
	}
	got, err := p(context.Background(), ReviewJob{SessionID: "s", WorkspaceKey: "ws"}, "transcript", "summary")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Op != OpCreate || got[0].Path != "skills/foo/SKILL.md" {
		t.Fatalf("patches=%+v", got)
	}
	if !strings.Contains(llm.last, "transcript") || !strings.Contains(llm.last, "summary") {
		t.Fatalf("prompt missing inputs: %q", llm.last)
	}
}

func TestLLMSkillProposer_StripsCodeFence(t *testing.T) {
	llm := &fakeLLM{resp: "```json\n[]\n```"}
	p := NewLLMSkillProposer(llm, LLMRunnerConfig{})
	got, err := p(context.Background(), ReviewJob{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty patches, got %+v", got)
	}
}

func TestLLMSkillProposer_ExtractsArrayFromChatter(t *testing.T) {
	// LLM 闲聊 + 数组：应抓取首个平衡 [..]
	llm := &fakeLLM{resp: `Sure, here is the plan:
[{"path":"skills/bar/SKILL.md","op":"patch","old":"old text","new":"new text"}]
Hope this helps.`}
	p := NewLLMSkillProposer(llm, LLMRunnerConfig{})
	got, err := p(context.Background(), ReviewJob{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Op != OpPatch {
		t.Fatalf("patches=%+v", got)
	}
}

func TestLLMSkillProposer_LLMError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("timeout")}
	p := NewLLMSkillProposer(llm, LLMRunnerConfig{})
	if _, err := p(context.Background(), ReviewJob{}, "", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestLLMSkillProposer_RejectsInvalidOp(t *testing.T) {
	// 非法 op 名应在 JSON 解析阶段被拒绝（path 越权由 ApplyPatchBatch 的 ValidatePatchBatch 处理）。
	llm := &fakeLLM{resp: `[{"path":"skills/x/SKILL.md","op":"frobnicate","content":"x"}]`}
	p := NewLLMSkillProposer(llm, LLMRunnerConfig{})
	if _, err := p(context.Background(), ReviewJob{}, "", ""); err == nil {
		t.Fatal("expected parse error for invalid op")
	}
}

func TestLLMSkillProposer_NilClient(t *testing.T) {
	if NewLLMSkillProposer(nil, LLMRunnerConfig{}) != nil {
		t.Fatal("nil client should yield nil proposer")
	}
}

func TestLLMCombinedProposer_ParsesObject(t *testing.T) {
	llm := &fakeLLM{resp: `{"patches":[{"path":"skills/x/SKILL.md","op":"create","content":"---\nname: x\ndescription: d\n---\n# x"}],"notify_memory":true}`}
	p := NewLLMCombinedProposer(llm, LLMRunnerConfig{})
	patches, notify, err := p(context.Background(), ReviewJob{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !notify || len(patches) != 1 || patches[0].Path != "skills/x/SKILL.md" {
		t.Fatalf("patches=%+v notify=%v", patches, notify)
	}
}

func TestLLMCombinedProposer_ExtractsFromChatter(t *testing.T) {
	llm := &fakeLLM{resp: "thinking...\n```json\n{\"patches\":[],\"notify_memory\":false}\n```\nbye"}
	p := NewLLMCombinedProposer(llm, LLMRunnerConfig{})
	patches, notify, err := p(context.Background(), ReviewJob{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if notify || len(patches) != 0 {
		t.Fatalf("patches=%+v notify=%v", patches, notify)
	}
}

func TestLLMCombinedProposer_LLMError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("502")}
	p := NewLLMCombinedProposer(llm, LLMRunnerConfig{})
	if _, _, err := p(context.Background(), ReviewJob{}, "", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 0); got != "hello" {
		t.Fatalf("max=0 should bypass: %q", got)
	}
	if got := truncateRunes("hello", 3); !strings.HasPrefix(got, "hel") || !strings.Contains(got, "truncated") {
		t.Fatalf("truncated=%q", got)
	}
	// Multibyte:
	got := truncateRunes("你好世界", 2)
	if !strings.HasPrefix(got, "你好") {
		t.Fatalf("rune trunc=%q", got)
	}
}

func TestExtractBracketed_Strings(t *testing.T) {
	// 字符串里的 ] 不应混淆配对
	in := `prefix [{"a":"]"},{"b":"["}] suffix`
	got := extractFirstJSONArray(in)
	if got != `[{"a":"]"},{"b":"["}]` {
		t.Fatalf("got=%q", got)
	}
}

func TestExtractBracketed_Nested(t *testing.T) {
	in := `noise {"k":{"nested":1}} tail`
	got := extractFirstJSONObject(in)
	if got != `{"k":{"nested":1}}` {
		t.Fatalf("got=%q", got)
	}
}
