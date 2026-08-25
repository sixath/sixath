package tool

import (
	"context"
	"errors"
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
			"Each hit includes ±3 lines of numbered context by default (set context=0 for the hit line only). " +
			"Skips vendor/, *_gen.go, and *.txt. Optionally limit to one repo. Returns file, line and snippet with the owning repo.",
		Toolset: ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Regex pattern for content search."},
				"repo":        map[string]any{"type": "string", "description": "Optional repo name to limit the search to a single repository root."},
				"glob":        map[string]any{"type": "string", "description": "Optional file glob filter, e.g. '*.go' or '**/*.go'."},
				"max_results": map[string]any{"type": "integer", "description": "Max results (default 100)."},
				"context":     map[string]any{"type": "integer", "description": "Lines of context before and after each hit (default 3, max 8, 0 = hit line only)."},
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
			contextLines := rcaGrepContextLines(params)
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
				res, err := searchRCAFileContents(root, pattern, glob, remaining+1)
				if err != nil {
					return rcaErrFrom(toolName, err), nil
				}
				res = attachRCAGrepContext(root, res, contextLines)
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
			"If the requested window sits inside a Go function of at most 400 lines, content expands to the whole function (requested_start_line/requested_end_line record the original window). " +
			"For .go files, also returns control_flow path tables and a same-package call_graph (caller→callee, resolved when the callee is in this file or directory). Other languages omit those fields (fail-open). " +
			"When claiming a call executes or a DB write happens, verify against control_flow path id or when, then tell the user in plain language; do not paste the path table into the final answer. Quote content verbatim in fenced blocks only when needed—do not reconstruct snippets from adjacent lines. " +
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
			reqStart := intFromParam(params["start_line"], 1)
			if reqStart < 1 {
				reqStart = 1
			}
			reqEnd := intFromParam(params["end_line"], len(lines))
			if reqEnd <= 0 || reqEnd > len(lines) {
				reqEnd = len(lines)
			}
			cf := ExtractControlFlow(b, file, reqStart, reqEnd)
			start, end, expanded, tooLarge, sig := expandGoReadWindow(lines, cf, reqStart, reqEnd)
			payload := map[string]any{
				"repo":                 repo,
				"file":                 file,
				"content":              numberedSourceWindow(lines, start, end),
				"total_lines":          len(lines),
				"start_line":           start,
				"end_line":             end,
				"requested_start_line": reqStart,
				"requested_end_line":   reqEnd,
			}
			if expanded {
				payload["expanded_to_function"] = true
			}
			if tooLarge {
				payload["expanded_to_function"] = false
				payload["function_too_large"] = true
				if sig != "" {
					payload["signature"] = sig
				}
			}
			if len(cf) > 0 {
				payload["control_flow"] = cf
				if cg := BuildCallGraph(b, full, file, cf); cg != nil {
					payload["call_graph"] = cg
				}
			}
			return rcaOK(toolName, payload), nil
		},
	})
}

const rcaReadMaxExpandLines = 400

// expandGoReadWindow expands a requested window to the enclosing Go function when the
// window sits inside one function of at most rcaReadMaxExpandLines lines.
func expandGoReadWindow(lines []string, cf []ControlFlowFunc, reqStart, reqEnd int) (start, end int, expanded, tooLarge bool, signature string) {
	start, end = reqStart, reqEnd
	encl, ok := enclosingGoFunc(cf, reqStart, reqEnd)
	if !ok {
		return start, end, false, false, ""
	}
	span := encl.EndLine - encl.StartLine + 1
	if span > rcaReadMaxExpandLines {
		sig := ""
		if encl.StartLine >= 1 && encl.StartLine <= len(lines) {
			sig = strings.TrimSpace(lines[encl.StartLine-1])
		}
		if sig == "" && encl.Function != "" {
			sig = "func " + encl.Function
		}
		return start, end, false, true, sig
	}
	if encl.StartLine == reqStart && encl.EndLine == reqEnd {
		return start, end, false, false, ""
	}
	ns, ne := encl.StartLine, encl.EndLine
	if ns < 1 {
		ns = 1
	}
	if ne > len(lines) {
		ne = len(lines)
	}
	return ns, ne, true, false, ""
}

func enclosingGoFunc(cf []ControlFlowFunc, start, end int) (ControlFlowFunc, bool) {
	var best ControlFlowFunc
	found := false
	for _, f := range cf {
		if f.StartLine <= start && f.EndLine >= end {
			if !found || (f.EndLine-f.StartLine) < (best.EndLine-best.StartLine) {
				best = f
				found = true
			}
		}
	}
	return best, found
}
