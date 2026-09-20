package model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/tool"
)

// 重试默认值。可用 SATH_MODEL_RETRY_* 环境变量覆盖（见 withEnvOverrides）。
const (
	DefaultRetryMaxAttempts = 3
	DefaultRetryBaseDelay   = 500 * time.Millisecond
	DefaultRetryMaxDelay    = 8 * time.Second
)

// APIStatusError 表示 provider 返回的 HTTP 层失败，由各 provider 适配器构造，
// 使重试层无需了解具体 SDK 的错误类型即可判定可否重试。
type APIStatusError struct {
	Provider   string
	StatusCode int
	Message    string
	// RetryAfter 来自响应头 Retry-After（缺省为 0）。
	RetryAfter time.Duration
	Err        error
}

func (e *APIStatusError) Error() string {
	if e == nil {
		return "<nil>"
	}
	prefix := "api error"
	if provider := strings.TrimSpace(e.Provider); provider != "" {
		prefix = provider + " api error"
	}
	if msg := strings.TrimSpace(e.Message); msg != "" {
		return fmt.Sprintf("%s: status %d: %s", prefix, e.StatusCode, msg)
	}
	return fmt.Sprintf("%s: status %d", prefix, e.StatusCode)
}

func (e *APIStatusError) Unwrap() error { return e.Err }

// RetryConfig 控制模型调用的重试与退避行为。零值即"使用默认值并启用重试"。
type RetryConfig struct {
	// MaxAttempts 为总尝试次数（含首次）；<=0 时使用 DefaultRetryMaxAttempts。
	MaxAttempts int
	// BaseDelay 为首次退避时长；<=0 时使用 DefaultRetryBaseDelay。
	BaseDelay time.Duration
	// MaxDelay 为单次退避上限；<=0 时使用 DefaultRetryMaxDelay。
	MaxDelay time.Duration
	// DisableJitter 关闭 full jitter（默认启用，用于打散重试风暴）。
	DisableJitter bool
	// OnRetry 在每次重试前回调，供调用方打点/日志；可为 nil。
	OnRetry func(attempt int, err error, delay time.Duration)
}

func (c RetryConfig) normalized() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = DefaultRetryMaxAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = DefaultRetryBaseDelay
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = DefaultRetryMaxDelay
	}
	if c.MaxDelay < c.BaseDelay {
		c.MaxDelay = c.BaseDelay
	}
	return c
}

// withEnvOverrides 允许不改代码即调节重试行为（部署期调优 + 事故时快速降级）：
//
//	SATH_MODEL_RETRY_MAX_ATTEMPTS=1        关闭重试
//	SATH_MODEL_RETRY_BASE_DELAY_MS=200
//	SATH_MODEL_RETRY_MAX_DELAY_MS=4000
//	SATH_MODEL_RETRY_DISABLE_JITTER=1
func (c RetryConfig) withEnvOverrides() RetryConfig {
	if v, ok := envInt("SATH_MODEL_RETRY_MAX_ATTEMPTS"); ok {
		c.MaxAttempts = v
	}
	if v, ok := envInt("SATH_MODEL_RETRY_BASE_DELAY_MS"); ok {
		c.BaseDelay = time.Duration(v) * time.Millisecond
	}
	if v, ok := envInt("SATH_MODEL_RETRY_MAX_DELAY_MS"); ok {
		c.MaxDelay = time.Duration(v) * time.Millisecond
	}
	if v := strings.TrimSpace(os.Getenv("SATH_MODEL_RETRY_DISABLE_JITTER")); v == "1" || strings.EqualFold(v, "true") {
		c.DisableJitter = true
	}
	return c
}

// parseRetryAfterHeader 解析 Retry-After 头（秒数或 HTTP-date）；无法解析返回 0。
func parseRetryAfterHeader(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func envInt(name string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

// backoff 计算第 attempt 次失败后的等待时长（attempt 从 1 开始）。
func (c RetryConfig) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > c.MaxDelay {
			return c.MaxDelay
		}
		return retryAfter
	}
	delay := c.BaseDelay
	for i := 1; i < attempt; i++ {
		next := delay * 2
		if next <= 0 || next >= c.MaxDelay {
			delay = c.MaxDelay
			break
		}
		delay = next
	}
	if delay > c.MaxDelay {
		delay = c.MaxDelay
	}
	if !c.DisableJitter && delay > 0 {
		// full jitter: 均匀分布在 (0, delay]
		delay = time.Duration(rand.Int64N(int64(delay))) + 1
	}
	return delay
}

