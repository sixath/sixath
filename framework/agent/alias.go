// Package agent is a one-season alias of github.com/sixath/framework/harness.
// New code should import harness directly.
package agent

import "github.com/sixath/framework/harness"

type (
	Agent                    = harness.Agent
	AgentContext             = harness.AgentContext
	ChatAgent                = harness.ChatAgent
	ChatConfig               = harness.ChatConfig
	ChatSessionHook          = harness.ChatSessionHook
	ChatSessionHookFunc      = harness.ChatSessionHookFunc
	ChatSessionHookRegistry  = harness.ChatSessionHookRegistry
	ContextCompressionConfig = harness.ContextCompressionConfig
	ContextOpsInvocation     = harness.ContextOpsInvocation
	ContextOpsTrace          = harness.ContextOpsTrace
	DroppedProposal          = harness.DroppedProposal
	EventStreamableAgent     = harness.EventStreamableAgent
	FailureCaptureConfig     = harness.FailureCaptureConfig
	FailureCaptureHook       = harness.FailureCaptureHook
	GuardrailDecision        = harness.GuardrailDecision
	GuardrailEvaluator       = harness.GuardrailEvaluator
	HarnessHookMatch         = harness.HarnessHookMatch
	HarnessHookRule          = harness.HarnessHookRule
	HarnessHooksFile         = harness.HarnessHooksFile
	Option                   = harness.Option
	PermissionDecision       = harness.PermissionDecision
	PermissionPolicy         = harness.PermissionPolicy
	ReActAgent               = harness.ReActAgent
	ReActConfig              = harness.ReActConfig
	ReActOption              = harness.ReActOption
	Request                  = harness.Request
	Response                 = harness.Response
	ResponseChunk            = harness.ResponseChunk
	RunError                 = harness.RunError
	RunTrace                 = harness.RunTrace
	StreamEvent              = harness.StreamEvent
	StreamEventType          = harness.StreamEventType
	StreamableAgent          = harness.StreamableAgent
	ToolCallRecord           = harness.ToolCallRecord
	ToolCallingModel         = harness.ToolCallingModel
	ToolGuardrailsConfig     = harness.ToolGuardrailsConfig
	ToolHook                 = harness.ToolHook
	TurnToolCall             = harness.TurnToolCall
	TurnTrace                = harness.TurnTrace
	TurnTraceMeta            = harness.TurnTraceMeta
	Usage                    = harness.Usage
)

const (
	ForcedFinalSummaryPrompt = harness.ForcedFinalSummaryPrompt
	HarnessHooksFileRel      = harness.HarnessHooksFileRel

	MetaAgentName    = harness.MetaAgentName
	MetaUserID       = harness.MetaUserID
	MetaModelName    = harness.MetaModelName
	MetaSystem       = harness.MetaSystem
	MetaTemperature  = harness.MetaTemperature
	MetaTokenInput   = harness.MetaTokenInput
	MetaTokenOutput  = harness.MetaTokenOutput
	MetaSessionID    = harness.MetaSessionID
	MetaDatasourceID = harness.MetaDatasourceID

	StreamEventDelta            = harness.StreamEventDelta
	StreamEventToolStarted      = harness.StreamEventToolStarted
	StreamEventToolCompleted    = harness.StreamEventToolCompleted
	StreamEventToolFailed       = harness.StreamEventToolFailed
	StreamEventPermissionDenied = harness.StreamEventPermissionDenied
	StreamEventHookBlocked      = harness.StreamEventHookBlocked
	StreamEventError            = harness.StreamEventError
	StreamEventDone             = harness.StreamEventDone
)

var (
	ErrToolPermissionDenied = harness.ErrToolPermissionDenied
	ErrToolNotFound         = harness.ErrToolNotFound
	ErrToolGuardrailHalt    = harness.ErrToolGuardrailHalt
	ErrToolHookBlocked      = harness.ErrToolHookBlocked

	AllowAllTools                = harness.AllowAllTools
	AnswerOriginalQuestionPrompt = harness.AnswerOriginalQuestionPrompt
	BuildTurnTrace               = harness.BuildTurnTrace
	CanonicalJSON                = harness.CanonicalJSON
	ContextFrom                  = harness.ContextFrom
	DefaultPermissionPolicy      = harness.DefaultPermissionPolicy
	DenyTools                    = harness.DenyTools
	EnsureContext                = harness.EnsureContext
	GuardrailHaltSystemMessage   = harness.GuardrailHaltSystemMessage
	HasSuccessfulBoundEvidence   = harness.HasSuccessfulBoundEvidence
	IsBoundEvidenceTool          = harness.IsBoundEvidenceTool
	IsSkillsFamilyToolName       = harness.IsSkillsFamilyToolName
	LoadWorkspaceHarnessHooks    = harness.LoadWorkspaceHarnessHooks
	NewChatAgent                 = harness.NewChatAgent
	NewChatSessionHookRegistry   = harness.NewChatSessionHookRegistry
	NewFailureCaptureHook        = harness.NewFailureCaptureHook
	NewGuardrailEvaluator        = harness.NewGuardrailEvaluator
	NewReActAgent                = harness.NewReActAgent
	ParseHarnessHooksYAML        = harness.ParseHarnessHooksYAML
	RequestMetadataFromContext   = harness.RequestMetadataFromContext
	RequestSource                = harness.RequestSource
	StableArgsKey                = harness.StableArgsKey
	ToolGuardrailsFromConfig     = harness.ToolGuardrailsFromConfig
	WithAgentContext             = harness.WithAgentContext
	WithEventBus                 = harness.WithEventBus
	WithMaxHistory               = harness.WithMaxHistory
	WithReActContextCompression  = harness.WithReActContextCompression
	WithReActEventBus            = harness.WithReActEventBus
	WithReActGuardrailEvaluator  = harness.WithReActGuardrailEvaluator
	WithReActMaxContextRunes     = harness.WithReActMaxContextRunes
	WithReActMaxHistory          = harness.WithReActMaxHistory
	WithReActMaxOutputTokens     = harness.WithReActMaxOutputTokens
	WithReActMaxParallel         = harness.WithReActMaxParallel
	WithReActMaxSteps            = harness.WithReActMaxSteps
	WithReActMemoryOrchestrator  = harness.WithReActMemoryOrchestrator
	WithReActParallelTools       = harness.WithReActParallelTools
	WithReActPermissionPolicy    = harness.WithReActPermissionPolicy
	WithReActSkillsDirs          = harness.WithReActSkillsDirs
	WithReActSnipCompactEnabled  = harness.WithReActSnipCompactEnabled
	WithReActSystemPrompt        = harness.WithReActSystemPrompt
	WithReActToolGuardrails      = harness.WithReActToolGuardrails
	WithReActToolHooks           = harness.WithReActToolHooks
	WithReActToolSuccessHook     = harness.WithReActToolSuccessHook
	WithReActWorkspace           = harness.WithReActWorkspace
	WithRequestMetadata          = harness.WithRequestMetadata
)
