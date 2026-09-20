package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/sixath/framework/model"
)

const (
	defaultMaxPlanRepairs = 2 // planner 输出非法 JSON 时的修复重试次数
	defaultMaxReplans     = 2 // 步骤失败后的重规划次数上限
)

// PlanExecuteAgent 将复杂任务拆解为「结构化规划 + 逐步执行」：
//  1. planner 产出 JSON 结构的 Plan（非法则修复重试，仍失败回退 ReAct）
//  2. executor 按步执行 worker，每步做轻量校验（非空输出，或可选 verifier 模型自评）
//  3. 步骤失败触发 replan（上限 MaxReplans），仍失败回退 ReAct
type PlanExecuteAgent struct {
	planner    model.Model
	worker     Agent
	verifier   model.Model // 可选：模型自评步骤是否满足 SuccessCriteria
	maxReplans int
	maxRepairs int
}

// PlanExecuteOption 配置 PlanExecuteAgent。
type PlanExecuteOption func(*PlanExecuteAgent)

// WithPlanVerifier 设置步骤自评模型（nil 则用「非空输出即成功」的轻量判定）。
func WithPlanVerifier(v model.Model) PlanExecuteOption {
	return func(p *PlanExecuteAgent) { p.verifier = v }
}

// WithPlanMaxReplans 设置步骤失败后的重规划上限（<=0 则关闭 replan，直接回退 ReAct）。
func WithPlanMaxReplans(n int) PlanExecuteOption {
	return func(p *PlanExecuteAgent) {
		if n >= 0 {
			p.maxReplans = n
		}
	}
}

// NewPlanExecuteAgent 构造 Plan-Execute Agent。
func NewPlanExecuteAgent(planner model.Model, worker Agent, opts ...PlanExecuteOption) *PlanExecuteAgent {
	p := &PlanExecuteAgent{
		planner:    planner,
		worker:     worker,
		maxReplans: defaultMaxReplans,
		maxRepairs: defaultMaxPlanRepairs,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *PlanExecuteAgent) Run(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, nil
	}
	plan, err := p.generatePlan(ctx, req)
	if err != nil {
		// 规划彻底失败 → 回退 ReAct：交给 worker 直接执行原始请求。
		return p.worker.Run(ctx, req)
	}

	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	req.Metadata["plan"] = plan
	req.Metadata["plan_mode"] = "plan"

	return p.execute(ctx, req, plan)
}

// generatePlan 产出结构化 Plan；非法输出时带错误回喂 planner 修复重试。
func (p *PlanExecuteAgent) generatePlan(ctx context.Context, req *Request) (*Plan, error) {
	if p.planner == nil {
		return nil, fmt.Errorf("plan agent: no planner configured")
	}
	prompt := buildPlanPrompt(req)
	var lastErr error
	for attempt := 0; attempt <= p.maxRepairs; attempt++ {
		gen, err := p.planner.Generate(ctx, prompt)
		if err != nil {
			return nil, err
		}
		plan, perr := ParsePlan(gen.Text)
		if perr == nil {
			return plan, nil
		}
		lastErr = perr
		prompt = buildRepairPrompt(req, gen.Text, perr)
	}
	return nil, lastErr
}

// execute 逐步执行；步骤失败触发 replan，超出上限回退 ReAct。
func (p *PlanExecuteAgent) execute(ctx context.Context, req *Request, plan *Plan) (*Response, error) {
	steps := plan.Steps
	var lastResp *Response
	replans := 0

	for len(steps) > 0 {
		step := steps[0]
		ok, resp, err := p.runStep(ctx, step)
		if err != nil {
			return nil, err // 硬错误（含 ctx 取消）直接上抛
		}
		if resp != nil {
			lastResp = resp
		}
		if ok {
			steps = steps[1:]
			continue
		}
		if replans >= p.maxReplans {
			return p.worker.Run(ctx, req) // replan 用尽 → 回退 ReAct
		}
		replans++
		newPlan, perr := p.replan(ctx, req, step, resp)
		if perr != nil {
			return p.worker.Run(ctx, req) // replan 失败 → 回退 ReAct
		}
		steps = newPlan.Steps
	}

	if lastResp == nil {
		return &Response{Text: "", Metadata: map[string]any{"plan": plan}}, nil
	}
	if lastResp.Metadata == nil {
		lastResp.Metadata = map[string]any{}
	}
	lastResp.Metadata["plan"] = plan
	return lastResp, nil
}

