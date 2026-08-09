package tool

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/framework/tool/lsp"
)

type fakeRCASymbolServer struct {
	definitionFn func(context.Context, string, string, lsp.Position) ([]lsp.Location, error)
	referencesFn func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error)
}

func (f *fakeRCASymbolServer) EnsureReady(context.Context, string) error { return nil }

func (f *fakeRCASymbolServer) Definition(ctx context.Context, root, file string, pos lsp.Position) ([]lsp.Location, error) {
	return f.definitionFn(ctx, root, file, pos)
}

func (f *fakeRCASymbolServer) References(ctx context.Context, root, file string, pos lsp.Position, includeDeclaration bool) ([]lsp.Location, error) {
	return f.referencesFn(ctx, root, file, pos, includeDeclaration)
}

func (f *fakeRCASymbolServer) Close(context.Context) error { return nil }

func newRCASymbolRegistry(t *testing.T, roots []string, server *fakeRCASymbolServer) *Registry {
	t.Helper()
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterRCASymbolTool(reg, roots, RCASymbolOpts{
		Factory: func(context.Context, string, lsp.ServerOpts) (lsp.LanguageServer, error) {
			return server, nil
		},
		ReadyTimeout:   time.Second,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RegisterRCASymbolTool: %v", err)
	}
	return reg
}

func TestRCASymbol_DefinitionByLine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	var gotRoot, gotFile string
	var gotPos lsp.Position
	server := &fakeRCASymbolServer{
		definitionFn: func(_ context.Context, root, file string, pos lsp.Position) ([]lsp.Location, error) {
			gotRoot, gotFile, gotPos = root, file, pos
			return []lsp.Location{{File: filepath.Join(root, "pkg", "target.go"), Line: 8, Character: 3, Name: "Target"}}, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
			t.Fatal("References should not be called")
			return nil, nil
		},
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, ok := reg.Get("rca_symbol")
	if !ok {
		t.Fatal("rca_symbol not registered")
	}
	if tl.Toolset != ToolsetRCA || !tl.RequiresSequential {
		t.Fatalf("tool metadata = %+v", tl)
	}

	out, err := tl.Execute(context.Background(), map[string]any{
		"action": "definition", "repo": "service-a", "file": "pkg/source.go", "line": 6, "character": 2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotRoot != root || gotFile != "pkg/source.go" || gotPos != (lsp.Position{Line: 5, Character: 2}) {
		t.Fatalf("LSP request = root=%q file=%q pos=%+v", gotRoot, gotFile, gotPos)
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "rca_symbol")
	locations := m["locations"].([]lsp.Location)
	if len(locations) != 1 || locations[0].Repo != "service-a" || locations[0].File != "pkg/target.go" {
		t.Fatalf("locations = %#v", locations)
	}
	refs := m["evidence_refs"].([]EvidenceRef)
	if len(refs) != 1 || refs[0] != (EvidenceRef{Kind: "rca_symbol", Repo: "service-a", Path: "pkg/target.go", Line: 8, Summary: "definition Target"}) {
		t.Fatalf("evidence_refs = %#v", refs)
	}
}

func TestRCASymbol_SymbolDisambiguation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	writeRCASymbolSource(t, root, "a.go", "package service\n\nfunc Foo() {}\n")
	writeRCASymbolSource(t, root, "b.go", "package service\n\nfunc Foo() {}\n")
	server := &fakeRCASymbolServer{
		definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) {
			t.Fatal("Definition should not be called for ambiguous symbols")
			return nil, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
			t.Fatal("References should not be called for ambiguous symbols")
			return nil, nil
		},
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")

	out, err := tl.Execute(context.Background(), map[string]any{
		"action": "definition", "repo": "service-a", "symbol": "Foo",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := out.(map[string]any)
	if m["ok"] != true || m["needs_disambiguation"] != true {
		t.Fatalf("disambiguation response = %#v", m)
	}
	if got := len(m["candidates"].([]lsp.Location)); got < 2 {
		t.Fatalf("candidates = %#v, want at least 2", m["candidates"])
	}
	if _, ok := m["locations"]; ok {
		t.Fatalf("disambiguation response must not include locations: %#v", m)
	}
	if _, ok := m["evidence_refs"]; ok {
		t.Fatalf("disambiguation response must not include evidence_refs: %#v", m)
	}
}

func TestRCASymbol_SymbolUniqueThenDefinition(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	writeRCASymbolSource(t, root, "pkg/source.go", "package pkg\n\nfunc Unique() {}\n")
	var calls int
	server := &fakeRCASymbolServer{
		definitionFn: func(_ context.Context, gotRoot, file string, pos lsp.Position) ([]lsp.Location, error) {
			calls++
			if gotRoot != root || file != "pkg/source.go" || pos != (lsp.Position{Line: 2, Character: 5}) {
				t.Fatalf("LSP request = root=%q file=%q pos=%+v", gotRoot, file, pos)
			}
			return []lsp.Location{{File: filepath.Join(root, "pkg", "source.go"), Line: 3, Name: "Unique"}}, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
			t.Fatal("References should not be called")
			return nil, nil
		},
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")

	out, err := tl.Execute(context.Background(), map[string]any{
		"action": "definition", "repo": "service-a", "symbol": "Unique",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Definition calls = %d, want 1", calls)
	}
	assertRCAEvidenceOK(t, out.(map[string]any), "rca_symbol")
}

func TestRCASymbol_NonDeclarationIdentifierIsTier2(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "source.go")
	if err := os.WriteFile(full, []byte("package sample\n\nvar _ = Foo\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var candidates []symbolCandidate
	err := collectRCASymbolCandidates(
		full, "source.go", "service-a", "Foo", "",
		regexp.MustCompile(`\bFoo\b`),
		regexp.MustCompile(`^\s*(?:func\s+(?:\([^)]*\)\s+)?|type\s+)Foo\b`),
		nil, &candidates,
	)
	if err != nil {
		t.Fatalf("collectRCASymbolCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].tier != 2 {
		t.Fatalf("tier = %d, want 2", candidates[0].tier)
	}
}

func writeRCASymbolSource(t *testing.T, root, rel, source string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", full, err)
	}
}

func TestRCASymbol_EmptyRootsRuntime(t *testing.T) {
	server := &fakeRCASymbolServer{}
	reg := newRCASymbolRegistry(t, nil, server)
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{"action": "definition", "repo": "service-a", "file": "a.go", "line": 1})
	assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
}

func TestRCASymbol_UnknownRepo(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	reg := newRCASymbolRegistry(t, []string{root}, &fakeRCASymbolServer{})
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{"action": "definition", "repo": "other", "file": "a.go", "line": 1})
	assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
}

func TestRCASymbol_FiltersOutOfRootLocations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	server := &fakeRCASymbolServer{
		definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) {
			return []lsp.Location{
				{File: filepath.Join(root, "ok.go"), Line: 3},
				{File: filepath.Join(filepath.Dir(root), "outside.go"), Line: 9},
			}, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) { return nil, nil },
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{"action": "definition", "repo": "service-a", "file": "a.go", "line": 1})
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "rca_symbol")
	locations := m["locations"].([]lsp.Location)
	if len(locations) != 1 || locations[0].File != "ok.go" {
		t.Fatalf("locations = %#v", locations)
	}
}

func TestRCASymbol_LogsDiscardedOutOfRootLocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	server := &fakeRCASymbolServer{
		definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) {
			return []lsp.Location{{File: filepath.Join(filepath.Dir(root), "outside.go"), Line: 9}}, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) { return nil, nil },
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{"action": "definition", "repo": "service-a", "file": "a.go", "line": 1})
	assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
	if got := logs.String(); !strings.Contains(got, "repo=service-a") || !strings.Contains(got, "path=") || !strings.Contains(got, "err=") {
		t.Fatalf("discard log lacks context: %q", got)
	}
}