// retryableModelError 判定错误是否值得重试，并返回 provider 建议的等待时长。
// 父 ctx 是否结束由重试循环负责检查，这里只看错误本身。
func retryableModelError(err error) (bool, time.Duration) {
	if err == nil {
		return false, 0
	}
	if errors.Is(err, context.Canceled) {
		return false, 0
	}
	var apiErr *APIStatusError
	if errors.As(err, &apiErr) {
		return statusCodeRetryable(apiErr.StatusCode), apiErr.RetryAfter
	}
	// 注意顺序：go-openai 在响应体不是合法 JSON 时会返回
	// RequestError{HTTPStatusCode: <真实状态码>, Err: APIError{HTTPStatusCode: 0}}，
	// 因此必须先用 RequestError 的状态码判定，否则 5xx 会被误判为不可重试。
	var oaiReq *openai.RequestError
	if errors.As(err, &oaiReq) {
		if oaiReq.HTTPStatusCode > 0 {
			return statusCodeRetryable(oaiReq.HTTPStatusCode), 0
		}
		// 传输层失败（连接重置、超时、EOF）——值得重试。
		return true, 0
	}
	var oaiAPI *openai.APIError
	if errors.As(err, &oaiAPI) {
		return statusCodeRetryable(oaiAPI.HTTPStatusCode), 0
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true, 0
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) {
		return true, 0
	}
	return false, 0
}

// statusCodeRetryable 只对明确的瞬时状态码放行；400/401/403/404 重试无意义。
func statusCodeRetryable(status int) bool {
	switch {
	case status == 408, status == 409, status == 425, status == 429:
		return true
	case status >= 500 && status <= 599:
		return true
	default:
		return false
	}
}

// sleepCtx 等待 d；父 ctx 结束则提前返回其错误。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryCall 执行 fn 并在可重试错误上退避重试。
//
// 约定：一次 fn 调用必须是「幂等或尚未产生副作用」的。对流式调用这意味着只重试
// setup 阶段（首 token 之前）；一旦已产生增量，错误一律直接上报，以免重复正文。
func (c RetryConfig) retryCall(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= c.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		retryable, retryAfter := retryableModelError(lastErr)
		if !retryable || attempt == c.MaxAttempts {
			return lastErr
		}
		delay := c.backoff(attempt, retryAfter)
		if c.OnRetry != nil {
			c.OnRetry(attempt, lastErr, delay)
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return err
		}
	}
	return lastErr
}

// toolCallingModel 与 framework/agent.ToolCallingModel 结构等价（同签名即满足）。
type toolCallingModel interface {
	Model
	ChatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error)
}

// resilientCore 持有被包装模型与重试配置，并实现所有"调用一次"的转发逻辑，
// 各包装类型只负责按能力暴露方法。
type resilientCore struct {
	inner Model
	cfg   RetryConfig
}

