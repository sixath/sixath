package biz

import (
	"bufio"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RepoScanMaxDepth matches chat.MaxCodeBrowseDepth.
const RepoScanMaxDepth = 32

// DiscoverGitRepos returns slash-separated paths (relative to root) of git repositories.
// A directory containing .git (dir or file) is a repository and is not descended into.
// Symlinked directories, node_modules and vendor are skipped; root itself is never reported.
func DiscoverGitRepos(root string, maxDepth int) ([]string, error) {
	root = filepath.Clean(root)
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("code root %q is not a directory", root)
	}
	var out []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr
			}
			return nil
		}
		if p == root || !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", "vendor":
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			out = append(out, rel)
			return filepath.SkipDir
		}
		if strings.Count(rel, "/")+1 >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// GitInfo is read directly from .git files (no git binary).
type GitInfo struct {
	Branch string
	Commit string
	Remote string
}

// ReadGitInfo reads branch, HEAD commit and origin URL (credentials stripped).
// Unborn branches yield an empty Commit without error.
func ReadGitInfo(repoDir string) (GitInfo, error) {
	var info GitInfo
	gitDir, err := resolveGitDir(repoDir)
	if err != nil {
		return info, err
	}
	commonDir := resolveCommonDir(gitDir)
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return info, err
	}
	s := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(s, "ref: "); ok {
		info.Branch = strings.TrimPrefix(ref, "refs/heads/")
		info.Commit, err = readGitRef(gitDir, commonDir, ref)
		if err != nil {
			return info, err
		}
	} else {
		info.Commit = s
	}
	info.Remote = readOriginURL(filepath.Join(commonDir, "config"))
	return info, nil
}

func resolveGitDir(repoDir string) (string, error) {
	p := filepath.Join(repoDir, ".git")
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return p, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return "", fmt.Errorf("unrecognized .git file in %q", repoDir)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoDir, dir)
	}
	return filepath.Clean(dir), nil
}

func resolveCommonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	c := strings.TrimSpace(string(b))
	if !filepath.IsAbs(c) {
		c = filepath.Join(gitDir, c)
	}
	return filepath.Clean(c)
}

func readGitRef(gitDir, commonDir, ref string) (string, error) {
	for _, dir := range []string{gitDir, commonDir} {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ref))); err == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}
	packed, err := os.ReadFile(filepath.Join(commonDir, "packed-refs"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, line := range strings.Split(string(packed), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		if sha, name, ok := strings.Cut(line, " "); ok && name == ref {
			return sha, nil
		}
	}
	return "", nil
}

func readOriginURL(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "url" {
			return stripURLCredentials(strings.TrimSpace(v))
		}
	}
	return ""
}

func stripURLCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}
