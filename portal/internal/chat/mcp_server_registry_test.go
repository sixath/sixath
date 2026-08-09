package chat

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

func TestMcpServerToConfig_MapsFields(t *testing.T) {
	meta := &biz.McpServerMeta{
		ID:         "confluence",
		Transport:  "stdio",
		Command:    "npx",
		Args:       []string{"-y", "@atlassian-dc-mcp/confluence"},
		Env:        map[string]string{"CONFLUENCE_HOST": "h", "TOKEN": "secret"},
		Backend:    "mark3labs",
		TimeoutSec: 30,
	}
	cfg := biz.McpServerToConfig(meta)
	if cfg == nil {
		t.Fatal("expected config")
	}
	if cfg.Id != "confluence" || cfg.Transport != "stdio" || cfg.Command != "npx" || cfg.TimeoutSec != 30 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.Args) != 2 || cfg.Env["TOKEN"] != "secret" {
		t.Fatalf("args/env not mapped: %+v", cfg)
	}
	// mutation isolation
	cfg.Env["TOKEN"] = "changed"
	if meta.Env["TOKEN"] != "secret" {
		t.Fatal("env should be copied")
	}
}

func TestBuildRegistry_StdioFixtureRegistersEcho(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// portal/internal/chat -> ../../.. = repo root? chat is portal/internal/chat
	// fixture lives in framework/tool/testdata relative to sixath root sibling of portal.
	fixture := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "framework", "tool", "testdata", "mcp_stdio_fixture.js"))
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("fixture missing at %s: %v", fixture, err)
	}

	reg := tool.NewRegistry()
	servers := []*biz.McpServerMeta{{
		ID:        "fixture",
		Name:      "fixture",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{fixture},
		Backend:   "mark3labs",
	}}
	result, err := BuildRegistry(nil, servers, reg)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if len(result.McpServers) != 1 || result.McpServers[0].Id != "fixture" {
		t.Fatalf("McpServers=%+v", result.McpServers)
	}
	if result.McpServers[0].Command != "node" || len(result.McpServers[0].Args) != 1 {
		t.Fatalf("skillops entry incomplete: %+v", result.McpServers[0])
	}
	if _, ok := reg.Get("echo"); !ok {
		t.Fatal("expected echo tool from stdio fixture")
	}
}

func TestBuildRegistry_BogusStdioCommandErrors(t *testing.T) {
	reg := tool.NewRegistry()
	servers := []*biz.McpServerMeta{{
		ID:        "bad-bash",
		Name:      "bad",
		Transport: "stdio",
		Command:   "bash",
		Args:      []string{"-c", "id"},
	}}
	_, err := BuildRegistry(nil, servers, reg)
	if err == nil {
		t.Fatal("expected BuildRegistry error for denied stdio command")
	}
	if !strings.Contains(err.Error(), `mcp server "bad-bash" failed to register`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.HasMcpServer("bad-bash") {
		t.Fatal("failed server must not be marked registered")
	}
}
