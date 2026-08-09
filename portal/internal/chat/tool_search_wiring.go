package chat

import (
	"context"

	"github.com/sixath/framework/tool"
)

// RegisterToolSearchIfNeeded registers tool_search/describe/call when activation conditions met.
// Returns (active bool, err error).
func RegisterToolSearchIfNeeded(ctx context.Context, reg *tool.Registry, catalog tool.ToolCatalog) (bool, error) {
	cfg := tool.ToolSearchConfigFromEnv()
	if !tool.ShouldActivateToolSearch(catalog, cfg) {
		return false, nil
	}
	err := tool.RegisterToolSearchTools(reg, tool.ToolSearchRegisterConfig{
		Registry: reg,
		Catalog:  catalog,
	})
	return err == nil, err
}
