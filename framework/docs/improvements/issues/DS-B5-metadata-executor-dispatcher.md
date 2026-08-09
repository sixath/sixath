# [DS-B5] metadata / executor 公共 dispatcher 抽取

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/metadata、framework/executor |
| **状态** | 已完成 |
| **关联报告** | [01-datasource.md B5](../01-datasource.md) |
| **预估工作量** | 1-2 天 |
| **依赖** | [DS-B1](DS-B1-type-and-executor-registry.md) 必须先完成 |

## 问题位置

- `framework/metadata/mysql.go` / `elasticsearch.go` / `mongodb.go` 与
- `framework/executor/mysql.go` / `elasticsearch.go` / `mongodb.go`

## 现状

两套并行的"按 ID 查 → 类型断言 → 走对应实现"逻辑。metadata 和 executor 各自持有 `*datasource.Registry`,各自做类型断言,各自定义 `dbProvider` / `ESClientProvider` / `MongoDatabaseProvider` 的判断。

## 问题分析

- 重复代码 ~30%+
- 增加新后端要在 metadata 和 executor 各改一遍
- 类型断言风格散落,难以统一改造

## 改进方案

基于 [DS-B1](DS-B1-type-and-executor-registry.md) 引入的 `Registry.Type()`,抽公共 dispatcher:

```go
// framework/datasource/dispatcher.go (新)

// TypedDispatcher 按数据源类型路由到对应实现,通用于 executor 和 metadata
type TypedDispatcher[T any] struct {
    reg     *Registry
    byType  map[string]T
}

func NewTypedDispatcher[T any](reg *Registry) *TypedDispatcher[T] {
    return &TypedDispatcher[T]{reg: reg, byType: make(map[string]T)}
}

func (d *TypedDispatcher[T]) Register(typ string, impl T) { d.byType[typ] = impl }

func (d *TypedDispatcher[T]) For(datasourceID string) (T, error) {
    var zero T
    ds, err := d.reg.Get(datasourceID)
    if err != nil { return zero, err }
    impl, ok := d.byType[ds.Type()]
    if !ok { return zero, ErrUnsupportedDataSource }
    return impl, nil
}
```

`ExecutorRegistry` 与 `metadata.Store` 都基于此实现:
```go
type ExecutorRegistry = TypedDispatcher[Executor]
type MetadataRegistry = TypedDispatcher[metadata.Store]
```

## 验收标准

- [ ] `TypedDispatcher[T]` 新增,有完整单测
- [ ] `ExecutorRegistry` 改为基于 `TypedDispatcher`,行为不变
- [ ] `metadata` 也补充对应的 registry 风格,新增 `metadata.Registry` 替代当前各包独立的 store
- [ ] metadata / executor 重复代码量减少至少 30%
- [ ] 现有调用点全部兼容

## 测试要求

- 泛型 dispatcher 单测: register / For / 未注册 type / 未注册 ID
- metadata 与 executor 各自的现有测试 0 回归

## 风险

- Go 泛型限制: 接口方法不能是泛型,所以 `TypedDispatcher` 是 struct + method,非 interface。已规避
- `metadata.Store` 接口与 dispatcher 集成时,需要每个数据源实现 `Store` 的工厂方法,可能要小幅改造现有 metadata 包
