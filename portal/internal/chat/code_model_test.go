package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/model"
)

type stubTurnModel struct{ name string }

func (s stubTurnModel) Generate(_ context.Context, _ string, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.name}, nil
}
func (s stubTurnModel) Chat(_ context.Context, _ []model.Message, _ ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: s.name}, nil
}
func (s stubTurnModel) Embed(_ context.Context, _ []string, _ ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestResolveTurnModel_nonCodeKeepsChat(t *testing.T) {
	chat := stubTurnModel{name: "chat"}
	got := ResolveTurnModel(familySet([]string{FamilyCore}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{CodeModel: "gpt-code", CodeAPIKey: "k", CodeProvider: "openai", CodeBaseURL: "http://127.0.0.1:9"},
	})
	if got != chat {
		t.Fatal("non-code family must keep session model")
	}
}

func TestResolveTurnModel_noSpecKeepsChat(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	chat := stubTurnModel{name: "chat"}
	got := ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{})
	if got != chat {
		t.Fatal("missing code spec must keep session model")
	}
}

func TestResolveCodeModelSpec_agentOverlaysGlobal(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{Provider: "openai", Model: "global-code", APIKey: "gk", BaseURL: "http://g"})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })
	got := resolveCodeModelSpec(biz.AgentMeta{ModelConfig: biz.ModelConfig{CodeModel: "agent-code"}})
	if got.Model != "agent-code" || got.APIKey != "gk" || got.BaseURL != "http://g" {
		t.Fatalf("agent should overlay model and inherit global key: %#v", got)
	}
}

func TestResolveCodeModelSpec_emptyAgentUsesGlobal(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{Model: "global-code", APIKey: "gk"})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })
	got := resolveCodeModelSpec(biz.AgentMeta{})
	if got.Model != "global-code" || got.APIKey != "gk" {
		t.Fatalf("empty agent must use global: %#v", got)
	}
}

func TestResolveCodeModelSpec_envWhenGlobalEmpty(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "env-code")
	t.Setenv("SATH_CODE_PROVIDER", "openai")
	t.Setenv("SATH_CODE_API_KEY", "env-key")
	t.Setenv("SATH_CODE_BASE_URL", "http://env")
	SetGlobalCodeModel(CodeModelSpec{})
	got := resolveCodeModelSpec(biz.AgentMeta{})
	if got.Model != "env-code" || got.APIKey != "env-key" {
		t.Fatalf("env fallback: %#v", got)
	}
}

func TestResolveTurnModel_codeFamilyUsesAgentSpec(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	chat := stubTurnModel{name: "chat"}
	got := ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{
			Provider: "openai", Model: "gpt-chat",
			CodeProvider: "openai", CodeModel: "gpt-code", CodeAPIKey: "sk-test", CodeBaseURL: "http://127.0.0.1:9",
		},
	})
	if got == chat {
		t.Fatal("code family with spec should swap model")
	}
}
