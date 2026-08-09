package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/sixath/framework/datasource"
)

// dbProvider 由可执行 SQL 的数据源实现，用于暴露 *sql.DB。
type dbProvider interface {
	DB() *sql.DB
}

// MySQLExecutor 基于 datasource.Registry 的 MySQL 执行器，支持超时、MaxRows、只读拦截。
type MySQLExecutor struct {
	Registry *datasource.Registry
	Logger   *slog.Logger // 可选；nil 时不记录 DSL
}

// NewMySQLExecutor 创建依赖给定 Registry 的 MySQL 执行器。
func NewMySQLExecutor(reg *datasource.Registry) *MySQLExecutor {
	return &MySQLExecutor{Registry: reg}
}

func (e *MySQLExecutor) mysqlDB(datasourceID string) (*sql.DB, error) {
	ds, err := e.Registry.Get(datasourceID)
	if err != nil {
		return nil, err
	}
	provider, ok := ds.(dbProvider)
	if !ok {
		return nil, ErrUnsupportedDataSource
	}
	return provider.DB(), nil
}

// Query 实现 Reader。
func (e *MySQLExecutor) Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error) {
	ctx, rec := beginOp(ctx, e.Registry, datasourceID, "query", opts.MaxRows, false)
	db, err := e.mysqlDB(datasourceID)
	if err != nil {
		e.finishRun(ctx, rec, datasourceID, "query", dsl, nil, err)
		return nil, err
	}
	cleaned, err := prepareSQL(dsl)
	if err != nil {
		e.finishRun(ctx, rec, datasourceID, "reject", dsl, nil, err)
		return nil, err
	}
	if isWriteSQL(cleaned) {
		e.finishRun(ctx, rec, datasourceID, "readonly_reject", dsl, nil, ErrReadOnlyViolation)
		return nil, ErrReadOnlyViolation
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Second)
		defer cancel()
	}
	res, err := e.execQuery(ctx, db, cleaned, opts)
	e.finishRun(ctx, rec, datasourceID, "query", dsl, res, err)
	return queryResultFromResult(res), err
}

// Exec 实现 Writer。
func (e *MySQLExecutor) Exec(ctx context.Context, datasourceID string, dsl string, opts ExecOptions) (*ExecResult, error) {
	ctx, rec := beginOp(ctx, e.Registry, datasourceID, "write", 0, true)
	db, err := e.mysqlDB(datasourceID)
	if err != nil {
		e.finishRun(ctx, rec, datasourceID, "write", dsl, nil, err)
		return nil, err
	}
	cleaned, err := prepareSQL(dsl)
	if err != nil {
		e.finishRun(ctx, rec, datasourceID, "reject", dsl, nil, err)
		return nil, err
	}
	if !isWriteSQL(cleaned) {
		err := fmt.Errorf("executor: not a write DSL")
		e.finishRun(ctx, rec, datasourceID, "write", dsl, nil, err)
		return nil, err
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Second)
		defer cancel()
	}
	execSQL, execArgs, err := querySQLWithArgs(cleaned, QueryOptions{
		PositionalParams: opts.PositionalParams,
		NamedParams:      opts.NamedParams,
		Params:           opts.Params,
	})
	if err != nil {
		e.finishRun(ctx, rec, datasourceID, "write", dsl, nil, err)
		return nil, err
	}
	sqlRes, err := db.ExecContext(ctx, execSQL, execArgs...)
	if err != nil {
		e.finishRun(ctx, rec, datasourceID, "write", dsl, nil, fmt.Errorf("executor: exec write: %w", err))
		return nil, fmt.Errorf("executor: exec write: %w", err)
	}
	affected, _ := sqlRes.RowsAffected()
	lastID, _ := sqlRes.LastInsertId()
	out := &ExecResult{AffectedRows: affected, LastInsertID: lastID}
	e.finishRun(ctx, rec, datasourceID, "write", dsl, resultFromExecResult(out), nil)
	return out, nil
}

func (e *MySQLExecutor) finishRun(ctx context.Context, rec *opRecorder, datasourceID, op, dsl string, res *Result, err error) {
	rec.finish(res, err)
	e.logExec(ctx, datasourceID, op, dsl, rec.start, res, err)
}

