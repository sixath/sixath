package agent

import (
	"context"
	"errors"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

var (
	ErrToolPermissionDenied = errors.New("tool permission denied")
	ErrToolNotFound         = errors.New("tool not found")
	// ErrToolGuardrailHalt 护栏硬停（非 warnings_only 且达到 halt 阈值）。
	ErrToolGuardrailHalt = errors.New("tool guardrail halt")
)

// ContextOpsInvocation 单次进入底层 model.Chat / ChatWithTools 等调用的上下文变换摘要（设计 §8.1、产品 §6.5 O1）。
type ContextOpsInvocation struct {
	Index           int    `json:"index"`
	Mode            string `json:"mode,omitempty"`
	L2Used          bool   `json:"l2_used,omitempty"`
	L2SummaryHash   string `json:"l2_summary_hash,omitempty"`
	SanitizeApplied bool   `json:"sanitize_applied,omitempty"`
	// 下列字段为该次 invocation 内 PrepareChatContext 路径上的增量（与 ContextOpsTrace 聚合字段同源）。
	L0DroppedMessages          int `json:"l0_dropped,omitempty"`
	StripOrphanTools           int `json:"strip_orphan_tools,omitempty"`
	L2ToolPrePruneRunesRemoved int `json:"l2_pre_prune_runes_removed,omitempty"`
	SnipCompactRemoved         int `json:"snip_compact_removed,omitempty"`
}

// ContextOpsTrace 可观测：压缩、strip、后续 L2 等（设计 §8、产品 §6.5 O1）。
type ContextOpsTrace struct {
	L0DroppedMessages      int                    `json:"l0_dropped,omitempty"`
	L2Used                 bool                   `json:"l2_used,omitempty"`
	L2SummaryHash          string                 `json:"l2_summary_hash,omitempty"`
	L2SummaryHashes        []string               `json:"l2_summary_hashes,omitempty"`
	L2InvocationCount      int                    `json:"l2_invocation_count,omitempty"`
	L2PrePruneRunesRemoved int                    `json:"l2_pre_prune_runes_removed,omitempty"`
	L2CooldownActive       bool                   `json:"l2_cooldown_active,omitempty"`
	SanitizeApplied        bool                   `json:"sanitize_applied,omitempty"`
	StripOrphanTools       int                    `json:"strip_orphan_tools,omitempty"`
	SnipCompactRemoved     int                    `json:"snip_compact_removed,omitempty"`
	Invocations            []ContextOpsInvocation `json:"invocations,omitempty"`
}

type RunTrace struct {
	RequestID string
	ToolCalls []ToolCallRecord
	Errors    []string

	// GuardrailHalt 为 true 表示因护栏硬停结束本次 Run（设计 §6.2、§3.1）。
	GuardrailHalt bool `json:"guardrail_halt,omitempty"`
	// GuardrailHaltMessage 硬停时注入的 system 消息（含 sixath.origin=guardrail_halt），供审计与回放。
	GuardrailHaltMessage *model.Message `json:"guardrail_halt_message,omitempty"`

	ContextOps *ContextOpsTrace `json:"context_ops,omitempty"`

	// LastL2Summary 最近一次 L2 摘要正文（不含 handoff 前缀）；供 Portal 持久化 compact boundary。
	LastL2Summary string `json:"last_l2_summary,omitempty"`
	// LastL2MiddleRemoved 最近一次 L2 替换的中段消息条数。
	LastL2MiddleRemoved int `json:"last_l2_middle_removed,omitempty"`

	// PrefetchSkipped 为 true 表示本 turn 尝试 memory prefetch 但未注入围栏消息（设计 §4.6）。
	PrefetchSkipped bool `json:"prefetch_skipped,omitempty"`
	// PrefetchSkipReason 取值见 memory.PrefetchSkipReason 常量字符串（如 backend_error、timeout）。
	PrefetchSkipReason string `json:"prefetch_skip_reason,omitempty"`

	// EvidenceNudges Soft EvidenceGate 已注入回压的次数（最多 1，见 Soft 策略）。
	EvidenceNudges int `json:"evidence_nudges,omitempty"`
	// CodeClaimNudges Soft Code Claim Auditor 已注入回压的次数（最多 1）。
	CodeClaimNudges int `json:"code_claim_nudges,omitempty"`
	// EmptyHitNudges Soft empty-hit speak gate 已注入回压的次数（最多 1）。
	EmptyHitNudges int `json:"empty_hit_nudges,omitempty"`
	// EmptyIdleNudges 工具已跑但终答空白时的回压次数（最多 1）。
	EmptyIdleNudges int `json:"empty_idle_nudges,omitempty"`
	// GoalDriftNudges idle 改题回压次数（最多 1）。
	GoalDriftNudges int `json:"goal_drift_nudges,omitempty"`
	// DroppedProposals 本轮被 family/drift/intake 丢掉的工具提议。
	DroppedProposals []DroppedProposal `json:"dropped_proposals,omitempty"`

	// ParallelTools 为 true 表示本 Run 中至少有一轮 tool_calls 走了并行执行（D2）。
	ParallelTools bool `json:"parallel_tools,omitempty"`

	// invocationSeq 为单次 Run 内 model 调用序号，不序列化。
	invocationSeq int `json:"-"`
}

// DroppedProposal records a tool call discarded by post-model policy (family/drift/intake).
type DroppedProposal struct {
	ToolName string `json:"tool_name,omitempty"`
	ArgName  string `json:"arg_name,omitempty"`
}

type RunError struct {
	Err   error
	Trace *RunTrace
}

func (e *RunError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *RunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func runError(err error, trace *RunTrace) error {
	if err == nil {
		return nil
	}
	var existing *RunError
	if errors.As(err, &existing) {
		if existing.Trace == nil {
			existing.Trace = trace
		}
		return existing
	}
	return &RunError{Err: err, Trace: trace}
}

type ToolCallRecord struct {
	Step       int
	ToolCallID string
	ToolName   string
	Arguments  map[string]any
	Result     any
	Error      string
	Allowed    bool
	Blocked    bool `json:"blocked,omitempty"`
	Decision   string
	DurationMS int64
}

type StreamEventType string

const (
	StreamEventDelta            StreamEventType = "delta"
	StreamEventToolStarted      StreamEventType = "tool_started"
	StreamEventToolCompleted    StreamEventType = "tool_completed"
	StreamEventToolFailed       StreamEventType = "tool_failed"
	StreamEventPermissionDenied StreamEventType = "permission_denied"
	StreamEventHookBlocked      StreamEventType = "hook_blocked"
	StreamEventError            StreamEventType = "error"
	StreamEventDone             StreamEventType = "done"
)

type StreamEvent struct {
	Type StreamEventType
	// Text is a delta chunk on StreamEventDelta, and the full final assistant
	// text on StreamEventDone (idle answers are not always appended to Messages).
	Text     string
	ToolCall *ToolCallRecord
	Error    string
	Trace    *RunTrace
	Metadata map[string]any
	// Messages is set on StreamEventDone: in-memory conversation snapshot (may include tool roles).
	Messages []model.Message `json:"-"`
}

type PermissionDecision struct {
	Allowed bool
	Reason  string
}

type PermissionPolicy interface {
	AllowTool(ctx context.Context, t tool.Tool, args map[string]any) PermissionDecision
}

type allowAllPolicy struct{}

func (allowAllPolicy) AllowTool(context.Context, tool.Tool, map[string]any) PermissionDecision {
	return PermissionDecision{Allowed: true, Reason: "allowed"}
}

func AllowAllTools() PermissionPolicy {
	return allowAllPolicy{}
}

func DefaultPermissionPolicy() PermissionPolicy {
	return DenyTools("execute_skill_script")
}

type denyToolsPolicy struct {
	names map[string]struct{}
}

func DenyTools(names ...string) PermissionPolicy {
	m := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name != "" {
			m[name] = struct{}{}
		}
	}
	return denyToolsPolicy{names: m}
}

func (p denyToolsPolicy) AllowTool(_ context.Context, t tool.Tool, _ map[string]any) PermissionDecision {
	if _, denied := p.names[t.Name]; denied {
		return PermissionDecision{Allowed: false, Reason: "tool denied by policy"}
	}
	return PermissionDecision{Allowed: true, Reason: "allowed"}
}
