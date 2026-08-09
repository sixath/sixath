package growth

import (
	"testing"
	"time"
)

func TestNewDefaults_positive(t *testing.T) {
	d := NewDefaults()
	if d.SkillToolInterval <= 0 {
		t.Fatalf("SkillToolInterval=%d", d.SkillToolInterval)
	}
	if d.MemoryTurnInterval <= 0 {
		t.Fatalf("MemoryTurnInterval=%d", d.MemoryTurnInterval)
	}
	if d.LeaseTTL <= 0 {
		t.Fatalf("LeaseTTL=%v", d.LeaseTTL)
	}
	if d.IdleCheckInterval != 10*time.Minute {
		t.Fatalf("IdleCheckInterval=%v want 10m", d.IdleCheckInterval)
	}
}

func TestDefaultNudgeConfig_enabledTrue(t *testing.T) {
	n := DefaultNudgeConfig()
	if !n.Enabled {
		t.Fatal("DefaultNudgeConfig.Enabled must be true (code default, not proto zero)")
	}
	if n.SkillToolInterval != 0 || n.MemoryTurnInterval != 0 {
		t.Fatalf("intervals should be 0 meaning use NewDefaults(), got skill=%d memory=%d",
			n.SkillToolInterval, n.MemoryTurnInterval)
	}
}

func TestNudgeConfig_EffectiveIntervals_zeroUsesDefaults(t *testing.T) {
	def := NewDefaults()
	n := NudgeConfig{Enabled: true, SkillToolInterval: 0, MemoryTurnInterval: 0}
	if got := n.EffectiveSkillToolInterval(); got != def.SkillToolInterval {
		t.Fatalf("skill: want %d got %d", def.SkillToolInterval, got)
	}
	if got := n.EffectiveMemoryTurnInterval(); got != def.MemoryTurnInterval {
		t.Fatalf("memory: want %d got %d", def.MemoryTurnInterval, got)
	}
}

func TestNudgeConfig_EffectiveIntervals_custom(t *testing.T) {
	n := NudgeConfig{SkillToolInterval: 2, MemoryTurnInterval: 7}
	if got := n.EffectiveSkillToolInterval(); got != 2 {
		t.Fatalf("skill want 2 got %d", got)
	}
	if got := n.EffectiveMemoryTurnInterval(); got != 7 {
		t.Fatalf("memory want 7 got %d", got)
	}
}
