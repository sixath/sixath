package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/framework/executor"
)

// ESLogCluster 是 es_log_query 可路由到的一个 ES 集群。
type ESLogCluster struct {
	ID           string
	DefaultIndex string
	TraceIDField string
	Purpose      string
	BodyField    string // optional log body column; empty → infer from mapping
	TimeField    string // time_from/time_to/sort/agg_interval 的默认时间字段；空则 @timestamp
	// IndexPriority 索引候选中优先列出的模式（按顺序），如 ["app-prod-*"]；由部署方配置，不在代码写死。
	IndexPriority []string
	// IndexSuggestLimit 索引不存在时返回的候选数量；<=0 用默认 30。
	IndexSuggestLimit int
}

// ESLogConfig 为 es_log_query 的静态配置。
type ESLogConfig struct {
	DatasourceID string         // 单集群简写：指向已注册的 ES datasource
	DefaultIndex string         // 单集群简写：默认业务日志索引
	TraceIDField string         // 单集群简写：日志中关联 trace 的字段名(如 trace_id)
	BodyField    string         // 单集群简写：日志正文列
	FieldMapper  ESFieldMapper  // 空击时查 mapping；nil 则按本次 cluster 从 Reader 推断
	IndexCatalog ESIndexCatalog // 物理索引探测；nil 则按 Reader 推断，测中可注入
	Clusters     []ESLogCluster
}

func (cfg ESLogConfig) resolvedClusters() []ESLogCluster {
	if len(cfg.Clusters) > 0 {
		out := append([]ESLogCluster(nil), cfg.Clusters...)
		for i := range out {
			out[i].ID = strings.TrimSpace(out[i].ID)
			if out[i].TraceIDField == "" {
				out[i].TraceIDField = "trace_id"
			}
		}
		return out
	}
	id := strings.TrimSpace(cfg.DatasourceID)
	if id == "" {
		return nil
	}
	tf := cfg.TraceIDField
	if tf == "" {
		tf = "trace_id"
	}
	return []ESLogCluster{{ID: id, DefaultIndex: cfg.DefaultIndex, TraceIDField: tf, BodyField: cfg.BodyField}}
}

func lookupCluster(clusters []ESLogCluster, id string) (ESLogCluster, bool) {
	for _, c := range clusters {
		if c.ID == id {
			return c, true
		}
	}
	return ESLogCluster{}, false
}

func formatESLogClusters(clusters []ESLogCluster) string {
	parts := make([]string, 0, len(clusters))
	for _, c := range clusters {
		parts = append(parts, fmt.Sprintf("%s / %s / %s", c.ID, c.DefaultIndex, c.Purpose))
	}
	return strings.Join(parts, "; ")
}

