package metadata

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sixath/framework/datasource"
)

// FetchSchema 从给定 *sql.DB 拉取当前库的元数据（表 + 列）
// 只依赖标准 information_schema，适用于 MySQL 及兼容实现。
func FetchSchema(ctx context.Context, db *sql.DB) (*Schema, error) {
	var dbName string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName); err != nil {
		return nil, fmt.Errorf("metadata: query current database: %w", err)
	}

	const q = `
SELECT
    t.table_name,
    IFNULL(t.table_comment, '') AS table_comment,
    c.column_name,
    c.data_type,
    c.is_nullable,
    IFNULL(c.column_comment, '') AS column_comment
FROM information_schema.tables t
JOIN information_schema.columns c
  ON t.table_schema = c.table_schema
 AND t.table_name = c.table_name
WHERE t.table_schema = ?
ORDER BY t.table_name, c.ordinal_position
`

	rows, err := db.QueryContext(ctx, q, dbName)
	if err != nil {
		return nil, fmt.Errorf("metadata: query information_schema: %w", err)
	}
	defer rows.Close()

	tablesMap := make(map[string]*Table)

	for rows.Next() {
		var (
			tableName, tableComment string
			colName, dataType       string
			isNullable              string
			colComment              string
		)
		if err := rows.Scan(&tableName, &tableComment, &colName, &dataType, &isNullable, &colComment); err != nil {
			return nil, fmt.Errorf("metadata: scan metadata row: %w", err)
		}

		tbl, ok := tablesMap[tableName]
		if !ok {
			tbl = &Table{
				Name:    tableName,
				Comment: tableComment,
			}
			tablesMap[tableName] = tbl
		}

		col := Column{
			Name:       colName,
			Type:       dataType,
			IsNullable: isNullable == "YES",
			Comment:    colComment,
		}
		tbl.Columns = append(tbl.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadata: iterate metadata rows: %w", err)
	}

	schema := &Schema{
		Name: dbName,
	}
	for _, t := range tablesMap {
		schema.Tables = append(schema.Tables, *t)
	}
	return schema, nil
}

// RefreshFromRegistry 通过 metadata.Registry 按 DataSource.Type() 刷新内存 Store。
func RefreshFromRegistry(ctx context.Context, reg *datasource.Registry, store *InMemoryStore, datasourceID string) (*Schema, error) {
	return RefreshWithRegistry(ctx, NewRegistry(reg), store, datasourceID)
}

// RefreshWithRegistry 使用显式 metadata.Registry 刷新（支持自定义类型注册）。
func RefreshWithRegistry(ctx context.Context, metaReg *Registry, store *InMemoryStore, datasourceID string) (*Schema, error) {
	fetch, err := metaReg.ResolveFetcher(datasourceID)
	if err != nil {
		return nil, err
	}
	store.fetch = fetch
	schema, err := store.Refresh(ctx)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	store.cachedDatasourceID = datasourceID
	store.mu.Unlock()
	return schema, nil
}
