package chat

import (
	"os"
	"strings"

	"github.com/sixath/framework/config"
	"gopkg.in/yaml.v3"
)

var storedEvolutionCfg *config.EvolutionConfig
var storedEvolutionConfigPath string

// SetEvolutionConfig stores agent_extra evolution settings.
func SetEvolutionConfig(cfg *config.EvolutionConfig) {
	if cfg == nil {
		storedEvolutionCfg = nil
		return
	}
	cp := *cfg
	storedEvolutionCfg = &cp
}

// SetEvolutionConfigPath stores the agent_extra.yaml path for later persistence.
func SetEvolutionConfigPath(path string) {
	storedEvolutionConfigPath = path
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

// UpdateEvolutionConfig updates the in-memory config and persists to agent_extra.yaml.
// If path is not set (empty), only in-memory update is performed.
func UpdateEvolutionConfig(cfg *config.EvolutionConfig) error {
	SetEvolutionConfig(cfg)
	if storedEvolutionConfigPath == "" {
		return nil
	}
	return persistEvolutionConfig()
}

// persistEvolutionConfig reads the YAML file, updates the evolution key, and writes back.
func persistEvolutionConfig() error {
	// Read entire file as generic map to preserve other top-level keys.
	data, err := os.ReadFile(storedEvolutionConfigPath)
	if err != nil {
		return err
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root == nil {
		root = make(map[string]any)
	}
	if storedEvolutionCfg == nil {
		delete(root, "evolution")
	} else {
		root["evolution"] = storedEvolutionCfg
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(storedEvolutionConfigPath, out, 0644)
}