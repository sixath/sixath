package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	fwctx "github.com/sixath/framework/context"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type ToolCallingModel interface {
	model.Model
	ChatWithTools(ctx context.Context, messages []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error)
}

type ReActAgent struct {
	model  model.Model
	mem    memory.Memory
	tools  *tool.Registry
	config ReActConfig
}

type ReActConfig struct {
	MaxSteps             int
	MaxHistory           int
	MaxContextRunes      int // 传给 context.PipelineConfig；0 表示关闭 L0 压缩
	MaxContextTokensSoft int
	TokenEstimateAlpha   float64
	MaxOutputTokens      int // 单次模型回复上限；0 使用 model 默认（1024）
	SnipCompactEnabled   bool
	SystemPrompt         string
	Workspace            string   // Agent 可写 workspace 根；PromptBuilder 读 MEMORY.md / USER.md / skills
	SkillsDirs           []string // 额外技能目录（Portal 共享 skills）
	EventBus             *events.Bus
	PermissionPolicy     PermissionPolicy
	MemoryOrchestrator   *memory.Orchestrator  // 可选；非空时在 messages 中注入 prefetch（设计 §4）
	ToolGuardrails       *ToolGuardrailsConfig // 可选；设计 §6
	// GuardrailEvaluator 非空时优先于由 ToolGuardrails 构造的默认评估器（设计 §6.2）；WithReActToolGuardrails 会将其置 nil。
	GuardrailEvaluator GuardrailEvaluator
	L2Runtime          *fwctx.L2Runtime // 可选；L2 摘要 + 冷却（设计 §5）
	// ToolSuccessHook 在工具执行成功且已发出 ToolCompleted 之后调用（可选）；用于成长计数等，须快速返回。
	ToolSuccessHook func(ctx context.Context, req *Request, rec ToolCallRecord)
	// ToolHooks 工具生命周期钩子（Before 可 block；After 与 Before 同序）。空切片与未设置行为一致。
	ToolHooks []ToolHook
	// ParallelTools 为 true 时，同轮多 tool 在无 RequiresSequential 时可并行执行（默认 false）。
	ParallelTools bool
	// MaxParallel 并行上限；<=0 时默认 8。
	MaxParallel int
}

type ReActOption func(*ReActConfig)

func WithReActMaxSteps(n int) ReActOption {
	return func(c *ReActConfig) {
		if n > 0 {
			c.MaxSteps = n
		}
	}
}

func WithReActMaxHistory(n int) ReActOption {
	return func(c *ReActConfig) {
		if n > 0 {
			c.MaxHistory = n
		}
	}
}

// WithReActMaxOutputTokens 设置单次 Chat/ChatWithTools 的 max_tokens；n<=0 使用框架默认 1024。
func WithReActMaxOutputTokens(n int) ReActOption {
	return func(c *ReActConfig) {
		if n > 0 {
			c.MaxOutputTokens = n
		}
	}
}

// WithReActMaxContextRunes 启用按字符预算的上下文压缩（见 model.CompressMessagesByRunesBudget）。n<=0 关闭。
func WithReActMaxContextRunes(n int) ReActOption {
	return func(c *ReActConfig) {
		if n > 0 {
			c.MaxContextRunes = n
		} else {
			c.MaxContextRunes = 0
		}
	}
}

func WithReActEventBus(bus *events.Bus) ReActOption {
	return func(c *ReActConfig) {
		c.EventBus = bus
	}
}

// WithReActToolSuccessHook 在单次工具调用成功完成（Allowed 且 Execute 无 error、已发布 ToolCompleted）后调用；nil 清除。
func WithReActToolSuccessHook(hook func(ctx context.Context, req *Request, rec ToolCallRecord)) ReActOption {
	return func(c *ReActConfig) {
		c.ToolSuccessHook = hook
	}
}

// WithReActToolHooks 注册工具生命周期钩子；零参数等价于空 hooks（与未设置行为一致）。
func WithReActToolHooks(hooks ...ToolHook) ReActOption {
	return func(c *ReActConfig) {
		c.ToolHooks = hooks
	}
}

func WithReActSystemPrompt(prompt string) ReActOption {
	return func(c *ReActConfig) {
		c.SystemPrompt = prompt
	}
}

func WithReActWorkspace(workspace string) ReActOption {
	return func(c *ReActConfig) {
		c.Workspace = workspace
	}
}

func WithReActSkillsDirs(dirs []string) ReActOption {
	return func(c *ReActConfig) {
		c.SkillsDirs = append([]string(nil), dirs...)
	}
}

func WithReActPermissionPolicy(policy PermissionPolicy) ReActOption {
	return func(c *ReActConfig) {
		if policy != nil {
			c.PermissionPolicy = policy
		}
	}
}

// WithReActMemoryOrchestrator 注入会话级记忆预取编排器；nil 清除。
func WithReActMemoryOrchestrator(orch *memory.Orchestrator) ReActOption {
	return func(c *ReActConfig) {
		c.MemoryOrchestrator = orch
	}
}

// ContextCompressionConfig ReAct 侧上下文 L2 选项（设计 §5、产品 §6.2 C1）；L2Enabled=false 时与未配置等价。
type ContextCompressionConfig struct {
	L2Enabled                bool
	AuxiliaryModel           model.Model
	SoftTokenEstimate        int
	MaxConsecutiveFailures   int
	CooldownSec              int
	EstimateAlpha            float64
	ToolContentPrePruneRunes int
	SnipCompactEnabled       bool
}

