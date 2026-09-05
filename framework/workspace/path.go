package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveWorkspacePath resolves rel under workspaceRoot and rejects path traversal.
// rel may be workspace-relative or absolute (must still lie under workspaceRoot).
func ResolveWorkspacePath(workspaceRoot, rel string) (string, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return "", fmt.Errorf("pathguard: workspace root is empty")
	}
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("pathguard: path is empty")
	}

	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("pathguard: resolve workspace root: %w", err)
	}
	rootClean := filepath.Clean(rootAbs)

	var full string
	if filepath.IsAbs(rel) {
		absP, err := filepath.Abs(rel)
		if err != nil {
			return "", fmt.Errorf("pathguard: resolve absolute path: %w", err)
		}
		full = filepath.Clean(absP)
	} else {
		full = filepath.Clean(filepath.Join(rootClean, rel))
	}

	if full != rootClean && !strings.HasPrefix(full, rootClean+string(filepath.Separator)) {
		return "", fmt.Errorf("pathguard: path resolves outside workspace (root %q, resolved %q)", rootClean, full)
	}
	return full, nil
}
