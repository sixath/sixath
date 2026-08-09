package biz

import (
	"context"
	"errors"
	"time"

	pkgErrors "backend/internal/pkg/errors"
	"backend/internal/growthwake"

	"github.com/sixath/framework/growth"
)

// ChatGrowthState 会话级成长游标（与 data/model.ChatGrowthState 字段对齐）。
type ChatGrowthState struct {
	SessionID              string
	ToolItersSinceReview   int
	TurnsSinceMemoryReview int
	PendingSkillReview     bool
	PendingMemoryReview    bool
	LastSkillError         string
	LastMemoryError        string
	ReviewFailedAt         *time.Time
	// ReviewRetryCount 累计连续失败次数（spec phase2 A5）；成功复盘后清零，达 max_retry 时由 worker 自动清 pending。
	ReviewRetryCount int
	LastIdleCheckAt  *time.Time
	// LastBackgroundReviewAt 最近一次 C3 即时 BackgroundReview 成功完成时间。
	LastBackgroundReviewAt *time.Time
	// LastReviewRequestID 最近一次已复盘的 chat request_id。
	LastReviewRequestID string
	// BgReviewInFlight 本进程 C3 fork 进行中。
	BgReviewInFlight bool
	// BgReviewInFlightSince in_flight 置位时间。
	BgReviewInFlightSince *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// GrowthPendingSession worker 拉取的待复盘行（workspace 作租约键）。
type GrowthPendingSession struct {
	SessionID    string
	AgentID      string
	WorkspaceKey string
}

// GrowthRepo 成长状态与 workspace 租约持久化。
type GrowthRepo interface {
	GetState(ctx context.Context, sessionID string) (*ChatGrowthState, error)
	SaveState(ctx context.Context, st *ChatGrowthState) error
	TryAcquireLease(ctx context.Context, workspaceKey, holderID string, ttl time.Duration) (acquired bool, err error)
	ReleaseLease(ctx context.Context, workspaceKey, holderID string) error
	ListPendingReviewSessions(ctx context.Context, limit int) ([]GrowthPendingSession, error)
	// ListIdleSessions 返回超过 idleInterval 未做空闲检查的活动会话（无 pending 标志的会话，供轻量 memory-only 复盘）。
	ListIdleSessions(ctx context.Context, idleInterval time.Duration, limit int) ([]GrowthPendingSession, error)
}

// GrowthUsecase 成长子域用例（portal 编排；复盘执行在 framework）。
type GrowthUsecase struct {
	repo GrowthRepo
	// sessionEndMemoryReview 开启 C2：ChatSession 结束时、未达记忆阈值也可置 pending_memory_review。
	sessionEndMemoryReview bool
	// sessionEndSkillReview 开启 C2s：ChatSession 结束时、未达技能阈值也可置 pending_skill_review。
	sessionEndSkillReview bool
	// backgroundReviewEnabled 开启 C3：FinalizeTurnForBackgroundReview 同步写计数/pending，不 Wake；
	// OnToolSuccess / OnAssistantTurn 计数路径关闭以免双计。默认 false（NewGrowthUsecase）；
	// ProvideGrowthUsecase 从 SATH_BACKGROUND_REVIEW 注入（缺省 true）。
	backgroundReviewEnabled bool
	// nudge 控制 OnToolSuccess / OnAssistantTurn 阈值触发（G1）；构造默认 Enabled=true。
	nudge growth.NudgeConfig
}

// NewGrowthUsecase constructs GrowthUsecase.
func NewGrowthUsecase(repo GrowthRepo) *GrowthUsecase {
	return &GrowthUsecase{repo: repo, nudge: growth.DefaultNudgeConfig()}
}

// SetNudgeConfig 覆盖 G1 阈值 nudge（Enabled 默认 true；interval<=0 用 framework Defaults）。
func (uc *GrowthUsecase) SetNudgeConfig(cfg growth.NudgeConfig) {
	if uc == nil {
		return
	}
	uc.nudge = cfg
}

// SetSessionEndMemoryReviewEnabled 配置 C2 会话结束轻量记忆复盘（默认 false）。
func (uc *GrowthUsecase) SetSessionEndMemoryReviewEnabled(enabled bool) {
	if uc == nil {
		return
	}
	uc.sessionEndMemoryReview = enabled
}

// SetSessionEndSkillReviewEnabled 配置 C2s 会话结束轻量技能复盘（默认 false）。
func (uc *GrowthUsecase) SetSessionEndSkillReviewEnabled(enabled bool) {
	if uc == nil {
		return
	}
	uc.sessionEndSkillReview = enabled
}

// SetBackgroundReviewEnabled 配置 C3 即时 BackgroundReview（FinalizeTurn 同步路径）。
func (uc *GrowthUsecase) SetBackgroundReviewEnabled(enabled bool) {
	if uc == nil {
		return
	}
	uc.backgroundReviewEnabled = enabled
}

// BackgroundReviewEnabled 报告 C3 是否开启（供 chat hooks / Task 11 spawn 门控）。
func (uc *GrowthUsecase) BackgroundReviewEnabled() bool {
	return uc != nil && uc.backgroundReviewEnabled
}

// EnsureState 返回已有行，否则插入默认计数并返回。
func (uc *GrowthUsecase) EnsureState(ctx context.Context, sessionID string) (*ChatGrowthState, error) {
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err == nil {
		return st, nil
	}
	if !errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, err
	}
	now := time.Now()
	st = &ChatGrowthState{
		SessionID: sessionID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := uc.repo.SaveState(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

// TryAcquireWorkspaceLease 尝试取得 workspace 复盘租约（多副本 CAS）。
func (uc *GrowthUsecase) TryAcquireWorkspaceLease(ctx context.Context, workspaceKey, holderID string, ttl time.Duration) (bool, error) {
	return uc.repo.TryAcquireLease(ctx, workspaceKey, holderID, ttl)
}

// ReleaseWorkspaceLease 释放租约（仅 holder 本人可释放）。
func (uc *GrowthUsecase) ReleaseWorkspaceLease(ctx context.Context, workspaceKey, holderID string) error {
	return uc.repo.ReleaseLease(ctx, workspaceKey, holderID)
}

// GetState 读取成长状态（无则返回 ErrNotFound 包装）。
func (uc *GrowthUsecase) GetState(ctx context.Context, sessionID string) (*ChatGrowthState, error) {
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}
	return uc.repo.GetState(ctx, sessionID)
}

// ListPendingReviewSessions 返回仍有 pending 标志的会话（供 worker 轮询）。
func (uc *GrowthUsecase) ListPendingReviewSessions(ctx context.Context, limit int) ([]GrowthPendingSession, error) {
	if limit <= 0 {
		limit = 50
	}
	return uc.repo.ListPendingReviewSessions(ctx, limit)
}

// ListIdleSessions 返回超过 idleInterval 未做空闲检查的活动会话（供 worker 空闲扫描）。
func (uc *GrowthUsecase) ListIdleSessions(ctx context.Context, idleInterval time.Duration, limit int) ([]GrowthPendingSession, error) {
	if limit <= 0 {
		limit = 50
	}
	return uc.repo.ListIdleSessions(ctx, idleInterval, limit)
}

// MarkIdleCheckDone 更新 last_idle_check_at 为当前时间。
func (uc *GrowthUsecase) MarkIdleCheckDone(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil
		}
		return err
	}
	now := time.Now()
	st.LastIdleCheckAt = &now
	st.UpdatedAt = now
	return uc.repo.SaveState(ctx, st)
}