// WithReActContextCompression 启用 L2 摘要（须 AuxiliaryModel）；nil 或 L2Enabled=false 时关闭。
func WithReActContextCompression(cc *ContextCompressionConfig) ReActOption {
	return func(c *ReActConfig) {
		if cc == nil || !cc.L2Enabled || cc.AuxiliaryModel == nil {
			c.L2Runtime = nil
			return
		}
		soft := cc.SoftTokenEstimate
		if soft <= 0 {
			soft = 32000
		}
		mf := cc.MaxConsecutiveFailures
		if mf <= 0 {
			mf = 3
		}
		cs := cc.CooldownSec
		if cs <= 0 {
			cs = 600
		}
		alpha := cc.EstimateAlpha
		if alpha <= 0 {
			alpha = fwctx.DefaultTokenEstimateAlpha
		}
		c.L2Runtime = fwctx.NewL2Runtime(cc.AuxiliaryModel, soft, mf, cs, alpha, cc.ToolContentPrePruneRunes)
		c.MaxContextTokensSoft = soft
		c.TokenEstimateAlpha = alpha
		c.SnipCompactEnabled = cc.SnipCompactEnabled
	}
}

// WithReActToolGuardrails 启用工具护栏（R1/R2/R3）；nil 清除。默认 HardHalt=false 仅告警。
func WithReActToolGuardrails(cfg *ToolGuardrailsConfig) ReActOption {
	return func(c *ReActConfig) {
		if cfg == nil {
			c.ToolGuardrails = nil
			c.GuardrailEvaluator = nil
			return
		}
		cp := *cfg
		if cfg.IdempotentTools != nil {
			cp.IdempotentTools = append([]string(nil), cfg.IdempotentTools...)
		}
		if cfg.MutatingTools != nil {
			cp.MutatingTools = append([]string(nil), cfg.MutatingTools...)
		}
		c.ToolGuardrails = &cp
		c.GuardrailEvaluator = nil
	}
}

// WithReActGuardrailEvaluator 注入自定义护栏评估器（例如单测 spy）。须在 WithReActToolGuardrails 之后应用才会保留。
func WithReActGuardrailEvaluator(ev GuardrailEvaluator) ReActOption {
	return func(c *ReActConfig) {
		c.GuardrailEvaluator = ev
	}
}

// WithReActParallelTools 启用同轮多 tool 并行（须无 RequiresSequential）；默认 false。
func WithReActParallelTools(enabled bool) ReActOption {
	return func(c *ReActConfig) {
		c.ParallelTools = enabled
	}
}

// WithReActMaxParallel 设置并行上限；n<=0 使用默认 8。
func WithReActMaxParallel(n int) ReActOption {
	return func(c *ReActConfig) {
		c.MaxParallel = n
	}
}

func (a *ReActAgent) ParallelToolsEnabled() bool {
	return a != nil && a.config.ParallelTools
}

func (a *ReActAgent) memoryOrchestrator() *memory.Orchestrator {
	if a == nil {
		return nil
	}
	return a.config.MemoryOrchestrator
}

func (a *ReActAgent) guardrailEval() GuardrailEvaluator {
	if a == nil {
		return noopGuardrailEvaluator{}
	}
	if a.config.GuardrailEvaluator != nil {
		return a.config.GuardrailEvaluator
	}
	return NewGuardrailEvaluator(a.config.ToolGuardrails)
}