func TestRCASymbol_CheckFnMissingGopls(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterRCASymbolTool(reg, []string{t.TempDir()}, RCASymbolOpts{GoplsPath: filepath.Join(t.TempDir(), "missing-gopls")}); err != nil {
		t.Fatalf("RegisterRCASymbolTool: %v", err)
	}
	for _, tool := range reg.ListForAPI(context.Background(), nil) {
		if tool.Name == "rca_symbol" {
			t.Fatal("rca_symbol should be excluded when gopls is unavailable")
		}
	}
}

func TestRCASymbol_ReferencesByLine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	var gotIncludeDeclaration bool
	server := &fakeRCASymbolServer{
		definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) { return nil, nil },
		referencesFn: func(_ context.Context, _ string, _ string, pos lsp.Position, includeDeclaration bool) ([]lsp.Location, error) {
			gotIncludeDeclaration = includeDeclaration
			if pos != (lsp.Position{Line: 4, Character: 0}) {
				t.Fatalf("position = %+v", pos)
			}
			return []lsp.Location{{File: filepath.Join(root, "ref.go"), Line: 11, Name: "Target"}}, nil
		},
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{
		"action": "references", "repo": "service-a", "file": "a.go", "line": 5, "include_declaration": true,
	})
	if !gotIncludeDeclaration {
		t.Fatal("include_declaration was not forwarded")
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "rca_symbol")
	if m["truncated"].(bool) {
		t.Fatal("unexpected truncation")
	}
}

