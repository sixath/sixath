package tool

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNormalizeMCPSchema(t *testing.T) {
	s := mcp.ToolInputSchema{Type: "object", Properties: map[string]any{"q": map[string]any{"type": "string"}}, Required: []string{"q"}}
	got, ok := normalizeMCPSchema(s, nil).(map[string]any)
	if !ok || got["type"] != "object" {
		t.Fatalf("got %#v", got)
	}
	if err := ValidateArguments("m", got, map[string]any{}); err == nil {
		t.Fatal("required must be enforced after normalization")
	}
	if err := ValidateArguments("m", got, map[string]any{"q": "x"}); err != nil {
		t.Fatalf("valid args rejected: %v", err)
	}

	raw := json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]}`)
	m, _ := normalizeMCPSchema(mcp.ToolInputSchema{}, raw).(map[string]any)
	if m["required"] == nil || m["properties"].(map[string]any)["a"] == nil {
		t.Fatalf("raw schema must take precedence: %#v", m)
	}

	already := map[string]any{"type": "object"}
	if normalizeMCPSchema(already, nil).(map[string]any)["type"] != "object" {
		t.Fatal("map passthrough")
	}
	if normalizeMCPSchema(nil, nil) != nil {
		t.Fatal("nil schema must stay nil")
	}
	if normalizeMCPSchema(make(chan int), nil) == nil {
		t.Fatal("unmarshalable schema must be returned unchanged")
	}
}
