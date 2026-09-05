package tooldata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/metadata"
	"github.com/sixath/framework/obs"
	"github.com/sixath/framework/tool"
)

// ExecuteReadConfig 构造 execute_read 工具所需依赖。
type ExecuteReadConfig struct {
	Reader              executor.Reader
	Exec                executor.Executor    // Deprecated: use Reader; 若 Reader 为空且 Exec 非空则经 executorAsReader 适配
	Registry            *datasource.Registry // 可选：用于将误传的 datasource_id "default" 解析为实际默认 id
	Store               *metadata.InMemoryStore
	DefaultDatasourceID string
	// DefaultTimeoutSec 默认超时时间（秒），0 表示无限制。
	DefaultTimeoutSec int
	// DefaultMaxRows 默认最大行数，0 表示无限制。
	DefaultMaxRows int
}

// RegisterExecuteReadTool 向注册表中注册 execute_read 工具。
// opts 可选：若 opts 中 Description 非空则覆盖默认描述（用于按数据源类型差异化表述）。
func RegisterExecuteReadTool(r *tool.Registry, cfg *ExecuteReadConfig, opts ...*tool.RegisterToolOptions) error {
	desc := "Execute a read-only DSL (e.g. SQL SELECT) on the current datasource and return rows. Prefer parameterized SQL with ? placeholders and positional_params for safety."
	if len(opts) > 0 && opts[0] != nil && opts[0].Description != "" {
		desc = opts[0].Description
	}
	return r.Register(tool.Tool{
		Name:        "execute_read",
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dsl": map[string]any{
					"type":        "string",
					"description": "Read-only DSL to execute (e.g. SQL SELECT). If omitted, `query` will be used.",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Alias of `dsl`.",
				},
				"datasource_id": map[string]any{
					"type":        "string",
					"description": "Datasource ID; if omitted, the session default is used.",
				},
				"timeout_sec": map[string]any{
					"type":        "integer",
					"description": "Execution timeout in seconds; non-negative.",
				},
				"max_rows": map[string]any{
					"type":        "integer",
					"description": "Maximum number of rows to return; non-negative.",
				},
				"index": map[string]any{
					"type":        "string",
					"description": "Optional. Elasticsearch: target index, comma-separated indices, or index pattern (e.g. vm-manager-*). Omit to search all indices.",
				},
				"positional_params": map[string]any{
					"type":        "array",
					"description": "Optional. Values for ? placeholders in SQL (safer than string interpolation).",
				},
				"named_params": map[string]any{
					"type":        "object",
					"description": "Optional. Map of :name placeholders to values (converted to ? for MySQL).",
				},
			},
			"required": []string{},
		},
		Execute: buildExecuteReadExecute(cfg),
	})
}

