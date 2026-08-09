# Datasource 数据源适配层 改进报告

> 模块: `framework/datasource/`
> 评审日期: 2026-06-03
> 综合评分: **6.5 / 10**

## 模块定位

提供按 ID 索引的数据源注册中心,屏蔽 MySQL / Elasticsearch / MongoDB / Hive 等异构后端的连接细节。`executor` 层和 `metadata` 层都依赖此层拿原生 client。

## 评分

| 维度 | 分数 | 备注 |
|------|------|------|
| 抽象设计 | 8 / 10 | 能力接口 + Factory + 错误归一化都很有水平 |
| 可扩展性 | 6 / 10 | MultiExecutor 类型断言路由随后端数量线性恶化 |
| 安全性 | 4 / 10 | 只读拦截可被绕过,ES 写判定误伤严重,日志/DSN 信息泄漏 |
| 健壮性 | 5 / 10 | Close 缺超时、重复 ID 静默覆盖、Hive 死代码 |
| 可观测性 | 3 / 10 | obs 包已就绪但 datasource 几乎零埋点 |
| 测试 | 6 / 10 | registry / mysql / inmemory 都有,执行层覆盖单薄 |

---

## 🔴 A 严重 — 影响正确性 / 安全

### A1. `isWriteDSL` 用前缀关键字判断,绕过点太多

**位置**: `framework/executor/mysql.go: isWriteDSL`

**现状**:
```go
for _, prefix := range []string{"INSERT", "UPDATE", "DELETE", "REPLACE", "CREATE", ...} {
    if upper == prefix || strings.HasPrefix(upper, prefix+" ") || ...
}
```

**绕过场景**:
- 前导注释: `/* hint */ DELETE FROM t`
- 多语句: `SELECT 1; DELETE FROM t`(DSN 加 `multiStatements=true` 立刻失守)
- CTE 写: `WITH cte AS (...) DELETE FROM t USING cte`
- 存储过程: `CALL p_delete_user(...)`
- 漏列: `MERGE` / `HANDLER` / `LOAD DATA` / `LOCK TABLES` / `SET` / `GRANT`

**改进**:
1. 优先用 DSN 强制 `?readOnly=true` + 会话级 `SET SESSION TRANSACTION READ ONLY`
2. DSL 解析改用轻量 SQL parser(`vitess/sqlparser` / `pingcap/parser`)做白名单
3. 至少在前缀判定前 **剥离注释 + 拒绝多语句**

**优先级**: P0
**工作量**: 入门方案(剥注释 + 拒绝多语句)0.5 天;parser 方案 2-3 天

---

### A2. ES 的写判定靠**字符串关键词**,误判率极高

**位置**: `framework/executor/elasticsearch.go: isESWriteDSL`

**现状**:
```go
if strings.Contains(s, `"index"`) || strings.Contains(s, `"update"`) || strings.Contains(s, `"delete"`) || ...
```

任意 query body 含 `"index"` 字段名就被误判为写,例如 `{"query":{"term":{"index":"foo"}}}` —— **功能性 bug**。
而 `Execute` 实际只调 `Search` API,真实写请求(Bulk、Update by Query)根本走不到这里。

**改进**: 删除整个 `isESWriteDSL`,改为"只允许走 `_search` API"作为兜底。

**优先级**: P0
**工作量**: 30 分钟

---

### A3. MySQL DSN 明文密码 + 日志直接打印 DSL

**位置**: `framework/executor/mysql.go: Execute` 起始处 + `framework/datasource/mysql.go: NewMySQLDataSource`

**现状**:
```go
log.Printf("exe sql: %s", dsl)            // executor/mysql.go
dsn = fmt.Sprintf("%s:%s@tcp(...)", ...)  // 密码明文进字符串
```

**风险**:
- DSL 含 `WHERE token='xxxx'` 等敏感字面量会落日志
- 失败错误某些驱动会把 DSN 反回栈

**改进**:
1. 日志统一过 `obs/logger`,提供 `SQL=` 字段级 mask + 截断
2. DSN 改用 `mysql.Config{}` 构造再 `FormatDSN()`,出错时只暴露 Host/DBName
3. 配套加 mask helper: 字符串字面量 `'[^']*'` → `'***'`(debug 以外的级别)

**优先级**: P0
**工作量**: 1 天

---

### A4. ES 数据源 `Close()` 是空操作

**位置**: `framework/datasource/elasticsearch.go: (*esDataSource).Close`

**现状**:
```go
func (e *esDataSource) Close() error { return nil }
```

