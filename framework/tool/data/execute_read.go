package tooldata

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
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
	t := tool.Tool{
		Name:        "execute_read",
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dsl": map[string]any{
					"type":        "string",
					"description": "SQL statement (MySQL/Hive) or the native query of the datasource; for Elasticsearch use es_log_query",
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
				"positional_params": map[string]any{
					"type":        "array",
					"description": "Optional. Values for ? placeholders in SQL (safer than string interpolation).",
				},
				"named_params": map[string]any{
					"type":        "object",
					"description": "Optional. Map of :name placeholders to values (converted to ? for MySQL).",
				},
			},
			"required": []string{"dsl"},
		},
		ArgAliases: map[string]string{"query": "dsl"},
		Execute:    buildExecuteReadExecute(cfg),
	}
	if cfg != nil {
		t.ArgChecks = []tool.ArgCheck{
			DatasourceIDCheck(cfg.Registry, cfg.DefaultDatasourceID),
			tool.Reject{
				Label:    "non_sql",
				Redirect: "for Elasticsearch logs use es_log_query (query_string / DSL go there)",
				Detect: func(params map[string]any) (string, string, bool) {
					id := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
					if !isSQLDatasource(datasourceType(cfg.Registry, id)) {
						return "", "", false
					}
					if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
						return "index", "execute_read does not take an index (SQL datasource)", true
					}
					dsl, _ := params["dsl"].(string)
					if reason, hit := nonSQLReason(dsl); hit {
						return "dsl", reason, true
					}
					return "", "", false
				},
			},
			sqlSchemaRule{cfg: cfg},
		}
		t.EmptyProbe = sqlEmptyProbe(cfg)
	}
	return r.Register(t)
}

var sqlSchemaRefresh = metadata.RefreshFromRegistry

// sqlSchemaRule 对 MySQL 单表 SELECT 校验表名与列名；表结构来自 metadata store，有缺失时强制刷新一次再判定。
type sqlSchemaRule struct{ cfg *ExecuteReadConfig }

func (sqlSchemaRule) Name() string { return "sql_schema" }

func (r sqlSchemaRule) Check(ctx context.Context, params map[string]any) ([]tool.SchemaError, error) {
	cfg := r.cfg
	if cfg == nil || cfg.Store == nil || cfg.Registry == nil {
		return nil, nil
	}
	id := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
	if datasourceType(cfg.Registry, id) != datasource.TypeMySQL {
		return nil, nil
	}
	dsl, _ := params["dsl"].(string)
	refs, ok := parseSingleTableSQL(dsl)
	if !ok || refs.Qualified {
		return nil, nil
	}
	tables, err := r.tables(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, nil
	}
	tbl, found := findTable(tables, refs.Table)
	if !found || len(missingColumns(tbl, refs.Columns)) > 0 {
		fresh, err := r.tables(ctx, id, true)
		if err != nil {
			return nil, err
		}
		if len(fresh) == 0 {
			return nil, nil
		}
		tables = fresh
		tbl, found = findTable(tables, refs.Table)
	}
	if !found {
		names := make([]string, 0, len(tables))
		for _, t := range tables {
			names = append(names, t.Name)
		}
		return []tool.SchemaError{{
			Path:       "dsl",
			Keyword:    tool.KeywordUnknownField,
			Message:    fmt.Sprintf("table %q does not exist in datasource %q", refs.Table, id),
			Candidates: tool.Suggest(refs.Table, names, 3),
			Hint:       "use list_tables / describe_table to find the right table",
		}}, nil
	}
	missing := missingColumns(tbl, refs.Columns)
	if len(missing) == 0 {
		return nil, nil
	}
	cols := make([]string, 0, len(tbl.Columns))
	for _, c := range tbl.Columns {
		cols = append(cols, c.Name)
	}
	errs := make([]tool.SchemaError, 0, len(missing))
	for _, c := range missing {
		errs = append(errs, tool.SchemaError{
			Path:       "dsl",
			Keyword:    tool.KeywordUnknownField,
			Message:    fmt.Sprintf("column %q does not exist in table %q", c, tbl.Name),
			Candidates: tool.Suggest(c, cols, 3),
			Hint:       "use describe_table " + tbl.Name + " to see columns",
		})
	}
	return errs, nil
}

