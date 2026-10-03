package harness

import (
	"context"
	"reflect"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestToolCallRecord_HitStatusAndCheckRejects(t *testing.T) {
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{Used: true, ToolName: "picky", Arguments: map[string]any{"x": "y"}},
			{Used: true, ToolName: "search", Arguments: map[string]any{}},
			{Used: true, ToolName: "fixture", Arguments: map[string]any{}},
		},
		finalReply: "done",
	}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(tool.Tool{Name: "picky", Execute: func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": false}, &tool.InvalidArgumentsError{Tool: "picky", Errors: []tool.SchemaError{
			{Path: "repo", Keyword: tool.KeywordOneOf, Message: "bad repo"},
			{Path: "x", Keyword: tool.KeywordUnknownField, Message: "unknown x"},
		}}
	}})
	_ = reg.Register(tool.Tool{Name: "search", Execute: func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true, "hit_status": tool.HitStatusSuspect}, nil
	}})
	_ = reg.Register(tool.Tool{Name: "fixture", Execute: func(context.Context, map[string]any) (any, error) {
		return `{"ok":true,"hit_status":"empty"}`, nil
	}})

	resp, err := NewReActAgent(fake, nil, reg).Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "q"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	trace := resp.Metadata["trace"].(*RunTrace)
	if len(trace.ToolCalls) != 3 {
		t.Fatalf("tool calls: %+v", trace.ToolCalls)
	}
	rejected, suspect, fixture := trace.ToolCalls[0], trace.ToolCalls[1], trace.ToolCalls[2]
	if !reflect.DeepEqual(rejected.CheckRejects, []string{tool.KeywordOneOf, tool.KeywordUnknownField}) || rejected.Error == "" {
		t.Fatalf("rejected: %+v", rejected)
	}
	if suspect.HitStatus != tool.HitStatusSuspect || len(suspect.CheckRejects) != 0 {
		t.Fatalf("suspect: %+v", suspect)
	}
	if fixture.HitStatus != tool.HitStatusEmpty {
		t.Fatalf("JSON string result: %+v", fixture)
	}
}
