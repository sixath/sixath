package toolvault

import (
	"context"
	"testing"

	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/vault"
)

func TestRegisterVaultRecallTool(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterVaultRecallTool(reg, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("vault_recall"); !ok {
		t.Fatal("vault_recall not registered")
	}
}

func TestVaultRecallExecute(t *testing.T) {
	reg := tool.NewRegistry()
	recall := func(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
		return []vault.Hit{{Note: vault.Note{Path: "ops/a.md", Type: "experience", Source: "session:x", Body: "body"}, Score: 0.9}}, nil
	}
	if err := RegisterVaultRecallTool(reg, recall); err != nil {
		t.Fatal(err)
	}
	got, _ := reg.Get("vault_recall")
	res, err := got.Execute(context.Background(), map[string]any{"query": "es 流水"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["ok"] != true {
		t.Fatalf("unexpected result: %#v", res)
	}
}
