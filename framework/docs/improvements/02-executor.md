# Executor 执行器层 改进报告

> 模块: `framework/executor/`
> 评审日期: 2026-06-03
> 综合评分: **6.0 / 10**

## 模块定位

跨数据源的统一查询执行器,目前支持 MySQL / Elasticsearch / MongoDB。被 `framework/tool/execute_read` 等工具调用,作为 LLM 与数据库之间的最后一道屏障(只读判定 / 超时 / 行数截断)。

## 评分

| 维度 | 分数 | 备注 |
|------|------|------|
| 抽象设计 | 7 / 10 | Executor 接口干净,但读/写未拆是会扩散的债务 |
| 跨源一致性 | 6 / 10 | SchemaRelatedError 是亮点;columns 处理、目标命名、写判定三处不一致 |
| 可扩展性 | 5 / 10 | MultiExecutor 类型断言链不符合开闭原则 |
| 安全性 | 4 / 10 | 默认 ReadOnly=false + 子串错误判定 + 全局 log 打 DSL |
| 性能 | 5 / 10 | Mongo 下推到位,MySQL/ES 都是先拉再切 |
| 健壮性 | 6 / 10 | ES 首行取 columns / 异构文档丢字段 / map 顺序不稳定 |
| 可观测性 | 2 / 10 | 0 metrics、0 tracing,只有 stdlib log;**离生产化最远的一项** |
| LLM 友好度 | 7 / 10 | `[]byte→string` 等细节贴合 LLM 使用,但缺 Truncated、缺一致目标命名 |
| 测试 | 6 / 10 | MySQL 测试质量高,ES / Mongo 完全空白 |

---

## 🔴 A 严重 — 影响正确性 / 安全

### A1. `Executor.Execute` 是"读写合一"的口子,后果蔓延

**位置**: `framework/executor/executor.go: Executor interface`

**现状**:
```go
type Executor interface {
    Execute(ctx, ds, dsl, opts) (*Result, error)
}
```

**代价**:
1. 每个实现都要重写 `isWriteDSL`(MySQL 有,ES 有,Mongo 没有 —— 三种执行器三套判定逻辑)
2. `Result` 同时承载 `Columns/Rows` 和 `AffectedRows`,两类完全不同的形状;LLM 看到 `AffectedRows=0` 分不清是"写了 0 行"还是"读结果"
3. LLM 工具很难在类型层面"只给读权限"

**改进**(强烈建议):
```go
type Reader interface {
    Query(ctx, ds, dsl, opts) (*QueryResult, error)
}
type Writer interface {
    Exec(ctx, ds, dsl, opts) (*ExecResult, error)
}
```
- Mongo 直接只实现 `Reader`,**类型系统消除越权**
- LLM `execute_read` 工具只持 `Reader`,`execute_write` 工具单独持 `Writer` —— 越权变成编译期错误
- 旧 `Executor` 作为兼容门面保留一段时间

**优先级**: P1
**工作量**: 1 周(含工具层迁移)

---

### A2. 仅 MySQL 写路径**没绕过 `ReadOnly` 校验**

**位置**: `framework/executor/mysql.go: (*MySQLExecutor).Execute`

**现状**:
```go
if opts.ReadOnly && isWriteDSL(dsl) { return ErrReadOnlyViolation }
if isWriteDSL(dsl) { return e.execWrite(ctx, db, dsl) }   // 不传 opts → 无 MaxRows、无 logging、无 metrics
```

**问题**:
- `execWrite` 完全脱离 `opts`,无法限流、无法 dry-run
- `MultiExecutor` 把 mongo / es 当成"只读";如果未来加 Mongo 写,`opts.ReadOnly` 默认 false → **默认允许** —— 定时炸弹

**改进**: ReadOnly 设成"约束默认值",`ExecuteOptions{}` 零值应该是 `ReadOnly=true`,显式 opt-in 写入。等同 Go context 的 "deny by default"。

**优先级**: P0
**工作量**: 0.5 天(改默认值 + 所有调用点排查 + 单测)

---

### A3. ES 执行器整体 schema 假设有 bug

