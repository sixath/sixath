package tool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
		Description: "Navigate Go source symbols (definition/references) via gopls across configured RCA repositories. " +
			"For module/call-chain analysis: rca_glob/rca_grep to locate entries, then rca_symbol with file+line (preferred) or a unique symbol. " +
			"Prefer file+line over symbol-only in large multi-module repos. character is optional (0-based; 0 snaps to the identifier on that line). " +
			"LSP attaches to the nearest go.mod under the repo root. max_results defaults to 50.",
		Toolset:            ToolsetRCA,
		RequiresSequential: true,
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
			return rcaErr(toolName, err.Error(), ErrorPermanent)
		}
		if shouldMarkDeadRCASymbolServer(err) {
			pool.MarkDead(moduleRoot)
		}
		return rcaErrFrom(toolName, err)
	}

	locations = remapLocationsToRepoRoot(repoRoot, moduleRoot, locations)
	locations = filterRCASymbolLocations(roots, repo, locations)
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
