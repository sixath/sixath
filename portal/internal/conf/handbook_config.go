package conf

import (
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// HandbookConfig is the handbook: section of config.yaml (LLM layer of repository handbooks).
// Zero values mean the defaults; the handbook usecase applies defaults and caps.
type HandbookConfig struct {
	// Model is a catalog model name or "<provider name or ID>/<model>"; "" disables the LLM
	// layer unless a repository overrides it.
	Model               string `yaml:"model"`
	Concurrency         int    `yaml:"concurrency"`
	MaxCardsPerRun      int    `yaml:"max_cards_per_run"`
	MaxFileKB           int    `yaml:"max_file_kb"`
	MaxRunMinutes       int    `yaml:"max_run_minutes"`
	SkeletonRebuildDays int    `yaml:"skeleton_rebuild_days"`
}

type handbookConfigYAML struct {
	Handbook *HandbookConfig `yaml:"handbook"`
}

// LoadHandbookFromConfigPath reads handbook.* from the -conf file/dir config.yaml.
// A missing file or section yields the zero config. Env SATH_HANDBOOK_MODEL overrides model.
func LoadHandbookFromConfigPath(confPath string) (*HandbookConfig, error) {
	out := &HandbookConfig{}
	for _, p := range resolveConfigYAMLPaths(confPath) {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var raw handbookConfigYAML
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw.Handbook != nil {
			*out = *raw.Handbook
		}
	}
	out.Model = strings.TrimSpace(out.Model)
	EnrichHandbookFromEnv(out)
	return out, nil
}

// EnrichHandbookFromEnv overlays SATH_HANDBOOK_MODEL when set.
func EnrichHandbookFromEnv(c *HandbookConfig) {
	if c == nil {
		return
	}
	if v := strings.TrimSpace(os.Getenv("SATH_HANDBOOK_MODEL")); v != "" {
		c.Model = v
	}
}
