package conf

import (
	"os"
	"path/filepath"
	"strings"

	fwconfig "github.com/sixath/framework/config"
	yaml "go.yaml.in/yaml/v2"
)

// webConfigYAML 从 config.yaml 读取 web 节（含 growth.llm.web，与现有 GrowthLLM proto 并存）。
type webConfigYAML struct {
	Web *fwconfig.WebTools `yaml:"web"`
	Growth struct {
		LLM struct {
			Web *fwconfig.WebTools `yaml:"web"`
		} `yaml:"llm"`
	} `yaml:"growth"`
}

// LoadWebToolsFromConfigPath 从 -conf 指向的文件或目录下的 config.yaml 加载 web 配置。
func LoadWebToolsFromConfigPath(confPath string) (*fwconfig.WebTools, error) {
	paths := resolveConfigYAMLPaths(confPath)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var raw webConfigYAML
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		out := mergeWebTools(raw.Web, raw.Growth.LLM.Web)
		if out != nil {
			return out, nil
		}
	}
	return nil, nil
}

func mergeWebTools(top, nested *fwconfig.WebTools) *fwconfig.WebTools {
	if top == nil && nested == nil {
		return nil
	}
	var out fwconfig.WebTools
	if top != nil {
		out = *top
	}
	if nested != nil {
		if nested.ToolsEnabled != nil {
			out.ToolsEnabled = nested.ToolsEnabled
		}
		if strings.TrimSpace(nested.SearchBackend) != "" {
			out.SearchBackend = nested.SearchBackend
		}
		if strings.TrimSpace(nested.BochaAPIKey) != "" {
			out.BochaAPIKey = nested.BochaAPIKey
		}
		if strings.TrimSpace(nested.TavilyAPIKey) != "" {
			out.TavilyAPIKey = nested.TavilyAPIKey
		}
		if nested.DefaultCount > 0 {
			out.DefaultCount = nested.DefaultCount
		}
		if nested.DefaultSummary != nil {
			out.DefaultSummary = nested.DefaultSummary
		}
	}
	return &out
}

func resolveConfigYAMLPaths(confPath string) []string {
	if confPath == "" {
		return nil
	}
	if st, err := os.Stat(confPath); err == nil && !st.IsDir() {
		return []string{confPath}
	}
	names := []string{"config.yaml", "config.yml"}
	var out []string
	for _, n := range names {
		out = append(out, filepath.Join(confPath, n))
	}
	return out
}
