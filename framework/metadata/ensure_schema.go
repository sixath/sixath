package metadata

import (
	"context"

	"github.com/sixath/framework/datasource"
)

// EnsureSchemaForDatasource 在缓存为空或 datasource_id 变化时从注册表刷新，避免多数据源串库。
func EnsureSchemaForDatasource(ctx context.Context, reg *datasource.Registry, store *InMemoryStore, datasourceID string) (*Schema, error) {
	if store == nil {
		return nil, ErrUnsupportedDataSource
	}
	if reg == nil {
		schema, err := store.GetSchema(ctx)
		return schema, err
	}
	if store.CachedDatasourceID() != datasourceID {
		return RefreshFromRegistry(ctx, reg, store, datasourceID)
	}
	schema, err := store.GetSchema(ctx)
	if err != nil {
		return nil, err
	}
	if schema == nil {
		return RefreshFromRegistry(ctx, reg, store, datasourceID)
	}
	return schema, nil
}