**位置**: `framework/executor/elasticsearch.go: (*ESExecutor).execSearch`

**现状**:
```go
columns := []string{}
if len(hits) > 0 {
    seen := make(map[string]struct{})
    for k := range hits[0].Source { seen[k] = struct{}{} }
    for k := range seen { columns = append(columns, k) }
    for _, h := range hits { ... row[i] = h.Source[col] }
}
```

**三个问题**:
1. **`columns` 取自首行**: ES 文档异构,后面 doc 多出的字段直接丢失
2. **map 遍历顺序未定义**: 每次 columns 顺序不同,LLM 看到的列序漂移,prompt 缓存命中率下降
3. **`columns/rows` 从 nil append**: 大 result 时 GC 压力

**改进**: 聚合所有行的 key union,做 deterministic sort(字母序,或 `_id, _score` 优先),然后构 rows;返回额外的 `_id` / `_score` / `_index` 三列。

**优先级**: P0
**工作量**: 0.5 天

---

### A4. `IsSchemaRelated` 用**子串匹配**而不是错误码

**位置**: `framework/executor/mysql.go: isMySQLSchemaRelated` 与 `elasticsearch.go: isESSchemaRelated`

**现状**:
```go
strings.Contains(s, "Unknown column") || strings.Contains(s, "1054") || strings.Contains(s, "42S22")
```

**问题**:
- MySQL 驱动若做了 locale(中文 `未知列`),立刻失效
- 任意用户输入的字面量含子串 `"1054"`(如 `WHERE port=1054`)会误命中

**改进**:
```go
var me *mysql.MySQLError
if errors.As(err, &me) && me.Number == 1054 { ... }
```
ES 错误同理: 解析 JSON body 的 `error.type`(如 `query_shard_exception` / `index_not_found_exception`)。

**优先级**: P0
**工作量**: 0.5 天

---

## 🟠 B 中等 — 影响可扩展性 / 性能

### B1. `MultiExecutor` 是 O(N) 类型断言链

详见 `01-datasource.md B1`。

**优先级**: P1
**工作量**: 2-3 天(与 datasource B1 / B2 合并)

---

### B2. `MaxRows` 没下推到底层 — 拉了全部再截断

**位置**: `framework/executor/mysql.go: execQuery` + `elasticsearch.go: execSearch`

**现状**:
- **MySQL**: `SELECT * FROM big_table` + `MaxRows=10` → 仍从 server 拉全表,客户端 `break`(网络 + 内存依旧爆)
- **ES**: body 里的 `size` 不被覆盖,只在客户端切片(server 仍取 10000 doc)
- **Mongo**: 已经下推(`SetLimit(MaxRows)`) —— 唯一做对的

**改进**:
- **MySQL**: 在 `dsl` 末尾加 `LIMIT ?`,或 wrap `SELECT * FROM (<dsl>) AS _sub LIMIT N`(子查询安全包裹,不破坏聚合 / order)
- **ES**: 把 body parse 出来,如未设 `size`,注入 `MaxRows`;已设但大于 → clamp

**优先级**: P1
**工作量**: 1-2 天

---

### B3. ES `WithBody(strings.NewReader(body))` 没 JSON 预校验

**位置**: `framework/executor/elasticsearch.go: execSearch`

**现状**: LLM 输出非法 JSON 直接打到 ES server,错误是 server 解析后的字符串,语义不如 client 端 `json.Unmarshal` 友好;请求体大小无上限。

**改进**:
```go
if !json.Valid([]byte(body)) {
    return nil, &SchemaRelatedError{Err: fmt.Errorf("invalid JSON body")}
}
if len(body) > maxBodyBytes {
    return nil, fmt.Errorf("body too large: %d > %d", len(body), maxBodyBytes)
}
```
非法 JSON 立即返回 `SchemaRelatedError`,让 LLM 自我修复。

**优先级**: P2
**工作量**: 0.5 天

---

### B4. Mongo Filter 顶层类型不一致

**位置**: `framework/executor/mongodb.go: Execute`