func clusterParamError(id string, clusters []ESLogCluster) string {
	listed := formatESLogClusters(clusters)
	if id == "" {
		return "cluster is required; known clusters: " + listed
	}
	return fmt.Sprintf("unknown cluster %q; known clusters: %s", id, listed)
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
	clusters := cfg.resolvedClusters()
	if len(clusters) == 0 {
		return errors.New("es log tool: no clusters configured")
	}
	for _, c := range clusters {
		if c.ID == "" {
			return errors.New("es log tool: cluster id is empty")
		}
	}
	if cfg.IndexCatalog == nil {
		cfg.IndexCatalog = catalogFromReader(reader)
	}
	clusterIDs := make([]string, len(clusters))
	desc := "Query ELK application logs (read-only). Prefer trace_id. query is Lucene query_string or a JSON ES query clause / search body. Index must be a real index or pattern on this cluster — do not invent names from service names. Omit index to use the cluster default_index; if that is also empty the call fails and lists discovered patterns. Prefer an unfielded query or a field that exists in the index mapping; do not assume identifiers like vmid or flow_id are mapped fields. Fields referenced in query/sort/agg_field/fields/time_field are checked against the index mapping before the search; unknown fields are rejected with candidate names. hit_status=empty means the index and fields were valid but no documents matched; suspect = 0 hits but relaxed probes found data (see diagnosis); fix the condition before concluding there is no data. Page large totals with from (use next_from from the previous result). Per-call limit max 500. term/match may be rewritten once to the clause that type supports (term on .keyword for text+keyword; match_phrase for text-only). Large pages are written to workspace tmp/results/*.jsonl; use result_stats on path instead of read_file. Complex transforms: run_result_script (not read_file). Investigation primitives: sort (asc|desc on the time field, or field:asc) to find the earliest/latest event; time_from/time_to (ISO time or date math like now-2h) to compare windows; agg_field (a keyword field, e.g. level or host.keyword) for top values with counts; agg_interval (e.g. 1m, 1h) for a per-interval count timeline; fields to return only the listed columns. Aggregations come back in aggregations.{by_field,timeline}.buckets and work across all matches, not only the returned page."
	for i, c := range clusters {
		clusterIDs[i] = c.ID
		var line string
		if strings.TrimSpace(c.DefaultIndex) == "" {
			line = fmt.Sprintf("\n`%s` — %s; no default index: index is REQUIRED", c.ID, c.Purpose)
			if len(c.IndexPriority) > 0 {
				line += fmt.Sprintf(" (known patterns: %s)", strings.Join(c.IndexPriority, ", "))
			}
		} else {
			line = fmt.Sprintf("\n`%s` — %s; default index `%s`", c.ID, c.Purpose, c.DefaultIndex)
		}
		if strings.TrimSpace(c.BodyField) != "" {
			line += fmt.Sprintf("; body field `%s`", c.BodyField)
		}
		desc += line
	}

	countESLog := func(ctx context.Context, params map[string]any, cl ESLogCluster, index string) (int64, error) {
		dsl, _, _, err := buildESLogDSL(params, cl, esLogTraceField(cl), 0, 0)
		if err != nil {
			return 0, err
		}
		dsl["size"] = 0
		delete(dsl, "aggs")
		delete(dsl, "aggregations")
		delete(dsl, "sort")
		dsl["track_total_hits"] = true
		b, err := json.Marshal(dsl)
		if err != nil {
			return 0, err
		}
		res, err := reader.Query(ctx, cl.ID, string(b), executor.QueryOptions{Extras: map[string]any{"index": index}})
		if err != nil {
			return 0, err
		}
		return int64(totalFromResult(res)), nil
	}

	mappers := newESMapperCache(cfg.FieldMapper, func(cluster string) ESFieldMapper {
		return mapperFromReader(reader, cluster)
	}, esMappingCacheTTL)

	resolveIndex := func(params map[string]any) (ESLogCluster, string, bool) {
		cl, ok := lookupCluster(clusters, trimmedStringParam(params, "cluster"))
		if !ok {
			return cl, "", false
		}
		index := cl.DefaultIndex
		if v := trimmedStringParam(params, "index"); v != "" {
			index = v
		}
		return cl, index, strings.TrimSpace(index) != ""
	}

	fieldCheck := FieldRefs{
		Label:   "es_fields",
		Extract: esLogFieldRefs,
		Fields: func(ctx context.Context, params map[string]any, refresh bool) ([]string, error) {
			cl, index, ok := resolveIndex(params)
			if !ok {
				return nil, nil
			}
			m := mappers.For(cl.ID)
			if m == nil {
				return nil, nil
			}
			if refresh {
				return m.Refresh(ctx, index), nil
			}
			return m.ListFields(ctx, index), nil
		},
		Known: func(field string, catalog []string) bool {
			return len(unknownQueryFields([]string{field}, catalog)) == 0
		},
		Similar: suggestSimilarMappedFields,
		BodyHint: func(params map[string]any) string {
			cl, _, _ := resolveIndex(params)
			body := strings.TrimSpace(cl.BodyField)
			if body == "" {
				return "for free-text search drop the field prefix (plain words search the log body)"
			}
			return fmt.Sprintf("for free-text search drop the field prefix or use %s:<text>", body)
		},
	}

	probe := &EmptyProbe{
		Relax: esLogRelax,
		Count: func(ctx context.Context, v ProbeVariant) (int64, error) {
			cl, index, ok := resolveIndex(v.Params)
			if !ok {
				return 0, errors.New("no index")
			}
			n, err := countESLog(ctx, v.Params, cl, index)
			// 原查询若靠 quoteLuceneSpecialTokens 重试才解析成功，变体继承了同样的裸特殊字符，按同一规则改写后再计数。
			if err != nil && isESQueryParseError(err) && trimmedStringParam(v.Params, "trace_id") == "" {
				if q, _ := v.Params["query"].(string); !strings.HasPrefix(strings.TrimSpace(q), "{") {
					if fixed, changed := quoteLuceneSpecialTokens(q); changed {
						p := make(map[string]any, len(v.Params))
						for k, val := range v.Params {
							p[k] = val
						}
						p["query"] = fixed
						return countESLog(ctx, p, cl, index)
					}
				}
			}
			return n, err
		},
	}

	return reg.Register(Tool{
		ArgChecks:   []ArgCheck{fieldCheck},
		EmptyProbe:  probe,
		Name:        "es_log_query",
		Description: desc,
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cluster": map[string]any{
					"type":        "string",
					"description": "ES cluster / datasource id to query. Required; do not omit or invent names.",
				},
				"trace_id":     map[string]any{"type": "string", "description": "Correlate logs by trace id (matched on the configured trace id field)."},
				"query":        map[string]any{"type": "string", "description": "Lucene query_string (unfielded value or a mapped field:value) or JSON query clause / search body when trace_id is not used."},
				"index":        map[string]any{"type": "string", "description": "Override the default log index/pattern. Must exist on the cluster; do not invent names."},
				"limit":        map[string]any{"type": "integer", "description": "Max hits per page (default 50, max 500)."},
				"from":         map[string]any{"type": "integer", "description": "Offset for pagination (default 0). Use next_from from the previous page when truncated."},
				"sort":         map[string]any{"type": "string", "description": "asc or desc on the time field (asc = earliest first), or <field>:<asc|desc>."},
				"time_from":    map[string]any{"type": "string", "description": "Inclusive lower bound on the time field (ISO 8601 or ES date math such as now-6h)."},
				"time_to":      map[string]any{"type": "string", "description": "Inclusive upper bound on the time field."},
				"time_field":   map[string]any{"type": "string", "description": "Time field for sort/time range/agg_interval (default: cluster time field or @timestamp)."},
				"agg_field":    map[string]any{"type": "string", "description": "Keyword field to count top values over all matches (terms aggregation)."},
				"agg_size":     map[string]any{"type": "integer", "description": "Number of top values for agg_field (default 20, max 200)."},
				"agg_interval": map[string]any{"type": "string", "description": "Bucket size for a count-over-time timeline, e.g. 1m, 5m, 1h."},
				"fields": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Only return these source fields (reduces noise).",
				},
			},
			// 不声明 required=["cluster"]：缺 cluster / 未知 cluster 由 execute 内的
			// clusterParamError 统一报错（含已知集群列表，对模型更友好），静态 required+enum
			// 会抢在前面拦截并丢失该上下文。
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "es_log_query"
			clusterID, _ := params["cluster"].(string)
			clusterID = strings.TrimSpace(clusterID)
			stampFail := func(out map[string]any, idx, cid string) (any, error) {
				if out == nil {
					out = map[string]any{}
				}
				out["cluster"] = cid
				return StampHitContract(out, HitStamp{
					Status: HitStatusError, QueriedIndex: idx, Tool: toolName, Ctx: ctx,
				}), nil
			}
			cl, ok := lookupCluster(clusters, clusterID)
			if clusterID == "" || !ok {
				return stampFail(rcaErr(toolName, clusterParamError(clusterID, clusters), ErrorPermanent), "", clusterID)
			}

			index := cl.DefaultIndex
			if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
				index = v
			}
			if strings.TrimSpace(index) == "" {
				out := rcaErr(toolName, "index is required when cluster default_index is empty", ErrorPermanent)
				attachSuggestedPatterns(ctx, out, cfg.IndexCatalog, cl, "")
				return stampFail(out, "", cl.ID)
			}
			if unresolved := indexUnresolvedPayload(ctx, cfg.IndexCatalog, cl, index); unresolved != nil {
				return stampFail(unresolved, index, cl.ID)
			}

			traceID, _ := params["trace_id"].(string)
			query, _ := params["query"].(string)
			if strings.TrimSpace(traceID) == "" && strings.TrimSpace(query) == "" {
				return stampFail(rcaErr(toolName, "either trace_id or query is required", ErrorPermanent), index, cl.ID)
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

			traceField := esLogTraceField(cl)
			mapper := ESFieldMapper(nil)
			if m := mappers.For(cl.ID); m != nil {
				mapper = m
			}

			dslObj, qopts, isBody, buildErr := buildESLogDSL(params, cl, traceField, limit, from)
			if buildErr != nil {
				return stampFail(rcaErr(toolName, buildErr.Error(), ErrorPermanent), index, cl.ID)
			}
			dslBytes, err := json.Marshal(dslObj)
			if err != nil {
				return stampFail(rcaErr(toolName, err.Error(), ErrorPermanent), index, cl.ID)
			}

			res, err := reader.Query(ctx, cl.ID, string(dslBytes), executor.QueryOptions{
				MaxRows: limit,
				Extras:  map[string]any{"index": index},
			})
			var parseRetryQuery string
			if err != nil && !isBody && strings.TrimSpace(traceID) == "" && isESQueryParseError(err) {
				if fixed, changed := quoteLuceneSpecialTokens(query); changed {
					retryDSL := make(map[string]any, len(dslObj))
					for k, v := range dslObj {
						retryDSL[k] = v
					}
					retryDSL["query"] = map[string]any{"query_string": map[string]any{"query": fixed}}
					qopts.apply(retryDSL)
					if retryBytes, mErr := json.Marshal(retryDSL); mErr == nil {
						retryRes, qErr := reader.Query(ctx, cl.ID, string(retryBytes), executor.QueryOptions{
							MaxRows: limit,
							Extras:  map[string]any{"index": index},
						})
						if qErr == nil {
							res, err = retryRes, nil
							dslObj = retryDSL
							parseRetryQuery = fixed
						}
					}
				}
			}
			if err != nil {
				return stampFail(rcaErrFrom(toolName, err), index, cl.ID)
			}
			var (
				origQuery      any
				rewrittenQuery any
				fieldHints     []ESFieldHint
				queryRewritten bool
			)
			if totalFromResult(res) == 0 && from == 0 && mapper != nil {
				fields := lookupQueryFields(ctx, mapper, index, dslObj)
				rewritten, changed, hints := rewriteEmptyHitQuery(dslObj, fields)
				fieldHints = hints
				if changed {
					origQuery = dslObj["query"]
					rewrittenQuery = rewritten["query"]
					queryRewritten = true
					retryBytes, mErr := json.Marshal(rewritten)
					if mErr != nil {
						return stampFail(rcaErr(toolName, mErr.Error(), ErrorPermanent), index, cl.ID)
					}
					retryRes, qErr := reader.Query(ctx, cl.ID, string(retryBytes), executor.QueryOptions{
						MaxRows: limit,
						Extras:  map[string]any{"index": index},
					})
					if qErr != nil {
						return stampFail(rcaErrFrom(toolName, qErr), index, cl.ID)
					}
					res = retryRes
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
				"cluster":   cl.ID,
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
			if res != nil {
				if aggs := summarizeESAggregations(res.Aggregations); len(aggs) > 0 {
					payload["aggregations"] = aggs
				}
			}
			if tid := strings.TrimSpace(traceID); tid != "" {
				payload["trace_id"] = tid
			}
			if queryRewritten {
				payload["query_rewritten"] = true
				payload["original_query"] = origQuery
				payload["rewritten_query"] = rewrittenQuery
			}
			if parseRetryQuery != "" {
				payload["query_rewritten"] = true
				payload["original_query"] = query
				payload["rewritten_query"] = parseRetryQuery
				payload["rewrite_reason"] = "query_string parse error; special characters in bare tokens were quoted as phrases"
			}
			if total == 0 && !isBody {
				if hint := queryStringRangeHint(query, cl.TimeField); hint != "" {
					payload["time_range_hint"] = hint
				}
			}
			if len(fieldHints) > 0 {
				payload["field_hints"] = fieldHints
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
			return rcaOK(toolName, payload), nil
		},
	})
}

