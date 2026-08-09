package tool

import (
	"fmt"
	"path/filepath"
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
	full, err := ResolveWorkspacePath(root, rel)
	if err != nil {
		return "", "", err
	}
	return full, root, nil
}
