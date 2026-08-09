package growth

import (
	"context"
	"testing"
)

// TestAgentReview_fallsBackToPatchOnAgentError 端到端验证 NewRunner 选出的 AgentReviewRunner
// 在 SpawnReviewAgent 失败/超时时降级到单次 LLM patch 路径（ProposeSkillPatches）。
func TestAgentReview_fallsBackToPatchOnAgentError(t *testing.T) {
	ws := t.TempDir()
	fallbackHit := false
	deps := RunnerDeps{
		Transcript: func(ctx context.Context, _ string) (string, error) { return "t", nil },
		SpawnReviewAgent: func(ctx context.Context, _ ReviewJob, _, _ string) error {
			return context.DeadlineExceeded // 模拟 agent 失败/超时
		},
		ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) {
			fallbackHit = true
			return nil, nil // 空 patch，ApplyPatchBatch 应接受
		},
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := NewRunner(
		RunnerSelect{LLMReviewEnabled: true, AgentReviewEnabled: true},
		deps,
	)
	if _, ok := r.(*AgentReviewRunner); !ok {
		t.Fatalf("expected *AgentReviewRunner, got %T", r)
	}
	job := ReviewJob{SessionID: "s1", WorkspaceRoot: ws, PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !fallbackHit {
		t.Fatal("expected fallback to single-shot LLM patch on agent failure")
	}
}
