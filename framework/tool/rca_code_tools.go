package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	rcaMaxResultsDefault = 100
	ToolsetRCA           = "rca"
)

// RegisterRCACodeTools 注册 rca_grep / rca_glob / rca_read 三个多仓库代码检索工具。
// roots 为允许检索的仓库根白名单(pathguard 守卫作用于每个根)。
func RegisterRCACodeTools(reg *Registry, roots []string) error {
	if reg == nil {
		return errors.New("rca code tools: registry is nil")
	}
	if err := registerRCAGrepTool(reg, roots); err != nil {
		return err
	}
	if err := registerRCAGlobTool(reg, roots); err != nil {
		return err
	}
	return registerRCAReadTool(reg, roots)
}

func registerRCAGrepTool(reg *Registry, roots []string) error {
	return reg.Register(Tool{
		Name: "rca_grep",
		Description: "Search source code by regex across configured code roots (multi-repo). " +
			"Prefer this over workspace search_files and over terminal/rg for source / call-chain analysis. " +
			"Optionally limit to one repo. Returns file, line and snippet with the owning repo.",
		Toolset: ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Regex pattern for content search."},
				"repo":        map[string]any{"type": "string", "description": "Optional repo name to limit the search to a single repository root."},
				"glob":        map[string]any{"type": "string", "description": "Optional file glob filter, e.g. '*.go' or '**/*.go'."},
				"max_results": map[string]any{"type": "integer", "description": "Max results (default 100)."},
			},
			"required": []string{"pattern"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "rca_grep"
			pattern, _ := params["pattern"].(string)
			if strings.TrimSpace(pattern) == "" {
				return rcaErr(toolName, "pattern is required", ErrorPermanent), nil
			}
			repo, _ := params["repo"].(string)
			glob, _ := params["glob"].(string)
			maxResults := intFromParam(params["max_results"], rcaMaxResultsDefault)
			if maxResults <= 0 {
				maxResults = rcaMaxResultsDefault
			}
			sel, err := selectRoots(roots, repo)
			if err != nil {
				return rcaErr(toolName, err.Error(), ErrorPermanent), nil
			}
			matches := make([]map[string]any, 0, maxResults)
			truncated := false
			for _, root := range sel {
				if len(matches) >= maxResults {
					break
				}
				remaining := maxResults - len(matches)
				res, err := searchFileContents(root, root, pattern, glob, remaining+1, 0)
				if err != nil {
					return rcaErrFrom(toolName, err), nil
				}
				name := repoNameFromRoot(root)
				for _, cm := range res {
					if len(matches) >= maxResults {
						truncated = true
						break
					}
					matches = append(matches, map[string]any{
						"repo":    name,
						"file":    cm.Path,
						"line":    cm.Line,
						"snippet": cm.Content,
					})
				}
			}
			return rcaOK(toolName, map[string]any{"matches": matches, "truncated": truncated}), nil
		},
	})
}

