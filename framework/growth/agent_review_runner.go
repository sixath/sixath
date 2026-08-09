package growth

import (
	"context"
	"fmt"
)

// AgentReviewRunner fork-agent 复盘路径（spec §4.2）：
// PendingSkill 时组装上下文交由 deps.SpawnReviewAgent 自主演化技能库；失败/超时降级到 SkillReviewRunner。
// PendingMemory 分支复用 SkillReviewRunner 的记忆通知语义。
type AgentReviewRunner struct {
	deps     RunnerDeps
	fallback *SkillReviewRunner
}

func (r *AgentReviewRunner) Run(ctx context.Context, job ReviewJob) error {
	if r == nil {
		return nil
	}
	// 技能分支走 fork-agent；成功后由 SpawnReviewAgent 内部完成 ClearGrowthPending(skill)。
	if job.PendingSkill && r.deps.SpawnReviewAgent != nil {
		if err := r.runAgentSkill(ctx, job); err != nil {
			// 降级：fork-agent 失败时回退单次 LLM patch（仅技能分支）。
			if r.fallback != nil {
				return r.fallback.runSkill(ctx, job, r.deps, workspaceOf(job))
			}
			return err
		}
	} else if job.PendingSkill && r.fallback != nil {
		// SpawnReviewAgent 未注入：直接单次 LLM。
		if err := r.fallback.runSkill(ctx, job, r.deps, workspaceOf(job)); err != nil {
			return err
		}
	}

	// 记忆分支：与 SkillReviewRunner 一致。
	if job.PendingMemory && r.deps.MemoryNotify != nil {
		r.deps.MemoryNotify(ctx, job.SessionID)
	}
	if job.PendingMemory && r.deps.ClearGrowthPending != nil {
		if err := r.deps.ClearGrowthPending(ctx, job.SessionID, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (r *AgentReviewRunner) runAgentSkill(ctx context.Context, job ReviewJob) error {
	workspace := workspaceOf(job)
	if workspace == "" {
		return fmt.Errorf("growth: agent review requires workspace root")
	}
	transcript, err := fetchReviewTranscript(ctx, job, r.deps)
	if err != nil {
		return err
	}
	idx, err := buildReviewIndex(workspace)
	if err != nil {
		return err
	}
	summary := buildReviewSummary(ctx, job, idx, r.deps)
	return r.deps.SpawnReviewAgent(ctx, job, transcript, summary)
}

// workspaceOf 优先 WorkspaceRoot，退回 WorkspaceKey。
func workspaceOf(job ReviewJob) string {
	if job.WorkspaceRoot != "" {
		return job.WorkspaceRoot
	}
	return job.WorkspaceKey
}
