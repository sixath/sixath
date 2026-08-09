package lsp

import (
	"context"
	"time"
)

// Position is an LSP position (0-based line and UTF-16 character).
type Position struct {
	Line      int `json:"line"`      // 0-based, LSP
	Character int `json:"character"` // 0-based, UTF-16 code unit
}

// Location is a repo-relative symbol location exposed to tool callers.
// Line is 1-based; Character is 0-based UTF-16 (converted at the tool layer).
type Location struct {
	Repo      string
	File      string // repo-relative, slash-separated path
	Line      int    // 1-based for external callers
	Character int    // 0-based, UTF-16
	Name      string // optional symbol name
}

// ServerOpts configures a language server process.
type ServerOpts struct {
	Command        string
	Env            []string
	InitTimeout    time.Duration
	RequestTimeout time.Duration
}

// LanguageServer provides definition and reference queries for a workspace root.
type LanguageServer interface {
	EnsureReady(ctx context.Context, root string) error
	Definition(ctx context.Context, root, relPath string, pos Position) ([]Location, error)
	References(ctx context.Context, root, relPath string, pos Position, includeDeclaration bool) ([]Location, error)
	Close(ctx context.Context) error
}

// ServerFactory constructs a LanguageServer for a workspace root.
type ServerFactory func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error)
