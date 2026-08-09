package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/conf"
	"backend/internal/growthwake"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/turntrace"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	defaultGrowthPollInterval = 45 * time.Second
	defaultIdleSweepInterval  = 10 * time.Minute
	defaultGrowthMaxRetry     = 5
)

// GrowthWorker 轮询 pending 复盘、抢 workspace 租约、跑 growth.NewRunner 并发布 growth 事件。
type GrowthWorker struct {
	log                    *log.Helper
	holderID               string
	servicePrincipalUserID string
	pollInterval           time.Duration
	idleInterval           time.Duration
	idleCheckInterval      time.Duration
	growthUC               *biz.GrowthUsecase
	growthCfg              *conf.Growth
	turnTraces             turntrace.Store
	runner                 growth.Runner
	// reviewModel 为 fork-agent 复盘路径使用的模型（spawnReviewAgent）；
	// 由装配阶段注入（下一个任务 wiring）。为 nil 时 spawnReviewAgent 返回 errNoReviewModel。
	reviewModel model.Model
	// memoryNotify mirrors RunnerDeps.MemoryNotify for C3 memory-only / combined paths.
	memoryNotify func(ctx context.Context, sessionID string)
	// cronRewrite 与 RunnerDeps.RewriteCronSkillRefs 同源；fork-agent 1:1 改名后直接调用。
	cronRewrite growth.CronSkillRefRewriter
	wake        chan struct{}
	// maxRetry 失败次数阈值（spec phase2 A5）；<=0 时不主动清 pending。
	maxRetry int
	// retryBackoff 记录每个 session 最近一次本地跳过时间，用于指数退避（不依赖 DB）。
	backoffMu sync.Mutex
	backoffAt map[string]time.Time
}

