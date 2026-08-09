package growth

import (
	"sync"
	"sync/atomic"
)

// SkillsIndexTracker 进程内维护每个 workspace 的技能索引代际计数（A4）。
// 复盘成功写盘后由 SkillReviewRunner.InvalidateSkillsCache 回调 Bump，
// 上层（如 portal）可读取 Generation 与上次缓存的世代号比较以决定是否
// 触发 BuildSkillsIndex 重建（当前 BuildSkillsIndex 每次扫盘，此 tracker 为
// 未来缓存层预留观察点；同时提供事件回调供 portal 注入日志/指标）。
//
// 当前实现：进程内 sync.Map + atomic.Uint64；多副本部署下各副本独立计数，
// 不保证全局一致（spec phase2 §1.6 E1 已说明若需多副本一致需另立 DB 列）。
type SkillsIndexTracker struct {
	mu    sync.Mutex
	gens  sync.Map // workspace -> *atomic.Uint64
	hooks []func(workspace string, generation uint64)
}

// NewSkillsIndexTracker 返回一个新的 tracker；零值也可直接使用。
func NewSkillsIndexTracker() *SkillsIndexTracker {
	return &SkillsIndexTracker{}
}

// Bump 递增指定 workspace 的代际计数并返回最新值；触发 OnBump 钩子。
// workspace 为空时返回 0 且不做任何事。
func (t *SkillsIndexTracker) Bump(workspace string) uint64 {
	if t == nil || workspace == "" {
		return 0
	}
	v, _ := t.gens.LoadOrStore(workspace, new(atomic.Uint64))
	gen := v.(*atomic.Uint64).Add(1)
	t.mu.Lock()
	hooks := make([]func(string, uint64), len(t.hooks))
	copy(hooks, t.hooks)
	t.mu.Unlock()
	for _, h := range hooks {
		h(workspace, gen)
	}
	return gen
}

// Generation 返回 workspace 当前代际（未初始化返回 0）。
func (t *SkillsIndexTracker) Generation(workspace string) uint64 {
	if t == nil || workspace == "" {
		return 0
	}
	v, ok := t.gens.Load(workspace)
	if !ok {
		return 0
	}
	return v.(*atomic.Uint64).Load()
}

// OnBump 注册 bump 回调（线程安全；可注册多个）。
func (t *SkillsIndexTracker) OnBump(fn func(workspace string, generation uint64)) {
	if t == nil || fn == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hooks = append(t.hooks, fn)
}

// DefaultSkillsIndexTracker 进程级共享 tracker（portal worker 与未来缓存层共用）。
var DefaultSkillsIndexTracker = NewSkillsIndexTracker()
