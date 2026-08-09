package growth

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Metrics 暴露 worker 关键运行时计数（spec phase2 §E2）。
// 当前实现：进程内 atomic counters / sync.Map gauge；portal 可挂 /metrics expvar 或 Prometheus collector。
// 多副本部署下各实例独立计数，与 SkillsIndexTracker 一致。
type Metrics struct {
	// 累计计数（only-up）。
	reviewsScheduled atomic.Uint64
	reviewsCompleted atomic.Uint64
	reviewsFailed    atomic.Uint64
	leaseContention  atomic.Uint64 // TryAcquireLease 返回 acquired=false 的次数
	leaseAcquireErr  atomic.Uint64 // TryAcquireLease 返回 error 的次数
	idleSweepRuns    atomic.Uint64
	pendingDropped   atomic.Uint64 // DropPendingAfterMaxRetry 触发次数
	curatorRuns        atomic.Uint64
	curatorFailed      atomic.Uint64
	cronRefsRewritten  atomic.Uint64
	// C3 / async Worker 门闩观测（trajectory phase1）。
	asyncSkippedRecentBG   atomic.Uint64
	asyncSkippedInFlight   atomic.Uint64
	bgInFlightStaleCleared atomic.Uint64

	// gauges：按 workspace 统计的瞬时值（pending 队列深度等）。
	pendingDepthMu sync.RWMutex
	pendingDepth   map[string]int64
}

// NewMetrics 返回独立计数器。
func NewMetrics() *Metrics {
	return &Metrics{pendingDepth: make(map[string]int64)}
}

// DefaultMetrics 进程级共享 Metrics（portal 与未来 collector 共用）。
var DefaultMetrics = NewMetrics()

// IncReviewScheduled / Completed / Failed 累计计数。
func (m *Metrics) IncReviewScheduled() {
	if m != nil {
		m.reviewsScheduled.Add(1)
	}
}
func (m *Metrics) IncReviewCompleted() {
	if m != nil {
		m.reviewsCompleted.Add(1)
	}
}
func (m *Metrics) IncReviewFailed() {
	if m != nil {
		m.reviewsFailed.Add(1)
	}
}
func (m *Metrics) IncLeaseContention() {
	if m != nil {
		m.leaseContention.Add(1)
	}
}
func (m *Metrics) IncLeaseAcquireErr() {
	if m != nil {
		m.leaseAcquireErr.Add(1)
	}
}
func (m *Metrics) IncIdleSweep() {
	if m != nil {
		m.idleSweepRuns.Add(1)
	}
}
func (m *Metrics) IncPendingDropped() {
	if m != nil {
		m.pendingDropped.Add(1)
	}
}
func (m *Metrics) IncCuratorRun() {
	if m != nil {
		m.curatorRuns.Add(1)
	}
}
func (m *Metrics) IncCuratorFailed() {
	if m != nil {
		m.curatorFailed.Add(1)
	}
}

// IncCronRefsRewritten 累计 cron skill_execute payload 反写次数。
func (m *Metrics) IncCronRefsRewritten() {
	if m != nil {
		m.cronRefsRewritten.Add(1)
	}
}

// IncAsyncSkippedRecentBG counts Worker claims skipped due to recent C3 BackgroundReview (dedupe_window).
func (m *Metrics) IncAsyncSkippedRecentBG() {
	if m != nil {
		m.asyncSkippedRecentBG.Add(1)
	}
}

// IncAsyncSkippedInFlight counts Worker claims skipped while bg_review_in_flight is fresh.
func (m *Metrics) IncAsyncSkippedInFlight() {
	if m != nil {
		m.asyncSkippedInFlight.Add(1)
	}
}

// IncBgInFlightStaleCleared counts forced clears of stale bg_review_in_flight after in_flight_ttl.
func (m *Metrics) IncBgInFlightStaleCleared() {
	if m != nil {
		m.bgInFlightStaleCleared.Add(1)
	}
}

// ObservePendingDepth 设置某 workspace 的 pending 深度（gauge）。
// depth=0 时保留 key 便于观察清零事件。
func (m *Metrics) ObservePendingDepth(workspace string, depth int64) {
	if m == nil || workspace == "" {
		return
	}
	m.pendingDepthMu.Lock()
	defer m.pendingDepthMu.Unlock()
	m.pendingDepth[workspace] = depth
}

// Snapshot 返回一份不可变的指标快照，方便 portal 暴露到 /metrics 或日志聚合。
type MetricsSnapshot struct {
	ReviewsScheduled uint64
	ReviewsCompleted uint64
	ReviewsFailed    uint64
	LeaseContention  uint64
	LeaseAcquireErr  uint64
	IdleSweepRuns    uint64
	PendingDropped   uint64
	CuratorRuns        uint64
	CuratorFailed      uint64
	CronRefsRewritten      uint64
	AsyncSkippedRecentBG   uint64
	AsyncSkippedInFlight   uint64
	BgInFlightStaleCleared uint64
	PendingDepth           map[string]int64
}

// Snapshot 返回当前指标快照（深拷贝 PendingDepth）。
func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	snap := MetricsSnapshot{
		ReviewsScheduled: m.reviewsScheduled.Load(),
		ReviewsCompleted: m.reviewsCompleted.Load(),
		ReviewsFailed:    m.reviewsFailed.Load(),
		LeaseContention:  m.leaseContention.Load(),
		LeaseAcquireErr:  m.leaseAcquireErr.Load(),
		IdleSweepRuns:    m.idleSweepRuns.Load(),
		PendingDropped:   m.pendingDropped.Load(),
		CuratorRuns:       m.curatorRuns.Load(),
		CuratorFailed:     m.curatorFailed.Load(),
		CronRefsRewritten:      m.cronRefsRewritten.Load(),
		AsyncSkippedRecentBG:   m.asyncSkippedRecentBG.Load(),
		AsyncSkippedInFlight:   m.asyncSkippedInFlight.Load(),
		BgInFlightStaleCleared: m.bgInFlightStaleCleared.Load(),
	}
	m.pendingDepthMu.RLock()
	defer m.pendingDepthMu.RUnlock()
	snap.PendingDepth = make(map[string]int64, len(m.pendingDepth))
	for k, v := range m.pendingDepth {
		snap.PendingDepth[k] = v
	}
	return snap
}

// SortedWorkspaces 返回 PendingDepth 的稳定排序 keys（便于日志/expvar 输出）。
func (s MetricsSnapshot) SortedWorkspaces() []string {
	keys := make([]string, 0, len(s.PendingDepth))
	for k := range s.PendingDepth {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
