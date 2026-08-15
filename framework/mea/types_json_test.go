package mea

import (
	"encoding/json"
	"testing"
)

func TestTaskStateJSON_RoundTrip(t *testing.T) {
	s := TaskState{
		Version:   1,
		SessionID: "sess-1",
		AgentID:   "agent-a",
		Goal:      "create out.txt with hello",
		Records: []TaskRecord{{
			ID:      "r1",
			Kind:    KindRequirement,
			Status:  StatusPending,
			Summary: "out.txt contains hello",
		}},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got TaskState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Goal != s.Goal || len(got.Records) != 1 || got.Records[0].Status != StatusPending {
		t.Fatalf("got %+v", got)
	}
}
