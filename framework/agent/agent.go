package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
)

// Request 表示对 Agent 的一次请求，V0.1 仅关注文本消息。
type Request struct {
	Messages  []model.Message
	Metadata  map[string]any
	RequestID string // 可选，用于事件关联与审计；为空时 Run 内可自动生成。

	// 高频字段（优先于 Metadata 中同名 key；Normalize() 会双向同步）。
	AgentName    string
	UserID       string
	ModelName    string
	Temperature  float32
	SystemPrompt string
}

// Response 表示 Agent 的一次回复。
type Response struct {
	Text     string
	Metadata map[string]any
	Usage    Usage

	// Messages is the in-memory conversation after this Run (may include tool roles).
	// Used by Portal background review; omit from large log dumps.
	Messages []model.Message `json:"-"`
}

// Agent 定义统一的 Agent 接口。
type Agent interface {
	Run(ctx context.Context, req *Request) (*Response, error)
}

// StreamableAgent 支持流式输出的 Agent 接口。
// RunStream 返回增量文本 channel，实现方在完成或错误时关闭 ch。
type StreamableAgent interface {
	Agent
	RunStream(ctx context.Context, req *Request) (<-chan string, error)
}

type EventStreamableAgent interface {
	StreamableAgent
	RunEvents(ctx context.Context, req *Request) (<-chan StreamEvent, error)
}

// ChatAgent 是 V0.1 的默认对话 Agent 实现。
type ChatAgent struct {
	model  model.Model
	mem    memory.Memory
	config ChatConfig
}

// ChatConfig 控制 ChatAgent 的行为。
type ChatConfig struct {
	MaxHistory int
	EventBus   *events.Bus // 可选；非空时在生命周期关键点发布事件。
}

// Option 为 ChatAgent 提供可选配置。
type Option func(*ChatConfig)

func WithMaxHistory(n int) Option {
	return func(c *ChatConfig) {
		if n > 0 {
			c.MaxHistory = n
		}
	}
}

// WithEventBus 设置用于发布生命周期事件的总线；为 nil 时不发布事件。
func WithEventBus(bus *events.Bus) Option {
	return func(c *ChatConfig) {
		c.EventBus = bus
	}
}

// NewChatAgent 创建一个默认对话 Agent。
func NewChatAgent(m model.Model, mem memory.Memory, opts ...Option) *ChatAgent {
	cfg := ChatConfig{
		MaxHistory: 10,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &ChatAgent{
		model:  m,
		mem:    mem,
		config: cfg,
	}
}

func requestID(req *Request) string {
	if req != nil && req.RequestID != "" {
		return req.RequestID
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return ""
}

// Run 将历史记忆与当前请求合并后调用底层模型，并在配置了 EventBus 时发布生命周期事件。
func (a *ChatAgent) Run(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, nil
	}
	ctx, ac := EnsureContext(ctx)
	req.Normalize()
	if ac != nil {
		ac.AgentName = req.EffectiveAgentName()
		ac.UserID = req.UserID
		ac.ModelName = req.ModelName
		if ac.StartTime.IsZero() {
			ac.StartTime = time.Now()
		}
	}

	rid := requestID(req)
	bus := a.config.EventBus
	if bus == nil {
		bus = events.DefaultBus()
	}

	emit := func(kind events.Kind, payload map[string]any) {
		if bus == nil {
			return
		}
		if payload == nil {
			payload = make(map[string]any)
		}
		bus.Publish(ctx, events.Event{Kind: kind, Payload: payload, RequestID: rid})
	}

	emit(events.RunStarted, map[string]any{"message_count": len(req.Messages)})

	history, _ := a.mem.GetRecent(ctx, a.config.MaxHistory)
	var messages []model.Message
	for _, h := range history {
		messages = append(messages, h.Message)
	}
	messages = append(messages, req.Messages...)

	emit(events.ModelInvoked, map[string]any{"message_count": len(messages)})
	gen, err := a.model.Chat(ctx, messages)
	if err != nil {
		emit(events.RunError, map[string]any{"error": err.Error()})
		return nil, err
	}

	emit(events.ModelResponded, map[string]any{"text_length": len(gen.Text)})

	_ = a.mem.Add(ctx, memory.Entry{
		Message: model.Message{
			Role:    "assistant",
			Content: gen.Text,
		},
	})

	emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text)})
	resp := &Response{Text: gen.Text}
	if gen.TokenUsage != nil {
		resp.Usage.InputTokens = int64(gen.TokenUsage.InputTokens)
		resp.Usage.OutputTokens = int64(gen.TokenUsage.OutputTokens)
		resp.SyncUsageToMetadata()
	}
	return resp, nil
}

// RunStream 流式运行；当 model 实现 StreamingModel 时使用真实流式，否则退化为同步后单块发送。
func (a *ChatAgent) RunStream(ctx context.Context, req *Request) (<-chan string, error) {
	if req == nil {
		return nil, nil
	}

	rid := requestID(req)
	bus := a.config.EventBus
	if bus == nil {
		bus = events.DefaultBus()
	}
	emit := func(kind events.Kind, payload map[string]any) {
		if bus == nil {
			return
		}
		if payload == nil {
			payload = make(map[string]any)
		}
		bus.Publish(ctx, events.Event{Kind: kind, Payload: payload, RequestID: rid})
	}
	emit(events.RunStarted, map[string]any{"message_count": len(req.Messages)})

	history, _ := a.mem.GetRecent(ctx, a.config.MaxHistory)
	var messages []model.Message
	for _, h := range history {
		messages = append(messages, h.Message)
	}
	messages = append(messages, req.Messages...)

	emit(events.ModelInvoked, map[string]any{"message_count": len(messages)})

	sm, ok := a.model.(model.StreamingModel)
	if !ok {
		gen, err := a.model.Chat(ctx, messages)
		if err != nil {
			emit(events.RunError, map[string]any{"error": err.Error()})
			return nil, err
		}
		ch := make(chan string, 1)
		go func() {
			defer close(ch)
			if gen.Text != "" {
				ch <- gen.Text
			}
			_ = a.mem.Add(ctx, memory.Entry{Message: model.Message{Role: "assistant", Content: gen.Text}})
			emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text)})
		}()
		return ch, nil
	}

	ch, err := sm.ChatStream(ctx, messages)
	if err != nil {
		emit(events.RunError, map[string]any{"error": err.Error()})
		return nil, err
	}

	out := make(chan string)
	go func() {
		defer close(out)
		var full strings.Builder
		for s := range ch {
			full.WriteString(s)
			select {
			case out <- s:
			case <-ctx.Done():
				return
			}
		}
		text := full.String()
		_ = a.mem.Add(ctx, memory.Entry{Message: model.Message{Role: "assistant", Content: text}})
		emit(events.ModelResponded, map[string]any{"text_length": len(text)})
		emit(events.RunCompleted, map[string]any{"text_length": len(text)})
	}()
	return out, nil
}
