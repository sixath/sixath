package context

import (
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/sixath/framework/model"
)

// stubCounter 返回固定估算值，用于验证"压缩触发确实用了注入的计数器"。
type stubCounter struct {
	count int
	alpha float64
}

func (s stubCounter) Count(msgs []model.Message) int {
	if s.count > 0 {
		return s.count
	}
	return len(msgs)
}

func (s stubCounter) Alpha() float64 {
	if s.alpha <= 0 {
		return 1
	}
	return s.alpha
}

func TestConservativeCounter_MatchesLegacyEstimate(t *testing.T) {
	msgs := []model.Message{{Role: "user", Content: "你好"}}
	c := NewConservativeCounter(0) // 默认 alpha
	if got, want := c.Count(msgs), EstimateTokensConservative(msgs, DefaultTokenEstimateAlpha); got != want {
		t.Fatalf("Count=%d want %d", got, want)
	}
	if got := c.Alpha(); got != DefaultTokenEstimateAlpha {
		t.Fatalf("Alpha=%v want %v", got, DefaultTokenEstimateAlpha)
	}

	explicit := NewConservativeCounter(2.0)
	if got := explicit.Alpha(); got != 2.0 {
		t.Fatalf("Alpha=%v want 2.0", got)
	}
}

func TestCalibratedCounter_FirstSampleCorrectsAlpha(t *testing.T) {
	c := NewCalibratedCounter(nil) // base alpha = 1.35
	if c.Observed() {
		t.Fatal("fresh counter must have no samples")
	}

	// 估算 135，真实 100 → ratio 0.7407 → alpha ≈ 1.0
	c.Observe(135, 100)
	st := c.Stats()
	if st.Samples != 1 {
		t.Fatalf("Samples=%d want 1", st.Samples)
	}
	if math.Abs(st.Alpha-1.0) > 0.01 {
		t.Fatalf("Alpha=%v want ≈1.0", st.Alpha)
	}
	if math.Abs(st.RatioEMA-(100.0/135.0)) > 1e-6 {
		t.Fatalf("RatioEMA=%v want %v", st.RatioEMA, 100.0/135.0)
	}
}

func TestCalibratedCounter_ConvergesWithinTenPercent(t *testing.T) {
	const runes = 500
	msgs := []model.Message{{Role: "user", Content: strings.Repeat("x", runes)}}

	// 模拟一个"真实" tokenizer：tokens = runes × 1.1（与默认 1.35 有系统性偏差）。
	const trueAlpha = 1.1
	actual := int(math.Ceil(float64(runes) * trueAlpha))

	uncalibrated := NewConservativeCounter(0)
	beforeErr := math.Abs(float64(uncalibrated.Count(msgs)-actual)) / float64(actual)

	c := NewCalibratedCounter(nil)
	for i := 0; i < 20; i++ {
		c.Observe(c.Count(msgs), actual)
	}
	afterErr := math.Abs(float64(c.Count(msgs)-actual)) / float64(actual)

	if afterErr > 0.10 {
		t.Fatalf("calibrated error=%.3f want <=0.10 (alpha=%v)", afterErr, c.Alpha())
	}
	if afterErr >= beforeErr {
		t.Fatalf("calibration did not improve error: before=%.3f after=%.3f", beforeErr, afterErr)
	}
}

func TestCalibratedCounter_ClampsExtremeRatios(t *testing.T) {
	tiny := NewCalibratedCounter(nil)
	tiny.Observe(10000, 1) // ratio 0.0001 → 被 min 夹住
	if got := tiny.Alpha(); got != defaultCalibrationMinAlpha {
		t.Fatalf("Alpha=%v want %v", got, defaultCalibrationMinAlpha)
	}

	huge := NewCalibratedCounter(nil)
	huge.Observe(1, 10000) // ratio 10000 → 被 max 夹住
	if got := huge.Alpha(); got != defaultCalibrationMaxAlpha {
		t.Fatalf("Alpha=%v want %v", got, defaultCalibrationMaxAlpha)
	}
}

