package tool

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	workspaceFileScopeHint = "For workspace files use read_file/write_file/patch/search_files; " +
		"for source / call-chain analysis prefer rca_grep/rca_glob/rca_read when those tools are available; " +
		"for datasource/SQL use execute_read/execute_write/list_tables/describe_table."

	readFileDefaultLimit = 500
	readFileMaxLimit     = 2000
	readFileMaxChars     = 100_000
)

// RegisterWorkspaceFileTools registers Hermes-aligned workspace file tools (no danger confirm).
func RegisterWorkspaceFileTools(reg *Registry) error {
	return RegisterWorkspaceFileToolsWithConfig(reg, nil)
}

// RegisterWorkspaceFileToolsWithConfig registers workspace file tools; when cfg.PendingStore
// and TokenGen are set, danger-path write_file/patch require confirm_token.
func RegisterWorkspaceFileToolsWithConfig(reg *Registry, cfg *WorkspaceFileConfig) error {
	if reg == nil {
		return errors.New("file tools: registry is nil")
	}
	c := workspaceFileConfigOrDefault(cfg)
	if err := registerReadFileTool(reg); err != nil {
		return err
	}
	if err := registerWriteFileTool(reg, c); err != nil {
		return err
	}
	if err := registerPatchFileTool(reg, c); err != nil {
		return err
	}
	if err := registerSearchFilesTool(reg); err != nil {
		return err
	}
	return RegisterResultStatsTool(reg)
}

func workspaceFileConfigOrDefault(cfg *WorkspaceFileConfig) *WorkspaceFileConfig {
	c := &WorkspaceFileConfig{
		DangerPathPatterns: defaultWorkspaceDangerPathPatterns(),
		ConfirmTTLSeconds:  300,
	}
	if cfg != nil {
		if cfg.PendingStore != nil {
			c.PendingStore = cfg.PendingStore
		}
		if cfg.TokenGen != nil {
			c.TokenGen = cfg.TokenGen
		}
		if len(cfg.DangerPathPatterns) > 0 {
			c.DangerPathPatterns = cfg.DangerPathPatterns
		}
		if cfg.ConfirmTTLSeconds > 0 {
			c.ConfirmTTLSeconds = cfg.ConfirmTTLSeconds
		}
	}
	return c
}

func defaultWorkspaceDangerPathPatterns() []string {
	return []string{
		`(?i)(^|/)\.env($|\.|/)`,
		`(?i)\.(pem|key|p12|pfx)$`,
		`(?i)(^|/)id_rsa`,
		`(?i)(^|/)credentials`,
		`(?i)(^|/)secrets?/`,
		`(?i)(^|/)harness/hooks\.ya?ml$`,
	}
}

func pathMatchesDanger(rel string, patterns []string) (bool, string) {
	norm := filepath.ToSlash(strings.TrimSpace(rel))
	norm = strings.TrimPrefix(norm, "./")
	return commandDenied(norm, patterns)
}

func workspaceRootFromCtx(ctx context.Context) (string, error) {
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	if strings.TrimSpace(ws) == "" {
		return "", fmt.Errorf("workspace_root not set")
	}
	return ws, nil
}

func registerReadFileTool(reg *Registry) error {
	return reg.Register(Tool{
		Name: "read_file",
		Description: "Read a workspace text file with line numbers (LINE_NUM|CONTENT). " +
			workspaceFileScopeHint + " Use offset/limit for pagination; ~100K char limit per read.",
		Toolset: ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "1-based start line (default 1).",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Max lines to return (default %d, max %d).", readFileDefaultLimit, readFileMaxLimit),
				},
			},
			"required": []string{"path"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			ws, err := workspaceRootFromCtx(ctx)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			rel, _ := params["path"].(string)
			if strings.TrimSpace(rel) == "" {
				return map[string]any{"error": "path is required"}, nil
			}
			full, err := ResolveWorkspacePath(ws, rel)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			b, err := os.ReadFile(full)
			if err != nil {
				if os.IsNotExist(err) {
					return map[string]any{
						"error":    "file not found",
						"path":     rel,
						"similar":  suggestSimilarFiles(ws, rel),
					}, nil
				}
				return map[string]any{"error": err.Error()}, nil
			}
			offset := intFromParam(params["offset"], 1)
			if offset < 1 {
				offset = 1
			}
			limit := intFromParam(params["limit"], readFileDefaultLimit)
			if limit <= 0 {
				limit = readFileDefaultLimit
			}
			if limit > readFileMaxLimit {
				limit = readFileMaxLimit
			}
			lines := strings.Split(string(b), "\n")
			start := offset - 1
			if start >= len(lines) {
				return map[string]any{
					"path":        rel,
					"offset":      offset,
					"limit":       limit,
					"total_lines": len(lines),
					"content":     "",
				}, nil
			}
			end := start + limit
			if end > len(lines) {
				end = len(lines)
			}
			var out strings.Builder
			for i := start; i < end; i++ {
				fmt.Fprintf(&out, "%d|%s\n", i+1, lines[i])
			}
			content := out.String()
			if len(content) > readFileMaxChars {
				return map[string]any{
					"error": fmt.Sprintf("read exceeds %d characters; use offset and limit", readFileMaxChars),
				}, nil
			}
			return map[string]any{
				"path":        rel,
				"offset":      offset,
				"limit":       limit,
				"total_lines": len(lines),
				"content":     strings.TrimSuffix(content, "\n"),
			}, nil
		},
	})
}