func TestRCASymbol_MaxResultsTruncatesLocations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	server := &fakeRCASymbolServer{
		definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) {
			return []lsp.Location{
				{File: filepath.Join(root, "a.go"), Line: 1},
				{File: filepath.Join(root, "b.go"), Line: 2},
				{File: filepath.Join(root, "c.go"), Line: 3},
			}, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) { return nil, nil },
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{
		"action": "definition", "repo": "service-a", "file": "source.go", "line": 1, "max_results": 2,
	})
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "rca_symbol")
	if !m["truncated"].(bool) {
		t.Fatal("expected truncated=true")
	}
	if got := len(m["locations"].([]lsp.Location)); got != 2 {
		t.Fatalf("locations = %d, want 2", got)
	}
}

func TestRCASymbol_ParamValidation(t *testing.T) {
	reg := newRCASymbolRegistry(t, []string{filepath.Join(t.TempDir(), "service-a")}, &fakeRCASymbolServer{})
	tl, _ := reg.Get("rca_symbol")
	for _, params := range []map[string]any{
		{"action": "invalid", "repo": "service-a", "file": "a.go", "line": 1},
		{"action": "definition", "file": "a.go", "line": 1},
		{"action": "definition", "repo": "service-a", "line": 1},
		{"action": "definition", "repo": "service-a", "file": "a.go"},
		{"action": "definition", "repo": "service-a", "file": "a.go", "line": 0},
		{"action": "definition", "repo": "service-a", "file": "a.go", "line": 1, "character": -1},
	} {
		out, _ := tl.Execute(context.Background(), params)
		assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
	}
}

func TestRCASymbol_MarkDeadOnTransientDefinitionError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	writeRCASymbolSource(t, root, "a.go", "package pkg\n\nfunc Foo() {}\n")

	var factoryCalls atomic.Int32
	var definitionCalls atomic.Int32

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterRCASymbolTool(reg, []string{root}, RCASymbolOpts{
		Factory: func(_ context.Context, _ string, _ lsp.ServerOpts) (lsp.LanguageServer, error) {
			factoryCalls.Add(1)
			return &fakeRCASymbolServer{
				definitionFn: func(_ context.Context, gotRoot, file string, pos lsp.Position) ([]lsp.Location, error) {
					// character 0 snaps to the Foo identifier on "func Foo() {}".
					if gotRoot != root || file != "a.go" || pos != (lsp.Position{Line: 2, Character: 5}) {
						t.Fatalf("LSP request = root=%q file=%q pos=%+v", gotRoot, file, pos)
					}
					if definitionCalls.Add(1) == 1 {
						return nil, errors.New("connection reset by peer")
					}
					return []lsp.Location{{File: filepath.Join(root, "a.go"), Line: 3, Name: "Foo"}}, nil
				},
				referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
					t.Fatal("References should not be called")
					return nil, nil
				},
			}, nil
		},
		ReadyTimeout:   time.Second,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RegisterRCASymbolTool: %v", err)
	}
	tl, _ := reg.Get("rca_symbol")

	out1, err := tl.Execute(context.Background(), map[string]any{
		"action": "definition", "repo": "service-a", "file": "a.go", "line": 3,
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	assertRCAEvidenceError(t, out1.(map[string]any), ErrorTransient)
	if factoryCalls.Load() != 1 {
		t.Fatalf("factory calls after first = %d, want 1", factoryCalls.Load())
	}

	out2, err := tl.Execute(context.Background(), map[string]any{
		"action": "definition", "repo": "service-a", "file": "a.go", "line": 3,
	})
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	assertRCAEvidenceOK(t, out2.(map[string]any), "rca_symbol")
	if factoryCalls.Load() != 2 {
		t.Fatalf("factory calls after second = %d, want 2", factoryCalls.Load())
	}
}

func TestRCASymbol_NoMarkDeadOnPermanentCapabilityError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	writeRCASymbolSource(t, root, "a.go", "package pkg\n\nfunc Foo() {}\n")

	var factoryCalls atomic.Int32
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterRCASymbolTool(reg, []string{root}, RCASymbolOpts{
		Factory: func(_ context.Context, _ string, _ lsp.ServerOpts) (lsp.LanguageServer, error) {
			factoryCalls.Add(1)
			return &fakeRCASymbolServer{
				definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) {
					return nil, fmt.Errorf("%w: definitionProvider", lsp.ErrPermanentCapabilityMissing)
				},
				referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
					return nil, nil
				},
			}, nil
		},
		ReadyTimeout:   time.Second,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RegisterRCASymbolTool: %v", err)
	}
	tl, _ := reg.Get("rca_symbol")

	for i := 0; i < 2; i++ {
		out, err := tl.Execute(context.Background(), map[string]any{
			"action": "definition", "repo": "service-a", "file": "a.go", "line": 3,
		})
		if err != nil {
			t.Fatalf("Execute[%d]: %v", i, err)
		}
		assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("factory calls = %d, want 1 (server should stay pooled)", factoryCalls.Load())
	}
}

