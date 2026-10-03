package executor

import (
	"context"
	"encoding/json"
)

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

// Diagnosis 是零结果探测的结论：放宽条件后的计数，用于区分"条件写错"与"确实无数据"。
type Diagnosis struct {
	Probes    []ProbeCount `json:"probes,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
	Hint      string       `json:"hint,omitempty"`
	Errors    []string     `json:"errors,omitempty"`
}

type ProbeCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
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
	// Aggregations ES 聚合结果原文；无聚合时为空。
	Aggregations json.RawMessage `json:"aggregations,omitempty"`
	Diagnosis    *Diagnosis      `json:"diagnosis,omitempty"`
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
		Aggregations:   r.Aggregations,
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
		Aggregations:   q.Aggregations,
	}
}
