# ADR-0002：Gateway wecom_bot 采用单副本租约（而非多副本并发订阅）

- **状态**：已接受（2026-09-12，单副本部署已确认）
- **日期**：2026-09-12

## 背景

企微智能机器人长连接（WSS）同一 `bot_id` 若被多个 Gateway 副本同时订阅，会重复收消息/重复回复。幂等（msgid 去重）可挡部分重复，但更稳妥是**同一时刻仅一个副本订阅**。

## 决策

- **采用单副本**部署 Gateway（已确认，2026-09-12）：幂等用进程内 `MemoryStore`（10 分钟 TTL），wecom_bot 长连接天然单实例，无需分布式租约。
- 若未来需多副本，再引入 **bot 租约**：向 Portal 申请/续约 workspace 租约（复用 `growth_workspace_leases` 的成熟模式，或新增 `gateway_bot_leases`）；未持租约的副本不订阅 WSS 并打 warn 指标；租约丢失主动断开 + 指数退避重新竞争。届时幂等存储需从 `MemoryStore` 替换为共享存储（`Store` 接口已就绪，Redis 实现为 drop-in）。

## 理由

- 企微 WSS 的订阅是「有状态、单实例友好」的语义，多副本并发订阅收益小、风险大。
- 租约复用 Portal 已有的 lease 模式，避免 Gateway 自建第二套分布式锁。

## 后果

- 单副本：实现简单、无额外依赖；代价是 Gateway 不可多副本横向扩展（可接受，Gateway 是薄代理层）。
- 多副本 + 租约：需要 Portal 租约 API + 共享存储（Redis/DB）；依赖幂等存储也接口化（Task 9 已完成接口）。
- 预留已落地：`internal/leadership.Leader` 接口 + `AlwaysLeader`（单副本）；wecom_bot 循环已接入领导权检查（非 leader 不订阅 WSS、打 warn + 轮询），多副本时 drop-in 分布式租约实现即可。
