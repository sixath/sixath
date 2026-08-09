package growth

import (
	"context"
	"testing"
)

func TestNewRunner_stubPath(t *testing.T) {
	var cleared string
	r := NewRunner(RunnerSelect{}, RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			_ = ctx
			if !cs || !cm {
				t.Fatalf("stub wants full clear, got skill=%v mem=%v", cs, cm)
			}
			cleared = sessionID
			return nil
		},
	})
	_, ok := r.(*StubRunner)
	if !ok {
		t.Fatalf("expected *StubRunner, got %T", r)
	}
	if err := r.Run(context.Background(), ReviewJob{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if cleared != "s1" {
		t.Fatalf("clear pending: got %q", cleared)
	}
}

func TestNewRunner_noopLLMDelegatesToStub(t *testing.T) {
	var cleared string
	r := NewRunner(RunnerSelect{LLMReviewEnabled: true}, RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			_ = ctx
			if !cs || !cm {
				t.Fatalf("noop wants full clear, got skill=%v mem=%v", cs, cm)
			}
			cleared = sessionID
			return nil
		},
	})
	n, ok := r.(*NoopLLMRunner)
	if !ok {
		t.Fatalf("expected *NoopLLMRunner, got %T", r)
	}
	if n.stub == nil {
		t.Fatal("expected non-nil inner stub")
	}
	if err := r.Run(context.Background(), ReviewJob{SessionID: "s2"}); err != nil {
		t.Fatal(err)
	}
	if cleared != "s2" {
		t.Fatalf("clear pending: got %q", cleared)
	}
}

func TestNewRunner_skillReviewRunnerWhenProposerSet(t *testing.T) {
	r := NewRunner(RunnerSelect{LLMReviewEnabled: true}, RunnerDeps{
		ClearGrowthPending: func(ctx context.Context, sessionID string, _, _ bool) error { return nil },
		ProposeSkillPatches: func(ctx context.Context, job ReviewJob, _, _ string) ([]Patch, error) {
			return nil, nil
		},
	})
	if _, ok := r.(*SkillReviewRunner); !ok {
		t.Fatalf("expected *SkillReviewRunner, got %T", r)
	}
}

func TestNewRunner_memoryNotifyStubOnly(t *testing.T) {
	var memSession string
	r := NewRunner(RunnerSelect{}, RunnerDeps{
		MemoryNotify: func(ctx context.Context, sessionID string) {
			memSession = sessionID
		},
		ClearGrowthPending: func(ctx context.Context, sessionID string, cs, cm bool) error {
			return nil
		},
	})
	_ = r.Run(context.Background(), ReviewJob{SessionID: "m1", PendingMemory: true})
	if memSession != "m1" {
		t.Fatalf("memory notify: got %q", memSession)
	}
}

func TestNewRunner_agentReviewRunnerWhenSpawnSet(t *testing.T) {
	r := NewRunner(
		RunnerSelect{LLMReviewEnabled: true, AgentReviewEnabled: true},
		RunnerDeps{
			ClearGrowthPending:  func(ctx context.Context, _ string, _, _ bool) error { return nil },
			ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) { return nil, nil },
			SpawnReviewAgent:    func(ctx context.Context, _ ReviewJob, _, _ string) error { return nil },
		},
	)
	if _, ok := r.(*AgentReviewRunner); !ok {
		t.Fatalf("expected *AgentReviewRunner, got %T", r)
	}
}

func TestNewRunner_fallsBackWhenAgentEnabledButNoSpawn(t *testing.T) {
	r := NewRunner(
		RunnerSelect{LLMReviewEnabled: true, AgentReviewEnabled: true},
		RunnerDeps{
			ClearGrowthPending:  func(ctx context.Context, _ string, _, _ bool) error { return nil },
			ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) { return nil, nil },
		},
	)
	if _, ok := r.(*SkillReviewRunner); !ok {
		t.Fatalf("expected *SkillReviewRunner fallback, got %T", r)
	}
}