// NewGrowthWorker 构造后台 worker；任一依赖为 nil 时返回 nil（wire 仍可提供非 nil UC，此处防御）。
//
// growthCfg 为 nil 时使用零值：相当于 stub runner。
// llmReviewEnabled 与 reviewPatchFile 与一期入口兼容；growthCfg 提供二期 L1/L2/A5 扩展（combined / 真 LLM / 重试阈值）。
// turnTraces 可选：非 nil 且 async_include_turn_traces 时，异步复盘 transcript 附加 TraceDigest。
func NewGrowthWorker(
	logger log.Logger,
	chatUC *biz.ChatUsecase,
	agentUC *biz.AgentUsecase,
	growthUC *biz.GrowthUsecase,
	cronRefUC *biz.CronRefRewriteUsecase,
	llmReviewEnabled bool,
	reviewPatchFile string,
	growthCfg *conf.Growth,
	auth *conf.Auth,
	turnTraces turntrace.Store,
) *GrowthWorker {
	if chatUC == nil || agentUC == nil || growthUC == nil {
		return nil
	}
	helper := log.NewHelper(logger)
	poll := defPollInterval(growthCfg)
	idle := defIdleSweepInterval(growthCfg)
	idleCheck := defIdleCheckInterval(growthCfg)
	maxRetry := defaultGrowthMaxRetry
	if growthCfg != nil && growthCfg.GetMaxRetry() > 0 {
		maxRetry = int(growthCfg.GetMaxRetry())
	} else if v := os.Getenv("SATH_GROWTH_MAX_RETRY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxRetry = n
		}
	}

	cronRewrite := newCronSkillRefRewriter(cronRefUC, helper)
	w := &GrowthWorker{
		log:                    helper,
		holderID:               uuid.NewString(),
		servicePrincipalUserID: servicePrincipalUserID(auth),
		pollInterval:           poll,
		idleInterval:           idle,
		idleCheckInterval:      idleCheck,
		growthUC:               growthUC,
		growthCfg:              growthCfg,
		turnTraces:             turnTraces,
		cronRewrite:            cronRewrite,
		wake:                   make(chan struct{}, 1),
		maxRetry:               maxRetry,
		backoffAt:              make(map[string]time.Time),
	}

	memoryNotify := func(ctx context.Context, sessionID string) {
		chat.NotifyMemorySessionDirty(ctx, sessionID, 0, 0, chatUC, agentUC, chat.NewChatTranscriptProvider(chatUC))
	}
	w.memoryNotify = memoryNotify

	baseTranscript := chat.NewChatTranscriptProvider(chatUC)
	deps := growth.RunnerDeps{
		MemoryNotify: memoryNotify,
		ClearGrowthPending: func(ctx context.Context, sessionID string, clearSkill, clearMemory bool) error {
			return growthUC.ClearGrowthPending(ctx, sessionID, clearSkill, clearMemory)
		},
		Transcript: func(ctx context.Context, sessionID string) (string, error) {
			tr, err := baseTranscript.GetTranscript(ctx, sessionID)
			if err != nil {
				return "", err
			}
			digest := fetchReviewTraceDigest(ctx, w.turnTraces, sessionID)
			return appendTraceDigest(tr, digest), nil
		},
		// B1：注入记忆子系统状态摘要供复盘 prompt 使用。
		MemoryState: func(ctx context.Context, sessionID string) (string, error) {
			return chat.GetMemoryStateSummary(ctx, sessionID, chatUC, agentUC)
		},
		// A4：补丁应用成功后，记录 framework 进程内 generation；外加 portal 日志便于追踪。
		InvalidateSkillsCache: func(ctx context.Context, workspace string) {
			gen := growth.DefaultSkillsIndexTracker.Generation(workspace)
			helper.Infof("growth skills cache invalidated workspace=%s generation=%d", workspace, gen)
		},
		RewriteCronSkillRefs: cronRewrite,
	}

	// 真 LLM 客户端优先于 file-stub：configured 时构造 LLMSkillProposer + 可选 Combined。
	if llmReviewEnabled && growthCfg != nil && growthCfg.GetLlm() != nil &&
		strings.TrimSpace(growthCfg.GetLlm().GetModel()) != "" {
		llmCfg := growthCfg.GetLlm()
		client, err := newGrowthModelClient(llmCfg)
		if err != nil {
			helper.Warnf("growth: failed to build LLM client, falling back: %v", err)
		} else {
			runnerCfg := growth.LLMRunnerConfig{
				SystemPrompt:       llmCfg.GetSkillSystemPrompt(),
				MaxTranscriptRunes: int(llmCfg.GetMaxTranscriptRunes()),
			}
			deps.ProposeSkillPatches = growth.NewLLMSkillProposer(client, runnerCfg)
			helper.Infof("growth: using LLM skill proposer model=%s provider=%s", llmCfg.GetModel(), llmCfg.GetProvider())
			if growthCfg.GetCombinedReviewEnabled() {
				combinedCfg := runnerCfg
				combinedCfg.SystemPrompt = llmCfg.GetCombinedSystemPrompt()
				deps.ProposeCombinedReview = growth.NewLLMCombinedProposer(client, combinedCfg)
			}
		}
	}

	// 没注入 LLM 时回退到 file-stub（保持一期/二期 2.2 行为）。
	if deps.ProposeSkillPatches == nil {
		deps.ProposeSkillPatches = patchProposerFromFile(reviewPatchFile)
		if reviewPatchFile != "" {
			helper.Infof("growth: using review_patch_file=%s", reviewPatchFile)
		}
	}

	// fork-agent 复盘路径（spec §4）：仅在 agent_review_enabled 且 LLM 已配置时装配。
	agentReviewEnabled := growthCfg != nil && growthCfg.GetAgentReviewEnabled() &&
		growthCfg.GetLlm() != nil && strings.TrimSpace(growthCfg.GetLlm().GetModel()) != ""
	if agentReviewEnabled {
		if rm, err := newGrowthReviewModel(growthCfg.GetLlm()); err != nil {
			helper.Warnf("growth: agent review model failed, disabling fork-agent path: %v", err)
			agentReviewEnabled = false
		} else {
			w.reviewModel = rm
			deps.SpawnReviewAgent = w.spawnReviewAgent
		}
	}

	w.runner = growth.NewRunner(growth.RunnerSelect{
		LLMReviewEnabled:   llmReviewEnabled,
		AgentReviewEnabled: agentReviewEnabled,
	}, deps)

	growthwake.Register(func() {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	})
	return w
}

