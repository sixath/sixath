package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the gateway process configuration.
// Web user auth uses opaque Bearer tokens via Portal GET /api/v1/auth/me;
// there is no jwt_secret on the gateway.
type Config struct {
	Listen         string `yaml:"listen"`
	PortalBaseURL  string `yaml:"portal_base_url"`
	RuntimeToken   string `yaml:"runtime_token"`
	TurnTimeoutSec int    `yaml:"turn_timeout_sec"`
	// ChannelsFile is deprecated. Runtime channel config comes from Portal
	// (GET /runtime/v1/gateway/channels). Kept only for one-shot import tooling.
	ChannelsFile string `yaml:"channels_file"`
}

// Load reads and validates a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8088"
	}
	if cfg.PortalBaseURL == "" {
		return nil, fmt.Errorf("portal_base_url is required")
	}
	if cfg.RuntimeToken == "" {
		return nil, fmt.Errorf("runtime_token is required")
	}
	if cfg.TurnTimeoutSec <= 0 {
		cfg.TurnTimeoutSec = 600
	}
	// channels_file is ignored at runtime; optional default retained for import scripts.
	if cfg.ChannelsFile == "" {
		cfg.ChannelsFile = "./configs/channels.yaml"
	}
	if v := os.Getenv("SATH_RUNTIME_TOKEN"); v != "" {
		cfg.RuntimeToken = v
	}
	return &cfg, nil
}
