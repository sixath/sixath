package biz

import (
	"backend/internal/conf"
)

// ProvideGrowthUsecase wires growth repo with optional phase2 flags from Bootstrap.Growth.
// G1 nudge: code default Enabled=true; override via SATH_GROWTH_NUDGE_* env (no conf.proto
// fields — avoids Windows protobuf regen). YAML Growth section does not yet expose nudge.
//
// C3 background_review.enabled: stand-in env SATH_BACKGROUND_REVIEW (default true when unset).
// When true, chat hooks skip OnToolSuccess/OnAssistantTurn; ChatService.afterTurnBackgroundReview
// calls FinalizeTurnForBackgroundReview + SpawnBackgroundReview after Run/Stream Done.
func ProvideGrowthUsecase(repo GrowthRepo, cfg *conf.Growth) *GrowthUsecase {
	uc := NewGrowthUsecase(repo)
	if cfg != nil {
		uc.SetSessionEndMemoryReviewEnabled(cfg.GetSessionEndMemoryReviewEnabled())
		uc.SetSessionEndSkillReviewEnabled(cfg.GetSessionEndSkillReviewEnabled())
	}
	uc.SetNudgeConfig(nudgeConfigFromEnv())
	uc.SetBackgroundReviewEnabled(backgroundReviewEnabledFromEnv())
	return uc
}