**现状**:
```go
filter := any(bson.D{})       // 空时是 bson.D
if q.Filter != nil { filter = q.Filter }  // 非空时是 map[string]any
```

**问题**: Mongo driver 接受两者,但 `$and: [...]` 等 operator 在 map 序列化时**顺序不稳定**,影响命中索引。

**改进**: 统一用 `bson.D`(有序)或 `bson.M` 一种。

**优先级**: P2
**工作量**: 0.5 天

---

### B5. ES 与 Mongo 的 target 传参风格不一致

**现状**:
- ES: 索引名通过 `opts.Params["index"]` 旁路传入(从 DSL JSON 外)
- Mongo: collection 在 DSL JSON 里(`{"collection":"users", ...}`)

LLM 工具描述要给两套不同模板,容易混乱。

**改进**: 统一把目标(table/index/collection)放进 `ExecuteOptions.Target string`,DSL 只描述查询本体。

**优先级**: P2
**工作量**: 1 天(含工具描述更新)

---

## 🟡 C 较小但应做

### C1. `log.Printf` 直接打全局 logger

**位置**: `framework/executor/mysql.go` + `elasticsearch.go`

```go
log.Printf("exe sql: %s", dsl)
log.Printf("elasticsearch dsl %s", dsl)
```

**问题**:
- 走 stdlib `log`,**不经过项目 `obs/logger`**
- 无结构化字段,过滤 / 采样困难
- DSL 含敏感字面量(IP / token / 邮箱)
- 没采样和级别开关,压测会爆

**改进**: 依赖注入 logger;级别 `debug`;敏感字段 mask(`'[^']*'` → `'***'`)。

**优先级**: P0
**工作量**: 1 天(三层一起改)

---

### C2. 没有任何 metrics / tracing

**位置**: `framework/executor/*`

**改进**: 最起码应有:
- `executor_duration_seconds{datasource, op, status}`
- `executor_rows_returned{datasource}`
- `executor_errors_total{datasource, error_kind}`(schema / readonly / timeout / driver)
- 一条 OTel span,把 SQL/DSL 作为 attribute(脱敏后)

对 Agent 平台,**这组指标是线上事故定位 + LLM 行为画像的双重命脉**。

**优先级**: P1
**工作量**: 1-2 天

---

### C3. `Result` 缺 truncated 标记

详见 `01-datasource.md C5`。

**优先级**: P0
**工作量**: 30 分钟

---

### C4. `Timeout` 单位是秒(int),零值陷阱

**位置**: `framework/executor/executor.go: ExecuteOptions`

**现状**:
```go
Timeout int // 超时秒数,0 表示不限制
```

**问题**:
- **0 = 不限制是危险默认值**
- 秒级粒度太粗,内网调用常常 200ms-2s

**改进**:
- 改 `time.Duration`,默认 30s,0 = 默认值,负数 = 不限
- 或直接要求传 ctx 带 deadline,不要再有 Timeout 字段(更 Go-idiomatic)

**优先级**: P2
**工作量**: 0.5 天

---

### C5. ES / Mongo 一次性吸完内存

**位置**: `framework/executor/elasticsearch.go` + `mongodb.go`

**现状**:
- ES: `json.NewDecoder().Decode(&out)` 整个 hits 一次性读到 struct
- Mongo: `cursor.All(&docs)` 同理

`MaxRows=10` 但 ES 返回 1000 条时仍全在内存。

**改进**: 配合 B2 下推 size/limit 后自然缓解;长期可考虑 row-by-row streaming(配合 D2)。

**优先级**: P2
**工作量**: 与 B2 / D2 合并

---

### C6. 测试覆盖偏 MySQL,ES / Mongo 完全没有 unit test

**位置**: `framework/executor/`

**现状**:
- `mysql_test.go` 5 个用例
- ES 和 Mongo 0 个

**改进**:
- ES: 用 `httptest` 风格的 mock transport
- Mongo: `mtest`(官方 mock 框架)
- 覆盖目标: 首行 columns / map 遍历顺序 / 写 DSL 误判 / schema error wrapping

