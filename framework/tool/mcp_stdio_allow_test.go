package tool_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestValidateStdioMcp_AllowsNpx(t *testing.T) {
	err := tool.ValidateStdioMcp("npx", []string{"-y", "@atlassian-dc-mcp/confluence"}, map[string]string{
		"CONFLUENCE_HOST":      "confluence.example.com",
		"CONFLUENCE_API_TOKEN": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateStdioMcp_DeniesBash(t *testing.T) {
	if err := tool.ValidateStdioMcp("bash", []string{"-c", "id"}, nil); err == nil {
		t.Fatal("expected deny")
	}
}

func TestValidateStdioMcp_DeniesNodeEval(t *testing.T) {
	if err := tool.ValidateStdioMcp("node", []string{"-e", "console.log(1)"}, nil); err == nil {
		t.Fatal("expected deny")
	}
}

func TestValidateStdioMcp_DeniesNodeEvalEquals(t *testing.T) {
	if err := tool.ValidateStdioMcp("node", []string{"--eval=console.log(1)"}, nil); err == nil {
		t.Fatal("expected deny")
	}
}

func TestValidateStdioMcp_DeniesPathEnv(t *testing.T) {
	if err := tool.ValidateStdioMcp("npx", []string{"-y", "x"}, map[string]string{"PATH": "/evil"}); err == nil {
		t.Fatal("expected deny")
	}
}

func TestResolveStdioMcpCommand_FindsNpxOnPATH(t *testing.T) {
	path, err := tool.ResolveStdioMcpCommand("npx")
	if err != nil {
		t.Skipf("npx not on PATH in this environment: %v", err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
}

func TestResolveStdioMcpCommand_Empty(t *testing.T) {
	if _, err := tool.ResolveStdioMcpCommand("  "); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveStdioMcpCommand_OverrideEnv(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "npx-fake")
	if runtime.GOOS == "windows" {
		fake += ".cmd"
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_MCP_STDIO_NPX", fake)
	got, err := tool.ResolveStdioMcpCommand("npx")
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Fatalf("got %q want %q", got, fake)
	}
}

func TestNormalizeAllowsWindowsCmdPath(t *testing.T) {
	err := tool.ValidateStdioMcp(`C:\Program Files\nodejs\npx.cmd`, []string{"-y", "pkg"}, nil)
	if err != nil {
		t.Fatal(err)
	}
}
