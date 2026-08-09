package conf

import (
	"os"
	"strconv"
	"strings"

	fwconfig "github.com/sixath/framework/config"
)

// EnrichWebToolsFromEnv 用环境变量补全 web 配置（YAML 未填写的字段）。
func EnrichWebToolsFromEnv(w *fwconfig.WebTools) {
	if w == nil {
		return
	}
	if strings.TrimSpace(w.SearchBackend) == "" {
		if v := strings.TrimSpace(os.Getenv("WEB_SEARCH_BACKEND")); v != "" {
			w.SearchBackend = v
		}
	}
	if strings.TrimSpace(w.BochaAPIKey) == "" {
		if v := strings.TrimSpace(os.Getenv("BOCHA_API_KEY")); v != "" {
			w.BochaAPIKey = v
		}
	}
	if strings.TrimSpace(w.TavilyAPIKey) == "" {
		if v := strings.TrimSpace(os.Getenv("TAVILY_API_KEY")); v != "" {
			w.TavilyAPIKey = v
		}
	}
	if w.DefaultCount <= 0 {
		if v := strings.TrimSpace(os.Getenv("WEB_SEARCH_DEFAULT_COUNT")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				w.DefaultCount = n
			}
		}
	}
}
