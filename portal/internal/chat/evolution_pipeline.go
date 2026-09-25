package chat

import (
	"context"

	"backend/internal/biz"
)

// globalTrialBuffer is the process-wide trial-and-error buffer.
var globalTrialBuffer = NewTrialBuffer(256)

// GetTrialBuffer returns the global trial buffer.
func GetTrialBuffer() *TrialBuffer {
	return globalTrialBuffer
}

// RunEvolutionPipeline executes the full detection-to-proposal pipeline for a turn.
// This is called asynchronously after each turn completes. Fail-open.
func RunEvolutionPipeline(
	ctx context.Context,
	agentID string,
	sessionID string,
	turnIndex int,
	messages []*biz.ChatMessage,
	loadedSkillNames []string,
	proposalRepo biz.EvolutionProposalRepo,
) {
	cfg := EvolutionConfig()
	if cfg == nil {
		return
	}

	result := DetectEvolutionSignals(ctx, messages, loadedSkillNames, nil, globalTrialBuffer)
	if result == nil || !result.IsSignal {
		return
	}

	content := generateProposalContent(ctx, result, messages)
	if content == "" {
		return
	}

	existingSkills := loadSkillSummaries(loadedSkillNames)
	arbResult := Arbitrate(ctx, result.Summary, content, existingSkills)
	if arbResult.Duplicate {
		return
	}

	targetPath, targetAction := resolveTarget(result.SignalType)
	proposal := &biz.EvolutionProposal{
		AgentID:             agentID,
		SessionID:           sessionID,
		TurnIndex:           turnIndex,
		SignalType:          result.SignalType,
		Confidence:          result.Confidence,
		ProblemSummary:      result.Summary,
		ProposedContent:     content,
		TargetPath:          targetPath,
		TargetAction:        targetAction,
		Conflict:            arbResult.Conflict,
		ConflictCheckFailed: arbResult.ConflictCheckFailed,
		DedupSkipped:        arbResult.DedupSkipped,
	}
	if arbResult.ConflictDetail != "" {
		s := arbResult.ConflictDetail
		proposal.ConflictDetail = &s
	}

	_ = proposalRepo.Create(ctx, proposal)
}

func generateProposalContent(ctx context.Context, result *DetectResult, messages []*biz.ChatMessage) string {
	// Placeholder: returns empty (needs LLM proposal generation prompt template).
	_ = ctx
	_ = result
	_ = messages
	return result.Summary
}

func loadSkillSummaries(skillNames []string) []SkillSummary {
	result := make([]SkillSummary, len(skillNames))
	for i, name := range skillNames {
		result[i] = SkillSummary{Name: name, Description: name}
	}
	return result
}

func resolveTarget(signalType string) (path, action string) {
	switch signalType {
	case "style_correction":
		return "USER.md", "patch"
	case "workflow_correction", "debugging_trick", "trial_error":
		return "skills/auto-generated.md", "create"
	case "stale_skill":
		return "skills/auto-generated.md", "deprecate"
	default:
		return "skills/auto-generated.md", "create"
	}
}