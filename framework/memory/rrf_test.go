package memory

import "testing"

func TestRRFMerge_DedupSumsRanks(t *testing.T) {
	like := []MemoryHit{
		{ID: "a", Content: "a", Score: 0},
		{ID: "b", Content: "b-like", Score: 0, Scope: ScopeSession, Source: SourceUnits},
	}
	vec := []MemoryHit{
		{ID: "b", Content: "b-vec", Score: 0.9, Scope: ScopeAgent, Source: SourceFiles},
		{ID: "c", Content: "c", Score: 0.8},
	}
	out := rrfMerge(like, vec, 3)
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	// b gets 1/(60+2)+1/(60+1) > a gets 1/(60+1) > c gets 1/(60+2)
	if out[0].ID != "b" {
		t.Fatalf("want b first, got %+v", out)
	}
	wantB := 1.0/62 + 1.0/61
	if out[0].Score < wantB-1e-9 || out[0].Score > wantB+1e-9 {
		t.Fatalf("b score=%v want %v", out[0].Score, wantB)
	}
	if out[0].Content != "b-like" {
		t.Fatalf("dual-hit must keep LIKE content, got %q", out[0].Content)
	}
	if out[0].Scope != ScopeSession || out[0].Source != SourceUnits {
		t.Fatalf("dual-hit must keep LIKE Scope/Source, got scope=%v source=%v", out[0].Scope, out[0].Source)
	}
	if out[1].ID != "a" || out[2].ID != "c" {
		t.Fatalf("order after b want a,c got %+v", out)
	}
}

func TestRRFMerge_TieBreakLikeThenVectorThenID(t *testing.T) {
	// z and y both rank-1 on their lists → same score 1/61; a is like rank-2 → 1/62.
	// After score: z and y tied; like-present wins over vector-only → z before y; then a.
	like := []MemoryHit{{ID: "z"}, {ID: "a"}}
	vec := []MemoryHit{{ID: "y"}}
	out := rrfMerge(like, vec, 3)
	if len(out) != 3 || out[0].ID != "z" || out[1].ID != "y" || out[2].ID != "a" {
		t.Fatalf("tie order = %+v, want z,y,a", out)
	}
	// equal score + both like-present: like=[m,n], vector=[n,m] → m,n (LIKE original order)
	like2 := []MemoryHit{{ID: "m"}, {ID: "n"}} // ranks 1,2
	vec2 := []MemoryHit{{ID: "n"}, {ID: "m"}}   // ranks 1,2 → both get 1/61+1/62
	out2 := rrfMerge(like2, vec2, 2)
	if out2[0].ID != "m" || out2[1].ID != "n" {
		t.Fatalf("equal dual-hit tie want like order m,n got %+v", out2)
	}
}

func TestRRFMerge_LimitEmptyIDAndNonPositiveLimit(t *testing.T) {
	like := []MemoryHit{
		{ID: "a"},
		{ID: ""},
		{ID: "b"},
	}
	vec := []MemoryHit{
		{ID: ""},
		{ID: "c"},
	}
	out := rrfMerge(like, vec, 2)
	if len(out) != 2 {
		t.Fatalf("limit truncate len=%d want 2, got %+v", len(out), out)
	}
	if out[0].ID == "" || out[1].ID == "" {
		t.Fatalf("empty ID must be skipped, got %+v", out)
	}

	if got := rrfMerge(like, vec, 0); got != nil {
		t.Fatalf("limit=0 want nil, got %+v", got)
	}
	if got := rrfMerge(like, vec, -1); got != nil {
		t.Fatalf("limit<0 want nil, got %+v", got)
	}
}