func (c *resilientCore) generate(ctx context.Context, prompt string, opts ...Option) (*Generation, error) {
	var out *Generation
	err := c.cfg.retryCall(ctx, func(cc context.Context) error {
		gen, err := c.inner.Generate(cc, prompt, opts...)
		if err != nil {
			return err
		}
		out = gen
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resilientCore) chat(ctx context.Context, messages []Message, opts ...Option) (*Generation, error) {
	var out *Generation
	err := c.cfg.retryCall(ctx, func(cc context.Context) error {
		gen, err := c.inner.Chat(cc, messages, opts...)
		if err != nil {
			return err
		}
		out = gen
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// chatStream 只重试 setup 阶段：拿到增量 channel 后不再重试；
// 流中途错误沿用底层实现既有语义（channel 关闭）。
func (c *resilientCore) chatStream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	sm, ok := c.inner.(StreamingModel)
	if !ok {
		return nil, fmt.Errorf("model: streaming is not supported by %T", c.inner)
	}
	var out <-chan string
	err := c.cfg.retryCall(ctx, func(cc context.Context) error {
		ch, err := sm.ChatStream(cc, messages, opts...)
		if err != nil {
			return err
		}
		out = ch
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resilientCore) chatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error) {
	tm, ok := c.inner.(toolCallingModel)
	if !ok {
		return nil, fmt.Errorf("model: tool calling is not supported by %T", c.inner)
	}
	var out *Generation
	err := c.cfg.retryCall(ctx, func(cc context.Context) error {
		gen, err := tm.ChatWithTools(cc, messages, reg, opts...)
		if err != nil {
			return err
		}
		out = gen
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// chatWithToolsStream 同理只重试 setup；已产生增量后的失败经 finalGen.Err 上报，不重试。
func (c *resilientCore) chatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	tsm, ok := c.inner.(ToolCallingStreamingModel)
	if !ok {
		return nil, nil, fmt.Errorf("model: tool-calling streaming is not supported by %T", c.inner)
	}
	var (
		textCh <-chan string
		genCh  <-chan *Generation
	)
	err := c.cfg.retryCall(ctx, func(cc context.Context) error {
		tc, gc, err := tsm.ChatWithToolsStream(cc, messages, reg, opts...)
		if err != nil {
			return err
		}
		textCh, genCh = tc, gc
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return textCh, genCh, nil
}

// resilientModel 为最小包装：仅转发 Model 能力。
type resilientModel struct{ *resilientCore }

// Unwrap 返回被包装的模型，供 UnwrapModel / 测试穿透装饰器。
func (r *resilientModel) Unwrap() Model { return r.inner }

func (r *resilientModel) Generate(ctx context.Context, prompt string, opts ...Option) (*Generation, error) {
	return r.generate(ctx, prompt, opts...)
}

func (r *resilientModel) Chat(ctx context.Context, messages []Message, opts ...Option) (*Generation, error) {
	return r.chat(ctx, messages, opts...)
}

// Embed 重试是安全的：向量计算只读且幂等。
func (r *resilientModel) Embed(ctx context.Context, texts []string, opts ...Option) ([]Embedding, error) {
	var out []Embedding
	err := r.cfg.retryCall(ctx, func(cc context.Context) error {
		emb, err := r.inner.Embed(cc, texts, opts...)
		if err != nil {
			return err
		}
		out = emb
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// resilientStreamingModel 额外暴露 ChatStream（Model + StreamingModel）。
type resilientStreamingModel struct{ *resilientModel }

func (r *resilientStreamingModel) ChatStream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	return r.chatStream(ctx, messages, opts...)
}

// resilientToolCallingModel 额外暴露 ChatWithTools（Model + ToolCalling）。
type resilientToolCallingModel struct{ *resilientModel }

func (r *resilientToolCallingModel) ChatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error) {
	return r.chatWithTools(ctx, messages, reg, opts...)
}

// resilientStreamingToolCallingModel 暴露 ChatStream + ChatWithTools。
type resilientStreamingToolCallingModel struct{ *resilientStreamingModel }

func (r *resilientStreamingToolCallingModel) ChatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error) {
	return r.chatWithTools(ctx, messages, reg, opts...)
}

// resilientToolCallingStreamingModel 暴露 ChatWithTools + ChatWithToolsStream。
type resilientToolCallingStreamingModel struct{ *resilientToolCallingModel }

func (r *resilientToolCallingStreamingModel) ChatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	return r.chatWithToolsStream(ctx, messages, reg, opts...)
}

// resilientToolStreamOnlyModel 仅暴露 ChatWithToolsStream（底层不支持 ChatWithTools 时使用），
// 避免包装器凭空"增加"底层不具备的能力。
type resilientToolStreamOnlyModel struct{ *resilientModel }

func (r *resilientToolStreamOnlyModel) ChatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	return r.chatWithToolsStream(ctx, messages, reg, opts...)
}

// resilientFullModel 暴露全部能力（Model + Streaming + ToolCalling + ToolCallingStreaming）。
type resilientFullModel struct {
	*resilientStreamingToolCallingModel
}

func (r *resilientFullModel) ChatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	return r.chatWithToolsStream(ctx, messages, reg, opts...)
}

// WrapResilient 用重试/退避装饰器包装模型，并**按底层模型的实际能力**返回，
// 使 agent 侧的类型断言（StreamingModel / ToolCallingModel /
// ToolCallingStreamingModel）在包装前后行为一致。
func WrapResilient(m Model, cfg RetryConfig) Model {
	if m == nil {
		return nil
	}
	cfg = cfg.withEnvOverrides().normalized()
	core := &resilientCore{inner: m, cfg: cfg}
	base := &resilientModel{core}

	_, hasStream := m.(StreamingModel)
	_, hasTools := m.(toolCallingModel)
	_, hasToolsStream := m.(ToolCallingStreamingModel)

	switch {
	case hasStream && hasTools && hasToolsStream:
		return &resilientFullModel{
			resilientStreamingToolCallingModel: &resilientStreamingToolCallingModel{
				resilientStreamingModel: &resilientStreamingModel{base},
			},
		}
	case hasTools && hasToolsStream:
		return &resilientToolCallingStreamingModel{resilientToolCallingModel: &resilientToolCallingModel{base}}
	case hasStream && hasTools:
		return &resilientStreamingToolCallingModel{resilientStreamingModel: &resilientStreamingModel{base}}
	case hasToolsStream:
		return &resilientToolStreamOnlyModel{base}
	case hasStream:
		return &resilientStreamingModel{base}
	case hasTools:
		return &resilientToolCallingModel{base}
	default:
		return base
	}
}

// UnwrapModel 穿透所有 WrapResilient 装饰器，返回最内层模型；
// 非装饰器（或已是最内层）时原样返回。
func UnwrapModel(m Model) Model {
	for {
		u, ok := m.(interface{ Unwrap() Model })
		if !ok {
			return m
		}
		inner := u.Unwrap()
		if inner == nil || inner == m {
			return m
		}
		m = inner
	}
}
