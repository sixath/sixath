package tooldata

import (
	"context"
	"errors"
	"fmt"
	"github.com/sixath/framework/events"
	"strings"
	"time"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/metadata"
	"github.com/sixath/framework/obs"
	"github.com/sixath/framework/tool"
)

// ListTablesConfig 用于构造 list_tables 工具的依赖（Store、Registry、默认数据源 ID）。
type ListTablesConfig struct {
	Store               *metadata.InMemoryStore
	Registry            *datasource.Registry
	DefaultDatasourceID string
}

// RegisterListTablesTool 向 r 注册 list_tables 工具。cfg 可为 nil，此时 Execute 会返回“未配置”错误。
// opts 可选：若 opts 中 Description 非空则覆盖默认描述（用于按数据源类型差异化表述）。
func RegisterListTablesTool(r *tool.Registry, cfg *ListTablesConfig, opts ...*tool.RegisterToolOptions) error {
	desc := "List tables (or collections) in the current datasource. Returns table names and optional comments."
	if len(opts) > 0 && opts[0] != nil && opts[0].Description != "" {
		desc = opts[0].Description
	}
	return r.Register(tool.Tool{
		Name:        "list_tables",
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"datasource_id": map[string]any{
					"type":        "string",
					"description": "Datasource ID (same as in tool binding, e.g. main); omit to use the agent default.",
				},
				"keyword": map[string]any{
					"type":        "string",
					"description": "Optional. Filter by keyword: only return tables/indices whose name contains this string (case-insensitive). E.g. 'vm' to list indices containing 'vm'.",
				},
			},
			"required": []string{},
		},
		Execute: buildListTablesExecute(cfg),
	})
}

func buildListTablesExecute(cfg *ListTablesConfig) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		start := time.Now()
		status := "ok"
		defer func() {
			obs.ObserveDataQueryTool("list_tables", status, time.Since(start))
		}()

		if cfg == nil || cfg.Store == nil {
			status = "error"
			return nil, errors.New("list_tables: not configured (missing store)")
		}
		datasourceID := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
		if datasourceID == "" {
			status = "error"
			return nil, errors.New("list_tables: datasource_id is required (or set default)")
		}
		if err := RejectElasticsearchDatasource(cfg.Registry, datasourceID, "list_tables"); err != nil {
			status = "error"
			return nil, err
		}

		schema, err := metadata.EnsureSchemaForDatasource(ctx, cfg.Registry, cfg.Store, datasourceID)
		if err != nil {
			status = "error"
			return nil, fmt.Errorf("list_tables: ensure schema: %w", err)
		}
		if schema == nil {
			status = "error"
			return nil, errors.New("list_tables: no schema available")
		}

		keyword := ""
		if p := params["keyword"]; p != nil {
			if s, ok := p.(string); ok {
				keyword = strings.TrimSpace(s)
			}
		}

		out := make([]map[string]string, 0, len(schema.Tables))
		kwLower := strings.ToLower(keyword)
		for _, t := range schema.Tables {
			if keyword != "" && !strings.Contains(strings.ToLower(t.Name), kwLower) {
				continue
			}
			row := map[string]string{"name": t.Name}
			if t.Comment != "" {
				row["comment"] = t.Comment
			}
			out = append(out, row)
		}
		rid, _ := ctx.Value(tool.ContextKeyRequestID).(string)
		invokedPayload := map[string]any{
			"tooName":      "list_table",
			"datasourceID": datasourceID,
		}
		events.DefaultBus().Publish(ctx, events.Event{
			Kind:      events.ToolExecuted,
			RequestID: rid,
			Payload:   invokedPayload,
		})
		return out, nil
	}
}

// ListTablesResult 供调用方做类型断言的返回结构（与 JSON 序列化一致）。
func ListTablesResult(raw any) ([]map[string]string, bool) {
	if raw == nil {
		return nil, false
	}
	// Execute 返回 []map[string]string
	slice, ok := raw.([]map[string]string)
	if ok {
		return slice, true
	}
	// 若从 JSON 反序列化得到 []interface{}
	if list, ok := raw.([]any); ok && len(list) > 0 {
		out := make([]map[string]string, 0, len(list))
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			row := make(map[string]string)
			for k, v := range m {
				if s, ok := v.(string); ok {
					row[k] = s
				}
			}
			out = append(out, row)
		}
		return out, true
	}
	return nil, false
}