// Loop 阻塞直至 ctx 取消；先执行一轮再按 ticker 轮询。
func (w *GrowthWorker) Loop(ctx context.Context) {
	if w == nil {
		return
	}
	ctx = w.internalContext(ctx)
	w.pollOnce(ctx)
	t := time.NewTicker(w.pollInterval)
	defer t.Stop()
	idleT := time.NewTicker(w.idleInterval)
	defer idleT.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.pollOnce(ctx)
		case <-t.C:
			w.pollOnce(ctx)
		case <-idleT.C:
			w.sweepIdle(ctx)
		}
	}
}

func (w *GrowthWorker) pollOnce(ctx context.Context) {
	rows, err := w.growthUC.ListPendingReviewSessions(ctx, 50)
	if err != nil {
		w.log.Warnf("growth worker list pending: %v", err)
		return
	}
	// E2: 按 workspace 聚合 pending 深度作 gauge。
	depth := make(map[string]int64, len(rows))
	for i := range rows {
		depth[rows[i].WorkspaceKey]++
	}
	for ws, n := range depth {
		growth.DefaultMetrics.ObservePendingDepth(ws, n)
	}
	for i := range rows {
		w.handleRow(ctx, rows[i])
	}
}

// shouldBackoff 当 session 最近一次失败后处于指数退避窗口时返回 true。
// 退避窗口 = base * 2^(retryCount-1)，base=30s，上限 10 分钟。
func (w *GrowthWorker) shouldBackoff(sessionID string, retryCount int) bool {
	if retryCount <= 0 {
		return false
	}
	w.backoffMu.Lock()
	last, ok := w.backoffAt[sessionID]
	w.backoffMu.Unlock()
	if !ok {
		return false
	}
	base := 30 * time.Second
	maxWindow := 10 * time.Minute
	shift := retryCount - 1
	if shift > 10 {
		shift = 10
	}
	window := base << shift
	if window > maxWindow {
		window = maxWindow
	}
	return time.Since(last) < window
}

func (w *GrowthWorker) markBackoff(sessionID string) {
	w.backoffMu.Lock()
	defer w.backoffMu.Unlock()
	w.backoffAt[sessionID] = time.Now()
}

func (w *GrowthWorker) clearBackoff(sessionID string) {
	w.backoffMu.Lock()
	defer w.backoffMu.Unlock()
	delete(w.backoffAt, sessionID)
}

