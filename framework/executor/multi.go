package executor

import (
	"context"

	"github.com/sixath/framework/datasource"
)

// MultiExecutor 按数据源类型分发到 MySQL、Elasticsearch 或 MongoDB 执行器。
//
// Deprecated: 请使用 ExecutorRegistry 或 Bundle。本类型保留兼容，内部基于 ExecutorRegistry 路由。
type MultiExecutor struct {
	Registry      *datasource.Registry
	MySQL         *MySQLExecutor
	Elasticsearch *ESExecutor
	Mongo         *MongoExecutor
	registry      *ExecutorRegistry
}

// NewMultiExecutor 创建多数据源执行器。各具体执行器可为 nil，对应类型将返回 ErrUnsupportedDataSource。
func NewMultiExecutor(reg *datasource.Registry, mysql *MySQLExecutor, es *ESExecutor, mongo *MongoExecutor) *MultiExecutor {
	m := &MultiExecutor{
		Registry:      reg,
		MySQL:         mysql,
		Elasticsearch: es,
		Mongo:         mongo,
		registry:      NewExecutorRegistry(reg),
	}
	if mysql != nil {
		m.registry.Register(datasource.TypeMySQL, mysql)
	}
	if es != nil {
		m.registry.Register(datasource.TypeElasticsearch, es)
	}
	if mongo != nil {
		m.registry.Register(datasource.TypeMongoDB, mongo)
	}
	return m
}

// Execute 实现 Executor。根据 datasource Type() 分发。
func (e *MultiExecutor) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
	return e.registry.Execute(ctx, datasourceID, dsl, opts)
}
