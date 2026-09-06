package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrEmptyWorkspace   = errors.New("workspace root is empty")
	ErrEmptyTarget      = errors.New("target is empty")
	ErrTargetNotAllowed = errors.New("target not under allowed roots")
	ErrLinkConflict     = errors.New("workspace/code already exists with a different target")
)

// LinkCode creates {workspace}/code → target when target is under allowedRoots.
// Same target is idempotent. Different existing target returns ErrLinkConflict.
func LinkCode(workspace, target string, allowedRoots []string) (linkPath, absTarget string, err error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", "", ErrEmptyWorkspace
	}
	absTarget, err = absTargetUnderRoots(target, allowedRoots)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return "", "", err
	}
	linkPath = filepath.Join(workspace, CodeDir)
	if _, err := os.Lstat(linkPath); err == nil {
		same, sameErr := sameSymlinkTarget(linkPath, absTarget)
		if sameErr != nil {
			return "", "", sameErr
		}
		if !same {
			return "", "", ErrLinkConflict
		}
		return linkPath, absTarget, nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	if err := os.Symlink(absTarget, linkPath); err != nil {
		return "", "", err
	}
	return linkPath, absTarget, nil
}

// ResolveCodeMount returns the absolute directory of workspace/code when it exists
// as a directory or as a symlink to a directory. Empty if missing.
func ResolveCodeMount(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	p := filepath.Join(workspace, CodeDir)
	fi, err := os.Lstat(p)
	if err != nil {
		return ""
	}
	target := p
	if fi.Mode()&os.ModeSymlink != 0 {
		eval, err := filepath.EvalSymlinks(p)
		if err != nil {
			return ""
		}
		st, err := os.Stat(eval)
		if err != nil || !st.IsDir() {
			return ""
		}
		target = eval
	} else if !fi.IsDir() {
		return ""
	}
	return filepath.Clean(target)
}

func absTargetUnderRoots(target string, allowedRoots []string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ErrEmptyTarget
	}
	if strings.ContainsRune(target, 0) {
		return "", ErrEmptyTarget
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", ErrEmptyTarget
	}
	abs = filepath.Clean(abs)
	sep := string(os.PathSeparator)
	for _, root := range normalizeRoots(allowedRoots) {
		if abs != root && !strings.HasPrefix(abs, root+sep) {
			continue
		}
		resolved := abs
		if _, err := os.Lstat(abs); err == nil {
			eval, evalErr := filepath.EvalSymlinks(abs)
			if evalErr != nil {
				return "", evalErr
			}
			resolved = filepath.Clean(eval)
			if resolved != root && !strings.HasPrefix(resolved, root+sep) {
				return "", ErrTargetNotAllowed
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
		return resolved, nil
	}
	return "", ErrTargetNotAllowed
}

func normalizeRoots(roots []string) []string {
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

func sameSymlinkTarget(link, wantTarget string) (bool, error) {
	existing, err := resolveLinkTarget(link)
	if err != nil {
		return false, err
	}
	want, err := resolveLinkTarget(wantTarget)
	if err != nil {
		return false, err
	}
	return normalizePathForCompare(existing) == normalizePathForCompare(want), nil
}

func resolveLinkTarget(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", os.ErrNotExist
	}
	if eval, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(eval), nil
	}
	if rl, err := os.Readlink(path); err == nil {
		rl = strings.TrimSpace(rl)
		if abs, absErr := filepath.Abs(rl); absErr == nil {
			return filepath.Clean(abs), nil
		}
		return filepath.Clean(rl), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func normalizePathForCompare(p string) string {
	p = filepath.Clean(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, `\\?\`)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
