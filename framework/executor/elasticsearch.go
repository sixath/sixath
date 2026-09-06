package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sixath/framework/datasource"
)

// ESExecutor 基于 datasource.Registry 的 Elasticsearch 执行器，支持只读 Search、超时与 MaxRows。
type ESExecutor struct {
	Registry *datasource.Registry
	Logger   *slog.Logger
}

// NewESExecutor 创建依赖给定 Registry 的 ES 执行器。
func NewESExecutor(reg *datasource.Registry) *ESExecutor {
	return &ESExecutor{Registry: reg}
}

// Query 实现 Reader。
func (e *ESExecutor) Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error) {
	res, err := e.Execute(ctx, datasourceID, dsl, ExecuteOptions{
		Timeout: opts.Timeout,
		MaxRows: opts.MaxRows,
		Params:  opts.Params,
		Extras:  opts.Extras,
	})
	return queryResultFromResult(res), err
}

// Execute 实现 Executor。仅支持只读：dsl 为 Search 请求体 JSON；写操作（index/update/delete 等）在 ReadOnly 时拒绝。
func (e *ESExecutor) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
	ctx, rec := beginOp(ctx, e.Registry, datasourceID, "search", opts.MaxRows, opts.AllowsWrite())
	ds, err := e.Registry.Get(datasourceID)
	if err != nil {
		e.finishES(ctx, rec, datasourceID, "search", dsl, nil, err)
		return nil, err
	}
	ep, ok := ds.(datasource.ESHTTPProvider)
	if !ok {
		err := ErrUnsupportedDataSource
		e.finishES(ctx, rec, datasourceID, "search", dsl, nil, err)
		return nil, err
	}
	client := ep.ESHTTP()

	if !opts.AllowsWrite() && isESWriteDSL(dsl) {
		e.finishES(ctx, rec, datasourceID, "readonly_reject", dsl, nil, ErrReadOnlyViolation)
		return nil, ErrReadOnlyViolation
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Second)
		defer cancel()
	}

	indexParam := ""
	if extras := opts.QueryExtras(); extras != nil {
		if v, ok := extras["index"].(string); ok && v != "" {
			indexParam = strings.TrimSpace(v)
		}
	}
	res, err := e.execSearch(ctx, client, dsl, opts.MaxRows, indexParam)
	e.finishES(ctx, rec, datasourceID, "search", dsl, res, err)
	return res, err
}

func (e *ESExecutor) finishES(ctx context.Context, rec *opRecorder, datasourceID, op, dsl string, res *Result, err error) {
	rec.finish(res, err)
	e.logES(ctx, datasourceID, op, dsl, rec.start, res, err)
}

func (e *ESExecutor) logES(ctx context.Context, datasourceID, op, dsl string, start time.Time, res *Result, err error) {
	logger := e.Logger
	if logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("datasource", datasourceID),
		slog.String("op", op),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	}
	if res != nil {
		attrs = append(attrs, slog.Int("rows", len(res.Rows)), slog.Bool("truncated", res.Truncated))
	}
	status := "ok"
	if err != nil {
		status = "error"
		attrs = append(attrs, slog.Any("error", err))
	} else if op == "readonly_reject" {
		status = "rejected"
	}
	attrs = append(attrs, slog.String("status", status))
	logger.LogAttrs(ctx, slog.LevelInfo, "executor.elasticsearch", attrs...)
	logger.LogAttrs(ctx, slog.LevelDebug, "executor.elasticsearch.dsl", slog.String("dsl", dsl))
}

// isESWriteDSL 根据请求体粗略判断是否为写操作（index/update/delete/bulk 等）。
func isESWriteDSL(dsl string) bool {
	s := strings.TrimSpace(strings.ToLower(dsl))
	if s == "" {
		return false
	}
	// 简单检查：包含 "index"/"update"/"delete"/"bulk" 等且非纯 query 结构
	if strings.Contains(s, `"index"`) || strings.Contains(s, `"update"`) ||
		strings.Contains(s, `"delete"`) || strings.Contains(s, `"bulk"`) {
		return true
	}
	return false
}