func NewReActAgent(m model.Model, mem memory.Memory, tools *tool.Registry, opts ...ReActOption) *ReActAgent {
	cfg := ReActConfig{
		MaxSteps:         3,
		MaxHistory:       10,
		PermissionPolicy: AllowAllTools(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &ReActAgent{
		model:  m,
		mem:    mem,
		tools:  tools,
		config: cfg,
	}
}

// WithReActSnipCompactEnabled 启用 snipCompact（与 L2 独立；可在未开 L2 时单独开启）。
func WithReActSnipCompactEnabled(enabled bool) ReActOption {
	return func(c *ReActConfig) {
		c.SnipCompactEnabled = enabled
	}
}

func (a *ReActAgent) modelOpts() []model.Option {
	if a.config.MaxOutputTokens > 0 {
		return []model.Option{model.WithMaxTokens(a.config.MaxOutputTokens)}
	}
	return nil
}

// modelRespondedPayload 构造 ModelResponded 事件 payload，附带 token 用量（若有）。
func modelRespondedPayload(gen model.Generation, step int) map[string]any {
	p := map[string]any{"text_length": len(gen.Text), "step": step}
	if gen.TokenUsage != nil {
		p["input_tokens"] = gen.TokenUsage.InputTokens
		p["output_tokens"] = gen.TokenUsage.OutputTokens
	}
	return p
}

func (a *ReActAgent) Run(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, nil
	}

	rid := requestID(req)
	ctx = context.WithValue(ctx, tool.ContextKeyRequestID, rid)
	trace := &RunTrace{RequestID: rid}
	bus := a.eventBus()
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
	messages, err := a.messages(ctx, req, trace)
	if err != nil {
		trace.Errors = append(trace.Errors, err.Error())
		emit(events.RunError, map[string]any{"error": err.Error()})
		return nil, runError(err, trace)
	}

	tm, ok := a.model.(ToolCallingModel)
	if !ok || a.tools == nil || !a.tools.HasTools() {
		return a.runPlain(ctx, messages, emit, trace)
	}

	var lastAnswer string
	noProgress := 0
	for step := 0; step < a.config.MaxSteps; step++ {
		emit(events.ModelInvoked, map[string]any{"message_count": len(messages), "step": step, "mode": "tools"})
		beginModelInvocation(trace, "tools")
		messages = a.prepareModelMessages(ctx, messages, trace)
		gen, err := tm.ChatWithTools(ctx, messages, a.tools, a.modelOpts()...)
		if err != nil {
			trace.Errors = append(trace.Errors, err.Error())
			emit(events.RunError, map[string]any{"error": err.Error(), "step": step})
			return nil, runError(err, trace)
		}
		emit(events.ModelResponded, modelRespondedPayload(*gen, step))

		stepInfo, _ := gen.Raw.(model.ToolStep)
		if !stepInfo.Used {
			lastAnswer = gen.Text
			_ = a.storeAssistant(ctx, lastAnswer)
			emit(events.RunCompleted, map[string]any{"text_length": len(lastAnswer), "tool_calls": len(trace.ToolCalls)})
			return responseWithTrace(lastAnswer, gen.TokenUsage, trace, messages), nil
		}

		messages = append(messages, toolRequestMessage(gen.Text, stepInfo))
		records, err := a.executeToolStep(ctx, req, step, stepInfo, trace, emit)
		trace.ToolCalls = append(trace.ToolCalls, records...)
		messages = appendToolResultMessages(messages, records)
		noProgress++
		if a.guardrailEval().Evaluate(trace.ToolCalls, noProgress, emit).Halt {
			trace.GuardrailHalt = true
			messages = appendGuardrailHaltForTrace(trace, messages)
			trace.Errors = append(trace.Errors, ErrToolGuardrailHalt.Error())
			emit(events.RunError, map[string]any{"error": ErrToolGuardrailHalt.Error(), "step": step, "guardrail_halt": true})
			return nil, runError(ErrToolGuardrailHalt, trace)
		}
		if err != nil {
			trace.Errors = append(trace.Errors, err.Error())
			emit(events.RunError, map[string]any{"error": err.Error(), "step": step})
			return nil, runError(err, trace)
		}
	}

	if lastAnswer != "" {
		_ = a.storeAssistant(ctx, lastAnswer)
		emit(events.RunCompleted, map[string]any{"text_length": len(lastAnswer), "tool_calls": len(trace.ToolCalls)})
		return responseWithTrace(lastAnswer, nil, trace, messages), nil
	}

	resp, sumErr := a.forceFinalSummary(ctx, req, messages, trace, emit)
	if sumErr != nil {
		trace.Errors = append(trace.Errors, sumErr.Error())
		emit(events.RunError, map[string]any{"error": sumErr.Error(), "forced_summary": true})
		return nil, runError(sumErr, trace)
	}
	if resp != nil {
		return resp, nil
	}

	maxErr := fmt.Errorf("react agent reached max steps: %d", a.config.MaxSteps)
	trace.Errors = append(trace.Errors, maxErr.Error())
	emit(events.RunError, map[string]any{"error": maxErr.Error()})
	return nil, runError(maxErr, trace)
}

func (a *ReActAgent) RunStream(ctx context.Context, req *Request) (<-chan string, error) {
	if req == nil {
		return nil, nil
	}

	eventsCh, err := a.RunEvents(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan string)
	go func() {
		defer close(out)
		for event := range eventsCh {
			if event.Type != StreamEventDelta || event.Text == "" {
				continue
			}
			select {
			case out <- event.Text:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (a *ReActAgent) RunEvents(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	if req == nil {
		return nil, nil
	}

	rid := requestID(req)
	ctx = context.WithValue(ctx, tool.ContextKeyRequestID, rid)
	trace := &RunTrace{RequestID: rid}
	bus := a.eventBus()
	emit := func(kind events.Kind, payload map[string]any) {
		if bus == nil {
			return
		}
		if payload == nil {
			payload = make(map[string]any)
		}
		bus.Publish(ctx, events.Event{Kind: kind, Payload: payload, RequestID: rid})
	}

	out := make(chan StreamEvent, 16)
	go func() {
		defer close(out)
		send := func(event StreamEvent) bool {
			select {
			case out <- event:
				return true
			default:
			}
			select {
			case out <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		sendError := func(err error, step int) {
			if err == nil {
				return
			}
			trace.Errors = append(trace.Errors, err.Error())
			emit(events.RunError, map[string]any{"error": err.Error(), "step": step})
			_ = send(StreamEvent{Type: StreamEventError, Error: err.Error(), Trace: trace})
		}

		emit(events.RunStarted, map[string]any{"message_count": len(req.Messages), "stream": true})
		messages, err := a.messages(ctx, req, trace)
		if err != nil {
			sendError(err, -1)
			return
		}

		_, hasTools := a.model.(ToolCallingModel)
		if !hasTools || a.tools == nil || !a.tools.HasTools() {
			a.runPlainEvents(ctx, messages, trace, emit, send, sendError)
			return
		}

		tsm, hasToolsStream := a.model.(model.ToolCallingStreamingModel)
		if !hasToolsStream {
			tm := a.model.(ToolCallingModel)
			a.runToolEventsSync(ctx, req, messages, trace, tm, emit, send, sendError)
			return
		}

		a.runToolEvents(ctx, req, messages, trace, tsm, emit, send, sendError)
	}()
	return out, nil
}

func (a *ReActAgent) executeToolStep(ctx context.Context, req *Request, step int, stepInfo model.ToolStep, trace *RunTrace, emit func(events.Kind, map[string]any)) ([]ToolCallRecord, error) {
	calls := toolCallsFromStep(stepInfo)
	if len(calls) == 0 {
		return nil, nil
	}

	if !a.shouldParallelizeToolStep(calls) {
		records := make([]ToolCallRecord, 0, len(calls))
		for _, call := range calls {
			record, err := a.executeOneToolCall(ctx, req, step, call, emit)
			records = append(records, record)
			if err != nil {
				return records, err
			}
		}
		return records, nil
	}

	if trace != nil {
		trace.ParallelTools = true
	}
	slots := make([]ToolCallRecord, len(calls))
	errs := make([]error, len(calls))
	maxPar := a.maxParallel(len(calls))
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c model.ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			record, err := a.executeOneToolCall(ctx, req, step, c, emit)
			slots[idx] = record
			errs[idx] = err
		}(i, call)
	}
	wg.Wait()

	records := make([]ToolCallRecord, 0, len(calls))
	for i := range slots {
		records = append(records, slots[i])
		if errs[i] != nil {
			return records, errs[i]
		}
	}
	return records, nil
}

func (a *ReActAgent) shouldParallelizeToolStep(calls []model.ToolCall) bool {
	if a == nil || !a.config.ParallelTools || len(calls) <= 1 || a.tools == nil {
		return false
	}
	for _, call := range calls {
		effectiveName, ok := effectiveToolNameForCall(call)
		if !ok {
			continue
		}
		tl, ok := a.tools.Get(effectiveName)
		if !ok {
			continue
		}
		if tl.RequiresSequential {
			return false
		}
	}
	return true
}

func (a *ReActAgent) maxParallel(n int) int {
	limit := 8
	if a != nil && a.config.MaxParallel > 0 {
		limit = a.config.MaxParallel
	}
	if n < limit {
		return n
	}
	return limit
}

func effectiveToolNameForCall(call model.ToolCall) (string, bool) {
	if call.Name != tool.ToolCallName {
		return call.Name, call.Name != ""
	}
	innerName, _ := call.Arguments["name"].(string)
	return innerName, innerName != ""
}

func (a *ReActAgent) runPlainEvents(
	ctx context.Context,
	messages []model.Message,
	trace *RunTrace,
	emit func(events.Kind, map[string]any),
	send func(StreamEvent) bool,
	sendError func(error, int),
) {
	emit(events.ModelInvoked, map[string]any{"message_count": len(messages), "step": -1, "mode": "plain_stream"})
	sm, hasStream := a.model.(model.StreamingModel)
	if !hasStream {
		beginModelInvocation(trace, "plain_stream")
		messages = a.prepareModelMessages(ctx, messages, trace)
		gen, err := a.model.Chat(ctx, messages, a.modelOpts()...)
		if err != nil {
			sendError(err, -1)
			return
		}
		_ = a.storeAssistant(ctx, gen.Text)
		emit(events.ModelResponded, modelRespondedPayload(*gen, -1))
		if gen.Text != "" && !send(StreamEvent{Type: StreamEventDelta, Text: gen.Text, Trace: trace}) {
			return
		}
		emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text), "stream": true})
		_ = send(streamDoneEvent(trace, messages, nil, gen.Text))
		return
	}

	beginModelInvocation(trace, "plain_stream")
	messages = a.prepareModelMessages(ctx, messages, trace)
	ch, err := sm.ChatStream(ctx, messages, a.modelOpts()...)
	if err != nil {
		sendError(err, -1)
		return
	}
	var full strings.Builder
	for s := range ch {
		full.WriteString(s)
		if !send(StreamEvent{Type: StreamEventDelta, Text: s, Trace: trace}) {
			return
		}
	}
	text := full.String()
	_ = a.storeAssistant(ctx, text)
	emit(events.ModelResponded, map[string]any{"text_length": len(text), "step": -1})
	emit(events.RunCompleted, map[string]any{"text_length": len(text), "stream": true})
	_ = send(streamDoneEvent(trace, messages, nil, text))
}

func (a *ReActAgent) runToolEventsSync(
	ctx context.Context,
	req *Request,
	messages []model.Message,
	trace *RunTrace,
	tm ToolCallingModel,
	emit func(events.Kind, map[string]any),
	send func(StreamEvent) bool,
	sendError func(error, int),
) {
	noProgress := 0
	for step := 0; step < a.config.MaxSteps; step++ {
		emit(events.ModelInvoked, map[string]any{"message_count": len(messages), "step": step, "mode": "tools"})
		beginModelInvocation(trace, "tools")
		messages = a.prepareModelMessages(ctx, messages, trace)
		gen, err := tm.ChatWithTools(ctx, messages, a.tools, a.modelOpts()...)
		if err != nil {
			sendError(err, step)
			return
		}
		emit(events.ModelResponded, modelRespondedPayload(*gen, step))

		stepInfo, _ := gen.Raw.(model.ToolStep)
		if !stepInfo.Used {
			_ = a.storeAssistant(ctx, gen.Text)
			if gen.Text != "" && !send(StreamEvent{Type: StreamEventDelta, Text: gen.Text, Trace: trace}) {
				return
			}
			emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text), "stream": true})
			_ = send(streamDoneEvent(trace, messages, nil, gen.Text))
			return
		}

		for _, call := range toolCallsFromStep(stepInfo) {
			record := ToolCallRecord{
				Step:       step,
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Arguments:  cloneArgs(call.Arguments),
			}
			if !send(StreamEvent{Type: StreamEventToolStarted, ToolCall: &record, Trace: trace}) {
				return
			}
		}

		messages = append(messages, toolRequestMessage(gen.Text, stepInfo))
		records, err := a.executeToolStep(ctx, req, step, stepInfo, trace, emit)
		trace.ToolCalls = append(trace.ToolCalls, records...)
		messages = appendToolResultMessages(messages, records)
		for i := range records {
			record := records[i]
			eventType := StreamEventToolCompleted
			if record.Blocked {
				eventType = StreamEventHookBlocked
			} else if !record.Allowed {
				eventType = StreamEventPermissionDenied
			} else if record.Error != "" {
				eventType = StreamEventToolFailed
			}
			if !send(StreamEvent{Type: eventType, ToolCall: &record, Trace: trace}) {
				return
			}
		}
		noProgress++
		if a.guardrailEval().Evaluate(trace.ToolCalls, noProgress, emit).Halt {
			trace.GuardrailHalt = true
			messages = appendGuardrailHaltForTrace(trace, messages)
			trace.Errors = append(trace.Errors, ErrToolGuardrailHalt.Error())
			emit(events.RunError, map[string]any{"error": ErrToolGuardrailHalt.Error(), "step": step, "guardrail_halt": true})
			sendError(ErrToolGuardrailHalt, step)
			return
		}
		if err != nil {
			sendError(err, step)
			return
		}
	}

	if handled, sumErr := a.forceFinalSummaryStream(ctx, req, messages, trace, emit, send); handled {
		if sumErr != nil {
			sendError(sumErr, a.config.MaxSteps)
		}
		return
	}

	maxStepsErr := fmt.Errorf("react agent reached max steps: %d", a.config.MaxSteps)
	sendError(maxStepsErr, a.config.MaxSteps)
}

