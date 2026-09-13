package model

import (
	"sort"
	"sync"
)

// TokenCounter 抽象「消息 → token 数」的估算，供上下文压缩与成本核算共用。
//
// 背景（docs/superpowers/plans/2026-09-12-maturity-hardening.md Task 4）：
// 早期实现用固定系数 alpha（DefaultTokenEstimateAlpha）把码点数换算成 token，
// 属于开环估计——误差既不可见也不会收敛。引入 TokenCounter 后，
// provider 拿到的真实 usage 可以回填校准（见 CalibratedCounter），
// 使压缩触发点从「猜」变成「有反馈的闭环」。
type TokenCounter interface {
	// Count 返回消息列表的 token 估算值。
	Count(msgs []Message) int
	// Alpha 返回当前使用的「码点 → token」系数。
	Alpha() float64
}

// ConservativeCounter 保留既有码点 × 系数粗估，作为无校准样本时的兜底实现。
type ConservativeCounter struct{ alpha float64 }

// NewConservativeCounter 创建粗估计数器；alpha<=0 时使用 DefaultTokenEstimateAlpha。
func NewConservativeCounter(alpha float64) *ConservativeCounter {
	if alpha <= 0 {
		alpha = DefaultTokenEstimateAlpha
	}
	return &ConservativeCounter{alpha: alpha}
}

func (c *ConservativeCounter) Count(msgs []Message) int {
	return EstimateTokensConservative(msgs, c.alpha)
}

func (c *ConservativeCounter) Alpha() float64 { return c.alpha }

// 校准边界：避免少量异常样本把系数推飞。
const (
	defaultCalibrationMinAlpha = 0.5
	defaultCalibrationMaxAlpha = 3.0
	// calibrationWarmup 为滑动平均前期的收敛步数。
	calibrationWarmup = 5
)

// TokenCounterStats 为可观测快照（供日志/指标使用）。
type TokenCounterStats struct {
	Samples   int     `json:"samples"`
	RatioEMA  float64 `json:"ratio_ema"`  // actual/estimated 的滑动平均
	Alpha     float64 `json:"alpha"`      // 生效系数
	BaseAlpha float64 `json:"base_alpha"` // 底层粗估系数
}

// CalibratedCounter 用真实 usage 校准 alpha 的闭环计数器。
//
// 反馈律：ratio = actualTokens / estimatedTokens，alphaEff ← clamp(alphaEff × ratio)。
//
// 之所以乘「当前生效系数」而不是 baseAlpha：估算值 = runes × alphaEff，
// 乘 ratio 后下一次估算正好等于真实值（一步收敛）；此后该轮 ratio 稳定在 1 附近，
// alphaEff 不再漂移。跨语言/跨模型偏差因此能被自动吸收。
// ratioEMA（滑动平均）只用于可观测，不参与修正，避免滞后造成振荡。
type CalibratedCounter struct {
	base     TokenCounter
	minAlpha float64
	maxAlpha float64

	mu       sync.Mutex
	samples  int
	ratioEMA float64
	alpha    float64
}

// NewCalibratedCounter 创建校准计数器；base 为 nil 时使用默认粗估计数器。
func NewCalibratedCounter(base TokenCounter) *CalibratedCounter {
	if base == nil {
		base = NewConservativeCounter(0)
	}
	return &CalibratedCounter{
		base:     base,
		minAlpha: defaultCalibrationMinAlpha,
		maxAlpha: defaultCalibrationMaxAlpha,
		alpha:    base.Alpha(),
	}
}

// Observe 记录一次「估算 vs 真实」样本。estimated/actual <= 0 时忽略。
func (c *CalibratedCounter) Observe(estimated, actual int) {
	if c == nil || estimated <= 0 || actual <= 0 {
		return
	}
	ratio := float64(actual) / float64(estimated)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.samples++
	if c.samples == 1 {
		c.ratioEMA = ratio
	} else {
		w := 1.0 / float64(min(c.samples, calibrationWarmup))
		c.ratioEMA += (ratio - c.ratioEMA) * w
	}
	c.alpha *= ratio
	if c.alpha < c.minAlpha {
		c.alpha = c.minAlpha
	}
	if c.alpha > c.maxAlpha {
		c.alpha = c.maxAlpha
	}
}

func (c *CalibratedCounter) Count(msgs []Message) int {
	return EstimateTokensConservative(msgs, c.Alpha())
}

func (c *CalibratedCounter) Alpha() float64 {
	if c == nil {
		return DefaultTokenEstimateAlpha
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.alpha
}

// Stats 返回可观测快照。
func (c *CalibratedCounter) Stats() TokenCounterStats {
	if c == nil {
		return TokenCounterStats{Alpha: DefaultTokenEstimateAlpha, BaseAlpha: DefaultTokenEstimateAlpha}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return TokenCounterStats{
		Samples:   c.samples,
		RatioEMA:  c.ratioEMA,
		Alpha:     c.alpha,
		BaseAlpha: c.base.Alpha(),
	}
}

// Observed 报告是否已有校准样本。
func (c *CalibratedCounter) Observed() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.samples > 0
}

// TokenCounterRegistry 按 key（如 "openai/gpt-4o"）维护校准计数器，
// 避免不同 provider/model 的消耗特征互相污染。
type TokenCounterRegistry struct {
	newBase func() TokenCounter

	mu       sync.Mutex
	counters map[string]*CalibratedCounter
}

// NewTokenCounterRegistry 创建注册表；每个 key 首次访问时创建计数器。
func NewTokenCounterRegistry() *TokenCounterRegistry {
	return &TokenCounterRegistry{
		newBase:  func() TokenCounter { return NewConservativeCounter(0) },
		counters: make(map[string]*CalibratedCounter),
	}
}

// Get 返回 key 对应的计数器（不存在则创建）。
func (r *TokenCounterRegistry) Get(key string) *CalibratedCounter {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counters == nil {
		r.counters = make(map[string]*CalibratedCounter)
	}
	if c, ok := r.counters[key]; ok {
		return c
	}
	c := NewCalibratedCounter(r.newBase())
	r.counters[key] = c
	return c
}

// Snapshot 返回全部 key 的统计快照（按 key 排序，便于稳定输出）。
func (r *TokenCounterRegistry) Snapshot() map[string]TokenCounterStats {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	keys := make([]string, 0, len(r.counters))
	for k := range r.counters {
		keys = append(keys, k)
	}
	counters := make(map[string]*CalibratedCounter, len(r.counters))
	for k, v := range r.counters {
		counters[k] = v
	}
	r.mu.Unlock()

	sort.Strings(keys)
	out := make(map[string]TokenCounterStats, len(keys))
	for _, k := range keys {
		out[k] = counters[k].Stats()
	}
	return out
}

// ObserveTokenUsage 用一次真实调用的 usage 校准计数器。
//
// 只校准 prompt 侧（InputTokens）与传入 messages 的估算值，保证口径一致；
// completion 的 token 不计入校准（否则会系统性抬高 ratio）。
func ObserveTokenUsage(counter TokenCounter, msgs []Message, gen *Generation) {
	if counter == nil || gen == nil || gen.TokenUsage == nil {
		return
	}
	calibrated, ok := counter.(*CalibratedCounter)
	if !ok || calibrated == nil {
		return
	}
	if gen.TokenUsage.InputTokens <= 0 {
		return
	}
	calibrated.Observe(calibrated.Count(msgs), gen.TokenUsage.InputTokens)
}
