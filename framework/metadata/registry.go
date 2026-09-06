package metadata

import (
	"context"
	"database/sql"

	"github.com/sixath/framework/datasource"
)

// FetcherResolver 根据具体 DataSource 实例构造 Schema 拉取函数。
type FetcherResolver func(ds datasource.DataSource) (func(context.Context) (*Schema, error), error)

// Registry 按 DataSource.Type() 路由元数据拉取逻辑。
type Registry struct {
	d *datasource.TypedDispatcher[FetcherResolver]
}

// NewRegistry 创建元数据注册表并注册内置 MySQL / Elasticsearch / MongoDB 解析器。
func NewRegistry(dsReg *datasource.Registry) *Registry {
	r := &Registry{d: datasource.NewTypedDispatcher[FetcherResolver](dsReg)}
	r.d.Register(datasource.TypeMySQL, mysqlFetcherResolver)
	r.d.Register(datasource.TypeElasticsearch, esFetcherResolver)
	r.d.Register(datasource.TypeMongoDB, mongoFetcherResolver)
	return r
}

// Register 为额外类型注册 FetcherResolver（如测试用自定义类型）。
func (r *Registry) Register(typ string, resolver FetcherResolver) {
	r.d.Register(typ, resolver)
}

func mysqlFetcherResolver(ds datasource.DataSource) (func(context.Context) (*Schema, error), error) {
	p, ok := ds.(interface{ DB() *sql.DB })
	if !ok {
		return nil, ErrUnsupportedDataSource
	}
	db := p.DB()
	return func(ctx context.Context) (*Schema, error) {
		return FetchSchema(ctx, db)
	}, nil
}

func esFetcherResolver(ds datasource.DataSource) (func(context.Context) (*Schema, error), error) {
	p, ok := ds.(datasource.ESHTTPProvider)
	if !ok {
		return nil, ErrUnsupportedDataSource
	}
	client := p.ESHTTP()
	return func(ctx context.Context) (*Schema, error) {
		return FetchSchemaElasticsearch(ctx, client)
	}, nil
}

func mongoFetcherResolver(ds datasource.DataSource) (func(context.Context) (*Schema, error), error) {
	p, ok := ds.(datasource.MongoDatabaseProvider)
	if !ok {
		return nil, ErrUnsupportedDataSource
	}
	db := p.MongoDatabase()
	return func(ctx context.Context) (*Schema, error) {
		return FetchSchemaMongo(ctx, db)
	}, nil
}

// ResolveFetcher 返回指定数据源的 schema 拉取函数。
func (r *Registry) ResolveFetcher(datasourceID string) (func(context.Context) (*Schema, error), error) {
	ds, err := r.d.RegistryRef().Get(datasourceID)
	if err != nil {
		return nil, err
	}
	resolver, ok := r.d.Lookup(ds.Type())
	if !ok {
		return nil, ErrUnsupportedDataSource
	}
	return resolver(ds)
}
