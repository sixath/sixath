package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sixath/framework/memorysearch"
)

var agentWorkspaceFileLocks sync.Map // absolute path -> *sync.Mutex

// AgentWorkspace persists agent memory in MEMORY.md and USER.md within a workspace.
type AgentWorkspace struct {
	GetManager func(ctx context.Context, agentID, workspaceRoot string) (memorysearch.MemorySearchManager, error)
}

var _ AgentWorkspaceBackend = (*AgentWorkspace)(nil)

func NewAgentWorkspace(getManager func(context.Context, string, string) (memorysearch.MemorySearchManager, error)) *AgentWorkspace {
	return &AgentWorkspace{GetManager: getManager}
}

func (a *AgentWorkspace) Remember(ctx context.Context, in RememberInput) (MemoryHit, error) {
	workspaceRoot, relPath, fullPath, err := agentWorkspacePath(in.WorkspaceRoot, in.Target)
	if err != nil {
		return MemoryHit{}, err
	}
	if err := validateFileAction(in); err != nil {
		return MemoryHit{}, err
	}

	unlock := lockAgentWorkspaceFile(fullPath)
	defer unlock()

	body, err := os.ReadFile(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return MemoryHit{}, err
	}
	newBody, err := ApplyMemoryAction(body, string(in.Action), in.Content, in.OldText)
	if err != nil {
		return MemoryHit{}, err
	}
	if err := AtomicWriteFile(fullPath, newBody); err != nil {
		return MemoryHit{}, err
	}

	// Index synchronization is best effort: the durable file write is already complete.
	if mgr, mgrErr := a.manager(ctx, in.AgentID, workspaceRoot); mgrErr == nil && mgr != nil {
		_ = mgr.Sync(ctx, &memorysearch.SyncParams{Reason: "memory_tool", Force: false})
	}
	return MemoryHit{
		Scope:   ScopeAgent,
		Source:  SourceFiles,
		ID:      relPath,
		Path:    relPath,
		Content: string(newBody),
	}, nil
}

