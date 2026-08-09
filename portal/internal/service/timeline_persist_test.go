package service

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestTimelineAccumulator_ToolAndModelFinalize(t *testing.T) {
	var a TimelineAccumulator
	a.ApplyToolCall(&ToolCallPayload{
		ID: "c1", Step: 1, Phase: "started", ToolName: "list_tools", Allowed: true,
	})
	a.ApplyModelCall(&ModelCallPayload{Step: 0, Phase: "invoked", Model: "gpt-4o"})
	a.ApplyToolCall(&ToolCallPayload{
		ID: "c1", Step: 1, Phase: "completed", ToolName: "list_tools", Allowed: true, DurationMS: 5,
	})
	a.ApplyModelCall(&ModelCallPayload{Step: 0, Phase: "responded", Model: "gpt-4o", InputTokens: 10, OutputTokens: 3})
	a.ApplyModelCall(&ModelCallPayload{Step: 2, Phase: "invoked", Model: "gpt-4o"})

	nodes := a.Finalize()
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
	// step order: model step0, tool step1, model step2(interrupted)
	if nodes[0].Kind != "model" || nodes[0].Phase != "responded" || nodes[0].InputTokens == nil || *nodes[0].InputTokens != 10 {
		t.Fatalf("model0: %+v", nodes[0])
	}
	if nodes[1].Kind != "tool" || nodes[1].Phase != "completed" || nodes[1].DurationMs == nil || *nodes[1].DurationMs != 5 {
		t.Fatalf("tool: %+v", nodes[1])
	}
	if nodes[2].Kind != "model" || nodes[2].Phase != "interrupted" {
		t.Fatalf("model2 should be interrupted: %+v", nodes[2])
	}

	md := MetadataWithTimeline(nodes)
	if md == nil {
		t.Fatal("expected metadata")
	}
	tl, ok := md["timeline"].([]any)
	if !ok || len(tl) != 3 {
		t.Fatalf("timeline: %#v", md["timeline"])
	}
}

func TestMetadataWithTimeline_Empty(t *testing.T) {
	if MetadataWithTimeline(nil) != nil {
		t.Fatal("nil timeline should yield nil metadata")
	}
}

func TestMetadataWithTimeline_StructpbCompatible(t *testing.T) {
	var a TimelineAccumulator
	a.ApplyToolCall(&ToolCallPayload{
		ID: "c1", Step: 1, Phase: "completed", ToolName: "list_tools",
		Allowed: true, DurationMS: 5, Arguments: map[string]any{"q": "x"},
	})
	a.ApplyModelCall(&ModelCallPayload{Step: 0, Phase: "responded", Model: "m", InputTokens: 2})
	md := MetadataWithTimeline(a.Finalize())
	st, err := structpb.NewStruct(md)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	if st.Fields["timeline"] == nil {
		t.Fatal("missing timeline field")
	}
}
