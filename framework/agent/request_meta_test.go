package agent

import "testing"

func TestRequest_TypedFieldsTakesPrecedence(t *testing.T) {
	r := &Request{
		AgentName: "typed",
		Metadata:  map[string]any{MetaAgentName: "meta"},
	}
	r.Normalize()
	if r.AgentName != "typed" {
		t.Fatalf("AgentName = %q", r.AgentName)
	}
	if r.Metadata[MetaAgentName] != "typed" {
		t.Fatalf("metadata = %v", r.Metadata[MetaAgentName])
	}
}

func TestUsage_SyncAndFill(t *testing.T) {
	resp := &Response{
		Usage: Usage{InputTokens: 10, OutputTokens: 5},
	}
	resp.SyncUsageToMetadata()
	if resp.Metadata[MetaTokenInput] != int64(10) {
		t.Fatal()
	}
	resp2 := &Response{Metadata: map[string]any{MetaTokenOutput: float64(7)}}
	resp2.FillUsageFromMetadata()
	if resp2.Usage.OutputTokens != 7 {
		t.Fatalf("got %d", resp2.Usage.OutputTokens)
	}
}
