package model

// 与设计稿 §3.1 对齐：注入类消息的 Metadata 键与取值。
const (
	MetadataKeySixathOrigin     = "sixath.origin"
	MetadataKeyReasoningContent = "reasoning_content" // thinking 模式 assistant 消息需原样回传

	OriginCompressionNotice = "compression_notice"
	OriginMemoryFence       = "memory_fence"
	OriginGuardrailHalt     = "guardrail_halt"
	OriginL2Handoff         = "l2_handoff"
	OriginCompactBoundary   = "compact_boundary"
	OriginCodeWorkset       = "code_workset"
	OriginCodePin           = "code_pin"
)
