package biz

import (
	"os"
	"path/filepath"
	"strings"
)

// SessionWorkspaceDir returns workspace/sessions/<sessionID> when both inputs are non-empty.
func SessionWorkspaceDir(workspace, sessionID string) string {
	workspace = strings.TrimSpace(workspace)
	sessionID = strings.TrimSpace(sessionID)
	if workspace == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(workspace, "sessions", sessionID)
}

// RemoveSessionWorkspaceDir removes workspace/sessions/<sessionID> (uploads tree). Best-effort.
func RemoveSessionWorkspaceDir(workspace, sessionID string) error {
	dir := SessionWorkspaceDir(workspace, sessionID)
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}
