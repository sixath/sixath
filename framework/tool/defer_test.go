package tool

import "testing"

func TestShouldDefer_McpAlways(t *testing.T) {
	cfg := DefaultDeferConfig()
	tool := Tool{Name: "mcp_tool", Toolset: ToolsetMCP}
	if !ShouldDefer(tool, cfg) {
		t.Fatal("MCP tools should always be deferred")
	}
}

func TestShouldDefer_FileNever(t *testing.T) {
	cfg := DefaultDeferConfig()
	tool := Tool{Name: "execute_read", Toolset: ToolsetFile}
	if ShouldDefer(tool, cfg) {
		t.Fatal("file toolset should not be deferred by default")
	}
}

func TestShouldDefer_AlwaysLoadOverrides(t *testing.T) {
	cfg := DefaultDeferConfig()
	tool := Tool{Name: "web_search", Toolset: ToolsetWeb, AlwaysLoad: true}
	if ShouldDefer(tool, cfg) {
		t.Fatal("AlwaysLoad should force inline regardless of toolset")
	}
}

func TestShouldDefer_SkillsNever(t *testing.T) {
	cfg := DefaultDeferConfig()
	tool := Tool{Name: "load_skill", Toolset: ToolsetSkills}
	if ShouldDefer(tool, cfg) {
		t.Fatal("skills toolset should not be deferred by default")
	}
}
