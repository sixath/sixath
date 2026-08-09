# [DS-A1] MySQL `isWriteDSL` 关键字前缀判定可被绕过

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/datasource(逻辑实际在 executor/mysql.go) |
| **状态** | 已完成 |
| **关联报告** | [01-datasource.md A1](../01-datasource.md) |
| **预估工作量** | 入门方案 0.5 天;parser 方案 2-3 天 |
| **依赖** | 无(独立改) |

## 问题位置

- `framework/executor/mysql.go: isWriteDSL`
- `framework/executor/mysql.go: (*MySQLExecutor).Execute` 调用点

## 现状

```go
func isWriteDSL(dsl string) bool {
    s := strings.TrimSpace(dsl)
    if s == "" { return false }
    upper := strings.ToUpper(s)
    for _, prefix := range []string{
        "INSERT", "UPDATE", "DELETE", "REPLACE",
        "CREATE", "DROP", "ALTER", "TRUNCATE", "RENAME",
    } {
        if upper == prefix || strings.HasPrefix(upper, prefix+" ") || ... {
            return true
        }
    }
    return false
}
```

## 问题分析

绕过场景:
- 前导注释: `/* hint */ DELETE FROM t` —— **绕过**
- 多语句: `SELECT 1; DELETE FROM t` —— **绕过**(若 DSN 含 `multiStatements=true` 立刻失守)
- CTE 写: `WITH cte AS (...) DELETE FROM t USING cte` —— **绕过**
- 存储过程: `CALL p_delete_user(...)` —— **绕过**
- 漏列: `MERGE`、`HANDLER`、`LOAD DATA`、`LOCK TABLES`、`SET`、`GRANT`

LLM 输出可控性是 Agent 平台的核心安全边界,任意一条绕过都会让"只读"形同虚设。

## 改进方案

**分阶段实施**,推荐先做 Step 1(0.5 天)上线,Step 2 / 3 作为长期演进。

### Step 1 — 入门防线(本 issue 必须包含)
1. 入口剥离 SQL 注释: `/* */`(块) + `--`(行)
2. 拒绝多语句: trim 后若含 `;` + 后续非空字符,直接 `ErrUnsupportedSyntax`
3. DSN 默认禁用 `multiStatements`(在 `datasource/mysql.go: NewMySQLDataSource` 强制覆盖)

### Step 2 — 加密关键字白名单
改"判定写"为"只放行 SELECT/SHOW/DESCRIBE/DESC/EXPLAIN":
```go
func isReadDSL(dsl string) bool { ... }  // 白名单优先于黑名单
```

### Step 3 — Parser 方案(可选)
用 `vitess/sqlparser` 或 `pingcap/parser` 解析 AST,基于 statement 类型判定。

### 配套
- 服务端兜底: 默认 `?readOnly=true` + 会话级 `SET SESSION TRANSACTION READ ONLY`(若 MySQL 5.6+)

## 验收标准

- [ ] `isWriteDSL` 对以下 case 全部正确判定为写:
  - `/* hint */ DELETE FROM t`
  - `-- comment\nDELETE FROM t`
  - `WITH cte AS (...) DELETE FROM t USING cte`
  - `CALL sp_delete()`
  - `LOAD DATA INFILE ...`
  - `LOCK TABLES t WRITE`
- [ ] 对 `SELECT 1; DELETE FROM t` 返回 `ErrUnsupportedSyntax`(不是误判)
- [ ] DSN 在 `NewMySQLDataSource` 中**强制移除** `multiStatements=true`,即使配置传入也覆盖
- [ ] 现有合法 SELECT / SHOW / DESCRIBE / EXPLAIN 用例 0 回归

## 测试要求

- 表驱动单测覆盖上述全部 case(放 `mysql_test.go`)
- 增加 fuzzing 测试: `go test -fuzz=FuzzIsWriteDSL`,运行 5 分钟无 panic 且无误判
- benchmark 对比改动前后 `isWriteDSL` 的开销(应 < 500ns/op)

## 风险

- **CTE 读路径**(`WITH cte AS (SELECT ...) SELECT ...`)是合法的,白名单方案要小心放过
- 注释剥离要处理转义字符串: `'/* not a comment */'` 在字符串字面量内不能误删
- 推荐用现成 SQL tokenizer 而不是自行 regex,**避免 ReDoS**