func (a *ReActAgent) runToolEvents(
	ctx context.Context,
	req *Request,
	messages []model.Message,
	trace *RunTrace,
	tsm model.ToolCallingStreamingModel,
	emit func(events.Kind, map[string]any),
	send func(StreamEvent) bool,
	sendError func(error, int),
) {
	noProgress := 0
	for step := 0; step < a.config.MaxSteps; step++ {
		emit(events.ModelInvoked, map[string]any{"message_count": len(messages), "step": step, "mode": "tools_stream"})
		beginModelInvocation(trace, "tools_stream")
		messages = a.prepareModelMessages(ctx, messages, trace)
		textCh, genCh, err := tsm.ChatWithToolsStream(ctx, messages, a.tools, a.modelOpts()...)
		if err != nil {
			sendError(err, step)
			return
		}
		for s := range textCh {
			if !send(StreamEvent{Type: StreamEventDelta, Text: s, Trace: trace}) {
				return
			}
		}
		gen, err := receiveStreamGeneration(ctx, genCh)
		if err != nil {
			sendError(err, step)
			return
		}
		emit(events.ModelResponded, modelRespondedPayload(*gen, step))

		stepInfo, _ := gen.Raw.(model.ToolStep)
		if !stepInfo.Used {
			_ = a.storeAssistant(ctx, gen.Text)
			emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text), "stream": true})
			_ = send(streamDoneEvent(trace, messages, nil, gen.Text))
			return
		}

		for _, call := range toolCallsFromStep(stepInfo) {
			record := ToolCallRecord{
				Step:       step,
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Arguments:  cloneArgs(call.Arguments),
			}
			if !send(StreamEvent{Type: StreamEventToolStarted, ToolCall: &record, Trace: trace}) {
				return
			}
		}

		messages = append(messages, toolRequestMessage(gen.Text, stepInfo))
		records, err := a.executeToolStep(ctx, req, step, stepInfo, trace, emit)
		// tools_stream 路径每轮在工具后不再强制 plain 收尾：按「连续仅工具」多轮计数 R3。
		trace.ToolCalls = append(trace.ToolCalls, records...)
		messages = appendToolResultMessages(messages, records)
		for i := range records {
			record := records[i]
			eventType := StreamEventToolCompleted
			if record.Blocked {
				eventType = StreamEventHookBlocked
			} else if !record.Allowed {
				eventType = StreamEventPermissionDenied
			} else if record.Error != "" {
				eventType = StreamEventToolFailed
			}
			if !send(StreamEvent{Type: eventType, ToolCall: &record, Trace: trace}) {
				return
			}
		}
		// tools_stream 路径每轮在工具后不再强制 plain 收尾：按「连续仅工具」多轮计数 R3。
		noProgress++
		if a.guardrailEval().Evaluate(trace.ToolCalls, noProgress, emit).Halt {
			trace.GuardrailHalt = true
			messages = appendGuardrailHaltForTrace(trace, messages)
			trace.Errors = append(trace.Errors, ErrToolGuardrailHalt.Error())
			emit(events.RunError, map[string]any{"error": ErrToolGuardrailHalt.Error(), "step": step, "guardrail_halt": true})
			sendError(ErrToolGuardrailHalt, step)
			return
		}
		if err != nil {
			sendError(err, step)
			return
		}
		// 不再强制 plain 收尾：回到循环顶部，让模型带工具继续下一轮（可能再次调用工具），
		// 直到模型不再调用工具（见循环顶部 !stepInfo.Used 分支）或达到 MaxSteps 后 forceFinalSummaryStream。
	}

	if ok, sumErr := a.forceFinalSummaryStream(ctx, req, messages, trace, emit, send); ok {
		if sumErr != nil {
			sendError(sumErr, a.config.MaxSteps)
		}
		return
	}

	maxStreamErr := fmt.Errorf("react stream reached max steps: %d", a.config.MaxSteps)
	sendError(maxStreamErr, a.config.MaxSteps)
}

