# [EX-D3] 参数化查询作为一等公民

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/executor、framework/tool |
| **状态** | 已完成 |
| **关联报告** | [02-executor.md D3](../02-executor.md) |
| **预估工作量** | 1 周(含 LLM 工具 prompt 改造 + 单测) |
| **依赖** | 与 [EX-A1](EX-A1-reader-writer-split.md) 一起做最自然 |

## 问题位置

- `framework/executor/executor.go: ExecuteOptions.Params`
- `framework/executor/mysql.go: execQuery` / `execWrite`
- LLM 工具 `execute_read` / `execute_write` 的 prompt

## 现状

```go
type ExecuteOptions struct {
    ...
    Params map[string]any   // 字段已有,但 MySQL 路径完全没用上
}

// MySQL execQuery
rows, err := db.QueryContext(ctx, dsl)   // 不传 args
```

`Params` 字段当前**只在 ES 路径取了 `index` 字段**,MySQL / Mongo 完全忽略。

## 问题分析

1. **SQL 注入风险**: LLM 输出的 dsl 是字符串拼接结果,只能靠 `ReadOnly` 兜底,**没有参数绑定**
2. **缓存命中率低**: prompt 里 user_id=123 vs user_id=456 是不同字符串,DB 查询 plan 也是新的
3. **可读性差**: LLM 看见 `WHERE user_id='123'` 不会主动归纳,看见 `WHERE user_id = ?` + params 才能学到结构

## 改进方案

### Step 1 — `QueryOptions` 强类型化

```go
type QueryOptions struct {
    Timeout time.Duration
    MaxRows int

    // PositionalParams 用于 ? 占位符 (MySQL)
    PositionalParams []any

    // NamedParams 用于 :name 占位符 (兼容 sqlx 风格)
    NamedParams map[string]any

    // 其他后端特有参数(如 ES "index", Mongo "collection" 字段)
    Extras map[string]any
}
```

### Step 2 — MySQL 接 prepared statement

```go
func (e *MySQLReader) Query(ctx, ds, dsl string, opts QueryOptions) (*QueryResult, error) {
    ...
    args := opts.PositionalParams
    if len(opts.NamedParams) > 0 {
        // 把 :name 转成 ?,args 按顺序填充
        dsl, args = bindNamed(dsl, opts.NamedParams)
    }
    rows, err := db.QueryContext(ctx, dsl, args...)
    ...
}
```

### Step 3 — LLM 工具描述改造

```yaml
execute_read:
  description: |
    Execute a read-only SQL query. Use parameterized queries with ? placeholders
    when possible — this is safer and improves cache hit rate.

    Good:
      dsl: "SELECT * FROM users WHERE id = ? AND status = ?"
      params: [123, "active"]

    Bad (still works, but discouraged):
      dsl: "SELECT * FROM users WHERE id = 123 AND status = 'active'"
  parameters:
    dsl: { type: string }
    positional_params: { type: array, items: any }
```

### Step 4 — 参数化白名单(可选,激进)

未来可考虑:**只接受参数化 SQL**,字面量含字符串 / 数字必须改成 `?`。这是最高安全级别,可作为 P2 项做。

## 验收标准

- [ ] `QueryOptions` 含 `PositionalParams` / `NamedParams`
- [ ] MySQL Reader 在传 params 时走 `QueryContext(ctx, dsl, args...)` 路径
- [ ] LLM 工具 `execute_read` 描述更新,prompt 中给出参数化示例
- [ ] 集成测试:相同结构 + 不同参数,DB query plan cache 命中率提升(可通过 `SHOW STATUS LIKE 'Qcache%'` 观察)
- [ ] 反例测试:LLM 输出含字符串拼接的 SQL 仍能工作(向后兼容)

## 测试要求

- `TestBindNamed`: `:user_id` → `?` 的转换正确,转义引号
- `TestMySQLReader_Parameterized`: 用 sqlmock 验证发出的是 prepared statement
- `TestMySQLReader_NoParams`: 不传 params 时退化到原有路径
- 安全测试: 用经典注入 payload `' OR 1=1 --` 作为 param 值,确认 server 端解析为字面量而非 SQL

## 风险

- LLM 不一定愿意输出参数化形式,需要 prompt 设计配合
- ES / Mongo 的"参数化"语义不同(ES `params` 是 painless script,Mongo 直接是 BSON),需要分别设计
- `bindNamed` 实现要小心字符串字面量内的 `:name` 不替换

## 关联 issue

- [EX-A1](EX-A1-reader-writer-split.md): Reader 接口拆分,本 issue 在新 `QueryOptions` 上加字段
- [EX-B2](EX-B2-maxrows-pushdown.md): MaxRows 下推可与参数化合并到同一 Query 流程
