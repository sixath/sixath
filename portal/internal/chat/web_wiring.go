package chat

import (
	"github.com/sixath/framework/tool"
)

// RegisterWebTools registers web_search and web_extract when WebToolsShouldRegister() is true.
func RegisterWebTools(reg *tool.Registry) error {
	return registerWebTools(reg, false)
}

// registerWebTools registers web tools; force=true skips WebToolsShouldRegister (Hermes P0 flag path).
func registerWebTools(reg *tool.Registry, force bool) error {
	if reg == nil || (!force && !WebToolsShouldRegister()) {
		return nil
	}
	s := WebSettingsSnapshot()
	count := s.DefaultCount
	if count <= 0 {
		count = 8
	}
	return tool.RegisterWebTools(reg, &tool.WebToolsConfig{
		SearchBackend:  tool.NewWebSearchBackend(s.SearchBackend, s.BochaAPIKey, s.TavilyAPIKey),
		DefaultCount:   count,
		DefaultSummary: s.DefaultSummary,
	})
}
