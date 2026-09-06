# RCA `es_log_query`（ELK 日志）

## 推荐配置（elasticsearch 数据源）

新建工具类型 **数据源 / datasource**，子类型 **elasticsearch**，填写：

| 字段 | 说明 |
|------|------|
| 工具目录名 | 即 `es_log_query` 的 `cluster`（如 `zj-elk`） |
| 连接 | DSN / Host+Port + 可选认证 |
| **默认索引** `default_index` | 未传 `index` 时使用 |
| **用途** `purpose` | 给模型选集群，如「应用日志」 |
| `trace_id` 字段 | 可选，默认 `trace_id` |

把该数据源绑定到 Agent。**不要新建 RCA「ELK 日志 / `es_log_query`」工具**；绑 ≥1 套 ES 数据源后运行时会自动注册 `es_log_query`。

查询必须带 `cluster`（等于数据源工具名）：

```
es_log_query(cluster="<elasticsearch tool name>", trace_id=...)
```

只绑一套也必须传 `cluster`。漏传会永久错误并列出可用集群，不会默认打第一套。同一 Agent 可绑多套；同一任务可再调一次不同的 `cluster`。

## 过渡：已有 RCA 内联 / datasource_id

现网已保存的 RCA ELK（内联 endpoint 或 `datasource_id`）仍会合并进集群表，直到迁到 elasticsearch 数据源。查询同样必须带 `cluster`。不要再新建 RCA ELK。

## 设计规格

见 [docs/superpowers/specs/2026-09-02-multi-es-cluster-route-design.md](../../docs/superpowers/specs/2026-09-02-multi-es-cluster-route-design.md)。
