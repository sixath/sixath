package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/growthwake"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/sixath/framework/growth"
)

type fakeGrowthRepo struct {
	state *ChatGrowthState
}

func (f *fakeGrowthRepo) GetState(ctx context.Context, sessionID string) (*ChatGrowthState, error) {
	_ = ctx
	if f.state == nil || f.state.SessionID != sessionID {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *f.state
	return &cp, nil
}

func (f *fakeGrowthRepo) SaveState(ctx context.Context, st *ChatGrowthState) error {
	_ = ctx
	if st == nil {
		return errors.New("nil state")
	}
	cp := *st
	f.state = &cp
	return nil
}

func (f *fakeGrowthRepo) TryAcquireLease(ctx context.Context, workspaceKey, holderID string, ttl time.Duration) (bool, error) {
	return false, errors.New("not used")
}

func (f *fakeGrowthRepo) ReleaseLease(ctx context.Context, workspaceKey, holderID string) error {
	return errors.New("not used")
}

func (f *fakeGrowthRepo) ListPendingReviewSessions(ctx context.Context, limit int) ([]GrowthPendingSession, error) {
	_ = ctx
	_ = limit
	return nil, nil
}

func (f *fakeGrowthRepo) ListIdleSessions(ctx context.Context, idleInterval time.Duration, limit int) ([]GrowthPendingSession, error) {
	_ = ctx
	_ = idleInterval
	_ = limit
	return nil, nil
}

func TestGrowthUsecase_OnToolSuccess_setsPendingAndResetsCounter(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "s1"}}
	uc := NewGrowthUsecase(repo)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true})
	ctx := context.Background()
	for i := 0; i < 9; i++ {
		if err := uc.OnToolSuccess(ctx, "s1"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if repo.state.PendingSkillReview {
		t.Fatal("pending too early")
	}
	if repo.state.ToolItersSinceReview != 9 {
		t.Fatalf("tool iters want 9 got %d", repo.state.ToolItersSinceReview)
	}
	if err := uc.OnToolSuccess(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("expected pending_skill_review")
	}
	if repo.state.ToolItersSinceReview != 0 {
		t.Fatalf("counter reset want 0 got %d", repo.state.ToolItersSinceReview)
	}
}

func TestGrowthUsecase_OnToolSuccess_nudgeDisabled_neverPending(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "nd1"}}
	uc := NewGrowthUsecase(repo)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: false, SkillToolInterval: 3})
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if err := uc.OnToolSuccess(ctx, "nd1"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if repo.state.PendingSkillReview {
		t.Fatal("Enabled=false must never set PendingSkillReview")
	}
	if repo.state.ToolItersSinceReview != 3 {
		t.Fatalf("counter should cap at interval=3, got %d", repo.state.ToolItersSinceReview)
	}
}

func TestGrowthUsecase_OnToolSuccess_customSkillInterval(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "ci1"}}
	uc := NewGrowthUsecase(repo)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 2})
	ctx := context.Background()
	if err := uc.OnToolSuccess(ctx, "ci1"); err != nil {
		t.Fatal(err)
	}
	if repo.state.PendingSkillReview {
		t.Fatal("pending too early on 1st success")
	}
	if err := uc.OnToolSuccess(ctx, "ci1"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("expected pending on 2nd success with SkillToolInterval=2")
	}
	if repo.state.ToolItersSinceReview != 0 {
		t.Fatalf("counter reset want 0 got %d", repo.state.ToolItersSinceReview)
	}
}

func TestGrowthUsecase_OnToolSuccess_intervalZeroUsesDefaults(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "z0"}}
	uc := NewGrowthUsecase(repo)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 0})
	ctx := context.Background()
	def := growth.NewDefaults().SkillToolInterval
	for i := 0; i < def-1; i++ {
		if err := uc.OnToolSuccess(ctx, "z0"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if repo.state.PendingSkillReview {
		t.Fatal("interval 0 must not fire every time; pending too early")
	}
	if repo.state.ToolItersSinceReview != def-1 {
		t.Fatalf("want tool iters %d got %d", def-1, repo.state.ToolItersSinceReview)
	}
	if err := uc.OnToolSuccess(ctx, "z0"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingSkillReview {
		t.Fatalf("expected pending at default interval=%d", def)
	}
}

func TestGrowthUsecase_OnAssistantTurn_nudgeDisabled_capsCounter(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "ndm"}}
	uc := NewGrowthUsecase(repo)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: false, MemoryTurnInterval: 2})
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := uc.OnAssistantTurn(ctx, "ndm"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if repo.state.PendingMemoryReview {
		t.Fatal("Enabled=false must never set PendingMemoryReview")
	}
	if repo.state.TurnsSinceMemoryReview != 2 {
		t.Fatalf("turns should cap at interval=2, got %d", repo.state.TurnsSinceMemoryReview)
	}
}