func registerWriteFileTool(reg *Registry, c *WorkspaceFileConfig) error {
	return reg.Register(Tool{
		Name: "write_file",
		Description: "Write workspace file content (full overwrite). Creates parent directories. " +
			workspaceFileScopeHint + " Use patch for targeted edits. " +
			"Sensitive paths (e.g. .env, keys) require user confirm via confirm_token.",
		Toolset: ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full file content.",
				},
				"confirm_token": map[string]any{
					"type":        "string",
					"description": "Confirmation token from a previous danger-path proposal.",
				},
			},
			"required": []string{"path", "content"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
				return confirmWorkspaceFile(ctx, c, "write_file", strings.TrimSpace(token))
			}
			ws, err := workspaceRootFromCtx(ctx)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			rel, _ := params["path"].(string)
			content, _ := params["content"].(string)
			if strings.TrimSpace(rel) == "" {
				return map[string]any{"error": "path is required"}, nil
			}
			if danger, pattern := pathMatchesDanger(rel, c.DangerPathPatterns); danger {
				if c.PendingStore != nil && c.TokenGen != nil {
					return proposeWorkspaceFile(ctx, c, PendingWorkspaceFile{
						Action:  "write_file",
						Path:    strings.TrimSpace(rel),
						Content: content,
						Pattern: pattern,
					})
				}
				// No confirm store: legacy direct write (zero regression).
			}
			full, err := ResolveWorkspacePath(ws, rel)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			if err := writeWorkspaceFile(full, []byte(content)); err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			return map[string]any{"status": "ok", "path": rel, "bytes": len(content)}, nil
		},
	})
}

func registerPatchFileTool(reg *Registry, c *WorkspaceFileConfig) error {
	return reg.Register(Tool{
		Name: "patch",
		Description: "Apply targeted find-and-replace edits in a workspace file (replace mode). " +
			workspaceFileScopeHint + " Supports exact, trimmed, and whitespace-normalized fuzzy matching. " +
			"Sensitive paths require user confirm via confirm_token.",
		Toolset: ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative file path.",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "Text to find.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "Replacement text.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "Replace all matches (default false).",
				},
				"confirm_token": map[string]any{
					"type":        "string",
					"description": "Confirmation token from a previous danger-path proposal.",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			if token, _ := params["confirm_token"].(string); strings.TrimSpace(token) != "" {
				return confirmWorkspaceFile(ctx, c, "patch", strings.TrimSpace(token))
			}
			ws, err := workspaceRootFromCtx(ctx)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			rel, _ := params["path"].(string)
			oldS, _ := params["old_string"].(string)
			newS, _ := params["new_string"].(string)
			replaceAll, _ := params["replace_all"].(bool)
			if strings.TrimSpace(rel) == "" {
				return map[string]any{"error": "path is required"}, nil
			}
			if oldS == "" {
				return map[string]any{"error": "old_string is required"}, nil
			}
			if danger, pattern := pathMatchesDanger(rel, c.DangerPathPatterns); danger {
				if c.PendingStore != nil && c.TokenGen != nil {
					return proposeWorkspaceFile(ctx, c, PendingWorkspaceFile{
						Action:     "patch",
						Path:       strings.TrimSpace(rel),
						OldString:  oldS,
						NewString:  newS,
						ReplaceAll: replaceAll,
						Pattern:    pattern,
					})
				}
			}
			full, err := ResolveWorkspacePath(ws, rel)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			prev, err := os.ReadFile(full)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			out, strategy, count, err := applyFilePatch(string(prev), oldS, newS, replaceAll)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			if err := writeWorkspaceFile(full, []byte(out)); err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			return map[string]any{
				"status":   "ok",
				"path":     rel,
				"strategy": strategy,
				"matches":  count,
			}, nil
		},
	})
}

