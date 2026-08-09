# [EX-C6] ES / Mongo 执行器补齐单元测试

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/executor |
| **状态** | 已完成 |
| **关联报告** | [02-executor.md C6](../02-executor.md) |
| **预估工作量** | 2 天 |
| **依赖** | 无;但与 [EX-A3](EX-A3-es-columns-stable.md) / [EX-A4](EX-A4-schema-error-by-code.md) / [EX-B2](EX-B2-maxrows-pushdown.md) 相关测试一起补 |

## 问题位置

- `framework/executor/elasticsearch.go`(无对应 `_test.go`)
- `framework/executor/mongodb.go`(无对应 `_test.go`)

## 现状

- `mysql_test.go`: 5 用例
- ES: 0 用例
- Mongo: 0 用例

ES / Mongo 的"首行收集列名"、"map 遍历顺序"、"写 DSL 误判"、"schema error wrapping" 等 bug 本可以用 mock 测出。

## 改进方案

### ES — 用 `httptest` mock transport

```go
// es_test.go
import "net/http/httptest"

func newMockESClient(handler http.HandlerFunc) *elasticsearch.Client {
    server := httptest.NewServer(handler)
    cfg := elasticsearch.Config{Addresses: []string{server.URL}}
    client, _ := elasticsearch.NewClient(cfg)
    return client
}
```

### Mongo — 用官方 `mtest`

```go
import "go.mongodb.org/mongo-driver/mongo/integration/mtest"

mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
mt.Run("test-name", func(mt *mtest.T) {
    // mt.AddMockResponses(...)
})
```

## 验收用例(必须全部覆盖)

### ES (`es_test.go`)
- [ ] `TestESExecutor_BasicSearch`: 单条命中,返回 columns + rows 正确
- [ ] `TestESExecutor_HeterogeneousColumns`(对应 EX-A3): 异构文档,columns 是 union 且确定排序
- [ ] `TestESExecutor_StableColumnOrder`(对应 EX-A3): 同输入 100 次,columns 完全一致
- [ ] `TestESExecutor_MaxRows`: server 返回 100 条,MaxRows=10,result 含 10 行 + Truncated=true
- [ ] `TestESExecutor_PushdownSize`(对应 EX-B2): mock 检查发出的 body 含 `"size"`
- [ ] `TestESExecutor_SchemaError`(对应 EX-A4): 返回 `query_shard_exception`,err 是 `SchemaRelatedError`
- [ ] `TestESExecutor_NonSchemaError`(对应 EX-A4): 返回 `cluster_block_exception`,err 不是 `SchemaRelatedError`
- [ ] `TestESExecutor_UnsupportedDataSource`: 数据源不实现 `ESClientProvider` → ErrUnsupportedDataSource
- [ ] `TestESExecutor_InvalidJSON`(对应 EX-B3): body 非法 JSON → SchemaRelatedError
- [ ] `TestESExecutor_IndexParam`: `opts.Params["index"]` 正确传到 URL

### Mongo (`mongodb_test.go`)
- [ ] `TestMongoExecutor_BasicFind`: 简单查询返回 columns + rows
- [ ] `TestMongoExecutor_MissingCollection`: dsl 缺 `collection` → 错误
- [ ] `TestMongoExecutor_InvalidJSON`: 非法 dsl JSON → 错误
- [ ] `TestMongoExecutor_LimitPushdown`: MaxRows=5 时 Mongo 收到的 limit 是 5
- [ ] `TestMongoExecutor_SortPushdown`: dsl 含 sort,Mongo 收到对应 SetSort 参数
- [ ] `TestMongoExecutor_HeterogeneousColumns`: 异构 doc 时 columns 取 union(对应 EX-A3 的 Mongo 等价)
- [ ] `TestMongoExecutor_UnsupportedDataSource`: 同上
- [ ] `TestMongoExecutor_Truncated`(对应 EX-C3): 返回的 cursor 还有更多数据时,Truncated=true

### 公共
- [ ] CI 跑 `go test -race ./...`,无 data race 警告
- [ ] 覆盖率 `go test -cover ./...` 到 70%+

## 风险

- mtest 只支持有限 Mongo 操作集,某些边界场景可能要 fallback 到 docker-based integration test
- `httptest` 不能完全模拟 ES 集群行为,但单测层面足够

## 关联 issue

- 这是一个"测试补齐"型 issue,会被多个其他 P0/P1 issue 引用作为验收依赖