func TestGrowthUsecase_ClearGrowthPending_partialSkill(t *testing.T) {
	now := time.Now()
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:           "p1",
		PendingSkillReview:  true,
		PendingMemoryReview: true,
		LastSkillError:      "se", LastMemoryError: "me", ReviewFailedAt: &now,
	}}
	uc := NewGrowthUsecase(repo)
	if err := uc.ClearGrowthPending(context.Background(), "p1", true, false); err != nil {
		t.Fatal(err)
	}
	if repo.state.PendingSkillReview {
		t.Fatal("skill should be cleared")
	}
	if !repo.state.PendingMemoryReview {
		t.Fatal("memory pending should remain")
	}
	if repo.state.LastSkillError != "" {
		t.Fatalf("LastSkillError want empty got %q", repo.state.LastSkillError)
	}
	if repo.state.LastMemoryError != "me" {
		t.Fatalf("LastMemoryError should remain: %q", repo.state.LastMemoryError)
	}
	if repo.state.ReviewFailedAt == nil {
		t.Fatal("ReviewFailedAt should remain while memory pending still true")
	}
}

func TestGrowthUsecase_ClearReviewPending(t *testing.T) {
	now := time.Now()
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:          "x",
		PendingSkillReview: true, PendingMemoryReview: true,
		LastSkillError: "e1", LastMemoryError: "e2", ReviewFailedAt: &now,
	}}
	uc := NewGrowthUsecase(repo)
	if err := uc.ClearReviewPending(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if repo.state.PendingSkillReview || repo.state.PendingMemoryReview {
		t.Fatal("expected pending cleared")
	}
	if repo.state.LastSkillError != "" || repo.state.LastMemoryError != "" || repo.state.ReviewFailedAt != nil {
		t.Fatal("expected errors and review_failed_at cleared")
	}
}

func TestGrowthUsecase_RecordReviewRunFailure(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "f1", PendingSkillReview: true, PendingMemoryReview: true}}
	uc := NewGrowthUsecase(repo)
	err := uc.RecordReviewRunFailure(context.Background(), "f1", errors.New("boom"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if repo.state.LastSkillError != "boom" || repo.state.LastMemoryError != "boom" {
		t.Fatalf("last errors %#v", repo.state)
	}
	if repo.state.ReviewFailedAt == nil {
		t.Fatal("expected ReviewFailedAt")
	}
}

func TestGrowthUsecase_OnAssistantTurn_setsPendingMemory(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "s2"}}
	uc := NewGrowthUsecase(repo)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := uc.OnAssistantTurn(ctx, "s2"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if repo.state.PendingMemoryReview {
		t.Fatal("pending memory too early")
	}
	if err := uc.OnAssistantTurn(ctx, "s2"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingMemoryReview {
		t.Fatal("expected pending_memory_review")
	}
	if repo.state.TurnsSinceMemoryReview != 0 {
		t.Fatalf("turns reset want 0 got %d", repo.state.TurnsSinceMemoryReview)
	}
}

func TestGrowthUsecase_TrySessionEndMemoryReview_setsPendingWhenEnabled(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:              "c2",
		TurnsSinceMemoryReview: 1,
	}}
	uc := NewGrowthUsecase(repo)
	uc.SetSessionEndMemoryReviewEnabled(true)
	if err := uc.TrySessionEndMemoryReview(context.Background(), "c2"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingMemoryReview {
		t.Fatal("expected pending_memory_review from session-end light review")
	}
}

