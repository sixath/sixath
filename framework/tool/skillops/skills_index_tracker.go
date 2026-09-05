package toolskill

import (
	"sync"
	"sync/atomic"
)

// SkillsIndexTracker 杩涚▼鍐呯淮鎶ゆ瘡涓?workspace 鐨勬妧鑳界储寮曚唬闄呰鏁帮紙A4锛夈€?
// 澶嶇洏鎴愬姛鍐欑洏鍚庣敱 SkillReviewRunner.InvalidateSkillsCache 鍥炶皟 Bump锛?
// 涓婂眰锛堝 portal锛夊彲璇诲彇 Generation 涓庝笂娆＄紦瀛樼殑涓栦唬鍙锋瘮杈冧互鍐冲畾鏄惁
// 瑙﹀彂 BuildSkillsIndex 閲嶅缓锛堝綋鍓?BuildSkillsIndex 姣忔鎵洏锛屾 tracker 涓?
// 鏈潵缂撳瓨灞傞鐣欒瀵熺偣锛涘悓鏃舵彁渚涗簨浠跺洖璋冧緵 portal 娉ㄥ叆鏃ュ織/鎸囨爣锛夈€?
//
// 褰撳墠瀹炵幇锛氳繘绋嬪唴 sync.Map + atomic.Uint64锛涘鍓湰閮ㄧ讲涓嬪悇鍓湰鐙珛璁℃暟锛?
// 涓嶄繚璇佸叏灞€涓€鑷达紙spec phase2 搂1.6 E1 宸茶鏄庤嫢闇€澶氬壇鏈竴鑷撮渶鍙︾珛 DB 鍒楋級銆?
type SkillsIndexTracker struct {
	mu    sync.Mutex
	gens  sync.Map // workspace -> *atomic.Uint64
	hooks []func(workspace string, generation uint64)
}

// NewSkillsIndexTracker 杩斿洖涓€涓柊鐨?tracker锛涢浂鍊间篃鍙洿鎺ヤ娇鐢ㄣ€?
func NewSkillsIndexTracker() *SkillsIndexTracker {
	return &SkillsIndexTracker{}
}

// Bump 閫掑鎸囧畾 workspace 鐨勪唬闄呰鏁板苟杩斿洖鏈€鏂板€硷紱瑙﹀彂 OnBump 閽╁瓙銆?
// workspace 涓虹┖鏃惰繑鍥?0 涓斾笉鍋氫换浣曚簨銆?
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

// Generation 杩斿洖 workspace 褰撳墠浠ｉ檯锛堟湭鍒濆鍖栬繑鍥?0锛夈€?
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

// OnBump 娉ㄥ唽 bump 鍥炶皟锛堢嚎绋嬪畨鍏紱鍙敞鍐屽涓級銆?
func (t *SkillsIndexTracker) OnBump(fn func(workspace string, generation uint64)) {
	if t == nil || fn == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hooks = append(t.hooks, fn)
}

// DefaultSkillsIndexTracker 杩涚▼绾у叡浜?tracker锛坧ortal worker 涓庢湭鏉ョ紦瀛樺眰鍏辩敤锛夈€?
var DefaultSkillsIndexTracker = NewSkillsIndexTracker()
