package chat

import (
	"errors"
	"strings"
	"testing"

	"backend/internal/biz"
)

func TestEvalGolden_assemblerPromptHasNoTaskLock(t *testing.T) {
	p := BuildEffectiveSystemPrompt("You are a helpful assistant.", nil)
	p = AppendAskUserToolPrompt(p)
	if strings.Contains(p, "本轮任务锁") {
		t.Fatal(p)
	}
	if strings.HasPrefix(strings.TrimSpace(p), "## 可用工具目录") {
		t.Fatal("catalog block must not be prepended")
	}
}

func TestEvalGolden_deny_write_files(t *testing.T) {
	if DefaultHermesP0ToolFlags.WorkspaceFilesEnabled {
		t.Fatal("workspace files must default off (E5 opt-in)")
	}
	var zero HermesP0ToolFlags
	if zero.WorkspaceFilesEnabled {
		t.Fatal("HermesP0ToolFlags zero value must deny write_file")
	}
}

func TestEvalGolden_code_model(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })

	chat := stubTurnModel{name: "chat"}

	got, err := ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{})
	if !errors.Is(err, ErrCodeModelRequired) || got != nil {
		t.Fatalf("missing spec must error, got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(familySet([]string{FamilyCore}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{CodeModel: "gpt-code", CodeAPIKey: "k", CodeProvider: "openai", CodeBaseURL: "http://127.0.0.1:9"},
	})
	if err != nil || got != chat {
		t.Fatalf("non-code must keep chat: got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(nil, chat, biz.AgentMeta{})
	if err != nil || got != chat {
		t.Fatalf("nil active must keep chat: got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{
			Provider: "openai", Model: "gpt-chat",
			CodeProvider: "openai", CodeModel: "gpt-code", CodeAPIKey: "sk-test", CodeBaseURL: "http://127.0.0.1:9",
		},
	})
	if err != nil || got == nil || got == chat {
		t.Fatalf("configured code model must swap: got=%v err=%v", got, err)
	}
}