// Execute 实现 Executor。默认禁止写；写操作返回 ErrReadOnlyViolation；查询支持超时与 MaxRows 截断。
func (e *MySQLExecutor) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
	cleaned, err := prepareSQL(dsl)
	if err != nil {
		return nil, err
	}
	if isWriteSQL(cleaned) {
		if !opts.AllowsWrite() {
			return nil, ErrReadOnlyViolation
		}
		er, err := e.Exec(ctx, datasourceID, dsl, ExecOptions{
			Timeout:          opts.Timeout,
			PositionalParams: opts.PositionalParams,
			NamedParams:      opts.NamedParams,
			Params:           opts.Params,
		})
		return resultFromExecResult(er), err
	}
	qr, err := e.Query(ctx, datasourceID, dsl, QueryOptions{
		Timeout:          opts.Timeout,
		MaxRows:          opts.MaxRows,
		PositionalParams: opts.PositionalParams,
		NamedParams:      opts.NamedParams,
		Extras:           opts.Extras,
		Params:           opts.Params,
	})
	return resultFromQueryResult(qr), err
}

func (e *MySQLExecutor) logExec(ctx context.Context, datasourceID, op, dsl string, start time.Time, res *Result, err error) {
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
		if res.AffectedRows > 0 {
			attrs = append(attrs, slog.Int64("affected", res.AffectedRows))
		}
	}
	status := "ok"
	if err != nil {
		status = "error"
		attrs = append(attrs, slog.Any("error", err))
	} else if op == "readonly_reject" {
		status = "rejected"
	}
	attrs = append(attrs, slog.String("status", status))
	logger.LogAttrs(ctx, slog.LevelInfo, "executor.mysql", attrs...)
	logger.LogAttrs(ctx, slog.LevelDebug, "executor.mysql.dsl", slog.String("dsl", MaskLiterals(dsl)))
}

// isMySQLSchemaRelated 判断 MySQL 驱动返回的错误是否与表/列结构相关。
// 使用 MySQL errno 而非子串匹配,避免 locale 化与字面量误命中。
//   1049: ER_BAD_DB_ERROR        - Unknown database
//   1051: ER_BAD_TABLE_ERROR     - Unknown table
//   1054: ER_BAD_FIELD_ERROR     - Unknown column
//   1146: ER_NO_SUCH_TABLE       - Table doesn't exist
func isMySQLSchemaRelated(err error) bool {
	var me *mysqldriver.MySQLError
	if !errors.As(err, &me) {
		return false
	}
	switch me.Number {
	case 1049, 1051, 1054, 1146:
		return true
	}
	return false
}

// wrapMaybeSchemaRelated 若 err 为结构相关错误则包装为 SchemaRelatedError，否则按 format 包装。
func wrapMaybeSchemaRelated(err error, format string, args ...any) error {
	wrapped := fmt.Errorf(format, args...)
	if isMySQLSchemaRelated(err) {
		return &SchemaRelatedError{Err: wrapped}
	}
	return wrapped
}

func (e *MySQLExecutor) execQuery(ctx context.Context, db *sql.DB, cleaned string, opts QueryOptions) (*Result, error) {
	sqlText, args, err := querySQLWithArgs(cleaned, opts)
	if err != nil {
		return nil, err
	}
	querySQL, err := applyMaxRowsToSQL(sqlText, opts.MaxRows)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, wrapMaybeSchemaRelated(err, "executor: query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, wrapMaybeSchemaRelated(err, "executor: columns: %w", err)
	}
	out := &Result{Columns: cols}

	for rows.Next() {
		if opts.MaxRows > 0 && len(out.Rows) >= opts.MaxRows {
			out.Truncated = true
			break
		}
		dest := make([]any, len(cols))
		destPtr := make([]interface{}, len(cols))
		for i := range dest {
			destPtr[i] = &dest[i]
		}
		if err := rows.Scan(destPtr...); err != nil {
			return nil, wrapMaybeSchemaRelated(err, "executor: scan row: %w", err)
		}
		row := make([]any, len(dest))
		for i, v := range dest {
			if b, ok := v.([]byte); ok {
				row[i] = string(b) // 避免 json.Marshal 将 []byte 序列化为 base64（如 IP 10.158.16.7 变成 MTAuMTU4LjE2Ljc）
			} else {
				row[i] = v
			}
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapMaybeSchemaRelated(err, "executor: iterate rows: %w", err)
	}
	if opts.MaxRows > 0 && len(out.Rows) >= opts.MaxRows {
		out.Truncated = true
	}
	return out, nil
}