func esLogTraceField(cl ESLogCluster) string {
	if cl.TraceIDField != "" {
		return cl.TraceIDField
	}
	return "trace_id"
}

var (
	luceneQuoted = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	// Lucene 正则字面量只能出现在词首（行首、空白、左括号或 field: 之后）；内容非空，因此 http:// 里的 // 不算。
	luceneRegexLiteral = regexp.MustCompile(`(^|[\s(:])/(?:[^/\\\n]|\\.)+/`)
	// 字段名前只能是行首、空白或左括号（可再跟一个 !+- 运算符），所以 url:http://x、msg:pre-start 中值里的片段不会被当成字段。
	luceneFieldRef = regexp.MustCompile(`(?:^|[\s(])[!+\-]?([A-Za-z@][\w.@-]*)\s*:`)
)

// luceneQueryFieldNames 抽取 query_string 中的 field: 前缀；引号短语与 /regex/ 字面量先抹掉，
// 冒号后紧跟 // 的（url: http://x 里的 http）视为 URL 值而非字段。
func luceneQueryFieldNames(q string) []string {
	s := luceneQuoted.ReplaceAllString(q, `""`)
	s = luceneRegexLiteral.ReplaceAllString(s, `${1}""`)
	var out []string
	for _, m := range luceneFieldRef.FindAllStringSubmatchIndex(s, -1) {
		if strings.HasPrefix(s[m[1]:], "//") {
			continue
		}
		out = append(out, s[m[2]:m[3]])
	}
	return out
}

