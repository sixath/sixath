package tool_test

import (
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