func (r sqlSchemaRule) tables(ctx context.Context, id string, refresh bool) ([]metadata.Table, error) {
	var (
		s   *metadata.Schema
		err error
	)
	if refresh {
		s, err = sqlSchemaRefresh(ctx, r.cfg.Registry, r.cfg.Store, id)
	} else {
		s, err = metadata.EnsureSchemaForDatasource(ctx, r.cfg.Registry, r.cfg.Store, id)
	}
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return s.Tables, nil
}

func findTable(tables []metadata.Table, name string) (metadata.Table, bool) {
	for _, t := range tables {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return metadata.Table{}, false
}

// missingColumns 在表没有列信息时返回空（无法判定）。
func missingColumns(tbl metadata.Table, refs []string) []string {
	if len(tbl.Columns) == 0 {
		return nil
	}
	var out []string
	for _, r := range refs {
		found := false
		for _, c := range tbl.Columns {
			if strings.EqualFold(c.Name, r) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, r)
		}
	}
	return out
}

// sqlEmptyProbe 对 SQL 单表带条件查询的 0 行结果做一次主表 COUNT(*)。
func sqlEmptyProbe(cfg *ExecuteReadConfig) *tool.EmptyProbe {
	return &tool.EmptyProbe{
		Relax: func(params map[string]any) []tool.ProbeVariant {
			id := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
			// Hive 上 COUNT(*) 是全表扫描作业，不适合作为探测。
			if datasourceType(cfg.Registry, id) != datasource.TypeMySQL {
				return nil
			}
			dsl, _ := params["dsl"].(string)
			refs, ok := parseSingleTableSQL(dsl)
			if !ok || !refs.HasCondition {
				return nil
			}
			return []tool.ProbeVariant{{Label: "table_total", Params: map[string]any{
				"datasource_id": id,
				"dsl":           "SELECT COUNT(*) FROM " + quoteSQLTable(refs.Table),
			}}}
		},
		Count: func(ctx context.Context, v tool.ProbeVariant) (int64, error) {
			reader := executor.CoalesceReader(cfg.Reader, cfg.Exec)
			if reader == nil {
				return 0, errors.New("reader not configured")
			}
			id, _ := v.Params["datasource_id"].(string)
			dsl, _ := v.Params["dsl"].(string)
			res, err := reader.Query(ctx, id, dsl, executor.QueryOptions{MaxRows: 1})
			if err != nil {
				return 0, err
			}
			if res == nil || len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
				return 0, errors.New("empty count result")
			}
			return toInt64(res.Rows[0][0])
		},
	}
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return 0, fmt.Errorf("count %d overflows int64", x)
		}
		return int64(x), nil
	case float64:
		return int64(x), nil
	case []byte:
		return strconv.ParseInt(string(x), 10, 64)
	case string:
		return strconv.ParseInt(x, 10, 64)
	}
	return 0, fmt.Errorf("unexpected count type %T", v)
}

func buildExecuteReadExecute(cfg *ExecuteReadConfig) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		start := time.Now()
		status := "ok"
		defer func() {
			obs.ObserveDataQueryTool("execute_read", status, time.Since(start))
		}()

		if cfg == nil || (cfg.Reader == nil && cfg.Exec == nil) {
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

		dsl, _ := params["dsl"].(string)
		if dsl == "" {
			status = "error"
			return nil, errors.New("execute_read: dsl is required and must be a string")
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
		res.HitStatus = tool.HitStatusFromCount(true, len(res.Rows))
		return res, nil
	}
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
