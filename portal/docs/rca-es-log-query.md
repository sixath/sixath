# RCA `es_log_query`（ELK 日志）

## 推荐配置（elasticsearch 数据源）

新建工具类型 **数据源 / datasource**，子类型 **elasticsearch**，填写：

| 字段 | 说明 |
|------|------|
| 工具目录名 | 即 `es_log_query` 的 `cluster`（如 `zj-elk`） |
| 连接 | DSN / Host+Port + 可选认证 |
| **默认索引** `default_index` | 未传 `index` 时使用；请填本集群真实 pattern，不要留空 |
| **用途** `purpose` | 给模型选集群，如「应用日志」 |
| `trace_id` 字段 | 可选，默认 `trace_id` |
| **日志正文列** `body_field` | 可选。未知 `field:value` 时改写到该列。不填则按 mapping 与常见列名（`message` / `msg` / `log` / `body` / `@message` / `M`）推断 |

把该数据源绑定到 Agent。**不要新建 RCA「ELK 日志 / `es_log_query`」工具**；绑 ≥1 套 ES 数据源后运行时会自动注册 `es_log_query`。

查询必须带 `cluster`（等于数据源工具名）：

```
es_log_query(cluster="<elasticsearch tool name>", trace_id=...)
```

只绑一套也必须传 `cluster`。漏传会永久错误并列出可用集群，不会默认打第一套。同一 Agent 可绑多套；同一任务可再调一次不同的 `cluster`。

`index` 必须是本集群真实索引或 pattern。匹配不到任何物理索引时返回 `ok: false`、`hit_status=error`、`index_error=unresolved`，并带 `suggested_index_patterns`——这不是「没日志」。`hit_status=empty` 才表示索引和字段都合法但仍无文档；`hit_status=suspect` 表示 0 条但放宽条件后有数据（`diagnosis.probes` 给出各放宽变体的计数），应先修正条件。query / sort / agg_field / fields / time_field 引用了 mapping 里不存在的字段时，执行前即返回 `ok: false` 与 `invalid_arguments`（`keyword=unknown_field`，附 `candidates` 与 `hint`），不会发起查询。字段存在但类型不匹配时仍会改写一次（`query_rewritten` / `rewrite_reason`）。

## 确定性校验与开关（`es_log_query` / `execute_read`）

执行前的参数校验（JSON Schema、ES 字段 / MySQL 表列存在性、`datasource_id` 候选）与 0 结果放宽探测都由框架工具中间件完成。校验所需的 mapping / 表结构拉取失败时 fail-open：照常执行，回包（map 结果）带 `check_skipped`；若此时结果为 0 条则标 `hit_status=suspect`，并在 `diagnosis.hint` 追加 `arg checks skipped: …`（`execute_read` 的 `*QueryResult` 没有 `check_skipped` 字段，只能从这里看到）。ES mapping 拉取失败按「集群 + 索引」负缓存 30s；MySQL 表/列未命中触发的强制刷新按数据源限速 30s，窗口内表不存在视为不适用（不拒绝）。

以下环境变量**每次调用读取**，改完即时生效、无需重启；取值 `off` / `0` / `false` / `no` / `disable`（`disabled`）表示关闭，其余或未设置为开启：

| 变量 | 关闭后 |
|------|--------|
| `SATH_TOOL_ARG_VALIDATION` | 跳过全部执行前校验（Schema 与 ArgChecks），参数原样交给工具 |
| `SATH_TOOL_EMPTY_PROBE` | 不做 0 结果放宽探测，不再产生 `suspect` / `diagnosis.probes` |
| `SATH_STOP_SUSPECT` | 关闭默认的 `suspect_evidence` stop hook（最终回答断言「查不到」但证据只有 empty/suspect 时的提醒） |

单个工具关闭探测：Go 侧配置 `tool.ESLogConfig.DisableEmptyProbe` / `tooldata.ExecuteReadConfig.DisableEmptyProbe` 设为 `true`（对应 spec「`empty_probe: false`」），只影响该工具，不影响全局开关。

## 过渡：已有 RCA 内联 / datasource_id

现网已保存的 RCA ELK（内联 endpoint 或 `datasource_id`）仍会合并进集群表，直到迁到 elasticsearch 数据源。查询同样必须带 `cluster`。不要再新建 RCA ELK。过渡条目请补上 `default_index`；空默认时错误回包会列出发现到的 pattern。

## 设计规格

- 多集群路由：[docs/superpowers/specs/2026-09-02-multi-es-cluster-route-design.md](../../docs/superpowers/specs/2026-09-02-multi-es-cluster-route-design.md)
- 索引/字段落地：[docs/superpowers/specs/2026-09-18-es-log-query-grounding-design.md](../../docs/superpowers/specs/2026-09-18-es-log-query-grounding-design.md)
