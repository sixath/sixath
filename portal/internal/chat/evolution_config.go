package chat

import (
	"os"
	"strings"

	"github.com/sixath/framework/config"
)

var storedEvolutionCfg *config.EvolutionConfig

// SetEvolutionConfig stores agent_extra evolution settings.
func SetEvolutionConfig(cfg *config.EvolutionConfig) {
	if cfg == nil {
		storedEvolutionCfg = nil
		return
	}
	cp := *cfg
	storedEvolutionCfg = &cp
}

// EvolutionEnabled reports whether the evolution pipeline is enabled.
func EvolutionEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("SATH_EVOLUTION_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return storedEvolutionCfg != nil && storedEvolutionCfg.Enabled
}

// EvolutionConfig returns the current evolution config (nil if disabled).
func EvolutionConfig() *config.EvolutionConfig {
	return storedEvolutionCfg
}