func TestGrowthUsecase_TrySessionEndMemoryReview_skipsWhenRecentBackgroundReview(t *testing.T) {
	last := time.Now().Add(-2 * time.Minute)
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:              "c2-dedupe",
		TurnsSinceMemoryReview: 2,
		LastBackgroundReviewAt: &last,
	}}
	uc := NewGrowthUsecase(repo)
	uc.SetSessionEndMemoryReviewEnabled(true)
	if err := uc.TrySessionEndMemoryReview(context.Background(), "c2-dedupe"); err != nil {
		t.Fatal(err)
	}
	if repo.state.PendingMemoryReview {
		t.Fatal("should skip session-end pending when last C3 review within dedupe_window")
	}
}

func TestGrowthUsecase_TrySessionEndSkillReview_skipsWhenRecentBackgroundReview(t *testing.T) {
	last := time.Now().Add(-1 * time.Minute)
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:              "c2s-dedupe",
		ToolItersSinceReview:   2,
		LastBackgroundReviewAt: &last,
	}}
	uc := NewGrowthUsecase(repo)
	uc.SetSessionEndSkillReviewEnabled(true)
	if err := uc.TrySessionEndSkillReview(context.Background(), "c2s-dedupe"); err != nil {
		t.Fatal(err)
	}
	if repo.state.PendingSkillReview {
		t.Fatal("should skip session-end skill pending when last C3 review within dedupe_window")
	}
}

func TestGrowthUsecase_TrySessionEndSkillReview_setsPendingWhenEnabled(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:            "c2s",
		ToolItersSinceReview: 2,
	}}
	uc := NewGrowthUsecase(repo)
	uc.SetSessionEndSkillReviewEnabled(true)
	if err := uc.TrySessionEndSkillReview(context.Background(), "c2s"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("expected pending_skill_review from session-end skill review")
	}
}

func TestGrowthUsecase_TrySessionEndSkillReview_skipsWhenDisabled(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:            "c2sb",
		ToolItersSinceReview: 3,
	}}
	uc := NewGrowthUsecase(repo)
	if err := uc.TrySessionEndSkillReview(context.Background(), "c2sb"); err != nil {
		t.Fatal(err)
	}
	if repo.state.PendingSkillReview {
		t.Fatal("should not set pending when C2s disabled")
	}
}

func TestGrowthUsecase_TrySessionEndSkillReview_skipsWhenAlreadyPendingSkill(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:            "c2sc",
		PendingSkillReview:   true,
		ToolItersSinceReview: 2,
	}}
	uc := NewGrowthUsecase(repo)
	uc.SetSessionEndSkillReviewEnabled(true)
	if err := uc.TrySessionEndSkillReview(context.Background(), "c2sc"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("pending_skill should remain true")
	}
}

func TestGrowthUsecase_SessionEndSkillAndMemoryBothPending(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID:              "c2d",
		ToolItersSinceReview:   1,
		TurnsSinceMemoryReview: 1,
	}}
	uc := NewGrowthUsecase(repo)
	uc.SetSessionEndMemoryReviewEnabled(true)
	uc.SetSessionEndSkillReviewEnabled(true)
	if err := uc.TrySessionEndMemoryReview(context.Background(), "c2d"); err != nil {
		t.Fatal(err)
	}
	if err := uc.TrySessionEndSkillReview(context.Background(), "c2d"); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingMemoryReview {
		t.Fatal("expected pending_memory_review")
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("expected pending_skill_review when both C2 and C2s enabled")
	}
}

func TestGrowthUsecase_MarkIdleCheckDone(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "i1"}}
	uc := NewGrowthUsecase(repo)
	if err := uc.MarkIdleCheckDone(context.Background(), "i1"); err != nil {
		t.Fatal(err)
	}
	if repo.state.LastIdleCheckAt == nil {
		t.Fatal("expected LastIdleCheckAt to be set")
	}
}

// --- State machine table-driven tests (O1) ---

// stateMachineCase describes a single state transition test.
type stateMachineCase struct {
	name         string
	initial      ChatGrowthState
	action       string // "OnToolSuccess", "OnAssistantTurn", "ClearGrowthPending", "RecordReviewRunFailure"
	// action-specific args:
	clearSkill  bool
	clearMemory bool
	runErr      string
	errSkill    bool
	errMemory   bool
	// expected outcomes after action:
	wantPendingSkill  bool
	wantPendingMemory bool
	wantLastSkillErr  string
	wantLastMemoryErr string
	wantToolIters     int
	wantTurns         int
	wantReviewFailed  bool // true if ReviewFailedAt should be non-nil
	wantErr           bool // true if action should return an error
}

