package tooldata

import (
	"context"
	"errors"
	"fmt"
	"github.com/sixath/framework/events"
	"time"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/metadata"
	"github.com/sixath/framework/obs"
	"github.com/sixath/framework/tool"
)

// DescribeTableConfig 用于构造 describe_table 工具的依赖。
type DescribeTableConfig struct {
	Store               *metadata.InMemoryStore
	Registry            *datasource.Registry
	DefaultDatasourceID string
}

// RegisterDescribeTableTool 向 r 注册 describe_table 工具。
// opts 可选：若 opts 中 Description 非空则覆盖默认描述（用于按数据源类型差异化表述）。
func RegisterDescribeTableTool(r *tool.Registry, cfg *DescribeTableConfig, opts ...*tool.RegisterToolOptions) error {
	desc := "Describe table structure in the current datasource. Returns columns with type and nullability."
	if len(opts) > 0 && opts[0] != nil && opts[0].Description != "" {
		desc = opts[0].Description
	}
	return r.Register(tool.Tool{
		Name:        "describe_table",
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"table_name": map[string]any{
					"type":        "string",
					"description": "Table name to describe",
				},
				"datasource_id": map[string]any{
					"type":        "string",
					"description": "Datasource ID; if omitted, the session default is used",
				},
			},
			"required": []string{"table_name"},
		},
		Execute: buildDescribeTableExecute(cfg),
	})
}

func buildDescribeTableExecute(cfg *DescribeTableConfig) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		start := time.Now()
		status := "ok"
		defer func() {
			obs.ObserveDataQueryTool("describe_table", status, time.Since(start))
		}()

		if cfg == nil || cfg.Store == nil {
			status = "error"
			return nil, errors.New("describe_table: not configured (missing store)")
		}

		datasourceID := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
		if datasourceID == "" {
			status = "error"
			return nil, errors.New("describe_table: datasource_id is required (or set default)")
		}

		rawName, ok := params["table_name"]
		if !ok {
			status = "error"
			return nil, errors.New("describe_table: table_name is required")
		}
		tableName, ok := rawName.(string)
		if !ok || tableName == "" {
			status = "error"
			return nil, errors.New("describe_table: table_name must be a non-empty string")
		}

		if _, err := metadata.EnsureSchemaForDatasource(ctx, cfg.Registry, cfg.Store, datasourceID); err != nil {
			status = "error"
			return nil, fmt.Errorf("describe_table: ensure schema: %w", err)
		}
		tbl, err := cfg.Store.GetTable(ctx, tableName)
		if err != nil {
			status = "error"
			return nil, fmt.Errorf("describe_table: get table: %w", err)
		}
		if tbl == nil && cfg.Registry != nil {
			if _, err := metadata.RefreshFromRegistry(ctx, cfg.Registry, cfg.Store, datasourceID); err != nil {
				status = "error"
				return nil, fmt.Errorf("describe_table: refresh schema: %w", err)
			}
			tbl, err = cfg.Store.GetTable(ctx, tableName)
			if err != nil {
				status = "error"
				return nil, fmt.Errorf("describe_table: get table after refresh: %w", err)
			}
		}
		if tbl == nil {
			status = "error"
			return nil, fmt.Errorf("describe_table: table not found: %s", tableName)
		}

		rid, _ := ctx.Value(tool.ContextKeyRequestID).(string)
		invokedPayload := map[string]any{
			"tooName":      "describe_table",
			"tableName":    tableName,
			"datasourceID": datasourceID,
		}
		events.DefaultBus().Publish(ctx, events.Event{
			Kind:      events.ToolExecuted,
			RequestID: rid,
			Payload:   invokedPayload,
		})
		// 直接返回 metadata.Table，具备良好的 JSON 标签，便于模型消费。
		return *tbl, nil
	}
}