// ClearGrowthPending 按位清除 pending 与对应 last_*；两者皆 false 时不写库。
// 当技能与记忆 pending 均被清除时，同时清除 review_failed_at 与重试计数（与一期全清语义对齐）。
func (uc *GrowthUsecase) ClearGrowthPending(ctx context.Context, sessionID string, clearSkill, clearMemory bool) error {
	if sessionID == "" || (!clearSkill && !clearMemory) {
		return nil
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil
		}
		return err
	}
	if clearSkill {
		st.PendingSkillReview = false
		st.LastSkillError = ""
	}
	if clearMemory {
		st.PendingMemoryReview = false
		st.LastMemoryError = ""
	}
	if !st.PendingSkillReview && !st.PendingMemoryReview {
		st.ReviewFailedAt = nil
		st.ReviewRetryCount = 0
	}
	st.UpdatedAt = time.Now()
	return uc.repo.SaveState(ctx, st)
}

// ClearReviewPending 在复盘完成后清除技能/记忆 pending 标志（v1 stub 全清）。
func (uc *GrowthUsecase) ClearReviewPending(ctx context.Context, sessionID string) error {
	return uc.ClearGrowthPending(ctx, sessionID, true, true)
}

// RecordReviewRunFailure 复盘执行失败时回写 last_*、review_failed_at 与递增重试计数（spec phase2 §A5；pending 保留以便重试）。
func (uc *GrowthUsecase) RecordReviewRunFailure(ctx context.Context, sessionID string, runErr error, pendingSkill, pendingMemory bool) error {
	if sessionID == "" || runErr == nil {
		return nil
	}
	msg := runErr.Error()
	const maxLen = 2048
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil
		}
		return err
	}
	now := time.Now()
	if pendingSkill {
		st.LastSkillError = msg
	}
	if pendingMemory {
		st.LastMemoryError = msg
	}
	if !pendingSkill && !pendingMemory {
		st.LastSkillError = msg
	}
	st.ReviewFailedAt = &now
	st.ReviewRetryCount++
	st.UpdatedAt = now
	return uc.repo.SaveState(ctx, st)
}

