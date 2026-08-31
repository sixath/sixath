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
	DatasourceID string        // 指向已注册的 ES datasource
	DefaultIndex string        // 默认业务日志索引
	TraceIDField string        // 日志中关联 trace 的字段名(如 trace_id)
	FieldMapper  ESFieldMapper // 空击时查 mapping；nil 则尝试从 Reader 推断
}

const esLogDefaultLimit = 50
const esLogMaxLimit = 500

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
	if cfg.FieldMapper == nil {
		cfg.FieldMapper = mapperFromReader(reader, cfg.DatasourceID)
	}
	return reg.Register(Tool{
		Name:        "es_log_query",
		Description: "Query ELK application logs (read-only). Prefer trace_id. query is Lucene query_string (field:value, AND/OR) or a JSON ES query clause / search body. Page large totals with from (use next_from from the previous result). Per-call limit max 500. On 0 hits, looks up field mapping: unknown fields are reported (do not invent names); term/match may be rewritten once to the clause that type supports (term on .keyword for text+keyword; match_phrase for text-only). Large pages are written to workspace tmp/results/*.jsonl; use result_stats on path instead of read_file. Complex transforms: run_result_script (not read_file).",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id": map[string]any{"type": "string", "description": "Correlate logs by trace id (matched on the configured trace id field)."},
				"query":    map[string]any{"type": "string", "description": "Lucene query_string (e.g. operation:DiscardUserArchive) or JSON query clause / search body when trace_id is not used."},
				"index":    map[string]any{"type": "string", "description": "Override the default log index/pattern."},
				"limit":    map[string]any{"type": "integer", "description": "Max hits per page (default 50, max 500)."},
				"from":     map[string]any{"type": "integer", "description": "Offset for pagination (default 0). Use next_from from the previous page when truncated."},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "es_log_query"
			traceID, _ := params["trace_id"].(string)
			query, _ := params["query"].(string)
			index := cfg.DefaultIndex
			if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
				index = v
			}
			if strings.TrimSpace(traceID) == "" && strings.TrimSpace(query) == "" {
				return StampHitContract(rcaErr(toolName, "either trace_id or query is required", ErrorPermanent), HitStamp{
					Status: HitStatusError, QueriedIndex: index, Tool: toolName, Ctx: ctx,
				}), nil
			}
			limit := intFromParam(params["limit"], esLogDefaultLimit)
			if limit <= 0 {
				limit = esLogDefaultLimit
			}
			if limit > esLogMaxLimit {
				limit = esLogMaxLimit
			}
			from := intFromParam(params["from"], 0)
			if from < 0 {
				from = 0
			}

			var inner map[string]any
			var body map[string]any
			if strings.TrimSpace(traceID) != "" {
				inner = map[string]any{"term": map[string]any{cfg.TraceIDField: traceID}}
			} else {
				var buildErr error
				inner, body, buildErr = parseESLogQuery(query)
				if buildErr != nil {
					return StampHitContract(rcaErr(toolName, buildErr.Error(), ErrorPermanent), HitStamp{
						Status: HitStatusError, QueriedIndex: index, Tool: toolName, Ctx: ctx,
					}), nil
				}
			}
			var dslObj map[string]any
			if body != nil {
				dslObj = body
				if _, ok := dslObj["size"]; !ok {
					dslObj["size"] = limit
				}
			} else {
				dslObj = map[string]any{"size": limit, "query": inner}
			}
			if from > 0 {
				dslObj["from"] = from
			}
			dslBytes, err := json.Marshal(dslObj)
			if err != nil {
				return StampHitContract(rcaErr(toolName, err.Error(), ErrorPermanent), HitStamp{
					Status: HitStatusError, QueriedIndex: index, Tool: toolName, Ctx: ctx,
				}), nil
			}

			res, err := reader.Query(ctx, cfg.DatasourceID, string(dslBytes), executor.QueryOptions{
				MaxRows: limit,
				Extras:  map[string]any{"index": index},
			})
			if err != nil {
				out := rcaErrFrom(toolName, err)
				return StampHitContract(out, HitStamp{Status: HitStatusError, QueriedIndex: index, Tool: toolName, Ctx: ctx}), nil
			}
			var (
				origQuery      any
				rewrittenQuery any
				fieldHints     []ESFieldHint
				queryRewritten bool
				unknownFields  []string
				similarFields  []string
			)
			if totalFromResult(res) == 0 && from == 0 && cfg.FieldMapper != nil {
				names := collectQueryFieldNames(dslObj)
				catalog := cfg.FieldMapper.ListFields(ctx, index)
				unknownFields = unknownQueryFields(names, catalog)
				seenSimilar := map[string]struct{}{}
				for _, u := range unknownFields {
					for _, s := range suggestSimilarMappedFields(u, catalog) {
						if _, ok := seenSimilar[s]; ok {
							continue
						}
						seenSimilar[s] = struct{}{}
						similarFields = append(similarFields, s)
					}
				}
				fields := lookupQueryFields(ctx, cfg.FieldMapper, index, dslObj)
				rewritten, changed, hints := rewriteEmptyHitQuery(dslObj, fields)
				fieldHints = hints
				if changed && len(unknownFields) == 0 {
					origQuery = dslObj["query"]
					rewrittenQuery = rewritten["query"]
					retryBytes, mErr := json.Marshal(rewritten)
					if mErr != nil {
						return StampHitContract(rcaErr(toolName, mErr.Error(), ErrorPermanent), HitStamp{
							Status: HitStatusError, QueriedIndex: index, Tool: toolName, Ctx: ctx,
						}), nil
					}
					retryRes, qErr := reader.Query(ctx, cfg.DatasourceID, string(retryBytes), executor.QueryOptions{
						MaxRows: limit,
						Extras:  map[string]any{"index": index},
					})
					if qErr != nil {
						out := rcaErrFrom(toolName, qErr)
						return StampHitContract(out, HitStamp{Status: HitStatusError, QueriedIndex: index, Tool: toolName, Ctx: ctx}), nil
					}
					res = retryRes
					queryRewritten = true
				}
			}
			truncated := false
			if res != nil {
				truncated = res.Truncated
			}
			hits := compactESLogHits(rowsToHits(res))
			total := totalFromResult(res)
			returned := len(hits)
			payload := map[string]any{
				"hits":      hits,
				"total":     total,
				"count":     total,
				"from":      from,
				"returned":  returned,
				"truncated": truncated,
			}
			if truncated {
				payload["has_more"] = true
			}
			nextFrom := from + returned
			if truncated && (total <= 0 || nextFrom < total) {
				payload["next_from"] = nextFrom
				payload["continue_from"] = nextFrom
			}
			if ids := extractIDsFromHits(hits); len(ids) > 0 {
				payload["extracted_ids"] = ids
			}
			if res != nil && res.Columns != nil {
				payload["columns"] = res.Columns
			}
			if tid := strings.TrimSpace(traceID); tid != "" {
				payload["trace_id"] = tid
			}
			if queryRewritten {
				payload["query_rewritten"] = true
				payload["original_query"] = origQuery
				payload["rewritten_query"] = rewrittenQuery
			}
			if len(fieldHints) > 0 {
				payload["field_hints"] = fieldHints
			}
			if len(unknownFields) > 0 {
				payload["unknown_fields"] = unknownFields
				if len(similarFields) > 0 {
					payload["similar_fields"] = similarFields
				}
				payload["mapping_error"] = unknownFieldsNote(unknownFields)
			}
			n := len(hits)
			if t := total; t > n {
				n = t
			}
			payload = StampHitContract(payload, HitStamp{
				Status:       HitStatusFromCount(true, n),
				QueriedIndex: index,
				Tool:         toolName,
				Ctx:          ctx,
			})
			refs := deriveESLogRefs(payload) // must still contain hits
			stub, fallback := MaybeSpill(ctx, toolName, hits, payload, refs)
			if stub != nil {
				return stub, nil
			}
			payload = fallback
			return rcaOK(toolName, payload), nil
		},
	})
}

// parseESLogQuery turns query into an ES query clause, or a full search body when
// the JSON object already has a "query" key. Plain text uses query_string (Lucene field:value).
func parseESLogQuery(query string) (inner map[string]any, body map[string]any, err error) {
	q := strings.TrimSpace(query)
	if strings.HasPrefix(q, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(q), &obj); err != nil {
			return nil, nil, err
		}
		if _, ok := obj["query"]; ok {
			return nil, obj, nil
		}
		return obj, nil, nil
	}
	return map[string]any{"query_string": map[string]any{"query": query}}, nil, nil
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
