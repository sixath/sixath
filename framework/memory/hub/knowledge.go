package hub

import "context"

// ToolDesc describes an LLM-callable knowledge tool.
type ToolDesc struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// KnowledgeProvider exposes on-demand knowledge tools (search/read/…).
// PrefetchHints is intentionally omitted in P0 (YAGNI).
type KnowledgeProvider interface {
	Name() string
	Capabilities() Capabilities
	DescribeTools() []ToolDesc
	Call(ctx context.Context, id Identity, tool string, args map[string]any) (any, error)
}