func (c stateMachineCase) run(t *testing.T) {
	t.Helper()
	repo := &fakeGrowthRepo{}
	cp := c.initial
	repo.state = &cp
	uc := NewGrowthUsecase(repo)
	ctx := context.Background()

	var err error
	switch c.action {
	case "OnToolSuccess":
		err = uc.OnToolSuccess(ctx, c.initial.SessionID)
	case "OnAssistantTurn":
		err = uc.OnAssistantTurn(ctx, c.initial.SessionID)
	case "ClearGrowthPending":
		err = uc.ClearGrowthPending(ctx, c.initial.SessionID, c.clearSkill, c.clearMemory)
	case "RecordReviewRunFailure":
		var runErr error
		if c.runErr != "" {
			runErr = errors.New(c.runErr)
		}
		err = uc.RecordReviewRunFailure(ctx, c.initial.SessionID, runErr, c.errSkill, c.errMemory)
	default:
		t.Fatalf("unknown action: %s", c.action)
	}

	if c.wantErr && err == nil {
		t.Fatal("expected error, got nil")
	}
	if !c.wantErr && err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.wantErr {
		return // don't check state on error path
	}

	st := repo.state
	if st.PendingSkillReview != c.wantPendingSkill {
		t.Errorf("PendingSkillReview: want %v, got %v", c.wantPendingSkill, st.PendingSkillReview)
	}
	if st.PendingMemoryReview != c.wantPendingMemory {
		t.Errorf("PendingMemoryReview: want %v, got %v", c.wantPendingMemory, st.PendingMemoryReview)
	}
	if st.LastSkillError != c.wantLastSkillErr {
		t.Errorf("LastSkillError: want %q, got %q", c.wantLastSkillErr, st.LastSkillError)
	}
	if st.LastMemoryError != c.wantLastMemoryErr {
		t.Errorf("LastMemoryError: want %q, got %q", c.wantLastMemoryErr, st.LastMemoryError)
	}
	if st.ToolItersSinceReview != c.wantToolIters {
		t.Errorf("ToolItersSinceReview: want %d, got %d", c.wantToolIters, st.ToolItersSinceReview)
	}
	if st.TurnsSinceMemoryReview != c.wantTurns {
		t.Errorf("TurnsSinceMemoryReview: want %d, got %d", c.wantTurns, st.TurnsSinceMemoryReview)
	}
	if c.wantReviewFailed && st.ReviewFailedAt == nil {
		t.Error("ReviewFailedAt: want non-nil, got nil")
	}
	if !c.wantReviewFailed && st.ReviewFailedAt != nil {
		t.Error("ReviewFailedAt: want nil, got non-nil")
	}
}

func TestStateMachine_failureDoesNotClearPending(t *testing.T) {
	// spec §7: failure records error but retains pending flags for retry.
	cases := []stateMachineCase{
		{
			name:              "skill failure retains pending_skill",
			initial:           ChatGrowthState{SessionID: "sm1", PendingSkillReview: true, ToolItersSinceReview: 0},
			action:            "RecordReviewRunFailure",
			runErr:            "skill apply failed",
			errSkill:          true,
			errMemory:         false,
			wantPendingSkill:  true, // retained
			wantPendingMemory: false,
			wantLastSkillErr:  "skill apply failed",
			wantLastMemoryErr: "",
			wantToolIters:     0,
			wantReviewFailed:  true,
		},
		{
			name:              "memory failure retains pending_memory",
			initial:           ChatGrowthState{SessionID: "sm2", PendingMemoryReview: true},
			action:            "RecordReviewRunFailure",
			runErr:            "memory refresh failed",
			errSkill:          false,
			errMemory:         true,
			wantPendingSkill:  false,
			wantPendingMemory: true, // retained
			wantLastSkillErr:  "",
			wantLastMemoryErr: "memory refresh failed",
			wantReviewFailed:  true,
		},
		{
			name:              "both failure retains both pendings",
			initial:           ChatGrowthState{SessionID: "sm3", PendingSkillReview: true, PendingMemoryReview: true},
			action:            "RecordReviewRunFailure",
			runErr:            "dual failure",
			errSkill:           true,
			errMemory:          true,
			wantPendingSkill:  true, // retained
			wantPendingMemory: true, // retained
			wantLastSkillErr:  "dual failure",
			wantLastMemoryErr: "dual failure",
			wantReviewFailed:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t) })
	}
}

