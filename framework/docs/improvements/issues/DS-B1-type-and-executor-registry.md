# [DS-B1+B2] 引入 `DataSource.Type()` 与按 type 路由的 ExecutorRegistry

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/datasource、framework/executor |
| **状态** | 已完成 |
| **关联报告** | [01-datasource.md B1 / B2](../01-datasource.md)、[02-executor.md B1](../02-executor.md) |
| **预估工作量** | 2-3 天 |
| **依赖** | 无;但本 issue 是 [EX-A1](EX-A1-reader-writer-split.md) 与 [DS-B5](DS-B5-metadata-executor-dispatcher.md) 的前置 |

## 问题位置

- `framework/datasource/datasource.go: DataSource interface`
- `framework/executor/multi.go: (*MultiExecutor).Execute`
- 各 datasource 实现(mysql.go / elasticsearch.go / mongodb.go / hive.go / noop.go)

## 现状

```go
// datasource.go
type DataSource interface {
    ID() string
    Ping(ctx context.Context) error
    Close() error
}

// multi.go
func (e *MultiExecutor) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
    ds, err := e.Registry.Get(datasourceID)
    if err != nil { return nil, err }
    if e.MySQL != nil {
        if _, ok := ds.(sqlDBProvider); ok { return e.MySQL.Execute(...) }
    }
    if e.Elasticsearch != nil {
        if _, ok := ds.(datasource.ESClientProvider); ok { return e.Elasticsearch.Execute(...) }
    }
    if e.Mongo != nil {
        if _, ok := ds.(datasource.MongoDatabaseProvider); ok { return e.Mongo.Execute(...) }
    }
    return nil, ErrUnsupportedDataSource
}
```

## 问题分析

1. **违反开闭原则**: 新增后端(ClickHouse / Postgres / Redis)要同时改 `MultiExecutor` 字段、构造器、Execute 方法 —— 三处
2. **与已有注册表风格不一致**: `datasource.Registry.RegisterType` 已是 by-type 注册,但 executor 层却在做 if-else 类型断言
3. **顺序敏感**: 若一个数据源同时实现两个 provider 接口,只走第一个匹配,行为不可控
4. **Type 信息丢失**: 注册时 `cfg.Type` 是有的,但运行时 `Registry.Get(id)` 拿不到类型,只能反向断言

## 改进方案

### Step 1 — `DataSource` 增加 `Type() string`

```go
type DataSource interface {
    ID() string
    Type() string         // 新增: "mysql" / "elasticsearch" / "mongodb" / ...
    Ping(ctx context.Context) error
    Close() error
}
```

各实现增加 `Type()` 方法,返回常量字符串。建议在 `datasource` 包内定义:
```go
const (
    TypeMySQL         = "mysql"
    TypeElasticsearch = "elasticsearch"
    TypeMongoDB       = "mongodb"
    TypeHive          = "hive"
    TypeNoop          = "noop"
)
```

### Step 2 — `ExecutorRegistry` 替代 `MultiExecutor`

```go
// framework/executor/registry.go (新文件)

type ExecutorRegistry struct {
    dsReg  *datasource.Registry
    byType map[string]Executor
}

func NewExecutorRegistry(dsReg *datasource.Registry) *ExecutorRegistry {
    return &ExecutorRegistry{
        dsReg:  dsReg,
        byType: make(map[string]Executor),
    }
}

func (r *ExecutorRegistry) Register(typ string, exec Executor) {
    r.byType[typ] = exec
}

func (r *ExecutorRegistry) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
    ds, err := r.dsReg.Get(datasourceID)
    if err != nil { return nil, err }
    exec, ok := r.byType[ds.Type()]
    if !ok { return nil, ErrUnsupportedDataSource }
    return exec.Execute(ctx, datasourceID, dsl, opts)
}
```

### Step 3 — 兼容门面

`MultiExecutor` 保留作为 deprecated 门面,内部改成调用 `ExecutorRegistry`,两个版本至少共存一个 minor release。

## 验收标准

- [ ] `DataSource` 接口增加 `Type() string`,所有现有实现都返回正确常量
- [ ] `ExecutorRegistry` 新增,接管路由职责
- [ ] `MultiExecutor` 改为内部转发,deprecated 注释指向 `ExecutorRegistry`
- [ ] 新增一个测试用 datasource(假装 ClickHouse),只需在 `init()` 里 `Register` 即可工作,**0 行修改 ExecutorRegistry**(开闭原则验证)
- [ ] 现有所有 portal / framework 调用点 0 改动(通过门面兼容)

## 测试要求

- 单测覆盖:
  - 已注册 type 路由正确
  - 未注册 type 返回 `ErrUnsupportedDataSource`
  - 未注册 ID 返回 `ErrNotFound`
  - 多 type 并发注册 race-free(`go test -race`)
- 集成测试: 用 noop 数据源 + 自定义 noop executor 验证完整链路

## 风险

- `DataSource` 接口扩展是 **breaking change**: 外部实现该接口的代码会编译错。**Mitigation**: 提供 `BasicDataSource` 嵌入式 struct + 默认 `Type()` 实现,外部只需嵌入即可
- `MultiExecutor` 字段被外部直接构造的情况(`MultiExecutor{MySQL: ..., Elasticsearch: ...}`)需要保留兼容 → 不删字段

## 关联 issue

- [DS-B5](DS-B5-metadata-executor-dispatcher.md): metadata / executor 公共 dispatcher,本 issue 完成后才能做
- [EX-A1](EX-A1-reader-writer-split.md): Reader / Writer 拆分,本 issue 完成后会更顺
