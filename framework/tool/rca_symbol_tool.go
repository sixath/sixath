package tool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sixath/framework/tool/lsp"
)

const rcaSymbolMaxResultsDefault = 50

// RCASymbolOpts configures the language server used by rca_symbol.
type RCASymbolOpts struct {
	GoplsPath      string
	ReadyTimeout   time.Duration
	RequestTimeout time.Duration
	Factory        lsp.ServerFactory
}

// RegisterRCASymbolTool registers source-symbol navigation through gopls.
// roots may be empty: the tool remains registered, while calls fail permanently.
func RegisterRCASymbolTool(reg *Registry, roots []string, opts RCASymbolOpts) error {
	if reg == nil {
		return errors.New("rca symbol tool: registry is nil")
	}

	command := strings.TrimSpace(opts.GoplsPath)
	if command == "" {
		command = "gopls"
	}
	serverOpts := lsp.ServerOpts{
		Command:        command,
		InitTimeout:    opts.ReadyTimeout,
		RequestTimeout: opts.RequestTimeout,
	}
	factory := opts.Factory
	if factory == nil {
		factory = lsp.GoplsFactory(serverOpts)
	}
	pool := lsp.NewPool(factory, serverOpts)

	return reg.Register(Tool{
		Name:               "rca_symbol",
		Description: "Navigate Go source symbols (definition/references) via gopls across configured code roots. " +
			"For call-chain analysis: after locating a function, call action=references to list inbound callers (file:line). " +
			"Do not treat the first handler hit as the only source. Empty callers (inbound_empty) means no in-root callers — then you may conclude. " +
			"If gopls fails, falls back to a name grep and sets symbol_ok=false. " +
			"Prefer file+line over symbol-only in large multi-module repos. character is optional (0-based; 0 snaps to the identifier on that line). " +
			"LSP attaches to the nearest go.mod under the repo root. max_results defaults to 50.",
		Toolset:            ToolsetRCA,
		RequiresSequential: false,
		CheckFn: func(context.Context) error {
			if filepath.IsAbs(command) {
				info, err := os.Stat(command)
				if err != nil {
					return err
				}
				if info.IsDir() {
					return fmt.Errorf("gopls path %q is a directory", command)
				}
				return nil
			}
			_, err := exec.LookPath(command)
			return err
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":              map[string]any{"type": "string", "enum": []string{"definition", "references"}, "description": "Navigation action."},
				"repo":                map[string]any{"type": "string", "description": "Repository name (basename of a configured root)."},
				"file":                map[string]any{"type": "string", "description": "Repo-relative source file path."},
				"line":                map[string]any{"type": "integer", "description": "1-based source line."},
				"symbol":              map[string]any{"type": "string", "description": "Go symbol name, optionally qualified as pkg.Name."},
				"character":           map[string]any{"type": "integer", "description": "Optional 0-based UTF-16 character; defaults to 0."},
				"include_declaration": map[string]any{"type": "boolean", "description": "For references, include the declaration; defaults to true."},
				"max_results":         map[string]any{"type": "integer", "description": "Maximum locations to return; defaults to 50."},
			},
			"required": []string{"action", "repo"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return executeRCASymbol(ctx, roots, pool, params), nil
		},
	})
}