// esLogFieldRefs 抽取参数中显式引用的字段：query_string 的 field: 前缀、agg_field、fields、
// sort 字段、显式 time_field。trace_id 查询只跳过 query（query 被忽略），其余参数照常校验；
// JSON body 查询不抽取；含通配符或以 _ 开头的元字段（_id/_score/_exists_）不校验。
func esLogFieldRefs(params map[string]any) []FieldRef {
	var refs []FieldRef
	add := func(param, field string) {
		field = strings.TrimSpace(field)
		if field == "" || strings.HasPrefix(field, "_") || strings.Contains(field, "*") {
			return
		}
		refs = append(refs, FieldRef{Param: param, Field: baseFieldName(field)})
	}
	if q := trimmedStringParam(params, "query"); q != "" && !strings.HasPrefix(q, "{") && trimmedStringParam(params, "trace_id") == "" {
		for _, f := range luceneQueryFieldNames(q) {
			add("query", f)
		}
	}
	add("agg_field", trimmedStringParam(params, "agg_field"))
	add("time_field", trimmedStringParam(params, "time_field"))
	if s := trimmedStringParam(params, "sort"); s != "" {
		if i := strings.LastIndex(s, ":"); i > 0 {
			add("sort", s[:i])
		} else if o := strings.ToLower(s); o != "asc" && o != "desc" {
			add("sort", s)
		}
	}
	if o, err := parseESLogQueryOpts(map[string]any{"fields": params["fields"]}, ""); err == nil {
		for _, f := range o.fields {
			add("fields", f)
		}
	}
	return refs
}

