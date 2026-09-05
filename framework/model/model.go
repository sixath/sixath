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
