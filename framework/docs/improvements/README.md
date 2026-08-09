# Framework 改进报告(架构评审)

> 评审日期: 2026-06-03
> 评审范围: `framework/datasource`、`framework/executor`、`framework/middleware`
> 评审方法: 全量阅读源码 + 使用方接缝 + 单测覆盖分析

## 目录

| 文件 | 模块 | 综合评分 |
|------|------|----------|
| [01-datasource.md](./01-datasource.md) | 数据源适配层 | 6.5 / 10 |
| [02-executor.md](./02-executor.md) | 执行器层 | 6.0 / 10 |
| [03-middleware.md](./03-middleware.md) | 中间件链体系 | 6.5 / 10 |

## 优先级图例

每份报告中改进项分四级,本文档统一使用以下记法:

- **🔴 A 严重** — 直接影响正确性 / 安全 / 数据完整;**生产阻塞项**
- **🟠 B 中等** — 影响可扩展性 / 性能 / 可维护性
- **🟡 C 较小** — 体验、文档、测试缺失类问题
- **🔵 D 方向** — 架构级长期演进项,需要单独立项评估

## 跨模块共性问题

三份报告里出现频次最高的几类问题:

### 1. 可观测性几乎全空白
- datasource / executor / middleware 都依赖 stdlib `log.Printf` 或手拼 JSON 字符串,**未使用项目 `obs/logger`**。
- executor 完全无 metrics 埋点,middleware 仅在 metrics 中间件里有埋点,但 token 解析有 bug(参见 `03-middleware.md A4`)。
- OTel tracing 已接入框架但 span attribute 极薄,APM 工具难做聚合。

**建议**: 立项一次 `obs/` 整改,把三层全部接入结构化日志 + Prometheus + OTel span,做成项目级 instrumentation guideline。

### 2. 类型断言路由违反开闭原则
- `executor.MultiExecutor` 用 if-else 类型断言链路由数据源。
- 新增任何后端(ClickHouse / Postgres / Redis)都要改 3 处。
- 与 `datasource.Registry` 已有的按类型注册模式不一致。

**建议**: `datasource.DataSource` 增加 `Type() string`,引入 `ExecutorRegistry` 按 type 索引(详见 `02-executor.md B1` 与 `01-datasource.md B1`)。

### 3. 默认值是不安全的方向
- `ExecuteOptions.ReadOnly` 零值 = false → 默认放行写。
- `Cache TTL=0` = 永不过期。
- `Timeout=0` = 不限时。
- ContentFilter 黑名单为空 = 全放行。

**建议**: 采用 "deny by default" / "安全副作用最小" 的零值语义,显式 opt-in 危险行为。

### 4. 重复造的轮子
- `datasource.intFromAny` 与 `middleware.MetricsMiddleware` 各自处理 `any → int` 解析,逻辑重复且后者还有 bug。
- MySQL / ES 各自实现 `wrapMaybeSchemaRelated`,逻辑一致只差判定函数。

**建议**: 抽 `framework/internal/anyx`(any 数值解析)与 `executor.WrapIf(err, kindFn, format, args...)` 公共 helper。

### 5. 测试密度与模块关键性不匹配
- datasource registry: 1 个测试文件
- executor MySQL: 5 用例,**ES/Mongo 各 0 用例**
- middleware: 3 用例,**核心 `ChainBuilder` / `MergeGlobalLocal` 完全没覆盖**

**建议**: 把测试覆盖率底线定为 70%,核心调度路径(Chain、Registry.Register、MultiExecutor.Execute)必须有专门用例。

## 实施建议(整体节奏)

### Phase 1 — 立即可做(本周, ~1 人天)
聚焦三份报告里 "🔴 A 严重 + 30 分钟级可做" 清单:

1. `02-executor.md A3`: 删 ES `isESWriteDSL` 误判,改为"只走 `_search` API"
2. `02-executor.md C3`: `Result` 加 `Truncated bool`
3. `02-executor.md A4`: MySQL schema 错误判定改用 `*mysql.MySQLError.Number`
4. `03-middleware.md A4`: middleware metrics token 解析改用 `intFromAny` 思路
5. `03-middleware.md A1`: 给 `ChainBuilder` 加单测,固化 Order 语义(若发现 bug,以测试为准修)
6. `01-datasource.md A1` + `01-datasource.md A3`: MySQL `isWriteDSL` 剥注释 + 拒绝多语句 + DSN 通过 `mysql.Config` 构造
7. `01-datasource.md C1` + `03-middleware.md C2`: 三层 `log.Printf` 全部换 `slog`,DSL/SQL 字段化并 mask 字面量

### Phase 2 — 1-2 周(架构性改动)
- `01-datasource.md B1` + `02-executor.md B1` + `D1`: 引入 `DataSource.Type()`,`MultiExecutor` 重构为 `ExecutorRegistry` 按 type 路由
- `02-executor.md A1`: 拆 `Reader` / `Writer` 接口,LLM 工具按需持有
- `01-datasource.md B5` + `02-executor.md D1`: 抽 metadata / executor 公共 dispatcher
- `03-middleware.md A3`: RateLimiter 加 LRU 上限,防内存泄漏
- `03-middleware.md B2` + `C4`: Cache 加 sweep + singleflight

### Phase 3 — 1-2 月(方向性演进)
- `02-executor.md D2`: 流式 `RowStream` 接口
- `02-executor.md D3`: 参数化查询一等公民
- `03-middleware.md D2`: 流式 `StreamMiddleware`(为 SSE 铺路)
- `03-middleware.md D1`: `agent.Context` per-request 共享通道

## 评分维度参考

每份报告统一使用以下 9 个维度评分:

| 维度 | 含义 |
|------|------|
| 抽象设计 | 接口形状、关注点分离、命名 |
| 实现正确性 | 是否存在功能 bug、逻辑漏洞 |
| 可扩展性 | 新增后端 / 中间件 / 数据源的成本 |
| 安全性 | 鉴权、注入、信息泄漏、默认值安全 |
| 性能 | 内存、CPU、网络下推、并发开销 |
| 健壮性 | 边界处理、资源释放、超时控制 |
| 可观测性 | 日志 / metrics / tracing 完备度 |
| 测试 | 覆盖率、关键路径覆盖、并发测试 |
| LLM 友好度 | 错误反馈、结果形状、参数模板 |