func TestCalibratedCounter_IgnoresInvalidSamples(t *testing.T) {
	c := NewCalibratedCounter(nil)
	c.Observe(0, 100)
	c.Observe(100, 0)
	c.Observe(-1, -1)
	if c.Observed() {
		t.Fatal("invalid samples must not be recorded")
	}
	if got := c.Alpha(); got != DefaultTokenEstimateAlpha {
		t.Fatalf("Alpha=%v want %v", got, DefaultTokenEstimateAlpha)
	}
}

func TestCalibratedCounter_NilReceiverIsSafe(t *testing.T) {
	var c *CalibratedCounter
	if got := c.Alpha(); got != DefaultTokenEstimateAlpha {
		t.Fatalf("Alpha=%v want default", got)
	}
	c.Observe(10, 10)
	if c.Observed() {
		t.Fatal("nil counter must report unobserved")
	}
	if st := c.Stats(); st.Samples != 0 {
		t.Fatalf("Stats=%+v", st)
	}
}

func TestCalibratedCounter_ConcurrentObserve(t *testing.T) {
	c := NewCalibratedCounter(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Observe(100, 90)
			_ = c.Alpha()
			_ = c.Count([]model.Message{{Role: "user", Content: "x"}})
		}()
	}
	wg.Wait()
	if st := c.Stats(); st.Samples != 50 {
		t.Fatalf("Samples=%d want 50", st.Samples)
	}
}

func TestObserveTokenUsage_CalibratesOnInputTokensOnly(t *testing.T) {
	c := NewCalibratedCounter(nil)
	msgs := []model.Message{{Role: "user", Content: strings.Repeat("x", 100)}} // 估算 = 135
	gen := &model.Generation{TokenUsage: &model.TokenUsage{InputTokens: 100, OutputTokens: 900}}

	ObserveTokenUsage(c, msgs, gen)

	st := c.Stats()
	want := 100.0 / 135.0 // 只看 prompt 侧，不能把 completion 算进来
	if math.Abs(st.RatioEMA-want) > 1e-6 {
		t.Fatalf("RatioEMA=%v want %v (completion tokens must not pollute calibration)", st.RatioEMA, want)
	}
}

func TestObserveTokenUsage_Noops(t *testing.T) {
	msgs := []model.Message{{Role: "user", Content: "hi"}}
	// 非校准计数器：静默忽略，不 panic。
	ObserveTokenUsage(NewConservativeCounter(0), msgs, &model.Generation{TokenUsage: &model.TokenUsage{InputTokens: 10}})
	// nil 参数组合。
	c := NewCalibratedCounter(nil)
	ObserveTokenUsage(nil, msgs, &model.Generation{TokenUsage: &model.TokenUsage{InputTokens: 10}})
	ObserveTokenUsage(c, msgs, nil)
	ObserveTokenUsage(c, msgs, &model.Generation{})
	ObserveTokenUsage(c, msgs, &model.Generation{TokenUsage: &model.TokenUsage{InputTokens: 0}})
	if c.Observed() {
		t.Fatal("no sample should have been recorded")
	}
}

func TestTokenCounterRegistry_IsolatesKeys(t *testing.T) {
	reg := NewTokenCounterRegistry()
	a := reg.Get("openai/gpt-4o")
	b := reg.Get("openai/gpt-4o")
	if a != b {
		t.Fatal("same key must return the same counter instance")
	}
	other := reg.Get("dashscope/qwen-plus")
	if a == other {
		t.Fatal("different keys must not share a counter")
	}
	a.Observe(100, 50)

	snap := reg.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len=%d want 2", len(snap))
	}
	if snap["openai/gpt-4o"].Samples != 1 {
		t.Fatalf("snapshot samples=%d want 1", snap["openai/gpt-4o"].Samples)
	}
	if snap["dashscope/qwen-plus"].Samples != 0 {
		t.Fatalf("unrelated key must stay untouched: %+v", snap["dashscope/qwen-plus"])
	}
}