func esSearchPath(index string) string {
	var indices []string
	for _, s := range strings.Split(index, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			indices = append(indices, s)
		}
	}
	if len(indices) == 0 {
		return "/_search"
	}
	return "/" + strings.Join(indices, ",") + "/_search"
}

func parseESHitsTotal(raw json.RawMessage) int64 {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	if raw[0] == '{' {
		var obj struct {
			Value int64 `json:"value"`
		}
		if json.Unmarshal(raw, &obj) == nil {
			return obj.Value
		}
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	return 0
}

func (e *ESExecutor) execSearch(ctx context.Context, client *datasource.ESHTTP, body string, maxRows int, index string) (*Result, error) {
	if client == nil {
		return nil, fmt.Errorf("executor: search: elasticsearch http client is nil")
	}
	searchBody, err := injectESSearchSize(body, maxRows)
	if err != nil {
		return nil, err
	}
	status, respBody, err := client.Do(ctx, http.MethodPost, esSearchPath(index), []byte(searchBody))
	if err != nil {
		return nil, fmt.Errorf("executor: search: %w", err)
	}
	if status >= 400 {
		baseErr := fmt.Errorf("executor: search: HTTP %d %s", status, string(respBody))
		if isESSchemaRelatedBody(respBody) {
			return nil, &SchemaRelatedError{Err: baseErr}
		}
		return nil, baseErr
	}

	var out struct {
		Hits struct {
			Total json.RawMessage `json:"total"`
			Hits  []struct {
				Source map[string]interface{} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("executor: decode search response: %w", err)
	}
	estimatedTotal := parseESHitsTotal(out.Hits.Total)

	hits := out.Hits.Hits
	truncated := false
	if maxRows > 0 && len(hits) > maxRows {
		hits = hits[:maxRows]
		truncated = true
	}
	// 即使本次 hits 数 ≤ maxRows,server 端 total 也可能更大(size 默认 10)
	// 只有在调用方明确设置了 maxRows 限制时才判断 server 端 total 是否超出,
	// 否则当 maxRows==0(未限制)时会误报 truncated。
	if maxRows > 0 && estimatedTotal > int64(len(hits)) {
		truncated = true
	}

	// 跨所有 hits 收集 key union,保证异构文档不丢字段
	keySet := make(map[string]struct{}, 16)
	for _, h := range hits {
		for k := range h.Source {
			keySet[k] = struct{}{}
		}
	}
	// 优先列(若存在则前置,顺序固定): _id, _score, _index
	var columns []string
	for _, p := range []string{"_id", "_score", "_index"} {
		if _, ok := keySet[p]; ok {
			columns = append(columns, p)
			delete(keySet, p)
		}
	}
	// 其余字段字母序追加,保证 columns 顺序稳定(LLM prompt 缓存友好)
	rest := make([]string, 0, len(keySet))
	for k := range keySet {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	columns = append(columns, rest...)

	rows := make([][]any, 0, len(hits))
	for _, h := range hits {
		row := make([]any, len(columns))
		for i, col := range columns {
			row[i] = h.Source[col]
		}
		rows = append(rows, row)
	}
	return &Result{Columns: columns, Rows: rows, Truncated: truncated, EstimatedTotal: estimatedTotal}, nil
}

// esSchemaErrorTypes 是 ES error.type 中代表 schema/mapping/索引不存在的类型集合
var esSchemaErrorTypes = map[string]struct{}{
	"query_shard_exception":            {},
	"index_not_found_exception":        {},
	"mapper_parsing_exception":         {},
	"strict_dynamic_mapping_exception": {},
}

// isESSchemaRelatedBody 解析 ES 错误响应 body,按 error.type 判定是否 schema 相关
func isESSchemaRelatedBody(body []byte) bool {
	var b struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return false
	}
	_, ok := esSchemaErrorTypes[b.Error.Type]
	return ok
}