func (w *GrowthWorker) handleRow(ctx context.Context, row biz.GrowthPendingSession) {
	if row.SessionID == "" || row.WorkspaceKey == "" {
		return
	}

	// A5：在抢租约之前判断是否需要主动清除 pending（超过 max_retry）。
	st, err := w.growthUC.GetState(ctx, row.SessionID)
	if err == nil && st != nil {
		if w.maxRetry > 0 && st.ReviewRetryCount >= w.maxRetry {
			if dropped, derr := w.growthUC.DropPendingAfterMaxRetry(ctx, row.SessionID, w.maxRetry); derr == nil && dropped {
				w.log.Warnf("growth: drop pending session=%s after %d failures", row.SessionID, st.ReviewRetryCount)
				w.clearBackoff(row.SessionID)
				growth.DefaultMetrics.IncPendingDropped()
				return
			}
		}
		if w.shouldBackoff(row.SessionID, st.ReviewRetryCount) {
			return
		}
		// C3 Worker gates：in_flight TTL + recent BackgroundReview dedupe（认领前，不占 lease）。
		if w.skipPendingClaim(ctx, st) {
			return
		}
	}

	def := growth.NewDefaults()
	acquired, err := w.growthUC.TryAcquireWorkspaceLease(ctx, row.WorkspaceKey, w.holderID, def.LeaseTTL)
	if err != nil {
		w.log.Warnf("growth lease acquire workspace=%s err=%v", row.WorkspaceKey, err)
		growth.DefaultMetrics.IncLeaseAcquireErr()
		return
	}
	if !acquired {
		growth.DefaultMetrics.IncLeaseContention()
		return
	}
	release := func() {
		rctx, cancel := context.WithTimeout(w.internalContext(context.Background()), 10*time.Second)
		defer cancel()
		if err := w.growthUC.ReleaseWorkspaceLease(rctx, row.WorkspaceKey, w.holderID); err != nil {
			w.log.Warnf("growth lease release workspace=%s err=%v", row.WorkspaceKey, err)
		}
	}
	defer release()

	st, err = w.growthUC.GetState(ctx, row.SessionID)
	if err != nil {
		w.log.Warnf("growth worker get state session=%s err=%v", row.SessionID, err)
		return
	}

	started := time.Now()
	pendingSkill := st.PendingSkillReview
	pendingMemory := st.PendingMemoryReview

	bus := events.DefaultBus()
	payload := map[string]any{
		"session_id":     row.SessionID,
		"agent_id":       row.AgentID,
		"workspace":      row.WorkspaceKey,
		"holder_id":      w.holderID,
		"pending_skill":  pendingSkill,
		"pending_memory": pendingMemory,
		"started_at":     started,
		"retry_count":    st.ReviewRetryCount,
	}
	if bus != nil {
		bus.Publish(ctx, events.Event{Kind: events.GrowthReviewScheduled, Payload: payload})
	}
	growth.DefaultMetrics.IncReviewScheduled()

	job := growth.ReviewJob{
		SessionID:     row.SessionID,
		WorkspaceKey:  row.WorkspaceKey,
		WorkspaceRoot: row.WorkspaceKey,
		PendingSkill:  pendingSkill,
		PendingMemory: pendingMemory,
	}
	w.fillLearningsSummary(&job)
	runErr := w.runner.Run(ctx, job)
	durationMs := time.Since(started).Milliseconds()
	if runErr != nil {
		w.log.Warnf("growth runner session=%s err=%v", row.SessionID, runErr)
		_ = w.growthUC.RecordReviewRunFailure(ctx, row.SessionID, runErr, pendingSkill, pendingMemory)
		w.markBackoff(row.SessionID)
		growth.DefaultMetrics.IncReviewFailed()
		if bus != nil {
			bus.Publish(ctx, events.Event{
				Kind: events.GrowthReviewFailed,
				Payload: map[string]any{
					"session_id":  row.SessionID,
					"workspace":   row.WorkspaceKey,
					"error":       runErr.Error(),
					"duration_ms": durationMs,
				},
			})
		}
		return
	}
	w.clearBackoff(row.SessionID)
	growth.DefaultMetrics.IncReviewCompleted()
	if bus != nil {
		bus.Publish(ctx, events.Event{
			Kind: events.GrowthReviewCompleted,
			Payload: map[string]any{
				"session_id":  row.SessionID,
				"workspace":   row.WorkspaceKey,
				"duration_ms": durationMs,
			},
		})
	}
}

// sweepIdle 扫描无 pending 标志但长时间未做空闲检查的会话，执行轻量 memory-only 复盘。
func (w *GrowthWorker) sweepIdle(ctx context.Context) {
	growth.DefaultMetrics.IncIdleSweep()
	rows, err := w.growthUC.ListIdleSessions(ctx, w.idleCheckInterval, 50)
	if err != nil {
		w.log.Warnf("growth worker list idle: %v", err)
		return
	}
	for i := range rows {
		w.handleIdleRow(ctx, rows[i])
	}
}

func (w *GrowthWorker) handleIdleRow(ctx context.Context, row biz.GrowthPendingSession) {
	if row.SessionID == "" || row.WorkspaceKey == "" {
		return
	}
	def := growth.NewDefaults()
	acquired, err := w.growthUC.TryAcquireWorkspaceLease(ctx, row.WorkspaceKey, w.holderID, def.LeaseTTL)
	if err != nil {
		w.log.Warnf("growth idle lease acquire workspace=%s err=%v", row.WorkspaceKey, err)
		return
	}
	if !acquired {
		return
	}
	release := func() {
		rctx, cancel := context.WithTimeout(w.internalContext(context.Background()), 10*time.Second)
		defer cancel()
		if err := w.growthUC.ReleaseWorkspaceLease(rctx, row.WorkspaceKey, w.holderID); err != nil {
			w.log.Warnf("growth idle lease release workspace=%s err=%v", row.WorkspaceKey, err)
		}
	}
	defer release()

	// 空闲扫描仅做 memory-only 复盘（无技能补丁）。
	job := growth.ReviewJob{
		SessionID:     row.SessionID,
		WorkspaceKey:  row.WorkspaceKey,
		WorkspaceRoot: row.WorkspaceKey,
		PendingSkill:  false,
		PendingMemory: true,
	}
	runErr := w.runner.Run(ctx, job)
	if runErr != nil {
		w.log.Warnf("growth idle runner session=%s err=%v", row.SessionID, runErr)
	}
	// 无论成功与否都标记已检查，避免反复重试。
	if err := w.growthUC.MarkIdleCheckDone(ctx, row.SessionID); err != nil {
		w.log.Warnf("growth idle mark done session=%s err=%v", row.SessionID, err)
	}
}

