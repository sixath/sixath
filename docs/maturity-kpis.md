# 成熟度 KPI 面板（Task 18）

本文定义 L3（可内部生产）需要持续观测的关键指标、PromQL 查询与告警阈值。指标来源：Portal `/metrics`（`framework/obs`）、Gateway `/metrics`（`gateway/internal/metrics`）、生产 `turn_trace` 表、eval 报告（`evals/`）。

## KPI 一览

| # | KPI | 来源 | 目标 |
|---|-----|------|------|
| K1 | 可用性（请求成功率） | Portal + Gateway | ≥ 99.9% |
| K2 | P99 延迟（Portal / Gateway 分开） | 各 `/metrics` histogram | 有预算值 |
| K3 | 任务完成率 | eval 报告 + `turn_trace` | 相对 baseline 不下降 |
| K4 | 单任务成本 | `agent_tokens_total` + 计费单价 | P95 有预算 |
| K5 | HITL 触发率 | `ask_user`/审批 相关计数 | 有基线、异常告警 |
| K6 | eval 回归通过率 | `evals/reports` gate | 100%（mock 门禁） |
| K7 | CI 时长 | GitHub Actions | < 8 分钟 |

## PromQL（按现有指标）

### K1 可用性

```promql
# Portal 请求成功率（按 agent）
sum(rate(agent_requests_total{status="ok"}[5m])) by (agent)
  /
sum(rate(agent_requests_total[5m])) by (agent)

# Gateway 入站事件成功率
sum(rate(gateway_inbound_events_total{status="ok"}[5m]))
  /
sum(rate(gateway_inbound_events_total[5m]))

# Gateway 5xx 比例（告警）
sum(rate(gateway_http_requests_total{status="server_error"}[5m]))
  /
sum(rate(gateway_http_requests_total[5m]))
```

### K2 P99 延迟

```promql
histogram_quantile(0.99, sum(rate(agent_request_duration_seconds_bucket[5m])) by (le, agent))
histogram_quantile(0.99, sum(rate(gateway_turn_duration_seconds_bucket[5m])) by (le, channel))
histogram_quantile(0.99, sum(rate(gateway_portal_request_duration_seconds_bucket[5m])) by (le, op))
```

### K4 单任务成本

```promql
# token 消耗（input/output 分开）
sum(rate(agent_tokens_total{type="input"}[5m]))
sum(rate(agent_tokens_total{type="output"}[5m]))
```

> 成本 = token × 单价。单价表当前未在指标层暴露，建议后续在 `turn_trace` 落 `estimated_cost`（Task 4 预留字段）后用 SQL 聚合 P95 单任务成本。

### K5 HITL 触发率（待补指标）

当前 `ask_user`/审批没有专用 counter。**待补**：`hitl_requests_total{type=ask_user|confirm|rejected}`。补后查询：

```promql
sum(rate(hitl_requests_total[5m]))
```

### K6 eval 回归通过率

无 PromQL，由 CI `evals` job + `--mode=report -baseline` 门禁产出（`gate_passed`），失败即阻断合并。

## 告警阈值（示例）

| 告警 | 表达式（节选） | 阈值 |
|------|----------------|------|
| Gateway 5xx 过高 | `sum(rate(gateway_http_requests_total{status="server_error"}[5m])) / sum(rate(gateway_http_requests_total[5m]))` | > 1%（5m） |
| Portal 失败率升高 | `sum(rate(agent_requests_total{status="error"}[5m])) / sum(rate(agent_requests_total[5m]))` | > 1%（5m） |
| 企微断连重连 | `sum(increase(gateway_wecom_reconnects_total[10m]))` | > 5 次/10m |
| 出站投递失败 | `sum(rate(gateway_reply_deliveries_total{status="error"}[5m]))` | > 0（持续 5m） |
| eval 回归 | CI gate `gate_passed=false` | 阻断合并 |

## 现状与待补

| 项 | 状态 |
|----|------|
| Portal / Gateway `/metrics` | ✅ 已暴露 |
| Gateway → Portal trace 贯通 | ✅ Task 8 |
| 成本 `estimated_cost` 落 `turn_trace` | ⏳ Task 4 预留字段，待接线 |
| 重试率指标 | ⏳ Task 3 打点到 OTel span attribute，未落地为 counter；建议补 `agent_retry_total` |
| HITL 触发率指标 | ⏳ 待补 `hitl_requests_total` |
| Grafana dashboard JSON | 暂以本文档化的 PromQL 代替；接入 Grafana 后可从本文导出 |
