package agent

import (
	"testing"

	"github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
)

func TestAlias_NewReActAgentIsHarnessType(t *testing.T) {
	reg := tool.NewRegistry()
	a := NewReActAgent(nil, memory.NewBufferMemory(8), reg, WithReActMaxSteps(2), WithReActWorkspace(t.TempDir()))
	var _ *harness.ReActAgent = a
	var _ Agent = a
}

func TestAlias_LoadWorkspaceHarnessHooksMissingOK(t *testing.T) {
	hooks, err := LoadWorkspaceHarnessHooks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if hooks != nil {
		t.Fatalf("hooks=%v want nil", hooks)
	}
}

func TestAlias_HookTypesCompile(t *testing.T) {
	h := NewFailureCaptureHook(FailureCaptureConfig{})
	var _ ToolHook = h
	_ = WithReActToolHooks(h)
}
