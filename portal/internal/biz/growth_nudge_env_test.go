package biz

import (
	"context"
	"testing"

	"github.com/sixath/framework/growth"
)

func TestProvideGrowthUsecase_nudgeFromEnv_customInterval(t *testing.T) {
	t.Setenv("SATH_BACKGROUND_REVIEW", "false") // keep legacy OnToolSuccess path for nudge assertions
	t.Setenv("SATH_GROWTH_NUDGE_ENABLED", "true")
	t.Setenv("SATH_GROWTH_NUDGE_SKILL_TOOL_INTERVAL", "2")
	t.Setenv("SATH_GROWTH_NUDGE_MEMORY_TURN_INTERVAL", "0")

	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "pe1"}}
	uc := ProvideGrowthUsecase(repo, nil)
	ctx := context.Background()
	_ = uc.OnToolSuccess(ctx, "pe1")
	if repo.state.PendingSkillReview {
		t.Fatal("pending too early")
	}
	_ = uc.OnToolSuccess(ctx, "pe1")
	if !repo.state.PendingSkillReview {
		t.Fatal("env SkillToolInterval=2 should pending on 2nd success")
	}
}

func TestProvideGrowthUsecase_nudgeFromEnv_disabled(t *testing.T) {
	t.Setenv("SATH_BACKGROUND_REVIEW", "false")
	t.Setenv("SATH_GROWTH_NUDGE_ENABLED", "false")
	t.Setenv("SATH_GROWTH_NUDGE_SKILL_TOOL_INTERVAL", "2")

	repo := &fakeGrowthRepo{state: &ChatGrowthState{SessionID: "pe2"}}
	uc := ProvideGrowthUsecase(repo, nil)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = uc.OnToolSuccess(ctx, "pe2")
	}
	if repo.state.PendingSkillReview {
		t.Fatal("SATH_GROWTH_NUDGE_ENABLED=false must not set pending")
	}
}

func TestNudgeConfigFromEnv_defaultsWhenUnset(t *testing.T) {
	t.Setenv("SATH_GROWTH_NUDGE_ENABLED", "")
	t.Setenv("SATH_GROWTH_NUDGE_SKILL_TOOL_INTERVAL", "")
	t.Setenv("SATH_GROWTH_NUDGE_MEMORY_TURN_INTERVAL", "")
	// Clear may leave empty; ensure Default
	n := nudgeConfigFromEnv()
	def := growth.DefaultNudgeConfig()
	if n.Enabled != def.Enabled {
		t.Fatalf("Enabled want %v got %v", def.Enabled, n.Enabled)
	}
	if n.SkillToolInterval != 0 || n.MemoryTurnInterval != 0 {
		t.Fatalf("intervals want 0,0 got %d,%d", n.SkillToolInterval, n.MemoryTurnInterval)
	}
}

func TestBackgroundReviewEnabledFromEnv_defaultFalse(t *testing.T) {
	t.Setenv("SATH_BACKGROUND_REVIEW", "")
	if backgroundReviewEnabledFromEnv() {
		t.Fatal("unset SATH_BACKGROUND_REVIEW must default false (P4: growth off default path)")
	}
}

func TestBackgroundReviewEnabledFromEnv_false(t *testing.T) {
	t.Setenv("SATH_BACKGROUND_REVIEW", "false")
	if backgroundReviewEnabledFromEnv() {
		t.Fatal("SATH_BACKGROUND_REVIEW=false must disable C3")
	}
}

func TestProvideGrowthUsecase_backgroundReviewFromEnv(t *testing.T) {
	t.Setenv("SATH_BACKGROUND_REVIEW", "")
	uc := ProvideGrowthUsecase(&fakeGrowthRepo{state: &ChatGrowthState{SessionID: "br1"}}, nil)
	if uc.BackgroundReviewEnabled() {
		t.Fatal("ProvideGrowthUsecase must disable C3 when SATH_BACKGROUND_REVIEW unset")
	}
	t.Setenv("SATH_BACKGROUND_REVIEW", "1")
	uc2 := ProvideGrowthUsecase(&fakeGrowthRepo{state: &ChatGrowthState{SessionID: "br2"}}, nil)
	if !uc2.BackgroundReviewEnabled() {
		t.Fatal("ProvideGrowthUsecase must honor SATH_BACKGROUND_REVIEW=1")
	}
}
