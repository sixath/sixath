package chat

import (
	"strings"

	fwconfig "github.com/sixath/framework/config"
)

// WebSettings 进程级 web_search 配置（YAML / agent_extra / 环境变量合并后）。
type WebSettings struct {
	// ToolsEnabledExplicit 非 nil 时强制开/关；nil 表示有 API key 则自动注册。
	ToolsEnabledExplicit *bool
	SearchBackend      string
	BochaAPIKey        string
	TavilyAPIKey       string
	DefaultCount       int
	DefaultSummary     bool
}

var globalWebSettings WebSettings

// SetWebSettings 由 main 在加载配置后调用。
func SetWebSettings(s WebSettings) {
	globalWebSettings = s
}

// WebSettingsSnapshot 返回当前配置副本。
func WebSettingsSnapshot() WebSettings {
	return globalWebSettings
}

// WebToolsConfigured 是否已配置可用的搜索 API key。
func WebToolsConfigured() bool {
	s := globalWebSettings
	switch effectiveSearchBackend(s) {
	case "tavily":
		return strings.TrimSpace(s.TavilyAPIKey) != ""
	default:
		return strings.TrimSpace(s.BochaAPIKey) != ""
	}
}

// WebToolsShouldRegister 是否为本轮 Agent 注册 web_search/web_extract。
func WebToolsShouldRegister() bool {
	s := globalWebSettings
	if s.ToolsEnabledExplicit != nil {
		return *s.ToolsEnabledExplicit
	}
	return WebToolsConfigured()
}

// MergeWebSettings 用 overlay 非空字段覆盖 base。
func MergeWebSettings(base WebSettings, overlay *fwconfig.WebTools) WebSettings {
	if overlay == nil {
		return base
	}
	if overlay.ToolsEnabled != nil {
		v := *overlay.ToolsEnabled
		base.ToolsEnabledExplicit = &v
	}
	if v := strings.TrimSpace(overlay.SearchBackend); v != "" {
		base.SearchBackend = strings.ToLower(v)
	}
	if v := strings.TrimSpace(overlay.BochaAPIKey); v != "" {
		base.BochaAPIKey = v
	}
	if v := strings.TrimSpace(overlay.TavilyAPIKey); v != "" {
		base.TavilyAPIKey = v
	}
	if overlay.DefaultCount > 0 {
		base.DefaultCount = overlay.DefaultCount
	}
	if overlay.DefaultSummary != nil {
		base.DefaultSummary = *overlay.DefaultSummary
	}
	return base
}

func effectiveSearchBackend(s WebSettings) string {
	if v := strings.TrimSpace(s.SearchBackend); v != "" {
		return strings.ToLower(v)
	}
	return "bocha"
}
