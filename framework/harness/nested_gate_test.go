package harness

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReActAgent_CompareInnerCallsGoThroughToolHooks(t *testing.T) {
	var mu sync.Mutex
	var probed []string
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:       "probe",
		Effect:     tool.EffectRead,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"host": map[string]any{"type": "string"}}},
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			h, _ := args["host"].(string)
			mu.Lock()
			probed = append(probed, h)
			mu.Unlock()
			return map[string]any{"stdout": "host=" + h}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tool.RegisterCompareTool(reg); err != nil {
		t.Fatal(err)
	}
	var hookSaw []string
	hook := &recordingHook{name: "deny-prod", order: new([]string), before: func(_ context.Context, name string, args map[string]any) (map[string]any, error) {
		mu.Lock()
		hookSaw = append(hookSaw, name)
		mu.Unlock()
		if h, _ := args["host"].(string); h == "prod" {
			return nil, errors.New("prod is off limits")
		}
		return args, nil
	}}
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: tool.CompareToolName,
			Arguments: map[string]any{
				"tool": "probe",
				"targets": []any{
					map[string]any{"label": "prod", "args": map[string]any{"host": "prod"}},
					map[string]any{"label": "staging", "args": map[string]any{"host": "staging"}},
				},
			},
		}},
		finalReply: "done",
	}
	react := NewReActAgent(fake, nil, reg, WithReActToolHooks(hook), WithReActMaxSteps(3))
	resp, err := react.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(probed) != 1 || probed[0] != "staging" {
		t.Fatalf("hook must block the prod probe, probed=%v", probed)
	}
	sort.Strings(hookSaw)
	if strings.Join(hookSaw, ",") != "compare,probe,probe" {
		t.Fatalf("hook should see the outer call and both inner calls, saw=%v", hookSaw)
	}
	tr := resp.Metadata["trace"].(*RunTrace)
	res := tr.ToolCalls[0].Result.(map[string]any)
	targets := res["targets"].([]map[string]any)
	if targets[0]["ok"] != false || !strings.Contains(targets[0]["error"].(string), "prod is off limits") {
		t.Fatalf("blocked target should report the hook reason: %#v", targets[0])
	}
}
