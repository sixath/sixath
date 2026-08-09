# [EX-A4] schema-error 判定改用错误码而非子串匹配

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/executor |
| **状态** | 已完成 |
| **完成批次** | [2026-06-03-p0-quickwin-batch](../../superpowers/plans/2026-06-03-p0-quickwin-batch.md) |
| **关联报告** | [02-executor.md A4](../02-executor.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/executor/mysql.go: isMySQLSchemaRelated`
- `framework/executor/elasticsearch.go: isESSchemaRelated`

## 现状

```go
// MySQL
func isMySQLSchemaRelated(err error) bool {
    s := err.Error()
    return strings.Contains(s, "Unknown column") ||
        strings.Contains(s, "1054") ||
        strings.Contains(s, "42S22")
}

// ES
func isESSchemaRelated(errMsg string) bool {
    return strings.Contains(errMsg, "No mapping found") ||
        strings.Contains(errMsg, "unknown field") ||
        strings.Contains(errMsg, "field_unknown") ||
        strings.Contains(errMsg, "strict_dynamic_mapping")
}
```

## 问题分析

1. **MySQL**: 子串 `"1054"` 会被任意含数字 1054 的字面量误命中(如 `WHERE port=1054` 报别的错)
2. **MySQL**: 驱动 locale 化(中文 `"未知列"`)直接失效
3. **ES**: 错误消息是 server 拼接的 JSON,直接做 `strings.Contains` 在 server 改文案时就破

## 改进方案

### MySQL: 使用 `*mysql.MySQLError.Number`

```go
import sqldriver "github.com/go-sql-driver/mysql"

// MySQL schema-related error codes
const (
    mysqlErrUnknownColumn   = 1054 // ER_BAD_FIELD_ERROR
    mysqlErrUnknownTable    = 1051 // ER_BAD_TABLE_ERROR
    mysqlErrNoSuchTable     = 1146 // ER_NO_SUCH_TABLE
    mysqlErrUnknownDatabase = 1049 // ER_BAD_DB_ERROR
)

func isMySQLSchemaRelated(err error) bool {
    var me *sqldriver.MySQLError
    if !errors.As(err, &me) { return false }
    switch me.Number {
    case mysqlErrUnknownColumn, mysqlErrUnknownTable, mysqlErrNoSuchTable, mysqlErrUnknownDatabase:
        return true
    }
    return false
}
```

### ES: 解析 response body 的 `error.type`

ES 错误响应是 JSON:
```json
{
  "error": {
    "type": "query_shard_exception",
    "reason": "No mapping found for [foo] in order to sort on",
    "root_cause": [...]
  },
  "status": 400
}
```

应解析 JSON 而非匹配字符串:
```go
type esErrorBody struct {
    Error struct {
        Type   string `json:"type"`
        Reason string `json:"reason"`
    } `json:"error"`
}

// schema-related ES error types
var esSchemaErrorTypes = map[string]struct{}{
    "query_shard_exception":      {},   // 包含 No mapping found
    "index_not_found_exception":  {},
    "mapper_parsing_exception":   {},
    "strict_dynamic_mapping_exception": {},
}

func isESSchemaRelatedFromBody(body io.Reader) bool {
    var b esErrorBody
    if err := json.NewDecoder(body).Decode(&b); err != nil { return false }
    _, ok := esSchemaErrorTypes[b.Error.Type]
    return ok
}
```

**注意**: ES 当前在 `res.IsError()` 后用 `res.String()` 拼字符串,要改成保留 body 的 `[]byte` 副本,既给用户错误消息也给 schema 判定。

### 公共 helper 抽取(对应 EX-C7)

把 `wrapMaybeSchemaRelated` 与 `wrapESMaybeSchemaRelated` 合并成:
```go
func wrapIf(err error, isSchemaErr func() bool, format string, args ...any) error {
    wrapped := fmt.Errorf(format, args...)
    if isSchemaErr() { return &SchemaRelatedError{Err: wrapped} }
    return wrapped
}
```

## 验收标准

- [ ] MySQL `isMySQLSchemaRelated` 使用 `*mysql.MySQLError.Number`,table-driven 单测覆盖 1054 / 1051 / 1146 / 1049
- [ ] MySQL 误判用例不再触发: `WHERE port=1054` 报别的错时返回 false
- [ ] ES `isESSchemaRelated` 改用 `error.type` 判定
- [ ] ES locale / 文案变更不影响判定(测试:把 reason 改成中文,error.type 不变,仍正确判定)
- [ ] 公共 helper `wrapIf` 抽出,MySQL / ES 各自调用

## 测试要求

- MySQL 单测: 用 `mysql.MySQLError{Number: 1054, Message: "..."}` 直接构造
- ES 单测: 用 `httptest` 返回 schema 错误 JSON,断言被正确归类为 `SchemaRelatedError`
- 反例:用一个 `Number=2002`(连接错误)的 MySQLError,断言**不**归类为 schema

## 风险

- 现有调用方依赖 `errors.As(err, &SchemaRelatedError{})` 行为不变 → 安全
- 部分 ES 错误可能落入新增的 type 之外,需要在 `esSchemaErrorTypes` 持续维护
