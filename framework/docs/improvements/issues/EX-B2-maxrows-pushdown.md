# [EX-B2] `MaxRows` 下推到 SQL `LIMIT` / ES `size`

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/executor |
| **状态** | 已完成 |
| **关联报告** | [02-executor.md B2](../02-executor.md) |
| **预估工作量** | 1-2 天 |
| **依赖** | 无 |

## 问题位置

- `framework/executor/mysql.go: execQuery`
- `framework/executor/elasticsearch.go: execSearch`

## 现状

- **MySQL**: `SELECT * FROM big_table` + `MaxRows=10` → 仍然从 server 拉全表,客户端 `break`(网络 + 内存依旧爆)
- **ES**: body 里的 `size` 不被覆盖,只在客户端切片(server 仍取 10000 doc)
- **Mongo**: 已经下推(`SetLimit(MaxRows)`) —— 只它做对了

## 问题分析

`MaxRows` 的初衷是"防 LLM 拉爆数据"。当前实现是**事后裁切**,只防了 LLM 上下文,**没防 DB / 网络 / 内存**。大表场景一查就 OOM。

## 改进方案

### MySQL

加子查询包裹,不破坏聚合 / order:
```go
func wrapWithLimit(dsl string, maxRows int) string {
    if maxRows <= 0 { return dsl }
    if hasLimitClause(dsl) { return dsl }    // 已有 LIMIT 不重复加
    return fmt.Sprintf("SELECT * FROM (%s) AS _limited LIMIT %d", dsl, maxRows)
}

func hasLimitClause(dsl string) bool {
    upper := strings.ToUpper(strings.TrimSpace(dsl))
    // 简单粗暴:末尾 LIMIT N(忽略注释、字符串字面量)
    // 严谨方案:用 SQL parser
    re := regexp.MustCompile(`(?i)\bLIMIT\s+\d+(\s*,\s*\d+)?\s*;?\s*$`)
    return re.MatchString(upper)
}
```

或者更安全:**强制要求 LLM 在 SQL 中显式带 LIMIT**,`isWriteDSL` 后增加 `requireLimit` 校验,缺则报 `LimitRequiredError`,提示 LLM 加。

### ES

解析 body JSON,注入或 clamp `size`:
```go
func injectSize(body string, maxRows int) (string, error) {
    if maxRows <= 0 { return body, nil }
    var m map[string]interface{}
    if err := json.Unmarshal([]byte(body), &m); err != nil {
        return "", err
    }
    sz, ok := m["size"].(float64)
    if !ok || int(sz) > maxRows {
        m["size"] = maxRows
    }
    out, _ := json.Marshal(m)
    return string(out), nil
}
```

注意: ES 的 `size` 默认 10,但 LLM 通常不会显式设。**如果 caller 没设 MaxRows 也没在 body 里设 size,应该塞一个保守默认值(如 100)**,避免误返回 10 条让 LLM 误判全集。

### 双保险

下推 + 客户端兜底裁切**都要保留**(防止驱动 / server 行为异常)。

## 验收标准

- [ ] MySQL `execQuery` 在传 `MaxRows>0` 且 dsl 无 LIMIT 时,真实发往 server 的 SQL 含 LIMIT
- [ ] MySQL 已有 LIMIT 时不重复包裹
- [ ] ES `execSearch` 在传 `MaxRows>0` 时,真实发往 server 的 body 含 `"size":N`
- [ ] ES body 已有更小的 size 时不被覆盖(只 clamp 上限)
- [ ] 客户端裁切逻辑保留作为兜底
- [ ] 大表(模拟 100 万行)+ MaxRows=10 测试: server 端实际只查询 10 条(检查 MySQL slow log 或 ES `_search?profile=true`)

## 测试要求

- MySQL: `TestMySQLExecutor_PushdownLimit` 表驱动:
  - `SELECT * FROM users` + MaxRows=10 → 实际 SQL `SELECT * FROM (SELECT * FROM users) AS _limited LIMIT 10`
  - `SELECT * FROM users LIMIT 5` + MaxRows=10 → 不变
  - `SELECT * FROM users LIMIT 100` + MaxRows=10 → 当前不处理(子查询包裹会让外层 LIMIT 生效)
- `hasLimitClause` 表驱动单测:覆盖 `LIMIT 10`、`LIMIT 10,5`、`LIMIT 10 ;`、字符串内含 `LIMIT` 不误判
- ES: `TestESExecutor_InjectSize` mock transport 验证发出的 body 含 `"size"`

## 风险

- 子查询包裹**会破坏某些复杂 SQL 语义**(如 `FOR UPDATE`、`INTO OUTFILE`、`UNION ALL` 末尾 LIMIT)。Mitigation: 检测这类语句直接拒绝包裹,报错让 LLM 加 LIMIT
- ES `size` 与 `from` 分页冲突时优先 `size`

## 关联 issue

- [DS-C5 / EX-C3](DS-C5-result-truncated-flag.md): 与 Truncated 标记互补