// patchProposerFromFile 从 JSON 文件读取补丁（growth.ParsePatchBatchJSON）；路径为空则 nil。
// 每次 Run 内调用时重新读文件，便于联调改文件无需重启。
func patchProposerFromFile(absPath string) func(ctx context.Context, job growth.ReviewJob, transcript, skillsSummary string) ([]growth.Patch, error) {
	if absPath == "" {
		return nil
	}
	return func(ctx context.Context, job growth.ReviewJob, transcript, skillsSummary string) ([]growth.Patch, error) {
		_ = ctx
		_ = job
		_ = transcript
		_ = skillsSummary
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("growth review patch file %q: %w", absPath, err)
		}
		return growth.ParsePatchBatchJSON(data)
	}
}

// growthModelClient 适配 framework/model.Model 到 framework/growth.LLMClient。
type growthModelClient struct {
	m model.Model
}

func (c *growthModelClient) Complete(ctx context.Context, prompt string) (string, error) {
	gen, err := c.m.Generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	if gen == nil {
		return "", nil
	}
	return gen.Text, nil
}

func defPollInterval(growthCfg *conf.Growth) time.Duration {
	if d := growthDuration(growthCfg, func(g *conf.Growth) *durationpb.Duration {
		if g == nil {
			return nil
		}
		return g.GetWorkerPollInterval()
	}, 5*time.Second, 24*time.Hour); d > 0 {
		return d
	}
	if v := os.Getenv("SATH_GROWTH_WORKER_POLL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 5*time.Second && d <= 24*time.Hour {
			return d
		}
	}
	return defaultGrowthPollInterval
}

func defIdleSweepInterval(growthCfg *conf.Growth) time.Duration {
	if d := growthDuration(growthCfg, func(g *conf.Growth) *durationpb.Duration {
		if g == nil {
			return nil
		}
		return g.GetIdleSweepInterval()
	}, 30*time.Second, 24*time.Hour); d > 0 {
		return d
	}
	if v := os.Getenv("SATH_GROWTH_IDLE_SWEEP"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 30*time.Second && d <= 24*time.Hour {
			return d
		}
	}
	return defaultIdleSweepInterval
}

func defIdleCheckInterval(growthCfg *conf.Growth) time.Duration {
	if d := growthDuration(growthCfg, func(g *conf.Growth) *durationpb.Duration {
		if g == nil {
			return nil
		}
		return g.GetIdleCheckInterval()
	}, time.Minute, 24*time.Hour); d > 0 {
		return d
	}
	return growth.NewDefaults().IdleCheckInterval
}

func (w *GrowthWorker) fillLearningsSummary(job *growth.ReviewJob) {
	if w == nil || job == nil || w.growthCfg == nil || !w.growthCfg.GetLearningsReviewEnabled() {
		return
	}
	max := int(w.growthCfg.GetLearningsMaxRunes())
	if max <= 0 {
		max = 6000
	}
	root := job.WorkspaceRoot
	if root == "" {
		root = job.WorkspaceKey
	}
	job.LearningsSummary = growth.ReadWorkspaceLearnings(root, max)
}