func TestStateMachine_clearThenRetry_success(t *testing.T) {
	// Full lifecycle: nudge → pending → run fails → retry succeeds → clear.
	t.Run("skill_review_lifecycle", func(t *testing.T) {
		repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "lc1"}}
		uc := NewGrowthUsecase(repo)
		uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true})
		ctx := context.Background()

		// Phase 1: accumulate tool successes until pending
		for i := 0; i < 10; i++ {
			if err := uc.OnToolSuccess(ctx, "lc1"); err != nil {
				t.Fatalf("tool %d: %v", i, err)
			}
		}
		if !repo.state.PendingSkillReview {
			t.Fatal("expected pending after threshold")
		}

		// Phase 2: review fails — pending retained
		if err := uc.RecordReviewRunFailure(ctx, "lc1", errors.New("oops"), true, false); err != nil {
			t.Fatal(err)
		}
		if !repo.state.PendingSkillReview {
			t.Fatal("pending skill should survive failure")
		}
		if repo.state.LastSkillError != "oops" {
			t.Fatalf("error recorded: %q", repo.state.LastSkillError)
		}

		// Phase 3: retry succeeds — clear skill pending
		if err := uc.ClearGrowthPending(ctx, "lc1", true, false); err != nil {
			t.Fatal(err)
		}
		if repo.state.PendingSkillReview {
			t.Fatal("pending skill should be cleared after success")
		}
		if repo.state.LastSkillError != "" {
			t.Fatalf("LastSkillError cleared: %q", repo.state.LastSkillError)
		}
		if repo.state.ReviewFailedAt != nil {
			t.Fatal("ReviewFailedAt should be nil after all pending cleared")
		}
	})
}

func TestStateMachine_counterIncrementsDuringPending(t *testing.T) {
	// Counter continues to accumulate even while pending flags are set
	// (the review hasn't happened yet — tool usage continues).
	t.Run("tool_counter_increments_while_pending", func(t *testing.T) {
		repo := &fakeGrowthRepo{state: &ChatGrowthState{
			SessionID:          "cp1",
			PendingSkillReview: true,
		}}
		uc := NewGrowthUsecase(repo)
		ctx := context.Background()

		if err := uc.OnToolSuccess(ctx, "cp1"); err != nil {
			t.Fatal(err)
		}
		// Pending remains true, counter incremented (will re-trigger at next threshold)
		if !repo.state.PendingSkillReview {
			t.Fatal("pending skill should remain true")
		}
		if repo.state.ToolItersSinceReview != 1 {
			t.Fatalf("tool iters should be 1, got %d", repo.state.ToolItersSinceReview)
		}
	})

	t.Run("turn_counter_increments_while_pending", func(t *testing.T) {
		repo := &fakeGrowthRepo{state: &ChatGrowthState{
			SessionID:           "cp2",
			PendingMemoryReview: true,
		}}
		uc := NewGrowthUsecase(repo)
		ctx := context.Background()

		if err := uc.OnAssistantTurn(ctx, "cp2"); err != nil {
			t.Fatal(err)
		}
		if !repo.state.PendingMemoryReview {
			t.Fatal("pending memory should remain true")
		}
		if repo.state.TurnsSinceMemoryReview != 1 {
			t.Fatalf("turns should be 1, got %d", repo.state.TurnsSinceMemoryReview)
		}
	})
}

