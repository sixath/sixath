# Runbook：企微 WSS 断连 / 重复订阅

## 症状

- `gateway_wecom_reconnects_total` 持续增长。
- 群里机器人重复回复同一消息（重复订阅）或完全不回。

## 排查

1. 看活跃连接数：`gateway_wecom_active_connections{channel="<id>"}` 应为 1（单副本）或恰为持租约副本数。
2. 看重连计数：`sum(increase(gateway_wecom_reconnects_total[10m]))` 若 > 5 说明在反复断连。
3. 看 Gateway 日志 `wecom_bot_disconnected`，确认是网络抖动、鉴权失败（bot_id/secret 轮换后未更新）还是服务端踢出。

## 处置

- 凭证问题：确认 `channels.local.yaml` / 环境变量的 `WECOM_BOT_ID`/`WECOM_BOT_SECRET` 与企微控制台一致。
- 重复订阅（多副本同一 bot_id）：立即缩容到单副本；长期走 bot 租约（ADR-0002）。
- 网络抖动：退避重连是内置行为，观察是否自愈；若持续，检查出网到 `openws.work.weixin.qq.com` 的连通性。

## 恢复确认

- `gateway_wecom_active_connections` 回到预期值，`wecom_reconnects_total` 增量回落。
- 群里发消息能正常收到回复。