const ForcedFinalSummaryPrompt = "You have finished collecting tool results. Do not call any tools. Reply directly with a complete answer to the user's original question—cover every section or numbered item they asked for; do not stop after the first few headings. Use Markdown tables where appropriate. Rules: (1) Only include facts present in tool outputs above—never infer hostname/IP patterns from the user's input table or naming conventions. (2) If a host or area was not successfully queried, say so explicitly; do not fill gaps with guessed rows. (3) When summarizing config files (e.g. YAML extra_hosts), list actual entries from stdout; do not collapse them into a simplified template unless every row is verified. (4) If web_search results are in the transcript, synthesize all of them with citations (title + URL), not just the first query."

// AnswerOriginalQuestionPrompt is the MaxSteps closer: facts-only answer, no task lock.
func AnswerOriginalQuestionPrompt() string {
	return ForcedFinalSummaryPrompt
}

func (a *ReActAgent) forceFinalSummary(ctx context.Context, req *Request, messages []model.Message, trace *RunTrace, emit func(events.Kind, map[string]any)) (*Response, error) {
	if trace == nil || len(trace.ToolCalls) == 0 {
		return nil, nil
	}
	msgs := append(append([]model.Message(nil), messages...), model.Message{
		Role:    "user",
		Content: AnswerOriginalQuestionPrompt(),
	})
	emit(events.ModelInvoked, map[string]any{"message_count": len(msgs), "step": -1, "mode": "plain_summary", "forced_summary": true})
	beginModelInvocation(trace, "plain_summary")
	msgs = a.prepareModelMessages(ctx, msgs, trace)
	gen, err := a.model.Chat(ctx, msgs, a.modelOpts()...)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(gen.Text)
	if text == "" {
		return nil, nil
	}
	_ = a.storeAssistant(ctx, text)
	emit(events.RunCompleted, map[string]any{
		"text_length":    len(text),
		"tool_calls":     len(trace.ToolCalls),
		"forced_summary": true,
	})
	return responseWithTrace(text, gen.TokenUsage, trace, messages), nil
}

