package executor

import "context"

// executorAsReader 将 Executor 适配为 Reader（兼容旧注入方式）。
type executorAsReader struct{ e Executor }

func (a executorAsReader) Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error) {
	res, err := a.e.Execute(ctx, datasourceID, dsl, ExecuteOptions{
		Timeout:          opts.Timeout,
		MaxRows:          opts.MaxRows,
		PositionalParams: opts.PositionalParams,
		NamedParams:      opts.NamedParams,
		Extras:           opts.QueryExtras(),
		Params:           opts.Params,
	})
	return queryResultFromResult(res), err
}

// executorAsWriter 将 Executor 适配为 Writer（写路径显式 AllowWrite）。
type executorAsWriter struct{ e Executor }

func (a executorAsWriter) Exec(ctx context.Context, datasourceID string, dsl string, opts ExecOptions) (*ExecResult, error) {
	res, err := a.e.Execute(ctx, datasourceID, dsl, ExecuteOptions{
		AllowWrite: true,
		Timeout:    opts.Timeout,
		Params:     opts.Params,
	})
	return execResultFromResult(res), err
}

// CoalesceReader 返回 Reader，优先 r，否则将 e 适配为 Reader。
func CoalesceReader(r Reader, e Executor) Reader {
	if r != nil {
		return r
	}
	if e != nil {
		return executorAsReader{e: e}
	}
	return nil
}

// CoalesceWriter 返回 Writer，优先 w，否则将 e 适配为 Writer。
func CoalesceWriter(w Writer, e Executor) Writer {
	if w != nil {
		return w
	}
	if e != nil {
		return executorAsWriter{e: e}
	}
	return nil
}
