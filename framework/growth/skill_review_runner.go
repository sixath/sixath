package growth

import (
	"context"
	"fmt"
)

// SkillReviewRunner 二期技能环：transcript + 技能索引摘要 → ProposeSkillPatches → Validate+Apply → 按位清 pending。
// 若未来在本 runner 内调用 agent.Run/ReAct，必须为 Request.Metadata 注入 MetaGrowthReview（portal 可用 chat.MergeGrowthReviewMetadata）（spec §4.3）。
type SkillReviewRunner struct {
	deps RunnerDeps
}

// Run 执行技能批（若 PendingSkill）与记忆分支（若 PendingMemory）。
// 当 PendingSkill+PendingMemory 同时存在且 ProposeCombinedReview 非空时，合并为单次 LLM 调用。
func (r *SkillReviewRunner) Run(ctx context.Context, job ReviewJob) error {
	if r == nil {
		return nil
	}
	deps := r.deps
	if deps.ClearGrowthPending == nil {
		return nil
	}
	workspace := job.WorkspaceRoot
	if workspace == "" {
		workspace = job.WorkspaceKey
	}

	both := job.PendingSkill && job.PendingMemory
	if both && deps.ProposeCombinedReview != nil {
		if err := r.runCombined(ctx, job, deps, workspace); err != nil {
			// L2 降级：合并 LLM 失败时回退为技能 patch + 记忆两次独立路径。
			return r.runSequential(ctx, job, deps, workspace)
		}
		return nil
	}

	return r.runSequential(ctx, job, deps, workspace)
}

func (r *SkillReviewRunner) runSequential(ctx context.Context, job ReviewJob, deps RunnerDeps, workspace string) error {
	if job.PendingSkill && deps.ProposeSkillPatches != nil {
		if err := r.runSkill(ctx, job, deps, workspace); err != nil {
			return err
		}
	}

	if job.PendingMemory && deps.MemoryNotify != nil {
		deps.MemoryNotify(ctx, job.SessionID)
	}
	if job.PendingMemory {
		if err := deps.ClearGrowthPending(ctx, job.SessionID, false, true); err != nil {
			return err
		}
	}

	return nil
}

func (r *SkillReviewRunner) runSkill(ctx context.Context, job ReviewJob, deps RunnerDeps, workspace string) error {
	if workspace == "" {
		return fmt.Errorf("growth: skill review requires workspace root")
	}
	transcript, err := fetchReviewTranscript(ctx, job, deps)
	if err != nil {
		return err
	}
	idx, err := buildReviewIndex(workspace)
	if err != nil {
		return err
	}
	summary := buildReviewSummary(ctx, job, idx, deps)
	batch, err := deps.ProposeSkillPatches(ctx, job, transcript, summary)
	if err != nil {
		return err
	}
	if err := ApplyPatchBatch(workspace, batch); err != nil {
		return err
	}
	DefaultSkillsIndexTracker.Bump(workspace)
	if deps.InvalidateSkillsCache != nil {
		deps.InvalidateSkillsCache(ctx, workspace)
	}
	if err := rewriteCronSkillRefs(ctx, deps, workspace, batch); err != nil {
		return err
	}
	return deps.ClearGrowthPending(ctx, job.SessionID, true, false)
}

func (r *SkillReviewRunner) runCombined(ctx context.Context, job ReviewJob, deps RunnerDeps, workspace string) error {
	if workspace == "" {
		return fmt.Errorf("growth: combined review requires workspace root")
	}
	transcript, err := fetchReviewTranscript(ctx, job, deps)
	if err != nil {
		return err
	}
	idx, err := buildReviewIndex(workspace)
	if err != nil {
		return err
	}
	summary := buildReviewSummary(ctx, job, idx, deps)
	patches, notifyMemory, err := deps.ProposeCombinedReview(ctx, job, transcript, summary)
	if err != nil {
		return err
	}
	if err := ApplyPatchBatch(workspace, patches); err != nil {
		return err
	}
	DefaultSkillsIndexTracker.Bump(workspace)
	if deps.InvalidateSkillsCache != nil {
		deps.InvalidateSkillsCache(ctx, workspace)
	}
	if err := rewriteCronSkillRefs(ctx, deps, workspace, patches); err != nil {
		return err
	}
	if err := deps.ClearGrowthPending(ctx, job.SessionID, true, false); err != nil {
		return err
	}
	if notifyMemory && deps.MemoryNotify != nil {
		deps.MemoryNotify(ctx, job.SessionID)
	}
	return deps.ClearGrowthPending(ctx, job.SessionID, false, true)
}

func rewriteCronSkillRefs(ctx context.Context, deps RunnerDeps, workspace string, patches []Patch) error {
	if deps.RewriteCronSkillRefs == nil {
		return nil
	}
	renames := ExtractSkillRenamesFromPatches(patches)
	if len(renames) == 0 {
		return nil
	}
	return deps.RewriteCronSkillRefs(ctx, workspace, renames)
}
