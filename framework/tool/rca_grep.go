package tool

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	rcaGrepContextDefault = 3
	rcaGrepContextMax     = 8
)

func rcaGrepSkipRelPath(rel string) bool {
	slash := filepath.ToSlash(rel)
	parts := strings.Split(slash, "/")
	for _, p := range parts {
		if p == "vendor" {
			return true
		}
	}
	base := parts[len(parts)-1]
	if strings.HasSuffix(base, "_gen.go") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(base), ".txt")
}

func rcaGrepContextLines(params map[string]any) int {
	n := intFromParam(params["context"], rcaGrepContextDefault)
	if n < 0 {
		return 0
	}
	if n > rcaGrepContextMax {
		return rcaGrepContextMax
	}
	return n
}

func searchRCAFileContents(root, pattern, fileGlob string, limit int) ([]contentMatch, error) {
	if limit <= 0 {
		return nil, nil
	}
	if results, err := searchWithRipgrepRCA(root, pattern, fileGlob, limit); err == nil {
		return results, nil
	}
	return searchRCAFileContentsWalk(root, pattern, fileGlob, limit)
}

func searchWithRipgrepRCA(root, pattern, fileGlob string, limit int) ([]contentMatch, error) {
	if _, err := exec.LookPath("rg"); err != nil {
		return nil, err
	}
	args := []string{
		"--no-heading", "--line-number", "--color=never",
		"--glob", "!**/vendor/**",
		"--glob", "!*_gen.go",
		"--glob", "!*.txt",
	}
	if fileGlob != "" {
		args = append(args, "--glob", fileGlob)
	}
	args = append(args, pattern, root)
	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var all []contentMatch
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		absPath, lineNo, content, ok := parseRipgrepMatchLine(line)
		if !ok {
			continue
		}
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			rel = absPath
		}
		rel = filepath.ToSlash(rel)
		if rcaGrepSkipRelPath(rel) {
			continue
		}
		all = append(all, contentMatch{
			Path:    rel,
			Line:    lineNo,
			Content: content,
		})
	}
	return slicePage(all, limit, 0), nil
}

func searchRCAFileContentsWalk(root, pattern, fileGlob string, limit int) ([]contentMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}
	var all []contentMatch
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := rcaSymbolSkipDirNames[d.Name()]; skip {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rcaGrepSkipRelPath(rel) {
			return nil
		}
		if fileGlob != "" {
			ok, err := matchFileGlob(fileGlob, rel, d.Name())
			if err != nil || !ok {
				return err
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if re.FindStringIndex(line) != nil {
				all = append(all, contentMatch{Path: rel, Line: lineNo, Content: line})
				if limit > 0 && len(all) >= limit {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return slicePage(all, limit, 0), nil
}

func attachRCAGrepContext(root string, matches []contentMatch, contextLines int) []contentMatch {
	if len(matches) == 0 {
		return matches
	}
	cache := make(map[string][]string, 8)
	out := make([]contentMatch, len(matches))
	for i, m := range matches {
		out[i] = m
		lines, ok := cache[m.Path]
		if !ok {
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.Path)))
			if err != nil {
				out[i].Content = numberedSourceWindow([]string{m.Content}, 1, 1)
				continue
			}
			text := strings.TrimSuffix(string(b), "\n")
			text = strings.TrimSuffix(text, "\r")
			lines = strings.Split(text, "\n")
			cache[m.Path] = lines
		}
		start := m.Line - contextLines
		end := m.Line + contextLines
		out[i].Content = numberedSourceWindow(lines, start, end)
	}
	return out
}

func numberedSourceWindow(lines []string, start, end int) string {
	if len(lines) == 0 {
		return ""
	}
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&b, "%d|%s\n", i+1, lines[i])
	}
	return strings.TrimSuffix(b.String(), "\n")
}
