package tool

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestRegisterHyperTool_Disabled(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterHyperTool(reg, &HyperToolOptions{Enabled: false}); err != nil {
		t.Fatalf("RegisterHyperTool: %v", err)
	}
	if _, ok := reg.Get(HyperToolName); ok {
		t.Fatal("hypertool should not register when disabled")
	}
}

func TestHyperTool_Chaining(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}

	reg := NewRegistry()
	_ = reg.Register(Tool{
		Name:        "echo_args",
		Description: "echo",
		Execute: func(_ context.Context, params map[string]any) (any, error) {
			return params, nil
		},
	})
	_ = reg.Register(Tool{
		Name:        "add_one",
		Description: "add one",
		Execute: func(_ context.Context, params map[string]any) (any, error) {
			n, _ := params["n"].(float64)
			return n + 1, nil
		},
	})
	if err := RegisterHyperTool(reg, &HyperToolOptions{Enabled: true, TimeoutSeconds: 10, MaxInternalCalls: 5}); err != nil {
		t.Fatalf("RegisterHyperTool: %v", err)
	}
	ht, ok := reg.Get(HyperToolName)
	if !ok {
		t.Fatal("hypertool not registered")
	}

	code := `
first = call_tool("echo_args", {"n": 1})
second = call_tool("add_one", {"n": first["n"]})
result = {"value": second}
`
	out, err := ht.Execute(context.Background(), map[string]any{"code": code})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("result type %T", out)
	}
	if !strings.Contains(s, `"value": 2`) && !strings.Contains(s, `"value":2`) {
		t.Fatalf("unexpected result: %s", s)
	}
}

func TestHyperTool_BlocksRecursiveTool(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	reg := NewRegistry()
	if err := RegisterHyperTool(reg, &HyperToolOptions{Enabled: true}); err != nil {
		t.Fatalf("RegisterHyperTool: %v", err)
	}
	ht, _ := reg.Get(HyperToolName)
	_, err := ht.Execute(context.Background(), map[string]any{
		"code": `result = call_tool("hypertool", {"code": "result = 1"})`,
	})
	if err == nil {
		t.Fatal("expected blocked recursive hypertool call")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHyperTool_MissingResult(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	reg := NewRegistry()
	if err := RegisterHyperTool(reg, &HyperToolOptions{Enabled: true}); err != nil {
		t.Fatalf("RegisterHyperTool: %v", err)
	}
	ht, _ := reg.Get(HyperToolName)
	_, err := ht.Execute(context.Background(), map[string]any{"code": "x = 1"})
	if err == nil || !strings.Contains(err.Error(), "result") {
		t.Fatalf("expected missing result error, got %v", err)
	}
}
