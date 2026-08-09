# [DS-A3] MySQL DSN 明文密码 + 日志直接打印 DSL

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/datasource、framework/executor |
| **状态** | 已完成 |
| **关联报告** | [01-datasource.md A3](../01-datasource.md) |
| **预估工作量** | 1 天 |
| **依赖** | 无 |

## 问题位置

- `framework/executor/mysql.go: (*MySQLExecutor).Execute` 起始处的 `log.Printf("exe sql: %s", dsl)`
- `framework/executor/elasticsearch.go: (*ESExecutor).Execute` 起始处的 `log.Printf("elasticsearch dsl %s", dsl)`
- `framework/datasource/mysql.go: NewMySQLDataSource` 中 DSN 字符串拼接

## 现状

```go
// executor/mysql.go
log.Printf("exe sql: %s", dsl)

// datasource/mysql.go
dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?...", cfg.User, cfg.Password, cfg.Host, port, cfg.DBName)
```

## 问题分析

1. **DSL 含敏感字面量**: `WHERE token='xxxx'`、`WHERE email='a@b.com'`、`INSERT ... VALUES('身份证号')` 都会原样落入日志
2. **日志走 stdlib `log`,不经 `obs/logger`**: 没结构化字段,无采样,压测会爆,过滤困难
3. **DSN 拼接密码**: 出错时某些驱动会把 DSN 反回栈帧,密码可能进 error 日志

## 改进方案

### A. 日志侧
1. 引入 `framework/obs/logger`(应已存在,需复用),用 `slog.Logger` 实例
2. 在 `MySQLExecutor` / `ESExecutor` 增加 `Logger *slog.Logger` 字段(nil-safe)
3. DSL 输出降级为 `Debug` level
4. 提供 `MaskLiterals(sql string) string` 辅助函数:
   - 单引号字符串字面量 `'xxxx'` → `'***'`
   - 数字字面量保留(不影响调试)
5. Info level 日志只输出: `datasource=`, `op=`, `duration_ms=`, `rows=`, `truncated=`, `status=`(成功/失败/拒绝)

### B. DSN 侧
1. `NewMySQLDataSource` 改用 `mysql.Config{}` 构造再 `FormatDSN()`
2. 错误信息禁止包含 DSN: `return nil, fmt.Errorf("mysql open failed for host=%s db=%s", cfg.Host, cfg.DBName)`(明确不带密码)

## 验收标准

- [ ] `MySQLExecutor` 与 `ESExecutor` 都接受可选 `Logger *slog.Logger`,注入后所有 `log.Printf` 调用消失
- [ ] Info 级别日志中,DSL/SQL 字面量被 mask(用 grep 验证: `grep -r "WHERE [a-z_]*=" .` 在测试日志里只看到 `'***'`)
- [ ] `NewMySQLDataSource` 失败错误中**不含密码**(单测断言 `!strings.Contains(err.Error(), cfg.Password)`)
- [ ] DSN 构造改用 `mysql.Config.FormatDSN()`,`go vet` 无警告

## 测试要求

- 单测: `MaskLiterals` 表驱动覆盖
  - `SELECT * FROM users WHERE token='abc123'` → `... WHERE token='***'`
  - `INSERT INTO logs VALUES (1, 'msg', 'secret')` → `... VALUES (1, '***', '***')`
  - 字符串内含 `\'` 转义不被截断
  - `SELECT 1` 不变
- 单测: DSN 错误信息断言
- 集成测试: 启动一次 MySQL executor + 一段含 token 字面量的查询,检查 stdout/stderr 不含原始 token

## 风险

- mask 太激进会损害 debug 体验 → 仅对 `info` 及以上 mask,`debug` 输出原样
- 项目可能有别的地方也用 `log.Printf`,本 issue 范围仅限 datasource / executor 三个文件,其余跟随 [DS-C4 / EX-C2](./) 统一改造

## 关联 issue

- [EX-C1](EX-C1-log-printf-replace.md): executor 层 logger 替换(本 issue 子集)
- [MW-B4](MW-B4-logging-slog.md): middleware logging 替换(同模式不同模块)
- 三个 issue 建议同一个 PR 提交,避免日志风格半新半旧
