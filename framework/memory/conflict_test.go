package memory

import (
	"context"
	"testing"
)

func TestStructuralReplaceResolver_ReturnsSupersede(t *testing.T) {
	var r StructuralReplaceResolver
	d, err := r.Resolve(context.Background(), MemoryHit{ID: "old", Content: "a"}, RememberInput{
		Action: ActionReplace, Content: "b", UnitID: "old",
	})
	if err != nil || d != ConflictSupersede {
		t.Fatalf("decision=%v err=%v", d, err)
	}
}
