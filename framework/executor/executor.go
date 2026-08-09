package executor

import (
	"context"
	"errors"
)

var (
	// ErrReadOnlyViolation 在只读模式下尝试执行写操作时返回。
	ErrReadOnlyViolation = errors.New("executor: write operation not allowed in read-only mode")
	// ErrUnsupportedDataSource 当数据源无法提供 *sql.DB 时返回。
	ErrUnsupportedDataSource = errors.New("executor: unsupported datasource for execution")
)

// SchemaRelatedError 表示与表/字段结构相关的执行错误（如未知列、未知字段、mapping 不存在等）。
// 各数据源实现（MySQL、Elasticsearch、PostgreSQL 等）在检测到此类错误时应将原始错误包装为 SchemaRelatedError，
// 以便上层（如 execute_read 工具）统一追加「请先 describe_table」类提示，与具体数据库无关。
type SchemaRelatedError struct{ Err error }

func (e *SchemaRelatedError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *SchemaRelatedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsSchemaRelated 判断 err 或其链中是否包含 SchemaRelatedError。
// 用于在工具层统一决定是否提示「先 describe_table 再重试」，无需针对各数据库写死错误码或文案。
func IsSchemaRelated(err error) bool {
	var se *SchemaRelatedError
	return errors.As(err, &se)
}

// ExecuteOptions 执行选项：超时、最大行数、写权限、可选参数。
// 零值默认禁止写（deny-by-default）；写操作须显式 AllowWrite: true。
type ExecuteOptions struct {
	Timeout    int  // 超时秒数，0 表示不限制
	MaxRows    int  // 查询结果最大行数，0 表示不限制
	AllowWrite bool // 为 true 时允许 INSERT/UPDATE/DELETE 等写操作
	// ReadOnly 已废弃，请使用 AllowWrite。ReadOnly: true 仍表示禁止写。
	ReadOnly bool // Deprecated: use AllowWrite instead.
	PositionalParams []any
	NamedParams      map[string]any
	Extras           map[string]any
	Params           map[string]any // Deprecated: use Extras.
}

// AllowsWrite 返回是否允许执行写操作。
func (o ExecuteOptions) AllowsWrite() bool {
	if o.AllowWrite {
		return true
	}
	if o.ReadOnly {
		return false
	}
	return false
}

// Result 执行结果。
type Result struct {
	Columns      []string // 查询结果列名（SELECT）
	Rows         [][]any  // 查询结果行数据
	AffectedRows int64    // 写操作影响行数（INSERT/UPDATE/DELETE）

	// Truncated 为 true 表示返回的 Rows 已被 MaxRows 截断,真实结果集更大。
	// LLM 工具应据此提示用户加 WHERE / LIMIT 缩小范围或翻页。
	Truncated bool

	// EstimatedTotal 给出真实结果集的估计大小(0 表示未填充)。
	// ES: hits.total.value
	// MySQL / Mongo: 当前默认不填(获取代价较大,后续可按需扩展)。
	EstimatedTotal int64
}

// Executor 执行 DSL（如 SQL）的抽象，支持超时、最大行数、只读拦截。
type Executor interface {
	// Execute 在指定数据源上执行 dsl，opts 控制超时、行数上限与只读策略。
	Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error)
}
