package conf

import (
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// RuntimeConfig is the portal Runtime gate (service token for Gateway→Portal).
type RuntimeConfig struct {
	ServiceToken string `yaml:"service_token"`
}

type runtimeConfigYAML struct {
	Runtime *RuntimeConfig `yaml:"runtime"`
}

// LoadRuntimeFromConfigPath reads runtime.* from -conf file/dir config.yaml
// (bypass of conf.proto, same pattern as LoadWebToolsFromConfigPath).
// Empty service_token is left empty so runtime.Auth returns 401; local/docker
// yaml should set the token explicitly (e.g. dev-runtime-token).
func LoadRuntimeFromConfigPath(confPath string) (*RuntimeConfig, error) {
	out := &RuntimeConfig{}
	for _, p := range resolveConfigYAMLPaths(confPath) {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var raw runtimeConfigYAML
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw.Runtime != nil {
			if tok := strings.TrimSpace(raw.Runtime.ServiceToken); tok != "" {
				out.ServiceToken = tok
			}
		}
	}
	EnrichRuntimeFromEnv(out)
	return out, nil
}

// EnrichRuntimeFromEnv overlays SATH_RUNTIME_TOKEN when set.
func EnrichRuntimeFromEnv(r *RuntimeConfig) {
	if r == nil {
		return
	}
	if tok := strings.TrimSpace(os.Getenv("SATH_RUNTIME_TOKEN")); tok != "" {
		r.ServiceToken = tok
	}
}
