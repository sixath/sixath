package executor

import "context"

// QueryExtras 返回 Extras，若为空则回退 Params。
func (o QueryOptions) QueryExtras() map[string]any {
	if len(o.Extras) > 0 {
		return o.Extras
	}
	return o.Params
}

// QueryExtras 返回 Extras，若为空则回退 Params。
func (o ExecuteOptions) QueryExtras() map[string]any {
	if len(o.Extras) > 0 {
		return o.Extras
	}
	return o.Params
}

// Reader 只读查询接口。
type Reader interface {
	Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error)
}

// QueryOptions 只读查询选项。
type QueryOptions struct {
	Timeout int // 秒，0 表示不限制
	MaxRows int

	// PositionalParams 用于 ? 占位符（MySQL 等）。
	PositionalParams []any
	// NamedParams 用于 :name 占位符（转换为 ?）。
	NamedParams map[string]any
	// Extras 为后端特有参数（如 ES index）；Params 为兼容别名。
	Extras map[string]any
	Params map[string]any // Deprecated: use Extras.
}

// QueryResult 只读查询结果。
type QueryResult struct {
	Columns        []string
	Rows           [][]any
	Truncated      bool
	EstimatedTotal int64
	HitStatus      string `json:"hit_status,omitempty"`
	QueriedIndex   string `json:"queried_index,omitempty"`
	// RepairedSQL / RepairNote are set when execute_read auto-rewrote a schema error.
	RepairedSQL string `json:"repaired_sql,omitempty"`
	RepairNote  string `json:"repair_note,omitempty"`
}

func queryResultFromResult(r *Result) *QueryResult {
	if r == nil {
		return nil
	}
	return &QueryResult{
		Columns:        r.Columns,
		Rows:           r.Rows,
		Truncated:      r.Truncated,
		EstimatedTotal: r.EstimatedTotal,
	}
}

func resultFromQueryResult(q *QueryResult) *Result {
	if q == nil {
		return nil
	}
	return &Result{
		Columns:        q.Columns,
		Rows:           q.Rows,
		Truncated:      q.Truncated,
		EstimatedTotal: q.EstimatedTotal,
	}
}
