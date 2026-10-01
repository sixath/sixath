package harness

import (
	"context"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestRecordEmptyFinal_CapturesDiagnostics(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{ToolName: "es_log_query"}}}
	tr.recordEmptyFinal(&model.Generation{
		Text:         " \n",
		FinishReason: "length",
		TokenUsage:   &model.TokenUsage{InputTokens: 9000, OutputTokens: 1024},
		Raw:          model.ToolStep{ReasoningContent: "思考了很久"},
	}, 15)
	d := tr.EmptyFinal
	if d == nil {
		t.Fatal("expected EmptyFinal")
	}
	if d.Step != 15 || d.FinishReason != "length" || d.InputTokens != 9000 || d.OutputTokens != 1024 || d.ToolCalls != 1 {
		t.Fatalf("diag=%+v", d)
	}
	if d.ReasoningChars != 5 || d.ReasoningPreview != "思考了很久" {
		t.Fatalf("reasoning diag=%+v", d)
	}
}

func TestRecordEmptyFinal_IgnoresNonEmpty(t *testing.T) {
	tr := &RunTrace{}
	tr.recordEmptyFinal(&model.Generation{Text: "答案"}, 1)
	if tr.EmptyFinal != nil {
		t.Fatalf("non-empty reply must not record: %+v", tr.EmptyFinal)
	}
}

func TestReActAgent_EmptyFinalRecordedInTrace(t *testing.T) {
	fake := &fakeOpenAIClient{finalReply: "\n\n"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, memory.NewBufferMemory(5), reg)
	resp, err := react.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "你好"}}})
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil || tr.EmptyFinal == nil {
		t.Fatalf("expected EmptyFinal in trace, got %#v", tr)
	}
}

func TestBuildTurnTrace_CopiesEmptyFinal(t *testing.T) {
	tr := &RunTrace{EmptyFinal: &EmptyFinalDiag{Step: 3, FinishReason: "stop"}}
	out := BuildTurnTrace(TurnTraceMeta{}, tr)
	if out.EmptyFinal == nil || out.EmptyFinal.Step != 3 {
		t.Fatalf("EmptyFinal not copied: %+v", out.EmptyFinal)
	}
}
