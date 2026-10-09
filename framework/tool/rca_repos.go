package tool

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	fwws "github.com/sixath/framework/workspace"
)

// RCARoot 是带逻辑名的仓库根。Name 在同一组根内唯一，是 rca_* 工具 repo 参数的取值。
type RCARoot struct {
	Name string
	Path string
}

// NamedRCARoots 为路径列表生成逻辑名：默认取 basename；basename 冲突时逐级加上父目录
// （如 cloudgame/gateway 与 migu/gateway），直到唯一；仍冲突时改用完整路径。空路径与重复路径被丢弃。
func NamedRCARoots(paths []string) []RCARoot {
	out := make([]RCARoot, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, RCARoot{Name: rcaPathSuffix(p, 1), Path: p})
	}
	for depth := 2; ; depth++ {
		dups := duplicateRCANames(out)
		if len(dups) == 0 {
			return out
		}
		changed := false
		for i := range out {
			if _, ok := dups[out[i].Name]; !ok {
				continue
			}
			if n := rcaPathSuffix(out[i].Path, depth); n != out[i].Name {
				out[i].Name = n
				changed = true
			}
		}
		if !changed {
			// 路径段完全相同（如 /a/gw 与 a/gw）时无法再加父目录，退回完整路径；路径已去重，故必唯一。
			for i := range out {
				if _, ok := dups[out[i].Name]; ok {
					out[i].Name = filepath.ToSlash(out[i].Path)
				}
			}
			slog.Warn("rca: repo names still collide after qualification; using full paths", "names", sortedNameSet(dups))
			return out
		}
		slog.Info("rca: repo basenames collide; using parent-qualified names", "names", sortedNameSet(dups))
	}
}

func rcaPathSuffix(p string, n int) string {
	parts := strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return p
	}
	if n > len(parts) {
		n = len(parts)
	}
	return strings.Join(parts[len(parts)-n:], "/")
}

func duplicateRCANames(roots []RCARoot) map[string]struct{} {
	count := make(map[string]int, len(roots))
	for _, r := range roots {
		count[r.Name]++
	}
	dups := map[string]struct{}{}
	for name, c := range count {
		if c > 1 {
			dups[name] = struct{}{}
		}
	}
	return dups
}

func sortedNameSet(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validateRCARoots 要求每个根都有非空 Name/Path，且 Name 唯一。
func validateRCARoots(roots []RCARoot) error {
	seen := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Path) == "" {
			return fmt.Errorf("rca: root name and path are required (name=%q path=%q)", r.Name, r.Path)
		}
		if _, ok := seen[r.Name]; ok {
			return fmt.Errorf("rca: duplicate repo name %q", r.Name)
		}
		seen[r.Name] = struct{}{}
	}
	return nil
}

// selectRoots 根据可选 repo 名从 roots 中筛选目标根。
// repo 为空返回全部;否则返回逻辑名匹配的单个根。roots 为空或 repo 未命中时报错。
func selectRoots(roots []RCARoot, repo string) ([]RCARoot, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("rca: no repository roots configured (rca.repos.roots is empty)")
	}
	if repo == "" {
		return roots, nil
	}
	for _, r := range roots {
		if r.Name == repo {
			return []RCARoot{r}, nil
		}
	}
	return nil, fmt.Errorf("rca: unknown repo %q; configured repos are %v", repo, repoNames(roots))
}

// repoNames 返回全部仓库逻辑名,用于错误提示。
func repoNames(roots []RCARoot) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, r.Name)
	}
	return out
}

// resolveInRepos 解析 repo 内相对路径 rel 为绝对路径,并用 ResolveWorkspacePath 拒绝越权。
// repo 必填(用于读单个文件的场景);返回 (绝对路径, 命中的仓库根路径, error)。
func resolveInRepos(roots []RCARoot, repo, rel string) (string, string, error) {
	if repo == "" {
		return "", "", fmt.Errorf("rca: repo is required to resolve a specific path")
	}
	sel, err := selectRoots(roots, repo)
	if err != nil {
		return "", "", err
	}
	root := sel[0].Path
	full, err := fwws.ResolveWorkspacePath(root, rel)
	if err != nil {
		return "", "", err
	}
	return full, root, nil
}

// repoCheck 是 rca_* 工具 repo 参数的候选规则。
func repoCheck(roots []RCARoot) OneOf {
	return OneOf{
		Param: "repo",
		Source: func(context.Context, map[string]any) ([]string, error) {
			return repoNames(roots), nil
		},
		Hint: "repo must be one of the configured repositories (the repo field returned by rca_grep / rca_glob)",
	}
}

const rcaSimilarScanLimit = 2000

// suggestRCAFiles 为不存在的相对路径给出候选：先同目录按文件名，再在仓库内按文件名（遍历至多 rcaSimilarScanLimit 个文件）。
func suggestRCAFiles(root, rel string) []string {
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)
	relDir := filepath.Dir(filepath.FromSlash(rel))
	if entries, err := os.ReadDir(filepath.Join(root, relDir)); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if s := Suggest(base, names, suggestN); len(s) > 0 {
			out := make([]string, len(s))
			for i, n := range s {
				out[i] = filepath.ToSlash(filepath.Join(relDir, n))
			}
			return out
		}
	}
	var paths, names []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(paths) >= rcaSimilarScanLimit {
			return filepath.SkipAll
		}
		r, _ := filepath.Rel(root, p)
		paths = append(paths, filepath.ToSlash(r))
		names = append(names, d.Name())
		return nil
	})
	var out []string
	for _, n := range Suggest(base, names, suggestN) {
		for i, nm := range names {
			if nm == n {
				out = append(out, paths[i])
				break
			}
		}
	}
	return out
}