func TestStateMachine_partialClearScenarios(t *testing.T) {
	now := time.Now()
	cases := []stateMachineCase{
		{
			name:              "clear only skill when both pending",
			initial:           ChatGrowthState{SessionID: "pc1", PendingSkillReview: true, PendingMemoryReview: true, LastSkillError: "se", LastMemoryError: "me", ReviewFailedAt: &now},
			action:            "ClearGrowthPending",
			clearSkill:        true,
			clearMemory:       false,
			wantPendingSkill:  false,
			wantPendingMemory: true,
			wantLastSkillErr:  "",
			wantLastMemoryErr: "me",
			wantReviewFailed:  true, // still has memory pending
		},
		{
			name:              "clear only memory when both pending",
			initial:           ChatGrowthState{SessionID: "pc2", PendingSkillReview: true, PendingMemoryReview: true, LastSkillError: "se", LastMemoryError: "me", ReviewFailedAt: &now},
			action:            "ClearGrowthPending",
			clearSkill:        false,
			clearMemory:       true,
			wantPendingSkill:  true,
			wantPendingMemory: false,
			wantLastSkillErr:  "se",
			wantLastMemoryErr: "",
			wantReviewFailed:  true, // still has skill pending
		},
		{
			name:              "clear both when both pending clears review_failed_at",
			initial:           ChatGrowthState{SessionID: "pc3", PendingSkillReview: true, PendingMemoryReview: true, LastSkillError: "se", LastMemoryError: "me", ReviewFailedAt: &now},
			action:            "ClearGrowthPending",
			clearSkill:        true,
			clearMemory:       true,
			wantPendingSkill:  false,
			wantPendingMemory: false,
			wantLastSkillErr:  "",
			wantLastMemoryErr: "",
			wantReviewFailed:  false, // all cleared
		},
		{
			name:              "neither flag set is no-op",
			initial:           ChatGrowthState{SessionID: "pc4", PendingSkillReview: true},
			action:            "ClearGrowthPending",
			clearSkill:        false,
			clearMemory:       false,
			wantPendingSkill:  true, // unchanged
			wantPendingMemory: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t) })
	}
}

func TestStateMachine_errorMessageTruncation(t *testing.T) {
	longMsg := ""
	for i := 0; i < 3000; i++ {
		longMsg += "x"
	}
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "tr1", PendingSkillReview: true}}
	uc := NewGrowthUsecase(repo)
	if err := uc.RecordReviewRunFailure(context.Background(), "tr1", errors.New(longMsg), true, false); err != nil {
		t.Fatal(err)
	}
	if len(repo.state.LastSkillError) > 2048 {
		t.Fatalf("error message not truncated: len=%d", len(repo.state.LastSkillError))
	}
}

func TestStateMachine_emptySessionID(t *testing.T) {
	repo := &fakeGrowthRepo{}
	uc := NewGrowthUsecase(repo)
	ctx := context.Background()

	// All operations should be no-ops or return early with empty session ID.
	if err := uc.OnToolSuccess(ctx, ""); err != nil {
		t.Fatalf("OnToolSuccess empty: %v", err)
	}
	if err := uc.OnAssistantTurn(ctx, ""); err != nil {
		t.Fatalf("OnAssistantTurn empty: %v", err)
	}
	if err := uc.ClearGrowthPending(ctx, "", true, true); err != nil {
		t.Fatalf("ClearGrowthPending empty: %v", err)
	}
	if err := uc.RecordReviewRunFailure(ctx, "", errors.New("x"), true, true); err != nil {
		t.Fatalf("RecordReviewRunFailure empty: %v", err)
	}
	if err := uc.MarkIdleCheckDone(ctx, ""); err != nil {
		t.Fatalf("MarkIdleCheckDone empty: %v", err)
	}
}

func TestStateMachine_RecordReviewRunFailure_nilError(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "ne1", PendingSkillReview: true}}
	uc := NewGrowthUsecase(repo)
	// nil error should be no-op
	if err := uc.RecordReviewRunFailure(context.Background(), "ne1", nil, true, false); err != nil {
		t.Fatal(err)
	}
	if repo.state.LastSkillError != "" {
		t.Fatalf("expected no error recorded, got %q", repo.state.LastSkillError)
	}
	if repo.state.ReviewFailedAt != nil {
		t.Fatal("expected no ReviewFailedAt")
	}
}

func TestStateMachine_multipleFailureAccumulates(t *testing.T) {
	// Multiple failures should overwrite error, keep pending.
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "mf1", PendingSkillReview: true}}
	uc := NewGrowthUsecase(repo)
	ctx := context.Background()

	if err := uc.RecordReviewRunFailure(ctx, "mf1", errors.New("first"), true, false); err != nil {
		t.Fatal(err)
	}
	if err := uc.RecordReviewRunFailure(ctx, "mf1", errors.New("second"), true, false); err != nil {
		t.Fatal(err)
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("pending should survive multiple failures")
	}
	if repo.state.LastSkillError != "second" {
		t.Fatalf("last error should be second, got %q", repo.state.LastSkillError)
	}
}

