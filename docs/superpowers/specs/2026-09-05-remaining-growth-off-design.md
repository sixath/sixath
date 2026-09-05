# S34 收口：一次性拆掉剩余 Growth 平行 OS

**日期**: 2026-09-05  
**状态**: 已确认（用户要求把剩余 leftover 一次补齐；2026-09-05 实施）  
**范围**: Portal opt-in worker/curator 与 `framework/growth` 整包。`skill_manage` 写盘原语迁到 skillops。不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S33](./2026-09-05-append-learning-off-design.md)

**一句话**: yaml 已关 worker，但 Wire 仍总是构造 GrowthUsecase；skill_manage 只借用 growth 的 lease/pin/patch。把写盘原语留下，平行 OS 删掉。

---

## 1. 背景

S12–S33 已把 Growth 拆出默认 Chat / Harness。磁盘上还活着：

| leftover | 现网 |
|----------|------|
| `ProvideGrowthUsecase` / `NewGrowthRepo` | Wire **总是**构造（即使 `worker_enabled=false`） |
| `GrowthWorker` / `CuratorWorker` | yaml 默认 false 时返回 nil，源码与 DI 仍在 |
| `framework/growth` | runner / curator / learnings / nudge 整包；skillops 只用 lease、pin、`ApplyPatchBatch`、`ScanUserContent`、tracker |
| `growth.llm` | **仍**给 Channel `/route` 分类器（`NewRouteCompleter`），不是复盘 OS |

父规格 §6.3：Growth 主循环 nudge / fork-agent / curator 移出默认；不重写成正确器官。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| skill_manage 写盘原语 | **迁到** `framework/tool/skillops`（`RuntimeWriteLease`、`IsSkillPinned`、`Patch`/`ApplyPatchBatch`、`ScanUserContent`、`SkillsIndexTracker`） |
| `framework/growth` | **整目录删除** |
| Portal worker / curator / GrowthUsecase / GrowthRepo / CronRefRewrite / growthwake | **删除**并拆 Wire |
| `data.go` AutoMigrate 的 growth 表模型 | **去掉**（历史 SQL migration 不删） |
| `conf.Growth` / `EnrichGrowthFromEnv` / `ProvideAgentRouteUsecase` | **保留**（路由分类器仍读 `growth.llm`；不 regen proto） |
| 发货 yaml `worker_enabled` 等死键 | **保留** |
| Channel `auto_route_*` / `MaybeSpill` / hub proto / assembler | **不改 / 不合** |

---

## 3. 行为

```text
github.com/sixath/framework/growth → 不存在
skill_manage 仍能 create/patch/delete（lease/pin/scan 行为不变）
Portal 启动不再构造 GrowthWorker / CuratorWorker / ProvideGrowthUsecase
/route 分类器仍可从 conf.Growth.llm 装配（未配则 fail-open）
```

---

## 4. 非目标

- 不 regen proto（`conf.Growth`、hub_*、`mea_enabled` 死字段留下）
- 不改 Channel / Gateway `auto_route_*`
- 不删 SQL migrations、不 DROP 已有表
- 不改 `MaybeSpill`
- 不合 assembler

---

## 5. 成功标准

1. `framework/growth` 目录不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `github.com/sixath/framework/growth`。
3. `main.go` / `wire_gen.go` 不含 `provideGrowthWorker` / `NewGrowthWorker` / `ProvideGrowthUsecase`。
4. `skill_manage` 测试仍绿。
5. `cd framework && go test ./tool/skillops ./harness ./templates -count=1` 绿。
6. `cd portal && go test ./internal/service ./internal/biz ./internal/data ./cmd/backend ./internal/runtime -count=1` 绿（skip 预存 SQLITE_BUSY）。
