# [EX-A3] ES 执行器 columns 取首行 + map 顺序不稳定

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/executor |
| **状态** | 已完成 |
| **完成批次** | [2026-06-03-p0-quickwin-batch](../../superpowers/plans/2026-06-03-p0-quickwin-batch.md) |
| **关联报告** | [02-executor.md A3](../02-executor.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/executor/elasticsearch.go: (*ESExecutor).execSearch` 内 columns 收集块

## 现状

```go
columns := []string{}
if len(hits) > 0 {
    seen := make(map[string]struct{})
    for k := range hits[0].Source { seen[k] = struct{}{} }   // 只看首行
    for k := range seen { columns = append(columns, k) }     // map 遍历顺序
    for _, h := range hits {
        row := make([]any, len(columns))
        for i, col := range columns {
            row[i] = h.Source[col]
        }
        rows = append(rows, row)
    }
}
```

## 问题分析

1. **首行决定 columns**: ES 文档异构,如果首行无 `error_code` 字段而后续行有,**该字段直接丢失** —— 用户看不到
2. **map 遍历顺序未定义**: 每次 columns 顺序都不同,LLM 看到的列序漂移,prompt 缓存命中率下降
3. **小问题**: `columns/rows` 从 nil append,大 result 时多次 reallocation

## 改进方案

```go
var rows [][]any
var columns []string

if len(hits) > 0 {
    // 1. 收集所有行的 key union
    keySet := make(map[string]struct{}, 16)
    for _, h := range hits {
        for k := range h.Source { keySet[k] = struct{}{} }
    }

    // 2. deterministic 排序: 优先级 _id > _score > _index > 字母序其他
    columns = make([]string, 0, len(keySet))
    priorityCols := []string{"_id", "_score", "_index"}
    for _, p := range priorityCols {
        if _, ok := keySet[p]; ok {
            columns = append(columns, p)
            delete(keySet, p)
        }
    }
    rest := make([]string, 0, len(keySet))
    for k := range keySet { rest = append(rest, k) }
    sort.Strings(rest)
    columns = append(columns, rest...)

    // 3. 构 rows
    rows = make([][]any, 0, len(hits))
    for _, h := range hits {
        row := make([]any, len(columns))
        for i, col := range columns {
            row[i] = h.Source[col]   // 缺字段为 nil
        }
        rows = append(rows, row)
    }
}
```

另外,要在响应解析中顺便提取 `_id` / `_score` / `_index`(原代码只取 `_source`):
```go
type esHit struct {
    ID     string                 `json:"_id"`
    Score  float64                `json:"_score"`
    Index  string                 `json:"_index"`
    Source map[string]interface{} `json:"_source"`
}
```
然后在构 row 时,如果 col 是 priorityCols 之一,从 hit 顶层字段取,而非 `Source`。

## 验收标准

- [ ] columns 取所有 hits 的 key union
- [ ] 排序确定:`_id` / `_score` / `_index` 优先,其余字母序
- [ ] 异构文档不丢字段(后行多出的字段会出现在 columns 中)
- [ ] 多次相同输入返回的 columns 顺序完全一致(LLM 缓存命中率不漂移)

## 测试要求

新增 `TestESExecutor_HeterogeneousColumns`(用 `httptest` mock):
```go
// hits[0] = {"name": "a"}
// hits[1] = {"name": "b", "error_code": 500}
// expected columns = ["_id", "_score", "_index", "error_code", "name"](顺序固定)
// expected rows[0] = [..., nil, "a"]
// expected rows[1] = [..., 500, "b"]
```

新增 `TestESExecutor_StableColumnOrder`:
- 同一 input 跑 100 次,所有 result.Columns 切片必须 `reflect.DeepEqual`

## 风险

- 改了 columns 顺序是行为变化,但当前顺序本身就不稳定,**不算 breaking change**
- LLM 工具描述里如果暗示了字段顺序,需要同步更新