func TestRCASymbol_EmptyLocationsPermanent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-a")
	server := &fakeRCASymbolServer{
		definitionFn: func(context.Context, string, string, lsp.Position) ([]lsp.Location, error) { return nil, nil },
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
			return nil, errors.New("should not call")
		},
	}
	reg := newRCASymbolRegistry(t, []string{root}, server)
	tl, _ := reg.Get("rca_symbol")
	out, _ := tl.Execute(context.Background(), map[string]any{"action": "definition", "repo": "service-a", "file": "a.go", "line": 1})
	assertRCAEvidenceError(t, out.(map[string]any), ErrorPermanent)
}

func TestRCASymbol_UsesNearestGoModuleRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "cloudgame")
	modRoot := filepath.Join(repo, "apps", "cgvmagent")
	writeRCASymbolSource(t, modRoot, "go.mod", "module cgvmagent\n\ngo 1.22\n")
	writeRCASymbolSource(t, modRoot, "cmd/main.go", "package main\n\nfunc main() {}\n")
	writeRCASymbolSource(t, modRoot, "internal/biz/agent.go", "package biz\n\nfunc NewAgent() {}\n")

	var gotRoot, gotFile string
	server := &fakeRCASymbolServer{
		definitionFn: func(_ context.Context, root, file string, pos lsp.Position) ([]lsp.Location, error) {
			gotRoot, gotFile = root, file
			if pos.Character == 0 {
				t.Fatalf("expected character snap away from 0, got %+v", pos)
			}
			return []lsp.Location{{File: "internal/biz/agent.go", Line: 3, Character: 5, Name: "NewAgent"}}, nil
		},
		referencesFn: func(context.Context, string, string, lsp.Position, bool) ([]lsp.Location, error) {
			t.Fatal("References should not be called")
			return nil, nil
		},
	}
	reg := newRCASymbolRegistry(t, []string{repo}, server)
	tl, _ := reg.Get("rca_symbol")

	out, err := tl.Execute(context.Background(), map[string]any{
		"action": "definition",
		"repo":   "cloudgame",
		"file":   "apps/cgvmagent/internal/biz/agent.go",
		"line":   3, // func NewAgent — character omitted (0) should snap to identifier
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lsp.NormalizeRoot(gotRoot) != lsp.NormalizeRoot(modRoot) {
		t.Fatalf("LSP root = %q, want module root %q", gotRoot, modRoot)
	}
	if gotFile != "internal/biz/agent.go" {
		t.Fatalf("LSP file = %q, want module-relative path", gotFile)
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "rca_symbol")
	locations := m["locations"].([]lsp.Location)
	if len(locations) != 1 || locations[0].File != "apps/cgvmagent/internal/biz/agent.go" {
		t.Fatalf("locations remapped = %#v", locations)
	}
}

func TestResolveSymbolCandidates_LongLineAndSkipGit(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "cloudgame")
	writeRCASymbolSource(t, repo, "ok.go", "package main\n\nfunc Target() {}\n")
	writeRCASymbolSource(t, repo, ".git/objects/pack/x.go", "package pack\n\nfunc Target() {}\n")
	long := strings.Repeat("x", bufio.MaxScanTokenSize+1024)
	writeRCASymbolSource(t, repo, "long.go", "package main\n\n"+long+"\nfunc TargetAlso() {}\n")

	locations, unique, _, err := resolveSymbolCandidates([]string{repo}, "cloudgame", "Target", 10)
	if err != nil {
		t.Fatalf("resolveSymbolCandidates: %v", err)
	}
	if !unique || len(locations) != 1 || locations[0].File != "ok.go" {
		t.Fatalf("want unique ok.go candidate, got unique=%v locations=%#v", unique, locations)
	}
}
