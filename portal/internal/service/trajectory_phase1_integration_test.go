package service

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/growth"
)

// TestTrajectoryPhase1_FinalizeTurnInFlightSkipsWorker is Task 14 Step 2:
// FinalizeTurn sets pending → SetBgReviewInFlight → worker skipPendingClaim.
func TestTrajectoryPhase1_FinalizeTurnInFlightSkipsWorker(t *testing.T) {
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{SessionID: "e2e-bg-1"}}
	uc := biz.NewGrowthUsecase(repo)
	uc.SetBackgroundReviewEnabled(true)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 1, MemoryTurnInterval: 99})

	spawnSkill, spawnMemory, err := uc.FinalizeTurnForBackgroundReview(context.Background(), "e2e-bg-1", "req-e2e", 1, true)
	if err != nil {
		t.Fatalf("FinalizeTurn: %v", err)
	}
	if !spawnSkill {
		t.Fatal("expected SpawnSkill after 1 successful tool with interval=1")
	}
	if spawnMemory {
		t.Fatal("memory interval 99 should not spawn on first turn")
	}

	if err := uc.SetBgReviewInFlight(context.Background(), "e2e-bg-1", true); err != nil {
		t.Fatalf("SetBgReviewInFlight: %v", err)
	}

	w := &GrowthWorker{growthUC: uc}
	st, err := uc.GetState(context.Background(), "e2e-bg-1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.PendingSkillReview || !st.BgReviewInFlight {
		t.Fatalf("state after finalize+inflight: %+v", st)
	}
	if !w.skipPendingClaim(context.Background(), st) {
		t.Fatal("worker must skip while C3 background review is in_flight")
	}

	// Stale in_flight clears and allows claim (Manual QA #2 path).
	stale := time.Now().Add(-20 * time.Minute)
	st2, _ := uc.GetState(context.Background(), "e2e-bg-1")
	st2.BgReviewInFlight = true
	st2.BgReviewInFlightSince = &stale
	_ = repo.SaveState(context.Background(), st2)
	st3, _ := uc.GetState(context.Background(), "e2e-bg-1")
	if w.skipPendingClaim(context.Background(), st3) {
		t.Fatal("stale in_flight should allow worker claim")
	}
	st4, _ := uc.GetState(context.Background(), "e2e-bg-1")
	if st4.BgReviewInFlight {
		t.Fatal("stale in_flight should be cleared")
	}
}
