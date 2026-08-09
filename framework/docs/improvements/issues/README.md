# Improvement Issues 索引

> 27 份 issue 由 [README.md](../README.md) 中的三模块改进报告拆分而来,每份 issue 自含验收标准、测试要求、风险说明。
> 已批量导入 GitHub Issues #2-#28(`sixath/framework`),映射见 [created-issues.tsv](created-issues.tsv)。
> **进度(2026-06-04): 27/27 已完成**(P0 全部 + P1 全部,含 EX-D3 / MW-B1 / MW-B3 / MW-C5 / MW-D2)。

## 状态

- ⬜ 待办 / 🟦 进行中 / ✅ 已完成 / ❌ 已取消

> 更新策略: 修改对应 issue 文件的"状态"字段,同步更新本表"状态"列。

## 数据源(framework/datasource)

| ID | 标题 | 优先级 | 状态 | 工作量 | GH issue |
|----|------|-------|------|-------|---------|
| [DS-A1](DS-A1-mysql-iswritedsl-bypass.md) | MySQL `isWriteDSL` 关键字前缀判定可被绕过 | P0 | ✅ | 0.5d–3d  [#3](https://github.com/sixath/framework/issues/3) |
| [DS-A3](DS-A3-dsn-and-log-leak.md) | MySQL DSN 明文密码 + 日志直接打印 DSL | P0 | ✅ | 1d  [#4](https://github.com/sixath/framework/issues/4) |
| [DS-B3](DS-B3-configfrommap-pool-fields.md) | `ConfigFromMap` 不读连接池参数与 read_only | P0 | ✅ | 30m  [#2](https://github.com/sixath/framework/issues/2) |
| [DS-C5](DS-C5-result-truncated-flag.md) | `Result` 缺 truncated 标记 | P0 | ✅ | 30m  [#8](https://github.com/sixath/framework/issues/8) |
| [DS-B1](DS-B1-type-and-executor-registry.md) | 引入 `DataSource.Type()` 与按 type 路由的 ExecutorRegistry | P1 | ✅ | 2-3d  [#5](https://github.com/sixath/framework/issues/5) |
| [DS-B5](DS-B5-metadata-executor-dispatcher.md) | metadata / executor 公共 dispatcher 抽取 | P1 | ✅ | 1-2d  [#6](https://github.com/sixath/framework/issues/6) |
| [DS-C4](DS-C4-observability.md) | datasource 与 executor 的可观测性埋点 | P1 | ✅ | 1-2d  [#7](https://github.com/sixath/framework/issues/7) |

## 执行器(framework/executor)

| ID | 标题 | 优先级 | 状态 | 工作量 | GH issue |
|----|------|-------|------|-------|---------|
| [EX-A2](EX-A2-readonly-default-true.md) | `ExecuteOptions{}` 零值改为 `ReadOnly=true` | P0 | ✅ | 0.5d  [#10](https://github.com/sixath/framework/issues/10) |
| [EX-A3](EX-A3-es-columns-stable.md) | ES 执行器 columns 取首行 + map 顺序不稳定 | P0 | ✅ | 0.5d  [#11](https://github.com/sixath/framework/issues/11) |
| [EX-A4](EX-A4-schema-error-by-code.md) | schema-error 判定改用错误码而非子串匹配 | P0 | ✅ | 0.5d  [#12](https://github.com/sixath/framework/issues/12) |
| [EX-A1](EX-A1-reader-writer-split.md) | 拆分 Reader / Writer 接口,在类型层消除越权 | P1 | ✅ | 1w  [#9](https://github.com/sixath/framework/issues/9) |
| [EX-B2](EX-B2-maxrows-pushdown.md) | `MaxRows` 下推到 SQL `LIMIT` / ES `size` | P1 | ✅ | 1-2d  [#13](https://github.com/sixath/framework/issues/13) |
| [EX-C6](EX-C6-es-mongo-tests.md) | ES / Mongo 执行器补齐单元测试 | P1 | ✅ | 2d  [#14](https://github.com/sixath/framework/issues/14) |
| [EX-D3](EX-D3-parameterized-queries.md) | 参数化查询作为一等公民 | P1 | ✅ | 1w  [#15](https://github.com/sixath/framework/issues/15) |

## 中间件(framework/middleware)

| ID | 标题 | 优先级 | 状态 | 工作量 | GH issue |
|----|------|-------|------|-------|---------|
| [MW-A1](MW-A1-chainbuilder-order-direction.md) | `ChainBuilder` 的 Order 排序方向疑似有 bug | P0 | ✅ | 0.5d  [#16](https://github.com/sixath/framework/issues/16) |
| [MW-A2](MW-A2-cache-key-completeness.md) | Cache key 缺漏 Parts / Metadata,导致错误命中 | P0 | ✅ | 1d  [#17](https://github.com/sixath/framework/issues/17) |
| [MW-A3](MW-A3-ratelimiter-leak.md) | RateLimiter 的 buckets map 永不清理 → 内存泄漏 | P0 | ✅ | 0.5d  [#18](https://github.com/sixath/framework/issues/18) |
| [MW-A4](MW-A4-metrics-token-parsing.md) | Metrics token 解析有 bug,数据全废 | P0 | ✅ | 0.5d  [#19](https://github.com/sixath/framework/issues/19) |
| [MW-B4](MW-B4-logging-slog.md) | Logging 改用 slog,err 安全转义 | P0 | ✅ | 0.5d  [#23](https://github.com/sixath/framework/issues/23) |
| [MW-B1](MW-B1-agent-context.md) | 引入 agent.Context 作为 per-request 共享通道,支持 short-circuit | P1 | ✅ | 1-2w  [#20](https://github.com/sixath/framework/issues/20) |
| [MW-B2](MW-B2-cache-sweep.md) | CacheStore 加 sweep / LRU,过期项真正删除 | P1 | ✅ | 1d  [#21](https://github.com/sixath/framework/issues/21) |
| [MW-B3](MW-B3-content-safety-aho-corasick.md) | ContentSafety 改用 Aho-Corasick + 按 Role / Parts 区分 | P1 | ✅ | 1d  [#22](https://github.com/sixath/framework/issues/22) |
| [MW-B5](MW-B5-tracing-attributes.md) | Tracing span 加 agent.name 与丰富 attribute | P1 | ✅ | 0.5d  [#24](https://github.com/sixath/framework/issues/24) |
| [MW-C1](MW-C1-test-coverage.md) | 中间件层测试矩阵补完 | P1 | ✅ | 2d  [#25](https://github.com/sixath/framework/issues/25) |
| [MW-C4](MW-C4-cache-singleflight.md) | CacheMiddleware 内嵌 Singleflight 防 stampede | P1 | ✅ | 0.5d  [#26](https://github.com/sixath/framework/issues/26) |
| [MW-C5](MW-C5-typed-metadata.md) | Metadata 高频字段 typed 化 | P1 | ✅ | 1d  [#27](https://github.com/sixath/framework/issues/27) |
| [MW-D2](MW-D2-streaming-middleware.md) | StreamMiddleware 流式接口(为 SSE 铺路) | P1 | ✅ | 2w  [#28](https://github.com/sixath/framework/issues/28) |

## 按优先级聚合

### ✅ 已完成 — P0 全部(12 个)
- 批次 1(2026-06-03): DS-B3, DS-C5, EX-A3, EX-A4, MW-A4
- 批次 2(2026-06-04): DS-A1, DS-A3, EX-A2, MW-A1, MW-A2, MW-A3, MW-B4

### ✅ 已完成 — 注册表链路(3 个)
- DS-B1, EX-A1, DS-B5

### ✅ 已完成 — 可观测性(2 个)
- DS-C4, MW-B5

### ✅ 已完成 — Cache(2 个)
- MW-B2, MW-C4（MW-A2 key 完整性 prerequisite）

### ✅ 已完成 — Result / 性能(1 个)
- EX-B2（与 DS-C5 Truncated 配套）

### ✅ 已完成 — 测试与质量(2 个)
- EX-C6, MW-C1

### ✅ 已完成 — P1 收尾(5 个)
- EX-D3(参数化 :name/? + execute_read)
- MW-B1(agent.Context + metrics source)
- MW-B3(Aho-Corasick + Parts/Role)
- MW-C5(typed metadata 常量 + Usage)
- MW-D2(StreamMiddleware + chat_stream 模板)

## 按主题聚合(同 PR 提交建议)

### 主题 1 — 日志统一改造 ~~(已完成)~~
- ~~DS-A3 + MW-B4~~ ✅(executor 可选 `Logger`; middleware 已 slog)
- 剩余: `obs/logger.go`、其它包 `log.Printf` 跟随 DS-C4 统一

### 主题 2 — 可观测性 ~~(已完成)~~
- ~~DS-C4(metrics + tracing 埋点)~~ ✅
- ~~MW-B5(tracing attribute 丰富)~~ ✅
- ~~MW-A4(metrics token 解析修)~~ ✅

### 主题 3 — 注册表与类型路由 ~~(已完成)~~
- ~~DS-B1(`Type()` + ExecutorRegistry)~~ ✅
- ~~DS-B5(公共 dispatcher)~~ ✅
- ~~EX-A1(Reader/Writer 拆分)~~ ✅

### 主题 4 — Cache 改造 ~~(已完成)~~
- ~~MW-A2(key 完整性)~~ ✅
- ~~MW-B2(sweep / LRU)~~ ✅
- ~~MW-C4(singleflight)~~ ✅

### 主题 5 — Result 形状 ~~(已完成)~~
- ~~DS-C5(Truncated 标记)~~ ✅
- ~~EX-A3(ES columns 稳定)~~ ✅
- ~~EX-B2(MaxRows 下推)~~ ✅

## 工作量汇总

| 优先级 | issue 数 | 已完成 | 累计工作量(人日) |
|--------|---------|--------|------------------|
| P0 | 12 | 12 | ~7 |
| P1 | 15 | 15 | ~25 |
| **总计** | **27** | **27** | **~32(约 6-7 周单人)** |

## 引用关系图

```
DS-B1 ──┬──► DS-B5
        └──► EX-A1 ──► EX-D3
                  ├──► EX-B2
                  └──► DS-C5

MW-A4 ──► MW-B5
MW-A2 ──┬──► MW-B2
        └──► MW-C4
MW-B1 ──► MW-D2
```
