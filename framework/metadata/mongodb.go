package metadata

import (
	"context"
	"fmt"

	"github.com/sixath/framework/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// FetchSchemaMongo 从 MongoDB 拉取当前库的元数据：集合列表及每个集合示例文档的顶层字段。
func FetchSchemaMongo(ctx context.Context, db *mongo.Database) (*Schema, error) {
	if db == nil {
		return nil, fmt.Errorf("metadata: mongodb database is nil")
	}

	schema := &Schema{
		Name: db.Name(),
	}

	collections, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("metadata: list collections: %w", err)
	}

	for _, name := range collections {
		tbl := Table{Name: name}

		coll := db.Collection(name)
		var doc bson.M
		err := coll.FindOne(ctx, bson.D{}).Decode(&doc)
		if err == nil && len(doc) > 0 {
			for field, val := range doc {
				tbl.Columns = append(tbl.Columns, Column{
					Name:       field,
					Type:       fmt.Sprintf("%T", val),
					IsNullable: true,
				})
			}
		}

		schema.Tables = append(schema.Tables, tbl)
	}

	return schema, nil
}

// RefreshFromRegistryMongo 与 RefreshFromRegistry 等价，保留兼容别名。
func RefreshFromRegistryMongo(ctx context.Context, reg *datasource.Registry, store *InMemoryStore, datasourceID string) (*Schema, error) {
	return RefreshFromRegistry(ctx, reg, store, datasourceID)
}
