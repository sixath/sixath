package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sixath/framework/events"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RegisterToolOptions 用于注册工具时的可选覆盖（如按数据源类型的描述文案）。
// 若 Description 非空，则覆盖该工具的默认 Description（模型可见）。
type RegisterToolOptions struct {
	Description string
}

// ExecuteFunc 是工具的执行函数签名。
type ExecuteFunc func(ctx context.Context, params map[string]any) (any, error)

// Tool 描述一个可被 Agent 调用的工具。
type Tool struct {
	Name        string
	Description string
	// Parameters 描述参数结构，遵循 JSON Schema 风格（V0.1 可简化为任意 map）。
	Parameters any
	Execute    ExecuteFunc
	// Toolset 逻辑分组，与 Hermes toolsets 命名对齐（如 ToolsetWeb、ToolsetFile）；用于 ListByToolsets。
	// 若为空，Register 会按内置工具名填入默认值（见 toolset.go）；未知名则保持为空。
	Toolset string
	// CheckFn 运行时门控；返回 non-nil error 时该工具不出 API schema（见 ListForAPI）。
	CheckFn func(ctx context.Context) error
	// SearchHints 额外 BM25 检索词（中/英）；Register 时可为空，由 BuiltinHintProvider 补充。
	SearchHints []string
	// AlwaysLoad 为 true 时强制 inline，忽略 defer 策略。
	AlwaysLoad bool
	// RequiresSequential 为 true 时，同轮 tool_calls 中若出现该工具则整轮串行（写冲突 / 交互 / 共享会话态）。
	RequiresSequential bool
	// Bindings 运行时绑定摘要，供 catalog / prompt 展示。
	Bindings map[string]string
}

// ContextKeyRequestID 为从 context 中读取 request_id 的键，与 templates 中 WithValue("request_id", ...) 一致。
const ContextKeyRequestID = "request_id"

// ContextKeyWorkspaceRoot 为从 context 中读取 workspace 根路径的键，供 memory_search、memory_get、read_workspace_file 等工具使用。
const ContextKeyWorkspaceRoot = "workspace_root"

// ContextKeyAgentID 为从 context 中读取 agent_id 的键，供 memory_search 等按 Agent 获取管理器使用。
const ContextKeyAgentID = "agent_id"

// ContextKeyAgentName 为 Agent 显示名 / 路由名（如 zone-4100-agent），供 P3-E pilot 匹配。
const ContextKeyAgentName = "agent_name"

// ContextKeySessionID 为当前对话 session_id，供 session_search 排除本会话等。
const ContextKeySessionID = "session_id"

// ContextKeyUserID 为当前用户 id，供 memory_remember/recall(scope=user) 与 Prefetch 使用。
const ContextKeyUserID = "user_id"

// ContextKeySkillManageUIConfirm marks a privileged UI confirm path for skill_manage.
// When SkillManageConfig.RequireUIConfirm is true, confirm_token is rejected unless this is set.
const ContextKeySkillManageUIConfirm = "skill_manage_ui_confirm"

// WithSkillManageUIConfirm binds the UI-confirm privilege onto ctx (portal ApplySkillManageConfirm).
func WithSkillManageUIConfirm(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ContextKeySkillManageUIConfirm, true)
}

// SkillManageUIConfirmAllowed reports whether ctx carries the UI-confirm privilege.
func SkillManageUIConfirmAllowed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	ok, _ := ctx.Value(ContextKeySkillManageUIConfirm).(bool)
	return ok
}

// ContextKeyToolCallID 为当前正在执行的 tool_call id，供 ask_user 等写入 pending。
const ContextKeyToolCallID = "tool_call_id"

// ContextKeyReasoningContent 为当轮模型 reasoning_content，供 ask_user pending 跨请求回放。
const ContextKeyReasoningContent = "reasoning_content"

// ContextKeyEnabledToolsets 为启用的 toolset 白名单（[]string）；空则 ListForAPI 不过滤 toolset。
const ContextKeyEnabledToolsets = "enabled_toolsets"

// ContextKeyRunKind 会话运行类型：省略或 "chat" 为普通对话；"cron" 为定时任务执行会话（spec §5.7.1）。
const ContextKeyRunKind = "run_kind"

// ContextKeyAllowCronCreate 是否允许 cronjob create；cron 执行会话应为 false。
const ContextKeyAllowCronCreate = "allow_cron_create"

// ContextKeySkipMemory 为 true 时跳过 memory 同步（cron 执行会话）。
const ContextKeySkipMemory = "skip_memory"

// ContextKeySkipGrowthReview 为 true 时跳过 Growth 复盘/计数（cron 执行会话）。
const ContextKeySkipGrowthReview = "skip_growth_review"

