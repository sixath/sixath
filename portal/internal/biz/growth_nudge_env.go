package biz

import (
	"os"
	"strconv"
	"strings"

	"github.com/sixath/framework/growth"
)

// nudgeConfigFromEnv builds G1 NudgeConfig from env (no conf.proto fields — Windows
// proto regen avoided). Unset vars keep DefaultNudgeConfig (Enabled=true, interval 0).
//
//	SATH_GROWTH_NUDGE_ENABLED           — "0"/"false"/"no" disables; "1"/"true"/"yes" enables
//	SATH_GROWTH_NUDGE_SKILL_TOOL_INTERVAL — >0 overrides; 0/unset → framework Defaults
//	SATH_GROWTH_NUDGE_MEMORY_TURN_INTERVAL — same for memory turns
func nudgeConfigFromEnv() growth.NudgeConfig {
	n := growth.DefaultNudgeConfig()
	if v := strings.TrimSpace(os.Getenv("SATH_GROWTH_NUDGE_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "0", "false", "no", "off":
			n.Enabled = false
		case "1", "true", "yes", "on":
			n.Enabled = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("SATH_GROWTH_NUDGE_SKILL_TOOL_INTERVAL")); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			n.SkillToolInterval = i
		}
	}
	if v := strings.TrimSpace(os.Getenv("SATH_GROWTH_NUDGE_MEMORY_TURN_INTERVAL")); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			n.MemoryTurnInterval = i
		}
	}
	return n
}

// backgroundReviewEnabledFromEnv reads C3 flag (stand-in for growth.background_review.enabled
// until conf.proto gains a nested message — avoids Windows protobuf regen).
//
//	SATH_BACKGROUND_REVIEW — unset → true; "0"/"false"/"no"/"off" → false; else true
func backgroundReviewEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("SATH_BACKGROUND_REVIEW"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
