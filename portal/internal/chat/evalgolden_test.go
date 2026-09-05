package chat

import (
	"strings"
	"testing"
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
