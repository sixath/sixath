package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool"
)

const (
	FailureCodeToolFailed      = "tool_failed"
	FailureCodeToolRepeatFail  = "tool_repeat_fail"
	FailureCodePolicyViolation = "policy_violation"
	// FailureCodeUserReject reserved; not mapped in P3-A unless a clear Bus event exists.
	FailureCodeUserReject = "user_reject"
	// FailureCodeTaskFailed reserved; no emitter in P3-A.
	FailureCodeTaskFailed = "task_failed"
)

// FailureSignal is evaluator-grounded evidence for later procedural repair (P3-A).
type FailureSignal struct {
	Code       string
	AgentID    string
	AgentName  string // optional display/route name for pilot match (P3-E)
	SessionID  string
	TaskFamily string
	ToolName   string
	SkillID    string
	Message    string
	Evidence   map[string]string
	At         time.Time
}

// FailureSignalFromEvent maps Bus events to FailureSignal.
// Returns ok=false for unrelated kinds or empty/unusable payloads.
func FailureSignalFromEvent(ctx context.Context, e events.Event) (FailureSignal, bool) {
	switch e.Kind {
	case events.ToolFailed:
		return mapToolFailed(ctx, e)
	case events.ToolGuardrailWarn:
		return mapGuardrailWarn(ctx, e)
	case events.PermissionDenied:
		return mapPermissionDenied(ctx, e)
	default:
		return FailureSignal{}, false
	}
}

func identityFromCtx(ctx context.Context) (agentID, agentName, sessionID, family string) {
	if ctx != nil {
		agentID, _ = ctx.Value(tool.ContextKeyAgentID).(string)
		agentName, _ = ctx.Value(tool.ContextKeyAgentName).(string)
		sessionID, _ = ctx.Value(tool.ContextKeySessionID).(string)
	}
	agentID = strings.TrimSpace(agentID)
	agentName = strings.TrimSpace(agentName)
	sessionID = strings.TrimSpace(sessionID)
	// TaskFamily: prefer agent name/label when present, else agent_id (§6.5).
	family = ResolveTaskFamily(agentID, agentName)
	return agentID, agentName, sessionID, family
}

func payloadString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	v, ok := p[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return fmt.Sprint(t)
	}
}

func mapToolFailed(ctx context.Context, e events.Event) (FailureSignal, bool) {
	toolName := payloadString(e.Payload, "tool")
	errMsg := payloadString(e.Payload, "error")
	if toolName == "" && errMsg == "" {
		return FailureSignal{}, false
	}
	agentID, agentName, sessionID, family := identityFromCtx(ctx)
	ev := map[string]string{}
	if errMsg != "" {
		ev["error"] = errMsg
	}
	if s := payloadString(e.Payload, "step"); s != "" {
		ev["step"] = s
	}
	if s := payloadString(e.Payload, "tool_call_id"); s != "" {
		ev["tool_call_id"] = s
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return FailureSignal{
		Code:       FailureCodeToolFailed,
		AgentID:    agentID,
		AgentName:  agentName,
		SessionID:  sessionID,
		TaskFamily: family,
		ToolName:   toolName,
		Message:    errMsg,
		Evidence:   ev,
		At:         at,
	}, true
}

func mapGuardrailWarn(ctx context.Context, e events.Event) (FailureSignal, bool) {
	rule := payloadString(e.Payload, "rule")
	toolName := payloadString(e.Payload, "tool")
	if rule == "" {
		return FailureSignal{}, false
	}
	agentID, agentName, sessionID, family := identityFromCtx(ctx)
	ev := map[string]string{"rule": rule}
	for _, k := range []string{"streak", "threshold_warn", "threshold_halt", "stable_args_key"} {
		if s := payloadString(e.Payload, k); s != "" {
			ev[k] = s
		}
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	msg := "tool_guardrail:" + rule
	return FailureSignal{
		Code:       FailureCodeToolRepeatFail,
		AgentID:    agentID,
		AgentName:  agentName,
		SessionID:  sessionID,
		TaskFamily: family,
		ToolName:   toolName,
		Message:    msg,
		Evidence:   ev,
		At:         at,
	}, true
}

func mapPermissionDenied(ctx context.Context, e events.Event) (FailureSignal, bool) {
	toolName := payloadString(e.Payload, "tool")
	reason := payloadString(e.Payload, "reason")
	if toolName == "" && reason == "" {
		return FailureSignal{}, false
	}
	agentID, agentName, sessionID, family := identityFromCtx(ctx)
	ev := map[string]string{}
	if reason != "" {
		ev["reason"] = reason
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return FailureSignal{
		Code:       FailureCodePolicyViolation,
		AgentID:    agentID,
		AgentName:  agentName,
		SessionID:  sessionID,
		TaskFamily: family,
		ToolName:   toolName,
		Message:    reason,
		Evidence:   ev,
		At:         at,
	}, true
}