func buildExecuteReadExecute(cfg *ExecuteReadConfig) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		start := time.Now()
		status := "ok"
		defer func() {
			obs.ObserveDataQueryTool("execute_read", status, time.Since(start))
		}()

		if cfg == nil || cfg.Exec == nil {
			status = "error"
			return nil, errors.New("execute_read: not configured (missing executor)")
		}

		datasourceID := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
		if datasourceID == "" {
			status = "error"
			return nil, errors.New("execute_read: datasource_id is required (or set default)")
		}
		if err := RejectElasticsearchDatasource(cfg.Registry, datasourceID, "execute_read"); err != nil {
			status = "error"
			return nil, err
		}

		// dsl 或 query 至少需要一个
		var dsl string
		if v, ok := params["dsl"]; ok {
			if s, ok := v.(string); ok {
				dsl = s
			}
		}
		if dsl == "" {
			if v, ok := params["query"]; ok {
				if s, ok := v.(string); ok {
					dsl = s
				}
			}
		}
		if dsl == "" {
			status = "error"
			return nil, errors.New("execute_read: dsl (or query) is required and must be a string")
		}

		timeout := cfg.DefaultTimeoutSec
		if v, ok := params["timeout_sec"]; ok {
			if n, ok := tool.ToIntNonNegative(v); ok {
				timeout = n
			} else {
				status = "error"
				return nil, errors.New("execute_read: timeout_sec must be a non-negative number")
			}
		}

		maxRows := cfg.DefaultMaxRows
		if v, ok := params["max_rows"]; ok {
			if n, ok := tool.ToIntNonNegative(v); ok {
				maxRows = n
			} else {
				status = "error"
				return nil, errors.New("execute_read: max_rows must be a non-negative number")
			}
		}

		reader := executor.CoalesceReader(cfg.Reader, cfg.Exec)
		if reader == nil {
			status = "error"
			return nil, errors.New("execute_read: Reader not configured")
		}

		qo := executor.QueryOptions{
			Timeout: timeout,
			MaxRows: maxRows,
			Extras:  params,
			Params:  params,
		}
		if v, ok := params["positional_params"]; ok {
			qo.PositionalParams = sliceAny(v)
		}
		if v, ok := params["named_params"]; ok {
			if m, ok := v.(map[string]any); ok {
				qo.NamedParams = m
			}
		}
		res, err := reader.Query(ctx, datasourceID, dsl, qo)
		if err != nil {
			status = "error"
			return nil, fmt.Errorf("execute_read: %w", err)
		}
		rid, _ := ctx.Value(tool.ContextKeyRequestID).(string)
		invokedPayload := map[string]any{
			"tooName":      "execute_read",
			"dsl":          dsl,
			"datasourceID": datasourceID,
		}
		events.DefaultBus().Publish(ctx, events.Event{
			Kind:      events.ToolExecuted,
			RequestID: rid,
			Payload:   invokedPayload,
		})
		if res == nil {
			return res, nil
		}
		n := len(res.Rows)
		idx := ""
		if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
			idx = strings.TrimSpace(v)
		}
		res.HitStatus = tool.HitStatusFromCount(true, n)
		res.QueriedIndex = idx
		return res, nil
	}
}

func queryWithSchemaHeal(ctx context.Context, cfg *ExecuteReadConfig, reader executor.Reader, datasourceID, dsl string, qo executor.QueryOptions) (*executor.QueryResult, error) {
	res, err := reader.Query(ctx, datasourceID, dsl, qo)
	if err == nil {
		return res, nil
	}
	if !executor.IsSchemaRelated(err) {
		return nil, fmt.Errorf("execute_read: %w", err)
	}
	cur := dsl
	var notes []string
	schema := schemaForHeal(ctx, cfg, datasourceID)
	for attempt := 0; attempt < maxSQLHealAttempts; attempt++ {
		next, note, ok := HealReadSQL(cur, err, schema)
		if !ok {
			break
		}
		notes = append(notes, note)
		cur = next
		res, err = reader.Query(ctx, datasourceID, cur, qo)
		if err == nil {
			if res != nil {
				res.RepairedSQL = cur
				res.RepairNote = strings.Join(notes, "; ")
			}
			return res, nil
		}
		if !executor.IsSchemaRelated(err) {
			return nil, fmt.Errorf("execute_read: %w", err)
		}
		if schema == nil {
			schema = schemaForHeal(ctx, cfg, datasourceID)
		}
	}
	hint := SchemaHealHint(cur, err, schema)
	return nil, fmt.Errorf("execute_read: %w; %s", err, hint)
}

func schemaForHeal(ctx context.Context, cfg *ExecuteReadConfig, datasourceID string) *metadata.Schema {
	if cfg == nil || cfg.Store == nil {
		return nil
	}
	schema, err := metadata.EnsureSchemaForDatasource(ctx, cfg.Registry, cfg.Store, datasourceID)
	if err != nil {
		return nil
	}
	return schema
}

func queryResultRows(res *executor.QueryResult) []map[string]any {
	if res == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(res.Rows))
	for _, row := range res.Rows {
		m := make(map[string]any, len(res.Columns))
		for i, col := range res.Columns {
			if i < len(row) {
				m[col] = row[i]
			}
		}
		out = append(out, m)
	}
	return out
}

func sliceAny(v any) []any {
	if x, ok := v.([]any); ok {
		return x
	}
	return nil
}