// esLogRelax 生成放宽变体（有序）：只保留时间窗、逐个去掉顶层 AND 子句、时间窗放宽到 24h。
// 顺序即优先级：探测最多跑 emptyProbeMaxVariants(3) 个，子句较多时排在最后的 time_window_24h
// 常被截掉（Diagnosis.Truncated=true）。子句切分只认大写 AND（splitTopLevelAND），
// && 与小写 and 不切分，整串作为一个子句。
func esLogRelax(params map[string]any) []ProbeVariant {
	if trimmedStringParam(params, "trace_id") != "" {
		return nil
	}
	q := trimmedStringParam(params, "query")
	if strings.HasPrefix(q, "{") {
		return nil
	}
	hasWindow := trimmedStringParam(params, "time_from") != "" || trimmedStringParam(params, "time_to") != ""
	with := func(mut func(p map[string]any)) map[string]any {
		p := make(map[string]any, len(params))
		for k, v := range params {
			p[k] = v
		}
		mut(p)
		return p
	}
	var out []ProbeVariant
	if hasWindow && q != "" && q != "*" {
		out = append(out, ProbeVariant{Label: "time_window_only", Params: with(func(p map[string]any) { p["query"] = "*" })})
	}
	if clauses := splitTopLevelAND(q); len(clauses) > 1 {
		for i, c := range clauses {
			rest := append(append([]string{}, clauses[:i]...), clauses[i+1:]...)
			out = append(out, ProbeVariant{Label: "without:" + c, Params: with(func(p map[string]any) { p["query"] = strings.Join(rest, " AND ") })})
		}
	}
	if relativeWindowUnder24h(trimmedStringParam(params, "time_from")) {
		out = append(out, ProbeVariant{Label: "time_window_24h", Params: with(func(p map[string]any) {
			p["time_from"] = "now-24h"
			delete(p, "time_to")
		})})
	}
	return out
}

