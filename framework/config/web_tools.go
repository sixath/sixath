package config

// WebTools Bocha/Tavily 等联网搜索配置（agent_extra.yaml 或 Bootstrap web 节）。
type WebTools struct {
	// ToolsEnabled 为 true 时注册 web_search/web_extract；未设置且已配置 API key 时 Portal 可自动开启。
	ToolsEnabled      *bool  `json:"tools_enabled" yaml:"tools_enabled"`
	SearchBackend     string `json:"search_backend" yaml:"search_backend"` // bocha | tavily
	BochaAPIKey       string `json:"bocha_api_key" yaml:"bocha_api_key"`
	TavilyAPIKey      string `json:"tavily_api_key" yaml:"tavily_api_key"`
	DefaultCount      int    `json:"default_count" yaml:"default_count"`
	DefaultSummary    *bool  `json:"default_summary" yaml:"default_summary"`
}