// DropPendingAfterMaxRetry 在重试次数超过阈值时强制清除 pending，避免无限循环（A5）。
// 返回是否真正清除，便于上层埋点。maxRetry <= 0 视为不启用。
func (uc *GrowthUsecase) DropPendingAfterMaxRetry(ctx context.Context, sessionID string, maxRetry int) (bool, error) {
	if sessionID == "" || maxRetry <= 0 {
		return false, nil
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if st.ReviewRetryCount < maxRetry {
		return false, nil
	}
	if !st.PendingSkillReview && !st.PendingMemoryReview {
		return false, nil
	}
	st.PendingSkillReview = false
	st.PendingMemoryReview = false
	st.ReviewRetryCount = 0
	st.ReviewFailedAt = nil
	st.UpdatedAt = time.Now()
	return true, uc.repo.SaveState(ctx, st)
}

// OnToolSuccess 在单次工具成功完成后递增技能环计数；达阈值则置 pending_skill_review 并清零该计数（不阻塞调用方，错误由上层记录）。
// Enabled=false 时仍计数但封顶在 interval，不置 pending、不 Wake。
// C3（backgroundReviewEnabled）开启时为 no-op：计数改由 FinalizeTurnForBackgroundReview 同步写入，避免与 hook 双计 / 提前 Wake。
func (uc *GrowthUsecase) OnToolSuccess(ctx context.Context, sessionID string) error {
	if sessionID == "" || uc.backgroundReviewEnabled {
		return nil
	}
	interval := uc.nudge.EffectiveSkillToolInterval()
	st, err := uc.EnsureState(ctx, sessionID)
	if err != nil {
		return err
	}
	st.ToolItersSinceReview++
	signalPending := false
	if st.ToolItersSinceReview >= interval {
		if !uc.nudge.Enabled {
			st.ToolItersSinceReview = interval
		} else {
			st.PendingSkillReview = true
			st.ToolItersSinceReview = 0
			signalPending = true
		}
	}
	st.UpdatedAt = time.Now()
	if err := uc.repo.SaveState(ctx, st); err != nil {
		return err
	}
	if signalPending {
		growthwake.Wake()
	}
	return nil
}

// OnAssistantTurn 在 assistant 消息持久化后递增记忆环计数；达阈值则置 pending_memory_review 并清零该计数。
// Enabled=false 时仍计数但封顶在 interval，不置 pending、不 Wake。
// C3 开启时为 no-op：见 FinalizeTurnForBackgroundReview。
func (uc *GrowthUsecase) OnAssistantTurn(ctx context.Context, sessionID string) error {
	if sessionID == "" || uc.backgroundReviewEnabled {
		return nil
	}
	interval := uc.nudge.EffectiveMemoryTurnInterval()
	st, err := uc.EnsureState(ctx, sessionID)
	if err != nil {
		return err
	}
	st.TurnsSinceMemoryReview++
	signalPending := false
	if st.TurnsSinceMemoryReview >= interval {
		if !uc.nudge.Enabled {
			st.TurnsSinceMemoryReview = interval
		} else {
			st.PendingMemoryReview = true
			st.TurnsSinceMemoryReview = 0
			signalPending = true
		}
	}
	st.UpdatedAt = time.Now()
	if err := uc.repo.SaveState(ctx, st); err != nil {
		return err
	}
	if signalPending {
		growthwake.Wake()
	}
	return nil
}

// TrySessionEndMemoryReview 在 ChatSession 结束（DeleteSession → ChatSessionHooks）时调用（C2 / G2）。
// 当无 pending、但有未达阈值的成长计数时，置 pending_memory_review 并唤醒 worker，走 memory-only 复盘。
// 若最近 C3 BackgroundReview 仍在 dedupe_window 内则跳过（与 Worker 去重对齐）。
func (uc *GrowthUsecase) TrySessionEndMemoryReview(ctx context.Context, sessionID string) error {
	if uc == nil || !uc.sessionEndMemoryReview || sessionID == "" {
		return nil
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil
		}
		return err
	}
	if st.PendingMemoryReview {
		return nil
	}
	if st.TurnsSinceMemoryReview == 0 && st.ToolItersSinceReview == 0 {
		return nil
	}
	if RecentlyBackgroundReviewed(st, BgReviewDedupeWindow(), time.Now()) {
		return nil
	}
	st.PendingMemoryReview = true
	st.UpdatedAt = time.Now()
	if err := uc.repo.SaveState(ctx, st); err != nil {
		return err
	}
	growthwake.Wake()
	return nil
}

