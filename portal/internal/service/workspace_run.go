package service

import (
	"backend/internal/biz"
	"backend/internal/chat"
)

// requireRunWorkspace rejects an empty or whole-repo workspace before a Run.
func requireRunWorkspace(workspace string, codeRoots []string) error {
	if err := biz.RequireWorkspaceRoot(workspace); err != nil {
		return err
	}
	if chat.WorkspaceUnderCodeRoots(workspace, codeRoots) {
		return biz.ErrWorkspaceWholeRepoRetired
	}
	return nil
}