Registry 假设 `Close()` 释放资源,但 ES 客户端持有 HTTP transport(keep-alive 连接、TLS session cache)。业务做 hot reload 时**会泄连接**。

**改进**:
```go
func (e *esDataSource) Close() error {
    if t, ok := http.DefaultTransport.(*http.Transport); ok {
        t.CloseIdleConnections()
    }
    return nil
}
```
或在 `NewElasticsearchDataSource` 里显式传入自定义 `http.Transport`,Close 时关掉它。

**优先级**: P1
**工作量**: 0.5 天

---

## 🟠 B 中等 — 影响可扩展性 / 可维护性

### B1. `MultiExecutor` 用类型断言路由 = O(N) if-else 链

**位置**: `framework/executor/multi.go: (*MultiExecutor).Execute`

**现状**:
```go
if _, ok := ds.(sqlDBProvider); ok { return e.MySQL... }
if _, ok := ds.(datasource.ESClientProvider); ok { return e.Elasticsearch... }
if _, ok := ds.(datasource.MongoDatabaseProvider); ok { return e.Mongo... }
```

新增后端(ClickHouse / Postgres / Redis)必须同时改适配器 + MultiExecutor —— **违反开闭原则**。

**改进**: 把执行器也注册到 registry 里,按 `DataSource.Type()` 索引(配合 B2):
```go
type ExecutorFactory func(ds DataSource) Executor
// Registry.RegisterExecutorType("mysql", ...)
// (*MultiExecutor).Execute 只剩 e.byType[ds.Type()].Execute(...)
```

**优先级**: P1
**工作量**: 2-3 天(含迁移现有 3 个执行器 + 单测)

---

### B2. `DataSource` 接口缺 `Type()` 与 `Driver()`

**位置**: `framework/datasource/datasource.go: DataSource interface`

**现状**: 注册时 `cfg.Type` 是有的,但运行时 `Registry.Get(id)` 拿不到类型信息,只能反过来类型断言。

**改进**: 接口增加 `Type() string`,各实现返回 `"mysql"` / `"elasticsearch"` / `"mongodb"`。配套 B1 一起改最划算。

**优先级**: P1
**工作量**: 0.5 天

---

### B3. `ConfigFromMap` 不读连接池参数与 TLS

**位置**: `framework/datasource/datasource.go: ConfigFromMap`

**现状**:
```go
// MaxOpenConns / MaxIdleConns / ConnMaxLifetime / TLS / ReadOnly 都没从 map 中取
```

`mysqlDataSource` 已支持这些字段 —— 也就是说**从 portal 通过 map 配置进来的数据源永远拿不到调优参数**,默认 0 = 无限制,生产风险。

**改进**: 补 8-10 行解析代码,加 `max_open_conns` / `max_idle_conns` / `conn_max_lifetime_sec` / `read_only`。

**优先级**: P0
**工作量**: 30 分钟

---

### B4. Hive 适配器是死代码 + 缺驱动注册校验

**位置**: `framework/datasource/hive.go`

**现状**:
```go
db, err := sql.Open("hive", dsn)  // "hive" 驱动从未在项目任何地方注册
```

**改进**: 二选一
1. **删除整个 hive.go**(项目实际未使用)
2. 保留但在 `Register` 前检查 `sql.Drivers()` 包含 `"hive"`,给 "driver not registered" 明确错误

**优先级**: P2
**工作量**: 删除 10 分钟;补校验 0.5 小时

---

### B5. 元数据(`metadata`)和执行器(`executor`)各自独立持有 Registry

**位置**: `framework/metadata/*.go` 与 `framework/executor/*.go` 对称结构

**现状**: 两套并行的"按 ID 查 → 类型断言 → 走对应实现"逻辑,几乎对称。

**改进**: 抽公共 dispatcher(基于 B1 的 by-type registry),metadata / executor 共用 → 代码量直接砍 30%+。

**优先级**: P1
**工作量**: 在 B1 完成后追加 1-2 天

---

## 🟡 C 较小但应做

### C1. `Registry.Register` 没去重,重复 ID 静默覆盖

**位置**: `framework/datasource/registry.go: (*Registry).Register`

**现状**:
```go
r.sources[cfg.ID] = ds   // 旧 ds 没 Close,直接泄漏
```

**改进**: ID 重复 → 返回 `ErrDuplicateID`,或显式调旧 `ds.Close()` 后替换(策略 via 参数)。

**优先级**: P2
**工作量**: 0.5 小时

---

### C2. `mongoDataSource.Close()` 用 `context.Background()`,无超时