// A5: 重试计数与上限保护。
func TestStateMachine_ReviewRetryCount_increments(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "rc1", PendingSkillReview: true}}
	uc := NewGrowthUsecase(repo)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if err := uc.RecordReviewRunFailure(ctx, "rc1", errors.New("boom"), true, false); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if repo.state.ReviewRetryCount != i {
			t.Fatalf("iter %d retry=%d", i, repo.state.ReviewRetryCount)
		}
	}
}

func TestStateMachine_ClearGrowthPending_resetsRetry(t *testing.T) {
	now := time.Now()
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID: "rc2", PendingSkillReview: true, PendingMemoryReview: true,
		ReviewRetryCount: 4, ReviewFailedAt: &now,
	}}
	uc := NewGrowthUsecase(repo)
	if err := uc.ClearGrowthPending(context.Background(), "rc2", true, true); err != nil {
		t.Fatal(err)
	}
	if repo.state.ReviewRetryCount != 0 {
		t.Fatalf("retry should reset, got %d", repo.state.ReviewRetryCount)
	}
	if repo.state.ReviewFailedAt != nil {
		t.Fatal("ReviewFailedAt should clear when both pendings clear")
	}
}

func TestStateMachine_ClearGrowthPending_partialKeepsRetry(t *testing.T) {
	now := time.Now()
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID: "rc3", PendingSkillReview: true, PendingMemoryReview: true,
		ReviewRetryCount: 2, ReviewFailedAt: &now,
	}}
	uc := NewGrowthUsecase(repo)
	if err := uc.ClearGrowthPending(context.Background(), "rc3", true, false); err != nil {
		t.Fatal(err)
	}
	if repo.state.ReviewRetryCount != 2 {
		t.Fatalf("retry should survive partial clear, got %d", repo.state.ReviewRetryCount)
	}
}

func TestStateMachine_DropPendingAfterMaxRetry(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID: "dr1", PendingSkillReview: true, ReviewRetryCount: 5,
	}}
	uc := NewGrowthUsecase(repo)
	dropped, err := uc.DropPendingAfterMaxRetry(context.Background(), "dr1", 5)
	if err != nil || !dropped {
		t.Fatalf("dropped=%v err=%v", dropped, err)
	}
	if repo.state.PendingSkillReview {
		t.Fatal("pending should be cleared")
	}
	if repo.state.ReviewRetryCount != 0 {
		t.Fatalf("retry should reset, got %d", repo.state.ReviewRetryCount)
	}
}

func TestStateMachine_DropPendingAfterMaxRetry_BelowThreshold(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID: "dr2", PendingSkillReview: true, ReviewRetryCount: 3,
	}}
	uc := NewGrowthUsecase(repo)
	dropped, err := uc.DropPendingAfterMaxRetry(context.Background(), "dr2", 5)
	if err != nil {
		t.Fatal(err)
	}
	if dropped {
		t.Fatal("should not drop when below threshold")
	}
	if !repo.state.PendingSkillReview {
		t.Fatal("pending should survive below threshold")
	}
}

func TestStateMachine_DropPendingAfterMaxRetry_DisabledByZero(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{
		SessionID: "dr3", PendingSkillReview: true, ReviewRetryCount: 100,
	}}
	uc := NewGrowthUsecase(repo)
	dropped, _ := uc.DropPendingAfterMaxRetry(context.Background(), "dr3", 0)
	if dropped {
		t.Fatal("maxRetry<=0 should disable drop")
	}
}

