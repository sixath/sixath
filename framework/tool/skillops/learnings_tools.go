package toolskill

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/tool"
)

// RegisterAppendLearningTool 注册 append_learning：向 workspace/.learnings 或已发现的 learnings 目录追加条目。
func RegisterAppendLearningTool(reg *tool.Registry) error {
	if reg == nil {
		return errors.New("append_learning: registry is nil")
	}
	return reg.Register(tool.Tool{
		Name:        "append_learning",
		Description: "Append a structured learning or error entry to .learnings/LEARNINGS.md or ERRORS.md under the agent workspace (or repo-root .learnings). Use after fixing a problem or receiving user correction.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "learnings or errors (default learnings).",
					"enum":        []string{"learnings", "errors"},
				},
				"category": map[string]any{
					"type":        "string",
					"description": "correction | insight | knowledge_gap | best_practice | error",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "One-line summary of what to remember.",
				},
				"details": map[string]any{
					"type":        "string",
					"description": "Optional longer explanation.",
				},
				"area": map[string]any{
					"type":        "string",
					"description": "Optional area tag, e.g. backend, ops.",
				},
			},
			"required": []string{"summary"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			ws, _ := ctx.Value(tool.ContextKeyWorkspaceRoot).(string)
			if strings.TrimSpace(ws) == "" {
				return nil, errors.New("append_learning: workspace_root missing in context")
			}
			target := strings.ToLower(strings.TrimSpace(paramString(params, "target")))
			if target == "" {
				target = "learnings"
			}
			category := strings.TrimSpace(paramString(params, "category"))
			summary := strings.TrimSpace(paramString(params, "summary"))
			if summary == "" {
				return nil, errors.New("append_learning: summary is required")
			}
			details := strings.TrimSpace(paramString(params, "details"))
			area := strings.TrimSpace(paramString(params, "area"))

			if err := growth.AppendLearning(ws, target, category, summary, details, area); err != nil {
				return nil, err
			}
			filename := "LEARNINGS.md"
			if target == "errors" {
				filename = "ERRORS.md"
			}
			dirs := growth.DiscoverLearningsDirs(ws)
			dir := filepath.Join(ws, ".learnings")
			if len(dirs) > 0 {
				dir = dirs[0]
			}
			return fmt.Sprintf("appended to %s", filepath.Join(dir, filename)), nil
		},
	})
}

func paramString(params map[string]any, key string) string {
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
