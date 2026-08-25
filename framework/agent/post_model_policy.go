package agent

import (
	"context"
	"strings"

	"github.com/sixath/framework/model"
)

// PostModelDecision is the action taken after a tool-calling model step.
type PostModelDecision int

const (
	// PostModelContinue keeps tool_calls as returned by the model.
	PostModelContinue PostModelDecision = iota
	// PostModelFinish discards tool_calls and treats the assistant text as the final answer.
	PostModelFinish
	// PostModelFilter executes only the returned ToolCalls subset (may be empty → finish).
	PostModelFilter
	// PostModelRetry discards tool_calls and injects Prompt for another model round.
	PostModelRetry
)

const defaultPostModelRetryPrompt = "Dropped tool calls that are outside the active tool families for this turn. Continue using only in-family tools, or answer directly."

// PostModelPolicyInput is the context for PostModelPolicy.Evaluate.
type PostModelPolicyInput struct {
	Req           *Request
	Step          int
	AssistantText string
	ToolStep      model.ToolStep
	Trace         *RunTrace
}

// PostModelPolicyResult is the decision from PostModelPolicy.
type PostModelPolicyResult struct {
	Decision  PostModelDecision
	ToolCalls []model.ToolCall // used when Decision is PostModelFilter
	Reason    string
	Prompt    string // used when Decision is PostModelRetry
}

// PostModelPolicy runs after the model returns and before tools execute.
// Nil policy is a no-op (always continue).
type PostModelPolicy interface {
	Evaluate(ctx context.Context, in PostModelPolicyInput) PostModelPolicyResult
}

// IdlePostModelPolicy optionally inspects steps with no tool_calls (Used=false).
// Policies that do not implement it keep the existing idle finish behavior.
type IdlePostModelPolicy interface {
	EvaluateIdle(ctx context.Context, in PostModelPolicyInput) PostModelPolicyResult
}

// applyPostModelPolicy may clear or filter tool_calls on stepInfo.
// retryPrompt non-empty means the caller must inject it and continue the ReAct loop
// instead of treating the cleared step as a final answer.
func (a *ReActAgent) applyPostModelPolicy(
	ctx context.Context,
	req *Request,
	step int,
	assistantText string,
	stepInfo model.ToolStep,
	trace *RunTrace,
) (model.ToolStep, string) {
	if a == nil || a.config.PostModelPolicy == nil {
		return stepInfo, ""
	}
	if !stepInfo.Used {
		if idle, ok := a.config.PostModelPolicy.(IdlePostModelPolicy); ok {
			res := idle.EvaluateIdle(ctx, PostModelPolicyInput{
				Req:           req,
				Step:          step,
				AssistantText: assistantText,
				ToolStep:      stepInfo,
				Trace:         trace,
			})
			if res.Decision == PostModelRetry {
				reason := res.Reason
				if reason == "" {
					reason = "retry"
				}
				if trace != nil {
					trace.Errors = append(trace.Errors, "post_model_policy:retry:"+reason)
				}
				prompt := strings.TrimSpace(res.Prompt)
				if prompt == "" {
					prompt = defaultPostModelRetryPrompt
				}
				return clearToolStep(stepInfo), prompt
			}
		}
		return stepInfo, ""
	}
	calls := toolCallsFromStep(stepInfo)
	if len(calls) == 0 {
		return stepInfo, ""
	}
	res := a.config.PostModelPolicy.Evaluate(ctx, PostModelPolicyInput{
		Req:           req,
		Step:          step,
		AssistantText: assistantText,
		ToolStep:      stepInfo,
		Trace:         trace,
	})
	switch res.Decision {
	case PostModelFinish:
		if trace != nil && res.Reason != "" {
			trace.Errors = append(trace.Errors, "post_model_policy:finish:"+res.Reason)
		}
		return clearToolStep(stepInfo), ""
	case PostModelRetry:
		reason := res.Reason
		if reason == "" {
			reason = "retry"
		}
		if trace != nil {
			trace.Errors = append(trace.Errors, "post_model_policy:retry:"+reason)
		}
		prompt := strings.TrimSpace(res.Prompt)
		if prompt == "" {
			prompt = defaultPostModelRetryPrompt
		}
		return clearToolStep(stepInfo), prompt
	case PostModelFilter:
		if len(res.ToolCalls) == 0 {
			if trace != nil {
				reason := res.Reason
				if reason == "" {
					reason = "filter_empty"
				}
				trace.Errors = append(trace.Errors, "post_model_policy:finish:"+reason)
			}
			return clearToolStep(stepInfo), ""
		}
		if len(res.ToolCalls) == len(calls) {
			return stepInfo, ""
		}
		if trace != nil && res.Reason != "" {
			trace.Errors = append(trace.Errors, "post_model_policy:filter:"+res.Reason)
		}
		return replaceToolCalls(stepInfo, res.ToolCalls), ""
	default:
		return stepInfo, ""
	}
}

func clearToolStep(step model.ToolStep) model.ToolStep {
	step.Used = false
	step.ToolCalls = nil
	step.ToolCallID = ""
	step.ToolName = ""
	step.Arguments = nil
	step.Observation = nil
	step.Error = ""
	return step
}

func replaceToolCalls(step model.ToolStep, calls []model.ToolCall) model.ToolStep {
	step.Used = true
	step.ToolCalls = append([]model.ToolCall(nil), calls...)
	if len(calls) > 0 {
		step.ToolCallID = calls[0].ID
		step.ToolName = calls[0].Name
		step.Arguments = calls[0].Arguments
	}
	return step
}
