package chat

import (
	"context"
	"fmt"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/tool"
)

// RegisterKnowledgeHubTools registers knowledge_* tools from the resolved KnowledgeProvider.
// Assembly failure (unregistered override) is returned to the caller (fail-closed at wiring).
func RegisterKnowledgeHubTools(reg *tool.Registry, rt biz.RuntimeToolsConfig) error {
	if reg == nil {
		return nil
	}
	_, know, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return fmt.Errorf("memory hub knowledge: %w", err)
	}
	idFromCtx := func(ctx context.Context) hub.Identity {
		return hub.Identity{
			AgentID:   contextString(ctx, tool.ContextKeyAgentID),
			UserID:    contextString(ctx, tool.ContextKeyUserID),
			SessionID: contextString(ctx, tool.ContextKeySessionID),
		}
	}
	for _, desc := range know.DescribeTools() {
		d := desc
		provider := know
		if err := reg.Register(tool.Tool{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.InputSchema,
			Toolset:     tool.ToolsetMemory,
			Execute: func(ctx context.Context, params map[string]any) (any, error) {
				return provider.Call(ctx, idFromCtx(ctx), d.Name, params)
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func contextString(ctx context.Context, key string) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(key).(string)
	return v
}
