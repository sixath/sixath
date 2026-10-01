package tool

import (
	"context"
	"testing"
)

func TestFieldHistory_SegmentsAndTransitions(t *testing.T) {
	reg := NewRegistry()
	var calls []map[string]any
	_ = reg.Register(Tool{Name: "es_log_query", Effect: EffectRead, Toolset: ToolsetRCA,
		Execute: func(ctx context.Context, p map[string]any) (any, error) {
			calls = append(calls, p)
			if p["from"].(int) > 0 {
				return map[string]any{"ok": true, "total": 4, "hits": []any{}}, nil
			}
			return map[string]any{"ok": true, "total": 4, "hits": []any{
				map[string]any{"@timestamp": "2026-09-28T04:12:00Z", "message": "LaunchGame gid[32745] startup_wait_seconds=180"},
				map[string]any{"@timestamp": "2026-09-28T04:13:00Z", "message": "LaunchGame gid[32745] startup_wait_seconds=180"},
				map[string]any{"@timestamp": "2026-09-28T04:15:30Z", "message": "LaunchGame gid[32745] startup_wait_seconds=360"},
				map[string]any{"@timestamp": "2026-09-28T04:21:00Z", "message": "LaunchGame gid[32745] startup_wait_seconds: 600"},
			}}, nil
		}})
	if err := RegisterFieldHistoryTool(reg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get(FieldHistoryToolName)
	res, _ := tl.Execute(context.Background(), map[string]any{
		"base_args": map[string]any{"query": "startup_wait_seconds"},
		"pattern":   `startup_wait_seconds\D{0,3}(\d+)`,
	})
	out := res.(map[string]any)
	segs, _ := out["segments"].([]map[string]any)
	trans, _ := out["transitions"].([]map[string]any)
	if len(segs) != 3 || segs[0]["value"] != "180" || segs[0]["count"] != 2 || segs[2]["value"] != "600" {
		t.Fatalf("segments = %v", segs)
	}
	if len(trans) != 2 || trans[0]["from"] != "180" || trans[0]["to"] != "360" || trans[0]["time"] != "2026-09-28T04:15:30.000Z" {
		t.Fatalf("transitions = %v", trans)
	}
	if calls[0]["time_from"] != "now-3d" || calls[0]["sort"] != "asc" {
		t.Fatalf("first call args = %v", calls[0])
	}
}

func TestFieldHistory_Validation(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Tool{Name: "es_log_query", Effect: EffectRead, Execute: func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true, "hits": []any{}}, nil
	}})
	_ = RegisterFieldHistoryTool(reg)
	tl, _ := reg.Get(FieldHistoryToolName)
	res, _ := tl.Execute(context.Background(), map[string]any{"base_args": map[string]any{}, "pattern": `timeout\d+`})
	if res.(map[string]any)["ok"] != false {
		t.Fatal("pattern without capture group must be rejected")
	}
	res, _ = tl.Execute(context.Background(), map[string]any{"base_args": map[string]any{}, "pattern": `timeout=(\d+)`})
	if out := res.(map[string]any); out["ok"] != true || out["note"] == nil {
		t.Fatalf("empty result must explain: %v", out)
	}
}
