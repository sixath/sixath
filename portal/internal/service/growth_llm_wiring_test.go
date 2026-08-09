package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/conf"

	"github.com/sixath/framework/growth"
)

// fakeLLMClient 实现 growth.LLMClient 供 portal 侧 wiring 单测使用。
type fakeLLMClient struct {
	resp string
	err  error
	last string
}

func (f *fakeLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	f.last = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.resp, nil
}

// TestGrowthLLMProposers_WiredFromConfig 验证 NewLLMSkillProposer / NewLLMCombinedProposer
// 在 portal 层的等价 wiring 行为：注入 fake client 后，proposer 能解析 JSON 并尊重 system prompt 覆盖。
func TestGrowthLLMProposers_WiredFromConfig(t *testing.T) {
	llm := &fakeLLMClient{resp: `[{"path":"skills/x/SKILL.md","op":"create","content":"---\nname: x\ndescription: d\n---\n# x"}]`}
	cfg := growth.LLMRunnerConfig{SystemPrompt: "CUSTOM_PROMPT"}
	skillProp := growth.NewLLMSkillProposer(llm, cfg)
	if skillProp == nil {
		t.Fatal("nil skill proposer")
	}
	patches, err := skillProp(context.Background(), growth.ReviewJob{SessionID: "s"}, "tr", "summary")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 || patches[0].Op != growth.OpCreate {
		t.Fatalf("patches=%+v", patches)
	}
	if !contains(llm.last, "CUSTOM_PROMPT") {
		t.Fatalf("custom prompt not honored: %q", llm.last)
	}

	llm2 := &fakeLLMClient{resp: `{"patches":[{"path":"skills/y/SKILL.md","op":"create","content":"---\nname: y\ndescription: d\n---\n# y"}],"notify_memory":true}`}
	combinedProp := growth.NewLLMCombinedProposer(llm2, growth.LLMRunnerConfig{})
	if combinedProp == nil {
		t.Fatal("nil combined proposer")
	}
	ps, notify, err := combinedProp(context.Background(), growth.ReviewJob{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !notify || len(ps) != 1 {
		t.Fatalf("patches=%+v notify=%v", ps, notify)
	}
}

func TestGrowthLLMProposer_PropagatesErrors(t *testing.T) {
	llm := &fakeLLMClient{err: errors.New("timeout")}
	skillProp := growth.NewLLMSkillProposer(llm, growth.LLMRunnerConfig{})
	if _, err := skillProp(context.Background(), growth.ReviewJob{}, "", ""); err == nil {
		t.Fatal("expected error to propagate")
	}
}

// TestNewGrowthModelClient_RejectsNil 验证 portal 适配器在缺配置时安全失败。
func TestNewGrowthModelClient_RejectsNil(t *testing.T) {
	if _, err := newGrowthModelClient(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

// TestNewGrowthModelClient_BadProvider 配置非法 provider 时报错。
func TestNewGrowthModelClient_BadProvider(t *testing.T) {
	cfg := &conf.GrowthLLM{Provider: "no-such-provider", Model: "x"}
	if _, err := newGrowthModelClient(cfg); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
