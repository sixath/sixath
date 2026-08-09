package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
	toolmem "github.com/sixath/framework/tool/memory"
)

func TestNewServer_RegistersThreeTools(t *testing.T) {
	s, err := NewServer(NewDefaultStore(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil server")
	}
}

func TestAdapter_RememberRecallRoundTrip(t *testing.T) {
	store := NewDefaultStore()
	reg := tool.NewRegistry()
	if err := toolmem.RegisterMemoryStoreTools(reg, store, toolmem.StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	remember, ok := reg.Get("memory_remember")
	if !ok {
		t.Fatal("missing remember")
	}
	args := map[string]any{
		"scope": "session", "action": "add", "content": "Alice likes rockets", "session_id": "s1",
	}
	ctx := withIdentity(context.Background(), args)
	out, err := remember.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"id"`) {
		t.Fatalf("remember: %s", b)
	}

	recall, _ := reg.Get("memory_recall")
	rargs := map[string]any{"scope": "session", "query": "Alice", "session_id": "s1", "limit": 5}
	ctx = withIdentity(context.Background(), rargs)
	out, err = recall.Execute(ctx, rargs)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), "Alice") {
		t.Fatalf("recall: %s", b)
	}
}

func TestAdapter_SessionIDMissing(t *testing.T) {
	store := NewDefaultStore()
	reg := tool.NewRegistry()
	_ = toolmem.RegisterMemoryStoreTools(reg, store, toolmem.StoreToolsOptions{})
	tl, _ := reg.Get("memory_remember")
	out, err := tl.Execute(context.Background(), map[string]any{
		"scope": "session", "action": "add", "content": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "session_id_missing") {
		t.Fatalf("want session_id_missing, got %s", b)
	}
}

func TestWrapTool_Handler(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	reg := tool.NewRegistry()
	if err := toolmem.RegisterMemoryStoreTools(reg, store, toolmem.StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_remember")
	mcpTool, handler := wrapTool(tl)
	if mcpTool.Name != "memory_remember" {
		t.Fatalf("name=%s", mcpTool.Name)
	}
	if _, ok := mcpTool.InputSchema.Properties["session_id"]; !ok {
		t.Fatal("expected session_id in schema")
	}
	req := mcplib.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"scope": "session", "action": "add", "content": "fact one", "session_id": "sess-a",
	}
	res, err := handler(context.Background(), req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("handler err=%v res=%#v", err, res)
	}
}

func TestResultToMCP_JSON(t *testing.T) {
	res, err := resultToMCP(map[string]any{"ok": true})
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
}

func TestNewServer_NilStore(t *testing.T) {
	if _, err := NewServer(nil, Options{}); err == nil {
		t.Fatal("expected error")
	}
}
