package growth

import "time"

// Defaults holds growth review trigger thresholds and workspace lease duration.
// Values align with docs/superpowers/specs/2026-05-10-growth-system-design.md §4
// (counter-based triggers) and §6 (lease TTL > review P99).
type Defaults struct {
	// SkillToolInterval is the number of successful tool executions after which
	// the portal layer should set pending_skill_review (overridable via NudgeConfig / env).
	SkillToolInterval int
	// MemoryTurnInterval is the number of completed assistant turns after which
	// pending_memory_review should be set (overridable via NudgeConfig / env).
	MemoryTurnInterval int
	// IdleCheckInterval 是空闲会话长时间无 pending 复盘的最小间隔；超时后触发轻量 memory-only 复盘。
	IdleCheckInterval time.Duration
	// LeaseTTL is how long a workspace lease row remains valid while a review runs;
	// must exceed typical review duration to avoid losing the lease mid-flight.
	LeaseTTL time.Duration
}

// NewDefaults returns documented built-in defaults (portal may override via YAML/DB later).
func NewDefaults() Defaults {
	return Defaults{
		SkillToolInterval:  10,
		MemoryTurnInterval: 3,
		IdleCheckInterval:  10 * time.Minute,
		LeaseTTL:           15 * time.Minute,
	}
}

// NudgeConfig controls threshold-based pending_skill / pending_memory triggers
// (OnToolSuccess / OnAssistantTurn). Interval 0 means use NewDefaults() values —
// never treat 0 as "fire every time".
type NudgeConfig struct {
	// Enabled when false: counters still advance (capped at interval) but never
	// set pending_* or Wake. Default true via DefaultNudgeConfig (code default,
	// not proto3 bool zero-value).
	Enabled bool
	// SkillToolInterval overrides Defaults.SkillToolInterval when > 0.
	SkillToolInterval int
	// MemoryTurnInterval overrides Defaults.MemoryTurnInterval when > 0.
	MemoryTurnInterval int
}

// DefaultNudgeConfig returns Enabled=true and intervals 0 (framework Defaults).
func DefaultNudgeConfig() NudgeConfig {
	return NudgeConfig{Enabled: true}
}

// EffectiveSkillToolInterval returns SkillToolInterval, or NewDefaults().SkillToolInterval when <= 0.
func (c NudgeConfig) EffectiveSkillToolInterval() int {
	if c.SkillToolInterval <= 0 {
		return NewDefaults().SkillToolInterval
	}
	return c.SkillToolInterval
}

// EffectiveMemoryTurnInterval returns MemoryTurnInterval, or NewDefaults().MemoryTurnInterval when <= 0.
func (c NudgeConfig) EffectiveMemoryTurnInterval() int {
	if c.MemoryTurnInterval <= 0 {
		return NewDefaults().MemoryTurnInterval
	}
	return c.MemoryTurnInterval
}