func (a *ReActAgent) forceFinalSummaryStream(
	ctx context.Context,
	req *Request,
	messages []model.Message,
	trace *RunTrace,
	emit func(events.Kind, map[string]any),
	send func(StreamEvent) bool,
) (handled bool, err error) {
	if trace == nil || len(trace.ToolCalls) == 0 {
		return false, nil
	}
	resp, err := a.forceFinalSummary(ctx, req, messages, trace, emit)
	if err != nil {
		return true, err
	}
	if resp == nil {
		return false, nil
	}
	if resp.Text != "" && !send(StreamEvent{Type: StreamEventDelta, Text: resp.Text, Trace: trace}) {
		return true, nil
	}
	var doneMeta map[string]any
	if resp.Metadata != nil {
		doneMeta = map[string]any{}
		if v, ok := resp.Metadata["evidence_incomplete"]; ok {
			doneMeta["evidence_incomplete"] = v
		}
		if v, ok := resp.Metadata["code_claim_mismatch"]; ok {
			doneMeta["code_claim_mismatch"] = v
		}
		if len(doneMeta) == 0 {
			doneMeta = nil
		}
	}
	_ = send(streamDoneEvent(trace, resp.Messages, doneMeta, resp.Text))
	return true, nil
}

func receiveStreamGeneration(ctx context.Context, genCh <-chan *model.Generation) (*model.Generation, error) {
	select {
	case gen, ok := <-genCh:
		if !ok || gen == nil {
			return nil, errors.New("missing streamed generation")
		}
		return gen, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *ReActAgent) executeOneToolCall(ctx context.Context, req *Request, step int, call model.ToolCall, emit func(events.Kind, map[string]any)) (ToolCallRecord, error) {
	args := cloneArgs(call.Arguments)
	record := ToolCallRecord{
		Step:       step,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Arguments:  args,
	}
	if call.RawArgumentsParseError != "" {
		record.Error = "invalid tool arguments json: " + call.RawArgumentsParseError
		emit(events.ToolFailed, map[string]any{
			"tool":         call.Name,
			"tool_call_id": call.ID,
			"error":        record.Error,
			"step":         step,
			"debug": map[string]any{
				"raw_arguments_preview": call.RawArgumentsPreview,
			},
		})
		// 参数 JSON 损坏属于可恢复错误：让模型看到 tool error 后在后续 step 自行修正并重试。
		return record, nil
	}

	effectiveName := call.Name
	effectiveArgs := args
	if call.Name == tool.ToolCallName {
		innerName, _ := args["name"].(string)
		innerArgs, _ := args["arguments"].(map[string]any)
		if innerArgs == nil {
			if raw, ok := args["arguments"].(map[string]interface{}); ok {
				innerArgs = make(map[string]any, len(raw))
				for k, v := range raw {
					innerArgs[k] = v
				}
			}
		}
		if innerName == "" {
			record.Error = "tool_call: name is required"
			emit(events.ToolFailed, map[string]any{
				"tool":         call.Name,
				"tool_call_id": call.ID,
				"error":        record.Error,
				"step":         step,
			})
			return record, nil
		}
		effectiveName = innerName
		if innerArgs == nil {
			effectiveArgs = map[string]any{}
		} else {
			effectiveArgs = innerArgs
		}
		record.ToolName = innerName
		record.Arguments = effectiveArgs
	}

	tl, ok := a.tools.Get(effectiveName)
	if !ok {
		record.Error = ErrToolNotFound.Error() + ": " + effectiveName
		return record, fmt.Errorf("%w: %s", ErrToolNotFound, effectiveName)
	}

	hookCtx := WithRequestMetadata(ctx, nil)
	if req != nil {
		hookCtx = WithRequestMetadata(ctx, req.Metadata)
	}
	hookedArgs, hookErr := runToolHooksBefore(hookCtx, a.config.ToolHooks, effectiveName, effectiveArgs)
	if hookErr != nil {
		record.Blocked = true
		record.Allowed = false
		record.Error = hookErr.Error()
		record.Decision = "hook_blocked"
		emit(events.HookBlocked, map[string]any{
			"tool":         effectiveName,
			"tool_call_id": call.ID,
			"reason":       hookErr.Error(),
			"step":         step,
		})
		return record, nil
	}
	effectiveArgs = hookedArgs
	record.Arguments = effectiveArgs

	decision := a.permissionPolicy().AllowTool(ctx, tl, effectiveArgs)
	record.Allowed = decision.Allowed
	record.Decision = decision.Reason
	if !decision.Allowed {
		record.Error = decision.Reason
		emit(events.PermissionDenied, map[string]any{"tool": effectiveName, "tool_call_id": call.ID, "reason": decision.Reason})
		return record, fmt.Errorf("%w: %s: %s", ErrToolPermissionDenied, effectiveName, decision.Reason)
	}

	start := time.Now()
	toolStartedPayload := map[string]any{"tool": effectiveName, "tool_call_id": call.ID, "input": effectiveArgs, "step": step}
	if call.ArgumentsRepaired || call.RawArgumentsParseError != "" {
		debug := map[string]any{
			"raw_arguments_preview": call.RawArgumentsPreview,
		}
		if call.ArgumentsRepaired {
			debug["tool_arguments_repaired"] = true
		}
		if call.RawArgumentsParseError != "" {
			debug["tool_arguments_parse_error"] = call.RawArgumentsParseError
		}
		toolStartedPayload["debug"] = debug
	}
	emit(events.ToolStarted, toolStartedPayload)
	result, err := tl.Execute(ctx, effectiveArgs)
	record.DurationMS = time.Since(start).Milliseconds()
	result, err = runToolHooksAfter(hookCtx, a.config.ToolHooks, effectiveName, result, err)
	record.Result = result
	if err != nil {
		record.Error = err.Error()
		emit(events.ToolFailed, map[string]any{"tool": effectiveName, "tool_call_id": call.ID, "error": err.Error(), "step": step})
		// 执行期错误与参数 JSON 损坏一致：返回 nil error，让模型在后续轮次看到 tool 结果并重试；护栏可检测重复失败（设计 §6）。
		return record, nil
	}
	emit(events.ToolCompleted, map[string]any{"tool": effectiveName, "tool_call_id": call.ID, "output": result, "step": step})
	if h := a.config.ToolSuccessHook; h != nil && req != nil && record.Error == "" && record.Allowed {
		h(ctx, req, record)
	}
	return record, nil
}

func (a *ReActAgent) runPlain(ctx context.Context, messages []model.Message, emit func(events.Kind, map[string]any), trace *RunTrace) (*Response, error) {
	emit(events.ModelInvoked, map[string]any{"message_count": len(messages), "step": -1, "mode": "plain"})
	beginModelInvocation(trace, "plain")
	messages = a.prepareModelMessages(ctx, messages, trace)
	gen, err := a.model.Chat(ctx, messages, a.modelOpts()...)
	if err != nil {
		trace.Errors = append(trace.Errors, err.Error())
		emit(events.RunError, map[string]any{"error": err.Error()})
		return nil, runError(err, trace)
	}
	_ = a.storeAssistant(ctx, gen.Text)
	emit(events.ModelResponded, modelRespondedPayload(*gen, -1))
	emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text)})
	return responseWithTrace(gen.Text, gen.TokenUsage, trace, messages), nil
}

