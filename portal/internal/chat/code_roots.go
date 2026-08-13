package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxCodeBrowseDepth   = 32
	MaxCodeBrowseEntries = 500
	WorkspaceCodeLink    = "code"
)

// CodeDirEntry is a directory entry under a code root.
type CodeDirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"` // relative to root
	Type string `json:"type"` // "dir"
}

// NormalizeCodeRoots trims, skips empty, and returns absolute Clean paths.
func NormalizeCodeRoots(roots []string) []string {
	if len(roots) == 0 {
		return nil
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		out = append(out, filepath.Clean(abs))
	}
	return out
}

// ResolveUnderRoot joins root with a relative path, rejecting escapes.
func ResolveUnderRoot(root, rel string) (string, error) {
	root = filepath.Clean(root)
	if root == "" || root == "." {
		return "", fmt.Errorf("empty code root")
	}
	if rel != "" && filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes root")
	}
	for _, p := range strings.Split(cleaned, string(os.PathSeparator)) {
		if p == ".." {
			return "", fmt.Errorf("path escapes root")
		}
	}
	joined := root
	if cleaned != "." && cleaned != "" {
		joined = filepath.Join(root, cleaned)
	}
	joined = filepath.Clean(joined)

	rootEval, err := evalIfExists(root)
	if err != nil {
		return "", err
	}
	target := joined
	if _, err := os.Lstat(joined); err == nil {
		target, err = filepath.EvalSymlinks(joined)
		if err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	target = filepath.Clean(target)
	if !underRoot(target, rootEval) {
		return "", fmt.Errorf("path escapes root")
	}
	// When EvalSymlinks does not change the path form, return joined so it
	// matches filepath.Join(root, ...) on Windows TempDir paths.
	if filepath.Clean(target) == joined {
		return joined, nil
	}
	return target, nil
}

func evalIfExists(p string) (string, error) {
	p = filepath.Clean(p)
	if _, err := os.Lstat(p); err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return "", err
	}
	eval, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(eval), nil
}

func underRoot(target, root string) bool {
	target = filepath.Clean(target)
	root = filepath.Clean(root)
	if target == root {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(target, root+sep)
}

// ListCodeDirs lists directories under root/rel (relative path field).
func ListCodeDirs(root, rel string) ([]CodeDirEntry, error) {
	abs, err := ResolveUnderRoot(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	baseRel := ""
	if rel != "" {
		baseRel = filepath.ToSlash(filepath.Clean(rel))
		if baseRel == "." {
			baseRel = ""
		}
	}
	out := make([]CodeDirEntry, 0, len(entries))
	for _, e := range entries {
		if len(out) >= MaxCodeBrowseEntries {
			break
		}
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		relPath := name
		if baseRel != "" {
			relPath = baseRel + "/" + name
		}
		out = append(out, CodeDirEntry{
			Name: name,
			Path: relPath,
			Type: "dir",
		})
	}
	return out, nil
}

// WorkspaceUnderCodeRoots reports whether workspace is under any code root.
func WorkspaceUnderCodeRoots(workspace string, roots []string) bool {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return false
	}
	ws, err := evalIfExists(workspace)
	if err != nil {
		return false
	}
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		rootEval, err := evalIfExists(r)
		if err != nil {
			continue
		}
		if underRoot(ws, rootEval) {
			return true
		}
	}
	return false
}