func executeRCASymbol(ctx context.Context, roots []string, pool *lsp.Pool, params map[string]any) map[string]any {
	const toolName = "rca_symbol"

	action, _ := params["action"].(string)
	if action != "definition" && action != "references" {
		return rcaErr(toolName, "action must be definition or references", ErrorPermanent)
	}
	repo, _ := params["repo"].(string)
	if strings.TrimSpace(repo) == "" {
		return rcaErr(toolName, "repo is required", ErrorPermanent)
	}
	file, _ := params["file"].(string)
	line := intFromParam(params["line"], 0)
	character := intFromParam(params["character"], 0)
	maxResults := intFromParam(params["max_results"], rcaSymbolMaxResultsDefault)
	if maxResults <= 0 {
		maxResults = rcaSymbolMaxResultsDefault
	}

	preferName, _ := params["symbol"].(string)
	hasLocation := strings.TrimSpace(file) != "" && line >= 1
	if !hasLocation {
		candidates, unique, candidatesTruncated, err := resolveSymbolCandidates(roots, repo, preferName, maxResults)
		if err != nil {
			return rcaErr(toolName, err.Error(), ErrorPermanent)
		}
		if len(candidates) == 0 {
			if strings.TrimSpace(preferName) == "" {
				if strings.TrimSpace(file) != "" {
					return rcaErr(toolName, "line must be a 1-based positive integer", ErrorPermanent)
				}
				return rcaErr(toolName, "file and line or symbol is required", ErrorPermanent)
			}
			return rcaErr(toolName, "no symbol candidates found", ErrorPermanent)
		}
		if !unique {
			return map[string]any{
				"ok": true, "action": action, "repo": repo, "needs_disambiguation": true,
				"candidates": candidates, "truncated": candidatesTruncated,
			}
		}
		file, line, character = candidates[0].File, candidates[0].Line, candidates[0].Character
		if preferName == "" {
			preferName = candidates[0].Name
		}
	}
	if character < 0 {
		return rcaErr(toolName, "character must be non-negative", ErrorPermanent)
	}

	full, repoRoot, err := resolveInRepos(roots, repo, file)
	if err != nil {
		return rcaErr(toolName, err.Error(), ErrorPermanent)
	}
	// gopls needs a real Go module / go.work root. Multi-module workspaces (e.g.
	// cloudgame) nest many go.mod trees under one RCA root — pin LSP to the nearest.
	moduleRoot := findNearestGoModuleRoot(repoRoot, full)
	relFile, err := filepath.Rel(moduleRoot, full)
	if err != nil {
		return rcaErrFrom(toolName, err)
	}
	character = snapRCASymbolCharacter(full, line, character, preferName)

	server, err := pool.Get(ctx, moduleRoot)
	if err != nil {
		if shouldMarkDeadRCASymbolServer(err) {
			pool.MarkDead(moduleRoot)
		}
		return rcaErrFrom(toolName, err)
	}
	pos := lsp.Position{Line: line - 1, Character: character}
	var locations []lsp.Location
	if action == "definition" {
		locations, err = server.Definition(ctx, moduleRoot, filepath.ToSlash(relFile), pos)
	} else {
		includeDeclaration := true
		if value, ok := params["include_declaration"].(bool); ok {
			includeDeclaration = value
		}
		locations, err = server.References(ctx, moduleRoot, filepath.ToSlash(relFile), pos, includeDeclaration)
	}
	if err != nil {
		if lsp.IsPermanentCapabilityError(err) {
			if action == "references" {
				return rcaSymbolGrepFallback(toolName, roots, repo, repoRoot, full, file, line, preferName, maxResults, err)
			}
			return rcaErr(toolName, err.Error(), ErrorPermanent)
		}
		if shouldMarkDeadRCASymbolServer(err) {
			pool.MarkDead(moduleRoot)
		}
		if action == "references" {
			return rcaSymbolGrepFallback(toolName, roots, repo, repoRoot, full, file, line, preferName, maxResults, err)
		}
		return rcaErrFrom(toolName, err)
	}

	locations = remapLocationsToRepoRoot(repoRoot, moduleRoot, locations)
	locations = filterRCASymbolLocations(roots, repo, locations)
	if action == "references" {
		return rcaSymbolReferencesOK(toolName, roots, repo, repoRoot, full, file, line, preferName, locations, maxResults, true, "")
	}
	if len(locations) == 0 {
		return rcaErr(toolName, "no symbol locations found", ErrorPermanent)
	}
	truncated := len(locations) > maxResults
	if truncated {
		locations = locations[:maxResults]
	}

	return rcaOK(toolName, map[string]any{
		"action": action, "repo": repo, "locations": locations, "truncated": truncated,
	})
}

func shouldMarkDeadRCASymbolServer(err error) bool {
	if err == nil || lsp.IsPermanentCapabilityError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "lsp: read ") {
		return false
	}
	if classifyRCAError(err) == ErrorTransient {
		return true
	}
	deadServerHints := []string{
		"server is closed",
		"not initialized",
		"json-rpc error",
		"decode json-rpc",
	}
	for _, hint := range deadServerHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

func filterRCASymbolLocations(roots []string, repo string, locations []lsp.Location) []lsp.Location {
	filtered := make([]lsp.Location, 0, len(locations))
	for _, location := range locations {
		full, root, err := resolveInRepos(roots, repo, location.File)
		if err != nil {
			slog.Warn("rca_symbol discarded out-of-root location", "repo", repo, "path", location.File, "err", err)
			continue
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			slog.Warn("rca_symbol discarded unresolvable location", "repo", repo, "path", location.File, "err", err)
			continue
		}
		location.Repo = repo
		location.File = filepath.ToSlash(rel)
		filtered = append(filtered, location)
	}
	return filtered
}

