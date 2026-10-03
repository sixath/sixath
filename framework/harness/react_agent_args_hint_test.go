package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestArgsParseErrorHint(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(tool.Tool{
		Name: "execute_read",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"dsl": map[string]any{"type": "string"}}, "required": []any{"dsl"}},
		Execute: func(context.Context, map[string]any) (any, error) { return nil, nil },
	})
	if got := argsParseErrorHint(reg, "execute_read", ""); !strings.Contains(got, "required: dsl(string)") {
		t.Fatalf("direct: %q", got)
	}
	if got := argsParseErrorHint(reg, tool.ToolCallName, `{"name":"execute_read","arguments":{"dsl":"SEL`); !strings.Contains(got, "required: dsl(string)") {
		t.Fatalf("tool_call wrapper: %q", got)
	}
	if got := argsParseErrorHint(reg, tool.ToolCallName, `{"arguments":{"name":"x"},"name":"execute_read"`); !strings.Contains(got, "required: dsl(string)") {
		t.Fatalf("fallback skips unregistered names: %q", got)
	}
	if got := argsParseErrorHint(reg, tool.ToolCallName, `{"name":"exec_read","arguments":{"name":"execute_read"`); !strings.Contains(got, "execute_read required: dsl(string)") {
		t.Fatalf("unregistered leading name keeps scanning: %q", got)
	}
	if got := argsParseErrorHint(reg, tool.ToolCallName, `{"arguments":{"name":"x"`); strings.Contains(got, "required") {
		t.Fatalf("no registered name: %q", got)
	}
	if got := argsParseErrorHint(reg, tool.ToolCallName, `{"arguments":{"dsl":"x"},"name":"execute_read"`); !strings.Contains(got, "required: dsl(string)") {
		t.Fatalf("fallback: %q", got)
	}
	if got := argsParseErrorHint(reg, "nope", ""); !strings.Contains(got, "valid JSON object") || strings.Contains(got, "required") {
		t.Fatalf("unknown: %q", got)
	}
	if got := argsParseErrorHint(nil, "execute_read", ""); !strings.Contains(got, "valid JSON object") {
		t.Fatalf("nil registry: %q", got)
	}
}
