package growth

import (
	"os"
	"path/filepath"
	"strings"
)

// Learnings filenames under a .learnings directory (self-improving-agent convention).
var learningsFiles = []string{"LEARNINGS.md", "ERRORS.md", "FEATURE_REQUESTS.md"}

// ReadWorkspaceLearnings 聚合 workspace 及其上级目录中的 .learnings/*.md，供技能复盘 prompt 使用。
// maxRunes <= 0 时不截断。
func ReadWorkspaceLearnings(workspace string, maxRunes int) string {
	dirs := DiscoverLearningsDirs(workspace)
	if len(dirs) == 0 {
		return ""
	}
	var parts []string
	for _, dir := range dirs {
		for _, name := range learningsFiles {
			p := filepath.Join(dir, name)
			data, err := os.ReadFile(p)
			if err != nil || len(data) == 0 {
				continue
			}
			parts = append(parts, "## "+name+" (from "+dir+")\n"+strings.TrimSpace(string(data)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, "\n\n")
	return truncateRunes(out, maxRunes) // shared with runner_llm.go
}

// DiscoverLearningsDirs 返回可用的 .learnings 目录（workspace、上级目录、SATH_LEARNINGS_DIR）。
func DiscoverLearningsDirs(workspace string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "" || dir == "." {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	if workspace != "" {
		add(filepath.Join(workspace, ".learnings"))
		cur := workspace
		for i := 0; i < 6; i++ {
			add(filepath.Join(cur, ".learnings"))
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	if extra := strings.TrimSpace(os.Getenv("SATH_LEARNINGS_DIR")); extra != "" {
		add(extra)
	}
	return out
}
