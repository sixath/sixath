package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/memorysearch"
)

func TestAgentWorkspaceRememberWritesMemoryAndUserFiles(t *testing.T) {
	root := t.TempDir()
	var syncReasons []string
	backend := NewAgentWorkspace(func(context.Context, string, string) (memorysearch.MemorySearchManager, error) {
		return &fakeMemorySearchManager{
			sync: func(_ context.Context, params *memorysearch.SyncParams) error {
				syncReasons = append(syncReasons, params.Reason)
				return nil
			},
		}, nil
	})

	tests := []struct {
		name   string
		target string
		path   string
	}{
		{name: "memory", target: "memory", path: "MEMORY.md"},
		{name: "user file", target: "user_file", path: "USER.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, err := backend.Remember(context.Background(), RememberInput{
				Scope:         ScopeAgent,
				AgentID:       "agent-1",
				WorkspaceRoot: root,
				Action:        ActionAdd,
				Target:        tt.target,
				Content:       "remember this",
			})
			if err != nil {
				t.Fatalf("Remember() error = %v", err)
			}
			if hit.Scope != ScopeAgent || hit.Source != SourceFiles || hit.Path != tt.path {
				t.Fatalf("Remember() hit = %#v, want agent file %q", hit, tt.path)
			}
			body, err := os.ReadFile(filepath.Join(root, tt.path))
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", tt.path, err)
			}
			if string(body) != "remember this\n" {
				t.Fatalf("%s content = %q, want remembered content", tt.path, body)
			}
		})
	}
	if len(syncReasons) != 2 {
		t.Fatalf("Sync() calls = %d, want 2", len(syncReasons))
	}
	for _, reason := range syncReasons {
		if reason != "memory_tool" {
			t.Fatalf("Sync() reason = %q, want memory_tool", reason)
		}
	}
}

func TestAgentWorkspaceRecallEmptyQueryReturnsNoHits(t *testing.T) {
	backend := NewAgentWorkspace(func(context.Context, string, string) (memorysearch.MemorySearchManager, error) {
		t.Fatal("GetManager must not be called for an empty query")
		return nil, nil
	})

	hits, err := backend.Recall(context.Background(), RecallQuery{
		Scope:         ScopeAgent,
		WorkspaceRoot: t.TempDir(),
		Query:         "  ",
	})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("Recall() returned %d hits, want 0", len(hits))
	}
}

func TestAgentWorkspaceRecallDropsSessionTranscriptHits(t *testing.T) {
	backend := NewAgentWorkspace(func(context.Context, string, string) (memorysearch.MemorySearchManager, error) {
		return &fakeMemorySearchManager{
			search: func(context.Context, string, *memorysearch.SearchOpts) ([]memorysearch.MemorySearchResult, error) {
				return []memorysearch.MemorySearchResult{
					{Path: "sessions/abc.md", Source: "sessions", Snippet: "noise E2E_AGENT_NOTE", Score: 1},
					{Path: "MEMORY.md", Source: "memory", Snippet: "E2E_AGENT_NOTE=ok", Score: 0.5},
					{Path: "memory/notes.md", Source: "memory", Snippet: "E2E_AGENT_NOTE also here", Score: 0.4},
				}, nil
			},
		}, nil
	})

	hits, err := backend.Recall(context.Background(), RecallQuery{
		Scope:         ScopeAgent,
		WorkspaceRoot: t.TempDir(),
		Query:         "E2E_AGENT_NOTE",
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Recall() hits = %d, want 2 file hits", len(hits))
	}
	if hits[0].Path != "MEMORY.md" || hits[1].Path != "memory/notes.md" {
		t.Fatalf("Recall() paths = %q,%q; want MEMORY.md then memory/notes.md", hits[0].Path, hits[1].Path)
	}
}

type fakeMemorySearchManager struct {
	sync   func(context.Context, *memorysearch.SyncParams) error
	search func(context.Context, string, *memorysearch.SearchOpts) ([]memorysearch.MemorySearchResult, error)
}

func (f *fakeMemorySearchManager) Search(ctx context.Context, query string, opts *memorysearch.SearchOpts) ([]memorysearch.MemorySearchResult, error) {
	if f.search != nil {
		return f.search(ctx, query, opts)
	}
	return nil, nil
}

func (f *fakeMemorySearchManager) ReadFile(context.Context, *memorysearch.ReadFileParams) (*memorysearch.ReadFileResult, error) {
	return nil, nil
}

func (f *fakeMemorySearchManager) Status(context.Context) (*memorysearch.MemoryProviderStatus, error) {
	return nil, nil
}

func (f *fakeMemorySearchManager) Sync(ctx context.Context, params *memorysearch.SyncParams) error {
	if f.sync == nil {
		return nil
	}
	return f.sync(ctx, params)
}
