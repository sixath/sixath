package cli

import (
	"fmt"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/workspace"
)

func applyCLIWorkspace(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("workspace: config is nil")
	}
	ws, err := workspace.EnsureCLIRoot(cfg.Workspace)
	if err != nil {
		return err
	}
	cfg.Workspace = ws
	return nil
}
