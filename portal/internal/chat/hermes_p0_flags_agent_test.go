package chat

import (
	"testing"

	"backend/internal/biz"
)

func TestMergeHermesP0Flags_OR(t *testing.T) {
	global := HermesP0ToolFlags{TodoEnabled: true}
	agent := HermesP0ToolFlags{MemoryWriteEnabled: true}
	got := MergeHermesP0Flags(global, agent)
	if !got.TodoEnabled || !got.MemoryWriteEnabled {
		t.Fatalf("OR merge failed: %+v", got)
	}
	if got.WebToolsEnabled {
		t.Fatal("unexpected web tools")
	}
}

func TestRuntimeToolsForAgent_BrowserEnabled(t *testing.T) {
	old := DefaultHermesP0ToolFlags
	DefaultHermesP0ToolFlags = HermesP0ToolFlags{}
	t.Cleanup(func() { DefaultHermesP0ToolFlags = old })

	agent := &biz.AgentMeta{RuntimeTools: biz.RuntimeToolsConfig{BrowserEnabled: true}}
	f := RuntimeToolsForAgent(agent)
	if !f.BrowserEnabled {
		t.Fatalf("expected BrowserEnabled from agent runtime_tools, got %+v", f)
	}
}

func TestRuntimeToolsForAgent_WebToolsFailClosedAgainstProcessDefault(t *testing.T) {
	old := DefaultHermesP0ToolFlags
	DefaultHermesP0ToolFlags = HermesP0ToolFlags{WebToolsEnabled: true}
	t.Cleanup(func() { DefaultHermesP0ToolFlags = old })

	agent := &biz.AgentMeta{RuntimeTools: biz.RuntimeToolsConfig{WebToolsEnabled: false, TodoEnabled: true}}
	f := RuntimeToolsForAgent(agent)
	if f.WebToolsEnabled {
		t.Fatal("agent webToolsEnabled=false must not inherit process WebToolsEnabled")
	}
	if !f.TodoEnabled {
		t.Fatal("other flags should still OR-merge")
	}
}

func TestRuntimeToolsForAgent_WebToolsEnabledWhenAgentOptsIn(t *testing.T) {
	old := DefaultHermesP0ToolFlags
	DefaultHermesP0ToolFlags = HermesP0ToolFlags{}
	t.Cleanup(func() { DefaultHermesP0ToolFlags = old })

	agent := &biz.AgentMeta{RuntimeTools: biz.RuntimeToolsConfig{WebToolsEnabled: true}}
	f := RuntimeToolsForAgent(agent)
	if !f.WebToolsEnabled {
		t.Fatal("expected agent web opt-in")
	}
}
