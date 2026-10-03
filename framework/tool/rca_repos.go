package tool

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	fwws "github.com/sixath/framework/workspace"
)

// repoNameFromRoot 返回仓库根目录的基名,作为该仓库的逻辑名。
func repoNameFromRoot(root string) string {
	return filepath.Base(filepath.Clean(root))
}

// selectRoots 根据可选 repo 名从 roots 中筛选目标根。
// repo 为空返回全部;否则返回名字匹配的单个根。roots 为空或 repo 未命中时报错。
func selectRoots(roots []string, repo string) ([]string, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("rca: no repository roots configured (rca.repos.roots is empty)")
	}
	if repo == "" {
		return roots, nil
	}
	for _, r := range roots {
		if repoNameFromRoot(r) == repo {
			return []string{r}, nil
		}
	}
	return nil, fmt.Errorf("rca: unknown repo %q; configured repos are %v", repo, repoNames(roots))
}

// repoNames 返回全部仓库逻辑名,用于错误提示。
func repoNames(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, repoNameFromRoot(r))
	}
	return out
}

// resolveInRepos 解析 repo 内相对路径 rel 为绝对路径,并用 ResolveWorkspacePath 拒绝越权。
// repo 必填(用于读单个文件的场景);返回 (绝对路径, 命中的仓库根, error)。
func resolveInRepos(roots []string, repo, rel string) (string, string, error) {
	if repo == "" {
		return "", "", fmt.Errorf("rca: repo is required to resolve a specific path")
	}
	sel, err := selectRoots(roots, repo)
	if err != nil {
		return "", "", err
	}
	root := sel[0]
	full, err := fwws.ResolveWorkspacePath(root, rel)
	if err != nil {
		return "", "", err
	}
	return full, root, nil
}

// repoCheck 是 rca_* 工具 repo 参数的候选规则。
func repoCheck(roots []string) OneOf {
	return OneOf{
		Param: "repo",
		Source: func(context.Context, map[string]any) ([]string, error) {
			return repoNames(roots), nil
		},
		Hint: "repo must be one of the configured repositories (basename of a code root)",
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