func proposeWorkspaceFile(ctx context.Context, c *WorkspaceFileConfig, pending PendingWorkspaceFile) (any, error) {
	if c == nil || c.PendingStore == nil || c.TokenGen == nil {
		return map[string]any{
			"error":   "confirm_required_but_unconfigured",
			"hint":    "path matched danger patterns but pending store is not configured",
			"path":    pending.Path,
			"pattern": pending.Pattern,
		}, nil
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return map[string]any{"error": "session_id is required for danger path confirm"}, nil
	}
	token, err := c.TokenGen.NewToken()
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("generate token: %v", err)}, nil
	}
	ttl := c.ConfirmTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	pending.Token = token
	pending.CreatedAt = time.Now()
	if err := c.PendingStore.SavePending(ctx, sessionID, pending); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	preview := pending.Path
	if pending.Action == "write_file" {
		preview = fmt.Sprintf("write %s (%d bytes)", pending.Path, len(pending.Content))
	} else {
		preview = fmt.Sprintf("patch %s", pending.Path)
	}
	return map[string]any{
		"status":     "pending",
		"token":      token,
		"action":     pending.Action,
		"path":       pending.Path,
		"pattern":    pending.Pattern,
		"preview":    preview,
		"expires_in": ttl,
		"hint":       "user must confirm; re-call with confirm_token to apply",
	}, nil
}

func confirmWorkspaceFile(ctx context.Context, c *WorkspaceFileConfig, expectedAction, token string) (any, error) {
	if c == nil || c.PendingStore == nil {
		return map[string]any{"error": "workspace file: confirm store not configured"}, nil
	}
	sessionID, _ := ctx.Value(ContextKeySessionID).(string)
	if sessionID == "" {
		return map[string]any{"error": "session_id is required"}, nil
	}
	pending, err := c.PendingStore.GetPending(ctx, sessionID, token)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if pending == nil {
		return ConfirmTokenError("not_found"), nil
	}
	if pending.Action != expectedAction {
		return map[string]any{
			"error":    "confirm_token action mismatch",
			"expected": expectedAction,
			"got":      pending.Action,
		}, nil
	}
	ttl := c.ConfirmTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	if time.Since(pending.CreatedAt) > time.Duration(ttl)*time.Second {
		_ = c.PendingStore.DeletePending(ctx, sessionID, token)
		return ConfirmTokenError("expired"), nil
	}
	ws, err := workspaceRootFromCtx(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	full, err := ResolveWorkspacePath(ws, pending.Path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var out any
	switch pending.Action {
	case "write_file":
		if err := writeWorkspaceFile(full, []byte(pending.Content)); err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		out = map[string]any{"status": "ok", "path": pending.Path, "bytes": len(pending.Content)}
	case "patch":
		prev, err := os.ReadFile(full)
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		patched, strategy, count, err := applyFilePatch(string(prev), pending.OldString, pending.NewString, pending.ReplaceAll)
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		if err := writeWorkspaceFile(full, []byte(patched)); err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		out = map[string]any{
			"status":   "ok",
			"path":     pending.Path,
			"strategy": strategy,
			"matches":  count,
		}
	default:
		return map[string]any{"error": "unknown pending action"}, nil
	}
	_ = c.PendingStore.DeletePending(ctx, sessionID, token)
	return out, nil
}

func registerSearchFilesTool(reg *Registry) error {
	return reg.Register(Tool{
		Name: "search_files",
		Description: "Search workspace file contents or find files by glob. " +
			workspaceFileScopeHint + " Prefer this over shell grep/find.",
		Toolset: ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regex pattern for content search, or glob fragment for file search.",
				},
				"target": map[string]any{
					"type":        "string",
					"enum":        []string{"content", "files"},
					"description": "content (default) searches inside files; files finds paths by glob.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative directory to search (default '.').",
				},
				"file_glob": map[string]any{
					"type":        "string",
					"description": "Optional glob filter (e.g. '*.go').",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max results (default 50).",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Skip first N results (default 0).",
				},
			},
			"required": []string{"pattern"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			ws, err := workspaceRootFromCtx(ctx)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			pattern, _ := params["pattern"].(string)
			if strings.TrimSpace(pattern) == "" {
				return map[string]any{"error": "pattern is required"}, nil
			}
			target, _ := params["target"].(string)
			if target == "" {
				target = "content"
			}
			searchPath, _ := params["path"].(string)
			if searchPath == "" {
				searchPath = "."
			}
			fileGlob, _ := params["file_glob"].(string)
			limit := intFromParam(params["limit"], 50)
			offset := intFromParam(params["offset"], 0)
			root, err := ResolveWorkspacePath(ws, searchPath)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			switch target {
			case "files":
				results, err := searchFilesByGlob(ws, root, pattern, fileGlob, limit, offset)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"target": "files", "matches": results}, nil
			case "content":
				results, err := searchFileContents(ws, root, pattern, fileGlob, limit, offset)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"target": "content", "matches": results}, nil
			default:
				return map[string]any{"error": "target must be content or files"}, nil
			}
		},
	})
}