// TrySessionEndSkillReview 在 ChatSession 结束（DeleteSession → ChatSessionHooks）时调用（C2s / G2）。
// 当无 pending_skill、但本轮有未达阈值的工具/回合计数时，置 pending_skill_review 并唤醒 worker。
// 若最近 C3 BackgroundReview 仍在 dedupe_window 内则跳过。
func (uc *GrowthUsecase) TrySessionEndSkillReview(ctx context.Context, sessionID string) error {
	if uc == nil || !uc.sessionEndSkillReview || sessionID == "" {
		return nil
	}
	st, err := uc.repo.GetState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil
		}
		return err
	}
	if st.PendingSkillReview {
		return nil
	}
	if st.ToolItersSinceReview == 0 && st.TurnsSinceMemoryReview == 0 {
		return nil
	}
	if RecentlyBackgroundReviewed(st, BgReviewDedupeWindow(), time.Now()) {
		return nil
	}
	st.PendingSkillReview = true
	st.UpdatedAt = time.Now()
	if err := uc.repo.SaveState(ctx, st); err != nil {
		return err
	}
	growthwake.Wake()
	return nil
}

// FinalizeTurnForBackgroundReview runs synchronously on the chat request goroutine
// after Run completes. When background_review.enabled (C3):
//   - apply toolSuccessCount / assistantTurn to counters
//   - set pending_* if thresholds crossed
//   - do NOT Wake()
//
// Returns whether skill and/or memory review should spawn in-process.
// spawn* is true if pending_* is set AFTER this call (including pending that
// already existed before this turn), not only when this turn newly crossed a threshold.
//
// When C3 disabled: no-op returns false,false (legacy OnToolSuccess / OnAssistantTurn + Wake still apply).
// requestID is accepted for Task 11 correlation (LastReviewRequestID / spawn); not persisted here.
func (uc *GrowthUsecase) FinalizeTurnForBackgroundReview(ctx context.Context, sessionID, requestID string, toolSuccessCount int, assistantTurn bool) (spawnSkill, spawnMemory bool, err error) {
	_ = requestID
	if uc == nil || !uc.backgroundReviewEnabled || sessionID == "" {
		return false, false, nil
	}
	st, err := uc.EnsureState(ctx, sessionID)
	if err != nil {
		return false, false, err
	}

	skillInterval := uc.nudge.EffectiveSkillToolInterval()
	memInterval := uc.nudge.EffectiveMemoryTurnInterval()
	changed := false

	if toolSuccessCount > 0 {
		for i := 0; i < toolSuccessCount; i++ {
			st.ToolItersSinceReview++
			changed = true
			if st.ToolItersSinceReview >= skillInterval {
				if !uc.nudge.Enabled {
					st.ToolItersSinceReview = skillInterval
				} else {
					st.PendingSkillReview = true
					st.ToolItersSinceReview = 0
				}
			}
		}
	}
	if assistantTurn {
		st.TurnsSinceMemoryReview++
		changed = true
		if st.TurnsSinceMemoryReview >= memInterval {
			if !uc.nudge.Enabled {
				st.TurnsSinceMemoryReview = memInterval
			} else {
				st.PendingMemoryReview = true
				st.TurnsSinceMemoryReview = 0
			}
		}
	}

	if changed {
		st.UpdatedAt = time.Now()
		if err := uc.repo.SaveState(ctx, st); err != nil {
			return false, false, err
		}
	}
	// Intentionally no growthwake.Wake() — Task 11 spawns in-process; Worker is fallback.
	return st.PendingSkillReview, st.PendingMemoryReview, nil
}