func (a *ReActAgent) messages(ctx context.Context, req *Request, trace *RunTrace) ([]model.Message, error) {
	incoming := req.Messages
	var messages []model.Message
	if a.config.SystemPrompt != "" {
		messages = append(messages, model.Message{Role: "system", Content: a.config.SystemPrompt})
	}
	if a.mem != nil {
		if history, err := a.mem.GetRecent(ctx, a.config.MaxHistory); err == nil {
			for _, h := range history {
				messages = append(messages, h.Message)
			}
		}
	}
	messages = append(messages, incoming...)
	if orch := a.memoryOrchestrator(); orch != nil && len(incoming) > 0 {
		q := prefetchQueryFrom(ctx, req, messages)
		pref, skipReason, err := orch.PrefetchForTurn(ctx, q)
		if err != nil {
			return nil, err
		}
		if skipReason != "" {
			if trace != nil {
				trace.PrefetchSkipped = true
				trace.PrefetchSkipReason = string(skipReason)
			}
			if bus := a.eventBus(); bus != nil && req != nil {
				rid := requestID(req)
				bus.Publish(ctx, events.Event{
					Kind:      events.MemoryPrefetchSkipped,
					RequestID: rid,
					Payload: map[string]any{
						"reason": string(skipReason),
					},
				})
			}
		}
		if len(pref) > 0 {
			// 插入 system 记忆块于「本轮 user 输入」之前：即紧挨在 incoming 首条之前
			insertAt := len(messages) - len(incoming)
			if insertAt < 0 {
				insertAt = 0
			}
			out := make([]model.Message, 0, len(messages)+len(pref))
			out = append(out, messages[:insertAt]...)
			out = append(out, pref...)
			out = append(out, messages[insertAt:]...)
			messages = out
		}
	}
	return messages, nil
}

