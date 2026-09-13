# Runbook：重试风暴与成本飙升

## 症状

- 单任务 token 消耗 / 成本突增（`agent_tokens_total` 爬升）。
- 模型 provider 出现大量 429（下游限流被重试放大）。

## 排查

1. 看是否 provider 侧持续 5xx/429 触发重试：查 `turn_trace` 的 `retry_count`（Task 3 落 OTel span attribute）。
2. 看 `agent_tokens_total` 增速是否异常（对比小时级基线）。
3. 看长任务是否因上下文压缩失败而反复重试（`memory_extract_*` / L2 摘要失败）。

## 处置

- 若 provider 侧限流 → 优先限流（降低并发）而非提高重试：重试退避已内置，但风暴根因通常是**上游不可用仍继续打**。
- 紧急降级：`SATH_MODEL_RETRY_MAX_ATTEMPTS=1` 临时关重试，恢复后再开。
- 长任务成本失控：调低 `MaxSteps`，或确认上下文压缩（L2）生效、没有因摘要失败反复触发。

## 恢复确认

- `agent_tokens_total` 增速回到基线。
- `turn_trace` 中 `retry_count` 回落，失败归因从「模型抖动」回到「业务/工具」。
