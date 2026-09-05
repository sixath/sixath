package context

import (
	stdctx "context"
	"encoding/json"
	"github.com/sixath/framework/model"
	"strings"
	"testing"
)

func TestEnsureCodePinMessages_extractsControlFlow(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tool": "rca_read",
		"result": map[string]any{
			"file":    "helper.go",
			"content": "if errcode == 0 { Insert() }",
			"control_flow": []any{
				map[string]any{
					"function": "Handle",
					"paths": []any{
						map[string]any{"id": "p1", "when": []any{"errcode == 0"}, "calls": []any{"InsertUnionUserAreaInfo"}},
					},
				},
			},
		},
	})
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q"},
		{Role: "tool", Content: string(body)},
	}
	out := ensureCodePinMessages(msgs)
	if !isCodePinMessage(out[1]) {
		t.Fatalf("expected pin after leading system, got %#v", out)
	}
	if !strings.Contains(out[1].Content, "errcode == 0") || !strings.Contains(out[1].Content, "Insert()") {
		t.Fatalf("pin missing when or content source: %s", out[1].Content)
	}
}

func TestEnsureCodePinMessages_pinsContentWithoutCFG(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tool": "rca_read",
		"result": map[string]any{
			"file":    "GameInfoModel.go",
			"repo":    "zone-4100",
			"content": "func GetGameInfo() error { return redis.Nil }",
		},
	})
	out := ensureCodePinMessages([]model.Message{
		{Role: "system", Content: "sys"},
		{Role: "tool", Content: string(body)},
	})
	if !isCodePinMessage(out[1]) || !strings.Contains(out[1].Content, "redis.Nil") {
		t.Fatalf("content-only rca_read must pin: %#v", out)
	}
	if !strings.Contains(out[1].Content, "zone-4100") {
		t.Fatalf("pin must include repo: %s", out[1].Content)
	}
}

func TestEnsureCodePinMessages_dropsCallGraphBeforeContent(t *testing.T) {
	// content 很长 + 巨大 call_graph 时，pin 截断后 content 仍非空
	content := strings.Repeat("SRC", 3000)
	cg := map[string]any{"nodes": []any{map[string]any{"id": strings.Repeat("N", 5000)}}}
	body, _ := json.Marshal(map[string]any{
		"tool":   "rca_read",
		"result": map[string]any{"file": "ReleaseVm.go", "content": content, "call_graph": cg},
	})
	out := ensureCodePinMessages([]model.Message{
		{Role: "system", Content: "sys"},
		{Role: "tool", Content: string(body)},
	})
	if !isCodePinMessage(out[1]) {
		t.Fatalf("expected pin, got %#v", out)
	}
	raw := out[1].Content
	if !strings.Contains(raw, "SRC") {
		t.Fatalf("must keep content window: %s", raw[:min(200, len(raw))])
	}
	if strings.Contains(raw, "call_graph") {
		t.Fatalf("must not pin full call_graph")
	}
}

func TestPrepareCtx_L2KeepsCodePin(t *testing.T) {
	cf := []any{
		map[string]any{
			"function": "Handle",
			"paths": []any{
				map[string]any{"id": "p1", "when": []any{"errcode == 0"}, "calls": []any{"InsertUnionUserAreaInfo"}},
			},
		},
	}
	body, _ := json.Marshal(map[string]any{
		"tool": "rca_read",
		"result": map[string]any{
			"file":         "helper.go",
			"content":      strings.Repeat("x", 4000),
			"control_flow": cf,
			"start_line":   10,
			"end_line":     80,
		},
	})
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: strings.Repeat("测", 3000)},
		{Role: "assistant", Content: "ack", Metadata: map[string]any{"tool_calls": []model.ToolCall{{ID: "1", Name: "rca_read"}}}},
		{Role: "tool", Content: string(body), Metadata: map[string]any{"tool_call_id": "1"}},
		{Role: "user", Content: "final question"},
	}
	var sawL2 bool
	sink := func(kind string, detail map[string]any) {
		if kind == "l2_summarize" {
			sawL2 = true
		}
	}
	r := NewL2Runtime(stubAuxModel{}, 200, 3, 600, 2.0, 400)
	out := PrepareCtx(stdctx.Background(), msgs, &PipelineConfig{L2: r, Trace: sink})
	if !sawL2 {
		t.Fatal("expected l2_summarize")
	}
	found := false
	for _, m := range out {
		if strings.Contains(m.Content, "xxxx") || strings.Contains(m.Content, "errcode == 0") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("content window or when missing after L2: %#v", out)
	}
}
