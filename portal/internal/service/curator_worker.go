package service

import (
	"context"
	"os"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/conf"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/sixath/framework/growth"
	"google.golang.org/protobuf/types/known/durationpb"
)

const defaultCuratorPollInterval = 1 * time.Hour

// CuratorWorker R2b/R2c：按 workspace 周期运行 Curator（复用 growth 租约；改名后反写 cron）。
type CuratorWorker struct {
	log          *log.Helper
	holderID     string
	pollInterval time.Duration
	interval     time.Duration
	minSkills    int
	curatorUC    *biz.CuratorUsecase
	runner       *growth.CuratorRunner
}

// NewCuratorWorker 构造 Curator worker；curator_enabled=false 或依赖缺失时返回 nil。
func NewCuratorWorker(
	logger log.Logger,
	curatorUC *biz.CuratorUsecase,
	cronRefUC *biz.CronRefRewriteUsecase,
	growthCfg *conf.Growth,
	llmReviewEnabled bool,
	curatorPatchFile string,
) *CuratorWorker {
	if curatorUC == nil || growthCfg == nil || !growthCfg.GetCuratorEnabled() {
		return nil
	}
	helper := log.NewHelper(logger)
	def := growth.NewCuratorDefaults()
	interval := def.Interval
	if d := growthDuration(growthCfg, func(g *conf.Growth) *durationpb.Duration {
		return g.GetCuratorInterval()
	}, time.Hour, 90*24*time.Hour); d > 0 {
		interval = d
	}
	minSkills := int(def.MinSkills)
	if growthCfg.GetCuratorMinSkills() > 0 {
		minSkills = int(growthCfg.GetCuratorMinSkills())
	}
	poll := defaultCuratorPollInterval
	if d := growthDuration(growthCfg, func(g *conf.Growth) *durationpb.Duration {
		return g.GetCuratorPollInterval()
	}, 5*time.Minute, 24*time.Hour); d > 0 {
		poll = d
	}

	deps := growth.CuratorDeps{
		InvalidateSkillsCache: func(ctx context.Context, workspace string) {
			gen := growth.DefaultSkillsIndexTracker.Generation(workspace)
			helper.Infof("curator skills cache invalidated workspace=%s generation=%d", workspace, gen)
		},
		RewriteCronSkillRefs: newCronSkillRefRewriter(cronRefUC, helper),
	}

	if growthCfg.GetCuratorLlmEnabled() && growthCfg.GetLlm() != nil && strings.TrimSpace(growthCfg.GetLlm().GetModel()) != "" {
		llmCfg := growthCfg.GetLlm()
		client, err := newGrowthModelClient(llmCfg)
		if err != nil {
			helper.Warnf("curator: LLM client failed, falling back to patch file: %v", err)
		} else {
			runnerCfg := growth.LLMRunnerConfig{
				SystemPrompt: llmCfg.GetCuratorSystemPrompt(),
			}
			deps.ProposeCuratorPatches = growth.NewLLMCuratorProposer(client, runnerCfg)
		}
	}
	if deps.ProposeCuratorPatches == nil {
		deps.ProposeCuratorPatches = curatorProposerFromFile(curatorPatchFile)
	}
	if deps.ProposeCuratorPatches == nil && llmReviewEnabled {
		// 与 growth 共用 review_patch_file 便于联调（当 curator 未单独配置时）
		deps.ProposeCuratorPatches = curatorProposerFromFile(strings.TrimSpace(growthCfg.GetReviewPatchFile()))
	}
	if deps.ProposeCuratorPatches == nil {
		helper.Warn("curator: no proposer configured (enable curator_llm or curator_patch_file)")
		return nil
	}

	return &CuratorWorker{
		log:          helper,
		holderID:     uuid.NewString(),
		pollInterval: poll,
		interval:     interval,
		minSkills:    minSkills,
		curatorUC:    curatorUC,
		runner:       growth.NewCuratorRunner(deps),
	}
}

// Loop 阻塞直至 ctx 取消。
func (w *CuratorWorker) Loop(ctx context.Context) {
	if w == nil {
		return
	}
	w.pollOnce(context.Background())
	t := time.NewTicker(w.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.pollOnce(context.Background())
		}
	}
}

func (w *CuratorWorker) pollOnce(ctx context.Context) {
	rows, err := w.curatorUC.ListWorkspaces(ctx, 200)
	if err != nil {
		w.log.Warnf("curator list workspaces: %v", err)
		return
	}
	leaseTTL := growth.NewDefaults().LeaseTTL
	for _, row := range rows {
		if row.WorkspaceKey == "" {
			continue
		}
		due, err := w.curatorUC.IsDue(ctx, row.WorkspaceKey, w.interval)
		if err != nil || !due {
			continue
		}
		w.runWorkspace(ctx, row, leaseTTL)
	}
}

func (w *CuratorWorker) runWorkspace(ctx context.Context, row biz.CuratorWorkspace, leaseTTL time.Duration) {
	acquired, err := w.curatorUC.TryAcquireWorkspaceLease(ctx, row.WorkspaceKey, w.holderID, leaseTTL)
	if err != nil {
		w.log.Warnf("curator lease acquire workspace=%s err=%v", row.WorkspaceKey, err)
		growth.DefaultMetrics.IncLeaseAcquireErr()
		return
	}
	if !acquired {
		growth.DefaultMetrics.IncLeaseContention()
		return
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = w.curatorUC.ReleaseWorkspaceLease(rctx, row.WorkspaceKey, w.holderID)
	}()

	job := growth.CuratorJob{
		WorkspaceKey:  row.WorkspaceKey,
		WorkspaceRoot: row.WorkspaceKey,
		AgentID:       row.AgentID,
	}
	runErr := w.runner.Run(ctx, job, w.minSkills)
	if runErr != nil {
		w.log.Warnf("curator workspace=%s err=%v", row.WorkspaceKey, runErr)
		_ = w.curatorUC.RecordCuratorFailure(ctx, row.WorkspaceKey, runErr)
		growth.DefaultMetrics.IncCuratorFailed()
		return
	}
	_ = w.curatorUC.MarkCuratorDone(ctx, row.WorkspaceKey)
	growth.DefaultMetrics.IncCuratorRun()
}

func curatorProposerFromFile(absPath string) func(ctx context.Context, job growth.CuratorJob, skillsSummary string) ([]growth.Patch, error) {
	if absPath == "" {
		return nil
	}
	return func(ctx context.Context, job growth.CuratorJob, skillsSummary string) ([]growth.Patch, error) {
		_ = ctx
		_ = job
		_ = skillsSummary
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		return growth.ParsePatchBatchJSON(data)
	}
}
