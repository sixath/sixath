package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureCLIRoot returns an absolute writable workspace root.
// Blank input falls back to {cwd}/.sath/workspace. Always MkdirAll.
func EnsureCLIRoot(workspace string) (string, error) {
	ws := strings.TrimSpace(workspace)
	if ws == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace: getwd: %w", err)
		}
		ws = filepath.Join(cwd, ".sath", "workspace")
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return "", fmt.Errorf("workspace: mkdir: %w", err)
	}
	abs, err := filepath.Abs(ws)
	if err != nil {
		return "", fmt.Errorf("workspace: abs: %w", err)
	}
	return abs, nil
}
