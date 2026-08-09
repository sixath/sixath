package growth

import (
	"context"
	"errors"
	"testing"
)

func TestAgentReviewRunner_success_noFallback(t *testing.T) {
	spawnCalled := false
	fallbackCalled := false
	deps := RunnerDeps{
		Transcript: func(ctx context.Context, _ string) (string, error) { return "t", nil },
		SpawnReviewAgent: func(ctx context.Context, _ ReviewJob, tr, _ string) error {
			spawnCalled = true
			if tr != "t" {
				t.Fatalf("transcript not passed through: %q", tr)
			}
			return nil
		},
		ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) {
			fallbackCalled = true
			return nil, nil
		},
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := &AgentReviewRunner{deps: deps, fallback: &SkillReviewRunner{deps: deps}}
	job := ReviewJob{SessionID: "s1", WorkspaceRoot: t.TempDir(), PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !spawnCalled {
		t.Fatal("SpawnReviewAgent not called")
	}
	if fallbackCalled {
		t.Fatal("fallback should not run on success")
	}
}

func TestAgentReviewRunner_fallbackOnError(t *testing.T) {
	fallbackCalled := false
	deps := RunnerDeps{
		Transcript:       func(ctx context.Context, _ string) (string, error) { return "t", nil },
		SpawnReviewAgent: func(ctx context.Context, _ ReviewJob, _, _ string) error { return errors.New("boom") },
		ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) {
			fallbackCalled = true
			return nil, nil
		},
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := &AgentReviewRunner{deps: deps, fallback: &SkillReviewRunner{deps: deps}}
	job := ReviewJob{SessionID: "s1", WorkspaceRoot: t.TempDir(), PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !fallbackCalled {
		t.Fatal("fallback should run when SpawnReviewAgent errors")
	}
}

func TestAgentReviewRunner_memoryOnlyUsesFallback(t *testing.T) {
	spawnCalled := false
	memNotified := false
	deps := RunnerDeps{
		SpawnReviewAgent:   func(ctx context.Context, _ ReviewJob, _, _ string) error { spawnCalled = true; return nil },
		MemoryNotify:       func(ctx context.Context, _ string) { memNotified = true },
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := &AgentReviewRunner{deps: deps, fallback: &SkillReviewRunner{deps: deps}}
	job := ReviewJob{SessionID: "s1", PendingMemory: true} // 无 PendingSkill
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if spawnCalled {
		t.Fatal("memory-only job must not spawn agent")
	}
	if !memNotified {
		t.Fatal("memory branch should notify memory")
	}
}
