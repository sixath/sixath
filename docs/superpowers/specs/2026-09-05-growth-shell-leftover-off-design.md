# S31 收口：Growth 退出默认外壳 leftover

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；Growth 默认装配 leftover；2026-09-05 实施）  
**范围**: 默认 HTTP 的 growth metrics、默认 Harness 上的 FailureCapture 装配、零调用 `growth_metadata.go`。不删 `framework/growth`，不拆 worker/curator Loop，不改 skillops。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S12](./2026-09-05-unwire-growth-react-opts-design.md)；[S16](./2026-09-05-background-review-off-design.md)；[S30](./2026-09-05-memory-hub-off-design.md)

**一句话**: worker/nudge 已经默认关；metrics 路由和 FailureCapture 装配点还焊在默认外壳上。拆掉。

---

## 1. 背景

S12–S19：默认 Chat 不再经 growth 钩子；worker/curator yaml 默认 false；nudge 默认 false。现网磁盘 leftover：

| leftover | 现网 |
|----------|------|
| `GET /api/v1/growth/metrics` | `http.go` **总是注册**（对齐 Insights 曾留隐藏路由） |
| `HarnessReActOptions` | `FailureCaptureEnabled` 时注入 `NewFailureCaptureHook` |
| `main.go` | 总是 `EnrichFailureCaptureFromEnv`（`SATH_GROWTH_FAILURE_CAPTURE`） |
| `growth_metadata.go` | 只包装 `MergeReviewMetadata`，**零调用者** |
| `NewFailureCaptureHook` / `framework/growth` / skillops | **保留**（opt-in 器官） |

父规格 P4：默认路径不再接线 growth。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Growth metrics HTTP | **删除**路由与 `growth_metrics.go` |
| 默认 Harness FailureCapture 装配 | **删除**（`HarnessReActOptions` 不再注入；main 不再读 `SATH_GROWTH_FAILURE_CAPTURE`） |
| `SATH_SKILL_MANAGE_CONFIRM_PATCH` | **保留**读取（从 EnrichFailureCapture 迁出，避免误删技能确认） |
| `growth_metadata.go` | **删除** |
| `NewFailureCaptureHook` | **保留**（与 S20 留 hypertool 实现同型；以后零调用再删） |
| worker / curator / skillops / `framework/growth` | **不改** |
| assembler | **不合** |

---

## 3. 行为

```text
GET /api/v1/growth/metrics → 不再注册
HarnessReActOptions → 仍装 workspace hooks.yaml；不再 FailureCapture
SATH_GROWTH_FAILURE_CAPTURE 对默认入口无效
SATH_SKILL_MANAGE_CONFIRM_PATCH 仍生效
NewFailureCaptureHook / GrowthWorker(opt-in) 仍可被测试或以后接线调用
```

---

## 4. 非目标

- 不删 `framework/growth`
- 不改 `provideGrowthWorker` / Curator 门控
- 不改 skill_manage 对 growth lease/patch 的依赖
- 不合 assembler

---

## 5. 成功标准

1. `http.go` 不含 `/growth/metrics`；`growth_metrics.go` 不存在。
2. `agent_builder.go` 不含 `NewFailureCaptureHook` / `FailureCaptureEnabled`。
3. `main.go` 不含 `EnrichFailureCaptureFromEnv` / `SATH_GROWTH_FAILURE_CAPTURE`。
4. `portal/internal/chat/growth_metadata.go` 不存在。
5. `cd portal && go test ./internal/chat ./internal/server ./cmd/backend -count=1` 绿（skip 预存 SQLITE_BUSY）。
