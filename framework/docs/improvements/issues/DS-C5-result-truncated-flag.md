# [DS-C5 / EX-C3] `Result` 缺 truncated 标记

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/executor |
| **状态** | 已完成 |
| **完成批次** | [2026-06-03-p0-quickwin-batch](../../superpowers/plans/2026-06-03-p0-quickwin-batch.md) |
| **关联报告** | [01-datasource.md C5](../01-datasource.md) / [02-executor.md C3](../02-executor.md) |
| **预估工作量** | 30 分钟(代码) + 1-2 小时(LLM 工具描述同步更新) |
| **依赖** | 无;但与 [EX-B2](EX-B2-maxrows-pushdown.md) 一起做更彻底 |

## 问题位置

- `framework/executor/executor.go: Result` 类型定义
- `framework/executor/mysql.go: execQuery`(MaxRows 截断点)
- `framework/executor/elasticsearch.go: execSearch`(同)
- `framework/executor/mongodb.go: Execute`(同)
- 上层 LLM 工具 `execute_read` 等的 description 与 prompt

## 现状

```go
type Result struct {
    Columns      []string
    Rows         [][]any
    AffectedRows int64
}

// MySQL execQuery 内
for rows.Next() {
    if maxRows > 0 && len(out.Rows) >= maxRows {
        break  // 静默截断
    }
    ...
}
```

LLM 看到 100 行就以为是全集,可能给出错误结论(如"统计 user 总数 = 100",而真实表有 100 万条)。

## 改进方案

```go
type Result struct {
    Columns      []string
    Rows         [][]any
    AffectedRows int64

    // Truncated 表示返回的 Rows 已被 MaxRows 截断,真实结果集更大
    Truncated bool

    // EstimatedTotal 给出真实结果集的估计大小(可填),不可填时为 0
    // ES: hits.total.value
    // MySQL: 无原生 total,可通过附加 SELECT COUNT(*) 但代价大,默认不填
    // Mongo: 同 MySQL 处理
    EstimatedTotal int64
}
```

各执行器在截断时设置:

```go
// MySQL
for rows.Next() {
    if maxRows > 0 && len(out.Rows) >= maxRows {
        out.Truncated = true
        break
    }
    ...
}

// ES
if maxRows > 0 && len(hits) > maxRows {
    hits = hits[:maxRows]
    out.Truncated = true
}
out.EstimatedTotal = parsedHitsTotal  // 从 ES response 解析

// Mongo
// Mongo 已经下推 SetLimit,但仍要标记: 如果 cursor.RemainingBatchLength() > 0 → Truncated = true
```

## 验收标准

- [ ] `Result` 新增 `Truncated bool` 与 `EstimatedTotal int64`
- [ ] MySQL / ES / Mongo 三个执行器在 MaxRows 触发截断时都正确置 `Truncated=true`
- [ ] ES 路径填充 `EstimatedTotal`(从 `hits.total.value` 解析)
- [ ] 上层 `execute_read` 工具的输出 schema / description 同步更新,告知 LLM 这两个字段的含义
- [ ] 现有调用方未读这两个新字段时**编译不破**(零值默认)

## 测试要求

- MySQL: `TestMySQLExecutor_Execute_Query_MaxRows` 已存在,扩展断言:
  ```go
  if !res.Truncated { t.Error("expected Truncated=true when MaxRows < total") }
  ```
- ES: 新增 `TestESExecutor_Truncated`,mock transport 返回 100 hits + total=10000,断言 Truncated=true 且 EstimatedTotal=10000
- Mongo: 新增 `TestMongoExecutor_Truncated`,使用 `mtest`

## 文档更新

- [ ] `framework/docs/api-reference.md` 中 `Result` 章节加新字段说明
- [ ] LLM 工具 `execute_read` 的 description 加一句:"如果返回 truncated=true,说明结果被截断,可加 WHERE / LIMIT 缩小范围或使用分页"

## 风险

- 现有调用方读 `Result` 时若用到反射 / JSON marshal,新字段会出现在输出中。需要 grep 一遍调用方确认
- 与 [EX-B2 MaxRows 下推] 是互补关系,本 issue 不依赖 B2 完成
