package growth

import "testing"

func TestIsGrowthReviewMetadata(t *testing.T) {
	if IsGrowthReviewMetadata(nil) {
		t.Fatal("nil")
	}
	if IsGrowthReviewMetadata(map[string]any{}) {
		t.Fatal("empty")
	}
	if !IsGrowthReviewMetadata(map[string]any{MetaGrowthReview: true}) {
		t.Fatal("bool true")
	}
	if !IsGrowthReviewMetadata(map[string]any{MetaGrowthReview: "1"}) {
		t.Fatal("string 1")
	}
	if IsGrowthReviewMetadata(map[string]any{MetaGrowthReview: false}) {
		t.Fatal("bool false")
	}
}

func TestShouldSkipGrowthReview(t *testing.T) {
	if !ShouldSkipGrowthReview(map[string]any{MetaSkipGrowthReview: true}) {
		t.Fatal("skip_growth_review")
	}
	if ShouldSkipGrowthReview(map[string]any{MetaSkipGrowthReview: false}) {
		t.Fatal("false skip")
	}
}

func TestShouldSkipMemory(t *testing.T) {
	if !ShouldSkipMemory(map[string]any{MetaSkipMemory: true}) {
		t.Fatal("skip_memory")
	}
	if ShouldSkipMemory(nil) {
		t.Fatal("nil")
	}
}

func TestMergeReviewMetadata(t *testing.T) {
	in := map[string]any{"session_id": "s1"}
	out := MergeReviewMetadata(in)
	if out["session_id"] != "s1" {
		t.Fatal("lost session_id")
	}
	if !IsGrowthReviewMetadata(out) {
		t.Fatal("expected growth review flag")
	}
	if _, ok := in[MetaGrowthReview]; ok {
		t.Fatal("should not mutate input map")
	}
}
