package events

import (
	"context"
	"time"
)

type Kind string

const (
	AgentInit      Kind = "agent.init"
	RunStarted     Kind = "agent.run.started"
	ModelInvoked   Kind = "agent.model.invoked"
	ModelResponded Kind = "agent.model.responded"

	ToolInvoked   Kind = "agent.tool.invoked"
	ToolExecuted  Kind = "agent.tool.executed"
	ToolStarted   Kind = "agent.tool.started"
	ToolCompleted Kind = "agent.tool.completed"
	ToolFailed    Kind = "agent.tool.failed"

	// ToolGuardrailWarn 工具护栏命中（R1/R2 等），默认 warnings_only 仅告警（设计 §6、产品 §6.3）。
	ToolGuardrailWarn Kind = "agent.tool_guardrail.warn"

	PermissionDenied Kind = "agent.permission.denied"
	HookBlocked      Kind = "agent.hook.blocked"
	// EvidenceIncomplete final-answer lacked required RCA evidence (EvidenceGate Soft/Hard).
	EvidenceIncomplete Kind = "agent.evidence.incomplete"
	// CodeClaimMismatch final-answer quoted/claimed code that does not match rca_read.
	CodeClaimMismatch Kind = "agent.code_claim.mismatch"
	RunCompleted      Kind = "agent.run.completed"
	RunError          Kind = "agent.run.error"

	// MemoryPrefetchSkipped 记忆预取未注入（fail-open 跳过或空结果），载荷含 reason（设计 §4.6）。
	MemoryPrefetchSkipped Kind = "agent.memory.prefetch_skipped"

	HttpRequestInvoked   Kind = "http.request.invoked"
	HttpRequestCompleted Kind = "http.request.completed"

	DataQueryWriteProposed Kind = "dataquery.write.proposed"
	DataQueryWriteExecuted Kind = "dataquery.write.executed"

	// Growth review lifecycle（成长系统 spec §7；portal/framework 协作发布）。
	GrowthReviewScheduled Kind = "growth.review.scheduled"
	GrowthReviewCompleted Kind = "growth.review.completed"
	GrowthReviewFailed    Kind = "growth.review.failed"

	// ProcessNotify background terminal finished with notify_on_complete.
	ProcessNotify Kind = "agent.process.notify"
)

type Event struct {
	Kind      Kind
	Payload   map[string]any
	RequestID string
	At        time.Time
}

type Listener func(ctx context.Context, e Event)