**位置**: `framework/datasource/mongodb.go: (*mongoDataSource).Close`

**现状**:
```go
return m.db.Client().Disconnect(context.Background())
```

**风险**: 网络分区时**卡住进程优雅关闭**。

**改进**: 5s 超时 context:
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
return m.db.Client().Disconnect(ctx)
```

**优先级**: P2
**工作量**: 10 分钟

---

### C3. ES 强制 `http://` 前缀,不支持 cloud_id / API key

**位置**: `framework/datasource/elasticsearch.go: NewElasticsearchDataSource`

**现状**:
```go
if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
    addr = "http://" + addr   // 明文兜底
}
```

**问题**:
- 接 Elastic Cloud 必须用 `CloudID` + `APIKey`,目前完全没接
- 默认拼 `http://` 而非 `https://` 是反向的安全默认值

**改进**:
1. `Config` 增加 `CloudID` / `APIKey` 字段
2. 默认协议改 `https://`,要 `http://` 必须显式声明
3. 文档说明配置示例

**优先级**: P2
**工作量**: 1 天

---

### C4. 没有连接 / 查询级指标

**位置**: `framework/datasource/*` + `framework/executor/*`

**现状**: `obs/` 包有 Prom 客户端但 datasource 和 executor 都没埋。

**改进**: 至少加这几个指标:
- `datasource_pool_in_use{id}` / `datasource_pool_idle{id}`(MySQL `db.Stats()`)
- `executor_query_duration_seconds{datasource, type, status}`
- `executor_rows_returned{datasource}`

对一个对外暴露 DB 执行能力的 Agent 框架,**这一组指标是事故定位的命根子**。

**优先级**: P1
**工作量**: 1-2 天(含 metric 命名约定 + 装饰)

---

### C5. `MaxRows` 截断后**没有 truncated 标记**返回上层

**位置**: `framework/executor/executor.go: Result`

**现状**:
```go
if maxRows > 0 && len(out.Rows) >= maxRows { break }
```

LLM 看到 100 行就以为是全集,可能给出错误结论。

**改进**: `Result` 加 `Truncated bool` / `EstimatedTotal int64`:
```go
type Result struct {
    Columns []string
    Rows    [][]any
    AffectedRows   int64
    Truncated      bool   // 新增
    EstimatedTotal int64  // 新增,ES 可填 hits.total
}
```

**优先级**: P0
**工作量**: 30 分钟

---

### C6. `noop` 数据源的测试 stub 太薄

**位置**: `framework/datasource/noop.go`

**改进**: 提供更丰富的 `fakeDS`,能模拟 ping 失败、返回伪 schema,作为 framework 提供的测试夹具。

**优先级**: P3
**工作量**: 0.5 天

---

## 🔵 D 架构方向(更大一步,可选)

### D1. 把"读"和"写"拆成两个执行器接口

详见 `02-executor.md A1` —— 与 executor 层一起改。

### D2. DSL 抽象 vs. 透传 raw SQL/JSON

详见 `02-executor.md D2`。

---

## 验收标准

每条 P0 / P1 改进项必须满足:
1. 有专门单测覆盖(table-driven)
2. CHANGELOG 记录行为变化
3. 若有 breaking change,提供 migration 说明
4. P0 项必须有 benchmark / 安全测试(如 SQL 注入用例)

---

## 实施清单(可直接转 issue)

### Phase 1(本周, ~1 人天)
- [ ] **P0** A2: 删 `isESWriteDSL`,改 `_search` API 兜底
- [ ] **P0** B3: `ConfigFromMap` 补连接池字段
- [ ] **P0** C5: `Result` 加 `Truncated bool`
- [ ] **P0** A1: MySQL `isWriteDSL` 剥注释 + 拒绝多语句
- [ ] **P0** A3: 三层 `log.Printf` 换 `slog` + 字面量 mask
- [ ] **P2** C1: Registry 重复 ID 处理
- [ ] **P2** C2: Mongo Close 加 5s 超时
- [ ] **P2** B4: Hive 死代码处置(删 / 加 driver 校验)

### Phase 2(1-2 周)
- [ ] **P1** B2 + B1: `DataSource.Type()` + ExecutorRegistry
- [ ] **P1** B5: metadata / executor 公共 dispatcher
- [ ] **P1** C4: connection / query 级 metrics 埋点
- [ ] **P2** C3: ES 支持 CloudID / APIKey
- [ ] **P2** A4: ES Close 真正关连接

### Phase 3(1-2 月)
- [ ] D1 / D2: 见 02-executor.md
