# Runbook：幂等冲突排查

## 症状

- 同一 `msgid` / `idempotency_key` 被重复处理（群机器人重复回复、webhook 重复开 turn）。
- 或相反：应该被处理的请求被判定为重复而丢弃。

## 排查

1. 看幂等指标：`gateway_idempotency_events_total{outcome="duplicate"}` 是否异常（过高 = 大量重复；`race` 高 = 并发竞争）。
2. 看 Gateway 日志 `idempotency_get_failed` / `idempotency_begin_failed`（存储抖动）。
3. 确认 key 生成：webhook 用 `idempotency_key`，wecom_bot 用 `msgid`；若上游没传 `idempotency_key`，则无从去重。

## 处置

- 存储读取失败导致 fail-open（重复处理）：确认幂等存储（内存单副本 / 未来 Redis）健康；多副本时必须用共享存储（ADR-0002 + Task 9）。
- 上游 `msgid` 不稳定（每次重试都变）：与上游对齐稳定的幂等 key。
- TTL 过短（默认 10 分钟）导致慢重试漏网：按业务重试窗口调大 `idempotency.NewStore(ttl)`。

## 恢复确认

- `gateway_idempotency_events_total{outcome="duplicate"}` 回到预期区间。
- 不再出现重复回复或误丢弃。
