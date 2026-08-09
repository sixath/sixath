package chat

import (
	"strings"

	"backend/internal/conf"

	fwconfig "github.com/sixath/framework/config"
)

// InitWebSettings 从 config.yaml、agent_extra、环境变量加载并设置进程级 Web 配置。
func InitWebSettings(confPath string, extra *fwconfig.PortalAgentExtra) {
	s := WebSettings{DefaultCount: 8, DefaultSummary: true}
	if wt, err := conf.LoadWebToolsFromConfigPath(confPath); err == nil && wt != nil {
		conf.EnrichWebToolsFromEnv(wt)
		s = MergeWebSettings(s, wt)
	}
	if extra != nil && extra.Web != nil {
		wt := *extra.Web
		conf.EnrichWebToolsFromEnv(&wt)
		s = MergeWebSettings(s, &wt)
	}
	if !webKeysPresent(s) {
		wt := &fwconfig.WebTools{}
		conf.EnrichWebToolsFromEnv(wt)
		s = MergeWebSettings(s, wt)
	}
	if s.DefaultCount <= 0 {
		s.DefaultCount = 8
	}
	// 最后再补环境变量中的 key/backend。
	wt := &fwconfig.WebTools{
		SearchBackend: s.SearchBackend,
		BochaAPIKey:   s.BochaAPIKey,
		TavilyAPIKey:  s.TavilyAPIKey,
	}
	conf.EnrichWebToolsFromEnv(wt)
	s = MergeWebSettings(s, wt)
	if s.DefaultCount <= 0 {
		s.DefaultCount = 8
	}
	SetWebSettings(s)
}

func webKeysPresent(s WebSettings) bool {
	return strings.TrimSpace(s.BochaAPIKey) != "" || strings.TrimSpace(s.TavilyAPIKey) != ""
}
