package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/sixath/framework/executor"
)

// ESLogConfig 为 es_log_query 的静态配置。
type ESLogConfig struct {
	DatasourceID string // 指向已注册的 ES datasource
	DefaultIndex string // 默认业务日志索引
	TraceIDField string // 日志中关联 trace 的字段名(如 trace_id)
}

const esLogDefaultLimit = 50

// RegisterESLogTool 注册 es_log_query 工具,复用只读 executor.Reader。
func RegisterESLogTool(reg *Registry, reader executor.Reader, cfg ESLogConfig) error {
	if reg == nil {
		return errors.New("es log tool: registry is nil")
	}
	if reader == nil {
		return errors.New("es log tool: reader is nil")
	}
	if cfg.DatasourceID == "" {
		return errors.New("es log tool: datasource id is empty")
	}
	if cfg.TraceIDField == "" {
		cfg.TraceIDField = "trace_id"
	}
	return reg.Register(Tool{
		Name:        "es_log_query",
		Description: "Query ELK application logs by trace_id (preferred) or keyword. Returns matching log lines. Read-only.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id": map[string]any{"type": "string", "description": "Correlate logs by trace id (matched on the configured trace id field)."},
				"query":    map[string]any{"type": "string", "description": "Keyword/full-text query when trace_id is not used."},
				"index":    map[string]any{"type": "string", "description": "Override the default log index/pattern."},
				"limit":    map[string]any{"type": "integer", "description": "Max hits (default 50)."},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "es_log_query"
			traceID, _ := params["trace_id"].(string)
			query, _ := params["query"].(string)
			if strings.TrimSpace(traceID) == "" && strings.TrimSpace(query) == "" {
				return rcaErr(toolName, "either trace_id or query is required", ErrorPermanent), nil
			}
			limit := intFromParam(params["limit"], esLogDefaultLimit)
			if limit <= 0 {
				limit = esLogDefaultLimit
			}
			index := cfg.DefaultIndex
			if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
				index = v
			}

			var inner map[string]any
			if strings.TrimSpace(traceID) != "" {
				inner = map[string]any{"term": map[string]any{cfg.TraceIDField: traceID}}
			} else {
				inner = map[string]any{"query_string": map[string]any{"query": query}}
			}
			dslObj := map[string]any{"size": limit, "query": inner}
			dslBytes, err := json.Marshal(dslObj)
			if err != nil {
				return rcaErr(toolName, err.Error(), ErrorPermanent), nil
			}

			res, err := reader.Query(ctx, cfg.DatasourceID, string(dslBytes), executor.QueryOptions{
				MaxRows: limit,
				Extras:  map[string]any{"index": index},
			})
			if err != nil {
				return rcaErrFrom(toolName, err), nil
			}
			truncated := false
			if res != nil {
				truncated = res.Truncated
			}
			payload := map[string]any{
				"hits":      rowsToHits(res),
				"total":     totalFromResult(res),
				"truncated": truncated,
			}
			if tid := strings.TrimSpace(traceID); tid != "" {
				payload["trace_id"] = tid
			}
			return rcaOK(toolName, payload), nil
		},
	})
}

// rowsToHits 把列式 QueryResult 转成 [{col:val}] 便于模型阅读。
func rowsToHits(res *executor.QueryResult) []map[string]any {
	hits := []map[string]any{}
	if res == nil {
		return hits
	}
	for _, row := range res.Rows {
		h := make(map[string]any, len(res.Columns))
		for i, col := range res.Columns {
			if i < len(row) {
				h[col] = row[i]
			}
		}
		hits = append(hits, h)
	}
	return hits
}

func totalFromResult(res *executor.QueryResult) int {
	if res == nil {
		return 0
	}
	if res.EstimatedTotal > 0 {
		return int(res.EstimatedTotal)
	}
	return len(res.Rows)
}
