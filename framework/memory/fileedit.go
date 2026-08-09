package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TargetRelPath maps a memory target to its workspace-relative file path.
// "user" is accepted only as a compatibility alias for legacy callers.
func TargetRelPath(target string) string {
	switch target {
	case "memory":
		return "MEMORY.md"
	case "user_file", "user":
		return "USER.md"
	default:
		return ""
	}
}

// ApplyMemoryAction applies an add, replace, or remove operation to a memory file.
func ApplyMemoryAction(body []byte, action, content, oldText string) ([]byte, error) {
	s := string(body)
	switch action {
	case string(ActionAdd):
		entry := strings.TrimRight(content, "\n") + "\n"
		if len(s) > 0 && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		return []byte(s + entry), nil
	case string(ActionReplace):
		if !strings.Contains(s, oldText) {
			return nil, fmt.Errorf("old_text not found in memory file")
		}
		if strings.Count(s, oldText) > 1 {
			return nil, fmt.Errorf("old_text is ambiguous (multiple matches)")
		}
		return []byte(strings.Replace(s, oldText, content, 1)), nil
	case string(ActionRemove):
		if !strings.Contains(s, oldText) {
			return nil, fmt.Errorf("old_text not found in memory file")
		}
		if strings.Count(s, oldText) > 1 {
			return nil, fmt.Errorf("old_text is ambiguous (multiple matches)")
		}
		return []byte(strings.Replace(s, oldText, "", 1)), nil
	default:
		return nil, fmt.Errorf("unsupported action %q", action)
	}
}

// AtomicWriteFile writes body through a temporary sibling file, then atomically renames it.
func AtomicWriteFile(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
