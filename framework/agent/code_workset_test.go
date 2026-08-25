package agent

import (
	"strings"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestCollectCodeWorkset_readAndCallers(t *testing.T) {
	cf := c304ControlFlow()
	ws := CollectCodeWorkset([]ToolCallRecord{
		{ToolName: "rca_read", Result: map[string]any{
			"file": "helper.go", "content": "1|ok", "control_flow": cf,
		}},
		{ToolName: "rca_symbol", Result: map[string]any{
			"ok": true, "action": "references",
			"callers": []map[string]any{{"file": "cmd/main.go", "line": 3, "path": "cmd/main.go:3"}},
		}},
	})
	if !containsStr(ws.Files, "helper.go") {
		t.Fatalf("want helper.go in files, got %v", ws.Files)
	}
	if len(ws.Functions) == 0 || !strings.Contains(ws.Functions[0], "RegisterUnionUserToArea") {
		t.Fatalf("functions=%v", ws.Functions)
	}
	if !containsStr(ws.Callers, "cmd/main.go:3") {
		t.Fatalf("callers=%v", ws.Callers)
	}
	if containsStr(ws.Open, "scan inbound") {
		t.Fatalf("open should not ask for inbound after references: %v", ws.Open)
	}
}

func TestCollectCodeWorkset_calleesFromCallGraph(t *testing.T) {
	ws := CollectCodeWorkset([]ToolCallRecord{
		{ToolName: "rca_read", Result: map[string]any{
			"file": "helper.go", "content": "1|ok",
			"control_flow": c304ControlFlow(),
			"call_graph": &tool.CallGraph{
				Language: "go",
				Edges:    []tool.CallGraphEdge{{From: "RegisterUnionUserToArea@helper.go", To: "InsertUnionUserAreaInfo@pkg/db.go"}},
				Nodes:    []tool.CallGraphNode{{Name: "InsertUnionUserAreaInfo", File: "pkg/db.go", Resolved: true}},
			},
		}},
	})
	if !containsStr(ws.Callees, "InsertUnionUserAreaInfo@pkg/db.go") {
		t.Fatalf("callees=%v", ws.Callees)
	}
	if !containsStr(ws.Files, "pkg/db.go") {
		t.Fatalf("files=%v", ws.Files)
	}
}

func TestCollectCodeWorkset_openInbound(t *testing.T) {
	cf := c304ControlFlow()
	ws := CollectCodeWorkset([]ToolCallRecord{
		{ToolName: "rca_read", Result: map[string]any{
			"file": "helper.go", "content": "1|ok", "control_flow": cf,
		}},
	})
	if !containsStr(ws.Open, "scan inbound callers (rca_symbol action=references)") {
		t.Fatalf("open=%v", ws.Open)
	}
}

func TestUpsertCodeWorksetMessage_pinsSystemCard(t *testing.T) {
	ws := CodeWorkset{Files: []string{"helper.go"}, Functions: []string{"F@helper.go:1-10"}}
	msgs := []model.Message{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "q"},
	}
	out := upsertCodeWorksetMessage(msgs, ws)
	if len(out) != 3 || out[1].Role != "system" || !strings.Contains(out[1].Content, "[code_workset]") {
		t.Fatalf("out=%#v", out)
	}
	if out[1].Metadata[model.MetadataKeySixathOrigin] != model.OriginCodeWorkset {
		t.Fatalf("origin=%v", out[1].Metadata)
	}
	out2 := upsertCodeWorksetMessage(out, CodeWorkset{Files: []string{"other.go"}})
	n := 0
	for _, m := range out2 {
		if isCodeWorksetMessage(m) {
			n++
		}
	}
	if n != 1 || !strings.Contains(out2[1].Content, "other.go") {
		t.Fatalf("expected one replaced card, got %#v", out2)
	}
}

func TestSnipCompact_preservesCodeWorkset(t *testing.T) {
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "system", Content: "[code_workset]\nfiles: a.go", Metadata: map[string]any{
			model.MetadataKeySixathOrigin: model.OriginCodeWorkset,
		}},
		{Role: "user", Content: "q"},
	}
	out, _ := model.SnipCompactMessages(msgs)
	found := false
	for _, m := range out {
		if isCodeWorksetMessage(m) {
			found = true
		}
	}
	if !found {
		t.Fatal("code workset must survive snip compact")
	}
}

func containsStr(xs []string, want string) bool {
	for _, s := range xs {
		if s == want {
			return true
		}
	}
	return false
}
