package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sixath/framework/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoExecutor 基于 datasource.Registry 的 MongoDB 执行器，目前仅支持只读 find 查询。
type MongoExecutor struct {
	Registry *datasource.Registry
}

// NewMongoExecutor 创建依赖给定 Registry 的 Mongo 执行器。
func NewMongoExecutor(reg *datasource.Registry) *MongoExecutor {
	return &MongoExecutor{Registry: reg}
}

// mongoQuery 描述 execute_read 传入的 Mongo 查询 DSL。
// 期望为 JSON 字符串，例如：
// {"collection":"users","filter":{"status":"active"},"limit":50}
type mongoQuery struct {
	Collection string         `json:"collection"`
	Filter     map[string]any `json:"filter"`
	Limit      int64          `json:"limit"`
	Projection map[string]any `json:"projection"`
	Sort       map[string]int `json:"sort"`
}

// Query 实现 Reader。
func (e *MongoExecutor) Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error) {
	res, err := e.Execute(ctx, datasourceID, dsl, ExecuteOptions{
		Timeout: opts.Timeout,
		MaxRows: opts.MaxRows,
		Params:  opts.Params,
	})
	return queryResultFromResult(res), err
}

// Execute 实现 Executor。仅支持只读 find 查询；写操作由上层 execute_write 工具与其他执行器处理。
func (e *MongoExecutor) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
	ctx, rec := beginOp(ctx, e.Registry, datasourceID, "find", opts.MaxRows, false)
	ds, err := e.Registry.Get(datasourceID)
	if err != nil {
		rec.finish(nil, err)
		return nil, err
	}
	mp, ok := ds.(datasource.MongoDatabaseProvider)
	if !ok {
		err := ErrUnsupportedDataSource
		rec.finish(nil, err)
		return nil, err
	}
	db := mp.MongoDatabase()

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Second)
		defer cancel()
	}

	var q mongoQuery
	if err := json.Unmarshal([]byte(dsl), &q); err != nil {
		err = fmt.Errorf("executor: parse mongo dsl as JSON: %w", err)
		rec.finish(nil, err)
		return nil, err
	}
	if q.Collection == "" {
		err := fmt.Errorf("executor: mongo dsl missing collection")
		rec.finish(nil, err)
		return nil, err
	}

	coll := db.Collection(q.Collection)

	filter := any(bson.D{})
	if q.Filter != nil {
		filter = q.Filter
	}

	findOpts := options.Find()
	if q.Limit > 0 {
		findOpts.SetLimit(q.Limit)
	}
	if opts.MaxRows > 0 && (q.Limit == 0 || q.Limit > int64(opts.MaxRows)) {
		findOpts.SetLimit(int64(opts.MaxRows))
	}
	if len(q.Projection) > 0 {
		findOpts.SetProjection(q.Projection)
	}
	if len(q.Sort) > 0 {
		findOpts.SetSort(q.Sort)
	}

	cursor, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		err = fmt.Errorf("executor: mongo find: %w", err)
		rec.finish(nil, err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		err = fmt.Errorf("executor: mongo iterate: %w", err)
		rec.finish(nil, err)
		return nil, err
	}
	// 当 MaxRows 限制了 SetLimit,Mongo 端返回的 docs 长度恰好 = limit 时,
	// 我们无法从单次 cursor 判断"是否还有更多"——保守估计:返回了 MaxRows 条就是截断。
	truncated := false
	if opts.MaxRows > 0 && int64(len(docs)) >= int64(opts.MaxRows) {
		truncated = true
	}
	if len(docs) == 0 {
		out := &Result{}
		rec.finish(out, nil)
		return out, nil
	}

	// 跨文档收集列名 union，排序保证稳定（与 ES 执行器一致）
	keySet := make(map[string]struct{}, 16)
	for _, doc := range docs {
		for k := range doc {
			keySet[k] = struct{}{}
		}
	}
	columns := make([]string, 0, len(keySet))
	for k := range keySet {
		columns = append(columns, k)
	}
	sort.Strings(columns)

	rows := make([][]any, 0, len(docs))
	for _, doc := range docs {
		row := make([]any, len(columns))
		for i, col := range columns {
			row[i] = doc[col]
		}
		rows = append(rows, row)
	}

	out := &Result{Columns: columns, Rows: rows, Truncated: truncated}
	rec.finish(out, nil)
	return out, nil
}