var relativeNow = regexp.MustCompile(`^now-(\d+)([smh])$`)

// relativeWindowUnder24h 仅对 now-<N><s|m|h> 且短于 24h 的起点返回 true；绝对时间或更长窗口不放宽，避免反而收窄/平移窗口。
func relativeWindowUnder24h(from string) bool {
	m := relativeNow.FindStringSubmatch(strings.TrimSpace(from))
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour}[m[2]]
	return time.Duration(n)*unit < 24*time.Hour
}

// splitTopLevelAND 按顶层 AND（大写，括号与引号外）切分 query_string；不识别 && 与小写 and。
func splitTopLevelAND(q string) []string {
	var parts []string
	depth, inQuote, start := 0, false, 0
	for i := 0; i < len(q); i++ {
		switch c := q[i]; {
		case c == '"' && (i == 0 || q[i-1] != '\\'):
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(q[i:], " AND "):
			parts = append(parts, strings.TrimSpace(q[start:i]))
			start = i + len(" AND ")
			i += len(" AND ") - 1
		}
	}
	parts = append(parts, strings.TrimSpace(q[start:]))
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// buildESLogDSL 按参数构造查询 DSL；size/from 由调用方传入。isBody 表示 query 是带 "query" 键的完整 search body。
func buildESLogDSL(params map[string]any, cl ESLogCluster, traceField string, size, from int) (dsl map[string]any, qopts esLogQueryOpts, isBody bool, err error) {
	traceID, _ := params["trace_id"].(string)
	query, _ := params["query"].(string)
	var inner, body map[string]any
	if strings.TrimSpace(traceID) != "" {
		inner = map[string]any{"term": map[string]any{traceField: traceID}}
	} else if inner, body, err = parseESLogQuery(query); err != nil {
		return nil, qopts, false, err
	}
	if body != nil {
		dsl = body
		if _, ok := dsl["size"]; !ok {
			dsl["size"] = size
		}
	} else {
		dsl = map[string]any{"size": size, "query": inner}
	}
	if from > 0 {
		dsl["from"] = from
	}
	if qopts, err = parseESLogQueryOpts(params, cl.TimeField); err != nil {
		return nil, qopts, false, err
	}
	qopts.apply(dsl)
	return dsl, qopts, body != nil, nil
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