// RunStream 流式运行：产出 plan + 各步 + 最终文本的文本流（供 StreamableAgent 兼容）。
func (p *PlanExecuteAgent) RunStream(ctx context.Context, req *Request) (<-chan string, error) {
	evCh, err := p.RunEvents(ctx, req)
	if err != nil {
		return nil, err
	}
	if evCh == nil {
		return nil, nil
	}
	out := make(chan string, 8)
	go func() {
		defer close(out)
		for ev := range evCh {
			if ev.Text == "" {
				continue
			}
			select {
			case out <- ev.Text:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// RunEvents 流式运行：先发 plan 事件，再逐步发 plan_step 事件，最后发最终文本与 done。
// 规划失败 / replan 耗尽时回退 worker 的事件流（等价于 Run 的 ReAct 回退）。
func (p *PlanExecuteAgent) RunEvents(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	if req == nil {
		return nil, nil
	}
	out := make(chan StreamEvent, 16)
	go func() {
		defer close(out)
		send := func(e StreamEvent) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		plan, err := p.generatePlan(ctx, req)
		if err != nil {
			p.forwardWorkerStream(ctx, req, send)
			return
		}
		if req.Metadata == nil {
			req.Metadata = map[string]any{}
		}
		req.Metadata["plan"] = plan
		req.Metadata["plan_mode"] = "plan"

		if !send(StreamEvent{Type: StreamEventPlan, Metadata: map[string]any{"plan": plan, "step_count": len(plan.Steps)}}) {
			return
		}
		p.executeStream(ctx, req, plan, send)
	}()
	return out, nil
}

// executeStream 逐步执行并流式上报 plan_step；replan / 回退语义与 execute 对齐。
func (p *PlanExecuteAgent) executeStream(ctx context.Context, req *Request, plan *Plan, send func(StreamEvent) bool) {
	steps := plan.Steps
	var lastResp *Response
	replans := 0

	for len(steps) > 0 {
		step := steps[0]
		if !send(StreamEvent{Type: StreamEventPlanStep, Metadata: map[string]any{"step": step, "remaining": len(steps)}}) {
			return
		}
		ok, resp, err := p.runStep(ctx, step)
		if err != nil {
			send(StreamEvent{Type: StreamEventError, Error: err.Error()})
			return
		}
		if resp != nil {
			lastResp = resp
		}
		if ok {
			steps = steps[1:]
			continue
		}
		if replans >= p.maxReplans {
			p.forwardWorkerStream(ctx, req, send)
			return
		}
		replans++
		newPlan, perr := p.replan(ctx, req, step, resp)
		if perr != nil {
			p.forwardWorkerStream(ctx, req, send)
			return
		}
		steps = newPlan.Steps
	}

	if lastResp != nil && lastResp.Text != "" {
		send(StreamEvent{Type: StreamEventDelta, Text: lastResp.Text})
	}
	var msgs []model.Message
	if lastResp != nil {
		msgs = lastResp.Messages
	}
	send(StreamEvent{Type: StreamEventDone, Messages: msgs})
}

// forwardWorkerStream 转发 worker 的事件流（回退 ReAct 时用）；worker 非流式则退化为单次 Run。
func (p *PlanExecuteAgent) forwardWorkerStream(ctx context.Context, req *Request, send func(StreamEvent) bool) {
	if es, ok := p.worker.(EventStreamableAgent); ok {
		ch, err := es.RunEvents(ctx, req)
		if err != nil {
			send(StreamEvent{Type: StreamEventError, Error: err.Error()})
			return
		}
		for ev := range ch {
			if !send(ev) {
				return
			}
		}
		return
	}
	resp, err := p.worker.Run(ctx, req)
	if err != nil {
		send(StreamEvent{Type: StreamEventError, Error: err.Error()})
		return
	}
	if resp != nil && resp.Text != "" {
		send(StreamEvent{Type: StreamEventDelta, Text: resp.Text})
	}
	send(StreamEvent{Type: StreamEventDone})
}

// runStep 执行单个步骤：非空输出即成功；配置 verifier 时做模型自评。
func (p *PlanExecuteAgent) runStep(ctx context.Context, step PlanStep) (bool, *Response, error) {
	stepReq := &Request{
		Messages: []model.Message{{Role: "user", Content: step.Goal}},
		Metadata: map[string]any{
			"plan_step":     step,
			"step_readonly": step.Readonly,
		},
	}
	resp, err := p.worker.Run(ctx, stepReq)
	if err != nil {
		return false, resp, err
	}
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		return false, resp, nil
	}
	if p.verifier != nil {
		ok, verr := p.verify(ctx, step, resp.Text)
		if verr != nil {
			return false, resp, nil // 自评失败视为步骤未通过
		}
		return ok, resp, nil
	}
	return true, resp, nil
}

// replan 让 planner 基于失败步骤与已产出重规划剩余工作。
func (p *PlanExecuteAgent) replan(ctx context.Context, req *Request, failed PlanStep, resp *Response) (*Plan, error) {
	gen, err := p.planner.Generate(ctx, buildReplanPrompt(req, failed, resp))
	if err != nil {
		return nil, err
	}
	return ParsePlan(gen.Text)
}

// verify 让 verifier 判断步骤是否满足 SuccessCriteria。
func (p *PlanExecuteAgent) verify(ctx context.Context, step PlanStep, output string) (bool, error) {
	prompt := fmt.Sprintf(
		"Did the step succeed according to its success criteria? Answer exactly YES or NO.\nGoal: %s\nSuccess criteria: %s\nOutput:\n%s",
		step.Goal, step.SuccessCriteria, output,
	)
	gen, err := p.verifier.Generate(ctx, prompt)
	if err != nil {
		return false, err
	}
	upper := strings.ToUpper(strings.TrimSpace(gen.Text))
	return strings.Contains(upper, "YES") && !strings.Contains(upper, "NO"), nil
}

func joinUserMessages(msgs []model.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "user" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(m.Content)
		}
	}
	if b.Len() == 0 && len(msgs) > 0 {
		return msgs[len(msgs)-1].Content
	}
	return b.String()
}

func buildPlanPrompt(req *Request) string {
	return fmt.Sprintf(`You are a planning agent. Break the user's task into an ordered list of steps and output ONLY valid JSON in this exact shape:
{"steps":[{"id":"...","goal":"...","success_criteria":"...","readonly":false,"suggested_tools":["..."]}]}

User task:
%s`, joinUserMessages(req.Messages))
}

func buildRepairPrompt(req *Request, badOutput string, parseErr error) string {
	return fmt.Sprintf(`Your previous output was not valid JSON in the required shape. Fix it and output ONLY valid JSON.
Error: %v
Previous output:
%s

Original task:
%s`, parseErr, badOutput, joinUserMessages(req.Messages))
}

func buildReplanPrompt(req *Request, failed PlanStep, resp *Response) string {
	soFar := ""
	if resp != nil {
		soFar = resp.Text
	}
	return fmt.Sprintf(`A step of the plan failed. Produce a revised plan (ONLY JSON) for the remaining work.
Failed step goal: %s
Failed step criteria: %s
Output so far:
%s

Original task:
%s`, failed.Goal, failed.SuccessCriteria, soFar, joinUserMessages(req.Messages))
}