func registerRCAGlobTool(reg *Registry, roots []string) error {
	return reg.Register(Tool{
		Name: "rca_glob",
		Description: "Find files by glob across configured code roots (multi-repo). " +
			"Supports basename patterns (go.mod, *.go) and path patterns (**/go.mod, pkg/**/*.go). " +
			"Prefer this over workspace search_files and over terminal find/dir for locating source files. " +
			"Optionally limit to one repo. Returns matching file paths with the owning repo.",
		Toolset: ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern. Examples: 'go.mod', '*.go', '**/go.mod', 'cmd/**/*.go'.",
				},
				"repo":        map[string]any{"type": "string", "description": "Optional repo name to limit to a single repository root."},
				"max_results": map[string]any{"type": "integer", "description": "Max results (default 100)."},
			},
			"required": []string{"pattern"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "rca_glob"
			pattern, _ := params["pattern"].(string)
			if strings.TrimSpace(pattern) == "" {
				return rcaErr(toolName, "pattern is required", ErrorPermanent), nil
			}
			repo, _ := params["repo"].(string)
			maxResults := intFromParam(params["max_results"], rcaMaxResultsDefault)
			if maxResults <= 0 {
				maxResults = rcaMaxResultsDefault
			}
			sel, err := selectRoots(roots, repo)
			if err != nil {
				return rcaErr(toolName, err.Error(), ErrorPermanent), nil
			}
			matches := make([]map[string]any, 0, maxResults)
			truncated := false
			for _, root := range sel {
				if len(matches) >= maxResults {
					break
				}
				remaining := maxResults - len(matches)
				res, err := searchFilesByGlob(root, root, pattern, "", remaining+1, 0)
				if err != nil {
					return rcaErrFrom(toolName, err), nil
				}
				name := repoNameFromRoot(root)
				for _, fm := range res {
					if len(matches) >= maxResults {
						truncated = true
						break
					}
					matches = append(matches, map[string]any{
						"repo": name,
						"file": fm.Path,
					})
				}
			}
			payload := map[string]any{"matches": matches, "truncated": truncated}
			if len(matches) == 0 {
				payload["hint"] = "No matches. Roots may be unset, or try basename (go.mod) / path (**/go.mod)."
			}
			return rcaOK(toolName, payload), nil
		},
	})
}

// registerRCAReadTool 注册 rca_read:按仓库读取文件并带行号,路径经守卫限制在仓库根内。
func registerRCAReadTool(reg *Registry, roots []string) error {
	return reg.Register(Tool{
		Name: "rca_read",
		Description: "Read a source file from a specific configured code root with line numbers (LINE_NUM|CONTENT). " +
			"Prefer this over terminal/type/cat and over workspace read_file when the path is under a code root. " +
			"Path is guarded to stay inside the repository root.",
		Toolset: ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo":       map[string]any{"type": "string", "description": "Repository name (basename of a configured root)."},
				"file":       map[string]any{"type": "string", "description": "Repo-relative file path."},
				"start_line": map[string]any{"type": "integer", "description": "1-based start line (default 1)."},
				"end_line":   map[string]any{"type": "integer", "description": "Inclusive end line (default end of file)."},
			},
			"required": []string{"repo", "file"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "rca_read"
			repo, _ := params["repo"].(string)
			file, _ := params["file"].(string)
			if strings.TrimSpace(repo) == "" {
				return rcaErr(toolName, "repo is required", ErrorPermanent), nil
			}
			if strings.TrimSpace(file) == "" {
				return rcaErr(toolName, "file is required", ErrorPermanent), nil
			}
			full, _, err := resolveInRepos(roots, repo, file)
			if err != nil {
				return rcaErr(toolName, err.Error(), ErrorPermanent), nil
			}
			b, err := os.ReadFile(full)
			if err != nil {
				if os.IsNotExist(err) {
					return NormalizeRCAResult(map[string]any{
						"error": "file not found", "repo": repo, "file": file,
					}, EvidenceMeta{Tool: toolName, OK: false, ErrorCode: ErrorPermanent}), nil
				}
				return rcaErrFrom(toolName, err), nil
			}
			text := strings.TrimSuffix(string(b), "\n")
			text = strings.TrimSuffix(text, "\r")
			lines := strings.Split(text, "\n")
			start := intFromParam(params["start_line"], 1)
			if start < 1 {
				start = 1
			}
			end := intFromParam(params["end_line"], len(lines))
			if end <= 0 || end > len(lines) {
				end = len(lines)
			}
			var out strings.Builder
			for i := start - 1; i < end && i < len(lines); i++ {
				fmt.Fprintf(&out, "%d|%s\n", i+1, lines[i])
			}
			return rcaOK(toolName, map[string]any{
				"repo":        repo,
				"file":        file,
				"content":     strings.TrimSuffix(out.String(), "\n"),
				"total_lines": len(lines),
			}), nil
		},
	})
}
