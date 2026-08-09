package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/sixath/framework/memory/hub"
)

// KnowledgeHit is a normalized search hit.
type KnowledgeHit struct {
	ID      string  `json:"id"`
	Source  string  `json:"source"` // transcript|workspace|graph|units|wiki|codegraph
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// TranscriptSearcher searches session transcripts.
type TranscriptSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// WorkspaceSearcher searches agent workspace files.
type WorkspaceSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// GraphSearcher searches Neo4j-style graph.
type GraphSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// UnitSearcher searches memory units (only when source explicitly includes units).
type UnitSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// WikiSearcher is optional local wiki index (P3; nil = capability off).
type WikiSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// CodeGraphSearcher is optional code graph index (P3; nil = capability off).
type CodeGraphSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// KnowledgeBackends wires optional search backends.
type KnowledgeBackends struct {
	Transcript TranscriptSearcher
	Workspace  WorkspaceSearcher
	Graph      GraphSearcher
	Units      UnitSearcher
	Wiki       WikiSearcher
	CodeGraph  CodeGraphSearcher
	UnitsWrite UnitWriter // optional write path for memory units
}

// LocalKnowledge is the default KnowledgeProvider.
type LocalKnowledge struct {
	backends KnowledgeBackends
}

func NewLocalKnowledge(b KnowledgeBackends) *LocalKnowledge {
	return &LocalKnowledge{backends: b}
}

// WikiWriter returns the wiki write backend when Wiki implements WikiWriter.
func (k *LocalKnowledge) WikiWriter() WikiWriter {
	if k == nil {
		return nil
	}
	w, _ := k.backends.Wiki.(WikiWriter)
	return w
}

// UnitWriter returns the optional units write backend.
func (k *LocalKnowledge) UnitWriter() UnitWriter {
	if k == nil {
		return nil
	}
	return k.backends.UnitsWrite
}

func (k *LocalKnowledge) Name() string { return localHubName }

func (k *LocalKnowledge) Capabilities() hub.Capabilities {
	flags := map[string]bool{}
	if k.backends.Wiki != nil {
		flags["wiki"] = true
	}
	if k.backends.CodeGraph != nil {
		flags["code_graph"] = true
	}
	write := k.backends.Wiki != nil || k.backends.UnitsWrite != nil
	if write {
		flags["knowledge_write"] = true
	}
	return hub.Capabilities{Write: write, Flags: flags}
}

func (k *LocalKnowledge) DescribeTools() []hub.ToolDesc {
	srcDesc := "comma list: transcript,workspace,graph,units"
	if k.backends.Wiki != nil {
		srcDesc += ",wiki"
	}
	if k.backends.CodeGraph != nil {
		srcDesc += ",codegraph"
	}
	tools := []hub.ToolDesc{
		{
			Name:        "knowledge_search",
			Description: "Search local knowledge (default: transcript/workspace/graph; units/wiki/codegraph only if source set and backend available).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":  map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer"},
					"source": map[string]any{"type": "string", "description": srcDesc},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "knowledge_read",
			Description: "Read a knowledge hit by id and source.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":            map[string]any{"type": "string"},
					"source":        map[string]any{"type": "string"},
					"include_draft": map[string]any{"type": "boolean", "description": "wiki: prefer draft over formal page when true"},
				},
				"required": []string{"id"},
			},
		},
	}
	if k.Capabilities().Write {
		tools = append(tools,
			hub.ToolDesc{
				Name:        "knowledge_write",
				Description: "Write a knowledge draft (wiki or units). Does not enter default search until approved.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"source":  map[string]any{"type": "string", "description": "wiki|units"},
						"id":      map[string]any{"type": "string", "description": "wiki path or units id (optional for new units)"},
						"content": map[string]any{"type": "string"},
						"title":   map[string]any{"type": "string", "description": "units title (optional)"},
					},
					"required": []string{"source", "content"},
				},
			},
			hub.ToolDesc{
				Name:        "knowledge_approve",
				Description: "Promote a knowledge draft to active (wiki or units).",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"source":    map[string]any{"type": "string", "description": "wiki|units"},
						"id":        map[string]any{"type": "string"},
						"overwrite": map[string]any{"type": "boolean", "description": "wiki: allow overwriting existing formal page"},
					},
					"required": []string{"source", "id"},
				},
			},
		)
	}
	return tools
}

func (k *LocalKnowledge) Call(ctx context.Context, id hub.Identity, tool string, args map[string]any) (any, error) {
	switch tool {
	case "knowledge_search":
		return k.search(ctx, args)
	case "knowledge_read":
		return k.read(ctx, args)
	case "knowledge_write":
		return k.write(ctx, id, args)
	case "knowledge_approve":
		return k.approve(ctx, id, args)
	default:
		return nil, fmt.Errorf("%w: tool %q", hub.ErrNotSupported, tool)
	}
}