func prefetchQueryFrom(ctx context.Context, req *Request, messages []model.Message) memory.PrefetchQuery {
	q := memory.PrefetchQuery{
		Recent: append([]model.Message(nil), messages...),
	}
	if req != nil {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if strings.EqualFold(req.Messages[i].Role, "user") {
				q.UserMessage = req.Messages[i].Content
				break
			}
		}
		if req.Metadata != nil {
			if s, ok := req.Metadata["session_id"].(string); ok {
				q.SessionID = s
			}
			if s, ok := req.Metadata["agent_id"].(string); ok {
				q.AgentID = s
			}
			if s, ok := req.Metadata["workspace_root"].(string); ok {
				q.WorkspaceRoot = s
			}
			if s, ok := req.Metadata["identity"].(string); ok {
				q.Identity = s
			}
			if s, ok := req.Metadata["locale"].(string); ok {
				q.Locale = s
			}
			if s, ok := req.Metadata["user_id"].(string); ok {
				q.UserID = s
			}
		}
	}
	if ctx != nil && q.WorkspaceRoot == "" {
		if s, ok := ctx.Value(tool.ContextKeyWorkspaceRoot).(string); ok {
			q.WorkspaceRoot = s
		}
	}
	if ctx != nil && q.AgentID == "" {
		if s, ok := ctx.Value(tool.ContextKeyAgentID).(string); ok {
			q.AgentID = s
		}
	}
	if ctx != nil && q.UserID == "" {
		if s, ok := ctx.Value(tool.ContextKeyUserID).(string); ok {
			q.UserID = s
		}
	}
	if q.Identity == "" {
		// Identity 优先外部显式传入；缺省时回退到 session_id，便于多租户/会话隔离检索键。
		q.Identity = q.SessionID
	}
	return q
}

func (a *ReActAgent) storeAssistant(ctx context.Context, text string) error {
	if a.mem == nil {
		return nil
	}
	return a.mem.Add(ctx, memory.Entry{Message: model.Message{Role: "assistant", Content: text}})
}

func (a *ReActAgent) eventBus() *events.Bus {
	if a.config.EventBus != nil {
		return a.config.EventBus
	}
	return events.DefaultBus()
}

func (a *ReActAgent) permissionPolicy() PermissionPolicy {
	if a.config.PermissionPolicy != nil {
		return a.config.PermissionPolicy
	}
	return DefaultPermissionPolicy()
}

func responseWithTrace(text string, usage *model.TokenUsage, trace *RunTrace, messages []model.Message) *Response {
	resp := &Response{
		Text:     text,
		Metadata: map[string]any{"trace": trace},
		Messages: snapshotMessages(messages),
	}
	if usage != nil {
		resp.Metadata["token_input"] = usage.InputTokens
		resp.Metadata["token_output"] = usage.OutputTokens
	}
	return resp
}

func snapshotMessages(msgs []model.Message) []model.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]model.Message, len(msgs))
	copy(out, msgs)
	return out
}

func streamDoneEvent(trace *RunTrace, messages []model.Message, metadata map[string]any, finalText string) StreamEvent {
	return StreamEvent{
		Type:     StreamEventDone,
		Text:     finalText,
		Trace:    trace,
		Messages: snapshotMessages(messages),
		Metadata: metadata,
	}
}

func toolMessageContent(record ToolCallRecord) string {
	payload := map[string]any{
		"tool":   record.ToolName,
		"result": record.Result,
	}
	if record.Error != "" {
		payload["error"] = record.Error
	}
	if record.Blocked {
		payload["blocked"] = true
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprint(record.Result)
	}
	return string(b)
}

func toolRequestMessage(content string, step model.ToolStep) model.Message {
	meta := map[string]any{
		"tool_calls": toolCallsFromStep(step),
	}
	if step.ReasoningContent != "" {
		meta[model.MetadataKeyReasoningContent] = step.ReasoningContent
	}
	return model.Message{
		Role:     "assistant",
		Content:  content,
		Metadata: meta,
	}
}

func assistantHistoryMessage(content string, step model.ToolStep) model.Message {
	if step.ReasoningContent == "" {
		return model.Message{Role: "assistant", Content: content}
	}
	return model.Message{
		Role:    "assistant",
		Content: content,
		Metadata: map[string]any{
			model.MetadataKeyReasoningContent: step.ReasoningContent,
		},
	}
}

func appendToolResultMessages(messages []model.Message, records []ToolCallRecord) []model.Message {
	for _, record := range records {
		messages = append(messages, model.Message{
			Role:    "tool",
			Content: toolMessageContent(record),
			Metadata: map[string]any{
				"tool_name":    record.ToolName,
				"tool_call_id": record.ToolCallID,
			},
		})
	}
	return messages
}

func toolCallsFromStep(step model.ToolStep) []model.ToolCall {
	if len(step.ToolCalls) > 0 {
		return step.ToolCalls
	}
	if !step.Used {
		return nil
	}
	return []model.ToolCall{{
		ID:        step.ToolCallID,
		Name:      step.ToolName,
		Arguments: step.Arguments,
	}}
}

func cloneArgs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isPermissionDenied(err error) bool {
	return errors.Is(err, ErrToolPermissionDenied)
}