// ContextKeyToolCatalog 为当前对话的工具目录快照（tool.ToolCatalog），供 list_tools / ask_user 守卫使用。
const ContextKeyToolCatalog = "tool_catalog"

// ContextKeyToolSearchActive 为 true 时 ListForAPIWithDefer 从 schema 中排除 deferred 工具。
const ContextKeyToolSearchActive = "tool_search_active"

var tracer = otel.Tracer("github.com/sixath/framework/tool")

// Registry 维护一组可用工具。
type Registry struct {
	mu           sync.RWMutex
	tools        map[string]Tool
	mcpServerIDs map[string]struct{} // 已注册的 MCP 服务 ID，用于按 Skill 使用时幂等注册
	eventBus     *events.Bus         // 可选；非空时在每次工具执行后发布 ToolExecuted
}

// NewRegistry 默认注册http工具
func NewRegistry() *Registry {
	reg := &Registry{
		tools:        make(map[string]Tool),
		mcpServerIDs: make(map[string]struct{}),
	}
	err := RegisterHTTPTool(reg)
	if err != nil {
		return nil
	}
	return reg
}

func (r *Registry) HasTools() bool {
	return len(r.tools) > 0
}

// Names 返回已注册工具名（无序），用于测试与诊断。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	return out
}

// HasMcpServer 返回该 MCP 服务 ID 是否已在本 Registry 中注册过（用于按 Skill 动态注册时去重）。
func (r *Registry) HasMcpServer(id string) bool {
	if id == "" {
		return false
	}
	r.mu.RLock()
	_, ok := r.mcpServerIDs[id]
	r.mu.RUnlock()
	return ok
}

// MarkMcpServer 标记某 MCP 服务 ID 已在本 Registry 注册，调用方应在成功注册 MCP 工具后调用。
func (r *Registry) MarkMcpServer(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	if r.mcpServerIDs == nil {
		r.mcpServerIDs = make(map[string]struct{})
	}
	r.mcpServerIDs[id] = struct{}{}
	r.mu.Unlock()
}

// SetEventBus 设置事件总线；非空时，工具执行前发布 events.ToolInvoked、执行后发布 events.ToolExecuted（RequestID 从 context 的 ContextKeyRequestID 读取）。
func (r *Registry) SetEventBus(b *events.Bus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventBus = b
}

// Register 向注册表中添加一个工具。若已设置 EventBus，则包装 Execute：执行前发布 ToolInvoked，执行后发布 ToolExecuted（含 input/output）。
func (r *Registry) Register(t Tool) error {
	if t.Name == "" {
		return errors.New("tool name is empty")
	}
	if t.Execute == nil {
		return errors.New("tool execute is nil")
	}
	if t.Toolset == "" {
		if s, ok := builtinDefaultToolset[t.Name]; ok {
			t.Toolset = s
		}
	}
	if !t.RequiresSequential {
		if seq, ok := builtinRequiresSequential[t.Name]; ok {
			t.RequiresSequential = seq
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name]; exists {
		return errors.New("tool already registered: " + t.Name)
	}
	bus := r.eventBus
	name := t.Name
	orig := t.Execute
	if bus != nil {
		t.Execute = func(ctx context.Context, params map[string]any) (any, error) {
			// 为每次工具调用创建 trace span，便于调用链追踪。
			ctx, span := tracer.Start(ctx, "tool."+name, trace.WithSpanKind(trace.SpanKindInternal))
			span.SetAttributes(attribute.String("tool.name", name))
			rid, _ := ctx.Value(ContextKeyRequestID).(string)
			if rid != "" {
				span.SetAttributes(attribute.String("request.id", rid))
			}

			input := any(params)
			if params == nil {
				input = map[string]any{}
			}
			span.SetAttributes(attribute.String("tool.input.type", fmt.Sprintf("%T", input)))

			bus.Publish(ctx, events.Event{
				Kind:      events.ToolInvoked,
				RequestID: rid,
				Payload:   map[string]any{"tool": name, "input": input},
			})

			result, err := orig(ctx, params)

			span.SetAttributes(attribute.String("tool.output.type", fmt.Sprintf("%T", result)))
			if err != nil {
				span.RecordError(err)
				span.SetAttributes(attribute.Bool("tool.error", true))
			}
			defer span.End()

			payload := map[string]any{"tool": name, "input": input, "output": result}
			if err != nil {
				payload["error"] = err.Error()
			}
			bus.Publish(ctx, events.Event{Kind: events.ToolExecuted, RequestID: rid, Payload: payload})
			return result, err
		}
	}
	r.tools[t.Name] = t
	return nil
}

// Get 根据名称获取工具。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List 返回当前注册的所有工具。
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}