func (k *LocalKnowledge) search(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("hub/local: empty query")
	}
	limit := 5
	switch v := args["limit"].(type) {
	case int:
		if v > 0 {
			limit = v
		}
	case float64:
		if int(v) > 0 {
			limit = int(v)
		}
	}
	sources := defaultSearchSources(args["source"])
	var hits []KnowledgeHit
	for _, src := range sources {
		part, err := k.searchSource(ctx, src, q, limit)
		if err != nil {
			return nil, err
		}
		hits = append(hits, part...)
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func defaultSearchSources(raw any) []string {
	s, _ := raw.(string)
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{"transcript", "workspace", "graph"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (k *LocalKnowledge) searchSource(ctx context.Context, src, q string, limit int) ([]KnowledgeHit, error) {
	switch src {
	case "transcript":
		if k.backends.Transcript == nil {
			return nil, nil
		}
		return k.backends.Transcript.Search(ctx, q, limit)
	case "workspace":
		if k.backends.Workspace == nil {
			return nil, nil
		}
		return k.backends.Workspace.Search(ctx, q, limit)
	case "graph":
		if k.backends.Graph == nil {
			return nil, nil
		}
		return k.backends.Graph.Search(ctx, q, limit)
	case "units":
		if k.backends.Units == nil {
			return nil, nil
		}
		return k.backends.Units.Search(ctx, q, limit)
	case "wiki":
		if k.backends.Wiki == nil {
			return nil, nil
		}
		return k.backends.Wiki.Search(ctx, q, limit)
	case "codegraph":
		if k.backends.CodeGraph == nil {
			return nil, nil
		}
		return k.backends.CodeGraph.Search(ctx, q, limit)
	default:
		return nil, fmt.Errorf("%w: source %q", hub.ErrNotSupported, src)
	}
}

// knowledgeReader is optional on wiki/codegraph backends for knowledge_read hydrate.
type knowledgeReader interface {
	Read(ctx context.Context, id string) (*KnowledgeHit, error)
}

func (k *LocalKnowledge) read(ctx context.Context, args map[string]any) (any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("hub/local: empty id")
	}
	src, _ := args["source"].(string)
	src = strings.ToLower(strings.TrimSpace(src))
	includeDraft := parseBoolArg(args["include_draft"])
	switch src {
	case "wiki":
		ww := k.WikiWriter()
		if ww != nil {
			hit, err := k.readWiki(ctx, ww, id, includeDraft)
			if err != nil {
				return nil, err
			}
			return *hit, nil
		}
		if r, ok := k.backends.Wiki.(knowledgeReader); ok {
			hit, err := r.Read(ctx, id)
			if err != nil {
				return nil, err
			}
			return *hit, nil
		}
	case "codegraph":
		if r, ok := k.backends.CodeGraph.(knowledgeReader); ok {
			hit, err := r.Read(ctx, id)
			if err != nil {
				return nil, err
			}
			return *hit, nil
		}
	}
	// Other sources: id/source echo until Portal hydrates.
	return KnowledgeHit{ID: id, Source: src, Content: ""}, nil
}

func (k *LocalKnowledge) readWiki(ctx context.Context, ww WikiWriter, id string, includeDraft bool) (*KnowledgeHit, error) {
	if IsWikiDraftFile(id) {
		hit, err := ww.Read(ctx, id)
		if err != nil {
			return nil, err
		}
		canonical, err := CanonicalWikiID(id)
		if err != nil {
			return nil, err
		}
		hit.ID = canonical
		return hit, nil
	}
	if includeDraft {
		return ww.ReadPreferDraft(ctx, id)
	}
	return ww.Read(ctx, id)
}

func (k *LocalKnowledge) write(ctx context.Context, identity hub.Identity, args map[string]any) (any, error) {
	src, _ := args["source"].(string)
	src = strings.ToLower(strings.TrimSpace(src))
	content, _ := args["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("hub/local: empty content")
	}
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)

	switch src {
	case "wiki":
		ww := k.WikiWriter()
		if ww == nil {
			return nil, fmt.Errorf("hub/local: wiki write not configured")
		}
		canonical, err := ww.WriteDraft(ctx, id, content)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"source": "wiki",
			"id":     canonical,
			"status": "draft",
			"path":   DraftPathForWikiID(canonical),
		}, nil
	case "units":
		uw := k.UnitWriter()
		if uw == nil {
			return nil, fmt.Errorf("hub/local: units write not configured")
		}
		agentID := strings.TrimSpace(identity.AgentID)
		if agentID == "" {
			return nil, fmt.Errorf("hub/local: empty agent id")
		}
		unitID, err := uw.WriteDraft(ctx, agentID, id, title, content)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"source": "units",
			"id":     unitID,
			"status": "draft",
		}, nil
	default:
		return nil, fmt.Errorf("%w: source %q", hub.ErrNotSupported, src)
	}
}

func (k *LocalKnowledge) approve(ctx context.Context, identity hub.Identity, args map[string]any) (any, error) {
	src, _ := args["source"].(string)
	src = strings.ToLower(strings.TrimSpace(src))
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("hub/local: empty id")
	}
	overwrite := parseBoolArg(args["overwrite"])

	switch src {
	case "wiki":
		ww := k.WikiWriter()
		if ww == nil {
			return nil, fmt.Errorf("hub/local: wiki write not configured")
		}
		if err := ww.ApproveDraft(ctx, id, overwrite); err != nil {
			return nil, err
		}
		canonical, err := CanonicalWikiID(id)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"source": "wiki",
			"id":     canonical,
			"status": "active",
		}, nil
	case "units":
		uw := k.UnitWriter()
		if uw == nil {
			return nil, fmt.Errorf("hub/local: units write not configured")
		}
		agentID := strings.TrimSpace(identity.AgentID)
		if agentID == "" {
			return nil, fmt.Errorf("hub/local: empty agent id")
		}
		if err := uw.ApproveDraft(ctx, agentID, id); err != nil {
			return nil, err
		}
		return map[string]any{
			"source": "units",
			"id":     id,
			"status": "active",
		}, nil
	default:
		return nil, fmt.Errorf("%w: source %q", hub.ErrNotSupported, src)
	}
}

func parseBoolArg(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		s := strings.TrimSpace(strings.ToLower(b))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return b != 0
	case int:
		return b != 0
	default:
		return false
	}
}

var _ hub.KnowledgeProvider = (*LocalKnowledge)(nil)
