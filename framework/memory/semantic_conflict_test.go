package memory

import (
	"context"
	"testing"
)

func TestStubSemanticConflictResolver_KeepBoth(t *testing.T) {
	r := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	v, err := r.ResolveAdd(context.Background(), RememberInput{Content: "x"}, nil)
	if err != nil || v.Decision != ConflictKeepBoth {
		t.Fatalf("got %+v err=%v", v, err)
	}
}