func rcaSymbolReferencesOK(toolName string, roots []string, repo, repoRoot, full, file string, line int, symbol string, locations []lsp.Location, maxResults int, symbolOK bool, fallback string) map[string]any {
	locations = appendCrossRepoGrepCallers(roots, repo, full, line, symbol, locations, maxResults)
	truncated := len(locations) > maxResults
	if truncated {
		locations = locations[:maxResults]
	}
	callers := callersFromLocations(repo, locations, file, line)
	entry := map[string]any{"repo": repo, "file": file, "line": line}
	if strings.TrimSpace(symbol) != "" {
		entry["symbol"] = symbol
	}
	scanned := reposScanned(repo, callers)
	payload := map[string]any{
		"action":        "references",
		"repo":          repo,
		"symbol_ok":     symbolOK,
		"entry":         entry,
		"callers":       callers,
		"locations":     locations,
		"truncated":     truncated,
		"inbound_empty": len(callers) == 0,
		"repos_scanned": scanned,
		"cross_repo":    len(roots) > 1,
		"workset": map[string]any{
			"entry":   entry,
			"callers": callers,
			"callees": []map[string]any{},
		},
	}
	if fallback != "" {
		payload["fallback"] = fallback
	}
	return rcaOK(toolName, payload)
}

func callersFromLocations(repo string, locations []lsp.Location, originFile string, originLine int) []map[string]any {
	originFile = filepath.ToSlash(originFile)
	out := make([]map[string]any, 0, len(locations))
	for _, loc := range locations {
		file := loc.File
		if file == "" {
			continue
		}
		locRepo := loc.Repo
		if locRepo == "" {
			locRepo = repo
		}
		if locRepo == repo && file == originFile && loc.Line == originLine {
			continue
		}
		item := map[string]any{
			"repo": loc.Repo,
			"file": file,
			"line": loc.Line,
			"path": fmt.Sprintf("%s:%d", file, loc.Line),
		}
		if item["repo"] == "" {
			item["repo"] = repo
		}
		if loc.Name != "" {
			item["name"] = loc.Name
		}
		out = append(out, item)
	}
	return out
}

func rcaSymbolGrepFallback(toolName string, roots []string, repo, repoRoot, full, file string, line int, preferName string, maxResults int, lspErr error) map[string]any {
	name := strings.TrimSpace(preferName)
	if name == "" {
		name = goIdentifierOnLine(full, line)
	}
	if name == "" {
		return rcaErrFrom(toolName, lspErr)
	}
	locations, err := grepSymbolCallers(roots, repo, repoRoot, name, maxResults+1)
	if err != nil {
		return rcaErrFrom(toolName, lspErr)
	}
	return rcaSymbolReferencesOK(toolName, roots, repo, repoRoot, full, file, line, name, locations, maxResults, false, "grep")
}

func grepSymbolCallers(roots []string, repo, repoRoot, name string, limit int) ([]lsp.Location, error) {
	pattern := `\b` + regexp.QuoteMeta(name) + `\b`
	matches, err := searchRCAFileContents(repoRoot, pattern, "*.go", limit)
	if err != nil {
		return nil, err
	}
	out := make([]lsp.Location, 0, len(matches))
	for _, m := range matches {
		out = append(out, lsp.Location{
			Repo: repo,
			File: m.Path,
			Line: m.Line,
			Name: name,
		})
	}
	return filterRCASymbolLocations(roots, repo, out), nil
}

func appendCrossRepoGrepCallers(roots []string, repo, full string, line int, symbol string, locations []lsp.Location, maxResults int) []lsp.Location {
	if len(roots) <= 1 {
		return locations
	}
	name := strings.TrimSpace(symbol)
	if name == "" {
		name = goIdentifierOnLine(full, line)
	}
	if name == "" {
		return locations
	}
	remain := maxResults
	if remain <= 0 {
		remain = rcaSymbolMaxResultsDefault
	}
	remain -= len(locations)
	if remain < 8 {
		remain = 8
	}
	extra, err := grepSymbolCallersOtherRoots(roots, repo, name, remain)
	if err != nil || len(extra) == 0 {
		return locations
	}
	return dedupeSymbolLocations(append(locations, extra...))
}

func grepSymbolCallersOtherRoots(roots []string, skipRepo, name string, limit int) ([]lsp.Location, error) {
	var out []lsp.Location
	remaining := limit
	for _, root := range roots {
		repo := repoNameFromRoot(root)
		if repo == skipRepo {
			continue
		}
		if remaining <= 0 {
			break
		}
		locs, err := grepSymbolCallers(roots, repo, root, name, remaining)
		if err != nil {
			continue
		}
		out = append(out, locs...)
		remaining = limit - len(out)
	}
	return out, nil
}

func dedupeSymbolLocations(locations []lsp.Location) []lsp.Location {
	seen := map[string]struct{}{}
	out := make([]lsp.Location, 0, len(locations))
	for _, loc := range locations {
		key := loc.Repo + "|" + filepath.ToSlash(loc.File) + "|" + fmt.Sprint(loc.Line)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, loc)
	}
	return out
}

func reposScanned(origin string, callers []map[string]any) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 1+len(callers))
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" {
			return
		}
		if _, ok := seen[r]; ok {
			return
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	add(origin)
	for _, c := range callers {
		add(anyStringTool(c["repo"]))
	}
	return out
}

func anyStringTool(v any) string {
	s, _ := v.(string)
	return s
}
