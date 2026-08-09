package growth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sixath/framework/skills"
)

// CuratorJob 描述一次 workspace 级 Curator 清扫（R2b 文件系统 + 可选 R2c cron 反写）。
type CuratorJob struct {
	WorkspaceKey  string
	WorkspaceRoot string
	// AgentID 可选，仅用于日志/指标。
	AgentID string
}

// CronSkillRefRewriter 在技能 frontmatter 改名后反写 cron skill_execute 引用（R2c）。
type CronSkillRefRewriter func(ctx context.Context, workspaceKey string, renames map[string]string) error

// CuratorDeps portal 注入：补丁提议 + 可选缓存失效（与 SkillReviewRunner 一致）。
type CuratorDeps struct {
	ProposeCuratorPatches   func(ctx context.Context, job CuratorJob, skillsSummary string) ([]Patch, error)
	InvalidateSkillsCache   func(ctx context.Context, workspace string)
	RewriteCronSkillRefs    CronSkillRefRewriter
}

// CuratorRunner 在 workspace 租约保护下执行技能合并/归档类 patch（spec R2b）。
type CuratorRunner struct {
	deps CuratorDeps
}

// NewCuratorRunner 构造 CuratorRunner。
func NewCuratorRunner(deps CuratorDeps) *CuratorRunner {
	return &CuratorRunner{deps: deps}
}

// CuratorDefaults R2b 门控默认值（portal 可用 YAML 覆盖）。
type CuratorDefaults struct {
	// Interval 两次 Curator 之间的最小间隔（按 workspace）。
	Interval time.Duration
	// MinSkills 索引中至少有多少技能才触发（避免空 workspace 调 LLM）。
	MinSkills int
}

// NewCuratorDefaults 默认 7 天、至少 2 个技能（合并语义上需要 ≥2）。
func NewCuratorDefaults() CuratorDefaults {
	return CuratorDefaults{
		Interval:  7 * 24 * time.Hour,
		MinSkills: 2,
	}
}

// Run 构建技能索引 → 提议 patch → 校验写盘 → bump generation。
func (r *CuratorRunner) Run(ctx context.Context, job CuratorJob, minSkills int) error {
	if r == nil || r.deps.ProposeCuratorPatches == nil {
		return nil
	}
	workspace := job.WorkspaceRoot
	if workspace == "" {
		workspace = job.WorkspaceKey
	}
	if workspace == "" {
		return fmt.Errorf("growth curator: empty workspace")
	}
	idx, err := r.buildIndex(workspace)
	if err != nil {
		return err
	}
	n := 0
	if idx != nil {
		n = len(idx.All())
	}
	if minSkills > 0 && n < minSkills {
		return nil
	}
	summary := FormatSkillsIndexSnapshot(idx, 128, 200)
	patches, err := r.deps.ProposeCuratorPatches(ctx, job, summary)
	if err != nil {
		return err
	}
	if len(patches) == 0 {
		return nil
	}
	if err := ApplyPatchBatch(workspace, patches); err != nil {
		return err
	}
	DefaultSkillsIndexTracker.Bump(workspace)
	if r.deps.InvalidateSkillsCache != nil {
		r.deps.InvalidateSkillsCache(ctx, workspace)
	}
	if err := r.rewriteCronRefs(ctx, workspace, patches); err != nil {
		return err
	}
	return nil
}

func (r *CuratorRunner) rewriteCronRefs(ctx context.Context, workspace string, patches []Patch) error {
	if r == nil || r.deps.RewriteCronSkillRefs == nil {
		return nil
	}
	renames := ExtractSkillRenamesFromPatches(patches)
	if len(renames) == 0 {
		return nil
	}
	return r.deps.RewriteCronSkillRefs(ctx, workspace, renames)
}

func (r *CuratorRunner) buildIndex(workspace string) (*skills.Index, error) {
	skillsDir := filepath.Join(workspace, "skills")
	if st, err := os.Stat(skillsDir); err == nil && st.IsDir() {
		idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("growth curator: skills index: %w", err)
		}
		return idx, nil
	}
	return skills.NewIndex(nil, nil, nil)
}
