package executor

import "context"

// Writer 写操作接口（仅支持写的后端实现此接口）。
type Writer interface {
	Exec(ctx context.Context, datasourceID string, dsl string, opts ExecOptions) (*ExecResult, error)
}

// ExecOptions 写操作选项。
type ExecOptions struct {
	Timeout          int // 秒，0 表示不限制
	PositionalParams []any
	NamedParams      map[string]any
	Params           map[string]any // Deprecated
}

// ExecResult 写操作结果。
type ExecResult struct {
	AffectedRows int64
	LastInsertID int64
}

func execResultFromResult(r *Result) *ExecResult {
	if r == nil {
		return nil
	}
	return &ExecResult{AffectedRows: r.AffectedRows}
}

func resultFromExecResult(e *ExecResult) *Result {
	if e == nil {
		return nil
	}
	return &Result{AffectedRows: e.AffectedRows}
}