func writeWorkspaceFile(full string, data []byte) error {
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

func intFromParam(v any, defaultVal int) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return defaultVal
	}
}

func suggestSimilarFiles(workspaceRoot, rel string) []string {
	base := filepath.Base(rel)
	dir := filepath.Dir(rel)
	searchDir, err := ResolveWorkspacePath(workspaceRoot, dir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.Contains(strings.ToLower(name), strings.ToLower(base)) {
			out = append(out, filepath.ToSlash(filepath.Join(dir, name)))
		}
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func applyFilePatch(content, oldS, newS string, replaceAll bool) (out, strategy string, count int, err error) {
	if replaceAll {
		if strings.Contains(content, oldS) {
			return strings.ReplaceAll(content, oldS, newS), "exact", strings.Count(content, oldS), nil
		}
		if out, n, ok := replaceAllTrimmedBlock(content, oldS, newS); ok {
			return out, "trim", n, nil
		}
		if out, n, ok := replaceAllNormalized(content, oldS, newS); ok {
			return out, "line-normalized", n, nil
		}
		return "", "", 0, fmt.Errorf("old_string not found")
	}

	c := strings.Count(content, oldS)
	if c == 1 {
		return strings.Replace(content, oldS, newS, 1), "exact", 1, nil
	}
	if c > 1 {
		return "", "", c, fmt.Errorf("old_string is ambiguous (%d exact matches); set replace_all or refine old_string", c)
	}
	if out, ok := replaceOnceTrimmedBlock(content, oldS, newS); ok {
		return out, "trim", 1, nil
	}
	if out, ok := replaceOnceNormalized(content, oldS, newS); ok {
		return out, "line-normalized", 1, nil
	}
	return "", "", 0, fmt.Errorf("old_string not found")
}

func replaceOnceTrimmedBlock(content, oldS, newS string) (string, bool) {
	oldLines := splitLines(oldS)
	if len(oldLines) == 0 {
		return "", false
	}
	contentLines := splitLines(content)
	for i := 0; i+len(oldLines) <= len(contentLines); i++ {
		if linesTrimEqual(contentLines[i:i+len(oldLines)], oldLines) {
			replaced := append(append([]string{}, contentLines[:i]...), splitLines(newS)...)
			replaced = append(replaced, contentLines[i+len(oldLines):]...)
			return strings.Join(replaced, "\n"), true
		}
	}
	return "", false
}

func replaceAllTrimmedBlock(content, oldS, newS string) (string, int, bool) {
	oldLines := splitLines(oldS)
	if len(oldLines) == 0 {
		return "", 0, false
	}
	contentLines := splitLines(content)
	var out []string
	matches := 0
	for i := 0; i < len(contentLines); {
		if i+len(oldLines) <= len(contentLines) && linesTrimEqual(contentLines[i:i+len(oldLines)], oldLines) {
			out = append(out, splitLines(newS)...)
			i += len(oldLines)
			matches++
			continue
		}
		out = append(out, contentLines[i])
		i++
	}
	if matches == 0 {
		return "", 0, false
	}
	return strings.Join(out, "\n"), matches, true
}

func replaceOnceNormalized(content, oldS, newS string) (string, bool) {
	idx, length, ok := findNormalizedMatch(content, oldS, false)
	if !ok {
		return "", false
	}
	return content[:idx] + newS + content[idx+length:], true
}

func replaceAllNormalized(content, oldS, newS string) (string, int, bool) {
	matches := 0
	for {
		idx, length, ok := findNormalizedMatch(content, oldS, true)
		if !ok {
			break
		}
		content = content[:idx] + newS + content[idx+length:]
		matches++
	}
	if matches == 0 {
		return "", 0, false
	}
	return content, matches, true
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func linesTrimEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func findNormalizedMatch(content, oldS string, allowMultiple bool) (start, length int, ok bool) {
	nOld := normalizeWhitespace(oldS)
	if nOld == "" {
		return 0, 0, false
	}
	var hits [][2]int
	lines := splitLines(content)
	for i := 0; i < len(lines); i++ {
		for j := i; j < len(lines); j++ {
			block := strings.Join(lines[i:j+1], "\n")
			if normalizeWhitespace(block) == nOld {
				hits = append(hits, [2]int{i, j})
			}
		}
	}
	if len(hits) == 0 {
		return 0, 0, false
	}
	if !allowMultiple && len(hits) != 1 {
		return 0, 0, false
	}
	h := hits[0]
	startLine, endLine := h[0], h[1]
	prefix := strings.Join(lines[:startLine], "\n")
	if startLine > 0 {
		prefix += "\n"
	}
	start = len(prefix)
	block := strings.Join(lines[startLine:endLine+1], "\n")
	return start, len(block), true
}

type contentMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

func searchFileContents(workspaceRoot, root, pattern, fileGlob string, limit, offset int) ([]contentMatch, error) {
	if results, err := searchWithRipgrep(workspaceRoot, root, pattern, fileGlob, limit, offset); err == nil {
		return results, nil
	}
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
			return nil
		}
		rel, err := filepath.Rel(workspaceRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
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
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return slicePage(all, limit, offset), nil
}

func searchWithRipgrep(workspaceRoot, root, pattern, fileGlob string, limit, offset int) ([]contentMatch, error) {
	if _, err := exec.LookPath("rg"); err != nil {
		return nil, err
	}
	args := []string{"--no-heading", "--line-number", "--color=never"}
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
		rel, err := filepath.Rel(workspaceRoot, absPath)
		if err != nil {
			rel = absPath
		}
		all = append(all, contentMatch{
			Path:    filepath.ToSlash(rel),
			Line:    lineNo,
			Content: content,
		})
	}
	return slicePage(all, limit, offset), nil
}

// parseRipgrepMatchLine parses "path:line:content", including Windows paths with drive letters (C:\...).
func parseRipgrepMatchLine(line string) (path string, lineNo int, content string, ok bool) {
	// Scan for ":digits:" from the left, requiring the digits segment to be purely numeric.
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		j := i + 1
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j == i+1 || j >= len(line) || line[j] != ':' {
			continue
		}
		n := 0
		for _, r := range line[i+1 : j] {
			n = n*10 + int(r-'0')
		}
		if n <= 0 {
			continue
		}
		return line[:i], n, line[j+1:], true
	}
	return "", 0, "", false
}

type fileMatch struct {
	Path    string `json:"path"`
	ModTime string `json:"mod_time"`
}

func searchFilesByGlob(workspaceRoot, root, pattern, fileGlob string, limit, offset int) ([]fileMatch, error) {
	glob := pattern
	if fileGlob != "" {
		glob = fileGlob
	}
	var all []fileMatch
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(workspaceRoot, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		name := d.Name()
		ok, err := matchFileGlob(glob, relSlash, name)
		if err != nil {
			return err
		}
		// Substring fallback only for simple basename-ish patterns (not path/** globs).
		if !ok && fileGlob == "" && !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
			ok = strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
		}
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		all = append(all, fileMatch{
			Path:    relSlash,
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime > all[j].ModTime
	})
	return slicePage(all, limit, offset), nil
}

// matchFileGlob matches a glob against the file basename and/or slash-normalized
// relative path. Supports ** across path segments (e.g. **/go.mod, pkg/**/*.go).
func matchFileGlob(pattern, relSlash, baseName string) (bool, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return false, nil
	}
	relSlash = filepath.ToSlash(relSlash)

	if ok, err := filepath.Match(pattern, baseName); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	if strings.Contains(pattern, "**") {
		return matchDoubleStarGlob(pattern, relSlash)
	}
	if strings.Contains(pattern, "/") {
		ok, err := filepath.Match(pattern, relSlash)
		return ok, err
	}
	return false, nil
}

func matchDoubleStarGlob(pattern, path string) (bool, error) {
	re, err := doubleStarGlobRegexp(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(path), nil
}

func doubleStarGlobRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); {
		if strings.HasPrefix(pattern[i:], "**/") {
			b.WriteString("(?:.*/)?")
			i += 3
			continue
		}
		if strings.HasPrefix(pattern[i:], "**") {
			b.WriteString(".*")
			i += 2
			continue
		}
		switch c := pattern[i]; c {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
		i++
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}

func slicePage[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	items = items[offset:]
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
