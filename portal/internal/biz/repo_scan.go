package biz

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// RepoScanMaxDepth matches chat.MaxCodeBrowseDepth.
const RepoScanMaxDepth = 32

// DiscoverGitRepos returns slash-separated paths (relative to root) of git repositories.
// A directory containing .git (dir or file) is a repository and is not descended into.
// Symlinked directories, node_modules and vendor are skipped; root itself is never reported.
// A symlinked root is resolved first. Subtrees that could not be read are returned in
// unreadable so callers do not mistake their repositories for missing ones.
func DiscoverGitRepos(root string, maxDepth int) (repos []string, unreadable []string, err error) {
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return nil, nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, nil, err
	}
	if !st.IsDir() {
		return nil, nil, fmt.Errorf("code root %q is not a directory", root)
	}
	relOf := func(p string) string {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return filepath.ToSlash(p)
		}
		return filepath.ToSlash(rel)
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr
			}
			unreadable = append(unreadable, relOf(p))
			if d != nil && d.IsDir() {
				return filepath.SkipDir
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
		rel := relOf(p)
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			repos = append(repos, rel)
			return filepath.SkipDir
		} else if !os.IsNotExist(err) {
			unreadable = append(unreadable, rel)
			return filepath.SkipDir
		}
		if strings.Count(rel, "/")+1 >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(repos)
	sort.Strings(unreadable)
	unreadable = slices.Compact(unreadable)
	return repos, unreadable, err
}

// underAnyPrefix reports whether slash-separated rel equals or lies under any prefix;
// an empty prefix matches everything.
func underAnyPrefix(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" || rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// GitInfo is read directly from .git files (no git binary).
type GitInfo struct {
	Branch string
	Commit string
	Remote string
}

const (
	gitSmallFileMax  = 4096
	gitPackedRefsMax = 8 << 20
)

// ReadGitInfo reads branch, HEAD commit and origin URL (credentials stripped).
// Unborn branches and malformed ref contents yield an empty Commit without error.
func ReadGitInfo(repoDir string) (GitInfo, error) {
	var info GitInfo
	gitDir, err := resolveGitDir(repoDir)
	if err != nil {
		return info, err
	}
	commonDir := resolveCommonDir(gitDir)
	head, err := readSmallFile(filepath.Join(gitDir, "HEAD"), gitSmallFileMax)
	if err != nil {
		return info, err
	}
	s := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(s, "ref: "); ok {
		if !validGitRef(ref) {
			return info, fmt.Errorf("invalid ref %q in %s", ref, filepath.Join(gitDir, "HEAD"))
		}
		info.Branch = strings.TrimPrefix(ref, "refs/heads/")
		info.Commit, err = readGitRef(gitDir, commonDir, ref)
		if err != nil {
			return info, err
		}
	} else {
		if !isGitSHA(s) {
			return info, fmt.Errorf("invalid detached HEAD commit %q in %s", s, filepath.Join(gitDir, "HEAD"))
		}
		info.Commit = s
	}
	info.Remote = readOriginURL(filepath.Join(commonDir, "config"))
	return info, nil
}

// readSmallFile reads at most max bytes from a regular file.
func readSmallFile(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return io.ReadAll(io.LimitReader(f, max))
}

func validGitRef(ref string) bool {
	ref = filepath.ToSlash(ref)
	if !strings.HasPrefix(ref, "refs/") {
		return false
	}
	for _, seg := range strings.Split(ref, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

func isGitSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
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
	b, err := readSmallFile(p, gitSmallFileMax)
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
	b, err := readSmallFile(filepath.Join(gitDir, "commondir"), gitSmallFileMax)
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
		if b, err := readSmallFile(filepath.Join(dir, filepath.FromSlash(ref)), gitSmallFileMax); err == nil {
			if sha := strings.TrimSpace(string(b)); isGitSHA(sha) {
				return sha, nil
			}
			return "", nil
		}
	}
	packed, err := readSmallFile(filepath.Join(commonDir, "packed-refs"), gitPackedRefsMax)
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
			if isGitSHA(sha) {
				return sha, nil
			}
			return "", nil
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

var urlUserinfoRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*://)[^/@]*@`)

func stripURLCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return urlUserinfoRE.ReplaceAllString(raw, "$1")
	}
	if u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}
