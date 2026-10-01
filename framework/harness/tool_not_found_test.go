package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestReActAgent_UnknownToolIsRecoverable(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name: "vm_run_cmd",
		Execute: func(context.Context, map[string]any) (any, error) {
			executed = true
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{
			{Used: true, ToolCalls: []model.ToolCall{{ID: "c1", Name: "em_run_cmd", Arguments: map[string]any{}}}},
			{Used: true, ToolCalls: []model.ToolCall{{ID: "c2", Name: "vm_run_cmd", Arguments: map[string]any{}}}},
		},
		finalReply: "done",
	}

	react := NewReActAgent(fake, memory.NewBufferMemory(5), reg)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "run"}},
	})
	if err != nil {
		t.Fatalf("unknown tool must not abort the run: %v", err)
	}
	if !executed {
		t.Fatalf("model retry with correct tool name was not executed")
	}
	trace, ok := resp.Metadata["trace"].(*RunTrace)
	if !ok || len(trace.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool call records, got %#v", resp.Metadata)
	}
	first := trace.ToolCalls[0]
	if !strings.Contains(first.Error, ErrToolNotFound.Error()) || !strings.Contains(first.Error, "vm_run_cmd") {
		t.Fatalf("expected not-found error with suggestion, got %q", first.Error)
	}
}

func TestSuggestToolNames(t *testing.T) {
	got := suggestToolNames("em_run_cmd", []string{"es_log_query", "vm_run_cmd", "rca_read"})
	if len(got) == 0 || got[0] != "vm_run_cmd" {
		t.Fatalf("expected vm_run_cmd first, got %v", got)
	}
	if s := suggestToolNames("totally_unrelated_xyz", []string{"vm_run_cmd"}); len(s) != 0 {
		t.Fatalf("expected no suggestion, got %v", s)
	}
}
