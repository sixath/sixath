# [DS-C4 / EX-C2] datasource 与 executor 的可观测性埋点

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/datasource、framework/executor、framework/obs |
| **状态** | 已完成 |
| **关联报告** | [01-datasource.md C4](../01-datasource.md)、[02-executor.md C2](../02-executor.md) |
| **预估工作量** | 1-2 天 |
| **依赖** | 无 |

## 问题位置

- `framework/datasource/*.go` 全部
- `framework/executor/*.go` 全部
- `framework/obs/`(已有 Prom 客户端 + OTel,需新增 metric 定义)

## 现状

- datasource: 0 metrics,0 tracing
- executor: 0 metrics,0 tracing,只有 `log.Printf`

`obs/` 包已就绪但**这两层完全没接入**,导致生产环境出问题时无法定位。

## 改进方案

### Metrics(Prometheus)

在 `obs/metrics.go` 新增定义:

```go
// 数据源连接池
DatasourcePoolInUse  = prom.NewGaugeVec(... {"id"})    // 周期性 db.Stats() 上报
DatasourcePoolIdle   = prom.NewGaugeVec(... {"id"})

// 执行器指标
ExecutorDurationSec  = prom.NewHistogramVec(... {"datasource", "type", "op", "status"})
ExecutorRowsReturned = prom.NewHistogramVec(... {"datasource", "type"})
ExecutorErrorsTotal  = prom.NewCounterVec(... {"datasource", "type", "error_kind"})
// error_kind: schema | readonly | timeout | driver | unsupported
```

### Tracing(OpenTelemetry)

在每个执行器入口起 span:
```go
ctx, span := tracer.Start(ctx, "Executor.Execute",
    trace.WithSpanKind(trace.SpanKindClient),
    trace.WithAttributes(
        attribute.String("db.system", ds.Type()),
        attribute.String("db.datasource_id", datasourceID),
        attribute.Int("executor.max_rows", opts.MaxRows),
        attribute.Bool("executor.read_only", opts.ReadOnly),
    ),
)
defer span.End()

// 执行后补:
span.SetAttributes(
    attribute.Int("executor.rows_returned", len(out.Rows)),
    attribute.Bool("executor.truncated", out.Truncated),
)
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}
```

### 连接池采样 goroutine

在 `(*MySQLDataSource).DB()` 改造或在 Registry 上层启动一个 sampler:
```go
// 每 30s 采样一次所有 mysql 类型数据源的 db.Stats()
go func() {
    tick := time.NewTicker(30 * time.Second)
    for range tick.C {
        for _, ds := range reg.List() {
            if p, ok := ds.(interface{ DB() *sql.DB }); ok {
                s := p.DB().Stats()
                obs.DatasourcePoolInUse.WithLabelValues(ds.ID()).Set(float64(s.InUse))
                obs.DatasourcePoolIdle.WithLabelValues(ds.ID()).Set(float64(s.Idle))
            }
        }
    }
}()
```

## 验收标准

- [ ] 新增的 metric 在 `obs/metrics.go` 定义,有完整文档注释
- [ ] MySQL / ES / Mongo executor 的 Execute 入口都起 OTel span,SetAttributes 至少包含 `db.system`、`db.datasource_id`、`executor.read_only`、`executor.max_rows`、`executor.rows_returned`、`executor.truncated`
- [ ] 错误路径 `span.RecordError` + `span.SetStatus(codes.Error)`
- [ ] `error_kind` 标签覆盖所有错误归类(schema / readonly / timeout / driver / unsupported / other)
- [ ] 连接池采样 goroutine 在 Registry 上启动,30s 周期可配置
- [ ] Prometheus `/metrics` 端点能 scrape 到新指标,值非零

## 测试要求

- 单测: span attribute 正确性(用 `tracetest.NewInMemoryExporter`)
- 单测: metric label 基数控制(error_kind 不出现 unbounded 字符串)
- 集成测试: 起一个 prom server,执行 100 次查询,断言 ExecutorDurationSec 有 100 个 sample

## 风险

- **Cardinality 爆炸**: `datasource_id` 标签可能上千。Mitigation: 按 type 聚合时不带 id,或限制 datasource 总数
- 连接池 sampler 不能阻塞 datasource 的 Close。Mitigation: 用 ctx 控制 sampler 生命周期,Close 时优雅停止

## 关联 issue

- [DS-A3 / EX-C1](DS-A3-dsn-and-log-leak.md): logger 改造,本 issue 与之配合,日志 + 指标双管齐下