func (a *AgentWorkspace) Recall(ctx context.Context, q RecallQuery) ([]MemoryHit, error) {
	if strings.TrimSpace(q.WorkspaceRoot) == "" {
		return nil, errors.New("memory: workspace_root is required")
	}
	query := strings.TrimSpace(q.Query)
	if query == "" {
		return []MemoryHit{}, nil
	}
	mgr, err := a.manager(ctx, q.AgentID, q.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return []MemoryHit{}, nil
	}
	// Request extra candidates so filtering sessions/ noise still leaves file hits.
	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}
	searchLimit := limit * 4
	if searchLimit < 20 {
		searchLimit = 20
	}
	hits, err := mgr.Search(ctx, query, &memorysearch.SearchOpts{
		MaxResults: searchLimit,
		MinScore:   q.MinScore,
	})
	if err != nil {
		return nil, err
	}
	out := make([]MemoryHit, 0, len(hits))
	for _, hit := range hits {
		if !isAgentMemoryFileHit(hit) {
			continue
		}
		out = append(out, MemoryHit{
			Scope:   ScopeAgent,
			Source:  SourceFiles,
			ID:      fmt.Sprintf("%s:%d-%d", hit.Path, hit.StartLine, hit.EndLine),
			Path:    hit.Path,
			Content: hit.Snippet,
			Score:   hit.Score,
			Metadata: map[string]any{
				"source":   hit.Source,
				"citation": hit.Citation,
			},
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// isAgentMemoryFileHit keeps MEMORY.md / USER.md / memory/** and drops sessions/ transcript noise.
func isAgentMemoryFileHit(hit memorysearch.MemorySearchResult) bool {
	if strings.EqualFold(strings.TrimSpace(hit.Source), "sessions") {
		return false
	}
	p := filepath.ToSlash(strings.TrimSpace(hit.Path))
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "sessions/") {
		return false
	}
	base := filepath.Base(p)
	if strings.EqualFold(base, "MEMORY.md") || strings.EqualFold(base, "USER.md") {
		return true
	}
	if strings.HasPrefix(p, "memory/") && strings.HasSuffix(strings.ToLower(p), ".md") {
		return true
	}
	// ExtraPaths / other indexed memory sources (not session transcripts).
	return strings.EqualFold(strings.TrimSpace(hit.Source), "memory")
}

func (a *AgentWorkspace) Get(ctx context.Context, ref GetRef) (MemoryHit, error) {
	workspaceRoot := strings.TrimSpace(ref.WorkspaceRoot)
	if workspaceRoot == "" {
		return MemoryHit{}, errors.New("memory: workspace_root is required")
	}
	relPath, fullPath, err := resolveAgentGetPath(workspaceRoot, ref.Path)
	if err != nil {
		return MemoryHit{}, err
	}
	if mgr, err := a.manager(ctx, ref.AgentID, workspaceRoot); err != nil {
		return MemoryHit{}, err
	} else if mgr != nil {
		result, err := mgr.ReadFile(ctx, &memorysearch.ReadFileParams{RelPath: relPath})
		if err != nil {
			return MemoryHit{}, err
		}
		if result == nil {
			return MemoryHit{}, fmt.Errorf("memory: file %q not found", relPath)
		}
		path := result.Path
		if path == "" {
			path = relPath
		}
		return MemoryHit{Scope: ScopeAgent, Source: SourceFiles, ID: path, Path: path, Content: result.Text}, nil
	}

	body, err := os.ReadFile(fullPath)
	if err != nil {
		return MemoryHit{}, err
	}
	return MemoryHit{Scope: ScopeAgent, Source: SourceFiles, ID: relPath, Path: relPath, Content: string(body)}, nil
}

// resolveAgentGetPath allows MEMORY.md, USER.md, and memory/**/*.md under workspace (no escape).
func resolveAgentGetPath(workspaceRoot, rel string) (relPath, fullPath string, err error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("memory: unsupported agent memory path %q", rel)
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "." || strings.HasPrefix(clean, "../") {
		return "", "", fmt.Errorf("memory: unsupported agent memory path %q", rel)
	}
	allowed := clean == "MEMORY.md" || clean == "USER.md" ||
		(strings.HasPrefix(clean, "memory/") && strings.HasSuffix(clean, ".md"))
	if !allowed {
		return "", "", fmt.Errorf("memory: unsupported agent memory path %q", rel)
	}
	absRoot, err := filepath.Abs(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return "", "", err
	}
	full := filepath.Join(absRoot, filepath.FromSlash(clean))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", "", err
	}
	relToRoot, err := filepath.Rel(absRoot, absFull)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return "", "", fmt.Errorf("memory: unsupported agent memory path %q", rel)
	}
	return clean, absFull, nil
}

func (a *AgentWorkspace) manager(ctx context.Context, agentID, workspaceRoot string) (memorysearch.MemorySearchManager, error) {
	if a == nil || a.GetManager == nil {
		return nil, nil
	}
	return a.GetManager(ctx, agentID, workspaceRoot)
}

func agentWorkspacePath(workspaceRoot, target string) (string, string, string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", "", "", errors.New("memory: workspace_root is required")
	}
	relPath := TargetRelPath(target)
	if relPath == "" {
		return "", "", "", fmt.Errorf("memory: unsupported agent memory target %q", target)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	return absRoot, relPath, filepath.Join(absRoot, relPath), nil
}

func memoryTargetForPath(path string) string {
	if path == "USER.md" {
		return "user_file"
	}
	return "memory"
}

func validateFileAction(in RememberInput) error {
	switch in.Action {
	case ActionAdd:
		if strings.TrimSpace(in.Content) == "" {
			return errors.New("memory: content is required for add")
		}
	case ActionReplace:
		if strings.TrimSpace(in.OldText) == "" {
			return errors.New("memory: old_text is required for replace")
		}
		if strings.TrimSpace(in.Content) == "" {
			return errors.New("memory: content is required for replace")
		}
	case ActionRemove:
		if strings.TrimSpace(in.OldText) == "" {
			return errors.New("memory: old_text is required for remove")
		}
	default:
		return fmt.Errorf("memory: unsupported agent action %q", in.Action)
	}
	return nil
}

func lockAgentWorkspaceFile(path string) func() {
	v, _ := agentWorkspaceFileLocks.LoadOrStore(path, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
