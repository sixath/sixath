package agent

import (
	"context"

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
)

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
}

// PostModelPolicy runs after the model returns and before tools execute.
// Nil policy is a no-op (always continue).
type PostModelPolicy interface {
	Evaluate(ctx context.Context, in PostModelPolicyInput) PostModelPolicyResult
}

// applyPostModelPolicy may clear or filter tool_calls on stepInfo.
// When the result is a finish (no remaining tools), Used is set false so callers
// share the existing !Used completion path.
func (a *ReActAgent) applyPostModelPolicy(
	ctx context.Context,
	req *Request,
	step int,
	assistantText string,
	stepInfo model.ToolStep,
	trace *RunTrace,
) model.ToolStep {
	if a == nil || a.config.PostModelPolicy == nil || !stepInfo.Used {
		return stepInfo
	}
	calls := toolCallsFromStep(stepInfo)
	if len(calls) == 0 {
		return stepInfo
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
		return clearToolStep(stepInfo)
	case PostModelFilter:
		if len(res.ToolCalls) == 0 {
			if trace != nil {
				reason := res.Reason
				if reason == "" {
					reason = "filter_empty"
				}
				trace.Errors = append(trace.Errors, "post_model_policy:finish:"+reason)
			}
			return clearToolStep(stepInfo)
		}
		if len(res.ToolCalls) == len(calls) {
			return stepInfo
		}
		if trace != nil && res.Reason != "" {
			trace.Errors = append(trace.Errors, "post_model_policy:filter:"+res.Reason)
		}
		return replaceToolCalls(stepInfo, res.ToolCalls)
	default:
		return stepInfo
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