// skipPendingClaim applies C3 Worker gates before lease/claim.
// 1) BgReviewInFlight && !stale → skip
// 2) in_flight && stale → clear in_flight, continue
// 3) within dedupe_window of LastBackgroundReviewAt && no newer pending → skip
func (w *GrowthWorker) skipPendingClaim(ctx context.Context, st *biz.ChatGrowthState) bool {
	if w == nil || st == nil || w.growthUC == nil {
		return false
	}
	now := time.Now()
	origUpdated := st.UpdatedAt

	if st.BgReviewInFlight {
		if !isBgReviewInFlightStale(st, biz.BgReviewInFlightTTL(), now) {
			growth.DefaultMetrics.IncAsyncSkippedInFlight()
			if w.log != nil {
				w.log.Infof("growth: skip claim session=%s bg_review_in_flight", st.SessionID)
			}
			return true
		}
		if err := w.growthUC.SetBgReviewInFlight(ctx, st.SessionID, false); err != nil {
			if w.log != nil {
				w.log.Warnf("growth: clear stale in_flight session=%s err=%v", st.SessionID, err)
			}
			return true // fail closed: do not race a possibly-live fork
		}
		st.BgReviewInFlight = false
		st.BgReviewInFlightSince = nil
		growth.DefaultMetrics.IncBgInFlightStaleCleared()
		if w.log != nil {
			w.log.Warnf("growth: cleared stale bg_review_in_flight session=%s", st.SessionID)
		}
	}

	if recentlyBackgroundReviewedSkip(st, origUpdated, biz.BgReviewDedupeWindow(), now) {
		growth.DefaultMetrics.IncAsyncSkippedRecentBG()
		if w.log != nil {
			w.log.Infof("growth: skip claim session=%s recent background review (dedupe)", st.SessionID)
		}
		return true
	}
	return false
}

func isBgReviewInFlightStale(st *biz.ChatGrowthState, ttl time.Duration, now time.Time) bool {
	if st == nil || !st.BgReviewInFlight {
		return false
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if st.BgReviewInFlightSince == nil {
		return true // missing since → treat as stale to avoid permanent block
	}
	return now.Sub(*st.BgReviewInFlightSince) > ttl
}

func recentlyBackgroundReviewedSkip(st *biz.ChatGrowthState, stateUpdatedAt time.Time, window time.Duration, now time.Time) bool {
	if !biz.RecentlyBackgroundReviewed(st, window, now) {
		return false
	}
	if biz.HasNewerPendingThanLastBG(st, stateUpdatedAt) {
		return false
	}
	return true
}

func growthDuration(growthCfg *conf.Growth, pick func(*conf.Growth) *durationpb.Duration, min, max time.Duration) time.Duration {
	if growthCfg == nil {
		return 0
	}
	pb := pick(growthCfg)
	if pb == nil {
		return 0
	}
	d := pb.AsDuration()
	if d < min || d > max {
		return 0
	}
	return d
}

func newGrowthModelClient(cfg *conf.GrowthLLM) (growth.LLMClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil GrowthLLM config")
	}
	// L3: auxiliary 优先 - 用独立的 cheap 模型跑复盘，避免占用主对话配额。
	target := cfg
	if aux := cfg.GetAuxiliary(); aux != nil && strings.TrimSpace(aux.GetModel()) != "" {
		target = aux
	}
	m, err := model.NewModelFromConfig(model.ModelConfig{
		Provider: target.GetProvider(),
		Model:    target.GetModel(),
		APIKey:   target.GetApiKey(),
		BaseURL:  target.GetBaseUrl(),
	})
	if err != nil {
		return nil, err
	}
	return &growthModelClient{m: m}, nil
}

// newGrowthReviewModel 复用 newGrowthModelClient 的 auxiliary-优先选择，返回裸 model.Model 供 ReActAgent 使用。
func newGrowthReviewModel(cfg *conf.GrowthLLM) (model.Model, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil GrowthLLM config")
	}
	target := cfg
	if aux := cfg.GetAuxiliary(); aux != nil && strings.TrimSpace(aux.GetModel()) != "" {
		target = aux
	}
	return model.NewModelFromConfig(model.ModelConfig{
		Provider: target.GetProvider(),
		Model:    target.GetModel(),
		APIKey:   target.GetApiKey(),
		BaseURL:  target.GetBaseUrl(),
	})
}
