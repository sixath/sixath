package tool_test

import (
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestMcpConfigFromMap_Stdio(t *testing.T) {
	c := tool.McpConfigFromMap(map[string]any{
		"mcp": map[string]any{
			"id":        "confluence",
			"transport": "stdio",
			"command":   "npx",
			"args":      []any{"-y", "@atlassian-dc-mcp/confluence"},
			"env":       map[string]any{"CONFLUENCE_HOST": "h"},
			"backend":   "mark3labs",
		},
	})
	if c == nil || c.Transport != "stdio" || c.Command != "npx" || len(c.Args) != 2 {
		t.Fatalf("%+v", c)
	}
	if c.Env["CONFLUENCE_HOST"] != "h" || c.Id != "confluence" {
		t.Fatalf("%+v", c)
	}
}

func TestMcpConfigFromMap_HTTPStillWorks(t *testing.T) {
	c := tool.McpConfigFromMap(map[string]any{
		"mcp": map[string]any{
			"endpoint": "http://127.0.0.1:8080/mcp",
			"id":       "remote",
			"backend":  "metoro",
		},
	})
	if c == nil || c.Endpoint != "http://127.0.0.1:8080/mcp" || c.Id != "remote" || c.Backend != "metoro" {
		t.Fatalf("%+v", c)
	}
	if c.Transport != "" || c.Command != "" {
		t.Fatalf("unexpected stdio fields: %+v", c)
	}
}

func TestNewMcpTool_StdioRejectsNonMark3labsBackend(t *testing.T) {
	_, err := tool.NewMcpTool(&tool.McpConfig{
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@scope/pkg"},
		Backend:   "metoro",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mark3labs") {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestNewMcpTool_StdioEmptyBackendUsesPoolAdapter(t *testing.T) {
	mt, err := tool.NewMcpTool(&tool.McpConfig{
		Id:        "stdio-demo",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@scope/pkg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mt == nil {
		t.Fatal("expected mcpTool")
	}
}
