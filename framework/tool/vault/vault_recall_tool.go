package toolvault

import (
	"context"
	"errors"
	"strings"

	"github.com/sixath/framework/tool"
	"github.com/sixath/framework/vault"
)

// VaultRecallFunc 是 vault_recall 的召回实现，由 portal 注入（闭包捕获索引与 embed）。
type VaultRecallFunc func(ctx context.Context, query string, limit int) ([]vault.Hit, error)

// RegisterVaultRecallTool 注册 vault_recall 工具；recall 为 nil 时工具仍注册但执行时报错（便于测试）。
func RegisterVaultRecallTool(reg *tool.Registry, recall VaultRecallFunc) error {
	if reg == nil {
		return errors.New("vault_recall: registry is nil")
	}
	return reg.Register(tool.Tool{
		Name: "vault_recall",
		Description: "Search the experience knowledge vault for prior troubleshooting notes " +
			"(workflows, gotchas, tool usage, cases). Use BEFORE guessing how to query logs or " +
			"trace a flow — there may already be a recorded way. Returns ranked note excerpts " +
			"with their source path, score, type, and creation date.",
		Effect:  tool.EffectRead,
		Toolset: tool.ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Natural-language search query for prior experience."},
				"limit": map[string]any{"type": "integer", "description": "Max notes to return (default 5)."},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			query, _ := params["query"].(string)
			query = strings.TrimSpace(query)
			if query == "" {
				return map[string]any{"ok": false, "error": "query is required"}, nil
			}
			if recall == nil {
				return map[string]any{"ok": false, "error": "vault recall not configured"}, nil
			}
			limit := 5
			if n, ok := params["limit"].(float64); ok && n > 0 {
				limit = int(n)
			}
			hits, err := recall(ctx, query, limit)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			out := make([]map[string]any, 0, len(hits))
			for _, h := range hits {
				excerpt := h.Note.Body
				if len(excerpt) > 500 {
					excerpt = excerpt[:500]
				}
				out = append(out, map[string]any{
					"path":    h.Note.Path,
					"score":   h.Score,
					"type":    h.Note.Type,
					"source":  h.Note.Source,
					"created": h.Note.Created,
					"excerpt": excerpt,
				})
			}
			return map[string]any{"ok": true, "hits": out}, nil
		},
	})
}
