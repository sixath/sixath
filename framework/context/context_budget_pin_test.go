package context

import (
	"encoding/json"
	"github.com/sixath/framework/model"
	"strings"
	"testing"
)

func TestPruneToolMessageBodies_PreservesControlFlow(t *testing.T) {
	huge := strings.Repeat("LINE|src\n", 400)
	body := map[string]any{
		"tool": "rca_read",
		"result": map[string]any{
			"ok":      true,
			"file":    "helper.go",
			"content": huge,
			"call_graph": map[string]any{
				"language": "go",
				"nodes": []any{
					map[string]any{"id": "F", "name": "F", "resolved": true},
					map[string]any{"id": "InsertUnionUserAreaInfo", "name": "InsertUnionUserAreaInfo", "resolved": false},
				},
				"edges": []any{
					map[string]any{"from": "F", "to": "InsertUnionUserAreaInfo", "when": []any{"errcode == 0"}},
				},
			},
			"control_flow": []any{
				map[string]any{
					"function":   "RegisterUnionUserToArea",
					"start_line": 3,
					"end_line":   17,
					"paths": []any{
						map[string]any{
							"id":    "p1",
							"when":  []any{"errcode == 0"},
							"calls": []any{"InsertUnionUserAreaInfo"},
						},
						map[string]any{
							"id":    "p2",
							"when":  []any{"errcode != 0"},
							"calls": []any{},
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []model.Message{{Role: "tool", Content: string(raw)}}
	out := pruneToolMessageBodies(msgs, 500)
	got := out[0].Content
	if !strings.Contains(got, `"control_flow"`) {
		t.Fatalf("control_flow missing after prune: %s", got)
	}
	if !strings.Contains(got, `"call_graph"`) || !strings.Contains(got, `"from"`) {
		t.Fatalf("call_graph missing after prune: %s", got)
	}
	if !strings.Contains(got, "errcode == 0") || !strings.Contains(got, "InsertUnionUserAreaInfo") {
		t.Fatalf("pinned path table lost: %s", got)
	}
	var parsed any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("pruned tool body must remain valid JSON: %v\n%s", err, got)
	}
}

func TestPruneToolMessageBodies_NonJSONStillTruncates(t *testing.T) {
	msgs := []model.Message{{Role: "tool", Content: strings.Repeat("z", 400)}}
	out := pruneToolMessageBodies(msgs, 50)
	if !strings.Contains(out[0].Content, "truncated for L2 pre-prune") {
		t.Fatalf("plain tool body should still truncate, got %q", out[0].Content)
	}
}

func TestPrepare_L2PrePrunePinsControlFlow(t *testing.T) {
	huge := strings.Repeat("x", 5000)
	body := `{"tool":"rca_read","result":{"content":"` + huge + `","control_flow":[{"function":"F","paths":[{"id":"p1","when":["errcode == 0"],"calls":["InsertUnionUserAreaInfo"]}]}]}}`
	msgs := []model.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "why 1105"},
		{Role: "tool", Content: body},
	}
	cfg := &PipelineConfig{L2: NewL2Runtime(nil, 32000, 3, 600, 0, 400)}
	out := Prepare(msgs, cfg)
	var tool string
	for _, m := range out {
		if strings.EqualFold(m.Role, "tool") {
			tool = m.Content
		}
	}
	if tool == "" {
		t.Fatal("tool message dropped")
	}
	if !strings.Contains(tool, "control_flow") || !strings.Contains(tool, "errcode == 0") {
		t.Fatalf("L2 pre-prune must keep CFG, got %s", tool)
	}
	var parsed any
	if err := json.Unmarshal([]byte(tool), &parsed); err != nil {
		t.Fatalf("pinned prune must keep valid JSON: %v", err)
	}
}
