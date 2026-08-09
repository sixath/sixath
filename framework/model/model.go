package model

import (
	"context"

	"github.com/sixath/framework/tool"
)

// ContentType 表示多模态消息中单个内容块的类型。
type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeImageURL ContentType = "image_url"
	ContentTypeAudioURL ContentType = "audio_url"
	ContentTypeVideoURL ContentType = "video_url"
)

// ContentPart 表示多模态消息中的一个内容块。
type ContentPart struct {
	Type     ContentType
	Text     string
	URL      string
	Metadata map[string]any
}

// Message 表示单条对话消息。
type Message struct {
	Role     string
	Content  string
	Parts    []ContentPart
	Metadata map[string]any
}

// TokenUsage 表示一次调用的 token 消耗。
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// Generation 表示一次模型生成结果。
type Generation struct {
	Text       string
	Raw        any
	TokenUsage *TokenUsage
	// Err 非空表示流式调用在 setup 成功后失败（如 Recv 网络错误）。
	// ChatWithToolsStream 通过 finalGenCh 传递，避免 channel 空关闭被误判为 missing generation。
	Err error
}

// Embedding 向量表示。
type Embedding struct {
	Vector []float32
	Raw    any
}

// ToolStep 表示一次基于 tools API 的决策结果。
type ToolCall struct {
	ID                     string
	Name                   string
	Arguments              map[string]any
	ArgumentsRepaired      bool
	RawArgumentsPreview    string
	RawArgumentsParseError string
}

type ToolStep struct {
	Used             bool
	ToolCallID       string
	ToolName         string
	Arguments        map[string]any
	ToolCalls        []ToolCall
	Observation      any
	Error            string
	ReasoningContent string // thinking 模式 API 返回的 reasoning_content，下一轮必须回传
}

// Model 定义统一的模型接口。
type Model interface {
	Generate(ctx context.Context, prompt string, opts ...Option) (*Generation, error)
	Chat(ctx context.Context, messages []Message, opts ...Option) (*Generation, error)
	Embed(ctx context.Context, texts []string, opts ...Option) ([]Embedding, error)
}

// StreamingModel 支持流式输出的模型接口。
type StreamingModel interface {
	Model
	ChatStream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error)
}

// ToolCallingStreamingModel 支持「带工具的流式」模型接口。
type ToolCallingStreamingModel interface {
	Model
	// ChatWithToolsStream 流式执行带工具的对话。
	// 返回 (textStream, finalGenCh, err)：finalGenCh 在 stream 结束后收到 *Generation。
	ChatWithToolsStream(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (textStream <-chan string, finalGenCh <-chan *Generation, err error)
}

// Option 为模型调用提供可选参数。
type Option func(*CallConfig)

// CallConfig 保存一次模型调用的可选配置。
type CallConfig struct {
	Temperature float32
	MaxTokens   int
	ModelName   string
	// MaxContextRunes 若 >0，则在调用前按 Unicode 码点近似压缩 messages（见 CompressMessagesByRunesBudget）。
	MaxContextRunes int
	// MaxContextTokensSoft 若 >0，则以保守 token 粗估触发 L0 压缩（与 MaxContextRunes 独立开关，可并存）。
	MaxContextTokensSoft int
	// TokenEstimateAlpha 为 token 粗估系数（<=0 使用 DefaultTokenEstimateAlpha）。
	TokenEstimateAlpha float64
	// ContextTrace 若非空，在 PrepareChatContext（压缩、strip 孤儿 tool）时回调，供可观测聚合（即 TraceSink）。
	ContextTrace ContextTraceFunc
	// L2 可选摘要运行时；非空且启用时在 PrepareChatContextCtx 末尾尝试 MaybeSummarize（设计 §5）。
	L2 *L2Runtime
	// SnipCompactEnabled 为 true 时在 L2 预剪枝前移除 superseded 的 tool 链（Claude Code snipCompact）。
	SnipCompactEnabled bool
}

// ApplyOptions 根据可选参数构建最终配置。
func ApplyOptions(opts ...Option) *CallConfig {
	cfg := &CallConfig{
		Temperature: 0.7,
		MaxTokens:   1024,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

func WithTemperature(t float32) Option {
	return func(c *CallConfig) {
		c.Temperature = t
	}
}

func WithMaxTokens(n int) Option {
	return func(c *CallConfig) {
		if n > 0 {
			c.MaxTokens = n
		}
	}
}

func WithModelName(name string) Option {
	return func(c *CallConfig) {
		if name != "" {
			c.ModelName = name
		}
	}
}

// WithMaxContextRunes 设置上下文字符预算；超过时裁剪较早「用户轮」与左侧工具链（见 context_budget.go）。0 表示关闭。
func WithMaxContextRunes(n int) Option {
	return func(c *CallConfig) {
		if n > 0 {
			c.MaxContextRunes = n
		}
	}
}

// WithMaxContextTokensSoft 设置上下文 token 软阈值（保守粗估）；>0 时触发 L0 近似压缩。
func WithMaxContextTokensSoft(n int) Option {
	return func(c *CallConfig) {
		if n > 0 {
			c.MaxContextTokensSoft = n
		}
	}
}

// WithTokenEstimateAlpha 设置 token 粗估系数；<=0 时保持默认。
func WithTokenEstimateAlpha(alpha float64) Option {
	return func(c *CallConfig) {
		if alpha > 0 {
			c.TokenEstimateAlpha = alpha
		}
	}
}

// WithContextTrace 注册上下文变换回调（压缩、strip 等），与 WithMaxContextRunes 等一并传入 Chat / ChatWithTools。
func WithContextTrace(fn ContextTraceFunc) Option {
	return func(c *CallConfig) {
		c.ContextTrace = fn
	}
}

// WithTraceSink 注册可观测 TraceSink（与 WithContextTrace 等价，便于按规格 O2 检索 API）。
func WithTraceSink(sink TraceSink) Option {
	return func(c *CallConfig) {
		c.ContextTrace = sink
	}
}

// WithL2Runtime 注入 L2 摘要运行时（与 WithContextTrace 配合）；nil 清除。
func WithL2Runtime(r *L2Runtime) Option {
	return func(c *CallConfig) {
		c.L2 = r
	}
}

// WithSnipCompactEnabled 启用 snipCompact：L2 前按工具引用关系移除 superseded 的 tool 链。
func WithSnipCompactEnabled(enabled bool) Option {
	return func(c *CallConfig) {
		c.SnipCompactEnabled = enabled
	}
}