func TestFinalizeTurn_SetsPendingWithoutWake(t *testing.T) {
	woke := 0
	growthwake.Register(func() { woke++ })
	t.Cleanup(func() { growthwake.Register(nil) })

	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "ft1"}}
	uc := NewGrowthUsecase(repo)
	uc.SetBackgroundReviewEnabled(true)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 2, MemoryTurnInterval: 1})

	ctx := context.Background()
	spawnSkill, spawnMemory, err := uc.FinalizeTurnForBackgroundReview(ctx, "ft1", "req-1", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if !spawnSkill {
		t.Fatal("expected spawnSkill after 2 tool successes (interval=2)")
	}
	if !spawnMemory {
		t.Fatal("expected spawnMemory after assistant turn (interval=1)")
	}
	if !repo.state.PendingSkillReview || !repo.state.PendingMemoryReview {
		t.Fatalf("pending flags want both true, got skill=%v memory=%v", repo.state.PendingSkillReview, repo.state.PendingMemoryReview)
	}
	if woke != 0 {
		t.Fatalf("FinalizeTurn must not Wake, woke=%d", woke)
	}

	// Pre-existing pending still reported even when this turn adds no new threshold cross.
	spawnSkill, spawnMemory, err = uc.FinalizeTurnForBackgroundReview(ctx, "ft1", "req-2", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !spawnSkill || !spawnMemory {
		t.Fatal("spawn* must reflect pre-existing pending after no-op turn")
	}
	if woke != 0 {
		t.Fatalf("second FinalizeTurn must not Wake, woke=%d", woke)
	}
}

func TestFinalizeTurn_DisabledNoOp(t *testing.T) {
	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "ft0", PendingSkillReview: true}}
	uc := NewGrowthUsecase(repo) // C3 off by default
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 1, MemoryTurnInterval: 1})

	spawnSkill, spawnMemory, err := uc.FinalizeTurnForBackgroundReview(context.Background(), "ft0", "r", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if spawnSkill || spawnMemory {
		t.Fatal("C3 disabled FinalizeTurn must no-op return false,false")
	}
	if repo.state.ToolItersSinceReview != 0 {
		t.Fatalf("counters must not change when C3 off, got tool_iters=%d", repo.state.ToolItersSinceReview)
	}
}

func TestOnToolSuccess_NoDoubleCountWhenC3Enabled(t *testing.T) {
	woke := 0
	growthwake.Register(func() { woke++ })
	t.Cleanup(func() { growthwake.Register(nil) })

	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "dc1"}}
	uc := NewGrowthUsecase(repo)
	uc.SetBackgroundReviewEnabled(true)
	uc.SetNudgeConfig(growth.NudgeConfig{Enabled: true, SkillToolInterval: 3, MemoryTurnInterval: 2})
	ctx := context.Background()

	// Legacy hooks must not bump counters or Wake when C3 on.
	for i := 0; i < 5; i++ {
		if err := uc.OnToolSuccess(ctx, "dc1"); err != nil {
			t.Fatal(err)
		}
		if err := uc.OnAssistantTurn(ctx, "dc1"); err != nil {
			t.Fatal(err)
		}
	}
	if repo.state.ToolItersSinceReview != 0 || repo.state.TurnsSinceMemoryReview != 0 {
		t.Fatalf("hooks must no-op under C3, got tool=%d turns=%d", repo.state.ToolItersSinceReview, repo.state.TurnsSinceMemoryReview)
	}
	if repo.state.PendingSkillReview || repo.state.PendingMemoryReview {
		t.Fatal("hooks must not set pending under C3")
	}
	if woke != 0 {
		t.Fatalf("hooks must not Wake under C3, woke=%d", woke)
	}

	// Sole writer: FinalizeTurn applies this turn's successes once.
	spawnSkill, spawnMemory, err := uc.FinalizeTurnForBackgroundReview(ctx, "dc1", "req-dc", 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if !spawnSkill {
		t.Fatal("FinalizeTurn should set pending_skill at interval 3")
	}
	if spawnMemory {
		t.Fatal("one assistant turn with memory interval 2 should not yet pending_memory")
	}
	if repo.state.ToolItersSinceReview != 0 {
		t.Fatalf("skill counter should reset after pending, got %d", repo.state.ToolItersSinceReview)
	}
	if repo.state.TurnsSinceMemoryReview != 1 {
		t.Fatalf("memory counter want 1 got %d", repo.state.TurnsSinceMemoryReview)
	}
	if woke != 0 {
		t.Fatalf("FinalizeTurn must not Wake, woke=%d", woke)
	}

	// Calling OnToolSuccess again must still not double-count.
	_ = uc.OnToolSuccess(ctx, "dc1")
	if repo.state.ToolItersSinceReview != 0 {
		t.Fatal("OnToolSuccess after FinalizeTurn must not bump counter under C3")
	}
}