// SetBgReviewInFlight sets or clears the C3 in-process fork gate on growth state.
// When inFlight is true, BgReviewInFlightSince is set to now; when false, since is cleared.
func (uc *GrowthUsecase) SetBgReviewInFlight(ctx context.Context, sessionID string, inFlight bool) error {
	if uc == nil || sessionID == "" {
		return nil
	}
	st, err := uc.EnsureState(ctx, sessionID)
	if err != nil {
		return err
	}
	now := time.Now()
	st.BgReviewInFlight = inFlight
	if inFlight {
		st.BgReviewInFlightSince = &now
	} else {
		st.BgReviewInFlightSince = nil
	}
	st.UpdatedAt = now
	return uc.repo.SaveState(ctx, st)
}

// MarkBackgroundReviewSuccess records a completed C3 BackgroundReview and clears in_flight.
// Call after ClearGrowthPending on the success path (or together from the spawner).
func (uc *GrowthUsecase) MarkBackgroundReviewSuccess(ctx context.Context, sessionID, requestID string) error {
	if uc == nil || sessionID == "" {
		return nil
	}
	st, err := uc.EnsureState(ctx, sessionID)
	if err != nil {
		return err
	}
	now := time.Now()
	st.LastBackgroundReviewAt = &now
	if requestID != "" {
		st.LastReviewRequestID = requestID
	}
	st.BgReviewInFlight = false
	st.BgReviewInFlightSince = nil
	st.UpdatedAt = now
	return uc.repo.SaveState(ctx, st)
}
