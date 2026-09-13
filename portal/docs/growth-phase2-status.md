# Growth phase-2 子能力状态（Task 17 收口）

本文记录 `portal/configs` 中 Growth 相关 feature flag 的现状与决策，避免「默认关闭 = 未实现」的误读，也作为 phase-2 能力的前进/暂缓依据。

## 结论一句话

四个 phase-2 子能力**均已实现但默认关闭（实验性/按需开启）**，保留开关；复盘 agent 的 full-tools 通用工具集**有意不接入**（安全约束）。

## Feature flags 逐项决策

| flag | 作用 | 状态 | 决策 |
|------|------|------|------|
| `combined_review_enabled` | 双 pending 同 tick 走单次 LLM（L2） | 已实现，默认 false | 保留，实验性；开启前需用 eval 证明「单次 LLM」比「两次」更省且不降质 |
| `curator_enabled` | 启动 R2b workspace Curator worker | 已实现，默认 false | 保留，实验性；`NewCuratorWorker` 在 false 时返回 nil |
| `session_end_memory_review_enabled` | C2：session 结束后按未达阈值计数置 `pending_memory` | 已实现，默认 false | 保留，实验性 |
| `session_end_skill_review_enabled` | C2s：session 结束后按活动置 `pending_skill` | 已实现，默认 false | 保留，实验性 |

> 判定依据：以上 flag 均被非 proto 代码消费（`internal/service/growth_worker.go`、`curator_worker.go`、`internal/biz/growth.go`、`growth_provider.go`），非纯声明。因此**不删除**，按「实验性、默认关闭、按需开启」处理。

## full-tools 决策（`growth_agent_review.go` 原 `TODO(phase-2)` 收口）

复盘 agent（`buildReviewRegistry`）的通用执行工具集**有意不接入**，工具面保持瘦身：

1. **安全**：后台复盘是策展员，不应具备 destructive 能力（shell/terminal/data-write）。该约束由 `TestSpawnReviewAgent_InjectsWorkspaceAndSkillManageTool` 的黑名单断言（`shell`/`terminal`/`bash`/`execute_command` 不得出现）持续守护。
2. **无统一入口**：portal 当前没有单一「注册通用 agent 工具」入口，各调用点自建 registry；临时拼装会制造两套心智模型。

**若未来要开 full-tools**，前置条件：先引入统一工具注册入口，并让 `ApprovalPolicy`（framework Task 7）对复盘 agent 禁用 destructive 级工具，再扩展。

## 验证

```bash
cd portal && go test ./internal/service/ -run TestSpawnReviewAgent -count=1
```
