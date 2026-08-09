package chat

import (
	"github.com/sixath/framework/tool"
)

// RegisterWorkspaceFileTools registers read_file, write_file, patch, search_files (opt-in).
// Danger-path writes (.env, keys, …) require confirm_token when PendingStore is wired.
func RegisterWorkspaceFileTools(reg *tool.Registry) error {
	if reg == nil {
		return nil
	}
	return tool.RegisterWorkspaceFileToolsWithConfig(reg, &tool.WorkspaceFileConfig{
		PendingStore: tool.NewInMemoryWorkspaceFilePendingStore(),
		TokenGen:     tool.RandomTokenGenerator{},
	})
}
