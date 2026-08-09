# [EX-A1] 拆分 Reader / Writer 接口,在类型层消除越权

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/executor、framework/tool |
| **状态** | 已完成 |
| **关联报告** | [02-executor.md A1](../02-executor.md) |
| **预估工作量** | 1 周(含工具层迁移) |
| **依赖** | [DS-B1](DS-B1-type-and-executor-registry.md) 完成后做更顺;[EX-A2](EX-A2-readonly-default-true.md) 是过渡方案,本 issue 是终态 |

## 问题位置

- `framework/executor/executor.go: Executor interface`
- `framework/executor/multi.go` / `mysql.go` / `elasticsearch.go` / `mongodb.go`
- `framework/tool/execute_read*` / `execute_write*` 工具

## 现状

```go
type Executor interface {
    Execute(ctx, ds, dsl, opts) (*Result, error)
}

type Result struct {
    Columns      []string
    Rows         [][]any
    AffectedRows int64    // 写操作影响行数
}
```

`Execute` 同时承接读写,各执行器内部用 `isWriteDSL` 判定路径。

## 问题分析

1. **每个实现都重复写 `isWriteDSL`** —— Mongo 干脆没写
2. **`Result` 形状混乱**: LLM 看到 `AffectedRows=0` 分不清是"写了 0 行"还是"读结果"
3. **类型层无法表达"只读"约束**: LLM 工具想限制权限只能靠运行时 flag

## 改进方案

### Step 1 — 接口拆分

```go
// framework/executor/reader.go (新)
type Reader interface {
    Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error)
}

type QueryOptions struct {
    Timeout time.Duration
    MaxRows int
    Params  map[string]any
}

type QueryResult struct {
    Columns        []string
    Rows           [][]any
    Truncated      bool
    EstimatedTotal int64
}

// framework/executor/writer.go (新)
type Writer interface {
    Exec(ctx context.Context, datasourceID string, dsl string, opts ExecOptions) (*ExecResult, error)
}

type ExecOptions struct {
    Timeout time.Duration
    Params  map[string]any
    DryRun  bool   // 解析但不执行,返回受影响行数估计
}

type ExecResult struct {
    AffectedRows int64
    LastInsertID int64
}
```

### Step 2 — 各执行器分裂

```go
type MySQLReader struct { Registry *datasource.Registry }
type MySQLWriter struct { Registry *datasource.Registry }
// ES 只实现 Reader(只读)
type ESReader struct { Registry *datasource.Registry }
// Mongo 同 ES,只读
type MongoReader struct { Registry *datasource.Registry }
```

写入安全靠"Mongo 没有 Writer 实现" → 编译期阻止。

### Step 3 — 兼容门面

`Executor` 保留作为兼容门面,内部按 DSL 类型分流到 Reader / Writer。`MultiExecutor` 同样保留。Deprecated 注释指向新接口。

### Step 4 — 工具层迁移

```go
// 旧:
type ExecuteReadTool struct { Exec executor.Executor }
// 新:
type ExecuteReadTool struct { Reader executor.Reader }   // 编译期保证只能 Query
```

## 验收标准

- [ ] 新 `Reader` / `Writer` 接口及 `QueryResult` / `ExecResult` 完整实现
- [ ] MySQL / ES / Mongo 各自分裂为 Reader 与(仅 MySQL)Writer
- [ ] 工具 `execute_read` 改用 `Reader`,`execute_write` 改用 `Writer`
- [ ] `Executor` / `MultiExecutor` 兼容门面保留至少一个 minor release
- [ ] 类型层验证: 把 `Mongo` 实例传给需要 `Writer` 的代码 → 编译错误

## 测试要求

- 现有 MySQL / ES / Mongo 的所有单测通过新接口跑一遍
- 新增工具层测试: `execute_read` 拿到一个真实 Writer 实例时编译失败(可用 `go vet` 自定义 lint)
- ExecResult 有 LastInsertID 时正确返回(MySQL `LastInsertId()`)

## 风险

- **Breaking change**: 大改动,涉及框架核心接口
- **Mitigation**:
  1. 兼容门面保留 ≥ 1 个 minor release
  2. 提供 migration guide
  3. portal 等内部 caller 提前接入新接口验证
- 旧代码 `Result.AffectedRows` 字段调用方需 grep 一遍

## 关联 issue

- [EX-A2](EX-A2-readonly-default-true.md): 过渡方案,先把默认值改对;本 issue 是终态
- [EX-D3](EX-D3-parameterized-queries.md): 参数化查询,新 `QueryOptions.Params` 同步实现
- [DS-B1](DS-B1-type-and-executor-registry.md): ExecutorRegistry,基于 type 路由,与 Reader/Writer 拆分一起做最干净
