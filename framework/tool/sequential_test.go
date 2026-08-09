package tool

import (
	"context"
	"testing"
)

func TestRegister_AppliesBuiltinRequiresSequential(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name:        "write_file",
		Description: "test",
		Parameters:  map[string]any{"type": "object"},
		Execute:     func(context.Context, map[string]any) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("write_file")
	if !ok || !tl.RequiresSequential {
		t.Fatalf("write_file RequiresSequential=%v want true", tl.RequiresSequential)
	}
}

func TestRegister_ReadToolNotSequential(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name:        "read_file",
		Description: "test",
		Parameters:  map[string]any{"type": "object"},
		Execute:     func(context.Context, map[string]any) (any, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("read_file")
	if tl.RequiresSequential {
		t.Fatal("read_file should not require sequential by default")
	}
}
