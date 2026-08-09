package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/growth"
)

func TestFormatTraceDigest_failuresFirst(t *testing.T) {
	traces := []agent.TurnTrace{
		{
			TurnSeq:   1,
			RequestID: "ok-req",
			Calls: []agent.TurnToolCall{
				{ToolName: "search", ResultPreview: "hits"},
			},
		},
		{
			TurnSeq:   2,
			RequestID: "fail-req",
			Calls: []agent.TurnToolCall{
				{ToolName: "shell", ResultPreview: "partial"},
				{ToolName: "write", Error: "disk full"},
			},
		},
	}
	got := formatTraceDigest(traces)
	if !strings.Contains(got, "# Turn traces") {
		t.Fatalf("missing header: %q", got)
	}
	failIdx := strings.Index(got, "request_id=fail-req")
	okIdx := strings.Index(got, "request_id=ok-req")
	if failIdx < 0 || okIdx < 0 {
		t.Fatalf("missing turns: %q", got)
	}
	if failIdx > okIdx {
		t.Fatalf("failed turn should come first:\n%s", got)
	}
	// Within failed turn, error call listed before ok call.
	writeIdx := strings.Index(got, "tool=write error=")
	shellIdx := strings.Index(got, "tool=shell result=")
	if writeIdx < 0 || shellIdx < 0 || writeIdx > shellIdx {
		t.Fatalf("failed calls should precede ok within turn:\n%s", got)
	}
}

func TestFormatTraceDigest_empty(t *testing.T) {
	if got := formatTraceDigest(nil); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestSkipPendingClaim_inFlightFresh(t *testing.T) {
	now := time.Now()
	since := now.Add(-2 * time.Minute)
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:             "g1",
		PendingSkillReview:    true,
		BgReviewInFlight:      true,
		BgReviewInFlightSince: &since,
		UpdatedAt:             now,
	}}
	uc := biz.NewGrowthUsecase(repo)
	w := &GrowthWorker{growthUC: uc}

	st, err := uc.GetState(context.Background(), "g1")
	if err != nil {
		t.Fatal(err)
	}
	if !w.skipPendingClaim(context.Background(), st) {
		t.Fatal("expected skip while in_flight is fresh")
	}
	st2, _ := uc.GetState(context.Background(), "g1")
	if !st2.BgReviewInFlight {
		t.Fatal("fresh in_flight must not be cleared")
	}
}

func TestSkipPendingClaim_staleInFlightCleared(t *testing.T) {
	now := time.Now()
	since := now.Add(-20 * time.Minute) // > default 15m TTL
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:             "g2",
		PendingSkillReview:    true,
		BgReviewInFlight:      true,
		BgReviewInFlightSince: &since,
		UpdatedAt:             now,
	}}
	uc := biz.NewGrowthUsecase(repo)
	w := &GrowthWorker{growthUC: uc}

	before := growth.DefaultMetrics.Snapshot().BgInFlightStaleCleared
	st, err := uc.GetState(context.Background(), "g2")
	if err != nil {
		t.Fatal(err)
	}
	if w.skipPendingClaim(context.Background(), st) {
		t.Fatal("stale in_flight should clear and allow claim")
	}
	st2, _ := uc.GetState(context.Background(), "g2")
	if st2.BgReviewInFlight {
		t.Fatal("stale in_flight should be cleared")
	}
	if growth.DefaultMetrics.Snapshot().BgInFlightStaleCleared <= before {
		t.Fatal("expected IncBgInFlightStaleCleared")
	}
}

func TestSkipPendingClaim_dedupeRecentBG(t *testing.T) {
	now := time.Now()
	last := now.Add(-3 * time.Minute) // within default 10m
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:              "g3",
		PendingSkillReview:     true,
		LastBackgroundReviewAt: &last,
		// UpdatedAt == last → no newer pending after review
		UpdatedAt: last,
	}}
	uc := biz.NewGrowthUsecase(repo)
	w := &GrowthWorker{growthUC: uc}
	st, _ := uc.GetState(context.Background(), "g3")
	if !w.skipPendingClaim(context.Background(), st) {
		t.Fatal("expected skip within dedupe_window without newer pending")
	}
}

func TestSkipPendingClaim_newerPendingOverridesDedupe(t *testing.T) {
	now := time.Now()
	last := now.Add(-3 * time.Minute)
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:              "g4",
		PendingSkillReview:     true,
		LastBackgroundReviewAt: &last,
		UpdatedAt:              now, // pending set after last review
	}}
	uc := biz.NewGrowthUsecase(repo)
	w := &GrowthWorker{growthUC: uc}
	st, _ := uc.GetState(context.Background(), "g4")
	if w.skipPendingClaim(context.Background(), st) {
		t.Fatal("newer pending within window must not skip")
	}
}

type fakeTurnTraceStore struct {
	bySession map[string][]agent.TurnTrace
}

func (f *fakeTurnTraceStore) Upsert(ctx context.Context, t *agent.TurnTrace) error {
	return nil
}
func (f *fakeTurnTraceStore) GetByRequest(ctx context.Context, sessionID, requestID string) (*agent.TurnTrace, error) {
	return nil, nil
}
func (f *fakeTurnTraceStore) ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.TurnTrace, error) {
	traces := f.bySession[sessionID]
	if limit > 0 && len(traces) > limit {
		return traces[:limit], nil
	}
	return traces, nil
}
func (f *fakeTurnTraceStore) DeactivateAfter(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}
func (f *fakeTurnTraceStore) ListByAgent(context.Context, string, time.Time, time.Time, int) ([]agent.TurnTrace, error) {
	return nil, nil
}

func TestFetchReviewTraceDigest_appendsSection(t *testing.T) {
	store := &fakeTurnTraceStore{bySession: map[string][]agent.TurnTrace{
		"s-dig": {{
			TurnSeq:   1,
			RequestID: "r1",
			Calls:     []agent.TurnToolCall{{ToolName: "x", Error: "boom"}},
		}},
	}}
	digest := fetchReviewTraceDigest(context.Background(), store, "s-dig")
	if !strings.Contains(digest, "# Turn traces") || !strings.Contains(digest, "tool=x error=boom") {
		t.Fatalf("unexpected digest: %q", digest)
	}
	got := appendTraceDigest("hello transcript", digest)
	if !strings.Contains(got, "hello transcript") || !strings.HasSuffix(strings.TrimSpace(got), "error=boom") {
		t.Fatalf("append failed: %q", got)
	}
}