**优先级**: P1
**工作量**: 2 天

---

### C7. `wrapMaybeSchemaRelated` / `wrapESMaybeSchemaRelated` 几乎重复

**位置**: `framework/executor/mysql.go` + `elasticsearch.go`

**改进**: 抽公共 helper:
```go
func WrapIf(err error, kind func(string) bool, format string, args ...any) error
```

**优先级**: P3
**工作量**: 30 分钟

---

## 🔵 D 架构方向(更大一步)

### D1. Executor 与 Datasource 的连接关系应该上升到 Registry

**改进**: 抽一个泛型 helper(Go 1.18+):
```go
func ClientOf[T any](r *Registry, id string) (T, error) {
    ds, err := r.Get(id); if err != nil { var z T; return z, err }
    if p, ok := ds.(interface{ Client() T }); ok { return p.Client(), nil }
    var z T; return z, ErrUnsupportedDataSource
}
```
每个 executor 一行拿到 typed client。

**优先级**: P2
**工作量**: 1 天

---

### D2. 流式结果接口(为长查询 / export / LLM 流式消费铺路)

**改进**:
```go
type Reader interface {
    QueryStream(ctx, ds, dsl, opts) (RowStream, error)
}
type RowStream interface {
    Columns() []string
    Next() bool
    Scan(dest ...any) error
    Err() error
    Close() error
}
```

现状是 buffered → 一次性返回。对接 LLM 工具流式响应 / `console.table` 增量渲染 / 落地到 CSV,流式接口非常关键。

**优先级**: P3
**工作量**: 1-2 周

---

### D3. 把"参数化查询"做成一等公民

**位置**: `framework/executor/executor.go: ExecuteOptions`

**现状**: `dsl string` 是拼好的 SQL/DSL,LLM 输出**完全没机会做参数绑定**,SQL 注入风险只能靠 ReadOnly 兜底。`ExecuteOptions.Params map[string]any` 已存在但**没在 MySQL 路径用上**(只在 ES 取了 `index`)。

**改进**: 支持 `?` / `:name` 参数化,executor 内部走 `db.QueryContext(ctx, dsl, args...)`;LLM 提示词鼓励参数化输出 —— 安全和缓存命中率双赢。

**优先级**: P1
**工作量**: 1 周(含 LLM 工具 prompt 改造)

---

## 验收标准

每条 P0 / P1 改进项必须满足:
1. 有专门单测(table-driven)
2. CHANGELOG 记录行为变化
3. 涉及 LLM 输入输出形态变化的(如 `Result.Truncated`),要同步更新对应 tool description
4. P0 项必须有 benchmark / 安全测试

---

## 实施清单(可直接转 issue)

### Phase 1(本周, ~1 人天)
- [ ] **P0** A3: ES `columns` 取 union 并 sort
- [ ] **P0** A4: 错误判定改 `*mysql.MySQLError.Number` + ES JSON `error.type`
- [ ] **P0** A2: `ExecuteOptions{}` 零值改 `ReadOnly=true`,显式 opt-in 写入
- [ ] **P0** C1: 三层 `log.Printf` 换 `slog`(与 datasource 合并)
- [ ] **P0** C3: `Result` 加 `Truncated bool`

### Phase 2(1-2 周)
- [ ] **P1** A1: 拆 `Reader` / `Writer` 接口
- [ ] **P1** B1: MultiExecutor → ExecutorRegistry(与 datasource B1/B2 合并)
- [ ] **P1** B2: `MaxRows` 下推到 SQL / ES body
- [ ] **P1** C2: executor 入口埋 OTel span + Prom 指标
- [ ] **P1** C6: ES / Mongo 补单测
- [ ] **P1** D3: 参数化查询一等公民

### Phase 3(1-2 月)
- [ ] **P2** B3 / B4 / B5: ES JSON 预校验 / Mongo bson.D 统一 / target 参数统一
- [ ] **P2** C4: Timeout 改 time.Duration
- [ ] **P2** D1: 泛型 ClientOf helper
- [ ] **P3** D2: 流式 RowStream 接口
